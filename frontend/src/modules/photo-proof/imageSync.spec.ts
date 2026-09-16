import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { webcrypto } from 'node:crypto'
import {
  mulberry32,
  markSeed,
  markPayload,
  assignments,
  sealMessage,
  sealOf,
  addSeal,
  stripSeal,
  exifSegment,
  withExif,
  embedMark,
  readMark,
  MARK_PATTERN,
  toRfc3339,
  type SyncInput,
} from './imageSync'

// jsdom подменяет crypto без subtle; настоящий Web Crypto берётся у Node.
if (!(globalThis as any).crypto?.subtle) {
  Object.defineProperty(globalThis, 'crypto', { value: webcrypto, configurable: true })
}

// Договор с сервером: тот же файл читает Go-тест (backend/photoproof/vectors_test.go).
const vectors = JSON.parse(
  readFileSync(resolve(__dirname, '../../../../backend/photoproof/testdata/proof_vectors.json'), 'utf8'),
)

function hexToBytes(hex: string): Uint8Array {
  return new Uint8Array(hex.match(/../g)!.map((b) => parseInt(b, 16)))
}

function bytesToHex(bytes: Uint8Array): string {
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}

function vectorInput(): SyncInput {
  const i = vectors.input
  return {
    orderId: i.order_id,
    symbolCode: i.symbol_code,
    symbolNumber: i.symbol_number,
    key: hexToBytes(i.key_hex),
    takenAt: new Date(i.device_taken_at),
    lat: i.device_lat,
    lon: i.device_lon,
  }
}

describe('векторы сервера', () => {
  it('зерно и генератор', async () => {
    const input = vectorInput()
    const seed = await markSeed(input.key)
    expect(seed).toBe(vectors.seed)
    const next = mulberry32(seed)
    expect(vectors.random_first.map(() => next())).toEqual(vectors.random_first)
  })

  it('нагрузка метки и назначение блоков', async () => {
    const input = vectorInput()
    expect((await markPayload(input)).toString(16).padStart(16, '0')).toBe(vectors.payload_hex)
    expect(await assignments(input.key, vectors.assignments_first.length)).toEqual(vectors.assignments_first)
  })

  it('узор поправки', () => {
    expect(MARK_PATTERN.map((v) => Math.round(v * 1e6) / 1e6)).toEqual(vectors.pattern)
  })

  it('сообщение и подпись', async () => {
    const input = vectorInput()
    const stripped = hexToBytes(vectors.input.stripped_hex)
    expect(await sealMessage(stripped, input)).toBe(vectors.seal_message)
    expect(bytesToHex(await sealOf(stripped, input))).toBe(vectors.seal_hex)
  })
})

// Минимальный «JPEG»: SOI, APP0, APP1 с EXIF, SOS, данные, EOI. Проверяется
// работа с сегментами, а не кодирование изображения.
function fakeJpeg(): Uint8Array {
  const app0 = [0xff, 0xe0, 0x00, 0x06, 0x4a, 0x46, 0x49, 0x46]
  const app1 = [0xff, 0xe1, 0x00, 0x08, 0x45, 0x78, 0x69, 0x66, 0x00, 0x00]
  const scan = [0xff, 0xda, 0x00, 0x04, 0x01, 0x02, 0x10, 0x20, 0x30, 0xff, 0xd9]
  return new Uint8Array([0xff, 0xd8, ...app0, ...app1, ...scan])
}

describe('сегменты файла', () => {
  it('подпись ставится после EXIF и снимается без следа', async () => {
    const input = vectorInput()
    const original = fakeJpeg()
    const sealed = await addSeal(original, input)
    expect(sealed.length).toBe(original.length + 4 + 36)
    expect(sealed[2 + 8 + 10]).toBe(0xff)
    expect(sealed[2 + 8 + 10 + 1]).toBe(0xeb)
    expect(Array.from(stripSeal(sealed))).toEqual(Array.from(original))
    // Повторная подпись заменяет прежнюю, а не добавляет вторую.
    expect((await addSeal(sealed, input)).length).toBe(sealed.length)
  })

  it('EXIF возвращается в перекодированный файл', () => {
    const exif = exifSegment(fakeJpeg())
    expect(exif).not.toBeNull()
    const encoded = new Uint8Array([0xff, 0xd8, 0xff, 0xe0, 0x00, 0x06, 1, 2, 3, 4, 0xff, 0xda, 0x00, 0x02, 0xff, 0xd9])
    const restored = withExif(encoded, exif)
    expect(exifSegment(restored)).not.toBeNull()
    expect(restored.length).toBe(encoded.length + exif!.length)
  })
})

function scene(width: number, height: number, seed: number) {
  const data = new Uint8ClampedArray(width * height * 4)
  const next = mulberry32(seed)
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      const base = 90 + 60 * Math.sin(x / 37) + 40 * Math.cos(y / 23)
      const noise = (next() % 16) - 8
      const p = (y * width + x) * 4
      data[p] = base + noise
      data[p + 1] = base * 0.9 + noise
      data[p + 2] = base * 0.7 + noise
      data[p + 3] = 255
    }
  }
  return { data, width, height }
}

describe('метка', () => {
  it('ставится и читается тем же ключом', async () => {
    const input = vectorInput()
    const px = scene(480, 320, 1)
    expect(await readMark(px, input)).toBe('NOT_FOUND')
    await embedMark(px, input)
    expect(await readMark(px, input)).toBe('FOUND')
    expect(await readMark(px, { ...input, symbolNumber: 4 })).toBe('MISMATCH')
    expect(await readMark(px, { ...input, key: new Uint8Array(32) })).toBe('NOT_FOUND')
  })
})

describe('время съёмки', () => {
  it('RFC 3339 с поясом и до секунды', () => {
    const text = toRfc3339(new Date(2026, 8, 16, 14, 30, 5, 700))
    expect(text).toMatch(/^2026-09-16T14:30:05[+-]\d\d:\d\d$/)
    expect(new Date(text).getSeconds()).toBe(5)
  })
})
