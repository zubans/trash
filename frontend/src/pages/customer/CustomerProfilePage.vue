<template>
  <div class="profile-page-wrapper">
    <div class="profile-container">
      <!-- Top Navigation Header -->
      <div class="top-nav">
        <button type="button" class="btn-back" @click="goBack">
          <i class="ph-bold ph-arrow-left"></i>
          Вернуться на главную
        </button>
        <div class="page-header-title">
          <i class="ph-fill ph-user-circle icon-title"></i>
          Профиль заказчика
        </div>
      </div>

      <div class="profile-card">
        <!-- Phone Banner -->
        <div class="user-phone-banner">
          <div class="banner-avatar">
            <i class="ph ph-user"></i>
          </div>
          <div class="banner-info">
            <div class="banner-phone-row">
              <span class="banner-phone">{{ userPhone || '7 920 705 07 07' }}</span>
            </div>
            <div v-if="userFullName" class="banner-fullname">{{ userFullName }}</div>
            <span v-else class="banner-subtitle">Личный кабинет заказчика</span>
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
              @change="setActiveAddress(addr.address)"
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
              @click.prevent="removeAddress(idx)"
            >
              <i class="ph ph-trash"></i>
            </button>
          </label>
        </div>

        <!-- Add New Address Form -->
        <div v-if="customerAddresses.length < 2" class="add-address-form">
          <input
            type="text"
            v-model="newAddressInput"
            placeholder="Введите новый адрес..."
            class="add-input"
            @keyup.enter="addNewAddress"
          />
          <button
            type="button"
            class="btn-add"
            :disabled="!newAddressInput.trim()"
            @click="addNewAddress"
          >
            <i class="ph-bold ph-plus"></i> Добавить
          </button>
        </div>
        <div v-else class="limit-warning-banner">
          <span>ℹ️ Можно сохранить не более 2 адресов. Удалите один, чтобы добавить новый.</span>
        </div>

        <!-- Action Footer -->
        <div class="profile-actions">
          <button type="button" class="btn-back-home" @click="goBack">
            <i class="ph-bold ph-house"></i>
            Вернуться на главную
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import api from '../../services/api'
import { useAuthStore } from '../../stores/auth-store'

export default defineComponent({
  name: 'CustomerProfilePage',
  setup() {
    const router = useRouter()
    const authStore = useAuthStore()

    const userEmail = ref('')
    const userPhone = ref('')
    const customerAddresses = ref<any[]>([])
    const defaultAddress = ref('')
    const newAddressInput = ref('')

    const emailInput = ref('')
    const savingEmail = ref(false)
    const userFullName = ref('')

    const currentEmail = computed(() => userEmail.value)

    const fetchUserProfile = async () => {
      try {
        const meRes = await api.get('/auth/me').catch(() => null)
        if (meRes?.data) {
          const parts = [meRes.data.last_name, meRes.data.first_name, meRes.data.patronymic].filter((p: string) => p && p.trim())
          userFullName.value = parts.join(' ')
        }
        const res = await api.get('/user/profile')
        userPhone.value = res.data.phone || authStore.userPhone || ''
        userEmail.value = res.data.email || ''
        emailInput.value = userEmail.value
        customerAddresses.value = res.data.addresses || []
        defaultAddress.value = res.data.default_address || (customerAddresses.value[0]?.address || '')
      } catch (err) {
        console.error('Failed to load profile:', err)
      }
    }

    const saveEmail = async () => {
      if (!emailInput.value || emailInput.value === userEmail.value || savingEmail.value) return
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

    const setActiveAddress = async (addr: string) => {
      try {
        await api.post('/user/address/default', { address: addr })
        defaultAddress.value = addr
      } catch (err) {
        console.error('Failed to set default address:', err)
      }
    }

    const addNewAddress = async () => {
      if (!newAddressInput.value.trim() || customerAddresses.value.length >= 2) return
      try {
        const res = await api.post('/user/address', { address: newAddressInput.value.trim() })
        customerAddresses.value = res.data.addresses || []
        if (customerAddresses.value.length === 1) {
          defaultAddress.value = newAddressInput.value.trim()
        }
        newAddressInput.value = ''
      } catch (err) {
        console.error('Failed to add address:', err)
      }
    }

    const removeAddress = async (idx: number) => {
      try {
        const res = await api.delete(`/user/address/${idx}`)
        customerAddresses.value = res.data.addresses || []
        if (!customerAddresses.value.some((a: any) => a.address === defaultAddress.value)) {
          defaultAddress.value = customerAddresses.value[0]?.address || ''
        }
      } catch (err) {
        console.error('Failed to remove address:', err)
      }
    }

    const goBack = () => {
      router.push('/customer')
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
      fetchUserProfile()
    })

    return {
      userEmail,
      userFullName,
      userPhone,
      customerAddresses,
      defaultAddress,
      newAddressInput,
      emailInput,
      savingEmail,
      emailMsg,
      emailMsgIsError,
      currentEmail,
      saveEmail,
      setActiveAddress,
      addNewAddress,
      removeAddress,
      goBack
    }
  }
})
</script>

