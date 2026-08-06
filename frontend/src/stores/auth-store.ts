import { defineStore } from 'pinia'
import { getCookie } from '../services/api'

function parseJwtSub(token: string): string {
  try {
    const base64Url = token.split('.')[1]
    const base64 = base64Url.replace(/-/g, '+').replace(/_/g, '/')
    const jsonPayload = decodeURIComponent(
      window.atob(base64)
        .split('')
        .map((c) => '%' + ('00' + c.charCodeAt(0).toString(16)).slice(-2))
        .join('')
    )
    const claims = JSON.parse(jsonPayload)
    return claims.sub || ''
  } catch {
    return ''
  }
}

// Helper to save a cookie
function setCookie(name: string, value: string, days = 1) {
  const date = new Date()
  date.setTime(date.getTime() + (days * 24 * 60 * 60 * 1000))
  const expires = `; expires=${date.toUTCString()}`
  document.cookie = `${name}=${value}${expires}; path=/; SameSite=Lax`
}

export const useAuthStore = defineStore('auth', {
  state: () => {
    const token = getCookie('token') || ''
    return {
      token,
      userID: getCookie('userID') || parseJwtSub(token),
      role: getCookie('role') || '',
      phone: getCookie('phone') || '',
      currency: 'RUB',
    }
  },
  getters: {
    isAuthenticated: (state) => !!state.token,
    isAdmin: (state) => state.role === 'ADMIN',
    isCustomer: (state) => state.role === 'CUSTOMER',
    isExecutor: (state) => state.role === 'EXECUTOR',
  },
  actions: {
    login(token: string, role: string, phone: string, userID: string) {
      this.token = token
      this.userID = userID
      this.role = role
      this.phone = phone
      setCookie('token', token, 1)
      setCookie('userID', userID, 1)
      setCookie('role', role, 1)
      setCookie('phone', phone, 1)
    },
    logout() {
      this.token = ''
      this.userID = ''
      this.role = ''
      this.phone = ''
      this.currency = 'RUB'
      document.cookie = 'token=; Max-Age=0; path=/;'
      document.cookie = 'userID=; Max-Age=0; path=/;'
      document.cookie = 'role=; Max-Age=0; path=/;'
      document.cookie = 'phone=; Max-Age=0; path=/;'
    },
    setCurrency(currency: string) {
      this.currency = currency || 'RUB'
    },
  },
})
