import axios from 'axios'
import { Capacitor } from '@capacitor/core'
import { debugConsoleEnabled, pushLog, updateLog, snippet } from './debugLog'

function resolveApiUrl(): string {
  const isNative = Capacitor.isNativePlatform()
  if (isNative) {
    // У нативных сборок нет источника, к которому можно откатиться, поэтому URL
    // обязан прийти из окружения сборки (.env.android задаёт VITE_API_URL).
    // Зашитого умолчания намеренно нет: неверное указало бы приложение на хост,
    // который мы не обслуживаем, и падало бы так, будто бэкенд лежит.
    const url = (import.meta.env.VITE_MOBILE_API_URL as string)
             || (import.meta.env.VITE_API_URL as string)
    if (!url) {
      console.error(
        'API URL is not configured: set VITE_MOBILE_API_URL or VITE_API_URL for the native build.',
      )
      return ''
    }
    return url
  }
  // Веб-сборки всегда используют относительные URL (baseURL = ''). Браузер
  // разрешает их относительно текущего источника — того хоста/порта/протокола,
  // который открыл пользователь. Это делает VITE_API_URL ненужным для веба и
  // избавляет от расхождений, когда внешний URL отличается от порта Docker (8443 → 443, CDN и т. п.).
  return ''
}

export const apiUrl = resolveApiUrl()

export const isDebug = import.meta.env.VITE_DEBUG === 'true'

// Ритм опроса в установившемся режиме. Держится грубым (30 с), потому что он
// работает непрерывно у каждого вошедшего клиента и каждый опрос проходит через
// middleware аутентификации; доставка чата — реалтайм по WebSocket, поэтому
// заказам и непрочитанному тесный цикл не нужен. Переопределяется через VITE_POLL_INTERVAL_SEC.
export const pollIntervalMs = (Number(import.meta.env.VITE_POLL_INTERVAL_SEC) || 30) * 1000

export function formatApiError(err: any, fallbackMessage: string): string {
  const data = err.response?.data
  const serverText = (typeof data === 'string' ? data : data?.error || data?.message || '').trim()

  if (isDebug) {
    const baseURL = err.config?.baseURL || ''
    const url = err.config?.url || ''
    const fullURL = url.startsWith('http') ? url : `${baseURL}${url}`
    const status = err.response?.status ? `HTTP ${err.response.status}` : 'no response'
    const errorText = serverText || err.message || 'Unknown error'
    return `${errorText}\n\n[Debug Info]\nURL: ${fullURL || 'unknown'}\nStatus: ${status}`
  }

  return serverText || fallbackMessage
}

const api = axios.create({
  baseURL: apiUrl,
})

// Добавляем префикс /api к каждому относительному URL запроса, чтобы маршруты
// бэкенда (смонтированные под /api) и маршруты SPA (которые nginx отдаёт из /)
// никогда не сталкивались. Абсолютные URL (http://...) и уже префиксованные пути пропускаются.
api.interceptors.request.use((config) => {
  const url = config.url || ''
  if (url && !url.startsWith('/api') && !url.startsWith('http') && !url.startsWith('ws')) {
    config.url = '/api' + (url.startsWith('/') ? url : '/' + url)
  }
  return config
})

// Строим URL WebSocket для эндпоинта чата на основе активного базового URL API.
// Схема следует за источником API (https -> wss, http -> ws) и принудительно
// становится защищённой wss://, когда приложение работает в защищённом
// контексте: нативное приложение (androidScheme: 'https') и любая https-страница.
// Незащищённый ws://, открытый из защищённого источника, — смешанное содержимое:
// WebView его блокирует или молча выбрасывает, из-за чего на мобильных пропадали
// отправки в чат. Префикс /api совпадает с монтированием маршрутов бэкенда, поэтому пути не сталкиваются.
export function buildChatWebSocketUrl(orderId: string): string {
  // Токен читается здесь, а не передаётся снаружи. Вызывающие раньше отдавали
  // authStore.token, который пишется при входе и больше никогда: после первого
  // тихого обновления он держит истёкший access-токен, и сокет больше не мог
  // аутентифицироваться. getAuthToken всегда возвращает актуальный.
  const token = getAuthToken()

  // Для веб-сборок apiUrl может быть пустым (режим относительных URL). Выводим
  // источник WS из window.location, чтобы протокол/хост/порт всегда совпадали со
  // страницей, которую действительно открыл пользователь.
  let base = apiUrl
  if (!base && typeof window !== 'undefined') {
    base = window.location.origin
  }
  let wsBase = base.replace(/^http/, 'ws').replace(/\/$/, '')

  // Никогда не открываем незащищённый ws:// из защищённого контекста. Остаться на
  // ws:// позволено только локальному dev-источнику на обычном http.
  if (wsBase.startsWith('ws://')) {
    const secureContext =
      Capacitor.isNativePlatform() ||
      (typeof window !== 'undefined' && window.location.protocol === 'https:')
    if (secureContext) {
      wsBase = 'wss://' + wsBase.slice('ws://'.length)
    }
  }

  return `${wsBase}/api/chats/${orderId}/ws?token=${encodeURIComponent(token)}`
}

