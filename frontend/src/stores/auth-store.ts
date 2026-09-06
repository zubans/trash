import { defineStore } from 'pinia'
import api, { getCookie, clearSession, storeSession } from '../services/api'

// CurrentUser повторяет полезную нагрузку GET /auth/me. Это единственный
// источник истины о вошедшем пользователе на всех экранах: страницы не должны
// держать собственную копию баланса, иначе они покажут значения разного возраста.
export interface CurrentUser {
  id: string
  phone: string
  email: string
  role: string
  roles?: string[]
  // Действующие права — объединение прав всех ролей пользователя. Меню и
  // кнопки строятся по ним, а не по названию роли: роли заводит администратор,
  // и зашивать их имена в интерфейс значит ломать его при каждой новой.
  permissions?: string[]
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

// Помощник для сохранения cookie
function setCookie(name: string, value: string, days = 1) {
  const date = new Date()
  date.setTime(date.getTime() + (days * 24 * 60 * 60 * 1000))
  const expires = `; expires=${date.toUTCString()}`
  document.cookie = `${name}=${value}${expires}; path=/; SameSite=Lax`
}

// Помощники для localStorage — основного хранилища авторизации в мобильном
// приложении, потому что мобильные WebView не видят cookie источника API.
function setStoredItem(name: string, value: string) {
  try {
    localStorage.setItem(name, value)
  } catch {
    // игнорируем окружения, где localStorage недоступен
  }
}

function getStoredItem(name: string): string {
  try {
    return localStorage.getItem(name) || ''
  } catch {
    return ''
  }
}


// readStoredPermissions поднимает сохранённые права. Испорченное значение
// читается как «прав нет»: меню тогда пусто до ответа /auth/me, что заметно, но
// безопасно.
function readStoredPermissions(): string[] {
  try {
    const raw = getStoredItem('permissions')
    if (!raw) return []
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

export const useAuthStore = defineStore('auth', {
  state: () => {
    // localStorage проверяется первым, чтобы мобильное приложение могло
    // восстановить сессию после перезапуска (cookie источника API в WebView не видны).
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
      // Все роли пользователя (мультироль). Права опираются на этот набор.
      roles,
      // Роль, чей дашборд сейчас показан; пользователь переключает её в интерфейсе.
      activeRole: getStoredItem('activeRole') || role,
      phone: getStoredItem('phone') || getCookie('phone') || '',
      // Права держатся и в localStorage: без этого первая отрисовка после
      // перезагрузки страницы шла бы с пустым меню и мигала бы, когда ответ
      // /auth/me возвращает те же права обратно.
      permissions: readStoredPermissions(),
      currency: 'RUB',
      // null означает «ещё не загружено» и намеренно отличается от нулевого
      // баланса, чтобы интерфейс показывал заглушку, а не неверный 0.
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
    // Действующий набор ролей: список мультиролей, когда он загружен, иначе одна
    // основная роль. Права и меню опираются на членство здесь.
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
    // can отвечает на вопрос «можно ли показывать этот пункт меню, эту кнопку».
    // Администратор проходит всё: он суперпользователь и на бэкенде тоже,
    // поэтому интерфейс не должен прятать от него раздел, до которого он дойдёт.
    can(): (permission: string) => boolean {
      const isAdmin = this.roleSet.includes('ADMIN')
      const granted = this.permissions
      return (permission: string) => isAdmin || granted.includes(permission)
    },
    // Есть ли у пользователя хоть одно право в разделах панели. Именно это, а не
    // роль ADMIN, открывает саму панель.
    hasAdminAccess(): boolean {
      return this.roleSet.includes('ADMIN') || this.permissions.length > 0
    },
    // Роли, между дашбордами которых пользователь может переключаться (MODERATOR
    // делит дашборд с исполнителем, поэтому отдельной целью переключения не служит).
    switchableRoles(): string[] {
      return this.roleSet.filter((r) => r === 'CUSTOMER' || r === 'EXECUTOR' || r === 'ADMIN')
    },
  },
  actions: {
    login(token: string, role: string, phone: string, userID: string, refreshToken?: string) {
      this.token = token
      this.userID = userID
      this.role = role
      // Полный набор приходит из fetchMe; засеваем его основной ролью, чтобы
      // интерфейс был связным сразу после входа.
      this.roles = role ? [role] : []
      this.activeRole = role
      this.phone = phone
      this.user = null
      // Права приходят из fetchMe; до него их нет, и старые от предыдущего
      // пользователя показывать нельзя.
      this.permissions = []
      setStoredItem('permissions', '')
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
      this.permissions = []
      setStoredItem('permissions', '')
      setStoredItem('roles', '')
      setStoredItem('activeRole', '')
      clearSession()
    },
    // Переключает, чей дашборд показывает интерфейс. Навигацию делают вызывающие.
    setActiveRole(role: string) {
      if (!this.roleSet.includes(role)) return
      this.activeRole = role
      setStoredItem('activeRole', role)
      setCookie('role', role, 1)
      setStoredItem('role', role)
      this.role = role
    },

    /**
     * Загружает вошедшего пользователя. Каждый экран, показывающий баланс,
     * вызывает это вместо собственного запроса /auth/me, поэтому все они
     * показывают одно и то же число в одно и то же время.
     */
    async fetchMe(): Promise<CurrentUser | null> {
      if (!this.token) return null
      this.userLoading = true
      try {
        const res = await api.get('/auth/me')
        this.user = res.data as CurrentUser
        this.role = this.user.role || this.role
        // Принимаем авторитетный набор ролей. Оставляем активную роль, если она всё
        // ещё есть, иначе откатываемся к основной роли.
        const loaded = this.user.roles && this.user.roles.length ? this.user.roles : this.role ? [this.role] : []
        this.roles = loaded
        setStoredItem('roles', JSON.stringify(loaded))
        const permissions = this.user.permissions || []
        this.permissions = permissions
        setStoredItem('permissions', JSON.stringify(permissions))
        if (!loaded.includes(this.activeRole)) {
          this.activeRole = this.role
          setStoredItem('activeRole', this.activeRole)
        }
        this.phone = this.user.phone || this.phone
        this.userError = ''
        return this.user
      } catch (err: any) {
        // Прежнее значение сохраняется: неудачное обновление не должно обнулять
        // баланс, который мгновение назад был верным.
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
