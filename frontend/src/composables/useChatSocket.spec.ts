import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'

const ensureFreshSession = vi.fn(async () => 'ok' as 'ok' | 'stale' | 'ended')
let tokenCounter = 0

vi.mock('../services/api', () => ({
  // Настоящая функция читает токен на каждый вызов — здесь это видно по тому,
  // что каждая попытка получает свой URL.
  buildChatWebSocketUrl: (orderId: string) => `wss://host/api/chats/${orderId}/ws?token=t${++tokenCounter}`,
  ensureFreshSession: () => ensureFreshSession(),
}))

import { useChatSocket } from './useChatSocket'

class FakeWebSocket {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSING = 2
  static CLOSED = 3
  static instances: FakeWebSocket[] = []

  readyState = FakeWebSocket.CONNECTING
  sent: string[] = []
  onopen: ((e?: any) => void) | null = null
  onclose: ((e: any) => void) | null = null
  onmessage: ((e: any) => void) | null = null
  onerror: ((e?: any) => void) | null = null

  constructor(public url: string) {
    FakeWebSocket.instances.push(this)
  }

  send(data: string) {
    this.sent.push(data)
  }

  close(code = 1000, reason = '') {
    this.readyState = FakeWebSocket.CLOSED
    this.onclose?.({ code, reason })
  }

  // Со стороны сервера/сети.
  accept() {
    this.readyState = FakeWebSocket.OPEN
    this.onopen?.()
  }

  drop(code: number) {
    this.readyState = FakeWebSocket.CLOSED
    this.onclose?.({ code, reason: '' })
  }

  emit(payload: unknown) {
    this.onmessage?.({ data: JSON.stringify(payload) })
  }
}

const flush = async () => {
  for (let i = 0; i < 5; i++) await Promise.resolve()
}

const setVisibility = (state: 'visible' | 'hidden') => {
  Object.defineProperty(document, 'visibilityState', { value: state, configurable: true })
  document.dispatchEvent(new Event('visibilitychange'))
}

const sockets = () => FakeWebSocket.instances
const last = () => FakeWebSocket.instances[FakeWebSocket.instances.length - 1]

// Каждый композабл живёт до конца своего теста: оставленный смонтированным, он
// продолжает слушать видимость документа и лезет переподключаться в чужом тесте.
const mounted: Array<{ unmount: () => void }> = []

function mountSocket(handlers: Parameters<typeof useChatSocket>[0]) {
  let chat!: ReturnType<typeof useChatSocket>
  const wrapper = mount(
    defineComponent({
      setup() {
        chat = useChatSocket(handlers)
        return () => null
      },
    }),
  )
  mounted.push(wrapper)
  return { wrapper, chat }
}

