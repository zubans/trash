import { ref } from 'vue'
import api, { resolveFileUrl } from './api'
import { readCache, writeCache, dropCache, onCacheCleared } from './cache'

/**
 * Изображения активного заказа: предзагрузка и кэш на время его жизни.
 *
 * Фотографии в чате заказа — то, ради чего чат чаще всего и открывают, но
 * загружаться они начинали только в момент открытия: пользователь видел пустые
 * прямоугольники ровно тогда, когда смотрел на них. Здесь они прогреваются
 * заранее, пока экран занят своими списками.
 *
 * Кэш держится ровно столько, сколько открыт заказ. Это не срок «на всякий
 * случай», а граница смысла: пока заказ в работе, к его фотографиям
 * возвращаются, а закрытый заказ уходит в историю, и держать его картинки в
 * памяти незачем. Object URL нужно ещё и освобождать явно — иначе блобы живут
 * до перезагрузки страницы, сколько бы заказов ни закрылось.
 */

// Статусы, после которых заказ считается закрытым. Всё остальное — открытый
// заказ, к которому пользователь ещё может вернуться.
const CLOSED_STATUSES = ['COMPLETED', 'CANCELED']

// path -> object URL. Реактивный, потому что шаблон читает его на каждой
// отрисовке: прогревшаяся картинка должна подменить сетевой URL сама.
const objectUrls = ref<Record<string, string>>({})

// path -> id заказа, которому картинка принадлежит. По нему решается, когда её
// освобождать.
const owners = new Map<string, string>()

// Одна загрузка на путь: список сообщений и предзагрузка легко просят одно и то
// же изображение одновременно.
const inFlight = new Map<string, Promise<string | undefined>>()

// Заказы, чьи вложения уже разобраны в этом сеансе. Повторно перечитывать
// сообщения ради предзагрузки не нужно: открытый чат кладёт новые вложения в
// кэш сам (`rememberOrderImages`).
const scanned = new Set<string>()

// Список путей к вложениям по заказам, пережившийся между запусками. Он избавляет
// холодный старт от ожидания: картинки открытых заказов начинают грузиться
// сразу, не дожидаясь, пока по каждому заказу приедут сообщения чата.
const MANIFEST_KEY = 'orders:images'

type ImageManifest = Record<string, string[]>

function readManifest(): ImageManifest {
  return readCache<ImageManifest>(MANIFEST_KEY)?.value || {}
}

export function isOrderOpen(order: any): boolean {
  return !!order && !CLOSED_STATUSES.includes(order.status)
}

/** Путь к вложению сообщения, если это изображение. */
export function messageImagePath(msg: any): string | null {
  const path = typeof msg === 'string' ? msg : msg?.file_url || msg?.content
  if (!path || typeof path !== 'string') return null
  if (msg?.file_type && msg.file_type !== 'image' && !path.startsWith('/uploads/')) return null
  const lower = path.toLowerCase()
  const looksLikeImage =
    lower.startsWith('/uploads/') ||
    ['.jpg', '.jpeg', '.png', '.webp', '.gif'].some((ext) => lower.endsWith(ext))
  return looksLikeImage ? path : null
}

/**
 * Что показать вместо картинки прямо сейчас: прогретый блоб, если он есть,
 * иначе обычный URL с токеном. Вызывается из шаблона, поэтому читает
 * реактивную карту — подмена происходит сама, как только блоб готов.
 */
export function orderImageSrc(pathOrMessage: any): string {
  const path = typeof pathOrMessage === 'string' ? pathOrMessage : messageImagePath(pathOrMessage)
  if (!path) return ''
  return objectUrls.value[path] || resolveFileUrl(path)
}

/** Уже прогрето ли изображение. */
export function isImageCached(path: string): boolean {
  return !!objectUrls.value[path]
}

// Картинки, которые браузер уже отрисовал. Нужны заглушке под фотографией: она
// гаснет по факту показа, а не по факту загрузки блоба — изображение, взятое
// напрямую из сети в обход предзагрузки, тоже должно её погасить.
const rendered = ref<Record<string, true>>({})

/** Отмечает, что <img> с этим путём отрисовался (обработчик `@load`). */
export function markImageRendered(pathOrMessage: any): void {
  const path = typeof pathOrMessage === 'string' ? pathOrMessage : messageImagePath(pathOrMessage)
  if (!path || rendered.value[path]) return
  rendered.value = { ...rendered.value, [path]: true }
}

/** Показывать ли заглушку вместо ещё не появившейся фотографии. */
export function isImagePending(pathOrMessage: any): boolean {
  const path = typeof pathOrMessage === 'string' ? pathOrMessage : messageImagePath(pathOrMessage)
  if (!path) return false
  return !rendered.value[path]
}

/**
 * Кладёт изображение в кэш заказа. Возвращает object URL или undefined, если
 * загрузить не удалось: предзагрузка — удобство, и её провал не должен ничего
 * ломать, обычный `<img>` сходит за картинкой сам.
 */
