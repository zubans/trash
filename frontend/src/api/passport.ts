import api from '../services/api'

// Паспорт, статус «проверенный» и согласие на обработку персональных данных
// (doc/implementation_plan_delivery_passport.md §2–§4). Полные данные паспорта
// сервер отдаёт только праву passports.view; владелец видит маску.

export interface PassportData {
  series: string
  number: string
  issued_at: string
  issued_by?: string
  division_code?: string
}

export interface PassportMask {
  exists: boolean
  series?: string
  number?: string
  issued_year?: string
  has_photo: boolean
  source?: 'OWNER' | 'VERIFICATION' | 'ADMIN'
  updated_at?: string
  // Пользователь «проверенный»: правка — через поддержку.
  locked: boolean
}

export interface PassportFull extends PassportData {
  has_photo: boolean
  source: 'OWNER' | 'VERIFICATION' | 'ADMIN'
  entered_by?: string
  updated_at: string
}

export interface PassportApiError {
  error: string
  message: string
  fields?: Record<string, string>
}

export function passportError(err: any): PassportApiError | null {
  const data = err?.response?.data
  if (data && typeof data === 'object' && typeof data.error === 'string') return data as PassportApiError
  return null
}

export function passportErrorText(err: any, fallback: string): string {
  const e = passportError(err)
  if (e?.message) return e.message
  const data = err?.response?.data
  return typeof data === 'string' && data.trim() ? data.trim() : fallback
}

function photoForm(photo: Blob, name = 'passport.jpg'): FormData {
  const form = new FormData()
  form.append('file', photo, name)
  return form
}

export const emptyPassport = (): PassportData => ({ series: '', number: '', issued_at: '', issued_by: '', division_code: '' })

// Необязательные поля, оставленные пустыми, не уходят на сервер.
export function passportPayload(data: PassportData): PassportData {
  const out: PassportData = { series: data.series.trim(), number: data.number.trim(), issued_at: data.issued_at }
  if (data.issued_by?.trim()) out.issued_by = data.issued_by.trim()
  if (data.division_code?.trim()) out.division_code = data.division_code.trim()
  return out
}

export async function getMyPassport(): Promise<PassportMask> {
  return (await api.get('/me/passport')).data
}

export async function saveMyPassport(data: PassportData): Promise<PassportMask> {
  return (await api.put('/me/passport', passportPayload(data))).data
}

export async function uploadMyPassportPhoto(photo: Blob): Promise<PassportMask> {
  return (await api.post('/me/passport/photo', photoForm(photo))).data
}

export async function acceptPDConsent(): Promise<void> {
  await api.post('/me/pd-consent')
}

export async function saveOrderPassport(orderId: string, data: PassportData): Promise<void> {
  await api.put(`/executor/orders/${orderId}/passport`, passportPayload(data))
}

export async function uploadOrderPassportPhoto(orderId: string, photo: Blob): Promise<void> {
  await api.post(`/executor/orders/${orderId}/passport/photo`, photoForm(photo))
}

export async function adminGetPassport(userId: string): Promise<PassportFull> {
  return (await api.get(`/admin/users/${userId}/passport`)).data
}

// Фото приходит байтами: показывается через object URL, который вызывающий
// обязан освободить.
export async function adminGetPassportPhoto(userId: string): Promise<Blob> {
  return (await api.get(`/admin/users/${userId}/passport/photo`, { responseType: 'blob' })).data
}

export async function adminSavePassport(userId: string, data: PassportData): Promise<PassportFull> {
  return (await api.put(`/admin/users/${userId}/passport`, passportPayload(data))).data
}

export async function adminUploadPassportPhoto(userId: string, photo: Blob): Promise<void> {
  await api.post(`/admin/users/${userId}/passport/photo`, photoForm(photo))
}

export async function adminDeletePassport(userId: string): Promise<void> {
  await api.delete(`/admin/users/${userId}/passport`)
}

export async function adminSetChecked(userId: string, checked: boolean, reason: string): Promise<void> {
  await api.post(`/admin/users/${userId}/checked`, { checked, reason })
}

export interface PassportStatus {
  exists: boolean
  has_photo: boolean
  source?: 'OWNER' | 'VERIFICATION' | 'ADMIN'
  updated_at?: string
  is_checked: boolean
  consent_given: boolean
}

// Состояние без паспортных данных — в журнал просмотров не пишется.
export async function adminPassportStatus(userId: string): Promise<PassportStatus> {
  return (await api.get(`/admin/users/${userId}/passport/status`)).data
}
