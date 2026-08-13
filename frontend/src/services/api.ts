import axios from 'axios'
import { Capacitor } from '@capacitor/core'

function resolveApiUrl(): string {
  const isNative = Capacitor.isNativePlatform()
  if (isNative) {
    return (import.meta.env.VITE_MOBILE_API_URL as string) || 'https://94.103.9.172:8443'
  }
  const url = (import.meta.env.VITE_API_URL as string) || ''
  if (!url) {
    throw new Error('VITE_API_URL is not defined. Please check your .env file.')
  }
  return url
}

export const apiUrl = resolveApiUrl()

export const isDebug = import.meta.env.VITE_DEBUG === 'true'

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
// Native apps use plain ws:// or wss:// against the active port.
export function buildChatWebSocketUrl(orderId: string, token: string): string {
  const wsBase = apiUrl.replace(/^https/, 'wss').replace(/^http/, 'ws').replace(/\/$/, '')
  return `${wsBase}/api/chats/${orderId}/ws?token=${encodeURIComponent(token)}`
}

// Convert a relative file path (e.g. /uploads/chat/...) into a full accessible URL.
// On web apps, relative paths (/uploads/...) are returned as-is so nginx
// serves them over the active origin (HTTPS / port 8443 or 443).
// On Android native apps, they are resolved against HTTPS backend host.
export function resolveFileUrl(path?: string): string {
  if (!path) return ''
  if (path.startsWith('http://') || path.startsWith('https://')) return path
  const cleanPath = path.startsWith('/') ? path : '/' + path
  const isNative = Capacitor.isNativePlatform()

  if (isNative || !window.location.origin || window.location.protocol === 'file:') {
    const base = apiUrl.replace(/\/$/, '')
    return `${base}${cleanPath}`
  }

  // On standard web browsers (HTTPS / 8443 or nginx), clean relative path allows origin proxying
  return cleanPath
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

// Handle auto logout when session expires (401 Unauthorized only)
api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response && error.response.status === 401) {
      // Clear authentication cookies and localStorage
      document.cookie = 'token=; Max-Age=0; path=/;'
      document.cookie = 'userID=; Max-Age=0; path=/;'
      document.cookie = 'role=; Max-Age=0; path=/;'
      document.cookie = 'phone=; Max-Age=0; path=/;'
      try {
        localStorage.removeItem('token')
        localStorage.removeItem('userID')
        localStorage.removeItem('role')
        localStorage.removeItem('phone')
      } catch {
        // localStorage may be unavailable in some environments
      }

      if (window.location.pathname !== '/login') {
        window.location.href = '/login'
      }
    }
    return Promise.reject(error)
  }
)

export default api
