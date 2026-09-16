import { ref } from 'vue'
import api from '../../services/api'
import { blobStore, type BlobStore } from './blobStore'
import { online, onOnline } from './network'
import { OFFLINE_PREFIX, patchOfflineOrder } from './offlineOrders'

/**
 * Очередь того, что исполнитель сделал без сети: снимки, точки трека и отметки
 * «Исполнил». Всё это уходит на сервер, когда сеть появляется, и в правильном
 * порядке: отметка заказа — только после его снимков, иначе сервер откажет в
 * отметке заказу, который требует фото.
 *
 * Очередь переживает перезапуск приложения: описание действий лежит в
 * localStorage, байты снимков — в хранилище файлов (blobStore). Каждое действие
 * несёт ключ отправки, поэтому повтор после потерянного ответа не создаёт на
 * сервере дублей.
 */

export interface PhotoAction {
  kind: 'photo'
  id: string
  orderId: string
  photoKind: 'AREA' | 'SELFIE'
  camera: 'FRONT' | 'REAR'
  clientKey: string
  takenAt: string
  /** Координаты — строками в шесть знаков: так же они подписаны. */
  lat: string | null
  lon: string | null
  accuracy: number | null
  blobKey: string
  createdAt: number
}

export interface PositionPoint {
  lat: number
  lon: number
  accuracy_m?: number | null
  source: 'LIVE'
  device_at: string
  client_key: string
}

export interface PositionsAction {
  kind: 'positions'
  id: string
  points: PositionPoint[]
  createdAt: number
}

export interface ExecuteAction {
  kind: 'execute'
  id: string
  orderId: string
  executedAtDevice: string
  createdAt: number
}

export type QueueAction = PhotoAction | PositionsAction | ExecuteAction

export interface QueueFailure {
  action: QueueAction
  message: string
  at: number
}

export interface QueueTransport {
  uploadPhoto(action: PhotoAction, bytes: Uint8Array): Promise<void>
  sendPositions(points: PositionPoint[]): Promise<void>
  execute(orderId: string, executedAtDevice: string): Promise<void>
}

export interface KeyValue {
  get(key: string): string | null
  set(key: string, value: string): void
}

// Пачка точек трека — не больше, чем принимает сервер за один запрос.
const POSITIONS_PER_ACTION = 500

