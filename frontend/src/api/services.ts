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
  // Скрипт поведения, несущий собственные правила этой услуги (кто её видит,
  // сколько она стоит, что происходит по завершении). Пусто для обычной услуги.
  behavior_code?: string
  // Конфигурация этого скрипта на уровне узла. Её ключи принадлежат поведению, а
  // не каталогу, поэтому это остаётся открытым объектом.
  behavior_config?: Record<string, unknown>
  // Собственный скрипт узла, написанный в конструкторе услуг: файл констант и
  // логика. Когда они заданы, узел выполняет их вместо любого библиотечного
  // поведения.
  behavior_constants?: string
  behavior_source?: string
  // Выставляется при списании узла. Каталог никогда не возвращает удалённые узлы
  // приложению; админ-панель запрашивает их явно.
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
