<template>
  <div v-if="show" class="verification-overlay" @click.self="dismiss">
    <div class="verification-modal">
      <header class="verification-header">
        <div class="verification-icon">
          <i class="ph-fill ph-seal-check"></i>
        </div>
        <button type="button" class="verification-close" :title="$t('common.close')" @click="dismiss">
          <i class="ph-bold ph-x"></i>
        </button>
      </header>

      <div class="verification-body">
        <h3 class="verification-title">{{ $t('executor.verificationPromptTitle') }}</h3>
        <p class="verification-text">{{ $t('executor.verificationPromptText') }}</p>
        <p v-if="errorText" class="verification-error">{{ errorText }}</p>
      </div>

      <footer class="verification-footer">
        <button type="button" class="verification-btn-secondary" @click="dismiss">
          {{ $t('executor.verificationPromptLater') }}
        </button>
        <button
          type="button"
          class="verification-btn-primary"
          :disabled="busy"
          @click="requestVerification"
        >
          <i v-if="busy" class="ph ph-spinner spinner"></i>
          <i v-else class="ph-bold ph-paper-plane-tilt"></i>
          {{ busy ? $t('executor.verificationPromptSending') : $t('executor.verificationPromptSubmit') }}
        </button>
      </footer>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import api from '../../../services/api'

export default defineComponent({
  name: 'VerificationPromptModal',
  props: {
    show: {
      type: Boolean,
      required: true,
    },
  },
  emits: ['update:show', 'close', 'sent'],
  setup(props, { emit }) {
    const { t } = useI18n()
    const busy = ref(false)
    const errorText = ref('')

    const dismiss = () => {
      emit('update:show', false)
      emit('close')
    }

    // Кнопка пишет заявку в поддержку за пользователя: чат открывается с уже
    // отправленной просьбой, и ему остаётся только ждать ответа администратора.
    // Отдельного endpoint для верификации нет — заявка уходит обычным сообщением
    // в тот же чат, где администратор отвечает на остальные вопросы.
    const requestVerification = async () => {
      if (busy.value) return
      busy.value = true
      errorText.value = ''
      try {
        const res = await api.get('/support/chat')
        const chatId = res.data?.id
        if (!chatId) throw new Error('no support chat')
        await api.post(`/support/chats/${chatId}/messages`, {
          text: t('executor.verificationPromptRequestText'),
        })
        emit('update:show', false)
        emit('sent')
      } catch (err: any) {
        errorText.value =
          typeof err?.response?.data === 'string' && err.response.data
            ? err.response.data
            : t('executor.verificationPromptError')
      } finally {
        busy.value = false
      }
    }

    // Прошлая ошибка не должна встречать пользователя при следующем показе.
    watch(
      () => props.show,
      (val) => {
        if (val) errorText.value = ''
      }
    )

    return { busy, errorText, dismiss, requestVerification }
  },
})
</script>

<style scoped>
.verification-overlay {
  position: fixed;
  inset: 0;
  background: rgba(15, 23, 42, 0.55);
  backdrop-filter: blur(6px);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
  z-index: 1300;
  animation: fadeIn 0.2s ease-out;
}

.verification-modal {
  background: #ffffff;
  border-radius: 20px;
  width: 100%;
  max-width: 420px;
  overflow: hidden;
  box-shadow: 0 24px 48px -16px rgba(15, 23, 42, 0.35);
  animation: scaleUp 0.25s cubic-bezier(0.16, 1, 0.3, 1);
}

.verification-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  padding: 20px 20px 0;
}

.verification-icon {
  width: 52px;
  height: 52px;
  border-radius: 16px;
  background: linear-gradient(135deg, #6366f1 0%, #4f46e5 100%);
  color: #ffffff;
  font-size: 28px;
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 8px 16px -6px rgba(99, 102, 241, 0.5);
}

.verification-close {
  border: none;
  background: #f1f5f9;
  color: #475569;
  width: 32px;
  height: 32px;
  border-radius: 10px;
  cursor: pointer;
  flex-shrink: 0;
  transition: background 0.2s ease;
}

.verification-close:hover {
  background: #e2e8f0;
}

.verification-body {
  padding: 14px 20px 8px;
}

.verification-title {
  margin: 0;
  font-size: 18px;
  font-weight: 700;
  color: #0f172a;
}

.verification-text {
  margin: 8px 0 0;
  font-size: 14px;
  color: #475569;
  line-height: 1.5;
}

.verification-error {
  margin: 10px 0 0;
  padding: 10px 12px;
  background: #fef2f2;
  border-radius: 10px;
  color: #b91c1c;
  font-size: 13px;
  line-height: 1.45;
}

.verification-footer {
  display: flex;
  gap: 10px;
  padding: 14px 20px 20px;
}

.verification-btn-secondary,
.verification-btn-primary {
  flex: 1;
  height: 46px;
  border-radius: 12px;
  font-size: 15px;
  font-weight: 600;
  cursor: pointer;
  border: none;
  font-family: inherit;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  transition: all 0.2s ease;
}

.verification-btn-secondary {
  background: #f1f5f9;
  color: #475569;
}

.verification-btn-secondary:hover {
  background: #e2e8f0;
}

.verification-btn-primary {
  background: #6366f1;
  color: #ffffff;
}

.verification-btn-primary:hover:not(:disabled) {
  background: #4f46e5;
}

.verification-btn-primary:disabled {
  opacity: 0.6;
  cursor: default;
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
</style>
