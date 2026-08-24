<template>
  <div v-if="show" class="modal-overlay" @click.self="show = false">
    <div class="modal-card">
      <!-- Header -->
      <div class="modal-header">
        <div class="modal-title">
          <i class="ph-fill ph-user-gear"></i>
          Профиль исполнителя
        </div>
        <button type="button" class="btn-close" aria-label="Закрыть" @click="show = false">
          <i class="ph ph-x"></i>
        </button>
      </div>

      <!-- Verification / Profile Box -->
      <div class="verification-box mb-4">
        <i class="ph-fill ph-user-circle v-icon"></i>
        <div class="v-content">
          <div class="v-header">
            <div class="v-title">{{ fullName || 'Исполнитель' }}</div>
          </div>
          <div class="v-desc">Телефон: {{ phone || '-' }}</div>
        </div>
      </div>

      <!-- Email Management -->
      <div class="section-header">
        <div class="section-title">
          <i class="ph-fill ph-envelope" style="color: #6366f1;"></i>
          Электронная почта
        </div>
        <div class="section-subtitle">При изменении потребуется подтверждение</div>
      </div>

      <div class="email-box mb-4">
        <div class="input-wrapper">
          <input
            v-model="emailInput"
            type="email"
            class="form-input"
            placeholder="example@domain.com"
          />
          <button type="button" class="btn-save-email" :disabled="savingEmail || !emailInput || emailInput === currentEmail" @click="saveEmail">
            <span v-if="savingEmail" class="spinner-sm"></span>
            <template v-else>Сохранить</template>
          </button>
        </div>
        <div v-if="emailMsg" class="email-msg-text" :class="{ error: emailMsgIsError }">
          {{ emailMsg }}
        </div>
      </div>

      <!-- Base Address Management -->
      <div class="section-header">
        <div class="section-title">
          <i class="ph-fill ph-map-pin" style="color: #ef4444;"></i>
          Базовый адрес (откуда начинать поиск заказов)
        </div>
        <div class="section-subtitle">Центр зоны поиска заказов при старте смены</div>
      </div>

      <div class="address-box mb-4">
        <div class="input-wrapper mb-2">
          <input
            v-model="addressInput"
            type="text"
            class="form-input"
            placeholder="Россия, Москва, Тверская улица, д. 1"
            @input="onAddressInput"
          />
          <button type="button" class="btn-save-email" :disabled="savingAddress || !addressInput || addressInput === currentAddress" @click="saveAddress">
            <span v-if="savingAddress" class="spinner-sm"></span>
            <template v-else>Сохранить</template>
          </button>
        </div>
        <div v-if="suggestions.length > 0" class="suggestions-dropdown">
          <div v-for="(s, idx) in suggestions" :key="idx" class="suggestion-item" @click="selectAddress(s)">
            {{ s.display }}
          </div>
        </div>
        <div v-if="addressMsg" class="email-msg-text" :class="{ error: addressMsgIsError }">
          {{ addressMsg }}
        </div>
      </div>

      <!-- Footer -->
      <div class="modal-footer">
        <button type="button" class="btn-cancel" @click="show = false">
          Закрыть
        </button>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, computed, ref, watch, onMounted } from 'vue'
import api from '../../../services/api'

export default defineComponent({
  name: 'ExecutorProfileModal',
  props: {
    modelValue: { type: Boolean, required: true },
    phone: { type: String, default: '' },
    fullName: { type: String, default: '' },
    userEmail: { type: String, default: '' },
    status: { type: String, default: 'ACTIVE' },
    baseAddress: { type: String, default: '' },
  },
  emits: ['update:modelValue', 'emailUpdated', 'addressUpdated'],
  setup(props, { emit }) {
    const show = computed({
      get: () => props.modelValue,
      set: (val) => emit('update:modelValue', val),
    })

    const currentEmail = computed(() => props.userEmail)
    const emailInput = ref(props.userEmail)
    const savingEmail = ref(false)
    const emailMsg = ref('')
    const emailMsgIsError = ref(false)

    const currentAddress = computed(() => props.baseAddress)
    const addressInput = ref(props.baseAddress)
    const savingAddress = ref(false)
    const addressMsg = ref('')
    const addressMsgIsError = ref(false)
    const suggestions = ref<any[]>([])
    let autocompleteTimeout: any = null

    watch(() => props.userEmail, (val) => { emailInput.value = val })
    watch(() => props.baseAddress, (val) => { addressInput.value = val })

    const saveEmail = async () => {
      if (!emailInput.value || emailInput.value === props.userEmail || savingEmail.value) return
      savingEmail.value = true
      emailMsg.value = ''
      emailMsgIsError.value = false
      try {
        await api.post('/user/email', { email: emailInput.value })
        emailMsg.value = 'Ссылка подтверждения отправлена на ' + emailInput.value + '. Email изменится после перехода по ссылке (действительна 60 минут).'
      } catch (err: any) {
        emailMsgIsError.value = true
        emailMsg.value = err.response?.data?.error || err.response?.data || 'Ошибка обновления Email'
      } finally {
        savingEmail.value = false
      }
    }

    const onAddressInput = () => {
      suggestions.value = []
      clearTimeout(autocompleteTimeout)
      const q = addressInput.value.trim()
      if (q.length < 3) return
      autocompleteTimeout = setTimeout(async () => {
        try {
          const res = await api.get('/geo/autocomplete', { params: { q } })
          suggestions.value = res.data || []
        } catch (err) {
          console.error('Autocomplete error:', err)
        }
      }, 400)
    }

    const selectAddress = (s: any) => {
      addressInput.value = s.address
      suggestions.value = []
    }

    const saveAddress = async () => {
      if (!addressInput.value || addressInput.value === props.baseAddress || savingAddress.value) return
      savingAddress.value = true
      addressMsg.value = ''
      addressMsgIsError.value = false
      try {
        await api.post('/customer/addresses', { address: addressInput.value })
        addressMsg.value = 'Базовый адрес успешно обновлен!'
        emit('addressUpdated', addressInput.value)
      } catch (err: any) {
        addressMsgIsError.value = true
        addressMsg.value = err.response?.data || 'Ошибка обновления адреса'
      } finally {
        savingAddress.value = false
      }
    }

    onMounted(() => {
      if (!document.getElementById('phosphor-icons-script')) {
        const script = document.createElement('script')
        script.id = 'phosphor-icons-script'
        script.src = 'https://unpkg.com/@phosphor-icons/web'
        document.head.appendChild(script)
      }
    })

    return {
      show,
      currentEmail,
      emailInput,
      savingEmail,
      emailMsg,
      emailMsgIsError,
      saveEmail,
      currentAddress,
      addressInput,
      savingAddress,
      addressMsg,
      addressMsgIsError,
      suggestions,
      onAddressInput,
      selectAddress,
      saveAddress,
    }
  },
})
</script>

