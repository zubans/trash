<template>
  <div v-if="show" class="support-modal-overlay" @click.self="closeModal">
    <div class="support-modal-card">
      <!-- Modal Header -->
      <div class="support-modal-header">
        <div class="header-title-group">
          <div class="headset-avatar">
            <i class="ph-fill ph-headset"></i>
          </div>
          <div>
            <h3 class="support-title">Поддержка</h3>
            <div class="support-subtitle">
              <span class="online-indicator"></span> Администратор онлайн
            </div>
          </div>
        </div>
        <button type="button" class="btn-close-modal" title="Закрыть" @click="closeModal">
          <i class="ph ph-x"></i>
        </button>
      </div>

      <!-- Messages Scroll Container -->
      <div ref="messagesContainerRef" class="support-messages-area">
        <div v-if="loading" class="support-loading">
          <i class="ph ph-spinner spinner"></i>
          <span>Загрузка чата...</span>
        </div>

        <div v-else-if="messages.length === 0" class="support-welcome">
          <div class="welcome-icon">💬</div>
          <h4>Здравствуйте!</h4>
          <p>Напишите нам, если у вас возникли вопросы или нужна помощь. Администратор ответит в ближайшее время.</p>
        </div>

        <div
          v-for="msg in messages"
          :key="msg.id"
          :class="['msg-bubble-wrap', isMyMessage(msg) ? 'outgoing' : 'incoming']"
        >
          <!-- Admin Avatar for incoming messages -->
          <div v-if="!isMyMessage(msg)" class="admin-avatar">
            <i class="ph-fill ph-shield-check"></i>
          </div>

          <div class="msg-bubble-content">
            <div v-if="!isMyMessage(msg)" class="msg-sender-name">
              Поддержка
            </div>

            <!-- Image attachment -->
            <div v-if="isImageAttachment(msg)" class="msg-img-box mb-1">
              <img
                :src="getImageSrc(msg)"
                alt="Изображение"
                class="msg-img"
                @click="openImagePreview(getImageSrc(msg))"
              />
            </div>

            <!-- File attachment -->
            <div v-else-if="msg.file_url" class="msg-file-box mb-1">
              <a :href="resolveFileUrl(msg.file_url)" target="_blank" download class="file-link">
                <i class="ph-fill ph-file-text me-1"></i> {{ msg.file_name || 'Скачать файл' }}
              </a>
            </div>

            <!-- Text -->
            <div v-if="msg.text || msg.content" class="msg-text">
              {{ msg.text || msg.content }}
            </div>

            <div class="msg-time-row">
              <span class="msg-time">{{ formatTime(msg.created_at) }}</span>
            </div>
          </div>
        </div>
      </div>

      <!-- Hidden file input for web attachment -->
      <input
        ref="fileInputRef"
        type="file"
        accept="image/*,application/pdf"
        class="d-none"
        @change="onFileSelected"
      />

      <!-- Banned Notice Banner -->
      <div v-if="isBanned" class="support-banned-banner">
        <i class="ph-bold ph-prohibit icon-ban"></i>
        <span>{{ banNoticeText }}</span>
      </div>

      <!-- Input Bar -->
      <div v-else class="support-input-bar">
        <button
          type="button"
          class="btn-attach"
          title="Прикрепить фото или файл"
          :disabled="uploading"
          @click="triggerFileSelect"
        >
          <i v-if="uploading" class="ph ph-spinner spinner"></i>
          <i v-else class="ph-bold ph-paperclip"></i>
        </button>

        <input
          v-model="inputText"
          type="text"
          class="support-input"
          placeholder="Напишите сообщение..."
          :disabled="sending"
          @keyup.enter="sendMessage"
        />

        <button
          type="button"
          class="btn-send"
          :disabled="!inputText.trim() && !uploading"
          @click="sendMessage"
        >
          <i class="ph-bold ph-paper-plane-right"></i>
        </button>
      </div>
    </div>

    <!-- Image Preview Modal -->
    <div v-if="showImageModal" class="img-preview-overlay" @click="showImageModal = false">
      <img :src="previewUrl" alt="Превью" class="img-preview-full" />
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, watch, nextTick, onUnmounted } from 'vue'
import api, { resolveFileUrl } from '../services/api'
import { useAuthStore } from '../stores/auth-store'
import { Capacitor } from '@capacitor/core'
import { Camera, CameraResultType, CameraSource } from '@capacitor/camera'
import { compressImage } from '../utils/imageCompressor'

