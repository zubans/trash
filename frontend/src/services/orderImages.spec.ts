import { describe, it, expect, beforeEach, vi } from 'vitest'

// Модуль ходит в сеть двумя путями: axios — за сообщениями чата, fetch — за
// самим файлом. Оба подменяются, чтобы тест проверял правила кэша, а не сеть.
const apiGet = vi.fn()
vi.mock('./api', () => ({
  default: { get: (...args: any[]) => apiGet(...args) },
  resolveFileUrl: (path: string) => `https://example.test${path}?token=t`,
}))

import {
  orderImageSrc,
  isImageCached,
  cacheOrderImage,
  rememberOrderImages,
  preloadOrderImages,
  releaseClosedOrderImages,
  clearOrderImages,
  messageImagePath,
  isOrderOpen,
} from './orderImages'

const revoked: string[] = []
let createdUrls = 0

beforeEach(() => {
  localStorage.clear()
  localStorage.setItem('userID', 'user-1')
  clearOrderImages()
  apiGet.mockReset()
  revoked.length = 0
  createdUrls = 0

  globalThis.URL.createObjectURL = vi.fn(() => `blob:${++createdUrls}`)
  globalThis.URL.revokeObjectURL = vi.fn((url: string) => { revoked.push(url) })
  globalThis.fetch = vi.fn(async () => ({
    ok: true,
    blob: async () => ({ size: 10 }),
  })) as any
})

const imageMessage = (path: string) => ({ id: path, file_url: path, file_type: 'image' })

describe('messageImagePath', () => {
  it('узнаёт вложение-изображение и пропускает всё прочее', () => {
    expect(messageImagePath(imageMessage('/uploads/chat/a.jpg'))).toBe('/uploads/chat/a.jpg')
    expect(messageImagePath({ text: 'просто текст' })).toBeNull()
    expect(messageImagePath({ file_url: '/files/act.pdf', file_type: 'document' })).toBeNull()
  })
})

describe('isOrderOpen', () => {
  it('закрытыми считает только завершённые и отменённые', () => {
    expect(isOrderOpen({ status: 'ASSIGNED' })).toBe(true)
    expect(isOrderOpen({ status: 'EXECUTED' })).toBe(true)
    expect(isOrderOpen({ status: 'COMPLETED' })).toBe(false)
    expect(isOrderOpen({ status: 'CANCELED' })).toBe(false)
  })
})

describe('кэш изображений заказа', () => {
  it('прогретая картинка подменяет сетевой URL', async () => {
    const path = '/uploads/chat/a.jpg'
    expect(orderImageSrc(path)).toBe('https://example.test/uploads/chat/a.jpg?token=t')

    await cacheOrderImage('order-1', path)

    expect(isImageCached(path)).toBe(true)
    expect(orderImageSrc(path)).toBe('blob:1')
  })

  it('один путь загружается один раз, даже если его просят одновременно', async () => {
    const path = '/uploads/chat/a.jpg'
    await Promise.all([
      cacheOrderImage('order-1', path),
      cacheOrderImage('order-1', path),
      cacheOrderImage('order-1', path),
    ])
    expect(globalThis.fetch).toHaveBeenCalledTimes(1)
  })

  it('неудачная загрузка ничего не ломает и не кэширует', async () => {
    globalThis.fetch = vi.fn(async () => { throw new Error('offline') }) as any
    const url = await cacheOrderImage('order-1', '/uploads/chat/a.jpg')
    expect(url).toBeUndefined()
    expect(isImageCached('/uploads/chat/a.jpg')).toBe(false)
  })

  // Главное правило: картинка живёт ровно столько, сколько открыт заказ.
  it('закрытие заказа освобождает его картинки', async () => {
    await cacheOrderImage('order-1', '/uploads/chat/a.jpg')
    await cacheOrderImage('order-2', '/uploads/chat/b.jpg')

    releaseClosedOrderImages([
      { id: 'order-1', status: 'COMPLETED' },
      { id: 'order-2', status: 'ASSIGNED' },
    ])

    expect(isImageCached('/uploads/chat/a.jpg')).toBe(false)
    expect(isImageCached('/uploads/chat/b.jpg')).toBe(true)
    // Object URL отзывается явно, иначе блоб живёт до перезагрузки страницы.
    expect(revoked).toEqual(['blob:1'])
  })

  it('заказ, пропавший из списка, тоже считается закрытым', async () => {
    await cacheOrderImage('order-1', '/uploads/chat/a.jpg')
    releaseClosedOrderImages([])
    expect(isImageCached('/uploads/chat/a.jpg')).toBe(false)
  })

  it('конец сессии освобождает всё', async () => {
    await cacheOrderImage('order-1', '/uploads/chat/a.jpg')
    clearOrderImages()
    expect(isImageCached('/uploads/chat/a.jpg')).toBe(false)
    expect(revoked).toEqual(['blob:1'])
  })
})

describe('preloadOrderImages', () => {
  it('читает вложения открытых заказов и прогревает их', async () => {
    apiGet.mockResolvedValue({ data: [imageMessage('/uploads/chat/a.jpg'), { text: 'привет' }] })

    await preloadOrderImages([
      { id: 'order-1', status: 'ASSIGNED' },
      { id: 'order-2', status: 'COMPLETED' },
    ])

    // Закрытый заказ не запрашивается вовсе.
    expect(apiGet).toHaveBeenCalledTimes(1)
    expect(apiGet).toHaveBeenCalledWith('/chats/order-1/messages')
    expect(isImageCached('/uploads/chat/a.jpg')).toBe(true)
  })

  it('не перечитывает сообщения уже разобранного заказа', async () => {
    apiGet.mockResolvedValue({ data: [imageMessage('/uploads/chat/a.jpg')] })
    const orders = [{ id: 'order-1', status: 'ASSIGNED' }]

    await preloadOrderImages(orders)
    await preloadOrderImages(orders)

    expect(apiGet).toHaveBeenCalledTimes(1)
  })

  // Сохранённый список путей — то, ради чего холодный старт не ждёт чата.
  it('после перезапуска греет картинки по сохранённому списку, без запроса чата', async () => {
    apiGet.mockResolvedValue({ data: [imageMessage('/uploads/chat/a.jpg')] })
    await preloadOrderImages([{ id: 'order-1', status: 'ASSIGNED' }])

    // Новый запуск приложения: блобы потеряны, localStorage — нет.
    clearOrderImages()
    apiGet.mockResolvedValue({ data: [] })

    await preloadOrderImages([{ id: 'order-1', status: 'ASSIGNED' }])

    expect(isImageCached('/uploads/chat/a.jpg')).toBe(true)
  })

  it('недоступный чат не отменяет остальные заказы', async () => {
    apiGet.mockImplementation(async (url: string) => {
      if (url.includes('order-1')) throw new Error('403')
      return { data: [imageMessage('/uploads/chat/b.jpg')] }
    })

    await preloadOrderImages([
      { id: 'order-1', status: 'ASSIGNED' },
      { id: 'order-2', status: 'ASSIGNED' },
    ])

    expect(isImageCached('/uploads/chat/b.jpg')).toBe(true)
  })
})

describe('rememberOrderImages', () => {
  it('кладёт в кэш вложения из уже показанных сообщений', async () => {
    rememberOrderImages('order-1', [imageMessage('/uploads/chat/a.jpg'), { text: 'привет' }])
    await vi.waitFor(() => expect(isImageCached('/uploads/chat/a.jpg')).toBe(true))
    // Сообщения уже на руках — за ними в сеть не ходят.
    expect(apiGet).not.toHaveBeenCalled()
  })
})
