import api from '../services/api'
import type { UserGift } from './achievements'

// Магазин: витрина, покупка, «мои покупки» и привилегии на комиссию
// (doc/implementation_plan_shop.md). Деньги сервер присылает рублями числом,
// как и везде в API; цены на витрине — справка, при оплате сервер сверяет
// ожидаемую цену с текущей.

export type ShopKind = 'PERK' | 'PHYSICAL' | 'CERTIFICATE'
// Константы правила привилегии: {"VALUE": 0.5}. Смысл им задаёт правило.
export type PerkConfig = Record<string, number | string | boolean>
export type FulfillmentMethod = 'PICKUP' | 'DELIVERY'
export type ShopOrderStatus = 'PAID' | 'PROCESSING' | 'SHIPPED' | 'COMPLETED' | 'CANCELED'

export type Localized = Record<string, string>

export interface ShopVariant {
  code: string
  title?: Localized
}

export interface ShopProduct {
  id: string
  kind: ShopKind
  category: string
  title: Localized
  description: Localized
  images: string[]
  price: number
  compare_at_price?: number
  roles: string[]
  requires_verified: boolean
  per_user_limit?: number
  max_qty_per_order: number
  gift_code?: string
  variants: ShopVariant[]
  fulfillment_methods: FulfillmentMethod[]
  // Правило, которое считает ставку, и его константы.
  perk_rule?: string
  perk_config?: PerkConfig
  perk_days?: number
  max_active_per_user?: number
  sort_order: number
  is_active: boolean
  in_stock: boolean
  // Точный остаток — только в админке.
  stock_count?: number
  created_at?: string
  updated_at?: string
}

export interface Storefront {
  enabled: boolean
  offer_version: number
  products: ShopProduct[]
}

// «Сейчас / с привилегией / окупаемость» — считает сервер по той же формуле,
// что и подтверждение заказа.
export interface PerkQuote {
  base_percent: number
  level_percent: number
  current_percent: number
  percent_with_perk: number
  commission_paid: number
  savings: number
  breakeven_turnover?: number
  starts_at: string
  expires_at: string
  queued: boolean
  queue_length: number
  max_queued: number
  useless: boolean
}

export interface ProductCard {
  product: ShopProduct
  perk_quote?: PerkQuote
  offer_version: number
  purchased: number
}

export interface PickupPoint {
  id: string
  title: Localized
  address: string
  hours?: string
  is_active: boolean
}

export interface UserPerk {
  id: string
  user_id: string
  rule_code: string
  // Название правила; константы в нём подставляет ruleText.
  rule_title: string
  rule_version_id?: string
  config: PerkConfig
  starts_at: string
  expires_at: string
  shop_order_id?: string
  shop_order_number?: number
  revoked_at?: string
  granted_by?: string
  reason?: string
  created_at: string
}

export interface ShopTransaction {
  id: string
  type: string
  amount: number
  counterparty?: string
  admin_id?: string
  created_at: string
  direction: number
}

export interface ShopOrder {
  id: string
  number: number
  user_id: string
  product_id: string
  product_snapshot: {
    title?: Localized
    kind?: ShopKind
    category?: string
    image?: string
    price?: number
    gift_code?: string
    perk_rule?: string
    perk_config?: PerkConfig
    perk_days?: number
  }
  variant?: string
  quantity: number
  unit_price: number
  total: number
  status: ShopOrderStatus
  fulfillment: {
    method?: FulfillmentMethod
    variant?: string
    pickup_point_id?: string
    pickup_point?: { title?: Localized; address?: string; hours?: string }
    address?: string
    recipient?: string
    phone?: string
    track?: string
  }
  refunded_amount: number
  offer_version: number
  cancel_reason?: string
  canceled_by?: string
  created_at: string
  updated_at: string
  // Для админки.
  user_phone?: string
  user_name?: string
  support_chat_id?: string
  refund_request_at?: string
  coupons?: UserGift[]
  perks?: UserPerk[]
  transactions?: ShopTransaction[]
}

export interface PurchaseRequest {
  product_id: string
  request_id: string
  expected_price: number
  offer_version: number
  quantity: number
  variant?: string
  fulfillment?: {
    method?: FulfillmentMethod
    pickup_point_id?: string
    address?: string
    recipient?: string
    phone?: string
  }
}

// Отказ магазина: код переводится ключом shop.errors.<code>, message — запасной
// текст сервера, fields — ошибки по полям формы.
export interface ShopApiError {
  error: string
  message: string
  fields?: Record<string, string>
  details?: Record<string, unknown>
}

