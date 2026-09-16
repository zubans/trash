import api from '../services/api'

// Жесты фото-подтверждения (/admin/watermark-symbols).

export interface WatermarkSymbol {
  id: string
  code: string
  number: number
  title: string
  description: string
  hint_image_url?: string
  fits_in_selfie: boolean
  sort_order: number
  deleted_at?: string
}

export type WatermarkSymbolInput = Pick<
  WatermarkSymbol,
  'code' | 'title' | 'description' | 'hint_image_url' | 'fits_in_selfie' | 'sort_order'
>

export async function listSymbols(includeDeleted: boolean): Promise<WatermarkSymbol[]> {
  const { data } = await api.get('/admin/watermark-symbols', { params: { include_deleted: includeDeleted } })
  return data || []
}

export async function createSymbol(input: WatermarkSymbolInput): Promise<WatermarkSymbol> {
  const { data } = await api.post('/admin/watermark-symbols', input)
  return data
}

export async function updateSymbol(id: string, input: WatermarkSymbolInput): Promise<WatermarkSymbol> {
  const { data } = await api.put(`/admin/watermark-symbols/${id}`, input)
  return data
}

export async function deleteSymbol(id: string): Promise<void> {
  await api.delete(`/admin/watermark-symbols/${id}`)
}

export async function restoreSymbol(id: string): Promise<void> {
  await api.post(`/admin/watermark-symbols/${id}/restore`)
}
