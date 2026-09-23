import { describe, it, expect, beforeEach } from 'vitest'
import { ProofQueue, isPermanentFailure, type QueueTransport, type KeyValue, type PhotoAction } from './queue'
import { MemoryBlobStore } from './blobStore'

function memoryStorage(): KeyValue {
  const items = new Map<string, string>()
  return { get: (k) => items.get(k) ?? null, set: (k, v) => void items.set(k, v) }
}

function httpError(status: number, message = 'отказ') {
  return Object.assign(new Error(message), { response: { status, data: message } })
}

class FakeTransport implements QueueTransport {
  calls: string[] = []
  photoFailure: any = null
  executeFailure: any = null
  offline = false

  async uploadPhoto(action: PhotoAction) {
    if (this.offline) throw new Error('Network Error')
    if (this.photoFailure) throw this.photoFailure
    this.calls.push(`photo:${action.orderId}:${action.photoKind}:${action.clientKey}`)
  }
  async sendPositions(points: any[]) {
    if (this.offline) throw new Error('Network Error')
    this.calls.push(`positions:${points.length}`)
  }
  async execute(orderId: string) {
    if (this.offline) throw new Error('Network Error')
    if (this.executeFailure) throw this.executeFailure
    this.calls.push(`execute:${orderId}`)
  }
  passports: { orderId: string; data: any; photo: Uint8Array; takenAt: string }[] = []
  async sendPassport(orderId: string, data: unknown, photo: Uint8Array, takenAt: string) {
    if (this.offline) throw new Error('Network Error')
    this.passports.push({ orderId, data, photo, takenAt })
    this.calls.push(`passport:${orderId}`)
  }
}

// Шифр для тестов: переворачивает байты и сдвигает их — достаточно, чтобы
// открытый текст не лежал в хранилище как есть.
const testSealer = {
  async seal(plain: Uint8Array) {
    return Uint8Array.from([...plain].reverse().map((b) => (b + 7) % 256))
  },
  async open(sealed: Uint8Array) {
    return Uint8Array.from([...sealed].map((b) => (b + 249) % 256).reverse())
  },
}

const photoMeta = (orderId: string, photoKind: 'AREA' | 'SELFIE' = 'AREA') => ({
  orderId,
  photoKind,
  camera: 'REAR' as const,
  takenAt: '2026-09-16T14:30:05+03:00',
  lat: '55.755800',
  lon: '37.617300',
  accuracy: 12,
})

let transport: FakeTransport
let blobs: MemoryBlobStore
let queue: ProofQueue

beforeEach(() => {
  localStorage.clear()
  transport = new FakeTransport()
  blobs = new MemoryBlobStore()
  queue = new ProofQueue(memoryStorage(), () => 'queue-key', blobs, transport, testSealer)
})

describe('passport in the queue', () => {
  const data = { series: '4510', number: '123456', issued_at: '2015-06-01' }
  const photo = new TextEncoder().encode('JPEG passport 123456')
  const takenAt = '2026-09-23T10:00:00Z'

  it('keeps the passport sealed on the device and removes it once sent', async () => {
    transport.offline = true
    await queue.enqueuePassport('order-7', data, photo, takenAt)
    await queue.flush()
    expect(queue.pendingPassportFor('order-7')).toBeTruthy()
    const stored = await Promise.all((await blobs.keys()).map((k) => blobs.get(k)))
    const plain = stored.map((b) => new TextDecoder().decode(b!)).join('|')
    expect(plain).not.toContain('123456')

    transport.offline = false
    await queue.flush()
    expect(transport.passports).toHaveLength(1)
    expect(transport.passports[0].orderId).toBe('order-7')
    expect(transport.passports[0].data).toEqual(data)
    expect([...transport.passports[0].photo]).toEqual([...photo])
    // Время съёмки доезжает вместе со снимком: без него сервер не проверит подпись.
    expect(transport.passports[0].takenAt).toBe(takenAt)
    expect(queue.pendingPassportFor('order-7')).toBeUndefined()
    expect(await blobs.keys()).toEqual([])
  })

  it('replaces an unsent passport of the same order', async () => {
    transport.offline = true
    await queue.enqueuePassport('order-7', data, photo, takenAt)
    await queue.enqueuePassport('order-7', { ...data, number: '654321' }, photo, takenAt)
    expect(queue.actions().filter((a) => a.kind === 'passport')).toHaveLength(1)
    expect(await blobs.keys()).toHaveLength(2)
    transport.offline = false
    await queue.flush()
    expect(transport.passports[0].data.number).toBe('654321')
  })

  it('refuses to keep a passport without a device cipher', async () => {
    const plainQueue = new ProofQueue(memoryStorage(), () => 'k', new MemoryBlobStore(), transport, null)
    expect(plainQueue.canQueuePassport()).toBe(false)
    await expect(plainQueue.enqueuePassport('order-1', data, photo, takenAt)).rejects.toThrow()
  })
})

