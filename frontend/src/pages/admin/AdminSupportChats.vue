<template>
  <div class="admin-support-chats">
    <!-- Header -->
    <div class="chat-page-header">
      <h1 class="page-title">Диалоги с клиентами</h1>
    </div>

    <!-- Chat Application Container -->
    <div class="chat-container">
      <!-- Left Sidebar: Contacts List -->
      <div :class="['chat-contacts', { 'mobile-hidden': selectedChat }]">
        <div class="contacts-header">
          <div class="search-box">
            <i class="ph ph-magnifying-glass"></i>
            <input
              v-model="searchQuery"
              type="text"
              placeholder="Поиск по ФИО или телефону..."
            />
          </div>
        </div>

        <div class="contacts-list">
          <div v-if="loading" class="empty-state">
            <div class="spinner-sm mb-2"></div>
            <span>Загрузка чатов...</span>
          </div>
          <div v-else-if="filteredChats.length === 0" class="empty-state">
            Чатов не найдено
          </div>
          <div
            v-for="c in filteredChats"
            :key="c.chat_id"
            :class="['contact-item', { active: selectedChat && selectedChat.chat_id === c.chat_id }]"
            @click="selectChat(c)"
          >
            <div class="c-avatar" :class="getAvatarClass(c.role)">
              {{ getInitials(c) }}
            </div>
            <div class="c-info">
              <div class="c-top-row">
                <span class="c-name" :title="c.full_name">{{ c.full_name }}</span>
                <span v-if="c.last_time" class="c-time">{{ formatTime(c.last_time) }}</span>
              </div>
              <div class="c-mid-row">
                <span class="c-role" :class="c.role.toLowerCase()">{{ c.role }}</span>
                <span class="c-phone">
                  <i class="ph-fill ph-phone"></i> {{ c.phone }}
                </span>
              </div>
              <div class="c-bottom-row">
                <div class="c-msg" :class="{ 'has-unread': c.unread_count > 0 }">
                  {{ c.last_message || 'Нет сообщений' }}
                </div>
                <span v-if="c.unread_count > 0" class="unread-badge">{{ c.unread_count }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- Right Area: Active Chat -->
      <div :class="['chat-main', { 'mobile-hidden': !selectedChat }]">
        <template v-if="selectedChat">
          <!-- Active User Header -->
          <div class="chat-header">
            <button
              type="button"
              class="btn-back-chat"
              title="К списку чатов"
              @click="selectedChat = null"
            >
              <i class="ph-bold ph-arrow-left"></i>
            </button>

            <div class="chat-user-info">
              <div class="c-avatar header-avatar" :class="getAvatarClass(selectedChat.role)">
                {{ getInitials(selectedChat) }}
              </div>
              <div class="chat-user-text">
                <div class="chat-user-name">{{ selectedChat.full_name }}</div>
                <div class="chat-user-meta">
                  <span class="c-role" :class="selectedChat.role.toLowerCase()">{{ selectedChat.role }}</span>
                  <span class="c-phone-meta">
                    <i class="ph-fill ph-phone"></i> {{ selectedChat.phone }}
                  </span>
                  <span v-if="selectedChat.is_banned" class="ban-badge">
                    <i class="ph-bold ph-prohibit"></i> {{ formatBanText(selectedChat.banned_until) }}
                  </span>
                </div>
              </div>
            </div>

            <!-- Kebab Menu for Actions (Ban / Unban) -->
            <div class="dropdown-wrapper">
              <button
                type="button"
                class="btn-ghost"
                title="Управление пользователем"
                @click.stop="dropdownOpen = !dropdownOpen"
              >
                <i class="ph-bold ph-dots-three-vertical"></i>
              </button>

              <div v-if="dropdownOpen" class="dropdown-menu" @click.stop>
                <template v-if="selectedChat.is_banned">
                  <div class="dropdown-label">{{ formatBanText(selectedChat.banned_until) }}</div>
                  <button
                    type="button"
                    class="dropdown-item"
                    @click="unbanChat(selectedChat); dropdownOpen = false"
                  >
                    <i class="ph-bold ph-lock-key-open"></i> Разбанить
                  </button>
                </template>
                <template v-else>
                  <div class="dropdown-label">Управление доступом</div>
                  <button
                    type="button"
                    class="dropdown-item danger"
                    @click="banChat(selectedChat, '10m'); dropdownOpen = false"
                  >
                    <i class="ph-bold ph-clock-countdown"></i> Бан на 10 минут
                  </button>
                  <button
                    type="button"
                    class="dropdown-item danger"
                    @click="banChat(selectedChat, '1h'); dropdownOpen = false"
                  >
                    <i class="ph-bold ph-hourglass-high"></i> Бан на 1 час
                  </button>
                  <button
                    type="button"
                    class="dropdown-item danger"
                    @click="banChat(selectedChat, 'forever'); dropdownOpen = false"
                  >
                    <i class="ph-bold ph-prohibit"></i> Заблокировать навсегда
                  </button>
                </template>
              </div>
            </div>
          </div>

          <!-- Messages Scroll Area -->
          <div ref="messagesContainerRef" class="chat-history">
            <div v-if="messagesLoading" class="empty-history-state">
              <div class="spinner-sm mb-2"></div>
              <span>Загрузка сообщений...</span>
            </div>
            <div v-else-if="messages.length === 0" class="empty-history-state">
              <i class="ph-fill ph-chat-teardrop-text empty-icon"></i>
              <p>История сообщений пуста. Напишите сообщение первыми.</p>
            </div>
            <template v-else>
              <!-- The server returns the most recent page; older history is
                   fetched on request rather than on every poll. -->
              <div v-if="canLoadOlder" class="load-older-row">
                <button type="button" :disabled="loadingOlder" @click="loadOlderMessages">
                  {{ loadingOlder ? 'Загрузка...' : 'Показать более ранние сообщения' }}
                </button>
              </div>
              <div
                v-for="msg in messages"
                :key="msg.id"
                :class="['msg-row', msg.sender_id === currentUserId ? 'out' : 'in']"
              >
                <div class="msg-bubble">
                  <div class="msg-sender-title">
                    {{ msg.sender_id === currentUserId ? 'Поддержка' : selectedChat.full_name }}
                  </div>

                  <!-- Media attachment -->
                  <div v-if="msg.file_url" class="msg-img-box mb-2">
                    <img
                      v-if="msg.file_type === 'image'"
                      :src="resolveUrl(msg.file_url)"
                      class="chat-img-thumb"
                      alt="Attachment"
                      @click="previewImage(resolveUrl(msg.file_url))"
                    />
                    <a v-else :href="resolveUrl(msg.file_url)" target="_blank" download class="file-download-link">
                      <i class="ph-fill ph-file-text me-1"></i> {{ msg.file_name || 'Скачать файл' }}
                    </a>
                  </div>

                  <div class="msg-text-content">{{ msg.text }}</div>

                  <div class="msg-time">
                    <span>{{ formatTime(msg.created_at) }}</span>
                    <i v-if="msg.sender_id === currentUserId" class="ph-bold ph-check-double status-check"></i>
                  </div>
                </div>
              </div>
            </template>
          </div>

          <!-- Input Area -->
          <div class="chat-input-area">
            <input
              type="file"
              ref="fileInputRef"
              style="display: none"
              @change="handleFileUpload"
            />
            <button
              type="button"
              class="btn-attach"
              title="Прикрепить файл"
              @click="triggerFileInput"
            >
              <i class="ph-bold ph-paperclip"></i>
            </button>
            <input
              v-model="inputText"
              type="text"
              class="input-field"
              placeholder="Напишите сообщение..."
              @keyup.enter="sendMessage"
            />
            <button
              type="button"
              class="btn-send"
              :disabled="(!inputText.trim() && !uploading) || selectedChat.is_banned"
              @click="sendMessage"
            >
              <div v-if="uploading" class="spinner-sm light"></div>
              <i v-else class="ph-fill ph-paper-plane-right"></i>
            </button>
          </div>
        </template>

        <div v-else class="no-chat-selected">
          <i class="ph-fill ph-chats-teardrop font-size-huge"></i>
          <h3>Выберите диалог слева</h3>
          <p>Центр поддержки пользователей в реальном времени</p>
        </div>
      </div>
    </div>

    <!-- Image Preview Modal -->
    <div v-if="showImageModal" class="img-modal-overlay" @click="showImageModal = false">
      <div class="img-modal-card" @click.stop>
        <button class="btn-close-modal" @click="showImageModal = false"><i class="ph ph-x"></i></button>
        <img :src="previewUrl" alt="Preview" class="full-img" />
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, computed, onMounted, onUnmounted, nextTick } from 'vue'
import api, { resolveFileUrl } from '../../services/api'
import { useAuthStore } from '../../stores/auth-store'

