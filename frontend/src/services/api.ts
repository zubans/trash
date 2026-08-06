import axios from 'axios'

export const apiUrl = import.meta.env.VITE_API_URL
if (!apiUrl) {
  throw new Error('VITE_API_URL is not defined. Please check your .env file.')
}

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

// Handle auto logout when session expires
api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response && [401, 403].includes(error.response.status)) {
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
