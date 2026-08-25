import { defineStore } from 'pinia'
import api, { getCookie, clearSession, storeSession } from '../services/api'

// CurrentUser mirrors the payload of GET /auth/me. It is the single source of
// truth for the signed-in user across every screen: pages must not keep their
// own copy of the balance, or they end up showing values of different ages.
export interface CurrentUser {
  id: string
  phone: string
  email: string
  role: string
  status: string
  balance: number
  first_name: string
  last_name: string
  patronymic: string
  birth_date: string
  age: number
  is_verified: boolean
}

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
      // null means "not loaded yet" and is deliberately distinct from a zero
      // balance, so the UI can show a placeholder instead of a wrong 0.
      user: null as CurrentUser | null,
      userLoading: false,
      userError: '',
    }
  },
  getters: {
    isAuthenticated: (state) => !!state.token,
    balance: (state) => state.user?.balance ?? null,
    fullName: (state) => {
      if (!state.user) return ''
      return [state.user.last_name, state.user.first_name, state.user.patronymic]
        .filter((part) => part && part.trim())
        .join(' ')
    },
    isAdmin: (state) => state.role === 'ADMIN',
    isCustomer: (state) => state.role === 'CUSTOMER',
    isExecutor: (state) => state.role === 'EXECUTOR',
  },
  actions: {
    login(token: string, role: string, phone: string, userID: string, refreshToken?: string) {
      this.token = token
      this.userID = userID
      this.role = role
      this.phone = phone
      this.user = null
      storeSession(token, refreshToken)
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
      this.user = null
      clearSession()
    },

    /**
     * Loads the signed-in user. Every screen that shows the balance calls this
     * instead of fetching /auth/me on its own, so all of them display the same
     * number at the same time.
     */
    async fetchMe(): Promise<CurrentUser | null> {
      if (!this.token) return null
      this.userLoading = true
      try {
        const res = await api.get('/auth/me')
        this.user = res.data as CurrentUser
        this.role = this.user.role || this.role
        this.phone = this.user.phone || this.phone
        this.userError = ''
        return this.user
      } catch (err: any) {
        // The previous value is kept: a failed refresh must not blank out a
        // balance that was correct a moment ago.
        this.userError = err?.response?.data || 'Не удалось обновить профиль'
        return this.user
      } finally {
        this.userLoading = false
      }
    },
    setCurrency(currency: string) {
      this.currency = currency || 'RUB'
    },
  },
})