export default defineComponent({
  name: 'AdminSupportChats',
  setup() {
    const authStore = useAuthStore()
    const currentUserId = computed(() => authStore.userID)

    const chats = ref<any[]>([])
    const selectedChat = ref<any>(null)
    const messages = ref<any[]>([])
    const searchQuery = ref('')
    const inputText = ref('')

    const loading = ref(false)
    const messagesLoading = ref(false)

    // Mirrors repository.DefaultMessagePageSize: a full page back means there
    // is probably more history above it.
    const PAGE_SIZE = 100
    const hasOlder = ref(false)
    const loadingOlder = ref(false)
    const canLoadOlder = computed(() => hasOlder.value && messages.value.length > 0)
    const uploading = ref(false)
    const dropdownOpen = ref(false)
    const messagesContainerRef = ref<any>(null)
    const fileInputRef = ref<HTMLInputElement | null>(null)

    const showImageModal = ref(false)
    const previewUrl = ref('')

    let pollInterval: any = null

    const closeDropdown = () => {
      dropdownOpen.value = false
    }

    const fetchChats = async () => {
      try {
        const res = await api.get('/admin/support/chats')
        if (res.data) {
          chats.value = res.data
        }
      } catch (err) {
        console.error('Failed to load support chats:', err)
      }
    }

    const filteredChats = computed(() => {
      if (!searchQuery.value.trim()) return chats.value
      const q = searchQuery.value.toLowerCase()
      return chats.value.filter(c =>
        (c.full_name && c.full_name.toLowerCase().includes(q)) ||
        (c.phone && c.phone.includes(q)) ||
        (c.last_message && c.last_message.toLowerCase().includes(q))
      )
    })

    // Incremental refresh of the conversation on screen: ask for what arrived
    // after the newest message already loaded, and append it. Re-reading the
    // whole conversation on a timer made the cost of an open admin tab grow
    // with the length of the conversation it happened to be showing.
    const pollOpenConversation = async () => {
      const chat = selectedChat.value
      if (!chat) return
      const newest = messages.value[messages.value.length - 1]
      try {
        const res = await api.get(`/support/chats/${chat.chat_id}/messages`, {
          params: newest?.created_at ? { after: newest.created_at } : {},
        })
        const incoming = res.data || []
        if (!incoming.length) return
        // Without a reference point this was a full read, so it replaces
        // rather than appends.
        messages.value = newest?.created_at ? [...messages.value, ...incoming] : incoming
        scrollToBottom()
      } catch (err) {
        console.error('Failed to refresh support messages:', err)
      }
    }

    // Fetch the page above what is on screen and prepend it, holding the
    // reader's position instead of jumping.
    const loadOlderMessages = async () => {
      const chat = selectedChat.value
      if (!chat || loadingOlder.value) return
      const oldest = messages.value[0]
      if (!oldest?.created_at) return

      loadingOlder.value = true
      const container = messagesContainerRef.value
      const heightBefore = container ? container.scrollHeight : 0
      try {
        const res = await api.get(`/support/chats/${chat.chat_id}/messages`, {
          params: { before: oldest.created_at },
        })
        const older = res.data || []
        hasOlder.value = older.length >= PAGE_SIZE
        if (older.length) {
          messages.value = [...older, ...messages.value]
          nextTick(() => {
            if (container) {
              container.scrollTop = container.scrollHeight - heightBefore
            }
          })
        }
      } catch (err) {
        console.error('Failed to load older support messages:', err)
      } finally {
        loadingOlder.value = false
      }
    }

    const selectChat = async (chat: any) => {
      selectedChat.value = chat
      messagesLoading.value = true
      dropdownOpen.value = false
      try {
        const res = await api.get(`/support/chats/${chat.chat_id}/messages`)
        messages.value = res.data || []
        hasOlder.value = messages.value.length >= PAGE_SIZE
        chat.unread_count = 0
        window.dispatchEvent(new Event('support-unread-updated'))
        scrollToBottom()
      } catch (err) {
        console.error('Failed to load support messages:', err)
      } finally {
        messagesLoading.value = false
      }
    }

    const sendMessage = async () => {
      if (!inputText.value.trim() || !selectedChat.value) return
      const text = inputText.value.trim()
      inputText.value = ''
      try {
        const res = await api.post(`/support/chats/${selectedChat.value.chat_id}/messages`, { text })
        if (res.data) {
          messages.value.push(res.data)
          selectedChat.value.last_message = text
          selectedChat.value.last_time = new Date().toISOString()
          scrollToBottom()
        }
      } catch (err) {
        console.error('Failed to send message:', err)
      }
    }

    const triggerFileInput = () => {
      fileInputRef.value?.click()
    }

    const handleFileUpload = async (event: Event) => {
      const target = event.target as HTMLInputElement
      if (!target.files || target.files.length === 0 || !selectedChat.value) return
      const file = target.files[0]
      uploading.value = true
      try {
        const formData = new FormData()
        formData.append('file', file)
        formData.append('text', inputText.value)
        inputText.value = ''

        const res = await api.post(`/support/chats/${selectedChat.value.chat_id}/upload`, formData, {
          headers: { 'Content-Type': 'multipart/form-data' }
        })
        if (res.data) {
          messages.value.push(res.data)
          selectedChat.value.last_message = res.data.file_name || 'Вложение'
          selectedChat.value.last_time = new Date().toISOString()
          scrollToBottom()
        }
      } catch (err) {
        console.error('Upload failed:', err)
      } finally {
        uploading.value = false
        if (fileInputRef.value) fileInputRef.value.value = ''
      }
    }

    const scrollToBottom = () => {
      nextTick(() => {
        if (messagesContainerRef.value) {
          messagesContainerRef.value.scrollTop = messagesContainerRef.value.scrollHeight
        }
      })
    }

    const getInitials = (user: any) => {
      if (!user) return '?'
      if (user.first_name || user.last_name) {
        return (user.last_name ? user.last_name[0] : '') + (user.first_name ? user.first_name[0] : '')
      }
      return user.phone ? user.phone.slice(-2) : '?'
    }

    const getAvatarClass = (role: string) => {
      return (role || '').toUpperCase() === 'EXECUTOR' ? 'executor' : 'customer'
    }

    const formatTime = (iso: string) => {
      if (!iso) return ''
      const d = new Date(iso)
      const now = new Date()
      if (d.toDateString() === now.toDateString()) {
        return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
      }
      return d.toLocaleDateString([], { day: '2-digit', month: '2-digit' })
    }

    const resolveUrl = (url: string) => resolveFileUrl(url)

    const previewImage = (url: string) => {
      previewUrl.value = url
      showImageModal.value = true
    }

    const banChat = async (chat: any, duration: string) => {
      if (!chat) return
      try {
        await api.post(`/admin/support/chats/${chat.chat_id}/ban`, { duration })
        chat.is_banned = true
        if (duration === '10m') {
          chat.banned_until = new Date(Date.now() + 10 * 60 * 1000).toISOString()
        } else if (duration === '1h') {
          chat.banned_until = new Date(Date.now() + 60 * 60 * 1000).toISOString()
        } else {
          chat.banned_until = new Date(Date.now() + 100 * 365 * 24 * 60 * 60 * 1000).toISOString()
        }
        await fetchChats()
      } catch (err) {
        console.error('Failed to ban chat:', err)
      }
    }

    const unbanChat = async (chat: any) => {
      if (!chat) return
      try {
        await api.post(`/admin/support/chats/${chat.chat_id}/unban`)
        chat.is_banned = false
        chat.banned_until = null
        await fetchChats()
      } catch (err) {
        console.error('Failed to unban chat:', err)
      }
    }

    const formatBanText = (untilISO?: string) => {
      if (!untilISO) return 'Заблокирован'
      const d = new Date(untilISO)
      const now = new Date()
      if (d.getFullYear() - now.getFullYear() > 10) return 'Заблокирован навсегда'
      return `Забанен до ${d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`
    }

    onMounted(() => {
      fetchChats()
      window.addEventListener('click', closeDropdown)
      // The chat list is expensive per row (last message and unread count are
      // resolved per conversation), so it refreshes on a slow timer. The open
      // conversation stays responsive because it asks only for messages newer
      // than the last one on screen, which costs almost nothing.
      pollInterval = setInterval(() => {
        fetchChats()
        pollOpenConversation()
      }, 15000)
    })

    onUnmounted(() => {
      window.removeEventListener('click', closeDropdown)
      if (pollInterval) clearInterval(pollInterval)
    })

    return {
      chats,
      filteredChats,
      selectedChat,
      messages,
      searchQuery,
      inputText,
      loading,
      messagesLoading,
      canLoadOlder,
      loadingOlder,
      loadOlderMessages,
      uploading,
      dropdownOpen,
      messagesContainerRef,
      fileInputRef,
      currentUserId,
      showImageModal,
      previewUrl,
      selectChat,
      sendMessage,
      triggerFileInput,
      handleFileUpload,
      getInitials,
      getAvatarClass,
      formatTime,
      resolveUrl,
      previewImage,
      banChat,
      unbanChat,
      formatBanText,
    }
  }
})
</script>

