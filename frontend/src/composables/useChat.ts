import { ref, onUnmounted } from 'vue'
import { useAuthStore } from '../stores/auth-store'
import api, { buildChatWebSocketUrl, formatApiError } from '../services/api'
import { NativeWebSocket } from '../plugins/native-websocket'

export function useChat(isNative: boolean) {
  const authStore = useAuthStore()

  const selectedChatOrder = ref<any>(null)
  const chatMessages = ref<any[]>([])
  const chatText = ref('')
  const chatLocked = ref(false)
  const sendingChat = ref(false)
  const chatError = ref('')
  const ws = ref<WebSocket | null>(null)
  let pollTimer: any = null

  const scheduleChatPoll = (orderID: string) => {
    if (pollTimer) clearInterval(pollTimer)
    pollTimer = setInterval(async () => {
      if (!selectedChatOrder.value || selectedChatOrder.value.id !== orderID) {
        clearInterval(pollTimer)
        pollTimer = null
        return
      }
      try {
        const response = await api.get(`/chats/${orderID}/messages`, {
          params: { _t: Date.now() },
          headers: { 'Cache-Control': 'no-cache, no-store, must-revalidate' },
          timeout: 4000,
        })
        const newMsgs = response.data || []
        const currentIds = new Set(chatMessages.value.map((m: any) => m.id))
        let added = false
        for (const msg of newMsgs) {
          if (!currentIds.has(msg.id)) {
            chatMessages.value.push(msg)
            added = true
          }
        }
      } catch (err) {
        // silent polling error
      }
    }, 2000)
  }

  const markChatAsRead = async (orderID: string, unreadSet?: { value: Set<string> }) => {
    if (unreadSet) unreadSet.value.delete(orderID)
    try {
      await api.post(`/chats/${orderID}/read`)
    } catch (err) {
      console.warn('[useChat] mark read failed:', err)
    }
    if (isNative) {
      try {
        await NativeWebSocket.send({ message: JSON.stringify({ type: 'read_ack' }) })
      } catch (e) {}
    } else if (ws.value && ws.value.readyState === WebSocket.OPEN) {
      try {
        ws.value.send(JSON.stringify({ type: 'read_ack' }))
      } catch (e) {}
    }
  }

  const sendDeliveryAck = () => {
    if (isNative) {
      try {
        NativeWebSocket.send({ message: JSON.stringify({ type: 'delivery_ack' }) })
      } catch (e) {}
    } else if (ws.value && ws.value.readyState === WebSocket.OPEN) {
      try {
        ws.value.send(JSON.stringify({ type: 'delivery_ack' }))
      } catch (e) {}
    }
  }

  const handleIncomingChatMessage = (
    data: any,
    order: any,
    unreadSet?: { value: Set<string> },
    chatToast?: { value: any },
    recipientLabel?: string
  ) => {
    if (data.type === 'message_deleted') {
      chatMessages.value = chatMessages.value.filter((m: any) => m.id !== data.message_id)
      return
    }

    const exists = chatMessages.value.some((m: any) => m.id === data.id)
    if (!exists) {
      chatMessages.value.push(data)
    }

    if (data.sender_id !== authStore.userID) {
      sendDeliveryAck()
      if (!selectedChatOrder.value || selectedChatOrder.value.id !== order.id) {
        if (unreadSet) unreadSet.value.add(order.id)
        if (chatToast) {
          chatToast.value = {
            id: order.id,
            title: recipientLabel || 'Новое сообщение',
            text: `Заказ #${order.id.slice(0, 8)}: ${data.text}`,
            order,
          }
        }
      } else {
        markChatAsRead(order.id, unreadSet)
      }
    }
  }

  const openChat = async (
    order: any,
    unreadSet?: { value: Set<string> },
    recipientLabel?: string
  ) => {
    selectedChatOrder.value = order
    chatMessages.value = []
    chatLocked.value = false
    chatError.value = ''

    markChatAsRead(order.id, unreadSet)

    try {
      const response = await api.get(
        `/chats/${order.id}/messages`,
        isNative
          ? {
              params: { _t: Date.now() },
              headers: { 'Cache-Control': 'no-cache, no-store, must-revalidate' },
              timeout: 5000,
            }
          : undefined
      )
      chatMessages.value = response.data || []
    } catch (err) {
      console.error(err)
      if (isNative) chatError.value = 'Не удалось загрузить историю чата'
    }

    const wsUrl = buildChatWebSocketUrl(order.id, authStore.token)

    if (isNative) {
      try {
        await NativeWebSocket.disconnect()
        await NativeWebSocket.addListener('onOpen', () => {
          chatError.value = ''
          sendDeliveryAck()
          markChatAsRead(order.id, unreadSet)
        })
        await NativeWebSocket.addListener('onMessage', (res) => {
          if (!res || !res.data) return
          const data = JSON.parse(res.data)
          handleIncomingChatMessage(data, order, unreadSet, undefined, recipientLabel)
        })
        await NativeWebSocket.connect({ url: wsUrl })
      } catch (nativeErr) {
        console.warn('[useChat] NativeWebSocket connection error:', nativeErr)
      }
    } else {
      if (ws.value) {
        ws.value.close()
        ws.value = null
      }

      ws.value = new WebSocket(wsUrl)
      ws.value.onopen = () => {
        sendDeliveryAck()
        markChatAsRead(order.id, unreadSet)
      }
      ws.value.onmessage = (event) => {
        const data = JSON.parse(event.data)
        handleIncomingChatMessage(data, order, unreadSet, undefined, recipientLabel)
      }
    }

    scheduleChatPoll(order.id)
  }

  const sendChatMessage = async (event?: Event) => {
    if (event) {
      event.preventDefault()
      event.stopPropagation()
    }
    if (!chatText.value.trim() || chatLocked.value) return
    const text = chatText.value.trim()

    if (isNative) {
      try {
        await NativeWebSocket.send({ message: JSON.stringify({ text }) })
        chatText.value = ''
      } catch (e) {
        console.warn('[useChat] NativeWebSocket send failed, fallback to REST:', e)
        try {
          sendingChat.value = true
          const res = await api.post(`/chats/${selectedChatOrder.value.id}/messages`, { text })
          if (res.data) {
            const exists = chatMessages.value.some((m: any) => m.id === res.data.id)
            if (!exists) chatMessages.value.push(res.data)
          }
          chatText.value = ''
        } catch (err: any) {
          chatError.value = formatApiError(err, 'Не удалось отправить сообщение')
        } finally {
          sendingChat.value = false
        }
      }
    } else if (ws.value && ws.value.readyState === WebSocket.OPEN) {
      try {
        ws.value.send(JSON.stringify({ text }))
        chatText.value = ''
      } catch (e) {
        console.error('[useChat] WebSocket send error:', e)
      }
    } else {
      try {
        sendingChat.value = true
        const res = await api.post(`/chats/${selectedChatOrder.value.id}/messages`, { text })
        if (res.data) {
          const exists = chatMessages.value.some((m: any) => m.id === res.data.id)
          if (!exists) chatMessages.value.push(res.data)
        }
        chatText.value = ''
      } catch (err: any) {
        chatError.value = formatApiError(err, 'Не удалось отправить сообщение')
      } finally {
        sendingChat.value = false
      }
    }
  }

  const closeChat = () => {
    selectedChatOrder.value = null
    if (pollTimer) {
      clearInterval(pollTimer)
      pollTimer = null
    }
    if (isNative) {
      NativeWebSocket.disconnect().catch(() => {})
    } else if (ws.value) {
      ws.value.close()
      ws.value = null
    }
  }

  const deleteMessage = async (messageID: string, confirmText: string) => {
    if (!selectedChatOrder.value || !messageID) return
    if (!confirm(confirmText)) return
    try {
      await api.delete(`/chats/${selectedChatOrder.value.id}/messages/${messageID}`)
      chatMessages.value = chatMessages.value.filter((m: any) => m.id !== messageID)
    } catch (err: any) {
      console.error('[useChat] failed to delete message:', err)
      chatError.value = formatApiError(err, 'Failed to delete message')
    }
  }

  onUnmounted(() => {
    closeChat()
  })

  return {
    selectedChatOrder,
    chatMessages,
    chatText,
    chatLocked,
    sendingChat,
    chatError,
    openChat,
    sendChatMessage,
    closeChat,
    deleteMessage,
    handleIncomingChatMessage,
    markChatAsRead,
  }
}
