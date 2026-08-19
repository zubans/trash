<template>
  <div v-if="show" class="modal-overlay" @click.self="show = false">
    <div class="modal-card">
      <!-- Header -->
      <div class="modal-header">
        <div class="modal-title">
          <i class="ph-fill ph-user-circle"></i>
          Профиль заказчика
        </div>
        <button type="button" class="btn-close" aria-label="Закрыть" @click="show = false">
          <i class="ph ph-x"></i>
        </button>
      </div>

      <!-- Verification Status Box -->
      <div class="verification-box">
        <i class="ph-fill ph-shield-check v-icon"></i>
        <div class="v-content">
          <div class="v-header">
            <div class="v-title">
              {{ isVerified ? 'Пользователь верифицирован' : 'Статус верификации' }}
            </div>
            <div class="v-badge">
              {{ isVerified ? 'Подтвержден' : 'Не верифицирован' }}
            </div>
          </div>
          <div class="v-desc">
            {{ isVerified ? 'Ваш паспорт проверен администратором системы' : 'Для верификации передайте данные администратору' }}
          </div>
        </div>
      </div>
      <!-- Email Management -->
      <div class="section-header">
        <div class="section-title">
          <i class="ph-fill ph-envelope" style="color: #6366f1;"></i>
          Электронная почта
        </div>
        <div class="section-subtitle">При изменении потребуется повторная верификация</div>
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

      <!-- Address Management -->
      <div class="section-header">
        <div class="section-title">
          <i class="ph-fill ph-map-pin" style="color: #ef4444;"></i>
          Сохраненные адреса <span class="counter-badge">{{ customerAddresses.length }}/2</span>
        </div>
        <div class="section-subtitle">Выберите активный для заказов</div>
      </div>

      <!-- Address List -->
      <div class="address-list">
        <div v-if="customerAddresses.length === 0" class="empty-address-box">
          Нет сохраненных адресов. Добавьте адрес ниже.
        </div>

        <label
          v-for="(addr, idx) in customerAddresses"
          :key="idx"
          class="address-card"
        >
          <input
            type="radio"
            name="active_address"
            :checked="addr.address === defaultAddress"
            @change="$emit('setActiveAddress', addr.address)"
          />
          <div class="custom-radio"></div>
          <div class="address-details">
            <div class="address-text">{{ addr.address }}</div>
            <div class="active-label"><i class="ph-bold ph-check"></i> Активный для заказов</div>
          </div>
          <button
            type="button"
            class="btn-trash"
            title="Удалить адрес"
            @click.prevent="$emit('removeAddress', idx)"
          >
            <i class="ph ph-trash"></i>
          </button>
        </label>
      </div>

      <!-- Add New Address Form -->
      <div v-if="customerAddresses.length < 2" class="add-address-form">
        <input
          type="text"
          :value="newAddressInput"
          @input="$emit('update:newAddressInput', ($event.target as HTMLInputElement).value)"
          placeholder="Введите новый адрес..."
          class="add-input"
          @keyup.enter="$emit('addNewAddress')"
        />
        <button
          type="button"
          class="btn-add"
          :disabled="!newAddressInput.trim()"
          @click="$emit('addNewAddress')"
        >
          <i class="ph-bold ph-plus"></i> Добавить
        </button>
      </div>
      <div v-else class="limit-warning-banner">
        <span>ℹ️ Можно сохранить не более 2 адресов. Удалите один, чтобы добавить новый.</span>
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
import { defineComponent, computed, onMounted, ref, watch } from 'vue'
import api from '../../../services/api'

export default defineComponent({
  name: 'CustomerProfileModal',
  props: {
    modelValue: { type: Boolean, required: true },
    isVerified: { type: Boolean, default: true },
    userEmail: { type: String, default: '' },
    customerAddresses: { type: Array as () => any[], default: () => [] },
    defaultAddress: { type: String, default: '' },
    newAddressInput: { type: String, default: '' },
  },
  emits: [
    'update:modelValue',
    'update:newAddressInput',
    'setActiveAddress',
    'addNewAddress',
    'removeAddress',
    'emailUpdated',
  ],
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

    watch(() => props.userEmail, (val) => {
      emailInput.value = val
    })

    const saveEmail = async () => {
      if (!emailInput.value || emailInput.value === props.userEmail || savingEmail.value) return
      savingEmail.value = true
      emailMsg.value = ''
      emailMsgIsError.value = false
      try {
        await api.post('/user/email', { email: emailInput.value })
        emailMsg.value = 'Ссылка для подтверждения отправлена на ' + emailInput.value + '. Почта изменится после перехода по ссылке (действительна 60 минут).'
      } catch (err: any) {
        emailMsgIsError.value = true
        emailMsg.value = err.response?.data?.error || err.response?.data || 'Ошибка обновления Email'
      } finally {
        savingEmail.value = false
      }
    }

    const loadPhosphorIcons = () => {
      if (!document.getElementById('phosphor-icons-script')) {
        const script = document.createElement('script')
        script.id = 'phosphor-icons-script'
        script.src = 'https://unpkg.com/@phosphor-icons/web'
        document.head.appendChild(script)
      }
    }

    onMounted(() => {
      loadPhosphorIcons()
    })

    return { 
      show,
      currentEmail,
      emailInput,
      savingEmail,
      emailMsg,
      emailMsgIsError,
      saveEmail
    }
  },
})
</script>

