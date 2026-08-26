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
              @keydown="onPhoneKeydown"
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

        <!-- FIO Inputs for Registration -->
        <div v-if="mode === 'register'" class="form-group mb-3">
          <label class="form-label">Фамилия</label>
          <div class="input-wrapper">
            <input
              v-model="lastName"
              type="text"
              placeholder="Иванов"
              class="form-input"
              required
            />
            <i class="ph ph-user-card input-icon"></i>
          </div>
        </div>

        <div v-if="mode === 'register'" class="form-group mb-3">
          <label class="form-label">Имя</label>
          <div class="input-wrapper">
            <input
              v-model="firstName"
              type="text"
              placeholder="Иван"
              class="form-input"
              required
            />
            <i class="ph ph-user-card input-icon"></i>
          </div>
        </div>

        <div v-if="mode === 'register'" class="form-group mb-3">
          <label class="form-label">Отчество</label>
          <div class="input-wrapper">
            <input
              v-model="patronymic"
              type="text"
              placeholder="Иванович"
              class="form-input"
              required
            />
            <i class="ph ph-user-card input-icon"></i>
          </div>
        </div>

        <!-- Address, with the flat inside the same field -->
        <div v-if="mode === 'register'" class="form-group mb-3">
          <AddressAutocomplete
            v-model="pickedAddress"
            :label="role === 'CUSTOMER' ? $t('login.pickupAddress') : 'Базовый адрес (откуда искать заказы)'"
            :placeholder="$t('login.pickupAddressPlaceholder')"
            :hint="$t('login.addressHint')"
            :flat-placeholder="$t('login.flatNumberPlaceholder')"
            :needs-flat="role === 'CUSTOMER'"
          />
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
        <form v-else @submit.prevent="resetPasswordWithCode">
          <p class="forgot-desc">Код восстановления отправлен на <strong>{{ resetEmail }}</strong>.</p>
          <div v-if="resetSuccessMsg" class="custom-alert success-alert mb-3">
            <i class="ph-fill ph-check-circle me-1"></i>
            <span class="alert-text">{{ resetSuccessMsg }}</span>
          </div>
          <div class="form-group mb-3">
            <label class="form-label">6-значный код из Email</label>
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
          <button type="submit" class="submit-btn mb-2" :disabled="resetLoading">
            <span v-if="resetLoading" class="spinner"></span>
            <template v-else>Сохранить новый пароль <i class="ph-bold ph-check"></i></template>
          </button>
          <div class="resend-row text-center">
            <button
              type="button"
              class="btn-link-resend"
              :disabled="resendTimer > 0 || resetLoading"
              @click="requestResetCode"
            >
              <template v-if="resendTimer > 0">Отправить повторно через {{ resendTimer }}с</template>
              <template v-else>Отправить код повторно</template>
            </button>
          </div>
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
import AddressAutocomplete, { StructuredAddress } from '../../components/AddressAutocomplete.vue'
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
  components: { LanguageSwitcher, AddressAutocomplete },
  setup() {
    const router = useRouter()
    const route = useRoute()
    const authStore = useAuthStore()
    const { t } = useI18n()

    const submitBtnRef = ref<HTMLButtonElement | null>(null)
    const mode = ref<'login' | 'register'>('login')
    const displayPhone = ref('')
    const phone = ref('')

    const onPhoneKeydown = (e: KeyboardEvent) => {
      const target = e.target as HTMLInputElement
      if (e.key === 'Backspace') {
        const start = target.selectionStart ?? 0
        const end = target.selectionEnd ?? 0

        // If user pressed Backspace with cursor right after a formatting character (space, dash, paren), delete preceding digit
        if (start === end && start > 0) {
          const charBefore = target.value[start - 1]
          if (/\D/.test(charBefore)) {
            e.preventDefault()
            // find nearest digit index before cursor
            let newEnd = start - 1
            while (newEnd > 0 && /\D/.test(target.value[newEnd - 1])) {
              newEnd--
            }
            if (newEnd > 0) {
              const val = target.value.slice(0, newEnd - 1) + target.value.slice(start)
              const formatted = formatPhoneMask(val)
              displayPhone.value = formatted
              phone.value = cleanPhoneDigits(formatted)
              nextTick(() => {
                const pos = Math.max(0, newEnd - 1)
                target.setSelectionRange(pos, pos)
              })
            } else {
              displayPhone.value = ''
              phone.value = ''
            }
          }
        }
      }
    }

    const onPhoneInput = (e: Event) => {
      const target = e.target as HTMLInputElement
      const oldVal = displayPhone.value
      const selectionPos = target.selectionStart || 0
      const formatted = formatPhoneMask(target.value)
      displayPhone.value = formatted
      phone.value = cleanPhoneDigits(formatted)

      // Maintain cursor position if input was made in the middle
      if (target.value.length > selectionPos) {
        nextTick(() => {
          let pos = selectionPos
          // Adjust position if mask added characters
          if (formatted.length > oldVal.length && pos < formatted.length) {
            pos += (formatted.length - target.value.length)
          }
          target.setSelectionRange(pos, pos)
        })
      }
    }
    const email = ref('')
    const password = ref('')
    const lastName = ref('')
    const firstName = ref('')
    const patronymic = ref('')
    const role = ref<'CUSTOMER' | 'EXECUTOR'>('CUSTOMER')
    // The whole address, as the register spells it: parts, coordinates and the
    // flat together. Null until something is actually chosen from the list,
    // which is also what stops a half-typed street from being submitted.
    const pickedAddress = ref<StructuredAddress | null>(null)
    const error = ref('')
    const message = ref('')
    const loading = ref(false)

    const showForgotModal = ref(false)
    const resetStep = ref(1)
    const resetEmail = ref('')
    const resetCode = ref('')
    const newPassword = ref('')
    const resetError = ref('')
    const resetSuccessMsg = ref('')
    const resetLoading = ref(false)
    const resendTimer = ref(0)
    let resendInterval: any = null

    const startResendTimer = () => {
      resendTimer.value = 60
      if (resendInterval) clearInterval(resendInterval)
      resendInterval = setInterval(() => {
        if (resendTimer.value > 0) {
          resendTimer.value--
        } else {
          clearInterval(resendInterval)
        }
      }, 1000)
    }

    const openForgotModal = () => {
      showForgotModal.value = true
      resetStep.value = 1
      resetEmail.value = ''
      resetCode.value = ''
      newPassword.value = ''
      resetError.value = ''
      resetSuccessMsg.value = ''
      resendTimer.value = 0
    }

    const requestResetCode = async () => {
      resetError.value = ''
      resetSuccessMsg.value = ''
      if (!resetEmail.value) return
      resetLoading.value = true
      try {
        await api.post('/auth/forgot-password', { email: resetEmail.value })
        resetStep.value = 2
        resetSuccessMsg.value = `Код восстановления отправлен на ${resetEmail.value}. Проверьте почту!`
        startResendTimer()
      } catch (err: any) {
        resetError.value = formatApiError(err, 'Не удалось отправить код')
      } finally {
        resetLoading.value = false
      }
    }

    const resetPasswordWithCode = async () => {
      resetError.value = ''
      if (!resetEmail.value || !resetCode.value || !newPassword.value) return
      resetLoading.value = true
      try {
        await api.post('/auth/reset-password', {
          email: resetEmail.value,
          code: resetCode.value,
          new_password: newPassword.value,
        })
        message.value = 'Пароль успешно изменён! Войдите с новым паролем.'
        showForgotModal.value = false
      } catch (err: any) {
        resetError.value = formatApiError(err, 'Ошибка сброса пароля')
      } finally {
        resetLoading.value = false
      }
    }

    const focusSubmitButton = () => {
      nextTick(() => {
        submitBtnRef.value?.focus()
      })
    }

    onMounted(async () => {
      focusSubmitButton()

      const modeParam = route.query.mode as string
      if (modeParam === 'register' || modeParam === 'login') {
        mode.value = modeParam
      }

      const token = route.query.token as string
      if (token) {
        try {
          const res = await api.get('/auth/verify-email', { params: { token } })
          message.value = res.data?.message || 'Email успешно подтверждён!'
        } catch (err: any) {
          const errData = err.response?.data
          if (errData?.code === 'TOKEN_EXPIRED' || errData?.error?.includes('expired')) {
            error.value = 'Срок действия ссылки подтверждения истек (60 минут). Запросите подтверждение или смену почты заново в профиле.'
          } else if (typeof errData?.error === 'string') {
            error.value = errData.error
          } else {
            error.value = 'Ссылка подтверждения недействительна или уже была использована.'
          }
        }
      }
    })

    watch(mode, () => {
      error.value = ''
      message.value = ''
      role.value = 'CUSTOMER'
      lastName.value = ''
      firstName.value = ''
      patronymic.value = ''
      pickedAddress.value = null
      focusSubmitButton()
    })




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

          authStore.login(token, claims.role, claims.phone, claims.sub, response.data.refresh_token)

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
            last_name: lastName.value,
            first_name: firstName.value,
            patronymic: patronymic.value,
            role: role.value,
          }

          const chosen = pickedAddress.value
          if (!chosen) {
            error.value = t('login.addressNotChosen')
            return
          }
          // The parts go over the wire, not a line to be parsed back. That is
          // what lets an address like "12 к. 1" through, and it carries the
          // coordinates the dispatcher matches on.
          payload.address = chosen.value
          payload.region = chosen.region
          payload.city = chosen.city
          payload.street = chosen.street
          payload.house = chosen.house
          payload.flat = chosen.flat
          payload.fias_id = chosen.fias_id
          payload.source = chosen.source
          if (chosen.lat !== undefined && chosen.lon !== undefined) {
            payload.lat = chosen.lat
            payload.lon = chosen.lon
          }

          await api.post('/register', payload)
          message.value = 'Регистрация успешна! Код/ссылка подтверждения отправлена на ' + email.value
          mode.value = 'login'
          email.value = ''
          password.value = ''
          lastName.value = ''
          firstName.value = ''
          patronymic.value = ''
          role.value = 'CUSTOMER'
          pickedAddress.value = null
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
      onPhoneKeydown,
      email,
      password,
      lastName,
      firstName,
      patronymic,
      role,
      pickedAddress,
      error,
      message,
      loading,
      handleSubmit,
      showForgotModal,
      resetStep,
      resetEmail,
      resetCode,
      newPassword,
      resetError,
      resetSuccessMsg,
      resendTimer,
      resetLoading,
      openForgotModal,
      requestResetCode,
      resetPasswordWithCode,
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

h1 {
  font-size: 32px;
  font-weight: 700;
  color: var(--text-title);
  letter-spacing: -0.5px;
  margin-bottom: 28px;
  text-align: center;
}

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
  background: #ecfdf5;
  color: #059669;
  border: 1px solid #a7f3d0;
  border-radius: 8px;
  padding: 10px 14px;
  font-size: 13px;
  font-weight: 500;
  display: flex;
  align-items: center;
}
.alert-icon {
  font-size: 20px;
}

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
  background: #f1f5f9;
  color: #0f172a;
  box-shadow: 0 4px 12px rgba(0,0,0,0.05);
  transform: rotate(90deg);
}

.btn-link-resend {
  background: none;
  border: none;
  color: #6366f1;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  padding: 4px 8px;
}

.btn-link-resend:disabled {
  color: #94a3b8;
  cursor: not-allowed;
}

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
