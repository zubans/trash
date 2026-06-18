import { defineStore } from 'pinia'
import { getCookie } from '../services/api'

// Helper to save a cookie
function setCookie(name: string, value: string, days = 1) {
  const date = new Date()
  date.setTime(date.getTime() + (days * 24 * 60 * 60 * 1000))
  const expires = `; expires=${date.toUTCString()}`
  document.cookie = `${name}=${value}${expires}; path=/; SameSite=Lax`
}

export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: getCookie('token') || '',
    role: getCookie('role') || '',
    phone: getCookie('phone') || '',
  }),
  getters: {
    isAuthenticated: (state) => !!state.token,
    isAdmin: (state) => state.role === 'ADMIN',
    isCustomer: (state) => state.role === 'CUSTOMER',
    isExecutor: (state) => state.role === 'EXECUTOR',
  },
  actions: {
    login(token: string, role: string, phone: string) {
      this.token = token
      this.role = role
      this.phone = phone
      setCookie('token', token, 1)
      setCookie('role', role, 1)
      setCookie('phone', phone, 1)
    },
    logout() {
      this.token = ''
      this.role = ''
      this.phone = ''
      document.cookie = 'token=; Max-Age=0; path=/;'
      document.cookie = 'role=; Max-Age=0; path=/;'
      document.cookie = 'phone=; Max-Age=0; path=/;'
    },
  },
})
