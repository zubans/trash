import api from '../services/api'

export interface OrderReview {
  id: string
  order_id: string
  author_id: string
  target_id: string
  author_role: 'CUSTOMER' | 'EXECUTOR'
  rating: number
  tags: string[]
  comment: string
  photos: string[]
  created_at: string
}

export interface UserRatingSummary {
  user_id: string
  rating: number
  reviews_count: number
}

export async function submitOrderReview(orderId: string, payload: { rating: number; tags?: string[]; comment?: string; photos?: string[] }): Promise<OrderReview> {
  const response = await api.post(`/orders/${orderId}/reviews`, payload)
  return response.data
}

// sendOrderTip charges a tip to the executor of a completed order. The amount
// is in rubles; the backend moves it from the customer's balance.
export async function sendOrderTip(orderId: string, amount: number): Promise<void> {
  await api.post(`/customer/orders/${orderId}/tip`, { amount })
}

export async function checkMyOrderReview(orderId: string): Promise<{ has_reviewed: boolean; review?: OrderReview }> {
  const response = await api.get(`/orders/${orderId}/reviews/mine`)
  return response.data
}

export async function getUserReviews(userId: string, limit = 20, offset = 0): Promise<OrderReview[]> {
  const response = await api.get(`/users/${userId}/reviews`, { params: { limit, offset } })
  return response.data || []
}

export async function getUserRating(userId: string, role = 'EXECUTOR'): Promise<UserRatingSummary> {
  const response = await api.get(`/users/${userId}/rating`, { params: { role } })
  return response.data
}
