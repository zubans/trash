<template>
  <div :class="['chat-panel', { open: !!selectedChatOrder }]">
    <div class="chat-header p-3 bg-telegram text-white d-flex align-items-center justify-content-between shadow-sm">
      <div class="d-flex align-items-center gap-2">
        <div class="telegram-avatar font-bold">{{ recipientInitials }}</div>
        <div>
          <div class="font-bold text-sm leading-tight">{{ recipientTitle }}</div>
          <div class="text-xxs opacity-75">
            {{ $t('customer.orderNumber', { id: selectedChatOrder ? selectedChatOrder.id.slice(0, 8) : '' }) }}
          </div>
        </div>
      </div>
      <button class="btn-close-chat border-0 text-white" @click="$emit('close')">✕</button>
    </div>

    <!-- Messages body -->
    <div ref="messagesContainerRef" class="chat-messages p-3 flex-grow-1 overflow-auto">
      <div v-if="chatMessages.length === 0" class="text-center text-white-50 my-auto py-5">
        <va-icon name="forum" size="large" class="mb-2 opacity-50" />
        <p class="text-xs m-0">{{ $t('customer.noMessagesYet') }}</p>
      </div>

      <div
        v-for="msg in chatMessages"
        :key="msg.id"
        :class="['telegram-bubble', msg.sender_id === currentUserId ? 'my-telegram-msg ml-auto' : 'their-telegram-msg mr-auto']"
      >
        <div class="telegram-sender" v-if="msg.sender_id !== currentUserId">{{ recipientRoleLabel }}</div>
        
        <!-- Attachment rendering -->
        <div v-if="msg.file_url" class="telegram-attachment mb-2">
          <div v-if="isImageAttachment(msg)" class="attachment-image-wrapper">
            <img
              :src="getImageSrc(msg.file_url)"
              class="attachment-img rounded-lg shadow-sm cursor-pointer"
              alt="photo"
              @click="$emit('previewImage', getImageSrc(msg.file_url))"
              @error="$emit('imgError', msg.file_url)"
            />
            <div v-if="isDebug" class="text-xxs text-warning bg-dark p-1 rounded mt-1 overflow-auto max-w-xs style-mono">
              [DEBUG] URL: {{ getImageSrc(msg.file_url) }}
            </div>
          </div>
          <div v-else class="attachment-doc-wrapper p-2 bg-white-10 rounded d-flex align-items-center">
            <span class="doc-icon mr-2">📄</span>
            <div class="flex-grow-1 overflow-hidden">
              <a :href="resolveFileUrl(msg.file_url)" target="_blank" download class="font-bold text-xs text-white truncate d-block">
                {{ msg.file_name || 'document' }}
              </a>
              <span class="text-xxs opacity-75" v-if="msg.file_size">{{ formatFileSize(msg.file_size) }}</span>
            </div>
            <a :href="resolveFileUrl(msg.file_url)" target="_blank" download class="btn-download ml-2">⬇</a>
          </div>
        </div>

        <div v-if="msg.text" class="telegram-text">{{ msg.text }}</div>
        <div class="telegram-meta d-flex align-items-center justify-content-between">
          <div class="d-flex align-items-center gap-1">
            <span class="telegram-time">{{ formatTime(msg.created_at) }}</span>
            <span v-if="msg.sender_id === currentUserId" class="telegram-ticks-status" :title="getMessageStatusTitle(msg.status)">
              <span v-if="msg.status === 'read'" class="ticks-read">✓✓</span>
              <span v-else-if="msg.status === 'delivered'" class="ticks-delivered">✓✓</span>
              <span v-else class="ticks-sent">✓</span>
            </span>
          </div>
          <button
            v-if="msg.sender_id === currentUserId"
            type="button"
            class="btn-delete-msg border-0 bg-transparent p-0 ml-2 cursor-pointer opacity-60 hover-opacity-100 d-inline-flex align-items-center"
            :title="$t('customer.deleteMessage')"
            @click.stop="$emit('deleteMessage', msg.id)"
          >
            <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="text-danger-light">
              <polyline points="3 6 5 6 21 6"></polyline>
              <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
            </svg>
          </button>
        </div>
      </div>
    </div>

    <!-- File Attachment Options Menu / Input area -->
    <div class="chat-input-area p-2 bg-white border-top">
      <div v-if="uploadingFile" class="text-xs text-primary mb-2 d-flex align-items-center">
        <span class="spinner-border spinner-border-sm mr-2"></span> {{ $t('customer.uploadingFile') }}
      </div>

      <!-- Hidden file inputs -->
      <input ref="fileInputRef" type="file" class="d-none" @change="$emit('fileSelected', $event)" />
      <input ref="galleryInputRef" type="file" accept="image/*" class="d-none" @change="$emit('fileSelected', $event)" />
      <input ref="cameraInputRef" type="file" accept="image/*" capture="environment" class="d-none" @change="$emit('fileSelected', $event)" />

      <!-- Custom Telegram Attach Menu -->
      <div v-if="showAttachMenu" class="attach-menu-popup shadow-lg border rounded p-2 mb-2 bg-white">
        <div class="attach-option p-2 hover-bg-light rounded cursor-pointer d-flex align-items-center" @click="$emit('triggerCamera')">
          <span class="mr-2 text-lg">📷</span>
          <div>
            <div class="font-bold text-xs">{{ $t('customer.takePhoto') }}</div>
            <div class="text-xxs text-secondary">{{ $t('customer.cameraSource') }}</div>
          </div>
        </div>
        <div class="attach-option p-2 hover-bg-light rounded cursor-pointer d-flex align-items-center" @click="$emit('triggerGallery')">
          <span class="mr-2 text-lg">🖼️</span>
          <div>
            <div class="font-bold text-xs">{{ $t('customer.gallery') }}</div>
            <div class="text-xxs text-secondary">{{ $t('customer.chooseFromGallery') }}</div>
          </div>
        </div>
        <div class="attach-option p-2 hover-bg-light rounded cursor-pointer d-flex align-items-center" @click="$emit('triggerDoc')">
          <span class="mr-2 text-lg">📄</span>
          <div>
            <div class="font-bold text-xs">{{ $t('customer.document') }}</div>
            <div class="text-xxs text-secondary">{{ $t('customer.chooseAnyFile') }}</div>
          </div>
        </div>
      </div>

      <div class="d-flex align-items-center gap-1">
        <button
          type="button"
          class="telegram-attach-btn text-secondary"
          :title="$t('customer.attach')"
          @click="$emit('toggleAttachMenu')"
        >
          📎
        </button>
        <va-input
          :model-value="chatText"
          @update:model-value="$emit('update:chatText', $event)"
          :placeholder="$t('customer.typeMessage')"
          class="flex-grow-1"
          :disabled="chatLocked"
          @keyup.enter="$emit('sendMessage')"
        />
        <button
          type="button"
          class="telegram-send-btn bg-primary text-white border-0 rounded-circle d-flex align-items-center justify-content-center"
          :disabled="chatLocked || sendingChat"
          @click="$emit('sendMessage')"
        >
          <svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor">
            <path d="M2.01 21L23 12 2.01 3 2 10l15 2-15 2z" />
          </svg>
        </button>
      </div>
      <div v-if="chatError" class="text-danger text-xs mt-2">{{ chatError }}</div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, watch, nextTick } from 'vue'
