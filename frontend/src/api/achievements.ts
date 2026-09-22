import api from '../services/api'
import type { UserPerk } from './shop'

// Геймификация исполнителя: значки, уровень и подарки. Письма, которыми о них
// сообщают, живут в api/mail.ts.
//
// Уровень здесь — не украшение, а ставка комиссии: баллы всех действующих
// ачивок складываются, каждые level_points баллов дают уровень, каждый уровень
// снимает discount_pp процентных пунктов, до нуля. Поэтому экран показывает не
// только «сколько до следующего», но и что именно уже снято.

export interface AchievementCard {
  code: string
  title: string
  description: string
  icon: string
  weight: number
  repeatable: boolean
  granted: boolean
  count: number
  points: number
  granted_at?: string
  // Когда сгорят баллы этой выдачи. Показывается специально: уровень считается
  // по действующим баллам, поэтому истечение его снижает.
  expires_at?: string
  progress?: number
  available_to?: string
  // Ачивку ещё можно заслужить. У полученной это отдельный от granted вопрос:
  // значок с закрытой акции остаётся на полке, но повторить его уже нельзя.
  available: boolean
}

export interface ExecutorLevel {
  points: number
  level: number
  next_level_points: number
  base_percent: number
  discount_pp: number
  percent: number
  max_useful_level: number
  // Ставка по уровню, до привилегии магазина: percent отличается от неё, только
  // пока привилегия действует.
  level_percent: number
  perk_id?: string
  perk_rule?: string
  perk_title?: string
  perk_expires_at?: string
  // Действующая привилегия и очередь за ней — для строки «дальше: …».
  perk_queue?: UserPerk[]
}

export interface Gift {
  code: string
  kind: 'BONUS' | 'CERTIFICATE' | 'PHYSICAL' | 'PROMO'
  title: Record<string, string>
  description: Record<string, string>
  image_url?: string
  amount: number
  partner?: string
  stock?: number
  valid_days?: number
  is_active: boolean
}

export interface UserGift {
  id: string
  gift_code: string
  // Покупка магазина, которой выдан купон; у подарков ачивок пусто.
  shop_order_id?: string
  coupon_code: string
  status: 'ISSUED' | 'REVEALED' | 'REDEEMED' | 'EXPIRED' | 'CANCELED'
  granted_at: string
  expires_at?: string
  revealed_at?: string
  redeemed_at?: string
  gift?: Gift
  // Код сертификата. Приходит только в ответе на reveal: список его не несёт,
  // потому что показ кода пишется в аудит, а список читают походя.
  secret?: string
}

export async function getAchievements(): Promise<AchievementCard[]> {
  const response = await api.get('/executor/achievements')
  return Array.isArray(response.data) ? response.data : []
}

export async function getLevel(): Promise<ExecutorLevel> {
  const response = await api.get('/executor/level')
  return response.data
}

// Купоны живут на общем маршруте: у заказчика ачивок нет, но купоны на
// купленные в магазине вещи — есть.
export async function getGifts(): Promise<UserGift[]> {
  const response = await api.get('/user/gifts')
  return Array.isArray(response.data) ? response.data : []
}

export async function revealGift(id: string): Promise<UserGift> {
  const response = await api.post(`/user/gifts/${id}/reveal`)
  return response.data
}

// --- Админ -------------------------------------------------------------------

export interface AdminAchievement {
  code: string
  is_active: boolean
  available_from?: string
  available_to?: string
  weight?: number
  config: Record<string, unknown>
  sort_order: number
  title: string
  description: string
  icon: string
  audience: string
  events: string[]
  repeatable: boolean
  script_weight: number
  effective_weight: number
  // Отличает выключенную ачивку от той, чей скрипт не скомпилировался: без
  // этого признака это одно и то же пустое место в списке.
  script_loaded: boolean
  // Ачивка приехала со сборкой: её скрипт править нельзя и удалить её нельзя —
  // строка исчезнет, а скрипт в бинарнике останется.
  is_library: boolean
  // Собственный скрипт, хранящийся в базе. У поставляемой пуст.
  constants: string
  source: string
  deleted_at?: string
  // Текст скрипта из движка — у поставляемой это её файлы из бинарника,
  // которые админ читает и копирует как шаблон для новой.
  constants_source?: string
  source_text?: string
}

export interface AchievementPayload {
  code?: string
  is_active: boolean
  available_from?: string | null
  available_to?: string | null
  weight?: number | null
  config?: Record<string, unknown>
  sort_order?: number
  constants?: string
  source?: string
}

