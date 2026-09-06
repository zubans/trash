import api from '../services/api'

// Роли и права. Роль — строка справочника, право — пара «раздел админки +
// действие». Каталог разделов приходит с бэкенда, а не дублируется здесь:
// раздел, добавленный там, обязан появиться в матрице сам, иначе права начнут
// расходиться с тем, что реально охраняют маршруты.

export type PermissionAction = 'view' | 'create' | 'edit' | 'delete'

export interface PermissionSection {
  key: string
  label: string
  group: string
  route: string
  actions: PermissionAction[]
  hint?: string
}

export interface PermissionCatalog {
  sections: PermissionSection[]
  actions: { key: PermissionAction; label: string }[]
}

export interface Role {
  code: string
  name: string
  description: string
  // Системные роли (CUSTOMER, EXECUTOR, MODERATOR, ADMIN) неудаляемы: на них
  // опираются маршруты и выбор дашборда.
  is_system: boolean
  created_at: string
  permissions: string[]
  user_count: number
}

export interface RoleUser {
  id: string
  phone: string
  email: string
  full_name: string
  status: string
  // Основная роль пользователя — та, чей дашборд открывается по умолчанию.
  is_primary: boolean
  created_at: string
}

export async function getPermissionCatalog(): Promise<PermissionCatalog> {
  const res = await api.get('/admin/permissions')
  return res.data
}

export async function getRoles(): Promise<Role[]> {
  const res = await api.get('/admin/roles')
  return res.data.roles || []
}

export async function createRole(payload: {
  code: string
  name: string
  description: string
  permissions: string[]
}): Promise<Role> {
  const res = await api.post('/admin/roles', payload)
  return res.data
}

export async function updateRole(
  code: string,
  payload: { name: string; description: string; permissions: string[] },
): Promise<Role> {
  const res = await api.put(`/admin/roles/${encodeURIComponent(code)}`, payload)
  return res.data
}

export async function deleteRole(code: string): Promise<void> {
  await api.delete(`/admin/roles/${encodeURIComponent(code)}`)
}

export async function getRoleUsers(
  code: string,
  params: { search?: string; limit?: number; offset?: number } = {},
): Promise<{ users: RoleUser[]; total: number }> {
  const res = await api.get(`/admin/roles/${encodeURIComponent(code)}/users`, { params })
  return { users: res.data.users || [], total: res.data.total || 0 }
}

export async function assignRole(code: string, userID: string): Promise<void> {
  await api.post(`/admin/roles/${encodeURIComponent(code)}/users`, { user_id: userID })
}

export async function unassignRole(code: string, userID: string): Promise<void> {
  await api.delete(`/admin/roles/${encodeURIComponent(code)}/users/${userID}`)
}
