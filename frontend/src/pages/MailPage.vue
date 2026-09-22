<template>
  <div class="mail-wrapper">
    <div class="mail-container">
      <div class="top-nav">
        <button type="button" class="btn-back" @click="goBack">
          <i class="ph-bold ph-arrow-left"></i>
          Назад
        </button>
        <div class="page-header-title">
          <i class="ph-fill ph-envelope-simple icon-title"></i>
          Почта
          <span v-if="unread" class="unread-badge">{{ unread }}</span>
        </div>
        <button v-if="unread" type="button" class="btn-secondary" @click="readAll">
          Прочитать все
        </button>
      </div>

      <div v-if="loading" class="state-note">Загружаем почту…</div>
      <div v-else-if="error" class="state-note error">{{ error }}</div>
      <div v-else-if="!messages.length" class="state-note">
        Писем пока нет. Сюда приходят достижения, подарки, акции, новости и письма от службы поддержки.
      </div>

      <div v-else class="mail-list">
        <div
          v-for="message in messages"
          :key="message.id"
          class="mail-card"
          :class="{ unread: isUnread(message) }"
          @click="open(message)"
        >
          <div class="mail-icon" :class="message.kind.toLowerCase()">
            <i :class="kindIcon(message.kind)"></i>
          </div>
          <div class="mail-body">
            <div class="mail-head">
              <span class="mail-subject">{{ message.subject }}</span>
              <span class="mail-date">{{ formatDate(message.last_at || message.created_at) }}</span>
            </div>
            <div class="mail-text">{{ message.body }}</div>
            <div v-if="message.kind === 'DIRECT'" class="mail-meta">
              <span class="mail-tag"><i class="ph-fill ph-headset"></i> Служба поддержки</span>
              <span v-if="message.replies" class="mail-tag replies">
                <i class="ph-fill ph-chats-circle"></i> {{ message.replies }} {{ repliesWord(message.replies) }}
              </span>
              <span class="mail-tag answer">Ответить</span>
            </div>
            <button
              v-else-if="message.ref_type === 'gift'"
              type="button"
              class="btn-link"
              @click.stop="goToGifts"
            >
              Открыть подарок
            </button>
            <button
              v-else-if="message.ref_type === 'achievement'"
              type="button"
              class="btn-link"
              @click.stop="goToAchievements"
            >
              Посмотреть достижения
            </button>
            <button
              v-else-if="message.ref_type === 'shop_order' || message.ref_type === 'perk'"
              type="button"
              class="btn-link"
              @click.stop="goToShop(message)"
            >
              {{ message.ref_type === 'perk' ? $t('shop.perk.cta') : $t('shop.tabs.orders') }}
            </button>
          </div>
          <button type="button" class="btn-delete" title="Удалить" @click.stop="remove(message)">
            <i class="ph ph-trash"></i>
          </button>
        </div>
      </div>
    </div>

    <!-- Переписка. Открывается поверх списка: разговор читают целиком, а не
         подглядывают в него из ленты. -->
    <div v-if="thread" class="thread-overlay" @click.self="closeThread">
      <div class="thread-panel">
        <div class="thread-head">
          <div class="thread-title">
            <i class="ph-fill ph-envelope-open"></i>
            {{ threadSubject }}
          </div>
          <button type="button" class="btn-close" @click="closeThread">
            <i class="ph ph-x"></i>
          </button>
        </div>

        <div ref="threadBody" class="thread-body">
          <div v-if="threadLoading" class="state-note">Загружаем переписку…</div>
          <div
            v-for="item in thread"
            v-else
            :key="item.id"
            class="bubble"
            :class="item.direction === 'OUT' ? 'mine' : 'theirs'"
          >
            <div class="bubble-from">
              {{ item.direction === 'OUT' ? 'Вы' : item.sender_name || 'Служба поддержки' }}
              <span class="bubble-date">{{ formatDateTime(item.created_at) }}</span>
            </div>
            <div class="bubble-text">{{ item.body }}</div>
          </div>
        </div>

        <div v-if="canReply" class="thread-reply">
          <textarea
            v-model="replyText"
            class="reply-input"
            rows="2"
            placeholder="Написать ответ…"
            :disabled="sending"
            @keydown.enter.ctrl.prevent="sendReply"
          ></textarea>
          <button
            type="button"
            class="btn-send"
            :disabled="sending || !replyText.trim()"
            @click="sendReply"
          >
            <i class="ph-fill ph-paper-plane-right"></i>
          </button>
        </div>
        <div v-else class="thread-note">На это письмо нельзя ответить.</div>
        <div v-if="replyError" class="thread-note error">{{ replyError }}</div>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, nextTick, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth-store'

