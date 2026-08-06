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

// Helpers for localStorage, which is the primary auth storage for the mobile
// app because mobile WebViews cannot access cookies set for the API origin.
function setStoredItem(name: string, value: string) {
  try {
    localStorage.setItem(name, value)
  } catch {
    // ignore environments where localStorage is unavailable
  }
}

function getStoredItem(name: string): string {
  try {
    return localStorage.getItem(name) || ''
  } catch {
    return ''
  }
}

function removeStoredItem(name: string) {
  try {
    localStorage.removeItem(name)
  } catch {
    // ignore
  }
}

export const useAuthStore = defineStore('auth', {
  state: () => {
    // localStorage is checked first so the mobile app can restore the session
    // after restart (cookies for the API origin are not visible in WebView).
    const token = getStoredItem('token') || getCookie('token') || ''
    return {
      token,
      userID: getStoredItem('userID') || getCookie('userID') || parseJwtSub(token),
      role: getStoredItem('role') || getCookie('role') || '',
      phone: getStoredItem('phone') || getCookie('phone') || '',
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
      setStoredItem('token', token)
      setStoredItem('userID', userID)
      setStoredItem('role', role)
      setStoredItem('phone', phone)
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
      removeStoredItem('token')
      removeStoredItem('userID')
      removeStoredItem('role')
      removeStoredItem('phone')
    },
    setCurrency(currency: string) {
      this.currency = currency || 'RUB'
    },
  },
})
