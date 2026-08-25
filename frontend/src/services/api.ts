import axios from 'axios'
import { Capacitor } from '@capacitor/core'

function resolveApiUrl(): string {
  const isNative = Capacitor.isNativePlatform()
  if (isNative) {
    return (import.meta.env.VITE_MOBILE_API_URL as string)
          || (import.meta.env.VITE_API_URL as string)
          || 'http://94.103.9.172:8089'  }
  // Web builds always use relative URLs (baseURL = ''). The browser resolves
  // them against the current origin — whatever host/port/proto the user opened.
  // This makes VITE_API_URL unnecessary for web and avoids mismatches when the
  // external URL differs from the Docker-mapped port (8443 → 443, CDN, etc.).
  return ''
}

export const apiUrl = resolveApiUrl()

export const isDebug = import.meta.env.VITE_DEBUG === 'true'

export const pollIntervalMs = (Number(import.meta.env.VITE_POLL_INTERVAL_SEC) || 15) * 1000

export function formatApiError(err: any, fallbackMessage: string): string {
  if (isDebug) {
    const baseURL = err.config?.baseURL || ''
    const url = err.config?.url || ''
    const fullURL = url.startsWith('http') ? url : `${baseURL}${url}`
    const status = err.response?.status ? `HTTP ${err.response.status}` : 'no response'
    const errorText = err.message || 'Unknown error'
    return `Request failed\nURL: ${fullURL || 'unknown'}\nStatus: ${status}\nError: ${errorText}`
  }

  if (err.response && err.response.data) {
    return typeof err.response.data === 'string' ? err.response.data : fallbackMessage
  }
  return fallbackMessage
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
// Native apps use plain ws:// against the mobile HTTP port, while the web uses
// wss:// against the HTTPS port. The /api prefix matches the backend route
// mounting so SPA and API paths never collide.
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
  const wsBase = base.replace(/^http/, 'ws').replace(/\/$/, '')
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
}

function setSessionCookie(name: string, value: string) {
  const date = new Date()
  date.setTime(date.getTime() + 24 * 60 * 60 * 1000)
  document.cookie = `${name}=${value}; expires=${date.toUTCString()}; path=/; SameSite=Lax`
}

export function clearSession() {
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

async function refreshSession(): Promise<string> {
  const refreshToken = getRefreshToken()
  if (!refreshToken) {
    throw new Error('no refresh token')
  }

  // A bare axios call: this must not go through the interceptor below, or a
  // failing refresh would try to refresh itself.
  const res = await axios.post(
    `${api.defaults.baseURL || ''}/auth/refresh`,
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
