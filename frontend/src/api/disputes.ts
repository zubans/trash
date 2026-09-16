import api from '../services/api'

// Споры по исполненным заказам (GET/POST /admin/disputes...).

export type DisputeDecision = 'executor' | 'customer' | 'unknown'

export interface AdminDispute {
  id: string
  order_id: string
  customer_id: string
  executor_id: string
  claim: string
  status: 'OPEN' | 'CLOSED'
  closure?: 'CUSTOMER_CONFIRMED' | 'EXECUTOR_CONCEDED' | 'ARBITRATION'
  decision?: 'EXECUTOR' | 'CUSTOMER' | 'UNKNOWN'
  resolution_note?: string
  created_at: string
  closed_at?: string
  order_status: string
  order_address?: string
  order_comment?: string
  hold_amount: number
  final_amount: number
  service_name: string
  order_created_at: string
  order_assigned_at?: string
  customer_name: string
  customer_phone: string
  executor_name: string
  executor_phone: string
  customer_active_points: number
  executor_active_points: number
  executor_disputes_total: number
}

export interface EvidenceTrack {
  lat: number
  lon: number
  source: string
  device_at: string
  reported_at: string
  age_min: number
  distance_to_photo_m?: number
  distance_to_order_m?: number
}

export interface EvidenceGeoAlert {
  id: string
  new_lat: number
  new_lon: number
  calculated_speed_kmh: number
  created_at: string
}

export interface ProofEvidence {
  id: string
  kind: 'AREA' | 'SELFIE'
  camera: 'FRONT' | 'REAR'
  file_url: string
  file_size: number
  exif_taken_at?: string
  exif_lat?: number
  exif_lon?: number
  device_taken_at: string
  device_lat?: number
  device_lon?: number
  seal_status: 'VALID' | 'MISSING' | 'INVALID'
  mark_status: 'FOUND' | 'NOT_FOUND' | 'MISMATCH'
  uploaded_at: string
  taken_vs_executed_min?: number
  exif_vs_device_min?: number
  distance_to_order_m?: number
  exif_distance_to_order_m?: number
  track?: EvidenceTrack
  geo_alerts: EvidenceGeoAlert[]
  flags: string[]
}

export interface DisputeEvidence {
  dispute: AdminDispute
  order: {
    id: string
    status: string
    address?: string
    pickup_lat?: number
    pickup_lon?: number
    photo_required: boolean
    executed_at?: string
    executed_at_device?: string
  }
  gesture?: { code: string; number: number; title: string; description: string; hint_image_url?: string }
  proofs: ProofEvidence[]
  limits: { max_time_diff_min: number; max_distance_m: number; max_track_gap_min: number; geo_alert_window_min: number }
}

export async function listDisputes(status: '' | 'OPEN' | 'CLOSED'): Promise<AdminDispute[]> {
  const { data } = await api.get('/admin/disputes', { params: { status, limit: 200 } })
  return data || []
}

export async function getDisputeEvidence(id: string): Promise<DisputeEvidence> {
  const { data } = await api.get(`/admin/disputes/${id}/evidence`)
  return data
}

export async function resolveDispute(id: string, decision: DisputeDecision, note: string): Promise<AdminDispute> {
  const { data } = await api.post(`/admin/disputes/${id}/resolve`, { decision, note })
  return data
}
