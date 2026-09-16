/**
 * Подготовка снимка к отправке: служебные данные, которые проверяет сервер
 * (backend/photoproof/check.go). Обе стороны обязаны считать одинаково до бита —
 * это закреплено векторами backend/photoproof/testdata/proof_vectors.json,
 * которые читает и тест этого файла.
 */

export interface SyncInput {
  orderId: string
  symbolCode: string
  symbolNumber: number
  /** Служебный ключ заказа (32 байта). */
  key: Uint8Array
  /** Время съёмки по часам телефона, с точностью до секунды. */
  takenAt: Date
  lat: number | null
  lon: number | null
}

export interface PixelBuffer {
  data: Uint8ClampedArray
  width: number
  height: number
}

// --- Криптография -----------------------------------------------------------

function subtle(): SubtleCrypto {
  const c: any = (globalThis as any).crypto
  if (c?.subtle) return c.subtle
  throw new Error('Web Crypto недоступен')
}

async function sha256(bytes: Uint8Array): Promise<Uint8Array> {
  return new Uint8Array(await subtle().digest('SHA-256', bytes as BufferSource))
}

function hex(bytes: Uint8Array): string {
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}

const encoder = new TextEncoder()

// --- Подпись ----------------------------------------------------------------

const SEAL_MARKER = 0xeb
const SEAL_MAGIC = 'HLS1'
const SEAL_LEN = SEAL_MAGIC.length + 32

interface Segment {
  marker: number
  start: number
  end: number
}

/** Сегменты заголовка JPEG до начала данных изображения. */
export function headerSegments(data: Uint8Array): Segment[] {
  const out: Segment[] = []
  let i = 2
  while (i + 4 <= data.length && data[i] === 0xff) {
    const marker = data[i + 1]
    if (marker === 0xda || marker === 0xd9) break
    const length = (data[i + 2] << 8) | data[i + 3]
    const end = i + 2 + length
    if (length < 2 || end > data.length) break
    out.push({ marker, start: i, end })
    i = end
  }
  return out
}

function concat(...parts: Uint8Array[]): Uint8Array {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0))
  let offset = 0
  for (const p of parts) {
    out.set(p, offset)
    offset += p.length
  }
  return out
}

/** Файл без сегмента подписи, если он есть. */
export function stripSeal(data: Uint8Array): Uint8Array {
  for (const s of headerSegments(data)) {
    const payload = data.subarray(s.start + 4, s.end)
    if (
      s.marker === SEAL_MARKER &&
      payload.length === SEAL_LEN &&
      String.fromCharCode(...payload.subarray(0, SEAL_MAGIC.length)) === SEAL_MAGIC
    ) {
      return concat(data.subarray(0, s.start), data.subarray(s.end))
    }
  }
  return data
}

/** Координата так, как её подписывает и отправляет клиент: шесть знаков. */
export function coordText(v: number | null): string {
  return v === null || v === undefined ? '' : v.toFixed(6)
}

export async function sealMessage(stripped: Uint8Array, input: SyncInput): Promise<string> {
  return [
    hex(await sha256(stripped)),
    input.orderId,
    input.symbolCode,
    String(Math.floor(input.takenAt.getTime() / 1000)),
    coordText(input.lat),
    coordText(input.lon),
  ].join('\n')
}

export async function sealOf(stripped: Uint8Array, input: SyncInput): Promise<Uint8Array> {
  const key = await subtle().importKey('raw', input.key as BufferSource, { name: 'HMAC', hash: 'SHA-256' }, false, ['sign'])
  const message = encoder.encode(await sealMessage(stripped, input))
  return new Uint8Array(await subtle().sign('HMAC', key, message))
}

/** Вставляет подпись после последнего сегмента APP0/APP1. */
export async function addSeal(data: Uint8Array, input: SyncInput): Promise<Uint8Array> {
  const stripped = stripSeal(data)
  let insertAt = 2
  for (const s of headerSegments(stripped)) {
    if (s.marker === 0xe0 || s.marker === 0xe1) insertAt = s.end
  }
  const payload = concat(encoder.encode(SEAL_MAGIC), await sealOf(stripped, input))
  const length = payload.length + 2
  const segment = concat(new Uint8Array([0xff, SEAL_MARKER, length >> 8, length & 0xff]), payload)
  return concat(stripped.subarray(0, insertAt), segment, stripped.subarray(insertAt))
}

