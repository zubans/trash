<template>
  <div class="admin-support-chats">
    <div class="telegram-container">
      <!-- Left Sidebar: User Chat List -->
      <div :class="['chat-sidebar', { 'mobile-hidden': selectedChat }]">
        <div class="sidebar-header">
          <h2 class="sidebar-title">Диалоги с клиентами</h2>
          <div class="search-box">
            <i class="ph ph-magnifying-glass"></i>
            <input
              v-model="searchQuery"
              type="text"
              placeholder="Поиск по ФИО или телефону..."
            />
          </div>
        </div>

        <div class="chat-list">
          <div v-if="loading" class="empty-state">
            <div class="spinner-sm"></div> Загрузка чатов...
          </div>
          <div v-else-if="filteredChats.length === 0" class="empty-state">
            Чатов не найдено
          </div>
          <div
            v-for="c in filteredChats"
            :key="c.chat_id"
            :class="['chat-item', { active: selectedChat && selectedChat.chat_id === c.chat_id }]"
            @click="selectChat(c)"
          >
            <div class="avatar" :class="getAvatarClass(c.role)">
              {{ getInitials(c) }}
            </div>
            <div class="chat-info">
              <div class="chat-top-row">
                <span class="user-fullname" :title="c.full_name">{{ c.full_name }}</span>
                <span v-if="c.last_time" class="chat-time">{{ formatTime(c.last_time) }}</span>
              </div>
              <div class="chat-sub-row">
                <span class="user-phone"><i class="ph-fill ph-phone me-1"></i>{{ c.phone }}</span>
                <span class="role-badge" :class="c.role.toLowerCase()">{{ c.role }}</span>
              </div>
              <div class="chat-bottom-row">
                <span class="last-msg-text">{{ c.last_message || 'Нет сообщений' }}</span>
                <span v-if="c.unread_count > 0" class="unread-badge">{{ c.unread_count }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- Right Panel: Active Dialogue -->
      <div :class="['chat-main', { 'mobile-hidden': !selectedChat }]">
        <template v-if="selectedChat">
          <!-- Chat Header -->
          <div class="main-header">
            <button type="button" class="btn-back-chat" title="К списку чатов" @click="selectedChat = null">
              <i class="ph-bold ph-arrow-left"></i>
            </button>
            <div class="user-meta-header">
              <div class="avatar" :class="getAvatarClass(selectedChat.role)">
                {{ getInitials(selectedChat) }}
              </div>
              <div>
                <div class="header-name">{{ selectedChat.full_name }}</div>
                <div class="header-details">
                  <span><i class="ph-fill ph-phone me-1"></i>{{ selectedChat.phone }}</span>
                  <span class="role-badge ms-2" :class="selectedChat.role.toLowerCase()">{{ selectedChat.role }}</span>
                  <span v-if="selectedChat.is_banned" class="ban-badge ms-2">
                    <i class="ph-bold ph-prohibit me-1"></i>
                    {{ formatBanText(selectedChat.banned_until) }}
                  </span>
                </div>
              </div>
            </div>

            <!-- Ban Controls -->
            <div class="header-ban-actions">
              <template v-if="selectedChat.is_banned">
                <button type="button" class="btn-ban-action btn-unban" title="Снять блокировку" @click="unbanChat(selectedChat)">
                  <i class="ph-bold ph-lock-key-open me-1"></i> Разбанить
                </button>
              </template>
              <template v-else>
                <span class="ban-label me-1">Бан:</span>
                <button type="button" class="btn-ban-action" title="Заблокировать на 10 минут" @click="banChat(selectedChat, '10m')">
                  10 мин
                </button>
                <button type="button" class="btn-ban-action" title="Заблокировать на 1 час" @click="banChat(selectedChat, '1h')">
                  1 час
                </button>
                <button type="button" class="btn-ban-action btn-ban-forever" title="Заблокировать навсегда" @click="banChat(selectedChat, 'forever')">
                  Навсегда
                </button>
              </template>
            </div>
          </div>

          <!-- Messages Scroll Area -->
          <div ref="messagesContainerRef" class="messages-area">
            <div v-if="messagesLoading" class="empty-state">
              <div class="spinner-sm"></div> Загрузка сообщений...
            </div>
            <div v-else-if="messages.length === 0" class="empty-state">
              История сообщений пуста. Напишите сообщение первыми.
            </div>
            <template v-else>
              <div
                v-for="msg in messages"
                :key="msg.id"
                :class="['msg-wrapper', msg.sender_id === currentUserId ? 'out' : 'in']"
              >
                <div class="msg-bubble">
                  <div class="msg-sender-name">
                    {{ msg.sender_id === currentUserId ? 'Администратор' : selectedChat.full_name }}
                  </div>
                  <!-- Media attachment -->
                  <div v-if="msg.file_url" class="attachment-box mb-2">
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
                  <div class="msg-text">{{ msg.text }}</div>
                  <div class="msg-meta">
                    <span class="msg-time">{{ formatTime(msg.created_at) }}</span>
                    <i v-if="msg.sender_id === currentUserId" class="ph-bold ph-check msg-status"></i>
                  </div>
                </div>
              </div>
            </template>
          </div>

          <!-- Input Footer -->
          <div class="main-footer">
            <input
              type="file"
              ref="fileInputRef"
              style="display: none"
              @change="handleFileUpload"
            />
            <button type="button" class="btn-attach" title="Прикрепить файл" @click="triggerFileInput">
              <i class="ph-bold ph-paperclip"></i>
            </button>
            <input
              v-model="inputText"
              type="text"
              class="chat-input"
              placeholder="Напишите сообщение..."
              @keyup.enter="sendMessage"
            />
            <button type="button" class="btn-send" :disabled="!inputText.trim() && !uploading" @click="sendMessage">
              <span v-if="uploading" class="spinner-sm"></span>
              <i v-else class="ph-bold ph-paper-plane-tilt"></i>
            </button>
          </div>
        </template>

        <div v-else class="no-chat-selected">
          <i class="ph-fill ph-chats-teardrop font-size-huge"></i>
          <h3>Выберите диалог слева</h3>
          <p>Полноценный клиент поддержки пользователей в стиле Telegram</p>
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
    const uploading = ref(false)
    const messagesContainerRef = ref<any>(null)
    const fileInputRef = ref<HTMLInputElement | null>(null)
    
    const showImageModal = ref(false)
    const previewUrl = ref('')

    let pollInterval: any = null

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

    const selectChat = async (chat: any) => {
      selectedChat.value = chat
      messagesLoading.value = true
      try {
        const res = await api.get(`/support/chats/${chat.chat_id}/messages`)
        messages.value = res.data || []
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
      return role === 'EXECUTOR' ? 'avatar-executor' : 'avatar-customer'
    }

    const formatTime = (iso: string) => {
      if (!iso) return ''
      const d = new Date(iso)
      return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
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
      pollInterval = setInterval(() => {
        fetchChats()
        if (selectedChat.value) {
          api.get(`/support/chats/${selectedChat.value.chat_id}/messages`).then(res => {
            if (res.data) {
              messages.value = res.data
            }
          })
        }
      }, 5000)
    })

    onUnmounted(() => {
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
      uploading,
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
  height: calc(100vh - 120px);
  display: flex;
  flex-direction: column;
}

.telegram-container {
  display: flex;
  height: 100%;
  background: var(--surface-card, #ffffff);
  border-radius: 20px;
  box-shadow: var(--shadow-card, 0 10px 30px rgba(0, 0, 0, 0.05));
  border: 1px solid rgba(0, 0, 0, 0.08);
  overflow: hidden;
}

/* Left Sidebar */
.chat-sidebar {
  width: 380px;
  border-right: 1px solid rgba(0, 0, 0, 0.08);
  display: flex;
  flex-direction: column;
  background: #f8fafc;
}

.sidebar-header {
  padding: 20px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.06);
  background: #ffffff;
}

.sidebar-title {
  font-size: 18px;
  font-weight: 700;
  color: var(--text-title, #0f172a);
  margin-bottom: 12px;
}

.search-box {
  position: relative;
  display: flex;
  align-items: center;
}

.search-box i {
  position: absolute;
  left: 14px;
  color: #94a3b8;
  font-size: 16px;
}

.search-box input {
  width: 100%;
  padding: 10px 14px 10px 38px;
  border-radius: 12px;
  border: 1px solid rgba(0, 0, 0, 0.08);
  background: #f1f5f9;
  font-size: 13px;
  outline: none;
}

.chat-list {
  flex: 1;
  overflow-y: auto;
}

.chat-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 16px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.04);
  cursor: pointer;
  transition: all 0.2s ease;
}

.chat-item:hover {
  background: #eef2ff;
}

.chat-item.active {
  background: #e0e7ff;
  border-left: 4px solid #5c60f5;
}

.avatar {
  width: 46px;
  height: 46px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 700;
  font-size: 15px;
  color: #ffffff;
  flex-shrink: 0;
}

.avatar-customer { background: linear-gradient(135deg, #3b82f6, #1d4ed8); }
.avatar-executor { background: linear-gradient(135deg, #10b981, #047857); }

.chat-info {
  flex: 1;
  min-width: 0;
}

.chat-top-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 2px;
}

.user-fullname {
  font-size: 14px;
  font-weight: 700;
  color: #0f172a;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.chat-time {
  font-size: 11px;
  color: #94a3b8;
}

.chat-sub-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 4px;
}

.user-phone {
  font-size: 12px;
  color: #64748b;
}

.role-badge {
  font-size: 10px;
  font-weight: 700;
  padding: 2px 6px;
  border-radius: 6px;
}

.role-badge.customer { background: #dbeafe; color: #1e40af; }
.role-badge.executor { background: #fef3c7; color: #d97706; }

.chat-bottom-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.last-msg-text {
  font-size: 12px;
  color: #64748b;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  flex: 1;
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

/* Right Main Panel */
.chat-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  background: #ffffff;
}

.main-header {
  padding: 16px 24px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.08);
  background: #ffffff;
}

.user-meta-header {
  display: flex;
  align-items: center;
  gap: 14px;
}

.header-name {
  font-size: 16px;
  font-weight: 700;
  color: #0f172a;
}

.header-details {
  font-size: 13px;
  color: #64748b;
  display: flex;
  align-items: center;
}

.messages-area {
  flex: 1;
  padding: 24px;
  overflow-y: auto;
  background: #f1f5f9;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.msg-wrapper {
  display: flex;
  flex-direction: column;
}

.msg-wrapper.out { align-items: flex-end; }
.msg-wrapper.in { align-items: flex-start; }

.msg-bubble {
  max-width: 65%;
  padding: 12px 16px;
  border-radius: 16px;
  position: relative;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.04);
}

.msg-wrapper.out .msg-bubble {
  background: #5c60f5;
  color: #ffffff;
  border-bottom-right-radius: 4px;
}

.msg-wrapper.in .msg-bubble {
  background: #ffffff;
  color: #0f172a;
  border-bottom-left-radius: 4px;
  border: 1px solid rgba(0, 0, 0, 0.06);
}

.msg-sender-name {
  font-size: 11px;
  font-weight: 700;
  margin-bottom: 4px;
  opacity: 0.8;
}

.msg-text {
  font-size: 14px;
  line-height: 1.4;
  word-break: break-word;
}

.msg-meta {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 4px;
  margin-top: 4px;
  font-size: 10px;
  opacity: 0.7;
}

.chat-img-thumb {
  max-width: 100%;
  max-height: 200px;
  border-radius: 8px;
  cursor: pointer;
}

.main-footer {
  padding: 16px 24px;
  border-top: 1px solid rgba(0, 0, 0, 0.08);
  display: flex;
  align-items: center;
  gap: 12px;
  background: #ffffff;
}

.btn-attach {
  width: 42px;
  height: 42px;
  border-radius: 12px;
  border: 1px solid rgba(0, 0, 0, 0.08);
  background: #f8fafc;
  color: #64748b;
  font-size: 20px;
  cursor: pointer;
}

.chat-input {
  flex: 1;
  padding: 12px 18px;
  border-radius: 14px;
  border: 1px solid rgba(0, 0, 0, 0.08);
  background: #f8fafc;
  outline: none;
  font-size: 14px;
}

.btn-send {
  width: 44px;
  height: 44px;
  border-radius: 14px;
  border: none;
  background: #5c60f5;
  color: #ffffff;
  font-size: 18px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
}

.btn-send:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.no-chat-selected {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  color: #94a3b8;
  gap: 12px;
}

.font-size-huge { font-size: 64px; }
.empty-state { text-align: center; color: #94a3b8; padding: 32px; font-size: 14px; }
.btn-back-chat {
  display: none;
  background: #f1f5f9;
  border: none;
  width: 36px;
  height: 36px;
  border-radius: 10px;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  color: #0f172a;
  cursor: pointer;
  margin-right: 8px;
  flex-shrink: 0;
}

@media (max-width: 768px) {
  .admin-support-chats {
    height: calc(100vh - 100px);
  }
  .telegram-container {
    flex-direction: column;
  }
  .chat-sidebar {
    width: 100% !important;
    height: 100%;
  }
  .chat-main {
    width: 100% !important;
    height: 100%;
  }
  .chat-sidebar.mobile-hidden,
  .chat-main.mobile-hidden {
    display: none !important;
  }
  .btn-back-chat {
    display: flex;
  }
  .user-meta-header {
    gap: 8px;
  }
  .header-name {
    font-size: 14px;
  }
  .header-details {
    font-size: 11px;
    flex-wrap: wrap;
  }
}
</style>