export default defineComponent({
  name: 'SupportChatModal',
  props: {
    show: {
      type: Boolean,
      required: true,
    },
  },
  emits: ['update:show', 'close'],
  setup(props, { emit }) {
    const authStore = useAuthStore()
    const loading = ref(false)
    const sending = ref(false)
    const uploading = ref(false)
    const chatId = ref<string | null>(null)
    const messages = ref<any[]>([])
    const inputText = ref('')
    const messagesContainerRef = ref<any>(null)
    const fileInputRef = ref<HTMLInputElement | null>(null)
    const isBanned = ref(false)
    const bannedUntil = ref<string | null>(null)

    const banNoticeText = computed(() => {
      if (!isBanned.value) return ''
      if (!bannedUntil.value) return 'Чат заблокирован администратором'
      const d = new Date(bannedUntil.value)
      const now = new Date()
      if (d.getFullYear() - now.getFullYear() > 10) return 'Чат заблокирован администратором навсегда'
      return `Чат заблокирован администратором до ${d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })} ${d.toLocaleDateString()}`
    })

    const showImageModal = ref(false)
    const previewUrl = ref('')

    let pollTimer: any = null

    const closeModal = () => {
      stopPolling()
      emit('update:show', false)
      emit('close')
    }

    const isMyMessage = (msg: any) => {
      if (!msg || !authStore.userID) return false
      return msg.sender_id === authStore.userID
    }

    const isImageAttachment = (msg: any) => {
      if (!msg) return false
      if (msg.file_type === 'image') return true
      const path = msg.file_url || msg.content || ''
      const url = path.toLowerCase()
      return url.endsWith('.jpg') || url.endsWith('.jpeg') || url.endsWith('.png') || url.endsWith('.webp') || url.endsWith('.gif') || url.startsWith('/uploads/')
    }

    const getImageSrc = (msg: any) => {
      const path = msg.file_url || msg.content
      if (!path) return ''
      return resolveFileUrl(path)
    }

    const openImagePreview = (url: string) => {
      if (!url) return
      previewUrl.value = url
      showImageModal.value = true
    }

    const formatTime = (dateStr?: string) => {
      if (!dateStr) return ''
      const d = new Date(dateStr)
      return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    }

    const scrollToBottom = () => {
      nextTick(() => {
        if (messagesContainerRef.value) {
          messagesContainerRef.value.scrollTop = messagesContainerRef.value.scrollHeight
        }
      })
    }

    const fetchMessages = async () => {
      if (!chatId.value) return
      try {
        const response = await api.get(`/support/chats/${chatId.value}/messages`)
        const newMsgs = response.data || []
        if (newMsgs.length !== messages.value.length) {
          messages.value = newMsgs
          scrollToBottom()
        } else {
          messages.value = newMsgs
        }
      } catch (err) {
        console.error('[SupportChatModal] failed to fetch messages:', err)
      }
    }

    const loadSupportChat = async () => {
      loading.value = true
      try {
        const res = await api.get('/support/chat')
        if (res.data?.id) {
          chatId.value = res.data.id
          isBanned.value = !!res.data.is_banned
          bannedUntil.value = res.data.banned_until || null
          await fetchMessages()
          startPolling()
        }
      } catch (err) {
        console.error('[SupportChatModal] failed to get support chat:', err)
      } finally {
        loading.value = false
      }
    }

    const startPolling = () => {
      stopPolling()
      pollTimer = setInterval(fetchMessages, 3000)
    }

    const stopPolling = () => {
      if (pollTimer) {
        clearInterval(pollTimer)
        pollTimer = null
      }
    }

    const sendMessage = async () => {
      if (!inputText.value.trim() || !chatId.value || sending.value) return
      const text = inputText.value.trim()
      inputText.value = ''
      sending.value = true

      try {
        const response = await api.post(`/support/chats/${chatId.value}/messages`, { text })
        if (response.data) {
          messages.value.push(response.data)
          scrollToBottom()
        }
      } catch (err) {
        console.error('[SupportChatModal] send message failed:', err)
      } finally {
        sending.value = false
      }
    }

    const triggerFileSelect = async () => {
      if (Capacitor.isNativePlatform()) {
        try {
          const photo = await Camera.getPhoto({
            quality: 85,
            allowEditing: false,
            resultType: CameraResultType.Uri,
            source: CameraSource.Prompt,
          })

          if (photo.webPath && chatId.value) {
            uploading.value = true
            const response = await fetch(photo.webPath)
            const blob = await response.blob()
            let file = new File([blob], `photo_${Date.now()}.${photo.format || 'jpg'}`, { type: `image/${photo.format || 'jpeg'}` })
            file = await compressImage(file, 150, 300)

            const formData = new FormData()
            formData.append('file', file)
            if (inputText.value.trim()) {
              formData.append('text', inputText.value.trim())
              inputText.value = ''
            }

            const res = await api.post(`/support/chats/${chatId.value}/upload`, formData, {
              headers: { 'Content-Type': 'multipart/form-data' },
            })
            if (res.data) {
              messages.value.push(res.data)
              scrollToBottom()
            }
          }
        } catch (err) {
          console.warn('[SupportChatModal] camera cancel/error:', err)
        } finally {
          uploading.value = false
        }
      } else {
        if (fileInputRef.value) {
          fileInputRef.value.click()
        }
      }
    }

    const onFileSelected = async (event: Event) => {
      const target = event.target as HTMLInputElement
      if (!target.files || target.files.length === 0 || !chatId.value) return
      let file = target.files[0]
      uploading.value = true

      try {
        if (file.type.startsWith('image/')) {
          file = await compressImage(file, 150, 300)
        }

        const formData = new FormData()
        formData.append('file', file)
        if (inputText.value.trim()) {
          formData.append('text', inputText.value.trim())
          inputText.value = ''
        }

        const res = await api.post(`/support/chats/${chatId.value}/upload`, formData, {
          headers: { 'Content-Type': 'multipart/form-data' },
        })

        if (res.data) {
          messages.value.push(res.data)
          scrollToBottom()
        }
      } catch (err) {
        console.error('[SupportChatModal] upload failed:', err)
      } finally {
        uploading.value = false
        target.value = ''
      }
    }

    watch(
      () => props.show,
      (newVal) => {
        if (newVal) {
          loadSupportChat()
        } else {
          stopPolling()
        }
      },
      { immediate: true }
    )

    onUnmounted(() => {
      stopPolling()
    })

    return {
      authStore,
      loading,
      sending,
      uploading,
      messages,
      inputText,
      messagesContainerRef,
      fileInputRef,
      showImageModal,
      previewUrl,
      isBanned,
      banNoticeText,
      closeModal,
      isMyMessage,
      isImageAttachment,
      getImageSrc,
      openImagePreview,
      formatTime,
      sendMessage,
      triggerFileSelect,
      onFileSelected,
      resolveFileUrl,
    }
  },
})
</script>