/** Сегмент APP1 (EXIF) исходного снимка — его возвращают после перекодирования. */
export function exifSegment(data: Uint8Array): Uint8Array | null {
  for (const s of headerSegments(data)) {
    if (s.marker === 0xe1 && String.fromCharCode(...data.subarray(s.start + 4, s.start + 8)) === 'Exif') {
      return data.subarray(s.start, s.end)
    }
  }
  return null
}

/** Возвращает EXIF в перекодированный файл: после APP0, иначе сразу после SOI. */
export function withExif(encoded: Uint8Array, exif: Uint8Array | null): Uint8Array {
  if (!exif) return encoded
  let insertAt = 2
  for (const s of headerSegments(encoded)) {
    if (s.marker === 0xe1) return encoded // EXIF уже есть
    if (s.marker === 0xe0) insertAt = s.end
  }
  return concat(encoded.subarray(0, insertAt), exif, encoded.subarray(insertAt))
}

// --- Метка ------------------------------------------------------------------

const MARK_BITS = 64
const MARK_STRENGTH = 12
const MARK_THRESHOLD = 0.3
const MIN_BLOCKS_PER_BIT = 8

export function mulberry32(seed: number): () => number {
  let a = seed >>> 0
  return () => {
    a = (a + 0x6d2b79f5) >>> 0
    let t = Math.imul(a ^ (a >>> 15), a | 1)
    t = (t + Math.imul(t ^ (t >>> 7), t | 61)) ^ t
    return (t ^ (t >>> 14)) >>> 0
  }
}

export async function markSeed(key: Uint8Array): Promise<number> {
  const sum = await sha256(concat(key, encoder.encode('mark')))
  return ((sum[0] << 24) | (sum[1] << 16) | (sum[2] << 8) | sum[3]) >>> 0
}

export async function markPayload(input: SyncInput): Promise<bigint> {
  const orderHash = await sha256(encoder.encode(input.orderId))
  const hash32 = BigInt(((orderHash[0] << 24) | (orderHash[1] << 16) | (orderHash[2] << 8) | orderHash[3]) >>> 0)
  const minutes = BigInt(Math.floor(Math.floor(input.takenAt.getTime() / 1000) / 60)) & 0xffffffn
  return (BigInt(input.symbolNumber & 0xff) << 56n) | (hash32 << 24n) | minutes
}

function payloadBit(payload: bigint, i: number): number {
  return Number((payload >> BigInt(63 - i)) & 1n)
}

export interface Assignment {
  bit: number
  chip: number
}

export async function assignments(key: Uint8Array, blocks: number): Promise<Assignment[]> {
  const next = mulberry32(await markSeed(key))
  const out: Assignment[] = new Array(blocks)
  for (let b = 0; b < blocks; b++) {
    out[b] = { bit: next() % MARK_BITS, chip: next() & 1 }
  }
  return out
}

function basis(u: number, v: number, x: number, y: number): number {
  const a = (k: number) => (k === 0 ? Math.sqrt(1 / 8) : 0.5)
  return a(u) * a(v) * Math.cos(((2 * x + 1) * u * Math.PI) / 16) * Math.cos(((2 * y + 1) * v * Math.PI) / 16)
}

const BASIS12: number[] = []
const BASIS21: number[] = []
for (let y = 0; y < 8; y++) {
  for (let x = 0; x < 8; x++) {
    BASIS12.push(basis(1, 2, x, y))
    BASIS21.push(basis(2, 1, x, y))
  }
}

/** Добавка к яркости блока на единицу поправки, по строкам. */
export const MARK_PATTERN: number[] = BASIS12.map((v, i) => v - BASIS21[i])

function luminanceAt(px: PixelBuffer, x: number, y: number): number {
  const p = (y * px.width + x) * 4
  return 0.299 * px.data[p] + 0.587 * px.data[p + 1] + 0.114 * px.data[p + 2]
}

function blockDiff(px: PixelBuffer, bx: number, by: number): number {
  let c12 = 0
  let c21 = 0
  for (let y = 0; y < 8; y++) {
    for (let x = 0; x < 8; x++) {
      const f = luminanceAt(px, bx * 8 + x, by * 8 + y)
      c12 += f * BASIS12[y * 8 + x]
      c21 += f * BASIS21[y * 8 + x]
    }
  }
  return c12 - c21
}