<style scoped>
@import url('https://fonts.googleapis.com/css2?family=Outfit:wght@300;400;500;600;700&display=swap');

/* --- Modal Overlay --- */
.modal-overlay {
  --bg-base: #f8f9fa;
  --surface-card: rgba(255, 255, 255, 0.92);
  --surface-input: rgba(255, 255, 255, 0.7);
  
  --text-title: #0f172a;
  --text-body: #334155;
  --text-muted: #8b98a5;
  
  --accent-main: #6366f1;
  --accent-glow: rgba(99, 102, 241, 0.4);
  
  --success-main: #10b981;
  --success-bg: rgba(16, 185, 129, 0.08);
  
  --rad-sm: 12px;
  --rad-md: 16px;
  --rad-lg: 32px;
  
  --shadow-float: 0 20px 50px -10px rgba(15, 23, 42, 0.1), 
                  0 1px 3px rgba(15, 23, 42, 0.05),
                  inset 0 1px 0 rgba(255,255,255,1);
  
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
  animation: fadeIn 0.3s ease-out;
  font-family: 'Outfit', sans-serif;
  color: var(--text-body);
}

@keyframes fadeIn { from { opacity: 0; } to { opacity: 1; } }

/* --- Modal Card --- */
.modal-card {
  background: var(--surface-card);
  backdrop-filter: blur(24px);
  -webkit-backdrop-filter: blur(24px);
  border: 1px solid rgba(255, 255, 255, 0.8);
  border-radius: var(--rad-lg);
  width: 100%;
  max-width: 560px;
  box-shadow: var(--shadow-float);
  padding: 32px;
  position: relative;
  animation: slideUp 0.4s cubic-bezier(0.16, 1, 0.3, 1);
  max-height: 90vh;
  overflow-y: auto;
}

@keyframes slideUp {
  from { opacity: 0; transform: translateY(20px) scale(0.95); }
  to { opacity: 1; transform: translateY(0) scale(1); }
}

/* --- Modal Header --- */
.modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 24px;
}

.modal-title {
  font-size: 24px;
  font-weight: 700;
  color: var(--text-title);
  letter-spacing: -0.5px;
  display: flex;
  align-items: center;
  gap: 12px;
}

.modal-title i {
  color: var(--accent-main);
  font-size: 28px;
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
  transition: var(--transition);
}

.btn-close:hover {
  background: #ffffff;
  color: #ef4444;
  box-shadow: 0 4px 12px rgba(0,0,0,0.05);
  transform: rotate(90deg);
}

/* --- Verification Box --- */
.verification-box {
  background: var(--success-bg);
  border: 1px solid rgba(16, 185, 129, 0.2);
  border-radius: var(--rad-md);
  padding: 16px 20px;
  margin-bottom: 32px;
  display: flex;
  align-items: flex-start;
  gap: 16px;
  position: relative;
  overflow: hidden;
}

.verification-box::before {
  content: '';
  position: absolute;
  left: 0; top: 0; bottom: 0; width: 4px;
  background: var(--success-main);
}

.v-icon {
  font-size: 24px;
  color: var(--success-main);
  margin-top: 2px;
}

.v-content { flex: 1; }

.v-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 4px;
}

.v-title {
  font-size: 16px;
  font-weight: 700;
  color: var(--text-title);
}

.v-badge {
  background: #ecfdf5;
  color: #059669;
  padding: 4px 10px;
  border-radius: 99px;
  font-size: 12px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.v-desc {
  font-size: 14px;
  color: var(--text-body);
}

/* --- Address Section --- */
.section-header {
  margin-bottom: 16px;
}

.section-title {
  font-size: 18px;
  font-weight: 700;
  color: var(--text-title);
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;
}

.counter-badge {
  background: rgba(99, 102, 241, 0.1);
  color: var(--accent-main);
  padding: 2px 8px;
  border-radius: 99px;
  font-size: 13px;
  font-weight: 700;
}

.section-subtitle {
  font-size: 13px;
  color: var(--text-muted);
}

.address-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-bottom: 20px;
}

.empty-address-box {
  padding: 16px;
  text-align: center;
  border: 1px dashed #cbd5e1;
  border-radius: var(--rad-md);
  font-size: 13px;
  color: var(--text-muted);
}

.address-card {
  display: flex;
  align-items: center;
  padding: 16px;
  background: var(--surface-input);
  border: 1.5px solid rgba(255,255,255,0.8);
  border-radius: var(--rad-md);
  cursor: pointer;
  transition: var(--transition);
  position: relative;
}