function newId(): string {
  const c: any = (globalThis as any).crypto
  if (c?.randomUUID) return c.randomUUID()
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 12)}`
}

function errorStatus(err: any): number | undefined {
  return err?.response?.status
}

/**
 * Ошибка, повтор которой ничего не изменит: сервер понял запрос и отказал.
 * Нет ответа, 408, 429 и 5xx — временные: действие остаётся в очереди.
 */
export function isPermanentFailure(err: any): boolean {
  const status = errorStatus(err)
  return status !== undefined && status >= 400 && status < 500 && status !== 408 && status !== 429
}

function failureText(err: any): string {
  const data = err?.response?.data
  const text = typeof data === 'string' ? data : data?.message || data?.error
  return (text || err?.message || 'ошибка отправки').toString().trim()
}

export class ProofQueue {
  /** Сколько действий ждут отправки. */
  readonly pending = ref(0)
  /** Сколько из них — снимки. */
  readonly pendingPhotos = ref(0)
  /** Отказы сервера — для показа исполнителю. */
  readonly failures = ref<QueueFailure[]>([])

  private flushing: Promise<void> | null = null

  constructor(
    private readonly storage: KeyValue,
    private readonly storageKey: () => string,
    private readonly blobs: BlobStore,
    private readonly transport: QueueTransport,
  ) {
    this.refreshCounters()
  }

  actions(): QueueAction[] {
    const key = this.storageKey()
    if (!key) return []
    try {
      const raw = this.storage.get(key)
      const parsed = raw ? JSON.parse(raw) : []
      return Array.isArray(parsed) ? parsed : []
    } catch {
      return []
    }
  }

  private save(actions: QueueAction[]) {
    const key = this.storageKey()
    if (!key) return
    this.storage.set(key, JSON.stringify(actions))
    this.refreshCounters(actions)
  }

  refreshCounters(actions = this.actions()) {
    this.pending.value = actions.length
    this.pendingPhotos.value = actions.filter((a) => a.kind === 'photo').length
  }

  /** Заказы, у которых есть неотправленная отметка «Исполнил». */
  pendingExecutions(): Set<string> {
    return new Set(this.actions().filter((a) => a.kind === 'execute').map((a) => (a as ExecuteAction).orderId))
  }

  /** Снимки заказа, ждущие отправки. */
  pendingPhotosFor(orderId: string): PhotoAction[] {
    return this.actions().filter((a): a is PhotoAction => a.kind === 'photo' && a.orderId === orderId)
  }

  /**
   * Ставит снимок в очередь. Пересъёмка того же вида по тому же заказу заменяет
   * прежний неотправленный снимок: на сервер уходит последний.
   */
  async enqueuePhoto(meta: Omit<PhotoAction, 'kind' | 'id' | 'blobKey' | 'createdAt' | 'clientKey'>, bytes: Uint8Array): Promise<PhotoAction> {
    const id = newId()
    const action: PhotoAction = { ...meta, kind: 'photo', id, clientKey: id, blobKey: `photo-${id}`, createdAt: Date.now() }
    await this.blobs.put(action.blobKey, bytes)

    const replaced: PhotoAction[] = []
    const actions = this.actions().filter((a) => {
      const same = a.kind === 'photo' && a.orderId === meta.orderId && a.photoKind === meta.photoKind
      if (same) replaced.push(a as PhotoAction)
      return !same
    })
    actions.push(action)
    this.save(actions)
    await Promise.all(replaced.map((a) => this.blobs.remove(a.blobKey)))
    return action
  }

  /** Добавляет точки трека к последней неотправленной пачке. */
  enqueuePositions(points: Omit<PositionPoint, 'client_key' | 'source'>[]): void {
    if (points.length === 0) return
    const actions = this.actions()
    for (const p of points) {
      const point: PositionPoint = { ...p, source: 'LIVE', client_key: newId() }
      const last = actions[actions.length - 1]
      if (last && last.kind === 'positions' && last.points.length < POSITIONS_PER_ACTION) {
        last.points.push(point)
      } else {
        actions.push({ kind: 'positions', id: newId(), points: [point], createdAt: Date.now() })
      }
    }
    this.save(actions)
  }

  /** Ставит в очередь отметку «Исполнил»; вторая отметка того же заказа не нужна. */
  enqueueExecute(orderId: string, executedAtDevice: string): void {
    const actions = this.actions()
    if (actions.some((a) => a.kind === 'execute' && a.orderId === orderId)) return
    actions.push({ kind: 'execute', id: newId(), orderId, executedAtDevice, createdAt: Date.now() })
    this.save(actions)
    patchOfflineOrder(orderId, { status: 'EXECUTED' })
  }

  /** Отправляет очередь. Одновременно идёт только одна отправка. */
  flush(): Promise<void> {
    if (!this.flushing) {
      this.flushing = this.run().finally(() => {
        this.flushing = null
      })
    }
    return this.flushing
  }

  private async drop(action: QueueAction, err?: any) {
    this.save(this.actions().filter((a) => a.id !== action.id))
    if (action.kind === 'photo') await this.blobs.remove(action.blobKey)
    if (err) {
      this.failures.value = [...this.failures.value.slice(-9), { action, message: failureText(err), at: Date.now() }]
    }
  }

  private async run(): Promise<void> {
    for (const action of this.actions()) {
      try {
        if (action.kind === 'photo') {
          const bytes = await this.blobs.get(action.blobKey)
          if (!bytes) {
            await this.drop(action, new Error('файл снимка потерян на устройстве'))
            continue
          }
          await this.transport.uploadPhoto(action, bytes)
        } else if (action.kind === 'positions') {
          await this.transport.sendPositions(action.points)
        } else {
          // Отметка ждёт снимков своего заказа: без них сервер откажет.
          if (this.pendingPhotosFor(action.orderId).length > 0) continue
          await this.transport.execute(action.orderId, action.executedAtDevice)
        }
        await this.drop(action)
      } catch (err) {
        if (isPermanentFailure(err)) {
          await this.drop(action, err)
          continue
        }
        // Сети нет или сервер недоступен: остальное тоже не уйдёт, пробуем позже.
        return
      }
    }
  }
}

const apiTransport: QueueTransport = {
  async uploadPhoto(action, bytes) {
    const form = new FormData()
    form.append('file', new Blob([bytes as BlobPart], { type: 'image/jpeg' }), `${action.photoKind.toLowerCase()}.jpg`)
    form.append('kind', action.photoKind)
    form.append('camera', action.camera)
    form.append('client_key', action.clientKey)
    form.append('device_taken_at', action.takenAt)
    if (action.lat !== null && action.lon !== null) {
      form.append('device_lat', action.lat)
      form.append('device_lon', action.lon)
    }
    if (action.accuracy !== null) form.append('device_accuracy_m', String(action.accuracy))
    await api.post(`/executor/orders/${action.orderId}/photo-proof`, form)
  },
  async sendPositions(points) {
    await api.post('/executor/positions', { positions: points })
  },
  async execute(orderId, executedAtDevice) {
    await api.post(`/executor/orders/${orderId}/execute`, { executed_at_device: executedAtDevice })
  },
}

const browserStorage: KeyValue = {
  get(key) {
    try {
      return localStorage.getItem(key)
    } catch {
      return null
    }
  },
  set(key, value) {
    try {
      localStorage.setItem(key, value)
    } catch {
      // Переполнение: действие не сохранится между запусками, но уйдёт в этом.
    }
  },
}

function userQueueKey(): string {
  try {
    const user = localStorage.getItem('userID')
    return user ? `${OFFLINE_PREFIX}${user}:queue` : ''
  } catch {
    return ''
  }
}

let instance: ProofQueue | null = null

/** Очередь текущего пользователя. */
export function proofQueue(): ProofQueue {
  if (!instance) instance = new ProofQueue(browserStorage, userQueueKey, blobStore(), apiTransport)
  return instance
}

let started = false

/**
 * Запускает фоновую отправку: при появлении сети, при возврате приложения из
 * фона и раз в полминуты — на случай, если событие о сети не пришло.
 */
export function startProofQueue(): void {
  if (started) return
  started = true
  const tryFlush = () => {
    proofQueue().refreshCounters()
    if (online.value) void proofQueue().flush()
  }
  onOnline(tryFlush)
  if (typeof document !== 'undefined') {
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible') tryFlush()
    })
  }
  setInterval(tryFlush, 30_000)
  tryFlush()
}
