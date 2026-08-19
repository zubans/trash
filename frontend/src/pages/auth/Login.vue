<template>
  <div class="login-wrapper">
    <!-- Floating Orbs Background -->
    <div class="bg-orb orb-1"></div>
    <div class="bg-orb orb-2"></div>

    <!-- Auth Card -->
    <div class="auth-card">
      <!-- Header with Tab Control & Language Switcher -->
      <div class="card-header">
        <div class="tabs-container">
          <div
            :class="['tab', { active: mode === 'login' }]"
            @click="mode = 'login'"
          >
            {{ $t('login.signIn') }}
          </div>
          <div
            :class="['tab', { active: mode === 'register' }]"
            @click="mode = 'register'"
          >
            {{ $t('login.register') }}
          </div>
        </div>

        <div class="lang-btn-wrapper">
          <LanguageSwitcher />
        </div>
      </div>

      <!-- Title -->
      <h1>
        {{ mode === 'login' ? $t('login.welcomeBack') : $t('login.createAccount') }}
      </h1>

      <!-- Alerts -->
      <transition name="fade">
        <div v-if="error" class="custom-alert error-alert mb-4">
          <i class="ph ph-warning-circle alert-icon"></i>
          <span class="alert-text" style="white-space: pre-wrap;">{{ error }}</span>
        </div>
      </transition>

      <transition name="fade">
        <div v-if="message" class="custom-alert success-alert mb-4">
          <i class="ph ph-check-circle alert-icon"></i>
          <span class="alert-text">{{ message }}</span>
        </div>
      </transition>

      <!-- Form -->
      <form @submit.prevent="handleSubmit">
        <!-- Phone Input -->
        <div class="form-group mb-3">
          <label class="form-label">{{ $t('login.phone') }}</label>
          <div class="input-wrapper">
            <input
              v-model="displayPhone"
              type="tel"
              placeholder="+7 (999) 999-99-99"
              class="form-input"
              required
              @input="onPhoneInput"
            />
            <i class="ph ph-phone input-icon"></i>
          </div>
        </div>

        <!-- Password Input -->
        <div class="form-group mb-3">
          <label class="form-label">{{ $t('login.password') }}</label>
          <div class="input-wrapper">
            <input
              v-model="password"
              type="password"
              placeholder="••••••••"
              class="form-input"
              required
            />
            <i class="ph ph-lock-key input-icon"></i>
          </div>
        </div>

        <!-- Registration Role Select -->
        <div v-if="mode === 'register'" class="form-group mb-3">
          <label class="form-label">{{ $t('login.role') }}</label>
          <div class="input-wrapper">
            <select v-model="role" class="form-input custom-select" required>
              <option value="CUSTOMER">{{ $t('roles.customer') }}</option>
              <option value="EXECUTOR">{{ $t('roles.executor') }}</option>
            </select>
            <i class="ph ph-user input-icon"></i>
          </div>
        </div>

        <!-- Address Autocomplete for Registration -->
        <div v-if="mode === 'register'" class="form-group mb-3 address-autocomplete">
          <label class="form-label">{{ role === 'CUSTOMER' ? $t('login.pickupAddress') : 'Базовый адрес (откуда искать заказы)' }}</label>
          <div class="input-wrapper">
            <input
              v-model="address"
              type="text"
              :placeholder="$t('login.pickupAddressPlaceholder')"
              class="form-input"
              required
              autocomplete="off"
              @input="onAddressInput"
            />
            <i class="ph ph-map-pin input-icon"></i>
            <span v-if="autocompleteLoading" class="input-spinner" />
          </div>
          <div v-if="addressSuggestions.length > 0" class="suggestions-dropdown">
            <div
              v-for="(suggestion, index) in addressSuggestions"
              :key="index"
              class="suggestion-item"
              @click="selectAddress(suggestion)"
            >
              {{ suggestion.display }}
            </div>
          </div>
          <div class="text-secondary text-xs mt-2">{{ $t('login.addressHint') }}</div>
        </div>

        <!-- Flat Number for Customer Registration -->
        <div v-if="mode === 'register' && role === 'CUSTOMER'" class="form-group mb-3">
          <label class="form-label">{{ $t('login.flatNumber') }}</label>
          <div class="input-wrapper">
            <input
              v-model="flatNumber"
              type="text"
              :placeholder="$t('login.flatNumberPlaceholder')"
              class="form-input"
              autocomplete="off"
            />
            <i class="ph ph-buildings input-icon"></i>
          </div>
        </div>

        <!-- Email Input for Registration -->
        <div v-if="mode === 'register'" class="form-group mb-3">
          <label class="form-label">Email</label>
          <div class="input-wrapper">
            <input
              v-model="email"
              type="email"
              placeholder="example@domain.com"
              class="form-input"
              required
            />
            <i class="ph ph-envelope input-icon"></i>
          </div>
        </div>

        <!-- Forgot Password Link (for login mode) -->
        <a v-if="mode === 'login'" href="#" class="forgot-link" @click.prevent="openForgotModal">
          Забыли пароль?
        </a>

        <!-- Submit Button -->
        <button ref="submitBtnRef" type="submit" class="submit-btn" :disabled="loading">
          <span v-if="loading" class="spinner"></span>
          <template v-else>
            {{ mode === 'login' ? $t('login.signInBtn') : $t('login.signUpBtn') }}
            <i class="ph-bold ph-arrow-right"></i>
          </template>
        </button>
      </form>
    </div>

    <!-- Forgot/Reset Password Modal -->
    <div v-if="showForgotModal" class="forgot-modal-overlay" @click.self="showForgotModal = false">
      <div class="forgot-modal-card">
        <div class="forgot-modal-header">
          <div class="forgot-modal-title">Сброс пароля</div>
          <button type="button" class="btn-close-forgot" @click="showForgotModal = false">
            <i class="ph ph-x"></i>
          </button>
        </div>

        <!-- Step 1: Request Code -->
        <form v-if="resetStep === 1" @submit.prevent="requestResetCode">
          <p class="forgot-desc">Введите ваш Email, чтобы получить 6-значный код восстановления.</p>
          <div class="form-group mb-3">
            <label class="form-label">Email</label>
            <div class="input-wrapper">
              <input v-model="resetEmail" type="email" class="form-input" placeholder="example@domain.com" required />
              <i class="ph ph-envelope input-icon"></i>
            </div>
          </div>
          <div v-if="resetError" class="custom-alert error-alert mb-3">
            <span class="alert-text">{{ resetError }}</span>
          </div>
          <button type="submit" class="submit-btn" :disabled="resetLoading">
            <span v-if="resetLoading" class="spinner"></span>
            <template v-else>Получить код <i class="ph-bold ph-paper-plane-tilt"></i></template>
          </button>
        </form>

        <!-- Step 2: Enter Code & New Password -->
        <form v-else @submit.prevent="submitNewPassword">
          <p class="forgot-desc">Код отправлен на {{ resetEmail }}. Введите код и новый пароль.</p>
          <div class="form-group mb-3">
            <label class="form-label">Код из Email</label>
            <div class="input-wrapper">
              <input v-model="resetCode" type="text" class="form-input" placeholder="123456" maxlength="6" required />
              <i class="ph ph-key input-icon"></i>
            </div>
          </div>
          <div class="form-group mb-3">
            <label class="form-label">Новый пароль</label>
            <div class="input-wrapper">
              <input v-model="newPassword" type="password" class="form-input" placeholder="••••••••" required />
              <i class="ph ph-lock-key input-icon"></i>
            </div>
          </div>
          <div v-if="resetError" class="custom-alert error-alert mb-3">
            <span class="alert-text">{{ resetError }}</span>
          </div>
          <button type="submit" class="submit-btn" :disabled="resetLoading">
            <span v-if="resetLoading" class="spinner"></span>
            <template v-else>Сохранить новый пароль <i class="ph-bold ph-check"></i></template>
          </button>
        </form>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, watch, onMounted, nextTick } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../../stores/auth-store'