import {
  deleteMail,
  getMail,
  getMailThread,
  markAllMailRead,
  markMailRead,
  replyToMail,
  type MailMessage,
} from '../api/mail'

const KIND_ICONS: Record<string, string> = {
  ACHIEVEMENT: 'ph-fill ph-trophy',
  GIFT: 'ph-fill ph-gift',
  PROMO: 'ph-fill ph-megaphone',
  NEWS: 'ph-fill ph-newspaper',
  SYSTEM: 'ph-fill ph-info',
  DIRECT: 'ph-fill ph-chats-circle',
  SHOP: 'ph-fill ph-storefront',
}

export default defineComponent({
  name: 'MailPage',
  setup() {
    const router = useRouter()
    const authStore = useAuthStore()

    const messages = ref<MailMessage[]>([])
    const unread = ref(0)
    const loading = ref(true)
    const error = ref('')

    const thread = ref<MailMessage[] | null>(null)
    const threadRoot = ref<MailMessage | null>(null)
    const threadLoading = ref(false)
    const threadBody = ref<HTMLElement | null>(null)
    const replyText = ref('')
    const replyError = ref('')
    const sending = ref(false)

    const load = async () => {
      loading.value = true
      error.value = ''
      try {
        const inbox = await getMail()
        messages.value = inbox.messages
        unread.value = inbox.unread
      } catch {
        error.value = 'Не удалось загрузить почту. Попробуйте обновить страницу.'
      } finally {
        loading.value = false
      }
    }

    // Непрочитанной переписка считается и тогда, когда не открыт ответ внутри
    // неё: карточка в ленте одна, и значок на ней говорит про весь разговор.
    const isUnread = (message: MailMessage) => !message.read_at || (message.thread_unread ?? 0) > 0

    const markLocallyRead = (message: MailMessage) => {
      const wasUnread = isUnread(message)
      if (!wasUnread) return
      const inThread = message.thread_unread ?? (message.read_at ? 0 : 1)
      message.read_at = message.read_at || new Date().toISOString()
      message.thread_unread = 0
      unread.value = Math.max(0, unread.value - Math.max(inThread, 1))
    }

    // Открытое письмо помечается прочитанным сразу в списке: ждать ответа
    // сервера, чтобы убрать точку, значит показывать её ещё секунду после того,
    // как человек уже прочитал.
    const open = async (message: MailMessage) => {
      if (message.kind === 'DIRECT') {
        await openThread(message)
        return
      }
      if (!isUnread(message)) return
      markLocallyRead(message)
      try {
        await markMailRead(message.id)
      } catch {
        /* отметка о прочтении не стоит того, чтобы о ней сообщать */
      }
    }

    const openThread = async (message: MailMessage) => {
      threadRoot.value = message
      thread.value = []
      threadLoading.value = true
      replyText.value = ''
      replyError.value = ''
      markLocallyRead(message)
      try {
        // Сервер отдаёт ветку и тем же движением отмечает её прочитанной:
        // человек видит разговор целиком, включая ответы внутри.
        thread.value = await getMailThread(message.id)
      } catch {
        replyError.value = 'Не удалось загрузить переписку.'
      } finally {
        threadLoading.value = false
        scrollThreadDown()
      }
    }

    const closeThread = () => {
      thread.value = null
      threadRoot.value = null
      replyText.value = ''
      replyError.value = ''
    }

    const scrollThreadDown = () => {
      void nextTick(() => {
        const el = threadBody.value
        if (el) el.scrollTop = el.scrollHeight
      })
    }

    const canReply = computed(() => threadRoot.value?.kind === 'DIRECT')

    const threadSubject = computed(() => threadRoot.value?.subject || 'Переписка')

    const sendReply = async () => {
      const root = threadRoot.value
      const text = replyText.value.trim()
      if (!root || !text || sending.value) return
      sending.value = true
      replyError.value = ''
      try {
        const reply = await replyToMail(root.id, text)
        thread.value = [...(thread.value || []), reply]
        replyText.value = ''
        // Ответ поднимает переписку в ленте: она стала свежей, и в следующий
        // раз человек найдёт её там, где ищут последнее.
        root.last_at = reply.created_at
        root.replies = (root.replies ?? 0) + 1
        messages.value = [...messages.value].sort(
          (a, b) =>
            new Date(b.last_at || b.created_at).getTime() -
            new Date(a.last_at || a.created_at).getTime(),
        )
        scrollThreadDown()
      } catch (err: any) {
        replyError.value =
          typeof err?.response?.data === 'string' && err.response.data
            ? err.response.data
            : 'Не удалось отправить ответ. Попробуйте ещё раз.'
      } finally {
        sending.value = false
      }
    }

    const readAll = async () => {
      const now = new Date().toISOString()
      messages.value.forEach((message) => {
        if (!message.read_at) message.read_at = now
        message.thread_unread = 0
      })
      unread.value = 0
      try {
        await markAllMailRead()
      } catch {
        /* см. выше */
      }
    }

    const remove = async (message: MailMessage) => {
      messages.value = messages.value.filter((item) => item.id !== message.id)
      if (isUnread(message)) unread.value = Math.max(0, unread.value - 1)
      if (threadRoot.value?.id === message.id) closeThread()
      try {
        await deleteMail(message.id)
      } catch {
        await load()
      }
    }

    const kindIcon = (kind: string) => KIND_ICONS[kind] ?? KIND_ICONS.SYSTEM

    const repliesWord = (count: number) => {
      const last = count % 10
      const tens = count % 100
      if (tens >= 11 && tens <= 14) return 'сообщений'
      if (last === 1) return 'сообщение'
      if (last >= 2 && last <= 4) return 'сообщения'
      return 'сообщений'
    }

    const formatDate = (value: string) => {
      const date = new Date(value)
      const today = new Date()
      if (date.toDateString() === today.toDateString()) {
        return date.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })
      }
      return date.toLocaleDateString('ru-RU', { day: 'numeric', month: 'short' })
    }

    const formatDateTime = (value: string) =>
      new Date(value).toLocaleString('ru-RU', {
        day: 'numeric',
        month: 'short',
        hour: '2-digit',
        minute: '2-digit',
      })

    const goBack = () => router.back()
    const goToGifts = () => router.push('/executor/gifts')
    const goToAchievements = () => router.push('/executor/achievements')
    // Письма магазина ведут в «Мои покупки» той роли, в которой человек сейчас,
    // а о привилегии — в магазин исполнителя, где её продлевают.
    const goToShop = (message: MailMessage) => {
      if (message.ref_type === 'perk') {
        router.push('/executor/shop')
        return
      }
      const home = authStore.activeRole === 'EXECUTOR' ? '/executor' : '/customer'
      router.push({ path: `${home}/shop`, query: { tab: 'orders', order: message.ref_id } })
    }

    onMounted(load)

    return {
      messages,
      unread,
      loading,
      error,
      thread,
      threadLoading,
      threadBody,
      threadSubject,
      canReply,
      replyText,
      replyError,
      sending,
      isUnread,
      open,
      closeThread,
      sendReply,
      readAll,
      remove,
      kindIcon,
      repliesWord,
      formatDate,
      formatDateTime,
      goBack,
      goToGifts,
      goToAchievements,
      goToShop,
    }
  },
})
</script>