<style scoped>
.admin-support-chats {
  height: calc(100vh - 135px);
  display: flex;
  flex-direction: column;
  gap: 16px;
  font-family: 'Outfit', sans-serif;
  color: #0f172a;
}

.chat-page-header {
  flex-shrink: 0;
}

.page-title {
  font-size: 24px;
  font-weight: 700;
  color: #0f172a;
  margin: 0;
  letter-spacing: -0.5px;
}

/* Chat Application Container */
.chat-container {
  background: #ffffff;
  border-radius: 24px;
  box-shadow: 0 4px 24px rgba(15, 23, 42, 0.04);
  display: flex;
  flex: 1;
  min-height: 0;
  overflow: hidden;
  border: 1px solid rgba(0, 0, 0, 0.04);
}

/* Left Sidebar: Contacts */
.chat-contacts {
  width: 360px;
  border-right: 1px solid rgba(0, 0, 0, 0.06);
  display: flex;
  flex-direction: column;
  background: #ffffff;
  flex-shrink: 0;
}

.contacts-header {
  padding: 20px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.04);
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.search-box {
  display: flex;
  align-items: center;
  gap: 10px;
  background: #f1f5f9;
  border-radius: 12px;
  padding: 10px 14px;
}

.search-box i {
  color: #64748b;
  font-size: 18px;
}