import { resolveFileUrl, isDebug } from '../../services/api'

export default defineComponent({
  name: 'ChatDrawer',
  props: {
    selectedChatOrder: { type: Object, default: null },
    chatMessages: { type: Array as () => any[], default: () => [] },
    chatText: { type: String, default: '' },
    currentUserId: { type: String, required: true },
    recipientTitle: { type: String, default: 'Чат' },
    recipientInitials: { type: String, default: '💬' },
    recipientRoleLabel: { type: String, default: 'Собеседник' },
    chatLocked: { type: Boolean, default: false },
    sendingChat: { type: Boolean, default: false },
    uploadingFile: { type: Boolean, default: false },
    showAttachMenu: { type: Boolean, default: false },
    chatError: { type: String, default: '' },
    getImageSrc: { type: Function, required: true },
    isImageAttachment: { type: Function, required: true },
  },
  emits: [
    'close',
    'sendMessage',
    'update:chatText',
    'toggleAttachMenu',
    'triggerCamera',
    'triggerGallery',
    'triggerDoc',
    'fileSelected',
    'deleteMessage',
    'previewImage',
    'imgError',
  ],
  setup(props) {
    const messagesContainerRef = ref<HTMLElement | null>(null)
    const fileInputRef = ref<HTMLInputElement | null>(null)
    const galleryInputRef = ref<HTMLInputElement | null>(null)
    const cameraInputRef = ref<HTMLInputElement | null>(null)

    const scrollToBottom = () => {
      nextTick(() => {
        if (messagesContainerRef.value) {
          messagesContainerRef.value.scrollTop = messagesContainerRef.value.scrollHeight
        }
      })
    }

    watch(
      () => props.chatMessages.length,
      () => scrollToBottom()
    )

    const formatTime = (dateStr: string) => {
      if (!dateStr) return ''
      const d = new Date(dateStr)
      return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    }

    const formatFileSize = (bytes?: number) => {
      if (!bytes) return ''
      if (bytes < 1024) return bytes + ' B'
      if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
      return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
    }

    const getMessageStatusTitle = (status?: string) => {
      if (status === 'read') return 'Прочитано'
      if (status === 'delivered') return 'Доставлено'
      return 'Отправлено'
    }

    return {
      messagesContainerRef,
      fileInputRef,
      galleryInputRef,
      cameraInputRef,
      scrollToBottom,
      formatTime,
      formatFileSize,
      getMessageStatusTitle,
      resolveFileUrl,
      isDebug,
    }
  },
})
</script>

