import api from '../services/api'

// Случай, переданный поведением услуги администратору: например, заказ
// верификации, чьи отправленные данные дважды не совпали. Попытки идут вместе с
// ним, потому что сравнить прочитанное модератором в документе с учётной
// записью — вся задача того экрана.
export interface OrderSubmission {
  id: string
  order_id: string
  executor_id: string
  attempt: number
  matched: boolean
  fields: Record<string, string>
  mismatches: string[]
  created_at: string
}

export interface BehaviorEscalation {
  id: string
  order_id: string
  behavior_code: string
  reason: string
  status: string
  created_at: string
  resolved_at?: string
  customer_id: string
  customer_name?: string
  order_status: string
  service_code?: string
  submissions?: OrderSubmission[]
}

export async function getEscalations(status = 'OPEN'): Promise<BehaviorEscalation[]> {
  const response = await api.get('/admin/escalations', { params: { status } })
  return Array.isArray(response.data) ? response.data : []
}

export async function resolveEscalation(id: string): Promise<void> {
  await api.post(`/admin/escalations/${id}/resolve`)
}
