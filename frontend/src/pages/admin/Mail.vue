<template>
  <div class="admin-mail">
    <div class="mail-page-header">
      <h1 class="page-title">Внутренняя почта</h1>
      <span v-if="unread" class="header-unread">{{ unread }} без ответа</span>
      <button type="button" class="btn-secondary" @click="broadcastOpen = true">
        <i class="ph-bold ph-megaphone"></i> Рассылка
      </button>
      <button type="button" class="btn-primary" @click="startCompose()">
        <i class="ph-bold ph-pencil-simple-line"></i> Написать пользователю
      </button>
    </div>

    <div class="mail-container">
      <!-- Левая панель: переписки -->
      <div :class="['dialog-list-pane', { 'mobile-hidden': selectedUserID }]">
        <div class="pane-header">
          <div class="search-box">
            <i class="ph ph-magnifying-glass"></i>
            <input v-model="searchQuery" type="text" placeholder="Поиск по ФИО или телефону…" />
          </div>
          <label class="filter-toggle">
            <input v-model="onlyUnanswered" type="checkbox" @change="loadDialogs" />
            Только без ответа
          </label>
        </div>

        <div class="dialog-list">
          <div v-if="loading" class="empty-state">Загрузка переписок…</div>
          <div v-else-if="!filteredDialogs.length" class="empty-state">
            Переписок нет. Начните с кнопки «Написать пользователю».
          </div>
          <div
            v-for="dialog in filteredDialogs"
            :key="dialog.user_id"
            :class="['dialog-item', { active: dialog.user_id === selectedUserID }]"
            @click="openDialog(dialog.user_id)"
          >
            <div class="d-avatar" :class="dialog.role.toLowerCase()">
              {{ initials(dialog.full_name, dialog.phone) }}
            </div>
            <div class="d-info">
              <div class="d-top">
                <span class="d-name">{{ dialog.full_name || dialog.phone }}</span>
                <span class="d-time">{{ formatTime(dialog.last_at) }}</span>
              </div>
              <div class="d-meta">
                <span class="d-role" :class="dialog.role.toLowerCase()">{{ dialog.role }}</span>
                <span class="d-phone"><i class="ph-fill ph-phone"></i> {{ dialog.phone }}</span>
              </div>
              <div class="d-bottom">
                <span class="d-last" :class="{ bold: dialog.unread > 0 }">
                  <i v-if="dialog.last_direction === 'IN'" class="ph ph-arrow-up-right out-mark"></i>
                  {{ dialog.last_body }}
                </span>
                <span v-if="dialog.unread > 0" class="d-unread">{{ dialog.unread }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- Правая панель: переписка -->
      <div :class="['dialog-pane', { 'mobile-hidden': !selectedUserID }]">
        <template v-if="selectedUserID">
          <div class="dialog-header">
            <button type="button" class="btn-back" title="К списку" @click="closeDialog">
              <i class="ph-bold ph-arrow-left"></i>
            </button>
            <div class="dialog-user">
              <div class="dialog-user-name">{{ recipient.full_name || recipient.phone }}</div>
              <div class="dialog-user-phone"><i class="ph-fill ph-phone"></i> {{ recipient.phone }}</div>
            </div>
            <button type="button" class="btn-ghost" title="Новая тема" @click="startCompose(selectedUserID)">
              <i class="ph-bold ph-plus-circle"></i> Новая тема
            </button>
          </div>

          <div ref="messagesBox" class="dialog-messages">
            <div v-if="dialogLoading" class="empty-state">Загружаем переписку…</div>
            <div v-else-if="!messages.length" class="empty-state">
              Переписки с этим пользователем ещё не было.
            </div>
            <template v-for="(message, index) in messages" v-else :key="message.id">
              <!-- Тема показывается один раз на ветку: дальше это разговор, а не
                   стопка писем с одинаковым заголовком. -->
              <div v-if="isThreadStart(index)" class="thread-divider">
                <span>{{ message.subject }}</span>
              </div>
              <div class="bubble" :class="message.direction === 'IN' ? 'mine' : 'theirs'">
                <div class="bubble-from">
                  {{ message.direction === 'IN' ? message.sender_name || 'Администрация' : recipient.full_name || 'Пользователь' }}
                  <span class="bubble-date">{{ formatDateTime(message.created_at) }}</span>
                  <span v-if="message.direction === 'IN'" class="bubble-status">
                    {{ message.read_at ? 'прочитано' : 'не прочитано' }}
                  </span>
                </div>
                <div class="bubble-text">{{ message.body }}</div>
              </div>
            </template>
          </div>

          <div class="reply-bar">
            <textarea
              v-model="replyText"
              class="reply-input"
              rows="2"
              :placeholder="activeThreadID ? 'Ответить в переписке…' : 'Начните переписку кнопкой «Новая тема»'"
              :disabled="sending || !activeThreadID"
              @keydown.enter.ctrl.prevent="sendReply"
            ></textarea>
            <button
              type="button"
              class="btn-send"
              :disabled="sending || !activeThreadID || !replyText.trim()"
              @click="sendReply"
            >
              <i class="ph-fill ph-paper-plane-right"></i>
            </button>
          </div>
        </template>
        <div v-else class="empty-state pane-placeholder">
          Выберите переписку слева или напишите пользователю.
        </div>
      </div>
    </div>

    <!-- Рассылка: одно письмо многим. Живёт здесь, а не среди подарков: это
         почта, а не склад купонов. -->
    <div v-if="broadcastOpen" class="modal-overlay" @click.self="broadcastOpen = false">
      <div class="modal">
        <div class="modal-head">
          <span>Рассылка во внутреннюю почту</span>
          <button type="button" class="btn-close" @click="broadcastOpen = false">
            <i class="ph ph-x"></i>
          </button>
        </div>
        <div class="modal-body">
          <p class="field-note">
            Новость или акция уходит в ящик приложения — туда же, куда приходят
            выданные ачивки и купоны. Письма о выдачах пишет ядро; отсюда их
            послать нельзя. Ответить на рассылку нельзя: отвечают на адресное
            письмо.
          </p>

          <label class="field-label">Тип</label>
          <select v-model="broadcastKind" class="field-input">
            <option value="NEWS">новость</option>
            <option value="PROMO">акция</option>
          </select>

          <label class="field-label">Кому</label>
          <select v-model="broadcastRole" class="field-input">
            <option value="">всем</option>
            <option value="EXECUTOR">исполнителям</option>
            <option value="CUSTOMER">заказчикам</option>
          </select>

          <label class="field-label">Тема</label>
          <input v-model="broadcastSubject" type="text" class="field-input" placeholder="О чём письмо" />

          <label class="field-label">Текст</label>
          <textarea v-model="broadcastBody" class="field-input" rows="5" placeholder="Текст письма"></textarea>

          <div v-if="broadcastMessage" class="field-note" :class="{ error: broadcastFailed }">
            {{ broadcastMessage }}
          </div>
        </div>
        <div class="modal-foot">
          <button type="button" class="btn-secondary" @click="broadcastOpen = false">Отмена</button>
          <button
            type="button"
            class="btn-primary"
            :disabled="sending || !broadcastSubject.trim()"
            @click="sendBroadcast"
          >
            <i class="ph-fill ph-megaphone"></i> Разослать
          </button>
        </div>
      </div>
    </div>

    <!-- Новое письмо -->
    <div v-if="composeOpen" class="modal-overlay" @click.self="composeOpen = false">
      <div class="modal">
        <div class="modal-head">
          <span>Новое письмо</span>
          <button type="button" class="btn-close" @click="composeOpen = false">
            <i class="ph ph-x"></i>
          </button>
        </div>
        <div class="modal-body">
          <template v-if="composeUser">
            <div class="compose-recipient">
              <i class="ph-fill ph-user"></i>
              {{ composeUser.full_name || composeUser.phone }}
              <span class="compose-phone">{{ composeUser.phone }}</span>
              <button type="button" class="btn-link" @click="composeUser = null">Сменить</button>
            </div>
          </template>
          <template v-else>
            <label class="field-label">Получатель</label>
            <input
              v-model="userQuery"
              type="text"
              class="field-input"
              placeholder="ФИО или телефон"
              @input="searchUsers"
            />
            <div v-if="userResults.length" class="user-results">
              <button
                v-for="user in userResults"
                :key="user.id"
                type="button"
                class="user-result"
                @click="pickUser(user)"
              >
                <span class="user-name">{{ user.full_name || user.phone }}</span>
                <span class="user-phone">{{ user.phone }}</span>
                <span class="user-role">{{ user.role }}</span>
              </button>
            </div>
            <div v-else-if="userQuery && !userSearching" class="field-note">Никого не найдено.</div>
          </template>

          <label class="field-label">Тема</label>
          <input v-model="composeSubject" type="text" class="field-input" placeholder="О чём письмо" />

          <label class="field-label">Текст</label>
          <textarea v-model="composeBody" class="field-input" rows="6" placeholder="Текст письма"></textarea>

          <div v-if="composeError" class="field-note error">{{ composeError }}</div>
        </div>
        <div class="modal-foot">
          <button type="button" class="btn-secondary" @click="composeOpen = false">Отмена</button>
          <button type="button" class="btn-primary" :disabled="sending" @click="send">
            <i class="ph-fill ph-paper-plane-right"></i> Отправить
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, nextTick, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import api from '../../services/api'
import {
  adminBroadcastMail,
  adminGetMailDialogs,
  adminGetUserMail,
  adminSendMail,
  type MailDialog,
  type MailMessage,
} from '../../api/mail'

interface PickedUser {
  id: string
  full_name: string
  phone: string
  role: string
}

export default defineComponent({
  name: 'AdminMail',
  setup() {
    const route = useRoute()
    const router = useRouter()

    const dialogs = ref<MailDialog[]>([])
    const unread = ref(0)
    const loading = ref(true)
    const searchQuery = ref('')
    const onlyUnanswered = ref(false)

    const selectedUserID = ref('')
    const recipient = ref<{ id: string; full_name: string; phone: string }>({
      id: '',
      full_name: '',
      phone: '',
    })
    const messages = ref<MailMessage[]>([])
    const dialogLoading = ref(false)
    const messagesBox = ref<HTMLElement | null>(null)
    const replyText = ref('')
    const sending = ref(false)

    const broadcastOpen = ref(false)
    const broadcastKind = ref<'NEWS' | 'PROMO'>('NEWS')
    const broadcastRole = ref('')
    const broadcastSubject = ref('')
    const broadcastBody = ref('')
    const broadcastMessage = ref('')
    const broadcastFailed = ref(false)

    const composeOpen = ref(false)
    const composeUser = ref<PickedUser | null>(null)
    const composeSubject = ref('')
    const composeBody = ref('')
    const composeError = ref('')
    const userQuery = ref('')
    const userResults = ref<PickedUser[]>([])
    const userSearching = ref(false)

    const loadDialogs = async () => {
      loading.value = true
      try {
        const data = await adminGetMailDialogs(onlyUnanswered.value)
        dialogs.value = data.dialogs
        unread.value = data.unread
      } finally {
        loading.value = false
      }
    }

    const filteredDialogs = computed(() => {
      const query = searchQuery.value.trim().toLowerCase()
      if (!query) return dialogs.value
      return dialogs.value.filter(
        (dialog) =>
          dialog.full_name.toLowerCase().includes(query) || dialog.phone.includes(query),
      )
    })

    // Последняя ветка — та, в которую отвечают. Отвечать в старую, когда есть
    // свежая, значило бы продолжать разговор, который уже закончился.
    const activeThreadID = computed(() => {
      for (let i = messages.value.length - 1; i >= 0; i -= 1) {
        const threadID = messages.value[i].thread_id
        if (threadID) return threadID
      }
      return ''
    })

    const isThreadStart = (index: number) => {
      if (index === 0) return true
      return messages.value[index].thread_id !== messages.value[index - 1].thread_id
    }

    const scrollDown = () => {
      void nextTick(() => {
        const el = messagesBox.value
        if (el) el.scrollTop = el.scrollHeight
      })
    }

    const openDialog = async (userID: string) => {
      selectedUserID.value = userID
      dialogLoading.value = true
      replyText.value = ''
      try {
        const data = await adminGetUserMail(userID)
        messages.value = data.messages
        recipient.value = data.user
        // Открыв переписку, администратор её и прочитал: строка в списке
        // перестаёт быть долгом ровно в этот момент.
        const dialog = dialogs.value.find((item) => item.user_id === userID)
        if (dialog) {
          unread.value = Math.max(0, unread.value - dialog.unread)
          dialog.unread = 0
        }
      } finally {
        dialogLoading.value = false
        scrollDown()
      }
    }

    const closeDialog = () => {
      selectedUserID.value = ''
      messages.value = []
    }

    const sendReply = async () => {
      const text = replyText.value.trim()
      if (!text || !activeThreadID.value || sending.value) return
      sending.value = true
      try {
        const sent = await adminSendMail(selectedUserID.value, {
          body: text,
          thread_id: activeThreadID.value,
        })
        messages.value = [...messages.value, sent]
        replyText.value = ''
        scrollDown()
        void loadDialogs()
      } finally {
        sending.value = false
      }
    }

    const sendBroadcast = async () => {
      if (!broadcastSubject.value.trim() || sending.value) return
      sending.value = true
      broadcastMessage.value = ''
      broadcastFailed.value = false
      try {
        const sent = await adminBroadcastMail({
          kind: broadcastKind.value,
          role: broadcastRole.value || undefined,
          subject: broadcastSubject.value.trim(),
          body: broadcastBody.value.trim(),
        })
        broadcastMessage.value = `Разослано писем: ${sent}.`
        broadcastSubject.value = ''
        broadcastBody.value = ''
      } catch {
        broadcastFailed.value = true
        broadcastMessage.value = 'Не удалось разослать.'
      } finally {
        sending.value = false
      }
    }

    const startCompose = (userID?: string) => {
      composeError.value = ''
      composeSubject.value = ''
      composeBody.value = ''
      userQuery.value = ''
      userResults.value = []
      composeUser.value = userID
        ? {
            id: userID,
            full_name: recipient.value.full_name,
            phone: recipient.value.phone,
            role: '',
          }
        : null
      composeOpen.value = true
    }

    let searchTimer: ReturnType<typeof setTimeout> | null = null
    const searchUsers = () => {
      if (searchTimer) clearTimeout(searchTimer)
      const query = userQuery.value.trim()
      if (query.length < 2) {
        userResults.value = []
        return
      }
      searchTimer = setTimeout(async () => {
        userSearching.value = true
        try {
          const response = await api.get('/admin/users', {
            params: { page: 1, limit: 10, search: query },
          })
          userResults.value = (response.data?.users || []).map((user: any) => ({
            id: user.id,
            full_name: [user.last_name, user.first_name].filter(Boolean).join(' '),
            phone: user.phone,
            role: user.role,
          }))
        } catch {
          userResults.value = []
        } finally {
          userSearching.value = false
        }
      }, 300)
    }

    const pickUser = (user: PickedUser) => {
      composeUser.value = user
      userResults.value = []
      userQuery.value = ''
    }

    const send = async () => {
      composeError.value = ''
      if (!composeUser.value) {
        composeError.value = 'Выберите получателя.'
        return
      }
      if (!composeSubject.value.trim()) {
        composeError.value = 'Тема письма обязательна: по ней письмо узнают в ящике.'
        return
      }
      if (!composeBody.value.trim()) {
        composeError.value = 'Текст письма пуст.'
        return
      }
      sending.value = true
      try {
        await adminSendMail(composeUser.value.id, {
          subject: composeSubject.value.trim(),
          body: composeBody.value.trim(),
        })
        composeOpen.value = false
        await loadDialogs()
        await openDialog(composeUser.value.id)
      } catch (err: any) {
        composeError.value =
          typeof err?.response?.data === 'string' && err.response.data
            ? err.response.data
            : 'Не удалось отправить письмо.'
      } finally {
        sending.value = false
      }
    }

    const initials = (fullName: string, phone: string) => {
      const parts = fullName.trim().split(/\s+/).filter(Boolean)
      if (!parts.length) return phone.slice(-2)
      return parts
        .slice(0, 2)
        .map((part) => part[0].toUpperCase())
        .join('')
    }

    const formatTime = (value: string) => {
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

    onMounted(async () => {
      await loadDialogs()
      // На страницу приходят с карточки пользователя — с готовым адресатом.
      const userID = String(route.query.user || '')
      if (userID) {
        await openDialog(userID)
        if (String(route.query.compose || '') === '1') startCompose(userID)
        void router.replace({ path: route.path })
      }
    })

    return {
      dialogs,
      filteredDialogs,
      unread,
      loading,
      searchQuery,
      onlyUnanswered,
      selectedUserID,
      recipient,
      messages,
      dialogLoading,
      messagesBox,
      replyText,
      sending,
      activeThreadID,
      broadcastOpen,
      broadcastKind,
      broadcastRole,
      broadcastSubject,
      broadcastBody,
      broadcastMessage,
      broadcastFailed,
      sendBroadcast,
      composeOpen,
      composeUser,
      composeSubject,
      composeBody,
      composeError,
      userQuery,
      userResults,
      userSearching,
      loadDialogs,
      openDialog,
      closeDialog,
      sendReply,
      startCompose,
      searchUsers,
      pickUser,
      send,
      isThreadStart,
      initials,
      formatTime,
      formatDateTime,
    }
  },
})
</script>

<style scoped>
.admin-mail {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.mail-page-header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
  flex-wrap: wrap;
}

.page-title {
  font-size: 20px;
  font-weight: 600;
  color: #111827;
  margin: 0;
}

.header-unread {
  background: #fef3c7;
  color: #b45309;
  border-radius: 999px;
  padding: 2px 10px;
  font-size: 12px;
  font-weight: 600;
}

.btn-primary,
.btn-secondary,
.btn-ghost {
  border: none;
  border-radius: 10px;
  padding: 8px 14px;
  font-size: 14px;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.btn-primary {
  background: #2563eb;
  color: #fff;
}

.mail-page-header .btn-secondary {
  margin-left: auto;
}

.btn-primary:disabled {
  background: #cbd5e1;
  cursor: default;
}

.btn-secondary {
  background: #f3f4f6;
  color: #4b5563;
}

.btn-ghost {
  background: transparent;
  color: #6b7280;
  margin-left: auto;
}

.mail-container {
  display: flex;
  gap: 12px;
  flex: 1;
  min-height: 0;
  height: 70vh;
}

.dialog-list-pane {
  width: 340px;
  background: #fff;
  border: 1px solid #eef0f4;
  border-radius: 12px;
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.pane-header {
  padding: 10px;
  border-bottom: 1px solid #eef0f4;
}

.search-box {
  display: flex;
  align-items: center;
  gap: 6px;
  background: #f6f7fb;
  border-radius: 8px;
  padding: 6px 10px;
  color: #9ca3af;
}

.search-box input {
  border: none;
  background: none;
  outline: none;
  font-size: 14px;
  width: 100%;
  color: #111827;
}

.filter-toggle {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: #6b7280;
  margin-top: 8px;
  cursor: pointer;
}

.dialog-list {
  overflow-y: auto;
  flex: 1;
}

.dialog-item {
  display: flex;
  gap: 10px;
  padding: 10px;
  cursor: pointer;
  border-bottom: 1px solid #f4f5f8;
}

.dialog-item:hover {
  background: #f9fafb;
}

.dialog-item.active {
  background: #eff6ff;
}

.d-avatar {
  width: 38px;
  height: 38px;
  border-radius: 50%;
  background: #e5e7eb;
  color: #4b5563;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 13px;
  font-weight: 600;
  flex-shrink: 0;
}

.d-avatar.executor {
  background: #dcfce7;
  color: #15803d;
}

.d-avatar.customer {
  background: #dbeafe;
  color: #1d4ed8;
}

.d-info {
  min-width: 0;
  flex: 1;
}

.d-top,
.d-bottom {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  align-items: center;
}

.d-name {
  font-weight: 600;
  font-size: 14px;
  color: #111827;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.d-time {
  font-size: 11px;
  color: #9ca3af;
  white-space: nowrap;
}

.d-meta {
  display: flex;
  gap: 8px;
  align-items: center;
  margin: 2px 0;
}

.d-role {
  font-size: 10px;
  text-transform: uppercase;
  background: #f3f4f6;
  color: #6b7280;
  border-radius: 4px;
  padding: 1px 5px;
}

.d-phone {
  font-size: 11px;
  color: #9ca3af;
}

.d-last {
  font-size: 12px;
  color: #6b7280;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.d-last.bold {
  color: #111827;
  font-weight: 600;
}

.out-mark {
  color: #9ca3af;
}

.d-unread {
  background: #f59e0b;
  color: #fff;
  font-size: 11px;
  font-weight: 700;
  border-radius: 999px;
  padding: 1px 7px;
  flex-shrink: 0;
}

.dialog-pane {
  flex: 1;
  background: #fff;
  border: 1px solid #eef0f4;
  border-radius: 12px;
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.dialog-header {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  border-bottom: 1px solid #eef0f4;
}

.btn-back {
  border: none;
  background: none;
  color: #6b7280;
  cursor: pointer;
  font-size: 16px;
  display: none;
}

.dialog-user-name {
  font-weight: 600;
  color: #111827;
}

.dialog-user-phone {
  font-size: 12px;
  color: #9ca3af;
}

.dialog-messages {
  flex: 1;
  overflow-y: auto;
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.thread-divider {
  text-align: center;
  font-size: 12px;
  color: #6b7280;
  margin: 6px 0;
  position: relative;
}

.thread-divider span {
  background: #f3f4f6;
  border-radius: 999px;
  padding: 3px 12px;
}

.bubble {
  max-width: 76%;
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
  flex-wrap: wrap;
}

.bubble-date,
.bubble-status {
  color: #9ca3af;
}

.bubble-text {
  white-space: pre-wrap;
  word-break: break-word;
}

.reply-bar {
  display: flex;
  gap: 8px;
  padding: 10px 14px;
  border-top: 1px solid #eef0f4;
  align-items: flex-end;
}

.reply-input,
.field-input {
  flex: 1;
  border: 1px solid #e5e7eb;
  border-radius: 10px;
  padding: 10px 12px;
  font-size: 14px;
  font-family: inherit;
  resize: none;
  outline: none;
  width: 100%;
}

.reply-input:focus,
.field-input:focus {
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

.empty-state {
  padding: 24px;
  text-align: center;
  color: #9ca3af;
  font-size: 14px;
}

.pane-placeholder {
  margin: auto;
}

/* --- Новое письмо --- */

.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(15, 23, 42, 0.45);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 70;
  padding: 16px;
}

.modal {
  background: #fff;
  border-radius: 14px;
  width: 100%;
  max-width: 520px;
  max-height: 88vh;
  display: flex;
  flex-direction: column;
}

.modal-head {
  display: flex;
  align-items: center;
  padding: 14px 16px;
  border-bottom: 1px solid #eef0f4;
  font-weight: 600;
  color: #111827;
}

.btn-close {
  margin-left: auto;
  border: none;
  background: none;
  color: #9ca3af;
  font-size: 18px;
  cursor: pointer;
}

.modal-body {
  padding: 14px 16px;
  overflow-y: auto;
}

.modal-foot {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
  padding: 12px 16px;
  border-top: 1px solid #eef0f4;
}

.modal-foot .btn-primary {
  margin-left: 0;
}

.field-label {
  display: block;
  font-size: 13px;
  color: #6b7280;
  margin: 10px 0 4px;
}

.field-note {
  font-size: 12px;
  color: #9ca3af;
  margin-top: 6px;
}

.field-note.error {
  color: #b91c1c;
}

.compose-recipient {
  display: flex;
  align-items: center;
  gap: 8px;
  background: #f6f7fb;
  border-radius: 10px;
  padding: 10px 12px;
  font-size: 14px;
  color: #111827;
}

.compose-phone {
  color: #9ca3af;
  font-size: 12px;
}

.btn-link {
  border: none;
  background: none;
  color: #2563eb;
  font-size: 13px;
  cursor: pointer;
  margin-left: auto;
}

.user-results {
  border: 1px solid #eef0f4;
  border-radius: 10px;
  margin-top: 6px;
  overflow: hidden;
}

.user-result {
  display: flex;
  gap: 8px;
  align-items: center;
  width: 100%;
  border: none;
  background: #fff;
  padding: 8px 10px;
  cursor: pointer;
  font-size: 13px;
  text-align: left;
}

.user-result:hover {
  background: #f9fafb;
}

.user-name {
  font-weight: 600;
  color: #111827;
}

.user-phone {
  color: #6b7280;
}

.user-role {
  margin-left: auto;
  font-size: 10px;
  text-transform: uppercase;
  color: #9ca3af;
}

@media (max-width: 900px) {
  .mail-container {
    height: calc(100vh - 190px);
  }

  .dialog-list-pane {
    width: 100%;
  }

  .mobile-hidden {
    display: none;
  }

  .btn-back {
    display: inline-flex;
  }
}
</style>
