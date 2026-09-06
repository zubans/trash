import api from '../services/api'

// Геймификация исполнителя: значки, уровень, подарки и внутренняя почта.
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
}

export interface ExecutorLevel {
  points: number
  level: number
  next_level_points: number
  base_percent: number
  discount_pp: number
  percent: number
  max_useful_level: number
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

export interface MailMessage {
  id: string
  kind: 'ACHIEVEMENT' | 'GIFT' | 'PROMO' | 'NEWS' | 'SYSTEM'
  subject: string
  body: string
  ref_type?: string
  ref_id?: string
  created_at: string
  read_at?: string
}

export async function getAchievements(): Promise<AchievementCard[]> {
  const response = await api.get('/executor/achievements')
  return Array.isArray(response.data) ? response.data : []
}

export async function getLevel(): Promise<ExecutorLevel> {
  const response = await api.get('/executor/level')
  return response.data
}

export async function getGifts(): Promise<UserGift[]> {
  const response = await api.get('/executor/gifts')
  return Array.isArray(response.data) ? response.data : []
}

export async function revealGift(id: string): Promise<UserGift> {
  const response = await api.post(`/executor/gifts/${id}/reveal`)
  return response.data
}

export async function getMail(): Promise<{ messages: MailMessage[]; unread: number }> {
  const response = await api.get('/user/mail')
  return { messages: response.data?.messages ?? [], unread: response.data?.unread ?? 0 }
}

export async function getMailUnread(): Promise<number> {
  const response = await api.get('/user/mail/unread')
  return response.data?.unread ?? 0
}

export async function markMailRead(id: string): Promise<void> {
  await api.post(`/user/mail/${id}/read`)
}

export async function markAllMailRead(): Promise<void> {
  await api.post('/user/mail/read-all')
}

export async function deleteMail(id: string): Promise<void> {
  await api.delete(`/user/mail/${id}`)
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

export async function adminBroadcastMail(payload: {
  kind: 'NEWS' | 'PROMO'
  role?: string
  subject: string
  body: string
}): Promise<number> {
  const response = await api.post('/admin/mail/broadcast', payload)
  return response.data?.sent ?? 0
}

export async function adminGetIncidents(all = false): Promise<MoneyIncident[]> {
  const response = await api.get('/admin/finances/incidents', { params: all ? { all: '1' } : {} })
  return Array.isArray(response.data) ? response.data : []
}

export async function adminResolveIncident(id: string, resolution: string): Promise<void> {
  await api.post(`/admin/finances/incidents/${id}/resolve`, { resolution })
}