describe('очередь снимков, трека и отметок', () => {
  it('в офлайне копит, при сети отправляет по порядку: снимки раньше отметки', async () => {
    transport.offline = true
    queue.enqueueExecute('order-1', '2026-09-16T14:35:00+03:00')
    await queue.enqueuePhoto(photoMeta('order-1'), new Uint8Array([1]))
    queue.enqueuePositions([{ lat: 55.75, lon: 37.61, device_at: '2026-09-16T14:29:00+03:00' }])
    await queue.flush()
    expect(queue.pending.value).toBe(3)
    expect(queue.pendingPhotos.value).toBe(1)
    expect(transport.calls).toEqual([])

    transport.offline = false
    await queue.flush()
    // Отметка стоит в очереди первой, но уходит после снимка своего заказа.
    expect(transport.calls.map((c) => c.split(':').slice(0, 2).join(':'))).toEqual(['photo:order-1', 'positions:1'])
    await queue.flush()
    expect(transport.calls[transport.calls.length - 1]).toBe('execute:order-1')
    expect(queue.pending.value).toBe(0)
    expect(await blobs.keys()).toEqual([])
  })

  it('пересъёмка заменяет неотправленный снимок того же вида', async () => {
    transport.offline = true
    await queue.enqueuePhoto(photoMeta('order-1'), new Uint8Array([1]))
    await queue.enqueuePhoto(photoMeta('order-1', 'SELFIE'), new Uint8Array([2]))
    const retake = await queue.enqueuePhoto(photoMeta('order-1'), new Uint8Array([3]))
    expect(queue.pendingPhotosFor('order-1').map((a) => a.photoKind).sort()).toEqual(['AREA', 'SELFIE'])
    expect(Array.from((await blobs.get(retake.blobKey)) || [])).toEqual([3])
    expect((await blobs.keys()).length).toBe(2)
  })

  it('отказ сервера убирает действие и сообщает о нём, временный сбой — нет', async () => {
    await queue.enqueuePhoto(photoMeta('order-1'), new Uint8Array([1]))
    transport.photoFailure = httpError(503)
    await queue.flush()
    expect(queue.pending.value).toBe(1)
    expect(queue.failures.value).toEqual([])

    transport.photoFailure = httpError(409, 'заказ уже отмечен исполненным')
    await queue.flush()
    expect(queue.pending.value).toBe(0)
    expect(queue.failures.value[0].message).toContain('отмечен')
  })

  it('вторая отметка того же заказа не ставится', () => {
    queue.enqueueExecute('order-1', 'a')
    queue.enqueueExecute('order-1', 'b')
    expect(queue.pending.value).toBe(1)
    expect([...queue.pendingExecutions()]).toEqual(['order-1'])
  })

  it('точки трека складываются в пачки', () => {
    const points = Array.from({ length: 501 }, (_, i) => ({ lat: 55, lon: 37, device_at: String(i) }))
    queue.enqueuePositions(points)
    const actions = queue.actions()
    expect(actions.length).toBe(2)
    expect((actions[0] as any).points.length).toBe(500)
    expect(new Set((actions[0] as any).points.map((p: any) => p.client_key)).size).toBe(500)
  })

  it('очередь переживает перезапуск', async () => {
    const storage = memoryStorage()
    const first = new ProofQueue(storage, () => 'k', blobs, transport)
    transport.offline = true
    await first.enqueuePhoto(photoMeta('order-9'), new Uint8Array([9]))
    const restarted = new ProofQueue(storage, () => 'k', blobs, transport)
    expect(restarted.pending.value).toBe(1)
    transport.offline = false
    await restarted.flush()
    expect(transport.calls[0]).toMatch(/^photo:order-9:AREA:/)
  })
})

describe('классификация ошибок', () => {
  it('временные и постоянные', () => {
    expect(isPermanentFailure(new Error('Network Error'))).toBe(false)
    expect(isPermanentFailure(httpError(500))).toBe(false)
    expect(isPermanentFailure(httpError(429))).toBe(false)
    expect(isPermanentFailure(httpError(422))).toBe(true)
    expect(isPermanentFailure(httpError(409))).toBe(true)
  })
})