.address-card:hover {
  background: #ffffff;
  box-shadow: 0 4px 12px rgba(0,0,0,0.03);
}

.address-card input[type="radio"] {
  position: absolute;
  opacity: 0;
}

.custom-radio {
  width: 22px;
  height: 22px;
  border-radius: 50%;
  border: 2px solid #cbd5e1;
  margin-right: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: var(--transition);
}

.custom-radio::after {
  content: '';
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: var(--accent-main);
  transform: scale(0);
  transition: var(--transition);
}

.address-card:has(input:checked) {
  background: rgba(99, 102, 241, 0.03);
  border-color: var(--accent-main);
}

.address-card input:checked + .custom-radio {
  border-color: var(--accent-main);
}

.address-card input:checked + .custom-radio::after {
  transform: scale(1);
}

.address-details {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.address-text {
  font-size: 15px;
  font-weight: 500;
  color: var(--text-title);
  line-height: 1.3;
}

.active-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--accent-main);
  display: flex;
  align-items: center;
  gap: 4px;
  opacity: 0;
  transition: var(--transition);
}

.address-card input:checked ~ .address-details .active-label {
  opacity: 1;
}

.btn-trash {
  width: 36px;
  height: 36px;
  border-radius: 10px;
  background: transparent;
  border: none;
  color: #94a3b8;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
  cursor: pointer;
  transition: var(--transition);
  margin-left: 12px;
}

.btn-trash:hover {
  background: #fee2e2;
  color: #ef4444;
}

/* --- Add Address Form --- */
.add-address-form {
  display: flex;
  gap: 8px;
  margin-bottom: 32px;
}

.add-input {
  flex: 1;
  padding: 14px 16px;
  border-radius: var(--rad-sm);
  background: var(--surface-input);
  border: 1.5px solid rgba(255, 255, 255, 0.8);
  font-family: inherit;
  font-size: 15px;
  color: var(--text-title);
  transition: var(--transition);
}

.add-input:focus {
  outline: none;
  border-color: var(--accent-main);
  background: #ffffff;
  box-shadow: 0 0 0 4px rgba(99, 102, 241, 0.1);
}

.btn-add {
  background: var(--accent-main);
  color: white;
  border: none;
  padding: 0 20px;
  border-radius: var(--rad-sm);
  font-size: 15px;
  font-weight: 600;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 6px;
  transition: var(--transition);
}

.btn-add:hover {
  background: #4f46e5;
  box-shadow: 0 4px 12px rgba(99, 102, 241, 0.3);
}

.btn-add:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.limit-warning-banner {
  font-size: 13px;
  color: #b45309;
  background: #fef3c7;
  padding: 10px 14px;
  border-radius: var(--rad-sm);
  margin-bottom: 24px;
}

/* --- Footer --- */
.modal-footer {
  display: flex;
  justify-content: flex-end;
  padding-top: 24px;
  border-top: 1px solid rgba(0,0,0,0.06);
}

.btn-cancel {
  padding: 14px 28px;
  border-radius: 14px;
  background: rgba(15, 23, 42, 0.05);
  color: var(--text-body);
  border: none;
  font-size: 15px;
  font-weight: 600;
  cursor: pointer;
  transition: var(--transition);
}

.btn-cancel:hover {
  background: rgba(15, 23, 42, 0.1);
  color: var(--text-title);
}

.email-box {
  margin-bottom: 24px;
}

.input-wrapper {
  position: relative;
  display: flex;
  align-items: center;
  width: 100%;
}

.form-input {
  width: 100%;
  padding: 14px 110px 14px 16px;
  border-radius: 14px;
  background: var(--surface-input);
  border: 1.5px solid rgba(255, 255, 255, 0.8);
  font-family: inherit;
  font-size: 15px;
  font-weight: 600;
  color: var(--text-title);
  box-shadow: inset 0 2px 4px rgba(0,0,0,0.02);
  transition: var(--transition);
}

.form-input:focus {
  outline: none;
  border-color: var(--accent-main);
  background: #ffffff;
  box-shadow: 0 0 0 4px rgba(99, 102, 241, 0.1);
}

.btn-save-email {
  position: absolute;
  right: 6px;
  top: 6px;
  bottom: 6px;
  padding: 0 18px;
  border-radius: 10px;
  background: var(--accent-main);
  color: white;
  border: none;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  transition: var(--transition);
}

.btn-save-email:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.email-msg-text {
  font-size: 13px;
  color: #10b981;
  margin-top: 8px;
  line-height: 1.4;
}

.email-msg-text.error {
  color: #ef4444;
}

/* Responsive */
@media (max-width: 480px) {
  .modal-card { padding: 24px; border-radius: 28px; }
  .v-header { flex-direction: column; align-items: flex-start; gap: 8px; }
  .add-address-form { flex-direction: column; }
  .btn-add { padding: 14px; justify-content: center; }
  .modal-footer { justify-content: stretch; }
  .btn-cancel { width: 100%; }
}
</style>