<style scoped>
.chat-panel {
  position: fixed;
  top: 0;
  right: 0;
  width: 420px;
  max-width: 100vw;
  height: 100%;
  background: #0f1826;
  z-index: 1000;
  transform: translateX(100%);
  transition: transform 0.28s cubic-bezier(0.2, 0, 0, 1);
  display: flex;
  flex-direction: column;
  box-shadow: -4px 0 20px rgba(0, 0, 0, 0.25);
}

.chat-panel.open {
  transform: translateX(0);
}

.bg-telegram {
  background: #517da2 !important;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.15);
}

.telegram-avatar {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  background: #3e6587;
  color: white;
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 700;
  font-size: 13px;
}

.btn-close-chat {
  background: transparent;
  font-size: 20px;
  cursor: pointer;
  opacity: 0.85;
}
.btn-close-chat:hover {
  opacity: 1;
}

.chat-messages {
  background-color: #0e1621;
  background-image: radial-gradient(rgba(255, 255, 255, 0.03) 1px, transparent 1px);
  background-size: 16px 16px;
  display: flex;
  flex-direction: column;
}

.telegram-bubble {
  max-width: 78%;
  padding: 8px 12px 6px 12px;
  position: relative;
  margin-bottom: 6px;
  font-size: 14.5px;
  line-height: 1.4;
  word-break: break-word;
}

.my-telegram-msg {
  background-color: #2b5278 !important;
  color: #f5f5f5 !important;
  border-radius: 14px 14px 2px 14px;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.2);
}

.their-telegram-msg {
  background-color: #182533 !important;
  color: #e4ecf2 !important;
  border-radius: 14px 14px 14px 2px;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.2);
}

.telegram-sender {
  font-size: 12px;
  font-weight: 700;
  color: #6bb4e8;
  margin-bottom: 2px;
}

.telegram-attach-btn {
  background: transparent;
  border: none;
  font-size: 18px;
  cursor: pointer;
  padding: 2px 6px;
  opacity: 0.85;
  transition: opacity 0.15s ease;
}
.telegram-attach-btn:hover {
  opacity: 1;
}

.attachment-img {
  width: 100%;
  max-width: 260px;
  min-width: 120px;
  min-height: 100px;
  max-height: 240px;
  object-fit: cover;
  display: block;
  cursor: pointer;
  pointer-events: auto;
}

.attachment-doc-wrapper {
  background: rgba(255, 255, 255, 0.08);
  border: 1px solid rgba(255, 255, 255, 0.15);
}

.btn-download {
  color: #7ce7ff;
  text-decoration: none;
  font-weight: bold;
  font-size: 14px;
}

.telegram-text {
  font-size: 14px;
}

.telegram-meta {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 4px;
  margin-top: 2px;
}

.telegram-time {
  font-size: 10.5px;
  color: rgba(255, 255, 255, 0.55);
}

.telegram-ticks-status {
  font-size: 11px;
  font-weight: bold;
}
.ticks-sent {
  color: rgba(255, 255, 255, 0.45);
}
.ticks-delivered {
  color: rgba(255, 255, 255, 0.65);
}
.ticks-read {
  color: #5bb3f0;
}

.telegram-send-btn {
  width: 36px;
  height: 36px;
  padding: 0;
  cursor: pointer;
}

.attach-menu-popup {
  animation: slide-up 0.18s ease-out;
}
</style>