export function cacheOrderImage(orderId: string, path: string): Promise<string | undefined> {
  if (!path) return Promise.resolve(undefined)
  owners.set(path, orderId)
  const existing = objectUrls.value[path]
  if (existing) return Promise.resolve(existing)

  const running = inFlight.get(path)
  if (running) return running

  const request = (async () => {
    try {
      const res = await fetch(resolveFileUrl(path))
      if (!res.ok) return undefined
      const blob = await res.blob()
      if (!blob.size) return undefined
      const url = URL.createObjectURL(blob)
      objectUrls.value = { ...objectUrls.value, [path]: url }
      return url
    } catch (err) {
      console.warn('[orderImages] не удалось предзагрузить', path, err)
      return undefined
    } finally {
      inFlight.delete(path)
    }
  })()

  inFlight.set(path, request)
  return request
}

/**
 * Запоминает вложения из уже полученных сообщений чата. Зовётся, когда чат
 * открыли или когда сообщение с фотографией пришло по сокету: то, что заказ
 * показал хоть раз, остаётся под рукой до его закрытия.
 */
export function rememberOrderImages(orderId: string, messages: any[]): void {
  const paths = collectImagePaths(messages)
  if (!paths.length) return

  const manifest = readManifest()
  const known = new Set(manifest[orderId] || [])
  for (const path of paths) {
    known.add(path)
    void cacheOrderImage(orderId, path)
  }
  manifest[orderId] = [...known]
  writeCache(MANIFEST_KEY, manifest)
}

function collectImagePaths(messages: any[]): string[] {
  const paths: string[] = []
  for (const msg of messages || []) {
    const path = messageImagePath(msg)
    if (path) paths.push(path)
  }
  return paths
}

async function fetchOrderImagePaths(orderId: string): Promise<string[]> {
  const res = await api.get(`/chats/${orderId}/messages`)
  return collectImagePaths(res.data || [])
}

/**
 * Предзагрузка изображений открытых заказов.
 *
 * Порядок внутри намеренный: сначала пути, известные с прошлого запуска, — они
 * дают картинки без единого лишнего запроса, — и только потом сообщения чата,
 * из которых берётся актуальный список. Заказы обходятся по очереди, а не
 * разом: их немного (лимит активных заказов), зато предзагрузка не отбирает
 * соединение у того, что пользователь видит на экране.
 */
export async function preloadOrderImages(orders: any[]): Promise<void> {
  const open = (orders || []).filter(isOrderOpen)
  releaseClosedOrderImages(orders)
  if (!open.length) return

  const manifest = readManifest()
  const warming: Promise<unknown>[] = []

  for (const order of open) {
    for (const path of manifest[order.id] || []) {
      warming.push(cacheOrderImage(order.id, path))
    }
  }

  for (const order of open) {
    if (scanned.has(order.id)) continue
    scanned.add(order.id)
    try {
      const paths = await fetchOrderImagePaths(order.id)
      manifest[order.id] = paths
      for (const path of paths) {
        warming.push(cacheOrderImage(order.id, path))
      }
    } catch (err) {
      // Заказ без доступного чата — не повод бросать остальные.
      scanned.delete(order.id)
      console.warn('[orderImages] не удалось прочитать вложения заказа', order.id, err)
    }
  }

  writeCache(MANIFEST_KEY, manifest)

  // Ступень очереди приоритетов считается пройденной, когда картинки
  // действительно прогреты, а не когда за ними ушли запросы: иначе
  // «предзагрузка» закончилась бы раньше, чем началась настоящая загрузка.
  await Promise.allSettled(warming)
}

/**
 * Освобождает всё, что принадлежит закрытым заказам. Список приходит целиком —
 * заказ, пропавший из него, тоже считается закрытым (его больше не показывают,
 * значит и картинки его не нужны).
 */
export function releaseClosedOrderImages(orders: any[]): void {
  const openIds = new Set((orders || []).filter(isOrderOpen).map((o: any) => o.id))

  for (const [path, orderId] of [...owners]) {
    if (openIds.has(orderId)) continue
    const url = objectUrls.value[path]
    if (url) {
      URL.revokeObjectURL(url)
      const next = { ...objectUrls.value }
      delete next[path]
      objectUrls.value = next
    }
    owners.delete(path)
  }

  for (const orderId of [...scanned]) {
    if (!openIds.has(orderId)) scanned.delete(orderId)
  }

  const manifest = readManifest()
  let changed = false
  for (const orderId of Object.keys(manifest)) {
    if (!openIds.has(orderId)) {
      delete manifest[orderId]
      changed = true
    }
  }
  if (changed) {
    if (Object.keys(manifest).length) {
      writeCache(MANIFEST_KEY, manifest)
    } else {
      dropCache(MANIFEST_KEY)
    }
  }
}

/**
 * Полная очистка — конец сессии. Подключена к очистке кэша, поэтому выход из
 * аккаунта освобождает и блобы: без явного отзыва object URL они живут до
 * перезагрузки страницы, а в мобильном WebView она может не случиться никогда.
 */
export function clearOrderImages(): void {
  for (const url of Object.values(objectUrls.value)) {
    URL.revokeObjectURL(url)
  }
  objectUrls.value = {}
  rendered.value = {}
  owners.clear()
  inFlight.clear()
  scanned.clear()
}

onCacheCleared(clearOrderImages)