<style scoped>
.mail-wrapper {
  min-height: 100vh;
  background: #f6f7fb;
  padding: 16px;
}

.mail-container {
  max-width: 720px;
  margin: 0 auto;
}

.top-nav {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
  flex-wrap: wrap;
}

.btn-back,
.btn-secondary {
  border: none;
  background: #fff;
  border-radius: 10px;
  padding: 8px 14px;
  font-size: 14px;
  color: #4b5563;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.btn-secondary {
  margin-left: auto;
}

.page-header-title {
  font-size: 18px;
  font-weight: 600;
  color: #111827;
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.icon-title {
  color: #3b82f6;
}

.unread-badge {
  background: #ef4444;
  color: #fff;
  font-size: 12px;
  border-radius: 999px;
  padding: 1px 8px;
}

.state-note {
  background: #fff;
  border-radius: 12px;
  padding: 20px;
  text-align: center;
  color: #6b7280;
  font-size: 14px;
}

.state-note.error {
  color: #b91c1c;
}

.mail-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.mail-card {
  background: #fff;
  border-radius: 12px;
  padding: 14px;
  display: flex;
  gap: 12px;
  cursor: pointer;
  border: 1px solid #eef0f4;
}

.mail-card.unread {
  border-color: #bfdbfe;
  background: #f8fbff;
}

.mail-icon {
  width: 38px;
  height: 38px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  flex-shrink: 0;
  background: #f3f4f6;
  color: #6b7280;
}

.mail-icon.achievement {
  background: #fffbeb;
  color: #d97706;
}

.mail-icon.gift {
  background: #fdf2f8;
  color: #db2777;
}

.mail-icon.promo {
  background: #eff6ff;
  color: #2563eb;
}

.mail-icon.direct {
  background: #fef3c7;
  color: #b45309;
}

.mail-body {
  flex: 1;
  min-width: 0;
}

.mail-head {
  display: flex;
  justify-content: space-between;
  gap: 8px;
}

.mail-subject {
  font-weight: 600;
  color: #111827;
  font-size: 14px;
}

.mail-date {
  font-size: 12px;
  color: #9ca3af;
  white-space: nowrap;
}

.mail-text {
  color: #4b5563;
  font-size: 13px;
  margin-top: 4px;
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.mail-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
  flex-wrap: wrap;
}

.mail-tag {
  font-size: 12px;
  color: #6b7280;
  background: #f3f4f6;
  border-radius: 999px;
  padding: 2px 8px;
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.mail-tag.replies {
  background: #eef2ff;
  color: #4338ca;
}

.mail-tag.answer {
  background: #fef3c7;
  color: #b45309;
}

.btn-link {
  border: none;
  background: none;
  color: #2563eb;
  font-size: 13px;
  padding: 6px 0 0;
  cursor: pointer;
}

.btn-delete {
  border: none;
  background: none;
  color: #d1d5db;
  cursor: pointer;
  align-self: flex-start;
  font-size: 16px;
}

.btn-delete:hover {
  color: #ef4444;
}

/* --- Переписка --- */

.thread-overlay {
  position: fixed;
  inset: 0;
  background: rgba(15, 23, 42, 0.45);
  display: flex;
  align-items: flex-end;
  justify-content: center;
  z-index: 60;
}

.thread-panel {
  background: #fff;
  width: 100%;
  max-width: 720px;
  max-height: 88vh;
  border-radius: 16px 16px 0 0;
  display: flex;
  flex-direction: column;
}

@media (min-width: 768px) {
  .thread-overlay {
    align-items: center;
  }

  .thread-panel {
    border-radius: 16px;
  }
}

.thread-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 14px 16px;
  border-bottom: 1px solid #eef0f4;
}

.thread-title {
  font-weight: 600;
  color: #111827;
  font-size: 15px;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.thread-title i {
  color: #d97706;
}

.btn-close {
  margin-left: auto;
  border: none;
  background: none;
  color: #9ca3af;
  font-size: 18px;
  cursor: pointer;
}

.thread-body {
  padding: 14px 16px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 10px;
  flex: 1;
}

.bubble {
  max-width: 82%;
  border-radius: 12px;
  padding: 10px 12px;
  font-size: 14px;
}

.bubble.theirs {
  background: #f3f4f6;
  color: #111827;
  align-self: flex-start;
  border-bottom-left-radius: 4px;
}

.bubble.mine {
  background: #eff6ff;
  color: #0f172a;
  align-self: flex-end;
  border-bottom-right-radius: 4px;
}

.bubble-from {
  font-size: 11px;
  color: #6b7280;
  margin-bottom: 4px;
  display: flex;
  gap: 8px;
}

.bubble-date {
  color: #9ca3af;
}

.bubble-text {
  white-space: pre-wrap;
  word-break: break-word;
}

.thread-reply {
  display: flex;
  gap: 8px;
  padding: 12px 16px;
  border-top: 1px solid #eef0f4;
  align-items: flex-end;
}

.reply-input {
  flex: 1;
  border: 1px solid #e5e7eb;
  border-radius: 10px;
  padding: 10px 12px;
  font-size: 14px;
  font-family: inherit;
  resize: none;
  outline: none;
}

.reply-input:focus {
  border-color: #93c5fd;
}

.btn-send {
  border: none;
  background: #2563eb;
  color: #fff;
  width: 40px;
  height: 40px;
  border-radius: 10px;
  font-size: 18px;
  cursor: pointer;
  flex-shrink: 0;
}

.btn-send:disabled {
  background: #cbd5e1;
  cursor: default;
}

.thread-note {
  padding: 12px 16px;
  font-size: 13px;
  color: #6b7280;
  border-top: 1px solid #eef0f4;
}

.thread-note.error {
  color: #b91c1c;
  border-top: none;
  padding-top: 0;
}
</style>
