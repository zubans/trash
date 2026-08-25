import { describe, it, expect, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useAuthStore } from './auth-store'

describe('useAuthStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
    document.cookie = 'token=; Max-Age=0; path=/;'
    document.cookie = 'userID=; Max-Age=0; path=/;'
    document.cookie = 'role=; Max-Age=0; path=/;'
    document.cookie = 'phone=; Max-Age=0; path=/;'
  })

  it('initializes with default empty state when no storage or cookie exists', () => {
    const store = useAuthStore()
    expect(store.token).toBe('')
    expect(store.userID).toBe('')
    expect(store.role).toBe('')
    expect(store.phone).toBe('')
    expect(store.isAuthenticated).toBe(false)
    expect(store.isCustomer).toBe(false)
    expect(store.isExecutor).toBe(false)
    expect(store.isAdmin).toBe(false)
  })

  it('updates state on login() and stores values', () => {
    const store = useAuthStore()
    store.login('jwt-token-123', 'EXECUTOR', '+79991234567', 'user-uuid-1')

    expect(store.token).toBe('jwt-token-123')
    expect(store.role).toBe('EXECUTOR')
    expect(store.phone).toBe('+79991234567')
    expect(store.userID).toBe('user-uuid-1')
    expect(store.isAuthenticated).toBe(true)
    expect(store.isExecutor).toBe(true)
    expect(store.isCustomer).toBe(false)
  })

  it('clears state on logout()', () => {
    const store = useAuthStore()
    store.login('jwt-token-123', 'CUSTOMER', '+79991234567', 'user-uuid-1')
    expect(store.isAuthenticated).toBe(true)

    store.logout()
    expect(store.token).toBe('')
    expect(store.userID).toBe('')
    expect(store.role).toBe('')
    expect(store.phone).toBe('')
    expect(store.isAuthenticated).toBe(false)
  })

  it('allows setting currency', () => {
    const store = useAuthStore()
    expect(store.currency).toBe('RUB')
    store.setCurrency('USD')
    expect(store.currency).toBe('USD')
  })

  it('exposes no balance until the user is loaded, so screens do not render a wrong zero', () => {
    const store = useAuthStore()
    expect(store.user).toBeNull()
    expect(store.balance).toBeNull()
  })

  it('keeps the last known user when a refresh fails', async () => {
    const store = useAuthStore()
    store.token = 'a-token'
    store.user = {
      id: 'u1', phone: '+79990000000', email: '', role: 'EXECUTOR', status: 'ACTIVE',
      balance: 1500, first_name: 'Иван', last_name: 'Иванов', patronymic: 'Иванович',
      birth_date: '', age: 30, is_verified: true,
    }

    // The network call is left unmocked: it rejects, which is the failure path.
    await store.fetchMe()

    expect(store.balance).toBe(1500)
    expect(store.userError).not.toBe('')
  })

  it('builds a full name from the loaded user', () => {
    const store = useAuthStore()
    store.user = {
      id: 'u1', phone: '', email: '', role: 'CUSTOMER', status: 'ACTIVE', balance: 0,
      first_name: 'Иван', last_name: 'Иванов', patronymic: 'Иванович',
      birth_date: '', age: 30, is_verified: false,
    }
    expect(store.fullName).toBe('Иванов Иван Иванович')
  })

  it('drops the refresh token on logout', () => {
    const store = useAuthStore()
    store.login('t', 'EXECUTOR', '+79990000000', 'u1', 'refresh-value')
    expect(localStorage.getItem('refreshToken')).toBe('refresh-value')

    store.logout()
    expect(localStorage.getItem('refreshToken')).toBeNull()
    expect(store.user).toBeNull()
  })
})