// Превращает относительный путь к файлу (например, /uploads/chat/...) в полный доступный URL.
// Вложения больше не публичны: бэкенд проверяет, что вызывающий участвует в
// переписке, которой принадлежит файл. Тег <img> не умеет слать заголовок
// Authorization, поэтому токен едет параметром запроса, который бэкенд
// переносит в заголовок и убирает до того, как что-либо будет залогировано.
export function resolveFileUrl(path?: string): string {
  if (!path) return ''
  if (path.startsWith('http://') || path.startsWith('https://')) return path
  const cleanPath = path.startsWith('/') ? path : '/' + path

  const token = getAuthToken()
  const withAuth = (url: string) =>
    token ? `${url}${url.includes('?') ? '&' : '?'}token=${encodeURIComponent(token)}` : url

  if (!Capacitor.isNativePlatform()) {
    return withAuth(cleanPath)
  }
  const base = apiUrl.replace(/\/$/, '')
  return withAuth(`${base}${cleanPath}`)
}

// Помощник для получения cookie по имени
export function getCookie(name: string): string {
  const value = `; ${document.cookie}`
  const parts = value.split(`; ${name}=`)
  if (parts.length === 2) {
    return parts.pop()?.split(';').shift() || ''
  }
  return ''
}

// Помощник для получения токена авторизации. localStorage используется как
// основной источник, потому что мобильные WebView не читают cookie источника API.
function getAuthToken(): string {
  try {
    return localStorage.getItem('token') || getCookie('token') || ''
  } catch {
    return getCookie('token') || ''
  }
}

