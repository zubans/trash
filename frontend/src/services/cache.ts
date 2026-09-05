/**
 * Постоянный кэш прочитанных экраном данных.
 *
 * Экран, открытый второй раз, не должен начинаться с пустоты: список заказов,
 * состояние смены и история уже были загружены минуту назад, а мобильная сеть
 * отдаёт их заново секундами. Кэш переживает переход между вкладками, сворачивание
 * приложения и его перезапуск, поэтому экран рисуется сразу тем, что было, а
 * сеть только уточняет.
 *
 * Записи привязаны к пользователю: на общем устройстве следующий вошедший не
 * должен увидеть чужие заказы, даже мельком. Ключ без известного пользователя
 * не читается и не пишется вовсе — анонимного владельца у этих данных нет.
 */

const PREFIX = 'cache:v1:'

// Сколько живёт запись. Кэш существует ради первого кадра, а не ради работы
// офлайн: показать вчерашний список заказов хуже, чем показать прелоадер.
const DEFAULT_TTL_MS = 12 * 60 * 60 * 1000

export interface CacheEntry<T> {
  value: T
  /** Когда значение положили в кэш (Date.now()). */
  ts: number
}

function currentScope(): string {
  try {
    return localStorage.getItem('userID') || ''
  } catch {
    return ''
  }
}

function storageKey(key: string, scope: string): string {
  return `${PREFIX}${scope}:${key}`
}

/**
 * Читает запись кэша. Возвращает null, когда её нет, она просрочена или
 * повреждена: во всех трёх случаях звать нечего, экран показывает прелоадер и
 * ждёт сеть.
 */
export function readCache<T>(key: string, ttlMs: number = DEFAULT_TTL_MS): CacheEntry<T> | null {
  const scope = currentScope()
  if (!scope) return null
  try {
    const raw = localStorage.getItem(storageKey(key, scope))
    if (!raw) return null
    const entry = JSON.parse(raw) as CacheEntry<T>
    if (!entry || typeof entry.ts !== 'number') return null
    // Сравнение нестрогое: нулевой срок годности означает «не годится
    // никогда», а не «годится ровно в миллисекунду записи».
    if (Date.now() - entry.ts >= ttlMs) {
      localStorage.removeItem(storageKey(key, scope))
      return null
    }
    return entry
  } catch {
    return null
  }
}

/**
 * Сохраняет значение. Любой сбой записи (приватный режим, переполненное
 * хранилище) молча игнорируется: кэш — ускорение, и его отсутствие не должно
 * ломать экран, который уже получил данные.
 */
export function writeCache<T>(key: string, value: T): void {
  const scope = currentScope()
  if (!scope) return
  try {
    const entry: CacheEntry<T> = { value, ts: Date.now() }
    localStorage.setItem(storageKey(key, scope), JSON.stringify(entry))
  } catch {
    // Переполнение — не повод падать: чистим свои записи и пробуем один раз.
    try {
      clearCachedData()
      localStorage.setItem(storageKey(key, scope), JSON.stringify({ value, ts: Date.now() }))
    } catch {
      // Хранилище недоступно — работаем без кэша.
    }
  }
}

/** Удаляет одну запись — например, когда данные заведомо устарели после действия. */
export function dropCache(key: string): void {
  const scope = currentScope()
  if (!scope) return
  try {
    localStorage.removeItem(storageKey(key, scope))
  } catch {
    // игнорируем
  }
}

/**
 * Стирает весь кэш приложения. Вызывается в конце сессии: данные заказов и
 * профиля не должны пережить выход из аккаунта на общем устройстве.
 */
export function clearCachedData(): void {
  try {
    const keys: string[] = []
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i)
      if (key && key.startsWith(PREFIX)) keys.push(key)
    }
    for (const key of keys) localStorage.removeItem(key)
  } catch {
    // игнорируем
  }
}
