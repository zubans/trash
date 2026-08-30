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
  roles?: string[]
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
    const role = getStoredItem('role') || getCookie('role') || ''
    let roles: string[] = []
    try {
      const raw = getStoredItem('roles')
      if (raw) roles = JSON.parse(raw)
    } catch {
      roles = []
    }
    if (roles.length === 0 && role) roles = [role]
    return {
      token,
      userID: getStoredItem('userID') || getCookie('userID') || parseJwtSub(token),
      role,
      // Every role the user holds (multi-role). Permissions key off this set.
      roles,
      // The role whose dashboard is currently shown; the user switches it in the UI.
      activeRole: getStoredItem('activeRole') || role,
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
    // The effective role set: the multi-role list when loaded, else the single
    // primary role. Permissions and menus key off membership here.
    roleSet: (state): string[] => (state.roles.length ? state.roles : state.role ? [state.role] : []),
    hasRole(): (role: string) => boolean {
      const set = this.roleSet
      return (role: string) => set.includes(role)
    },
    isAdmin(): boolean {
      return this.roleSet.includes('ADMIN')
    },
    isCustomer(): boolean {
      return this.roleSet.includes('CUSTOMER')
    },
    isExecutor(): boolean {
      return this.roleSet.includes('EXECUTOR')
    },
    isModerator(): boolean {
      return this.roleSet.includes('MODERATOR')
    },
    // Roles the user can switch the UI between (MODERATOR shares the executor
    // dashboard, so it is not offered as a separate switch target).
    switchableRoles(): string[] {
      return this.roleSet.filter((r) => r === 'CUSTOMER' || r === 'EXECUTOR' || r === 'ADMIN')
    },
  },
  actions: {
    login(token: string, role: string, phone: string, userID: string, refreshToken?: string) {
      this.token = token
      this.userID = userID
      this.role = role
      // The full set arrives from fetchMe; seed it with the primary role so the
      // UI is coherent immediately after login.
      this.roles = role ? [role] : []
      this.activeRole = role
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
      setStoredItem('roles', JSON.stringify(this.roles))
      setStoredItem('activeRole', this.activeRole)
      setStoredItem('phone', phone)
    },
    logout() {
      this.token = ''
      this.userID = ''
      this.role = ''
      this.roles = []
      this.activeRole = ''
      this.phone = ''
      this.currency = 'RUB'
      this.user = null
      setStoredItem('roles', '')
      setStoredItem('activeRole', '')
      clearSession()
    },
    // Switch which role's dashboard the UI shows. Callers navigate afterwards.
    setActiveRole(role: string) {
      if (!this.roleSet.includes(role)) return
      this.activeRole = role
      setStoredItem('activeRole', role)
      setCookie('role', role, 1)
      setStoredItem('role', role)
      this.role = role
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
        // Adopt the authoritative role set. Keep the active role if it is still
        // held, otherwise fall back to the primary role.
        const loaded = this.user.roles && this.user.roles.length ? this.user.roles : this.role ? [this.role] : []
        this.roles = loaded
        setStoredItem('roles', JSON.stringify(loaded))
        if (!loaded.includes(this.activeRole)) {
          this.activeRole = this.role
          setStoredItem('activeRole', this.activeRole)
        }
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
