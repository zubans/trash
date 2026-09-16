/**
 * Взятые заказы на устройстве — для работы без сети.
 *
 * Обычный кэш экрана (services/cache.ts) живёт двенадцать часов и нужен ради
 * первого кадра. Здесь другое: исполнитель, у которого пропала сеть, должен
 * видеть свои заказы, их жесты и служебные данные проверки столько, сколько
 * заказы открыты, — иначе снять фото-подтверждение без сети было бы нечем.
 * Поэтому срока годности нет: снимок списка заменяется следующим ответом сети,
 * а закрытые заказы из него выпадают сами.
 *
 * Записи привязаны к пользователю и стираются при выходе из аккаунта.
 */

const PREFIX = 'offline:v1:'
const ORDERS_KEY = 'orders'

// Статусы, при которых заказ ещё нужен исполнителю на устройстве.
const OPEN_STATUSES = new Set(['ASSIGNED', 'EXECUTED', 'DISPUTED'])

function scope(): string {
  try {
    return localStorage.getItem('userID') || ''
  } catch {
    return ''
  }
}

function key(name: string, user: string): string {
  return `${PREFIX}${user}:${name}`
}

export interface OfflineOrdersSnapshot {
  orders: any[]
  savedAt: number
}

/** Сохраняет открытые заказы из ответа сети. */
export function saveOfflineOrders(orders: any[]): void {
  const user = scope()
  if (!user || !Array.isArray(orders)) return
  const snapshot: OfflineOrdersSnapshot = {
    orders: orders.filter((o) => o && OPEN_STATUSES.has(o.status)),
    savedAt: Date.now(),
  }
  try {
    localStorage.setItem(key(ORDERS_KEY, user), JSON.stringify(snapshot))
  } catch {
    // Переполнение не должно ломать экран, который уже получил данные.
  }
}

/** Последний сохранённый список открытых заказов пользователя. */
export function loadOfflineOrders(): OfflineOrdersSnapshot | null {
  const user = scope()
  if (!user) return null
  try {
    const raw = localStorage.getItem(key(ORDERS_KEY, user))
    if (!raw) return null
    const snapshot = JSON.parse(raw) as OfflineOrdersSnapshot
    return Array.isArray(snapshot?.orders) ? snapshot : null
  } catch {
    return null
  }
}

/** Заказ из сохранённого списка. */
export function offlineOrder(orderId: string): any | null {
  return loadOfflineOrders()?.orders.find((o) => o.id === orderId) ?? null
}

/** Обновляет статус заказа на устройстве — например, после отметки «Исполнил» в офлайне. */
export function patchOfflineOrder(orderId: string, patch: Record<string, unknown>): void {
  const snapshot = loadOfflineOrders()
  const user = scope()
  if (!snapshot || !user) return
  snapshot.orders = snapshot.orders.map((o) => (o.id === orderId ? { ...o, ...patch } : o))
  try {
    localStorage.setItem(key(ORDERS_KEY, user), JSON.stringify(snapshot))
  } catch {
    // см. saveOfflineOrders
  }
}

/** Стирает офлайн-данные всех пользователей: вызывается в конце сессии. */
export function clearOfflineData(): void {
  try {
    const keys: string[] = []
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i)
      if (k && k.startsWith(PREFIX)) keys.push(k)
    }
    for (const k of keys) localStorage.removeItem(k)
  } catch {
    // игнорируем
  }
}

export const OFFLINE_PREFIX = PREFIX