/** Ставит метку в пиксели: меняется только яркость, одинаково в R, G и B. */
export async function embedMark(px: PixelBuffer, input: SyncInput): Promise<void> {
  const w = Math.floor(px.width / 8)
  const h = Math.floor(px.height / 8)
  const payload = await markPayload(input)
  const assigned = await assignments(input.key, w * h)
  for (let b = 0; b < assigned.length; b++) {
    const bx = b % w
    const by = Math.floor(b / w)
    const want = payloadBit(payload, assigned[b].bit) ^ assigned[b].chip
    const d = blockDiff(px, bx, by)
    let delta = 0
    if (want === 1 && d < MARK_STRENGTH) delta = (MARK_STRENGTH - d) / 2
    else if (want === 0 && d > -MARK_STRENGTH) delta = (-MARK_STRENGTH - d) / 2
    if (delta === 0) continue
    for (let y = 0; y < 8; y++) {
      for (let x = 0; x < 8; x++) {
        const change = delta * MARK_PATTERN[y * 8 + x]
        const p = ((by * 8 + y) * px.width + bx * 8 + x) * 4
        for (let c = 0; c < 3; c++) {
          px.data[p + c] = Math.round(Math.min(255, Math.max(0, px.data[p + c] + change)))
        }
      }
    }
  }
}

/** Извлечение метки — как на сервере. В приложении не нужно; держится для тестов. */
export async function readMark(px: PixelBuffer, input: SyncInput): Promise<'FOUND' | 'NOT_FOUND' | 'MISMATCH'> {
  const w = Math.floor(px.width / 8)
  const h = Math.floor(px.height / 8)
  if (w * h < MARK_BITS * MIN_BLOCKS_PER_BIT) return 'NOT_FOUND'
  const votes = new Array(MARK_BITS).fill(0)
  const counts = new Array(MARK_BITS).fill(0)
  const assigned = await assignments(input.key, w * h)
  for (let b = 0; b < assigned.length; b++) {
    const observed = blockDiff(px, b % w, Math.floor(b / w)) > 0 ? 1 : 0
    votes[assigned[b].bit] += (observed ^ assigned[b].chip) === 1 ? 1 : -1
    counts[assigned[b].bit]++
  }
  let strength = 0
  let decoded = 0n
  for (let i = 0; i < MARK_BITS; i++) {
    if (counts[i] === 0) return 'NOT_FOUND'
    strength += Math.abs(votes[i]) / counts[i]
    if (votes[i] > 0) decoded |= 1n << BigInt(63 - i)
  }
  if (strength / MARK_BITS < MARK_THRESHOLD) return 'NOT_FOUND'
  return decoded === (await markPayload(input)) ? 'FOUND' : 'MISMATCH'
}

// --- Снимок целиком ---------------------------------------------------------

/**
 * Готовит снимок камеры к отправке: метка в пикселях, перекодирование,
 * возврат исходного EXIF, подпись. Работает в браузере и WebView.
 *
 * Ориентация из EXIF к пикселям не применяется: сервер декодирует файл как
 * есть, а тег ориентации возвращается вместе с EXIF, поэтому просмотрщик
 * повернёт снимок так же, как исходный.
 */
export async function prepareProofPhoto(original: Uint8Array, input: SyncInput): Promise<Uint8Array> {
  const bitmap = await createImageBitmap(new Blob([original as BlobPart], { type: 'image/jpeg' }), {
    imageOrientation: 'none',
  } as ImageBitmapOptions)
  const canvas = document.createElement('canvas')
  canvas.width = bitmap.width
  canvas.height = bitmap.height
  const ctx = canvas.getContext('2d', { willReadFrequently: true })
  if (!ctx) throw new Error('canvas недоступен')
  ctx.drawImage(bitmap, 0, 0)
  bitmap.close?.()

  const image = ctx.getImageData(0, 0, canvas.width, canvas.height)
  await embedMark(image, input)
  ctx.putImageData(image, 0, 0)

  const blob: Blob = await new Promise((resolve, reject) =>
    canvas.toBlob((b) => (b ? resolve(b) : reject(new Error('не удалось закодировать снимок'))), 'image/jpeg', 0.92),
  )
  const encoded = new Uint8Array(await blob.arrayBuffer())
  return addSeal(withExif(encoded, exifSegment(original)), input)
}

/** Время съёмки с поясом телефона, с точностью до секунды (RFC 3339). */
export function toRfc3339(date: Date): string {
  const pad = (n: number) => String(Math.abs(n)).padStart(2, '0')
  const offset = -date.getTimezoneOffset()
  const sign = offset >= 0 ? '+' : '-'
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}` +
    `T${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}` +
    `${sign}${pad(Math.trunc(offset / 60))}:${pad(offset % 60)}`
  )
}

export function base64ToBytes(value: string): Uint8Array {
  const binary = atob(value)
  const out = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i)
  return out
}