// Подставляем JWT-токен в каждый запрос к API
api.interceptors.request.use((config) => {
  const token = getAuthToken()
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// Отладочное логирование запросов. Ничего не пишет, пока не включён режим
// отладки, поэтому обычные пользователи за это не платят. Префикс /api и
// заголовок авторизации уже применены выше, поэтому логируется то, что уходит в сеть.
api.interceptors.request.use((config) => {
  if (debugConsoleEnabled.value) {
    const base = config.baseURL || ''
    const url = config.url || ''
    const fullURL = url.startsWith('http') ? url : `${base}${url}`
    const id = pushLog({
      ts: Date.now(),
      method: (config.method || 'get').toUpperCase(),
      url: fullURL,
    })
    ;(config as any).__debugId = id
    ;(config as any).__debugStart =
      typeof performance !== 'undefined' ? performance.now() : Date.now()
  }
  return config
})

function logOutcome(config: any, status: number | undefined, ok: boolean, body: unknown, errText?: string) {
  if (!config || config.__debugId == null) return
  const start = config.__debugStart
  const now = typeof performance !== 'undefined' ? performance.now() : Date.now()
  updateLog(config.__debugId, {
    status,
    ok,
    durationMs: typeof start === 'number' ? Math.round(now - start) : undefined,
    // Порог больше умолчания: тело отрисовывается, только когда строку
    // развернули, поэтому его сохранение в большем объёме делает детали полезными.
    responseSnippet: snippet(body, 4000),
    error: errText,
  })
}

api.interceptors.response.use(
  (response) => {
    logOutcome(response.config, response.status, true, response.data)
    return response
  },
  (error) => {
    logOutcome(
      error.config,
      error.response?.status,
      false,
      error.response?.data,
      error.message,
    )
    return Promise.reject(error)
  },
)

// Работа с сессией.
//
// Access-токен короткоживущий, поэтому 401 — нормальный конец его жизни, а не
// повод выбрасывать пользователя. На первый 401 клиент обменивает свой
// refresh-токен на новую пару и повторяет исходный запрос. Сессия по-настоящему
// закончена, только когда падает само обновление.

const REFRESH_TOKEN_KEY = 'refreshToken'

export function getRefreshToken(): string {
  try {
    return localStorage.getItem(REFRESH_TOKEN_KEY) || ''
  } catch {
    return ''
  }
}

let proactiveTimer: any = null

function scheduleProactiveRefresh(token: string) {
  if (proactiveTimer) {
    clearTimeout(proactiveTimer)
    proactiveTimer = null
  }
  try {
    const base64Url = token.split('.')[1]
    if (!base64Url) return
    const base64 = base64Url.replace(/-/g, '+').replace(/_/g, '/')
    const jsonPayload = decodeURIComponent(
      window.atob(base64)
        .split('')
        .map((c) => '%' + ('00' + c.charCodeAt(0).toString(16)).slice(-2))
        .join('')
    )
    const claims = JSON.parse(jsonPayload)
    if (typeof claims.exp === 'number') {
      const expiresMs = claims.exp * 1000
      const nowMs = Date.now()
      // Обновляем за 2 минуты до истечения (или на половине срока, если он очень короткий)
      const refreshInMs = Math.max(10000, expiresMs - nowMs - 2 * 60 * 1000)
      proactiveTimer = setTimeout(async () => {
        try {
          if (getRefreshToken()) {
            await refreshSession()
          }
        } catch (e) {
          console.warn('[auth] proactive refresh failed, will retry on demand:', e)
        }
      }, refreshInMs)
    }
  } catch {
    // игнорируем ошибки разбора
  }
}

export function storeSession(token: string, refreshToken?: string) {
  try {
    localStorage.setItem('token', token)
    if (refreshToken) {
      localStorage.setItem(REFRESH_TOKEN_KEY, refreshToken)
    }
  } catch {
    // localStorage может быть недоступен в некоторых окружениях
  }
  setSessionCookie('token', token)
  if (token) {
    scheduleProactiveRefresh(token)
  }
}

function setSessionCookie(name: string, value: string) {
  const date = new Date()
  date.setTime(date.getTime() + 24 * 60 * 60 * 1000)
  document.cookie = `${name}=${value}; expires=${date.toUTCString()}; path=/; SameSite=Lax`
}

export function clearSession() {
  if (proactiveTimer) {
    clearTimeout(proactiveTimer)
    proactiveTimer = null
  }
  for (const name of ['token', 'userID', 'role', 'phone']) {
    document.cookie = `${name}=; Max-Age=0; path=/;`
  }
  try {
    for (const key of ['token', 'userID', 'role', 'phone', REFRESH_TOKEN_KEY]) {
      localStorage.removeItem(key)
    }
  } catch {
    // localStorage может быть недоступен в некоторых окружениях
  }
}

function redirectToLogin() {
  if (Capacitor.isNativePlatform()) {
    if (window.location.hash !== '#/login') {
      window.location.hash = '#/login'
    }
  } else if (window.location.pathname !== '/login') {
    window.location.href = '/login'
  }
}

// Одно выполняющееся обновление, общее для всех запросов, получивших 401
// одновременно. Без него экран, стреляющий пятью параллельными запросами, послал
// бы пять обновлений, и ротация выдала бы четыре из них за повтор — а на это
// бэкенд отвечает завершением всех сессий.
let refreshInFlight: Promise<string> | null = null

export async function refreshSession(): Promise<string> {
  const refreshToken = getRefreshToken()
  if (!refreshToken) {
    throw new Error('no refresh token')
  }

  const baseUrl = (api.defaults.baseURL || '').replace(/\/$/, '')
  const refreshUrl = baseUrl.endsWith('/api') ? `${baseUrl}/auth/refresh` : `${baseUrl}/api/auth/refresh`

  // Голый вызов axios: он не должен идти через перехватчик ниже, иначе падающее
  // обновление пыталось бы обновить само себя.
  const res = await axios.post(
    refreshUrl,
    { refresh_token: refreshToken },
    { headers: { 'Content-Type': 'application/json' } }
  )

  const token: string = res.data?.token
  if (!token) {
    throw new Error('refresh response carried no token')
  }
  storeSession(token, res.data?.refresh_token)
  return token
}

api.interceptors.response.use(
  (response) => response,
  async (error) => {
    const original = error.config
    const status = error.response?.status

    if (status !== 401 || !original || original._retriedAfterRefresh) {
      if (status === 401) {
        clearSession()
        redirectToLogin()
      }
      return Promise.reject(error)
    }

    original._retriedAfterRefresh = true

    try {
      if (!refreshInFlight) {
        refreshInFlight = refreshSession().finally(() => {
          refreshInFlight = null
        })
      }
      const token = await refreshInFlight
      original.headers = original.headers || {}
      original.headers.Authorization = `Bearer ${token}`
      return api(original)
    } catch {
      clearSession()
      redirectToLogin()
      return Promise.reject(error)
    }
  }
)

export default api
