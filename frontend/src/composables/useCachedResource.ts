import { ref, type Ref } from 'vue'
import { readCache, writeCache, dropCache } from '../services/cache'

/**
 * Ресурс экрана, который сначала показывают из кэша, а потом догружают.
 *
 * Порядок один и тот же везде: синхронно поднять последнее известное значение
 * из хранилища, отрисовать его и только затем сходить в сеть. Пользователь,
 * вернувшийся на экран, видит свои данные мгновенно, а обновление приезжает
 * тихо, не сбрасывая экран в состояние загрузки.
 *
 * Три разных состояния специально не сведены в один флаг:
 * - `loading` — показывать нечего вообще, здесь место прелоадеру;
 * - `refreshing` — на экране есть данные, поверх них идёт догрузка;
 * - `fromCache` — то, что сейчас на экране, ещё не подтверждено сетью.
 */
export interface CachedResourceOptions<T> {
  /** Ключ хранения. Уникален в пределах пользователя. */
  key: string
  /** Как получить свежее значение. Ошибка отдаётся наружу через `error`. */
  fetcher: () => Promise<T>
  /** Значение до первой загрузки — пустой список, null и т. п. */
  initial: T
  /** Срок годности записи; по умолчанию — умолчание кэша. */
  ttlMs?: number
  /**
   * Проверка кэшированного значения перед показом. Возврат false означает
   * «данные больше не имеют смысла» — например, смена, чьё время уже вышло, —
   * и экран начинает с прелоадера, а не с заведомо неверного состояния.
   */
  acceptCached?: (value: T) => boolean
  /** Вызывается после каждой смены значения — из кэша или из сети. */
  onData?: (value: T, source: 'cache' | 'network') => void
}

export interface CachedResource<T> {
  data: Ref<T>
  loading: Ref<boolean>
  refreshing: Ref<boolean>
  fromCache: Ref<boolean>
  /** Когда значение на экране было получено (Date.now()), null — ещё никогда. */
  updatedAt: Ref<number | null>
  error: Ref<unknown>
  /** Кэш сразу, сеть следом. Точка входа при открытии экрана. */
  load: () => Promise<void>
  /**
   * Только кэш, синхронно. Нужен экрану, который распределяет сетевые запросы
   * по приоритетам: показать всё, что уже известно, надо сразу и всем, а
   * очередь выстраивают только походы в сеть. Возвращает, нашлось ли значение.
   */
  hydrate: () => boolean
  /**
   * Только сеть. Запрос, уже находящийся в пути, переиспользуется — так опрос
   * по таймеру, возврат из фона и обновление экрана не превращаются в очередь
   * одинаковых запросов.
   */
  refresh: () => Promise<void>
  /**
   * Перезагрузка после действия пользователя. От `refresh` отличается тем, что
   * не присоединяется к запросу, отправленному ДО действия: такой ответ о
   * действии не знает и вернул бы состояние до него. Ждёт текущий запрос и
   * спрашивает сервер заново.
   */
  reload: () => Promise<void>
  /** Записать значение, полученное в обход fetcher (например, ответом действия). */
  set: (value: T) => void
  /** Забыть сохранённое значение, оставив то, что уже на экране. */
  invalidate: () => void
}

export function useCachedResource<T>(options: CachedResourceOptions<T>): CachedResource<T> {
  const data = ref(options.initial) as Ref<T>
  const loading = ref(true)
  const refreshing = ref(false)
  const fromCache = ref(false)
  const updatedAt = ref<number | null>(null)
  const error = ref<unknown>(null)

  // Один запрос за раз. Опрос по таймеру, возврат из фона и обновление после
  // действия легко приходятся на один момент, и без этого экран получил бы
  // несколько ответов подряд — в том числе в порядке, обратном отправке.
  let inFlight: Promise<void> | null = null

  const apply = (value: T, source: 'cache' | 'network') => {
    data.value = value
    loading.value = false
    if (options.onData) options.onData(value, source)
  }

  const hydrate = (): boolean => {
    const entry = readCache<T>(options.key, options.ttlMs)
    if (!entry) return false
    if (options.acceptCached && !options.acceptCached(entry.value)) {
      dropCache(options.key)
      return false
    }
    fromCache.value = true
    updatedAt.value = entry.ts
    apply(entry.value, 'cache')
    return true
  }

  const refresh = (force = false): Promise<void> => {
    if (inFlight) {
      if (!force) return inFlight
      return inFlight.then(() => refresh(true))
    }
    refreshing.value = true
    inFlight = options
      .fetcher()
      .then((value) => {
        error.value = null
        fromCache.value = false
        updatedAt.value = Date.now()
        writeCache(options.key, value)
        apply(value, 'network')
      })
      .catch((err) => {
        // Прежнее значение остаётся на экране: сорванное обновление — не повод
        // очищать список, который мгновение назад был верным. Прелоадер
        // гасится в любом случае, иначе экран без кэша крутил бы его вечно.
        error.value = err
        loading.value = false
      })
      .finally(() => {
        refreshing.value = false
        inFlight = null
      })
    return inFlight
  }

  const load = (): Promise<void> => {
    hydrate()
    return refresh()
  }

  const reload = (): Promise<void> => refresh(true)

  const set = (value: T) => {
    fromCache.value = false
    updatedAt.value = Date.now()
    writeCache(options.key, value)
    apply(value, 'network')
  }

  const invalidate = () => dropCache(options.key)

  return {
    data,
    loading,
    refreshing,
    fromCache,
    updatedAt,
    error,
    load,
    hydrate,
    refresh: () => refresh(),
    reload,
    set,
    invalidate,
  }
}