export function shopError(err: any): ShopApiError | null {
  const data = err?.response?.data
  if (data && typeof data === 'object' && typeof data.error === 'string') return data as ShopApiError
  return null
}

// Текст отказа для человека: перевод по коду, иначе текст сервера, иначе запасной.
export function shopErrorText(err: any, t: (key: string) => string, fallback: string): string {
  const e = shopError(err)
  if (!e) {
    const data = err?.response?.data
    return typeof data === 'string' && data.trim() ? data : fallback
  }
  const key = `shop.errors.${e.error}`
  const translated = t(key)
  return translated !== key ? translated : e.message || fallback
}

// request_id — UUID, созданный при открытии окна оформления. crypto.randomUUID
// есть не во всех WebView, поэтому запасной путь — через getRandomValues.
export function newRequestId(): string {
  const c: any = typeof crypto !== 'undefined' ? crypto : null
  if (c?.randomUUID) return c.randomUUID()
  const bytes = new Uint8Array(16)
  if (c?.getRandomValues) c.getRandomValues(bytes)
  else for (let i = 0; i < 16; i++) bytes[i] = Math.floor(Math.random() * 256)
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}

export function localized(value: Localized | undefined, locale: string): string {
  if (!value) return ''
  return value[locale] || value.ru || value.en || Object.values(value)[0] || ''
}

// --- Покупатель ---------------------------------------------------------------

export async function getStorefront(category?: string): Promise<Storefront> {
  const response = await api.get('/shop/products', { params: category ? { category } : {} })
  const data = response.data || {}
  return {
    enabled: !!data.enabled,
    offer_version: Number(data.offer_version) || 1,
    products: Array.isArray(data.products) ? data.products : [],
  }
}

export async function getProductCard(id: string): Promise<ProductCard> {
  const response = await api.get(`/shop/products/${id}`)
  return response.data
}

export async function getPickupPoints(): Promise<PickupPoint[]> {
  const response = await api.get('/shop/pickup-points')
  return Array.isArray(response.data) ? response.data : []
}

export async function purchase(req: PurchaseRequest): Promise<ShopOrder> {
  const response = await api.post('/shop/orders', req)
  return response.data
}

export async function getMyOrders(): Promise<ShopOrder[]> {
  const response = await api.get('/shop/orders')
  return Array.isArray(response.data) ? response.data : []
}

export async function getMyOrder(id: string): Promise<ShopOrder> {
  const response = await api.get(`/shop/orders/${id}`)
  return response.data
}

export interface MyPerks {
  level: {
    base_percent: number
    level_percent: number
    percent: number
    perk_id?: string
    perk_rule?: string
    perk_title?: string
    perk_expires_at?: string
  }
  queue: UserPerk[]
}

export async function getMyPerks(): Promise<MyPerks> {
  const response = await api.get('/me/perks')
  return {
    level: response.data?.level || { base_percent: 0, level_percent: 0, percent: 0 },
    queue: Array.isArray(response.data?.queue) ? response.data.queue : [],
  }
}

// --- Админка ------------------------------------------------------------------

export type ProductPayload = Omit<ShopProduct, 'id' | 'in_stock' | 'stock_count' | 'created_at' | 'updated_at'>

export async function adminGetProducts(params: { kind?: string; category?: string } = {}): Promise<ShopProduct[]> {
  const response = await api.get('/admin/shop/products', { params })
  return Array.isArray(response.data) ? response.data : []
}

export async function adminSaveProduct(id: string | null, payload: ProductPayload): Promise<ShopProduct> {
  const response = id
    ? await api.put(`/admin/shop/products/${id}`, payload)
    : await api.post('/admin/shop/products', payload)
  return response.data
}

export async function adminUploadImage(file: File | Blob, name = 'image'): Promise<string> {
  const form = new FormData()
  form.append('file', file, name)
  const response = await api.post('/admin/shop/images', form)
  return response.data?.url
}

export async function adminGetPickupPoints(): Promise<PickupPoint[]> {
  const response = await api.get('/admin/shop/pickup-points')
  return Array.isArray(response.data) ? response.data : []
}

export async function adminSavePickupPoint(id: string | null, payload: Omit<PickupPoint, 'id'>): Promise<PickupPoint> {
  const response = id
    ? await api.put(`/admin/shop/pickup-points/${id}`, payload)
    : await api.post('/admin/shop/pickup-points', payload)
  return response.data
}

export interface AdminOrdersQuery {
  status?: string
  product_id?: string
  from?: string
  to?: string
  q?: string
  refund?: boolean
  limit?: number
  offset?: number
}

