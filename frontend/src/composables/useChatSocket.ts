import { onUnmounted, ref } from 'vue'
import { buildChatWebSocketUrl, ensureFreshSession } from '../services/api'
import { logWsEvent } from '../services/debugLog'

// Сокет чата живёт ровно столько, сколько ему позволяет сеть под мобильным
// приложением: WebView в фоне усыпляет соединение, оператор рвёт сессию, а
// access-токен, предъявленный в query при рукопожатии, рано или поздно истекает.
// Раньше обработчик onclose только писал код в лог — чат тихо умирал до тех пор,
// пока пользователь не откроет экран заново. Здесь обрыв — рабочее состояние:
// соединение поднимается снова с растущей паузой и всегда со свежим URL, потому
// что buildChatWebSocketUrl каждый раз читает актуальный токен.
//
// Логика общая для дашбордов заказчика и исполнителя: два одинаковых сокета в
// двух файлах разъезжались, и чинить пришлось бы дважды.

const RECONNECT_BASE_MS = 1000
const RECONNECT_MAX_MS = 30 * 1000

export interface ChatSocketHandlers {
  // Вызывается на каждое открытие. reconnected = true означает, что соединение
  // поднялось после обрыва: всё присланное за время простоя прошло мимо сокета,
  // поэтому историю надо перечитать по REST.
  onOpen?: (info: { orderId: string; reconnected: boolean }) => void
  onMessage: (data: any, orderId: string) => void
}

