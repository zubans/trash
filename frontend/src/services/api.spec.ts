import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import axios from 'axios'
import api, {
  formatApiError,
  isStaleStateError,
  statusDetail,
  clearSession,
  storeSession,
  getRefreshToken,
  refreshSession,
  ensureFreshSession,
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

// Сокет чата предъявляет access-токен один раз, в query при рукопожатии:
// просроченный отвергается ещё до апгрейда, и обычного пути через 401 у него
// нет. Поэтому перед каждой попыткой соединения токен доводится до годного.
describe('подготовка сессии к рукопожатию', () => {
  beforeEach(() => {
    localStorage.clear()
    setSessionExpiredHandler(null)
  })

  afterEach(() => {
    api.defaults.adapter = undefined
    axios.defaults.adapter = undefined
    setSessionExpiredHandler(null)
    clearSession()
  })

  it('не трогает живой токен', async () => {
    storeSession(jwt(900), 'refresh-1')
    stubTransport(() => {
      throw new Error('обмена быть не должно')
    })

    await expect(ensureFreshSession()).resolves.toBe('ok')
    expect(getRefreshToken()).toBe('refresh-1')
  })

  it('обменивает истекающий токен до попытки соединения', async () => {
    const fresh = jwt(900)
    // Токен ещё жив, но кончится раньше, чем соединение успеет пригодиться.
    storeSession(jwt(30), 'refresh-1')
    stubTransport((url) => {
      if (url.endsWith('/auth/refresh')) return { status: 200, data: { token: fresh, refresh_token: 'refresh-2' } }
      return { status: 404 }
    })

    await expect(ensureFreshSession()).resolves.toBe('ok')
    expect(localStorage.getItem('token')).toBe(fresh)
  })

  it('называет временный сбой обмена stale и оставляет сессию на месте', async () => {
    storeSession(jwt(-60), 'refresh-1')
    const onExpired = vi.fn()
    setSessionExpiredHandler(onExpired)
    stubTransport(() => ({ status: 503 }))

    // Попытку стоит повторить позже — это сеть, а не конец сессии.
    await expect(ensureFreshSession()).resolves.toBe('stale')
    expect(onExpired).not.toHaveBeenCalled()
    expect(getRefreshToken()).toBe('refresh-1')
  })

  it('называет отвергнутый refresh-токен ended и завершает сессию', async () => {
    storeSession(jwt(-60), 'refresh-1')
    const onExpired = vi.fn()
    setSessionExpiredHandler(onExpired)
    stubTransport(() => ({ status: 401 }))

    await expect(ensureFreshSession()).resolves.toBe('ended')
    expect(onExpired).toHaveBeenCalledTimes(1)
    expect(getRefreshToken()).toBe('')
  })

  it('называет отсутствие сессии ended, никуда не ходя', async () => {
    clearSession()
    stubTransport(() => {
      throw new Error('обмена быть не должно')
    })

    await expect(ensureFreshSession()).resolves.toBe('ended')
  })
})

// Ответ с ошибкой в той форме, в какой его отдаёт axios.
const httpError = (status: number, data: unknown) => ({ response: { status, data } })

describe('текст ошибки для человека (formatApiError)', () => {
  it('показывает русский текст сервера на 404/403/409/422/503 как есть', () => {
    expect(formatApiError(httpError(404, 'заказ не найден'), 'Ошибка')).toBe('заказ не найден')
    expect(formatApiError(httpError(403, 'доступ запрещён\n'), 'Ошибка')).toBe('доступ запрещён')
    expect(formatApiError(httpError(409, 'заказ уже взят другим исполнителем'), 'Ошибка')).toBe(
      'заказ уже взят другим исполнителем',
    )
    expect(formatApiError(httpError(422, 'недостаточно средств'), 'Ошибка')).toBe('недостаточно средств')
    expect(formatApiError(httpError(503, 'услуга временно недоступна'), 'Ошибка')).toBe(
      'услуга временно недоступна',
    )
  })

  it('не показывает «internal error» 500, а дописывает к своему тексту понятную причину', () => {
    const text = formatApiError(httpError(500, 'internal error\n'), 'Ошибка отмены')
    expect(text).toBe('Ошибка отмены: сбой на сервере, попробуйте позже')
    expect(text).not.toContain('internal')
  })

  it('без своего текста у вызывающего отдаёт пояснение по коду с заглавной буквы', () => {
    expect(formatApiError(httpError(500, 'internal error'))).toBe('Сбой на сервере, попробуйте позже')
    expect(formatApiError(httpError(409, ''))).toBe('Данные изменились, обновите страницу')
  })

  it('прячет HTML-страницы прокси и голый «Bad request»', () => {
    expect(formatApiError(httpError(502, '<html><body>Bad Gateway</body></html>'), 'Не сохранено')).toBe(
      'Не сохранено: сервер недоступен, попробуйте позже',
    )
    expect(formatApiError(httpError(400, 'Bad request\n'), 'Не сохранено')).toBe('Не сохранено: запрос отклонён')
    // Осмысленный текст 400 (каталог услуг) остаётся как был.
    expect(formatApiError(httpError(400, 'код услуги уже занят'), 'Не сохранено')).toBe('код услуги уже занят')
  })

  it('обрыв связи называет обрывом, а не ошибкой сервера', () => {
    expect(formatApiError(new Error('Network Error'), 'Не отправлено')).toBe('Не отправлено: нет связи с сервером')
  })

  it('у JSON-ошибок берёт message, а машинный код в error не показывает', () => {
    expect(
      formatApiError(httpError(409, { error: 'passport_required', message: 'Внесите паспорт заново' }), 'Ошибка'),
    ).toBe('Внесите паспорт заново')
    expect(formatApiError(httpError(403, { error: 'account_soft_banned' }), 'Ошибка')).toBe(
      'Ошибка: действие недоступно',
    )
    // Русский текст в error (подтверждение почты) показывается.
    expect(formatApiError(httpError(400, { error: 'Ссылка устарела' }), 'Ошибка')).toBe('Ссылка устарела')
  })
})

describe('пояснение по коду ответа (statusDetail)', () => {
  it('знает классы ошибок бэкенда и сводит незнакомые 5xx к сбою сервера', () => {
    expect(statusDetail(404)).toBe('не найдено')
    expect(statusDetail(409)).toBe('данные изменились, обновите страницу')
    expect(statusDetail(422)).toBe('проверьте введённые данные')
    expect(statusDetail(507)).toBe('сбой на сервере, попробуйте позже')
    expect(statusDetail(undefined)).toBe('нет связи с сервером')
    expect(statusDetail(418)).toBe('')
  })
})

describe('устаревшая карточка (isStaleStateError)', () => {
  it('404 и 409 требуют перечитать данные, прочие коды — нет', () => {
    expect(isStaleStateError(httpError(404, 'заказ не найден'))).toBe(true)
    expect(isStaleStateError(httpError(409, 'заказ уже взят другим исполнителем'))).toBe(true)
    expect(isStaleStateError(httpError(403, 'доступ запрещён'))).toBe(false)
    expect(isStaleStateError(httpError(422, 'недостаточно средств'))).toBe(false)
    expect(isStaleStateError(httpError(500, 'internal error'))).toBe(false)
    expect(isStaleStateError(new Error('Network Error'))).toBe(false)
  })
})
