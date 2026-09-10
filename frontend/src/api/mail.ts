import api from '../services/api'

// Внутренняя почта: ящик пользователя и переписка с администрацией.
//
// Почта — не чат. Чат живёт при заказе и исчезает вместе с ним; письмо
// адресовано человеку, переживает заказ и приходит тому, у кого заказов нет
// вовсе. Поэтому и уведомление здесь другое: конвертик рядом с телефоном, а не
// всплывающее окно поверх экрана.

export type MailKind = 'ACHIEVEMENT' | 'GIFT' | 'PROMO' | 'NEWS' | 'SYSTEM' | 'DIRECT'

export interface MailMessage {
  id: string
  user_id?: string
  kind: MailKind
  subject: string
  body: string
  ref_type?: string
  ref_id?: string
  created_at: string
  read_at?: string
  // IN — письмо пришло владельцу ящика, OUT — он сам ответил администрации.
  direction?: 'IN' | 'OUT'
  // Ветка переписки. У писем ядра и рассылок её нет: отвечать в них некому.
  thread_id?: string
  sender_name?: string
  admin_read_at?: string
  // Считается только для корня ветки в списке ящика: лента показывает
  // переписку одной строкой, а не рассыпает ответы по ящику.
  replies?: number
  thread_unread?: number
  last_at?: string
}

// MailDialog — одна переписка в списке администратора.
export interface MailDialog {
  user_id: string
  phone: string
  full_name: string
  role: string
  thread_id: string
  subject: string
  last_body: string
  last_at: string
  last_direction: 'IN' | 'OUT'
  // Ответы, которых администрация ещё не читала: счётчик долга.
  unread: number
  user_unread: number
  total: number
}

export async function getMail(): Promise<{ messages: MailMessage[]; unread: number }> {
  const response = await api.get('/user/mail')
  return { messages: response.data?.messages ?? [], unread: response.data?.unread ?? 0 }
}

export async function getMailUnread(): Promise<number> {
  const response = await api.get('/user/mail/unread')
  return response.data?.unread ?? 0
}

export async function getMailThread(id: string): Promise<MailMessage[]> {
  const response = await api.get(`/user/mail/${id}/thread`)
  return Array.isArray(response.data?.messages) ? response.data.messages : []
}

export async function replyToMail(id: string, body: string): Promise<MailMessage> {
  const response = await api.post(`/user/mail/${id}/reply`, { body })
  return response.data
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

export async function adminGetMailDialogs(
  onlyUnanswered = false,
): Promise<{ dialogs: MailDialog[]; unread: number }> {
  const response = await api.get('/admin/mail/dialogs', {
    params: onlyUnanswered ? { unanswered: '1' } : {},
  })
  return { dialogs: response.data?.dialogs ?? [], unread: response.data?.unread ?? 0 }
}

export async function adminGetMailUnread(): Promise<number> {
  const response = await api.get('/admin/mail/unread')
  return response.data?.unread ?? 0
}

export async function adminGetUserMail(
  userId: string,
): Promise<{ messages: MailMessage[]; user: { id: string; full_name: string; phone: string } }> {
  const response = await api.get(`/admin/mail/users/${userId}`)
  return {
    messages: Array.isArray(response.data?.messages) ? response.data.messages : [],
    user: response.data?.user ?? { id: userId, full_name: '', phone: '' },
  }
}

// Письмо одному человеку. С thread_id — продолжение начатой переписки, без
// него — новая: тема тогда обязательна, потому что по ней письмо и узнают в
// ящике.
export async function adminSendMail(
  userId: string,
  payload: { subject?: string; body: string; thread_id?: string },
): Promise<MailMessage> {
  const response = await api.post(`/admin/mail/users/${userId}`, payload)
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