<style scoped>
.support-modal-overlay {
  position: fixed;
  top: 0;
  left: 0;
  width: 100vw;
  height: 100vh;
  background: rgba(15, 23, 42, 0.6);
  backdrop-filter: blur(6px);
  z-index: 99999;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
  animation: fadeIn 0.2s ease-out;
}

.support-modal-card {
  width: 100%;
  max-width: 480px;
  height: 85vh;
  max-height: 650px;
  background: #ffffff;
  border-radius: 24px;
  box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.25);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  animation: scaleUp 0.25s cubic-bezier(0.16, 1, 0.3, 1);
}

.support-modal-header {
  padding: 16px 20px;
  background: linear-gradient(135deg, #1e293b 0%, #0f172a 100%);
  color: #ffffff;
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.header-title-group {
  display: flex;
  align-items: center;
  gap: 12px;
}

.headset-avatar {
  width: 42px;
  height: 42px;
  border-radius: 14px;
  background: rgba(255, 255, 255, 0.15);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 22px;
  color: #38bdf8;
}

.support-title {
  margin: 0;
  font-size: 17px;
  font-weight: 700;
  line-height: 1.2;
}

.support-subtitle {
  font-size: 12px;
  opacity: 0.85;
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 2px;
}

.online-indicator {
  width: 8px;
  height: 8px;
  background-color: #22c55e;
  border-radius: 50%;
  box-shadow: 0 0 6px rgba(34, 197, 94, 0.8);
}

.btn-close-modal {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.1);
  border: none;
  color: #ffffff;
  font-size: 18px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.2s ease;
}

.btn-close-modal:hover {
  background: rgba(255, 255, 255, 0.2);
}

.support-messages-area {
  flex: 1;
  padding: 16px;
  overflow-y: auto;
  background: #f8fafc;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.support-loading, .support-welcome {
  margin: auto;
  text-align: center;
  color: #64748b;
  max-width: 300px;
  padding: 24px;
}

.support-loading {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
}

.welcome-icon {
  font-size: 40px;
  margin-bottom: 8px;
}

.support-welcome h4 {
  margin: 0 0 6px 0;
  color: #0f172a;
  font-size: 18px;
  font-weight: 700;
}

.support-welcome p {
  font-size: 13px;
  line-height: 1.4;
  margin: 0;
}

.msg-bubble-wrap {
  display: flex;
  gap: 8px;
  max-width: 82%;
}

.msg-bubble-wrap.outgoing {
  align-self: flex-end;
  flex-direction: row-reverse;
}

.msg-bubble-wrap.incoming {
  align-self: flex-start;
}

.admin-avatar {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: #0284c7;
  color: #ffffff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 16px;
  flex-shrink: 0;
  margin-top: 4px;
}

.msg-bubble-content {
  background: #ffffff;
  padding: 10px 14px;
  border-radius: 18px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.04);
  position: relative;
  font-size: 14px;
  color: #0f172a;
}