export async function adminGetOrders(query: AdminOrdersQuery): Promise<{ orders: ShopOrder[]; total: number }> {
  const params: Record<string, string | number> = {}
  for (const [k, v] of Object.entries(query)) {
    if (v === undefined || v === '' || v === false) continue
    params[k] = v === true ? '1' : (v as string | number)
  }
  const response = await api.get('/admin/shop/orders', { params })
  return {
    orders: Array.isArray(response.data?.orders) ? response.data.orders : [],
    total: Number(response.data?.total) || 0,
  }
}

export async function adminCountPaid(): Promise<number> {
  const response = await api.get('/admin/shop/orders/count')
  return Number(response.data?.paid) || 0
}

export async function adminGetOrder(id: string): Promise<ShopOrder> {
  const response = await api.get(`/admin/shop/orders/${id}`)
  return response.data
}

export interface RefundQuote {
  suggested: number
  max: number
  certificate_revealed: boolean
  redeemed: number
}

export async function adminRefundQuote(id: string): Promise<RefundQuote> {
  const response = await api.get(`/admin/shop/orders/${id}/refund-quote`)
  return response.data
}

export async function adminSetOrderStatus(id: string, status: ShopOrderStatus, track = ''): Promise<ShopOrder> {
  const response = await api.post(`/admin/shop/orders/${id}/status`, { status, track })
  return response.data
}

export interface CancelPayload {
  reason: string
  amount?: number
  restock: boolean
  partner_confirmed: boolean
}

export async function adminCancelOrder(id: string, payload: CancelPayload): Promise<ShopOrder> {
  const response = await api.post(`/admin/shop/orders/${id}/cancel`, payload)
  return response.data
}

export async function adminGetUserShop(userId: string): Promise<{ orders: ShopOrder[]; perks: UserPerk[] }> {
  const response = await api.get(`/admin/users/${userId}/shop`)
  return {
    orders: Array.isArray(response.data?.orders) ? response.data.orders : [],
    perks: Array.isArray(response.data?.perks) ? response.data.perks : [],
  }
}

export async function adminGrantPerk(
  userId: string,
  payload: { rule: string; config: PerkConfig; days: number; reason: string },
): Promise<UserPerk> {
  const response = await api.post(`/admin/users/${userId}/perks`, payload)
  return response.data
}

export async function adminRevokePerk(perkId: string): Promise<UserPerk> {
  const response = await api.delete(`/admin/perks/${perkId}`)
  return response.data
}

export interface ShopSalesRow {
  product_id: string
  title: Localized
  kind: ShopKind
  orders: number
  quantity: number
  total: number
  refunded: number
}

export interface ShopRevenue {
  balance: number
  from: string
  to: string
  sales: ShopSalesRow[]
  total: number
  refunded: number
}

export async function adminGetRevenue(from?: string, to?: string): Promise<ShopRevenue> {
  const response = await api.get('/admin/finances/shop', { params: { from, to } })
  return {
    ...response.data,
    sales: Array.isArray(response.data?.sales) ? response.data.sales : [],
  }
}

export async function adminPayoutRevenue(amount: number): Promise<number> {
  const response = await api.post('/admin/finances/shop/payout', { amount })
  return Number(response.data?.balance) || 0
}

// Правила привилегий (implementation_plan_delivery_passport.md §1): скрипты,
// которые считают ставку. Поставляемые приезжают со сборкой и только
// включаются, собственные пишутся здесь.
export interface PerkRule {
  code: string
  title: string
  origin: 'SHIPPED' | 'OWN'
  is_active: boolean
  description: string
  defaults: PerkConfig
  source: string
  version_id?: string
}

// Строка прогона по сетке: какую ставку правило даёт при такой базе и уровне.
export interface PerkGridRow {
  base: number
  level: number
  level_percent: number
  percent: number
}

export async function adminPerkRules(): Promise<PerkRule[]> {
  const response = await api.get('/admin/shop/perk-rules')
  return Array.isArray(response.data) ? response.data : []
}

export async function adminCheckPerkRule(source: string): Promise<{ grid: PerkGridRow[]; defaults: PerkConfig }> {
  const response = await api.post('/admin/shop/perk-rules/check', { source })
  return { grid: response.data?.grid ?? [], defaults: response.data?.defaults ?? {} }
}

export async function adminSavePerkRule(
  payload: { code: string; title: string; source: string; is_active: boolean },
  create: boolean,
): Promise<{ rule: PerkRule; grid: PerkGridRow[] }> {
  const response = create
    ? await api.post('/admin/shop/perk-rules', payload)
    : await api.put(`/admin/shop/perk-rules/${payload.code}`, payload)
  return { rule: response.data?.rule, grid: response.data?.grid ?? [] }
}
