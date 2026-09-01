import api from '../services/api'

export interface ServiceNode {
  id: string
  parent_id?: string
  code: string
  name: Record<string, string>
  description?: Record<string, string>
  node_type: 'CATEGORY' | 'VARIANT'
  base_price?: number
  is_auction: boolean
  is_active: boolean
  sort_order: number
  requires_verification?: boolean
  moderator_only?: boolean
  min_age?: number
  // Behaviour script that carries this service's own rules (who sees it, what
  // it costs, what happens when it is done). Empty for an ordinary service.
  behavior_code?: string
  // That script's per-node configuration. Its keys belong to the behaviour, not
  // to the catalog, which is why this stays an open object.
  behavior_config?: Record<string, unknown>
  // The node's own script, written in the service constructor: the constants
  // file and the logic. When they are set the node runs them instead of any
  // library behaviour.
  behavior_constants?: string
  behavior_source?: string
  // Set when the node was retired. The catalog never returns deleted nodes to
  // the app; the admin panel asks for them explicitly.
  deleted_at?: string | null
}

function normalizeArray<T>(data: unknown): T[] {
  if (Array.isArray(data)) {
    return data as T[]
  }
  if (data && typeof data === 'object' && Array.isArray((data as any).data)) {
    return (data as any).data as T[]
  }
  return []
}

export async function getServiceCategories(): Promise<ServiceNode[]> {
  const response = await api.get('/service-categories')
  return normalizeArray<ServiceNode>(response.data)
}

export async function getServiceCategoryChildren(categoryId: string): Promise<ServiceNode[]> {
  const response = await api.get(`/service-categories/${categoryId}/children`)
  return normalizeArray<ServiceNode>(response.data)
}

export async function getServiceVariants(): Promise<ServiceNode[]> {
  const response = await api.get('/service-variants')
  return normalizeArray<ServiceNode>(response.data)
}

export async function getCategoryVariants(categoryId: string): Promise<ServiceNode[]> {
  const response = await api.get(`/service-categories/${categoryId}/variants`)
  return normalizeArray<ServiceNode>(response.data)
}

export async function getServiceVariant(variantId: string): Promise<{ variant: ServiceNode; path: ServiceNode[] }> {
  const response = await api.get(`/service-variants/${variantId}`)
  return response.data
}
