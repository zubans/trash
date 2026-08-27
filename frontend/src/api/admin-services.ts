import api from '../services/api'
import type { ServiceNode } from './services'

export interface CreateServiceNodeRequest {
  parent_id?: string
  code: string
  name: Record<string, string>
  description?: Record<string, string>
  node_type: 'CATEGORY' | 'VARIANT'
  base_price?: number
  is_auction?: boolean
  is_active?: boolean
  sort_order?: number
}

export interface UpdateServiceNodeRequest {
  parent_id?: string
  name: Record<string, string>
  description?: Record<string, string>
  base_price?: number
  is_auction?: boolean
  is_active?: boolean
  sort_order?: number
}

export interface DeleteServiceNodeResult {
  message: string
  soft: boolean
  had_orders: boolean
}

export async function getAdminServiceNodes(
  includeDeleted = false
): Promise<Array<{ node: ServiceNode; children: any[] }>> {
  const response = await api.get('/admin/service-nodes', {
    params: includeDeleted ? { include_deleted: 'true' } : undefined,
  })
  return response.data
}

export async function getAdminServiceNode(nodeId: string): Promise<ServiceNode> {
  const response = await api.get(`/admin/service-nodes/${nodeId}`)
  return response.data
}

export async function createServiceNode(payload: CreateServiceNodeRequest): Promise<ServiceNode> {
  const response = await api.post('/admin/service-nodes', payload)
  return response.data
}

export async function updateServiceNode(nodeId: string, payload: UpdateServiceNodeRequest): Promise<ServiceNode> {
  const response = await api.put(`/admin/service-nodes/${nodeId}`, payload)
  return response.data
}

// Deletion is soft: the node is retired, orders placed for it keep their
// service, and restoreServiceNode brings it back (switched off).
export async function deleteServiceNode(nodeId: string): Promise<DeleteServiceNodeResult> {
  const response = await api.delete(`/admin/service-nodes/${nodeId}`)
  return response.data
}

export async function restoreServiceNode(nodeId: string): Promise<ServiceNode> {
  const response = await api.post(`/admin/service-nodes/${nodeId}/restore`)
  return response.data
}