import api, { formatApiError } from '../../services/api'
import LanguageSwitcher from '../../components/LanguageSwitcher.vue'
import { formatPhoneMask, cleanPhoneDigits } from '../../utils/phoneMask'

function parseJwt(token: string) {
  try {
    const base64Url = token.split('.')[1]
    const base64 = base64Url.replace(/-/g, '+').replace(/_/g, '/')
    const jsonPayload = decodeURIComponent(
      window.atob(base64)
        .split('')
        .map((c) => '%' + ('00' + c.charCodeAt(0).toString(16)).slice(-2))
        .join('')
    )
    return JSON.parse(jsonPayload)
  } catch (e) {
    return null
  }
}

export default defineComponent({
  name: 'Login',
  components: { LanguageSwitcher },
  setup() {
    const router = useRouter()
    const route = useRoute()
    const authStore = useAuthStore()
    const { t } = useI18n()

    const submitBtnRef = ref<HTMLButtonElement | null>(null)
    const mode = ref<'login' | 'register'>('login')
    const displayPhone = ref('')
    const phone = ref('')

    const onPhoneInput = (e: Event) => {
      const target = e.target as HTMLInputElement
      const formatted = formatPhoneMask(target.value)
      displayPhone.value = formatted
      phone.value = cleanPhoneDigits(formatted)
    }
    const email = ref('')
    const password = ref('')
    const role = ref<'CUSTOMER' | 'EXECUTOR'>('CUSTOMER')
    const address = ref('')
    const flatNumber = ref('')
    const addressSuggestions = ref<any[]>([])
    const autocompleteLoading = ref(false)
    const selectedCoords = ref<{ lat: number; lon: number } | null>(null)
    const error = ref('')
    const message = ref('')
    const loading = ref(false)
    let autocompleteTimeout: any = null

    // Forgot Password Modal State
    const showForgotModal = ref(false)
    const resetStep = ref<1 | 2>(1)
    const resetEmail = ref('')
    const resetCode = ref('')
    const newPassword = ref('')
    const resetError = ref('')
    const resetLoading = ref(false)

    const openForgotModal = () => {
      showForgotModal.value = true
      resetStep.value = 1
      resetEmail.value = ''
      resetCode.value = ''
      newPassword.value = ''
      resetError.value = ''
    }

    const requestResetCode = async () => {
      resetError.value = ''
      if (!resetEmail.value) return
      resetLoading.value = true
      try {
        const res = await api.post('/auth/forgot-password', { email: resetEmail.value })
        resetStep.value = 2
        // Show reset code if returned in dev
        if (res.data.code) {
          resetError.value = `[Тестовый режим] Код сброса: ${res.data.code}`
        }
      } catch (err: any) {
        resetError.value = err.response?.data || 'Ошибка отправки кода сброса'
      } finally {
        resetLoading.value = false
      }
    }

    const submitNewPassword = async () => {
      resetError.value = ''
      if (!resetCode.value || !newPassword.value) return
      resetLoading.value = true
      try {
        await api.post('/auth/reset-password', {
          email: resetEmail.value,
          code: resetCode.value,
          new_password: newPassword.value,
        })
        showForgotModal.value = false
        message.value = 'Пароль успешно изменен! Войдите с новым паролем.'
      } catch (err: any) {
        resetError.value = err.response?.data || 'Ошибка изменения пароля'
      } finally {
        resetLoading.value = false
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

    const focusSubmitButton = () => {
      nextTick(() => {
        submitBtnRef.value?.focus()
      })
    }

    onMounted(async () => {
      loadPhosphorIcons()
      focusSubmitButton()

      const token = route.query.token as string
      if (token) {
        try {
          const res = await api.get('/auth/verify-email', { params: { token } })
          message.value = res.data?.message || 'Email успешно подтверждён!'
        } catch (err: any) {
          if (err.response?.data?.can_retry || err.response?.data?.code === 'TOKEN_EXPIRED') {
            error.value = err.response.data.error || 'Срок действия ссылки истек (60 минут). Запросите изменение почты заново в профиле.'
          } else {
            error.value = err.response?.data?.error || err.response?.data || 'Ошибка подтверждения Email'
          }
        }
      }
    })

    watch(mode, () => {
      error.value = ''
      message.value = ''
      addressSuggestions.value = []
      autocompleteLoading.value = false
      role.value = 'CUSTOMER'
      flatNumber.value = ''
      selectedCoords.value = null
      focusSubmitButton()
    })

    const onAddressInput = () => {
      addressSuggestions.value = []
      selectedCoords.value = null
      clearTimeout(autocompleteTimeout)
      const query = address.value.trim()
      if (query.length < 3) {
        autocompleteLoading.value = false
        return
      }
      autocompleteLoading.value = true
      autocompleteTimeout = setTimeout(async () => {
        try {
          const response = await api.get('/geo/autocomplete', { params: { q: query } })
          addressSuggestions.value = response.data || []
        } catch (err) {
          console.error('Autocomplete failed:', err)
        } finally {
          autocompleteLoading.value = false
        }
      }, 400)
    }

    const selectAddress = (suggestion: any) => {
      address.value = suggestion.address
      selectedCoords.value = { lat: suggestion.lat, lon: suggestion.lon }
      addressSuggestions.value = []
    }

    function normalizeAddress(streetAddress: string, flat?: string): string {
      const flatPart = flat && flat.trim() ? ` кв. ${flat.trim()}` : ''
      const full = `${streetAddress.trim()}${flatPart}`
      const match = full.match(/^Россия,\s*([^,]+?),\s*([^,]+?),\s*д\.\s*(\d+)(?:\s+кв\.\s*(\d+))?$/i)
      if (!match) {
        throw new Error(t('login.addressFormatError'))
      }
      const city = match[1].trim()
      const road = match[2].trim()
      const house = match[3].trim()
      const flatNum = match[4] ? match[4].trim() : (flat && flat.trim() ? flat.trim() : '')
      let result = `Россия, ${city}, ${road}, д. ${house}`
      if (flatNum) {
        result += ` кв. ${flatNum}`
      }
      return result
    }

    const handleSubmit = async () => {
      error.value = ''
      message.value = ''
      loading.value = true

      try {
        if (mode.value === 'login') {
          const response = await api.post('/login', {
            phone: phone.value,
            password: password.value,
          })
          const token = response.data.token
          const claims = parseJwt(token)

          if (!claims) {
            error.value = t('login.failedParse')
            return
          }

          authStore.login(token, claims.role, claims.phone, claims.sub)

          if (claims.role === 'ADMIN') {
            router.push('/admin')
          } else if (claims.role === 'CUSTOMER') {
            router.push('/customer')
          } else if (claims.role === 'EXECUTOR') {
            router.push('/executor')
          } else {
            error.value = t('login.roleNotSupported')
          }
        } else {
          const payload: any = {
            phone: phone.value,
            email: email.value,
            password: password.value,
            role: role.value,
          }

          let normalizedAddress: string
          try {
            normalizedAddress = normalizeAddress(address.value, flatNumber.value)
          } catch (addrErr: any) {
            error.value = addrErr.message || t('login.addressFormatError')
            return
          }
          payload.address = normalizedAddress
          if (selectedCoords.value) {
            payload.lat = selectedCoords.value.lat
            payload.lon = selectedCoords.value.lon
          }

          await api.post('/register', payload)
          message.value = 'Регистрация успешна! Код/ссылка подтверждения отправлена на ' + email.value
          mode.value = 'login'
          email.value = ''
          password.value = ''
          role.value = 'CUSTOMER'
          address.value = ''
          flatNumber.value = ''
          selectedCoords.value = null
        }
      } catch (err: any) {
        error.value = formatApiError(err, t('login.networkError'))
      } finally {
        loading.value = false
      }
    }

    return {
      submitBtnRef,
      mode,
      displayPhone,
      phone,
      onPhoneInput,
      email,
      password,
      role,
      address,
      flatNumber,
      addressSuggestions,
      autocompleteLoading,
      error,
      message,
      loading,
      handleSubmit,
      onAddressInput,
      selectAddress,
      showForgotModal,
      resetStep,
      resetEmail,
      resetCode,
      newPassword,
      resetError,
      resetLoading,
      openForgotModal,
      requestResetCode,
      submitNewPassword,
    }
  },
})
</script>

<style scoped>
@import url('https://fonts.googleapis.com/css2?family=Outfit:wght@300;400;500;600;700&display=swap');

.login-wrapper {
  --bg-base: #f8f9fa;
  --surface-card: rgba(255, 255, 255, 0.7);
  --surface-input: rgba(255, 255, 255, 0.6);
  
  --text-title: #0f172a;
  --text-body: #334155;
  --text-muted: #8b98a5;
  
  --accent-main: #6366f1;
  --accent-glow: rgba(99, 102, 241, 0.4);
  
  --rad-sm: 12px;
  --rad-md: 20px;
  --rad-lg: 32px;
  
  --shadow-float: 0 20px 50px -10px rgba(15, 23, 42, 0.1), 
                  0 1px 3px rgba(15, 23, 42, 0.05),
                  inset 0 1px 0 rgba(255,255,255,1);
  
  --transition: all 0.4s cubic-bezier(0.16, 1, 0.3, 1);

  font-family: 'Outfit', sans-serif;
  background-color: var(--bg-base);
  background-image: 
      radial-gradient(at 0% 0%, rgba(99, 102, 241, 0.12) 0px, transparent 50%),
      radial-gradient(at 100% 0%, rgba(236, 72, 153, 0.08) 0px, transparent 50%),
      radial-gradient(at 100% 100%, rgba(14, 165, 233, 0.12) 0px, transparent 50%);
  background-attachment: fixed;
  color: var(--text-body);
  line-height: 1.5;
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
  position: relative;
  overflow: hidden;
}

/* Background floating animated orbs */
@keyframes orbDrift1 {
  0%, 100% { transform: translate(0, 0) scale(1) rotate(0deg); }
  50% { transform: translate(40px, -40px) scale(1.1) rotate(20deg); }
}
@keyframes orbDrift2 {
  0%, 100% { transform: translate(0, 0) scale(1) rotate(0deg); }
  50% { transform: translate(-40px, 40px) scale(1.2) rotate(-20deg); }
}

.bg-orb {
  position: absolute;
  border-radius: 50%;
  filter: blur(80px);
  z-index: 0;
}
.orb-1 {
  top: 20%; left: 30%; width: 300px; height: 300px;
  background: rgba(99, 102, 241, 0.3);
  animation: orbDrift1 14s ease-in-out infinite;
}
.orb-2 {
  bottom: 20%; right: 30%; width: 350px; height: 350px;
  background: rgba(236, 72, 153, 0.2);
  animation: orbDrift2 18s ease-in-out infinite;
}

/* Auth Card */
.auth-card {
  width: 100%;
  max-width: 440px;
  background: var(--surface-card);
  backdrop-filter: blur(24px);
  -webkit-backdrop-filter: blur(24px);
  border-radius: var(--rad-lg);
  padding: 40px;
  box-shadow: var(--shadow-float);
  border: 1px solid rgba(255, 255, 255, 0.8);
  position: relative;
  z-index: 1;
}

/* Card Header */
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 32px;
}

.tabs-container {
  display: flex;
  background: rgba(15, 23, 42, 0.04);
  padding: 4px;
  border-radius: 99px;
  flex: 1;
  margin-right: 16px;
}

.tab {
  flex: 1;
  text-align: center;
  padding: 10px 16px;
  border-radius: 99px;
  font-size: 15px;
  font-weight: 600;
  color: var(--text-muted);
  cursor: pointer;
  transition: var(--transition);
}

.tab.active {
  background: #ffffff;
  color: var(--text-title);
  box-shadow: 0 2px 8px rgba(0,0,0,0.04);
}

.lang-btn-wrapper {
  display: flex;
  align-items: center;
}

/* Title */
h1 {
  font-size: 32px;
  font-weight: 700;
  color: var(--text-title);
  letter-spacing: -0.5px;
  margin-bottom: 28px;
  text-align: center;
}

/* Custom Alerts */
.custom-alert {
  padding: 12px 16px;
  border-radius: 12px;
  font-size: 14px;
  display: flex;
  align-items: center;
  gap: 10px;
}
.error-alert {
  background: rgba(239, 68, 68, 0.1);
  color: #dc2626;
  border: 1px solid rgba(239, 68, 68, 0.2);
}
.success-alert {
  background: rgba(16, 185, 129, 0.1);
  color: #059669;
  border: 1px solid rgba(16, 185, 129, 0.2);
}
.alert-icon {
  font-size: 20px;
}

/* Form Controls */
.form-group {
  margin-bottom: 20px;
}

.form-label {
  display: block;
  font-size: 13px;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin-bottom: 8px;
  padding-left: 4px;
}

.input-wrapper {
  position: relative;
  display: flex;
  align-items: center;
}

.input-icon {
  position: absolute;
  left: 16px;
  font-size: 20px;
  color: var(--text-muted);
  transition: var(--transition);
  pointer-events: none;
}

.form-input {
  width: 100%;
  padding: 16px 16px 16px 48px;
  border-radius: 16px;
  background: var(--surface-input);
  border: 1.5px solid rgba(255, 255, 255, 0.8);
  font-family: inherit;
  font-size: 16px;
  color: var(--text-title);
  font-weight: 500;
  transition: var(--transition);
  box-shadow: inset 0 2px 4px rgba(0,0,0,0.01);
}

.custom-select {
  appearance: none;
  cursor: pointer;
}

.form-input:focus {
  outline: none;
  border-color: var(--accent-main);
  background: #ffffff;
  box-shadow: 0 0 0 4px rgba(99, 102, 241, 0.1), inset 0 2px 4px rgba(0,0,0,0.01);
}

.form-input:focus ~ .input-icon,
.form-input:not(:placeholder-shown) ~ .input-icon {
  color: var(--accent-main);
}

.form-input::placeholder {
  color: #94a3b8;
  font-weight: 400;
}

.suggestions-dropdown {
  background: #ffffff;
  border: 1px solid var(--rad-sm);
  border-radius: 12px;
  box-shadow: 0 10px 25px rgba(0,0,0,0.08);
  margin-top: 4px;
  max-height: 180px;
  overflow-y: auto;
  position: absolute;
  width: 100%;
  z-index: 20;
}

.suggestion-item {
  padding: 10px 14px;
  font-size: 14px;
  cursor: pointer;
  transition: background 0.15s ease;
}

.suggestion-item:hover {
  background: #f1f5f9;
}

.forgot-link {
  display: block;
  text-align: right;
  font-size: 14px;
  font-weight: 600;
  color: var(--accent-main);
  text-decoration: none;
  margin-top: 12px;
  margin-bottom: 28px;
  transition: var(--transition);
}

.forgot-link:hover {
  color: #4f46e5;
  text-decoration: underline;
}

.submit-btn {
  width: 100%;
  background: linear-gradient(135deg, #6366f1, #4f46e5);
  color: white;
  border: none;
  padding: 18px;
  border-radius: 16px;
  font-size: 18px;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  cursor: pointer;
  box-shadow: 0 10px 24px -6px var(--accent-glow);
  transition: var(--transition);
}

.submit-btn:hover {
  transform: translateY(-2px);
  box-shadow: 0 15px 30px -6px rgba(99, 102, 241, 0.6);
}

.submit-btn:disabled {
  opacity: 0.7;
  cursor: not-allowed;
}

.spinner {
  width: 22px;
  height: 22px;
  border: 3px solid rgba(255,255,255,0.3);
  border-top-color: #ffffff;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

/* Forgot Password Modal Styles */
.forgot-modal-overlay {
  position: fixed;
  top: 0; left: 0; right: 0; bottom: 0;
  background: rgba(15, 23, 42, 0.4);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
  padding: 20px;
  animation: fadeIn 0.3s ease;
}

.forgot-modal-card {
  background: rgba(255, 255, 255, 0.9);
  backdrop-filter: blur(24px);
  -webkit-backdrop-filter: blur(24px);
  border: 1px solid rgba(255, 255, 255, 0.8);
  border-radius: var(--rad-lg);
  width: 100%;
  max-width: 420px;
  box-shadow: var(--shadow-float);
  padding: 32px;
  position: relative;
  animation: slideUp 0.4s cubic-bezier(0.16, 1, 0.3, 1);
}

.forgot-modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.forgot-modal-title {
  font-size: 22px;
  font-weight: 700;
  color: var(--text-title);
  letter-spacing: -0.5px;
}

.forgot-desc {
  font-size: 14px;
  color: var(--text-body);
  margin-bottom: 20px;
  line-height: 1.5;
}

.btn-close-forgot {
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

.btn-close-forgot:hover {
  background: #ffffff;
  color: #ef4444;
  box-shadow: 0 4px 12px rgba(0,0,0,0.05);
  transform: rotate(90deg);
}

/* Responsive */
@media (max-width: 480px) {
  .auth-card {
    padding: 32px 24px;
    border-radius: 28px;
  }
  h1 {
    font-size: 28px;
  }
}
</style>