<style scoped>
@import url('https://fonts.googleapis.com/css2?family=Outfit:wght@300;400;500;600;700&display=swap');

.profile-page-wrapper {
  min-height: 100vh;
  background: #f8fafc;
  font-family: 'Outfit', sans-serif;
  padding: 32px 16px;
  color: #334155;
}

.profile-container {
  max-width: 640px;
  margin: 0 auto;
}

.top-nav {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 24px;
}

.btn-back {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  background: #ffffff;
  border: 1px solid #e2e8f0;
  padding: 10px 18px;
  border-radius: 12px;
  color: #0f172a;
  font-weight: 600;
  font-size: 14px;
  cursor: pointer;
  transition: all 0.2s ease;
  box-shadow: 0 2px 8px rgba(0,0,0,0.03);
}

.btn-back:hover {
  background: #f1f5f9;
  border-color: #cbd5e1;
  transform: translateX(-2px);
}

.page-header-title {
  font-size: 20px;
  font-weight: 700;
  color: #0f172a;
  display: flex;
  align-items: center;
  gap: 8px;
}

.icon-title {
  font-size: 26px;
  color: #6366f1;
}

.profile-card {
  background: #ffffff;
  border-radius: 24px;
  border: 1px solid #e2e8f0;
  box-shadow: 0 10px 30px rgba(15, 23, 42, 0.05);
  padding: 32px;
}

/* User phone banner */
.user-phone-banner {
  display: flex;
  align-items: center;
  gap: 16px;
  background: #f1f5f9;
  border-radius: 16px;
  padding: 20px;
  margin-bottom: 28px;
}

.banner-avatar {
  width: 52px;
  height: 52px;
  border-radius: 14px;
  background: #6366f1;
  color: #ffffff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 26px;
}

