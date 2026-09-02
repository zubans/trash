import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import axios from 'axios'
import api, {
  clearSession,
  storeSession,
  getRefreshToken,
  refreshSession,
  setSessionExpiredHandler,
} from './api'

// Access-токен с заданным сроком жизни. Клиент читает exp сам, поэтому подпись
// в этих тестах не важна — важен только разбираемый payload.
function jwt(expiresInSec: number): string {
  const payload = btoa(JSON.stringify({ sub: 'user-1', exp: Math.floor(Date.now() / 1000) + expiresInSec }))
  return `header.${payload}.signature`
}

type Reply = { status: number; data?: any }

// Заглушка транспорта: и у инстанса, и у голого axios (им идёт обновление)
// подменяется адаптер, поэтому перехватчики работают по-настоящему.
function stubTransport(handler: (url: string, body: any) => Reply) {
  const adapter = async (config: any) => {
    const url = config.url || ''
    const body = config.data ? JSON.parse(config.data) : undefined
    const reply = handler(url, body)
    const response = {
      data: reply.data,
      status: reply.status,
      statusText: '',
      headers: {},
      config,
    }
    if (reply.status >= 400) {
      throw new axios.AxiosError('request failed', String(reply.status), config, {}, response as any)
    }
    return response
  }
  api.defaults.adapter = adapter
  axios.defaults.adapter = adapter
}

describe('обновление сессии', () => {
  beforeEach(() => {
    localStorage.clear()
    setSessionExpiredHandler(null)
    vi.useFakeTimers({ shouldAdvanceTime: true })
  })

  afterEach(() => {
    vi.useRealTimers()
    api.defaults.adapter = undefined
    axios.defaults.adapter = undefined
    setSessionExpiredHandler(null)
    clearSession()
  })

  it('обменивает токен один раз на несколько одновременных 401', async () => {
    const expired = jwt(-60)
    const fresh = jwt(900)
    storeSession(expired, 'refresh-1')

    let refreshCalls = 0
    const seenAuth: string[] = []

    // Обмен идёт голым axios, поэтому инстанс его видеть не должен, а
    // истёкший токен обязан получать 401 — как на живом бэкенде.
    axios.defaults.adapter = (async (config: any) => {
      refreshCalls++
      expect(JSON.parse(config.data).refresh_token).toBe('refresh-1')
      return {
        data: { token: fresh, refresh_token: 'refresh-2' },
        status: 200,
        statusText: '',
        headers: {},
        config,
      }
    }) as any

    api.defaults.adapter = (async (config: any) => {
      const auth = String(config.headers?.Authorization || '')
      seenAuth.push(auth)
      const response = { data: { ok: true }, status: 200, statusText: '', headers: {}, config }
      if (auth.includes(expired)) {
        throw new axios.AxiosError('unauthorized', '401', config, {}, {
          ...response,
          status: 401,
          data: undefined,
        } as any)
      }
      return response
    }) as any

    const results = await Promise.all([api.get('/orders'), api.get('/chats'), api.get('/auth/me')])

    // Ротация разрешает обменять токен один раз: три одновременных 401 обязаны
    // сойтись в один обмен, иначе бэкенд считает лишние попытки утечкой.
    expect(refreshCalls).toBe(1)
    expect(results.every((r) => r.status === 200)).toBe(true)
    expect(localStorage.getItem('token')).toBe(fresh)
    expect(getRefreshToken()).toBe('refresh-2')
    expect(seenAuth.filter((h) => h.includes(expired))).toHaveLength(3)
    expect(seenAuth.filter((h) => h.includes(fresh))).toHaveLength(3)
  })

  it('не завершает сессию, когда обновление не удалось по временной причине', async () => {
    storeSession(jwt(-60), 'refresh-1')
    const onExpired = vi.fn()
    setSessionExpiredHandler(onExpired)

    stubTransport((url) => {
      if (url.endsWith('/auth/refresh')) return { status: 429 }
      return { status: 401 }
    })

    await expect(api.get('/orders')).rejects.toBeTruthy()

    // 429 от общего адреса оператора и обрыв связи — не конец сессии:
    // учётные данные обновления обязаны пережить их.
    expect(onExpired).not.toHaveBeenCalled()
    expect(getRefreshToken()).toBe('refresh-1')
  })

  it('завершает сессию, только когда сервер отверг сам refresh-токен', async () => {
    storeSession(jwt(-60), 'refresh-1')
    const onExpired = vi.fn()
    setSessionExpiredHandler(onExpired)

    stubTransport(() => ({ status: 401 }))

    await expect(api.get('/orders')).rejects.toBeTruthy()

    expect(onExpired).toHaveBeenCalledTimes(1)
    expect(getRefreshToken()).toBe('')
    expect(localStorage.getItem('token')).toBe(null)
  })

  it('принимает пару, которую записал другой контекст, вместо выхода', async () => {
    storeSession(jwt(-60), 'refresh-1')
    const onExpired = vi.fn()
    setSessionExpiredHandler(onExpired)
    const rotated = jwt(900)

    stubTransport((url) => {
      if (url.endsWith('/auth/refresh')) {
        // Другая вкладка успела обменять тот же токен, пока запрос был в пути.
        storeSession(rotated, 'refresh-2')
        return { status: 401 }
      }
      return { status: 401 }
    })

    await expect(refreshSession()).resolves.toBe(rotated)
    expect(onExpired).not.toHaveBeenCalled()
    expect(getRefreshToken()).toBe('refresh-2')
  })
})
