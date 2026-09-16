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
        <template v-if="step === 'intro'">
          <h3 class="verification-title">{{ $t('executor.verificationPromptTitle') }}</h3>
          <p class="verification-text">{{ $t('executor.verificationPromptText') }}</p>
        </template>

        <template v-else-if="step === 'form'">
          <h3 class="verification-title">{{ $t('executor.verificationFormTitle') }}</h3>
          <p class="verification-text">{{ $t('executor.verificationFormText') }}</p>
          <div class="verification-form">
            <label v-if="missing.includes('last_name')" class="verification-field">
              <span>{{ $t('executor.verificationLastName') }}</span>
              <input v-model="lastName" type="text" class="verification-input" name="last_name" />
            </label>
            <label v-if="missing.includes('first_name')" class="verification-field">
              <span>{{ $t('executor.verificationFirstName') }}</span>
              <input v-model="firstName" type="text" class="verification-input" name="first_name" />
            </label>
            <label v-if="missing.includes('patronymic')" class="verification-field">
              <span>{{ $t('executor.verificationPatronymic') }}</span>
              <input v-model="patronymic" type="text" class="verification-input" name="patronymic" />
            </label>
            <label v-if="missing.includes('birth_date')" class="verification-field">
              <span>{{ $t('executor.verificationBirthDate') }}</span>
              <input v-model="birthDate" type="date" :max="maxBirthDate" class="verification-input" name="birth_date" />
            </label>
            <AddressAutocomplete
              v-if="missing.includes('address')"
              v-model="address"
              :label="$t('executor.verificationAddress')"
              :hint="$t('executor.verificationAddressHint')"
              :needs-flat="false"
            />
          </div>
        </template>

        <template v-else>
          <h3 class="verification-title">{{ $t('executor.verificationPendingTitle') }}</h3>
          <p class="verification-text">{{ $t('executor.verificationPendingText') }}</p>
          <p v-if="orderAddress" class="verification-address">
            <i class="ph ph-map-pin"></i> {{ orderAddress }}
          </p>
        </template>

        <p v-if="errorText" class="verification-error">{{ errorText }}</p>
      </div>

      <footer class="verification-footer">
        <template v-if="step === 'pending'">
          <button type="button" class="verification-btn-secondary" :disabled="busy" @click="cancelRequest">
            {{ $t('executor.verificationCancel') }}
          </button>
          <button type="button" class="verification-btn-primary" @click="dismiss">
            {{ $t('executor.verificationPendingOk') }}
          </button>
        </template>
        <template v-else>
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
            <i v-else class="ph-bold ph-seal-check"></i>
            {{ busy ? $t('executor.verificationPromptSending') : $t('executor.verificationPromptSubmit') }}
          </button>
        </template>
      </footer>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, computed, watch, PropType } from 'vue'
import { useI18n } from 'vue-i18n'
import api from '../../../services/api'
import AddressAutocomplete, { StructuredAddress } from '../../../components/AddressAutocomplete.vue'

type Step = 'intro' | 'form' | 'pending'

interface VerificationOrder {
  id: string
  address?: string
}

export default defineComponent({
  name: 'VerificationPromptModal',
  components: { AddressAutocomplete },
  props: {
    show: {
      type: Boolean,
      required: true,
    },
    // Уже размещённая заявка: окно открывается сразу на её состоянии.
    order: {
      type: Object as PropType<VerificationOrder | null>,
      default: null,
    },
  },
  emits: ['update:show', 'close', 'created', 'cancelled'],
  setup(props, { emit }) {
    const { t } = useI18n()
    const busy = ref(false)
    const errorText = ref('')
    const step = ref<Step>('intro')
    const missing = ref<string[]>([])
    const createdOrder = ref<VerificationOrder | null>(null)

    const lastName = ref('')
    const firstName = ref('')
    const patronymic = ref('')
    const birthDate = ref('')
    const address = ref<StructuredAddress | null>(null)
    const maxBirthDate = new Date().toISOString().slice(0, 10)

    const orderAddress = computed(() => (createdOrder.value || props.order)?.address || '')

    const dismiss = () => {
      emit('update:show', false)
      emit('close')
    }

    const apiError = (err: any, fallback: string) => {
      const data = err?.response?.data
      if (typeof data === 'string' && data) return data
      if (data?.error) return data.error
      return fallback
    }

    // Кнопка размещает заказ на услугу верификации — тот же, что заказчик берёт
    // из каталога, его выполняет модератор. Если адрес и данные уже есть в
    // профиле, заказ создаётся сразу; иначе сервер перечисляет, чего не
    // хватает, и окно просит дозаполнить ровно это.
    const requestVerification = async () => {
      if (busy.value) return
      busy.value = true
      errorText.value = ''
      try {
        const payload: Record<string, any> = {}
        if (step.value === 'form') {
          if (missing.value.includes('last_name')) payload.last_name = lastName.value
          if (missing.value.includes('first_name')) payload.first_name = firstName.value
          if (missing.value.includes('patronymic')) payload.patronymic = patronymic.value
          if (missing.value.includes('birth_date')) payload.birth_date = birthDate.value
          if (missing.value.includes('address') && address.value) {
            const a = address.value
            payload.address = {
              address: a.value,
              region: a.region,
              city: a.city,
              street: a.street,
              house: a.house,
              flat: a.flat,
              fias_id: a.fias_id,
              source: a.source,
              lat: a.lat,
              lon: a.lon,
            }
          }
        }
        const res = await api.post('/executor/verification', payload)
        createdOrder.value = res.data
        step.value = 'pending'
        emit('created', res.data)
      } catch (err: any) {
        const fields = err?.response?.status === 422 ? err.response.data?.missing : null
        if (Array.isArray(fields) && fields.length > 0) {
          if (step.value === 'form') errorText.value = t('executor.verificationFormIncomplete')
          missing.value = fields
          step.value = 'form'
        } else {
          errorText.value = apiError(err, t('executor.verificationPromptError'))
        }
      } finally {
        busy.value = false
      }
    }

    const cancelRequest = async () => {
      if (busy.value) return
      busy.value = true
      errorText.value = ''
      try {
        await api.post('/executor/verification/cancel')
        createdOrder.value = null
        step.value = 'intro'
        emit('cancelled')
        dismiss()
      } catch (err: any) {
        errorText.value = apiError(err, t('executor.verificationCancelError'))
      } finally {
        busy.value = false
      }
    }

    // Каждый показ начинается с чистого листа: прошлая ошибка или форма не
    // должны встречать пользователя снова.
    watch(
      () => props.show,
      (val) => {
        if (!val) return
        errorText.value = ''
        createdOrder.value = null
        step.value = props.order ? 'pending' : 'intro'
      },
      { immediate: true }
    )

    return {
      busy,
      errorText,
      step,
      missing,
      lastName,
      firstName,
      patronymic,
      birthDate,
      address,
      maxBirthDate,
      orderAddress,
      dismiss,
      requestVerification,
      cancelRequest,
    }
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
  max-height: calc(100vh - 32px);
  overflow-y: auto;
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

.verification-form {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin-top: 12px;
}

.verification-field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 13px;
  color: #475569;
}

.verification-input {
  height: 42px;
  border: 1px solid #e2e8f0;
  border-radius: 10px;
  padding: 0 12px;
  font-size: 15px;
  font-family: inherit;
  color: #0f172a;
}

.verification-address {
  margin: 10px 0 0;
  padding: 10px 12px;
  background: #f1f5f9;
  border-radius: 10px;
  color: #0f172a;
  font-size: 14px;
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