.banner-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.banner-phone-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.banner-phone { font-size: 18px; font-weight: 700; color: #0f172a; letter-spacing: -0.3px; }
.banner-fullname { font-size: 14px; font-weight: 600; color: #0f172a; margin-top: 2px; }
.banner-subtitle { font-size: 13px; color: #64748b; display: block; margin-top: 2px; }

/* Section Header */
.section-header {
  margin-bottom: 14px;
}

.section-title {
  font-size: 16px;
  font-weight: 600;
  color: #0f172a;
  display: flex;
  align-items: center;
  gap: 8px;
}

.section-subtitle {
  font-size: 13px;
  color: #8b98a5;
  margin-top: 2px;
}

.counter-badge {
  font-size: 12px;
  background: #e2e8f0;
  color: #475569;
  padding: 2px 8px;
  border-radius: 20px;
  margin-left: 4px;
}

/* Email Box */
.email-box {
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  border-radius: 16px;
  padding: 16px;
}

.input-wrapper {
  display: flex;
  gap: 12px;
}

.form-input {
  flex: 1;
  background: #ffffff;
  border: 1px solid #cbd5e1;
  border-radius: 12px;
  padding: 12px 16px;
  font-size: 15px;
  color: #0f172a;
  outline: none;
  transition: all 0.2s ease;
}

.form-input:focus {
  border-color: #6366f1;
  box-shadow: 0 0 0 3px rgba(99, 102, 241, 0.15);
}

.btn-save-email {
  background: #6366f1;
  color: #ffffff;
  border: none;
  border-radius: 12px;
  padding: 12px 20px;
  font-weight: 600;
  font-size: 14px;
  cursor: pointer;
  transition: all 0.2s ease;
}

.btn-save-email:hover:not(:disabled) {
  background: #4f46e5;
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

/* Address List */
.address-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-bottom: 16px;
}

.empty-address-box {
  background: #f8fafc;
  border: 1px dashed #cbd5e1;
  border-radius: 14px;
  padding: 16px;
  text-align: center;
  font-size: 14px;
  color: #64748b;
}

.address-card {
  display: flex;
  align-items: center;
  gap: 14px;
  background: #f8fafc;
  border: 1.5px solid #e2e8f0;
  border-radius: 16px;
  padding: 14px 18px;
  cursor: pointer;
  transition: all 0.2s ease;
  position: relative;
}

.address-card:hover {
  border-color: #cbd5e1;
  background: #ffffff;
}

.address-card input[type="radio"] {
  display: none;
}

.custom-radio {
  width: 20px;
  height: 20px;
  border-radius: 50%;
  border: 2px solid #cbd5e1;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.2s ease;
  flex-shrink: 0;
}

.custom-radio::after {
  content: '';
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: #6366f1;
  transform: scale(0);
  transition: transform 0.2s ease;
}

.address-card input[type="radio"]:checked + .custom-radio {
  border-color: #6366f1;
}

.address-card input[type="radio"]:checked + .custom-radio::after {
  transform: scale(1);
}

.address-card input[type="radio"]:checked ~ .address-details .active-label {
  display: flex;
}

.address-details {
  flex: 1;
}

.address-text {
  font-size: 15px;
  font-weight: 500;
  color: #0f172a;
}

.active-label {
  display: none;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  font-weight: 600;
  color: #10b981;
  margin-top: 4px;
}

.btn-trash {
  background: transparent;
  border: none;
  color: #94a3b8;
  font-size: 18px;
  cursor: pointer;
  padding: 6px;
  border-radius: 8px;
  transition: all 0.2s ease;
}

.btn-trash:hover {
  color: #ef4444;
  background: #fee2e2;
}

/* Add address form */
.add-address-form {
  display: flex;
  gap: 12px;
  margin-top: 16px;
}

.add-input {
  flex: 1;
  background: #ffffff;
  border: 1px solid #cbd5e1;
  border-radius: 12px;
  padding: 12px 16px;
  font-size: 14px;
  outline: none;
  transition: all 0.2s ease;
}

.add-input:focus {
  border-color: #6366f1;
  box-shadow: 0 0 0 3px rgba(99, 102, 241, 0.15);
}

.btn-add {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: #0f172a;
  color: #ffffff;
  border: none;
  border-radius: 12px;
  padding: 12px 18px;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s ease;
}

.btn-add:hover:not(:disabled) {
  background: #1e293b;
}

.btn-add:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.limit-warning-banner {
  background: #eff6ff;
  border: 1px solid #bfdbfe;
  border-radius: 12px;
  padding: 12px 16px;
  font-size: 13px;
  color: #1e40af;
  margin-top: 16px;
}

.profile-actions {
  margin-top: 32px;
  padding-top: 24px;
  border-top: 1px solid #e2e8f0;
  display: flex;
  justify-content: flex-end;
}

.btn-back-home {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  background: #f1f5f9;
  border: 1px solid #cbd5e1;
  color: #334155;
  border-radius: 12px;
  padding: 12px 24px;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s ease;
}

.btn-back-home:hover {
  background: #e2e8f0;
  color: #0f172a;
}

@media (max-width: 640px) {
  .profile-card { padding: 20px; border-radius: 20px; }
  .top-nav { flex-direction: column; align-items: flex-start; gap: 12px; }
  .input-wrapper, .add-address-form { flex-direction: column; }
  .btn-save-email, .btn-add { width: 100%; justify-content: center; }
  .btn-back-home { width: 100%; justify-content: center; }
}
</style>
