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
}

export async function getServiceCategories(): Promise<ServiceNode[]> {
  const response = await api.get('/service-categories')
  return response.data
}

export async function getServiceCategoryChildren(categoryId: string): Promise<ServiceNode[]> {
  const response = await api.get(`/service-categories/${categoryId}/children`)
  return response.data
}

export async function getServiceVariants(): Promise<ServiceNode[]> {
  const response = await api.get('/service-variants')
  return response.data
}

export async function getCategoryVariants(categoryId: string): Promise<ServiceNode[]> {
  const response = await api.get(`/service-categories/${categoryId}/variants`)
  return response.data
}

export async function getServiceVariant(variantId: string): Promise<{ variant: ServiceNode; path: ServiceNode[] }> {
  const response = await api.get(`/service-variants/${variantId}`)
  return response.data
}