.search-box input {
  border: none;
  background: transparent;
  outline: none;
  width: 100%;
  font-family: inherit;
  font-size: 14px;
  color: #0f172a;
}

.contacts-list {
  flex: 1;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
}

/* Contact Item */
.contact-item {
  display: flex;
  gap: 14px;
  padding: 16px 20px;
  cursor: pointer;
  border-bottom: 1px solid rgba(0, 0, 0, 0.02);
  transition: all 0.2s ease-in-out;
  position: relative;
}

.contact-item:hover {
  background: #f8fafc;
}

.contact-item.active {
  background: #eef2ff;
}

.contact-item.active::before {
  content: '';
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 4px;
  background: #5c60f5;
}

.c-avatar {
  width: 48px;
  height: 48px;
  border-radius: 50%;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 16px;
  font-weight: 700;
}

.c-avatar.customer {
  background: #eff6ff;
  color: #3b82f6;
}

.c-avatar.executor {
  background: #ecfdf5;
  color: #10b981;
}

.c-info {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-width: 0;
  gap: 4px;
}

.c-top-row {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
}

.c-name {
  font-size: 15px;
  font-weight: 700;
  color: #0f172a;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.c-time {
  font-size: 12px;
  font-weight: 500;
  color: #64748b;
  flex-shrink: 0;
}

