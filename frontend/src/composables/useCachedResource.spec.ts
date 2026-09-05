import { describe, it, expect, beforeEach, vi } from 'vitest'
import { nextTick } from 'vue'
import { useCachedResource } from './useCachedResource'
import { readCache, writeCache, clearCachedData } from '../services/cache'

// Кэш привязан к вошедшему пользователю: без него записи не читаются и не
// пишутся, поэтому каждый тест начинается с известного «пользователя».
beforeEach(() => {
  localStorage.clear()
  localStorage.setItem('userID', 'user-1')
})

describe('cache', () => {
  it('возвращает записанное значение', () => {
    writeCache('orders', [{ id: 'a' }])
    expect(readCache<any[]>('orders')?.value).toEqual([{ id: 'a' }])
  })

  it('не отдаёт просроченную запись', () => {
    writeCache('orders', [{ id: 'a' }])
    expect(readCache('orders', 0)).toBeNull()
  })

  it('не отдаёт записи другого пользователя', () => {
    writeCache('orders', [{ id: 'a' }])
    localStorage.setItem('userID', 'user-2')
    expect(readCache('orders')).toBeNull()
  })

  it('ничего не пишет без вошедшего пользователя', () => {
    localStorage.removeItem('userID')
    writeCache('orders', [{ id: 'a' }])
    localStorage.setItem('userID', 'user-1')
    expect(readCache('orders')).toBeNull()
  })

  it('очистка стирает записи всех пользователей', () => {
    writeCache('orders', [{ id: 'a' }])
    clearCachedData()
    expect(readCache('orders')).toBeNull()
  })
})

describe('useCachedResource', () => {
  it('без кэша показывает загрузку, затем данные из сети', async () => {
    const resource = useCachedResource<string[]>({
      key: 'k',
      initial: [],
      fetcher: async () => ['fresh'],
    })

    expect(resource.loading.value).toBe(true)
    await resource.load()

    expect(resource.loading.value).toBe(false)
    expect(resource.data.value).toEqual(['fresh'])
    expect(resource.fromCache.value).toBe(false)
    // Загруженное сразу становится кэшем для следующего открытия экрана.
    expect(readCache<string[]>('k')?.value).toEqual(['fresh'])
  })

  it('сначала показывает кэш, потом заменяет его сетевыми данными', async () => {
    writeCache('k', ['cached'])
    let resolveFetch: (v: string[]) => void = () => {}
    const resource = useCachedResource<string[]>({
      key: 'k',
      initial: [],
      fetcher: () => new Promise<string[]>((resolve) => { resolveFetch = resolve }),
    })

    const done = resource.load()
    // Кэш поднимается синхронно: прелоадера в этом кадре уже нет.
    expect(resource.loading.value).toBe(false)
    expect(resource.data.value).toEqual(['cached'])
    expect(resource.fromCache.value).toBe(true)
    expect(resource.refreshing.value).toBe(true)

    resolveFetch(['fresh'])
    await done
    await nextTick()

    expect(resource.data.value).toEqual(['fresh'])
    expect(resource.fromCache.value).toBe(false)
    expect(resource.refreshing.value).toBe(false)
  })

  it('оставляет показанные данные, когда обновление сорвалось', async () => {
    writeCache('k', ['cached'])
    const resource = useCachedResource<string[]>({
      key: 'k',
      initial: [],
      fetcher: async () => { throw new Error('network down') },
    })

    await resource.load()

    expect(resource.data.value).toEqual(['cached'])
    expect(resource.loading.value).toBe(false)
    expect(resource.error.value).toBeInstanceOf(Error)
  })

  it('не запускает второй запрос, пока идёт первый', async () => {
    const fetcher = vi.fn(async () => ['x'])
    const resource = useCachedResource<string[]>({ key: 'k', initial: [], fetcher })

    await Promise.all([resource.refresh(), resource.refresh(), resource.refresh()])

    expect(fetcher).toHaveBeenCalledTimes(1)
  })

  it('reload не присоединяется к запросу, отправленному до действия', async () => {
    const answers = [['before'], ['after']]
    let pending: (() => void)[] = []
    const fetcher = vi.fn(
      () =>
        new Promise<string[]>((resolve) => {
          pending.push(() => resolve(answers.shift() as string[]))
        }),
    )
    const resource = useCachedResource<string[]>({ key: 'k', initial: [], fetcher })

    const polling = resource.refresh()
    // Действие пользователя происходит, пока опрос ещё в пути.
    const afterAction = resource.reload()

    pending.shift()!()
    await polling
    pending.shift()!()
    await afterAction

    expect(fetcher).toHaveBeenCalledTimes(2)
    expect(resource.data.value).toEqual(['after'])
  })

  it('отвергнутое acceptCached значение не показывается и стирается', async () => {
    writeCache('k', { status: 'ACTIVE', stale: true })
    const resource = useCachedResource<any>({
      key: 'k',
      initial: null,
      acceptCached: (value) => !value?.stale,
      fetcher: async () => ({ status: 'ACTIVE', stale: false }),
    })

    const done = resource.load()
    // Кэш не принят — экран остаётся на прелоадере до ответа сети.
    expect(resource.loading.value).toBe(true)
    expect(resource.data.value).toBeNull()

    await done
    expect(resource.data.value).toEqual({ status: 'ACTIVE', stale: false })
  })
})
