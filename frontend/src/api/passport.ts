import api from '../services/api'

// Паспорт, статус «проверенный» и согласие на обработку персональных данных
// (doc/implementation_plan_delivery_passport.md §2–§4). Полные данные паспорта
// сервер отдаёт только праву passports.view; владелец видит маску.

export interface PassportData {
  series: string
  number: string
  issued_at: string
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
  // Заявка на подтверждение уже подана — просить ещё раз не нужно.
  check_requested_at?: string
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

// takenAt — время съёмки по часам телефона. Сервер проверяет им подпись снимка;
// снимок, принесённый готовым, его не имеет.
function photoForm(photo: Blob, takenAt?: string, name = 'passport.jpg'): FormData {
  const form = new FormData()
  form.append('file', photo, name)
  if (takenAt) form.append('taken_at', takenAt)
  return form
}

// CaptureKey — то, чем приложение подписывает снимок паспорта: ключ и
// идентификатор области, для которой он выдан.
export interface CaptureKey {
  id: string
  key: string
}

export const emptyPassport = (): PassportData => ({ series: '', number: '', issued_at: '' })

export function passportPayload(data: PassportData): PassportData {
  return { series: data.series.trim(), number: data.number.trim(), issued_at: data.issued_at }
}

export async function getMyPassport(): Promise<PassportMask> {
  return (await api.get('/me/passport')).data
}

// Свои данные целиком — для формы правки: набранное однажды не набирают снова.
export async function getMyPassportData(): Promise<PassportData> {
  return (await api.get('/me/passport/data')).data
}

export async function saveMyPassport(data: PassportData): Promise<PassportMask> {
  return (await api.put('/me/passport', passportPayload(data))).data
}

export async function uploadMyPassportPhoto(photo: Blob, takenAt?: string): Promise<PassportMask> {
  return (await api.post('/me/passport/photo', photoForm(photo, takenAt))).data
}

export async function myPassportPhotoKey(): Promise<CaptureKey> {
  return (await api.get('/me/passport/photo/key')).data
}

export async function orderPassportPhotoKey(orderId: string): Promise<CaptureKey> {
  return (await api.get(`/executor/orders/${orderId}/passport/photo/key`)).data
}

export async function acceptPDConsent(): Promise<void> {
  await api.post('/me/pd-consent')
}

export async function saveOrderPassport(orderId: string, data: PassportData): Promise<void> {
  await api.put(`/executor/orders/${orderId}/passport`, passportPayload(data))
}

export async function uploadOrderPassportPhoto(orderId: string, photo: Blob, takenAt?: string): Promise<void> {
  await api.post(`/executor/orders/${orderId}/passport/photo`, photoForm(photo, takenAt))
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
  // Человек ждёт подтверждения статуса: паспорт отдан на верификации.
  check_requested_at?: string
  // Итог скрытой проверки снимка: подписан ли он приложением и есть ли метка в
  // изображении. Владельцу паспорта не показывается.
  photo_seal?: 'VALID' | 'MISSING' | 'INVALID'
  photo_mark?: 'FOUND' | 'NOT_FOUND' | 'MISMATCH'
}

// Состояние без паспортных данных — в журнал просмотров не пишется.
export async function adminPassportStatus(userId: string): Promise<PassportStatus> {
  return (await api.get(`/admin/users/${userId}/passport/status`)).data
}