export interface MoneyIncident {
  id: string
  kind: string
  severity: string
  order_id?: string
  user_id?: string
  expected?: number
  actual?: number
  applied?: number
  details?: Record<string, unknown>
  created_at: string
  resolved_at?: string
  resolution?: string
}

export async function adminGetAchievements(deleted = false): Promise<AdminAchievement[]> {
  const response = await api.get('/admin/achievements', { params: deleted ? { deleted: '1' } : {} })
  return Array.isArray(response.data) ? response.data : []
}

export async function adminCreateAchievement(payload: AchievementPayload): Promise<void> {
  await api.post('/admin/achievements', payload)
}

export async function adminSaveAchievement(code: string, payload: AchievementPayload): Promise<void> {
  await api.put(`/admin/achievements/${code}`, payload)
}

// Удаление мягкое: у ачивки есть выданные экземпляры и начисленные по ним
// баллы, то есть чей-то уровень и чья-то ставка комиссии.
export async function adminDeleteAchievement(code: string): Promise<void> {
  await api.delete(`/admin/achievements/${code}`)
}

export async function adminRestoreAchievement(code: string): Promise<void> {
  await api.post(`/admin/achievements/${code}/restore`)
}

// --- Ачивки на карточке пользователя -----------------------------------------

// UserGrant — одна выдача, как её видит администратор. От карточки исполнителя
// отличается тем, что показывает и отозванные: карточка — это история, а не
// витрина.
export interface UserGrant {
  id: string
  code: string
  grant_key: string
  points: number
  order_id?: string
  granted_at: string
  expires_at?: string
  revoked_at?: string
  revoke_reason?: string
}

export interface RecheckResult {
  orders_replayed: number
  granted: string[]
}

export async function adminGetUserAchievements(
  userId: string,
): Promise<{ grants: UserGrant[]; level: ExecutorLevel }> {
  const response = await api.get(`/admin/users/${userId}/achievements`)
  return {
    grants: Array.isArray(response.data?.grants) ? response.data.grants : [],
    level: response.data?.level,
  }
}

// Пересчёт повторяет подтверждённые заказы пользователя и выдаёт то, что выдало
// бы правило. Он ничего не обходит: заказ, не подходящий под условие, ачивкой не
// станет оттого, что нажали кнопку.
export async function adminRecheckUserAchievements(userId: string): Promise<RecheckResult> {
  const response = await api.post(`/admin/users/${userId}/achievements/recheck`)
  return {
    orders_replayed: response.data?.orders_replayed ?? 0,
    granted: Array.isArray(response.data?.granted) ? response.data.granted : [],
  }
}

// Выдача вручную правило обходит — в этом её смысл. Выдать можно только
// включённую ачивку.
export async function adminGrantAchievement(
  userId: string,
  code: string,
  reason: string,
): Promise<void> {
  await api.post(`/admin/users/${userId}/achievements/${code}`, { reason })
}

export async function adminRevokeGrant(grantId: string, reason: string): Promise<void> {
  await api.post(`/admin/achievements/grants/${grantId}/revoke`, { reason })
}

// Пересчёт агрегатов по журналу заказов. Стоит рядом с пересчётом ачивок, потому
// что чинит соседнюю поломку: правило смотрит на эти счётчики, и разошедшийся
// счётчик — вторая причина, по которой заслуженный значок не выдался.
export async function adminRecalculateStats(userId: string): Promise<void> {
  await api.post(`/admin/users/${userId}/stats/recalculate`)
}

export async function adminGetGifts(): Promise<(Gift & { free_codes: number })[]> {
  const response = await api.get('/admin/gifts')
  return Array.isArray(response.data) ? response.data : []
}

export async function adminSaveGift(code: string, gift: Partial<Gift>): Promise<void> {
  await api.put(`/admin/gifts/${code}`, gift)
}

export async function adminAddGiftCodes(code: string, codes: string[]): Promise<number> {
  const response = await api.post(`/admin/gifts/${code}/codes`, { codes })
  return response.data?.added ?? 0
}

export async function adminRedeemCoupon(coupon: string): Promise<UserGift> {
  const response = await api.post(`/admin/gifts/coupons/${coupon}/redeem`)
  return response.data
}

export async function adminGetIncidents(all = false): Promise<MoneyIncident[]> {
  const response = await api.get('/admin/finances/incidents', { params: all ? { all: '1' } : {} })
  return Array.isArray(response.data) ? response.data : []
}

export async function adminResolveIncident(id: string, resolution: string): Promise<void> {
  await api.post(`/admin/finances/incidents/${id}/resolve`, { resolution })
}
