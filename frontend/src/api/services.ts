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
  min_age?: number
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
