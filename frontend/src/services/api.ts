import axios from 'axios'
import { Capacitor } from '@capacitor/core'
import { debugConsoleEnabled, pushLog, updateLog, snippet } from './debugLog'

function resolveApiUrl(): string {
  const isNative = Capacitor.isNativePlatform()
  if (isNative) {
    // Native builds have no origin to fall back on, so the URL must come from
    // the build env (.env.android sets VITE_API_URL). There is deliberately no
    // hardcoded default: a wrong one points the app at a host we do not serve
    // and fails as if the backend were down.
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
  // Web builds always use relative URLs (baseURL = ''). The browser resolves
  // them against the current origin — whatever host/port/proto the user opened.
  // This makes VITE_API_URL unnecessary for web and avoids mismatches when the
  // external URL differs from the Docker-mapped port (8443 → 443, CDN, etc.).
  return ''
}

export const apiUrl = resolveApiUrl()

export const isDebug = import.meta.env.VITE_DEBUG === 'true'

// Steady-state polling cadence. Kept coarse (30s) because it runs for every
// signed-in client continuously and each poll goes through the auth middleware;
// chat delivery is realtime over the WebSocket, so orders/unread don't need a
// tight loop. Overridable via VITE_POLL_INTERVAL_SEC.
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

// Prepend the /api prefix to every relative request URL so the backend routes
// (mounted under /api) and the SPA routes (served by nginx from /) never
// collide. Absolute URLs (http://...) and already-prefixed paths are skipped.
api.interceptors.request.use((config) => {
  const url = config.url || ''
  if (url && !url.startsWith('/api') && !url.startsWith('http') && !url.startsWith('ws')) {
    config.url = '/api' + (url.startsWith('/') ? url : '/' + url)
  }
  return config
})

// Build a WebSocket URL for the chat endpoint based on the active API base URL.
// The scheme follows the API origin (https -> wss, http -> ws) and is forced to
// the secure wss:// whenever the app runs in a secure context — the native app
// (androidScheme: 'https') and any https web page. An insecure ws:// opened from
// a secure origin is mixed content: the WebView blocks or silently drops it,
// which is why chat sends went missing on mobile. The /api prefix matches the
// backend route mounting so SPA and API paths never collide.
export function buildChatWebSocketUrl(orderId: string): string {
  // The token is read here rather than passed in. Callers used to hand over
  // authStore.token, which is written at login and never again: after the first
  // silent refresh it holds an expired access token, and the socket could no
  // longer authenticate. getAuthToken always returns the current one.
  const token = getAuthToken()

  // For web builds apiUrl may be empty (relative-URL mode). Derive the WS
  // origin from window.location so the protocol/host/port always match the
  // page the user actually opened.
  let base = apiUrl
  if (!base && typeof window !== 'undefined') {
    base = window.location.origin
  }
  let wsBase = base.replace(/^http/, 'ws').replace(/\/$/, '')

  // Never open an insecure ws:// from a secure context. Only a plain-http local
  // dev origin is allowed to stay on ws://.
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

// Convert a relative file path (e.g. /uploads/chat/...) into a full accessible URL.
// Attachments are no longer public: the backend checks that the caller takes
// part in the conversation the file belongs to. An <img> tag cannot send an
// Authorization header, so the token travels as a query parameter, which the
// backend moves into the header and strips before anything is logged.
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

// Helper to retrieve cookie by name
export function getCookie(name: string): string {
  const value = `; ${document.cookie}`
  const parts = value.split(`; ${name}=`)
  if (parts.length === 2) {
    return parts.pop()?.split(';').shift() || ''
  }
  return ''
}

// Helper to retrieve the auth token. localStorage is used as the primary
// source because mobile WebViews cannot read cookies set for the API origin.
function getAuthToken(): string {
  try {
    return localStorage.getItem('token') || getCookie('token') || ''
  } catch {
    return getCookie('token') || ''
  }
}

// Inject JWT token into every API request
api.interceptors.request.use((config) => {
  const token = getAuthToken()
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// Debug request logging. Records nothing unless debug mode is on, so ordinary
// users pay no cost. The /api prefix and auth header are already applied above,
// so what is logged is what actually goes on the wire.
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
    // A larger cap than the default: the body is only rendered when a row is
    // expanded, so keeping more of it makes the details actually useful.
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

// Session handling.
//
// The access token is short-lived, so a 401 is the normal end of its life, not
// a reason to throw the user out. On the first 401 the client exchanges its
// refresh token for a new pair and replays the original request. Only when the
// refresh itself fails is the session really over.

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
      // Refresh 2 minutes before expiration (or halfway if lifetime is very short)
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
    // ignore parse errors
  }
}

export function storeSession(token: string, refreshToken?: string) {
  try {
    localStorage.setItem('token', token)
    if (refreshToken) {
      localStorage.setItem(REFRESH_TOKEN_KEY, refreshToken)
    }
  } catch {
    // localStorage may be unavailable in some environments
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
    // localStorage may be unavailable in some environments
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

// A single in-flight refresh shared by every request that hit a 401 at the same
// time. Without it, a screen that fires five parallel requests would send five
// refreshes, and rotation would make four of them look like a replay — which
// the backend answers by ending every session.
let refreshInFlight: Promise<string> | null = null

export async function refreshSession(): Promise<string> {
  const refreshToken = getRefreshToken()
  if (!refreshToken) {
    throw new Error('no refresh token')
  }

  const baseUrl = (api.defaults.baseURL || '').replace(/\/$/, '')
  const refreshUrl = baseUrl.endsWith('/api') ? `${baseUrl}/auth/refresh` : `${baseUrl}/api/auth/refresh`

  // A bare axios call: this must not go through the interceptor below, or a
  // failing refresh would try to refresh itself.
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
