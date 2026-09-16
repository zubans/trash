import api from '../services/api'

// Штрафные баллы пользователя в админке (/admin/users/{id}/penalties).

export interface PenaltyPoint {
  id: string
  role: 'CUSTOMER' | 'EXECUTOR'
  order_id?: string
  dispute_id?: string
  assigned_by?: string
  reason: string
  created_at: string
  revoked_at?: string
  expired_at?: string
}

export interface PenaltyStatus {
  role: 'CUSTOMER' | 'EXECUTOR'
  active_points: number
  photo_required_until?: string
  silent_block_started_at?: string
  silent_block_ends_at?: string
}

export interface PenaltyFlags {
  had_silent_block_at?: string
  soft_banned_at?: string
  soft_banned_by?: string
  soft_ban_reason?: string
}

export interface AdminPenaltyView {
  points: PenaltyPoint[]
  statuses: PenaltyStatus[]
  flags: PenaltyFlags
  limits: Record<string, number>
}

export async function getUserPenalties(userId: string): Promise<AdminPenaltyView> {
  const { data } = await api.get(`/admin/users/${userId}/penalties`)
  return data
}

export async function revokePenaltyPoint(userId: string, pointId: string): Promise<void> {
  await api.post(`/admin/users/${userId}/penalties/${pointId}/revoke`)
}

export async function resetSilentBlockFlag(userId: string): Promise<void> {
  await api.post(`/admin/users/${userId}/penalties/reset-silent-flag`)
}

export async function setUserStatus(userId: string, status: 'ACTIVE' | 'SOFT_BANNED' | 'BANNED', reason = ''): Promise<void> {
  await api.post(`/admin/users/${userId}/status`, { status, reason })
}
