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
  behavior_code?: string
  behavior_config?: Record<string, unknown>
  behavior_constants?: string
  behavior_source?: string
}

export interface UpdateServiceNodeRequest {
  parent_id?: string
  name: Record<string, string>
  description?: Record<string, string>
  base_price?: number
  is_auction?: boolean
  is_active?: boolean
  sort_order?: number
  behavior_code?: string
  behavior_config?: Record<string, unknown>
  behavior_constants?: string
  behavior_source?: string
}

// ServiceBehavior — один скрипт поведения, загруженный сервером: то, что узел
// может назвать в behavior_code, и что означает его настройка. Список берётся из
// самих скриптов, поэтому новое поведение появляется в админ-панели, как только
// его файл выкачен.
export interface ServiceBehavior {
  code: string
  name: string
  description?: string
  once_per_user?: boolean
  release_claim_on_cancel?: boolean
  events?: string[]
  defaults?: Record<string, unknown>
  hooks?: string[]
  // Собственный текст скрипта, который конструктор предлагает как стартовый
  // шаблон и показывает для узла, выполняющего это библиотечное поведение.
  constants_source?: string
  source?: string
}

export async function getServiceBehaviors(): Promise<ServiceBehavior[]> {
  const response = await api.get('/admin/service-behaviors')
  return Array.isArray(response.data) ? response.data : []
}

export interface DeleteServiceNodeResult {
  message: string
  soft: boolean
  had_orders: boolean
  // Сколько узлов ушло вместе с этим (узел + всё поддерево). Для категории с
  // вложенными элементами > 1.
  deleted_count: number
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

// Удаление мягкое: узел списывается, размещённые по нему заказы сохраняют свою
// услугу, а restoreServiceNode возвращает его (выключенным).
export async function deleteServiceNode(nodeId: string): Promise<DeleteServiceNodeResult> {
  const response = await api.delete(`/admin/service-nodes/${nodeId}`)
  return response.data
}

export async function restoreServiceNode(nodeId: string): Promise<ServiceNode> {
  const response = await api.post(`/admin/service-nodes/${nodeId}/restore`)
  return response.data
}