.outgoing .msg-bubble-content {
  background: #5c60f5;
  color: #ffffff;
  border-bottom-right-radius: 4px;
}

.incoming .msg-bubble-content {
  background: #ffffff;
  border-bottom-left-radius: 4px;
  border: 1px solid rgba(0, 0, 0, 0.06);
}

.msg-sender-name {
  font-size: 11px;
  font-weight: 700;
  color: #0284c7;
  margin-bottom: 4px;
}

.msg-text {
  word-break: break-word;
  line-height: 1.4;
}

.msg-img-box {
  margin-top: 4px;
}

.msg-img {
  max-width: 220px;
  max-height: 220px;
  border-radius: 12px;
  object-fit: cover;
  cursor: pointer;
}

.file-link {
  color: inherit;
  font-weight: 600;
  text-decoration: underline;
  font-size: 13px;
}

.msg-time-row {
  display: flex;
  justify-content: flex-end;
  margin-top: 4px;
}

.msg-time {
  font-size: 10px;
  opacity: 0.7;
}

.support-input-bar {
  padding: 12px 16px;
  background: #ffffff;
  border-top: 1px solid rgba(0, 0, 0, 0.06);
  display: flex;
  align-items: center;
  gap: 8px;
}

.btn-attach {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  background: #f1f5f9;
  border: none;
  color: #64748b;
  font-size: 18px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.2s ease;
}

.btn-attach:hover:not(:disabled) {
  background: #e2e8f0;
  color: #0f172a;
}

.support-input {
  flex: 1;
  padding: 10px 16px;
  border-radius: 99px;
  border: 1.5px solid rgba(0, 0, 0, 0.08);
  background: #f8fafc;
  font-size: 14px;
  color: #0f172a;
  outline: none;
  transition: all 0.2s ease;
}

.support-input:focus {
  border-color: #5c60f5;
  background: #ffffff;
  box-shadow: 0 0 0 3px rgba(92, 96, 245, 0.1);
}

.btn-send {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  background: #5c60f5;
  color: #ffffff;
  border: none;
  font-size: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.2s ease;
}

.btn-send:hover:not(:disabled) {
  background: #4f46e5;
  transform: scale(1.05);
}

.btn-send:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.spinner {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  100% { transform: rotate(360deg); }
}

@keyframes fadeIn {
  from { opacity: 0; }
  to { opacity: 1; }
}

@keyframes scaleUp {
  from { transform: scale(0.92); opacity: 0; }
  to { transform: scale(1); opacity: 1; }
}

.img-preview-overlay {
  position: fixed;
  top: 0;
  left: 0;
  width: 100vw;
  height: 100vh;
  background: rgba(0, 0, 0, 0.9);
  z-index: 100000;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
}

.img-preview-full {
  max-width: 90vw;
  max-height: 90vh;
  border-radius: 12px;
  object-fit: contain;
}

.support-banned-banner {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 14px 18px;
  background: #fef2f2;
  border-top: 1px solid #fee2e2;
  color: #ef4444;
  font-size: 13px;
  font-weight: 600;
  border-radius: 0 0 20px 20px;
}

.icon-ban {
  font-size: 18px;
}
</style>