<style scoped>
@import url('https://fonts.googleapis.com/css2?family=Outfit:wght@300;400;500;600;700&display=swap');

.modal-overlay {
  --bg-base: #f8f9fa;
  --surface-card: rgba(255, 255, 255, 0.92);
  --surface-input: rgba(255, 255, 255, 0.7);
  --text-title: #0f172a;
  --text-body: #334155;
  --text-muted: #8b98a5;
  --accent-main: #6366f1;
  --rad-sm: 12px;
  --rad-md: 16px;
  --rad-lg: 32px;
  --shadow-float: 0 20px 50px -10px rgba(15, 23, 42, 0.1);
  --transition: all 0.3s cubic-bezier(0.16, 1, 0.3, 1);

  position: fixed;
  top: 0; left: 0; right: 0; bottom: 0;
  background: rgba(15, 23, 42, 0.4);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
  z-index: 1050;
  font-family: 'Outfit', sans-serif;
  color: var(--text-body);
}

.modal-card {
  background: var(--surface-card);
  backdrop-filter: blur(24px);
  border: 1px solid rgba(255, 255, 255, 0.8);
  border-radius: var(--rad-lg);
  width: 100%;
  max-width: 520px;
  box-shadow: var(--shadow-float);
  padding: 32px;
  position: relative;
  max-height: 90vh;
  overflow-y: auto;
}

.modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 24px;
}

.modal-title {
  font-size: 22px;
  font-weight: 700;
  color: var(--text-title);
  display: flex;
  align-items: center;
  gap: 10px;
}

.btn-close {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  border: 1px solid rgba(255,255,255,0.8);
  background: rgba(255,255,255,0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  color: var(--text-muted);
  cursor: pointer;
}

.verification-box {
  background: rgba(16, 185, 129, 0.08);
  border: 1px solid rgba(16, 185, 129, 0.2);
  border-radius: var(--rad-md);
  padding: 16px;
  display: flex;
  gap: 14px;
}

.v-icon { font-size: 28px; color: #10b981; }
.v-header { display: flex; justify-content: space-between; font-weight: 700; margin-bottom: 4px; }
.v-badge { background: #10b981; color: white; padding: 2px 8px; border-radius: 10px; font-size: 12px; }

.section-header { margin-bottom: 12px; }
.section-title { font-size: 16px; font-weight: 700; color: var(--text-title); display: flex; align-items: center; gap: 8px; }
.section-subtitle { font-size: 13px; color: var(--text-muted); }

.input-wrapper { position: relative; display: flex; align-items: center; }

.form-input {
  width: 100%;
  padding: 14px 100px 14px 16px;
  border-radius: 14px;
  background: var(--surface-input);
  border: 1.5px solid rgba(255, 255, 255, 0.8);
  font-size: 15px;
  font-weight: 600;
  color: var(--text-title);
}

.btn-save-email {
  position: absolute;
  right: 6px; top: 6px; bottom: 6px;
  padding: 0 16px;
  border-radius: 10px;
  background: var(--accent-main);
  color: white;
  border: none;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
}

.btn-save-email:disabled { opacity: 0.5; cursor: not-allowed; }

.email-msg-text { font-size: 13px; color: #10b981; margin-top: 6px; }
.email-msg-text.error { color: #ef4444; }

.suggestions-dropdown {
  background: white; border-radius: 12px; border: 1px solid #e2e8f0; margin-top: 4px; overflow: hidden;
}
.suggestion-item { padding: 10px 14px; cursor: pointer; font-size: 14px; }
.suggestion-item:hover { background: #f1f5f9; }

.modal-footer { display: flex; justify-content: flex-end; padding-top: 20px; border-top: 1px solid rgba(0,0,0,0.06); }
.btn-cancel { padding: 12px 24px; border-radius: 12px; background: rgba(15,23,42,0.05); border: none; font-weight: 600; cursor: pointer; }
</style>