describe('useChatSocket', () => {
  const onMessage = vi.fn()
  const onOpen = vi.fn()

  beforeEach(() => {
    vi.useFakeTimers()
    // Убирает разброс паузы: без него точное время попытки не проверить.
    vi.spyOn(Math, 'random').mockReturnValue(0)
    FakeWebSocket.instances = []
    tokenCounter = 0
    ensureFreshSession.mockResolvedValue('ok')
    onMessage.mockClear()
    onOpen.mockClear()
    ;(globalThis as any).WebSocket = FakeWebSocket
    setVisibility('visible')
  })

  afterEach(() => {
    while (mounted.length) mounted.pop()!.unmount()
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('поднимает соединение заново после обрыва, с растущей паузой и свежим URL', async () => {
    const { chat } = mountSocket({ onMessage, onOpen })
    chat.connect('order-1')
    await flush()

    expect(sockets()).toHaveLength(1)
    last().accept()
    expect(chat.isConnected.value).toBe(true)

    last().drop(1006)
    expect(chat.isConnected.value).toBe(false)

    // Первая пауза — база (1000 мс) с минимальным разбросом.
    await vi.advanceTimersByTimeAsync(499)
    expect(sockets()).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(sockets()).toHaveLength(2)
    expect(sockets()[1].url).not.toBe(sockets()[0].url)

    // Вторая попытка тоже не удалась — пауза удваивается.
    last().drop(1006)
    await vi.advanceTimersByTimeAsync(999)
    expect(sockets()).toHaveLength(2)
    await vi.advanceTimersByTimeAsync(1)
    expect(sockets()).toHaveLength(3)
  })

  it('сообщает об открытии после обрыва отдельно от первого', async () => {
    const { chat } = mountSocket({ onMessage, onOpen })
    chat.connect('order-1')
    await flush()
    last().accept()
    expect(onOpen).toHaveBeenLastCalledWith({ orderId: 'order-1', reconnected: false })

    last().drop(1006)
    await vi.advanceTimersByTimeAsync(500)
    last().accept()
    expect(onOpen).toHaveBeenLastCalledWith({ orderId: 'order-1', reconnected: true })
  })

  it('не возвращается после штатного закрытия', async () => {
    const { chat } = mountSocket({ onMessage, onOpen })
    chat.connect('order-1')
    await flush()
    last().accept()

    last().drop(1000)
    await vi.advanceTimersByTimeAsync(60_000)
    expect(sockets()).toHaveLength(1)
  })

  it('не возвращается после того, как сервер закрыл чат заказа', async () => {
    const { chat } = mountSocket({ onMessage, onOpen })
    chat.connect('order-1')
    await flush()
    last().accept()

    last().emit({ type: 'system', action: 'lock' })
    expect(onMessage).toHaveBeenCalledWith({ type: 'system', action: 'lock' }, 'order-1')

    last().drop(1006)
    await vi.advanceTimersByTimeAsync(60_000)
    expect(sockets()).toHaveLength(1)
  })

  it('не возвращается, когда сессия завершена, и пробует снова, пока она лишь просрочена', async () => {
    const { chat } = mountSocket({ onMessage, onOpen })
    ensureFreshSession.mockResolvedValue('ended')
    chat.connect('order-1')
    await flush()
    expect(sockets()).toHaveLength(0)

    // Обновление не удалось, но токены на месте — это обычно пропавшая сеть.
    ensureFreshSession.mockResolvedValue('stale')
    setVisibility('hidden')
    setVisibility('visible')
    await flush()
    expect(sockets()).toHaveLength(1)
  })

  it('пробует вернуться сразу при возврате на экран, не дожидаясь паузы', async () => {
    const { chat } = mountSocket({ onMessage, onOpen })
    chat.connect('order-1')
    await flush()
    last().accept()
    last().drop(1006)

    setVisibility('hidden')
    setVisibility('visible')
    await flush()
    expect(sockets()).toHaveLength(2)

    // Отложенная попытка снята — второго сокета на ту же паузу не появится.
    await vi.advanceTimersByTimeAsync(60_000)
    expect(sockets()).toHaveLength(2)
  })

  it('прекращает попытки при закрытии чата и при размонтировании', async () => {
    const { wrapper, chat } = mountSocket({ onMessage, onOpen })
    chat.connect('order-1')
    await flush()
    last().accept()
    last().drop(1006)

    chat.disconnect()
    await vi.advanceTimersByTimeAsync(60_000)
    expect(sockets()).toHaveLength(1)

    chat.connect('order-2')
    await flush()
    expect(sockets()).toHaveLength(2)
    last().accept()
    last().drop(1006)

    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(60_000)
    expect(sockets()).toHaveLength(2)

    // Слушатель видимости снят вместе с компонентом.
    setVisibility('hidden')
    setVisibility('visible')
    await flush()
    expect(sockets()).toHaveLength(2)
  })

  it('шлёт кадры только по открытому сокету', async () => {
    const { chat } = mountSocket({ onMessage, onOpen })
    expect(chat.send({ type: 'read_ack' })).toBe(false)

    chat.connect('order-1')
    await flush()
    expect(chat.send({ type: 'read_ack' })).toBe(false)

    last().accept()
    expect(chat.send({ type: 'read_ack' })).toBe(true)
    // Первым уходит диагностический ping из обработчика открытия.
    expect(last().sent).toEqual([JSON.stringify({ type: 'ping' }), JSON.stringify({ type: 'read_ack' })])
  })

  it('не отдаёт наверх ни pong, ни битый кадр', async () => {
    const { chat } = mountSocket({ onMessage, onOpen })
    chat.connect('order-1')
    await flush()
    last().accept()

    last().emit({ type: 'pong', ts: 1 })
    last().onmessage?.({ data: 'не json' })
    expect(onMessage).not.toHaveBeenCalled()

    last().emit({ id: 'm1', text: 'привет' })
    expect(onMessage).toHaveBeenCalledWith({ id: 'm1', text: 'привет' }, 'order-1')
  })
})