export function useChatSocket(handlers: ChatSocketHandlers) {
  // Открыт ли сокет прямо сейчас — по нему вызывающие решают, есть ли смысл слать кадр.
  const isConnected = ref(false)

  let socket: WebSocket | null = null
  // Заказ, чей чат открыт пользователем. null — закрыт намеренно, возвращаться некуда.
  let orderId: string | null = null
  let attempt = 0
  let retryTimer: any = null
  let everOpened = false
  // Отсекает попытку, которая ждала обновления токена, пока чат уже закрыли или переоткрыли.
  let generation = 0
  let disposed = false

  const clearRetry = () => {
    if (retryTimer) {
      clearTimeout(retryTimer)
      retryTimer = null
    }
  }

  // Снимает обработчики до close(), чтобы закрытие по нашей инициативе не
  // выглядело обрывом и не запускало реконнект.
  const dropSocket = () => {
    if (!socket) return
    const dying = socket
    socket = null
    isConnected.value = false
    dying.onopen = null
    dying.onmessage = null
    dying.onerror = null
    dying.onclose = null
    try {
      dying.close(1000, 'client closed')
    } catch {
      // Сокет мог не дожить до CONNECTING — закрывать тогда уже нечего.
    }
  }

  const scheduleReconnect = () => {
    if (disposed || !orderId || retryTimer) return
    const backoff = Math.min(RECONNECT_BASE_MS * 2 ** attempt, RECONNECT_MAX_MS)
    // Разброс паузы: после обрыва на стороне сервера возвращаются сразу все
    // клиенты, и без него они возвращаются одной волной.
    const delay = Math.round(backoff * (0.5 + Math.random() * 0.5))
    attempt += 1
    logWsEvent(`reconnect in ${delay} ms (attempt ${attempt})`)
    retryTimer = setTimeout(() => {
      retryTimer = null
      void open()
    }, delay)
  }

  const open = async () => {
    const target = orderId
    if (disposed || !target) return
    if (socket && (socket.readyState === WebSocket.CONNECTING || socket.readyState === WebSocket.OPEN)) return

    const myGeneration = ++generation
    const session = await ensureFreshSession()
    // Пока ждали обновление токена, чат могли закрыть или переоткрыть на другом заказе.
    if (disposed || myGeneration !== generation || orderId !== target) return
    if (session === 'ended') {
      logWsEvent('reconnect stopped: сессия завершена', { ok: false, error: 'no session' })
      clearRetry()
      return
    }

    const url = buildChatWebSocketUrl(target)
    logWsEvent('connect ' + url.replace(/token=[^&]+/, 'token=…'))

    let next: WebSocket
    try {
      next = new WebSocket(url)
    } catch (e: any) {
      logWsEvent('connect THREW', { ok: false, error: String(e?.message || e) })
      scheduleReconnect()
      return
    }
    socket = next

    next.onopen = () => {
      if (socket !== next) return
      isConnected.value = true
      const reconnected = everOpened
      everOpened = true
      attempt = 0
      logWsEvent('open (readyState=' + next.readyState + ')', { ok: true })
      // Проба round-trip: если сервер отвечает «pong», исходящие кадры из этого
      // WebView до сервера доходят.
      send({ type: 'ping' })
      handlers.onOpen?.({ orderId: target, reconnected })
    }

    next.onerror = () => {
      logWsEvent('error', { ok: false, error: 'socket error event' })
    }

    next.onclose = (e) => {
      if (socket !== next) return
      socket = null
      isConnected.value = false
      logWsEvent('close code=' + e.code + (e.reason ? ' reason=' + e.reason : ''), {
        ok: e.code === 1000,
        error: e.code !== 1000 ? 'code ' + e.code : undefined,
      })
      // 1000 — «закрыто штатно и больше не нужно». Так закрываем мы сами и так
      // прощается сервер; возвращаться после этого некуда. Любой другой код —
      // обрыв: истёкший токен, уснувший WebView, упавшая сеть.
      if (e.code === 1000) return
      scheduleReconnect()
    }

    next.onmessage = (event) => {
      if (socket !== next) return
      let data: any
      try {
        data = JSON.parse(event.data)
      } catch (e) {
        console.error('WS message parse error:', e)
        return
      }
      if (data?.type === 'pong') {
        logWsEvent('recv pong ✓ round-trip OK', { ok: true })
        return
      }
      logWsEvent('recv ' + (data?.type || 'message'), { ok: true, detail: String(event.data).slice(0, 200) })
      // Сервер гасит чат завершённого или отменённого заказа: рассылает замок и
      // рвёт соединение. Возвращаться туда бессмысленно — следующее рукопожатие
      // получит тот же замок, и так по кругу.
      if (data?.type === 'system' && data?.action === 'lock') {
        logWsEvent('reconnect stopped: чат заказа закрыт')
        orderId = null
        clearRetry()
      }
      handlers.onMessage(data, target)
    }
  }

  // Отправляет кадр, если сокет открыт. false означает «не ушло»: вызывающий
  // сам решает, повторить это по REST или просто забыть.
  const send = (payload: Record<string, unknown>): boolean => {
    const label = String(payload.type || 'message')
    if (!socket || socket.readyState !== WebSocket.OPEN) return false
    try {
      socket.send(JSON.stringify(payload))
      logWsEvent(`send ${label} (readyState=${socket.readyState})`, { ok: true })
      return true
    } catch (e: any) {
      logWsEvent(`send ${label} THREW`, { ok: false, error: String(e?.message || e) })
      return false
    }
  }

  const connect = (id: string) => {
    disconnect()
    if (disposed) return
    orderId = id
    void open()
  }

  const disconnect = () => {
    orderId = null
    attempt = 0
    everOpened = false
    generation += 1
    clearRetry()
    dropSocket()
  }

  const onVisibilityChange = () => {
    if (document.visibilityState !== 'visible') return
    if (disposed || !orderId) return
    if (socket && (socket.readyState === WebSocket.CONNECTING || socket.readyState === WebSocket.OPEN)) return
    // Усыплённый WebView не досчитывает свои таймеры, поэтому отложенная попытка
    // могла простоять весь фон нетронутой. Возврат на экран — момент, когда сеть
    // заведомо есть: пробуем сразу и с нуля, а не в конце разъехавшейся паузы.
    clearRetry()
    attempt = 0
    void open()
  }

  document.addEventListener('visibilitychange', onVisibilityChange)

  onUnmounted(() => {
    disposed = true
    document.removeEventListener('visibilitychange', onVisibilityChange)
    disconnect()
  })

  return { isConnected, connect, disconnect, send }
}
