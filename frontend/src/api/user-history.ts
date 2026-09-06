import api from '../services/api'

// История одного пользователя для его карточки в админке: проводки и заказы.
//
// Отдельные эндпоинты, а не общие журналы с поиском по телефону: там поиск
// нестрогий (LIKE по цифрам), поэтому короткий номер подтянул бы чужие строки.
// На карточке пользователя показывать чужие деньги нельзя, поэтому отбор идёт
// по его id.

export interface UserTransaction {
  id: string
  user_id: string
  user_phone: string
  order_id?: string
  type: string
  amount: number
  counterparty?: string
  admin_id?: string
  created_at: string
  // Как проводка двигает баланс: +1, -1 или 0. Приходит с сервера, чтобы
  // клиент не выводил соглашение о знаках заново.
  direction: number
}

export interface UserOrder {
  id: string
  customer_id: string
  executor_id?: string
  status: string
  hold_amount: number
  final_amount?: number
  address?: string
  created_at: string
  completed_at?: string
  canceled_at?: string
  customer_phone: string
  executor_phone?: string
  service_variant_name: string
}

export async function getUserTransactions(
  userID: string,
  params: { limit?: number; offset?: number } = {},
): Promise<{ transactions: UserTransaction[]; total: number }> {
  const res = await api.get(`/admin/users/${userID}/transactions`, { params })
  return { transactions: res.data.transactions || [], total: res.data.total || 0 }
}

export async function getUserOrders(
  userID: string,
  params: { limit?: number; offset?: number } = {},
): Promise<{ orders: UserOrder[]; total: number }> {
  const res = await api.get(`/admin/users/${userID}/orders`, { params })
  return { orders: res.data.orders || [], total: res.data.total || 0 }
}