.c-mid-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.c-phone {
  font-family: 'JetBrains Mono', monospace;
  font-size: 12px;
  color: #64748b;
  display: flex;
  align-items: center;
  gap: 4px;
}

.c-role {
  font-size: 10px;
  font-weight: 700;
  padding: 2px 8px;
  border-radius: 99px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.c-role.customer {
  background: #eff6ff;
  color: #3b82f6;
}

.c-role.executor {
  background: #ecfdf5;
  color: #10b981;
}

.c-bottom-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 2px;
}

.c-msg {
  font-size: 13px;
  color: #64748b;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  flex: 1;
}

.c-msg.has-unread {
  color: #5c60f5;
  font-weight: 600;
}

.unread-badge {
  background: #ef4444;
  color: #ffffff;
  font-size: 11px;
  font-weight: 700;
  padding: 2px 8px;
  border-radius: 99px;
  margin-left: 8px;
}

/* Right Main Area */
.chat-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  background: #f8fafc;
}

/* Chat Header */
.chat-header {
  background: #ffffff;
  padding: 16px 28px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.06);
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.chat-user-info {
  display: flex;
  align-items: center;
  gap: 16px;
}

.header-avatar {
  width: 52px;
  height: 52px;
  font-size: 18px;
}

.chat-user-text {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.chat-user-name {
  font-size: 18px;
  font-weight: 700;
  color: #0f172a;
  line-height: 1.1;
}

.chat-user-meta {
  display: flex;
  align-items: center;
  gap: 12px;
}

.c-phone-meta {
  font-family: 'JetBrains Mono', monospace;
  font-size: 13px;
  color: #64748b;
  display: flex;
  align-items: center;
  gap: 4px;
}

.ban-badge {
  font-size: 11px;
  font-weight: 700;
  color: #ef4444;
  background: #fef2f2;
  padding: 2px 8px;
  border-radius: 6px;
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

/* Dropdown Menu for Actions */
.dropdown-wrapper {
  position: relative;
}

.btn-ghost {
  width: 40px;
  height: 40px;
  border-radius: 12px;
  border: none;
  background: transparent;
  color: #64748b;
  font-size: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.2s ease-in-out;
}

.btn-ghost:hover {
  background: #f1f5f9;
  color: #0f172a;
}

.dropdown-menu {
  position: absolute;
  right: 0;
  top: calc(100% + 6px);
  z-index: 100;
  background: rgba(255, 255, 255, 0.98);
  backdrop-filter: blur(12px);
  border: 1px solid rgba(0, 0, 0, 0.08);
  border-radius: 14px;
  padding: 8px;
  box-shadow: 0 10px 40px -10px rgba(15, 23, 42, 0.15), 0 1px 3px rgba(15, 23, 42, 0.05);
  min-width: 230px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.dropdown-label {
  font-size: 11px;
  font-weight: 700;
  color: #64748b;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  padding: 8px 12px 4px 12px;
}

.dropdown-item {
  padding: 10px 12px;
  border-radius: 8px;
  border: none;
  background: transparent;
  font-family: inherit;
  font-size: 14px;
  font-weight: 600;
  color: #0f172a;
  display: flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  transition: all 0.2s ease-in-out;
  text-align: left;
  width: 100%;
}

.dropdown-item i {
  font-size: 18px;
  color: #64748b;
  transition: all 0.2s ease-in-out;
}

.dropdown-item:hover {
  background: #f8fafc;
  color: #5c60f5;
}

.dropdown-item.danger {
  color: #ef4444;
}

.dropdown-item.danger i {
  color: #ef4444;
}

.dropdown-item.danger:hover {
  background: #fef2f2;
  color: #ef4444;
}

/* Chat History Area */
.load-older-row {
  display: flex;
  justify-content: center;
  margin-bottom: 8px;
}

.load-older-row button {
  border: none;
  background: #e2e8f0;
  color: #475569;
  font-size: 13px;
  padding: 6px 14px;
  border-radius: 999px;
  cursor: pointer;
}

.load-older-row button:disabled {
  opacity: 0.6;
  cursor: default;
}

.chat-history {
  flex: 1;
  padding: 28px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.msg-row {
  display: flex;
  width: 100%;
}

.msg-row.in {
  justify-content: flex-start;
}

.msg-row.out {
  justify-content: flex-end;
}

.msg-bubble {
  max-width: 65%;
  padding: 14px 18px;
  font-size: 15px;
  line-height: 1.5;
  position: relative;
  word-break: break-word;
}

.msg-row.in .msg-bubble {
  background: #ffffff;
  color: #0f172a;
  border-radius: 20px 20px 20px 4px;
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.04);
  border: 1px solid rgba(0, 0, 0, 0.03);
}

.msg-row.out .msg-bubble {
  background: #5c60f5;
  color: #ffffff;
  border-radius: 20px 20px 4px 20px;
  box-shadow: 0 4px 16px rgba(92, 96, 245, 0.25);
}

.msg-sender-title {
  font-size: 11px;
  font-weight: 700;
  margin-bottom: 4px;
  opacity: 0.85;
}

.msg-text-content {
  line-height: 1.45;
}

.msg-time {
  font-size: 11px;
  opacity: 0.75;
  margin-top: 6px;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 4px;
  font-family: 'JetBrains Mono', monospace;
  font-weight: 500;
}

.status-check {
  font-size: 12px;
}

.chat-img-thumb {
  max-width: 100%;
  max-height: 220px;
  border-radius: 12px;
  cursor: pointer;
  object-fit: cover;
}

.file-download-link {
  color: inherit;
  font-weight: 600;
  text-decoration: underline;
  display: inline-flex;
  align-items: center;
}

/* Chat Input Area */
.chat-input-area {
  background: #ffffff;
  padding: 16px 28px;
  border-top: 1px solid rgba(0, 0, 0, 0.06);
  display: flex;
  gap: 14px;
  align-items: center;
}

.btn-attach {
  width: 48px;
  height: 48px;
  border-radius: 14px;
  border: 1px solid rgba(0, 0, 0, 0.1);
  background: #ffffff;
  color: #64748b;
  font-size: 22px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.2s ease-in-out;
}

.btn-attach:hover {
  background: #f8fafc;
  color: #0f172a;
}

.input-field {
  flex: 1;
  background: #f1f5f9;
  border-radius: 16px;
  padding: 14px 20px;
  border: 1px solid transparent;
  font-family: inherit;
  font-size: 15px;
  color: #0f172a;
  outline: none;
  transition: all 0.2s ease-in-out;
}

.input-field:focus {
  background: #ffffff;
  border-color: #5c60f5;
  box-shadow: 0 0 0 4px rgba(92, 96, 245, 0.1);
}

.input-field::placeholder {
  color: #94a3b8;
}

.btn-send {
  width: 48px;
  height: 48px;
  border-radius: 14px;
  border: none;
  background: #5c60f5;
  color: #ffffff;
  font-size: 22px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.2s ease-in-out;
  box-shadow: 0 4px 12px rgba(92, 96, 245, 0.2);
}

.btn-send:hover:not(:disabled) {
  transform: translateY(-2px);
  box-shadow: 0 6px 16px rgba(92, 96, 245, 0.3);
}

.btn-send:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* Empty & Loading States */
.no-chat-selected,
.empty-history-state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  color: #94a3b8;
  gap: 12px;
  text-align: center;
  padding: 32px;
}

.font-size-huge {
  font-size: 64px;
  color: #cbd5e1;
}

.empty-state {
  text-align: center;
  color: #94a3b8;
  padding: 32px;
  font-size: 14px;
}

.spinner-sm {
  width: 20px;
  height: 20px;
  border: 2px solid rgba(92, 96, 245, 0.2);
  border-top-color: #5c60f5;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
  display: inline-block;
}

.spinner-sm.light {
  border-color: rgba(255, 255, 255, 0.3);
  border-top-color: #ffffff;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

.btn-back-chat {
  display: none;
  background: #f1f5f9;
  border: none;
  width: 38px;
  height: 38px;
  border-radius: 12px;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  color: #0f172a;
  cursor: pointer;
  margin-right: 8px;
  flex-shrink: 0;
}

/* Image Preview Modal */
.img-modal-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(15, 23, 42, 0.85);
  backdrop-filter: blur(4px);
  z-index: 9999;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
}

.img-modal-card {
  position: relative;
  max-width: 90vw;
  max-height: 90vh;
}

.full-img {
  max-width: 90vw;
  max-height: 90vh;
  border-radius: 16px;
  box-shadow: 0 20px 50px rgba(0, 0, 0, 0.5);
  object-fit: contain;
}

.btn-close-modal {
  position: absolute;
  top: -16px;
  right: -16px;
  width: 36px;
  height: 36px;
  border-radius: 50%;
  background: #ffffff;
  border: none;
  color: #0f172a;
  font-size: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.2);
}

@media (max-width: 1024px) {
  .admin-support-chats {
    height: calc(100vh - 110px);
  }
  .chat-contacts {
    width: 300px;
  }
}

@media (max-width: 768px) {
  .admin-support-chats {
    height: calc(100vh - 90px);
  }
  .chat-container {
    flex-direction: column;
  }
  .chat-contacts {
    width: 100% !important;
    height: 100%;
  }
  .chat-main {
    width: 100% !important;
    height: 100%;
  }
  .chat-contacts.mobile-hidden,
  .chat-main.mobile-hidden {
    display: none !important;
  }
  .btn-back-chat {
    display: flex;
  }
  .msg-bubble {
    max-width: 85%;
  }
  .chat-header {
    padding: 12px 16px;
  }
  .chat-history {
    padding: 16px;
  }
  .chat-input-area {
    padding: 12px 16px;
    gap: 8px;
  }
}
</style>
