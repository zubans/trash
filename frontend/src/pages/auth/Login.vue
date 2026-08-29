<template>
  <div class="split-layout">
    <!-- MOBILE HEADER (visible on screen width <= 900px) -->
    <header class="app-header">
      <AppLogo />
      <div class="mobile-lang-switch">
        <LanguageSwitcher />
      </div>
    </header>

    <!-- LEFT SIDE: Branding Presentation (visible on screen width > 900px) -->
    <div class="layout-left">
      <!-- Scaled Logo Wrapper -->
      <div class="logo-wrapper">
        <AppLogo />
      </div>

      <div class="brand-content">
        <h1>
          <template v-if="mode === 'register'">
            Начни работать<br />и зарабатывать
          </template>
          <template v-else>
            Твои помощники<br />тут
          </template>
        </h1>
        <p>
          <template v-if="mode === 'register'">
            Присоединяйтесь к платформе. Находите заказы поблизости, управляйте своим временем и получайте стабильный доход без комиссий посредников.
          </template>
          <template v-else>
            Управляйте своими заказами, отслеживайте статус исполнения и находите надежных исполнителей в вашем городе.
          </template>
        </p>
      </div>

      <div><!-- Spacer for vertical alignment --></div>
    </div>

    <!-- RIGHT SIDE: Form Container -->
    <div class="layout-right">
      <div class="form-container" :class="{ 'is-register': mode === 'register' }">
        <!-- Form Header (Tabs & Lang Switcher for Desktop) -->
        <div class="form-header">
          <div class="auth-tabs">
            <button
              type="button"
              :class="['tab-btn', { active: mode === 'login' }]"
              @click="mode = 'login'"
            >
              {{ $t('login.signIn') }}
            </button>
            <button
              type="button"
              :class="['tab-btn', { active: mode === 'register' }]"
              @click="mode = 'register'"
            >
              {{ $t('login.register') }}
            </button>
          </div>

          <div class="desktop-lang-switch">
            <LanguageSwitcher />
          </div>
        </div>

        <!-- Form Title -->
        <h2 class="form-title">
          {{ mode === 'login' ? $t('login.welcomeBack') : $t('login.createAccount') }}
        </h2>

        <!-- Alerts -->
        <transition name="fade">
          <div v-if="error" class="custom-alert error-alert mb-4">
            <i class="ph-bold ph-warning-circle alert-icon"></i>
            <span class="alert-text" style="white-space: pre-wrap;">{{ error }}</span>
          </div>
        </transition>

        <transition name="fade">
          <div v-if="message" class="custom-alert success-alert mb-4">
            <i class="ph-bold ph-check-circle alert-icon"></i>
            <span class="alert-text">{{ message }}</span>
          </div>
        </transition>

        <!-- Form -->
        <form @submit.prevent="handleSubmit">
          <!-- LOGIN MODE FIELDS -->
          <div v-if="mode === 'login'" class="form-fields">
            <div class="input-group">
              <label class="input-label">{{ $t('login.phone') }}</label>
              <div class="input-wrapper">
                <i class="ph-bold ph-phone input-icon"></i>
                <input
                  v-model="displayPhone"
                  type="tel"
                  placeholder="+7 (999) 999-99-99"
                  class="form-input"
                  required
                  @input="onPhoneInput"
                  @keydown="onPhoneKeydown"
                />
              </div>
            </div>

            <div class="input-group">
              <label class="input-label">{{ $t('login.password') }}</label>
              <div class="input-wrapper">
                <i class="ph-bold ph-lock-key input-icon"></i>
                <input
                  v-model="password"
                  type="password"
                  placeholder="••••••••"
                  class="form-input"
                  required
                />
              </div>
            </div>

            <a href="#" class="forgot-link" @click.prevent="openForgotModal">
              Забыли пароль?
            </a>

            <!-- Desktop inline submit button -->
            <div class="desktop-submit-wrap">
              <button ref="submitBtnRef" type="submit" class="btn-submit" :disabled="loading">
                <span v-if="loading" class="spinner"></span>
                <template v-else>
                  {{ $t('login.signInBtn') }} <i class="ph-bold ph-arrow-right"></i>
                </template>
              </button>
            </div>
          </div>

          <!-- REGISTRATION MODE FIELDS -->
          <div v-else class="form-grid">
            <div class="input-group">
              <label class="input-label">{{ $t('login.phone') }}</label>
              <div class="input-wrapper">
                <i class="ph-bold ph-phone input-icon"></i>
                <input
                  v-model="displayPhone"
                  type="tel"
                  placeholder="+7 (999) 999-99-99"
                  class="form-input"
                  required
                  @input="onPhoneInput"
                  @keydown="onPhoneKeydown"
                />
              </div>
            </div>

            <div class="input-group">
              <label class="input-label">{{ $t('login.password') }}</label>
              <div class="input-wrapper">
                <i class="ph-bold ph-lock-key input-icon"></i>
                <input
                  v-model="password"
                  type="password"
                  placeholder="••••••••"
                  class="form-input"
                  required
                />
              </div>
            </div>

            <div class="divider col-span-2 mobile-only"></div>

            <div class="input-group col-span-2">
              <label class="input-label">Кем вы хотите быть?</label>
              <div class="role-selector">
                <label class="role-card">
                  <input type="radio" name="role" value="CUSTOMER" v-model="role" />
                  <div class="role-option">
                    <i class="ph-bold ph-briefcase"></i>
                    <span>Заказчик</span>
                  </div>
                </label>
                <label class="role-card">
                  <input type="radio" name="role" value="EXECUTOR" v-model="role" />
                  <div class="role-option">
                    <i class="ph-bold ph-wrench"></i>
                    <span>Исполнитель</span>
                  </div>
                </label>
              </div>
            </div>

            <div class="divider col-span-2 mobile-only"></div>

            <div class="input-group">
              <label class="input-label">Фамилия</label>
              <input
                v-model="lastName"
                type="text"
                class="form-input no-icon"
                placeholder="Иванов"
                required
              />
            </div>

            <div class="input-group">
              <label class="input-label">Имя</label>
              <input
                v-model="firstName"
                type="text"
                class="form-input no-icon"
                placeholder="Иван"
                required
              />
            </div>

            <div class="input-group col-span-2">
              <label class="input-label">Отчество</label>
              <input
                v-model="patronymic"
                type="text"
                class="form-input no-icon"
                placeholder="Иванович"
                required
              />
            </div>

            <div class="divider col-span-2 mobile-only"></div>

            <div class="input-group col-span-2">
              <AddressAutocomplete
                v-model="pickedAddress"
                :label="role === 'CUSTOMER' ? $t('login.pickupAddress') : 'Базовый адрес (откуда искать заказы)'"
                :placeholder="$t('login.pickupAddressPlaceholder')"
                :hint="$t('login.addressHint')"
                :flat-placeholder="$t('login.flatNumberPlaceholder')"
                :needs-flat="false"
              />
            </div>

            <div class="input-group col-span-2">
              <label class="input-label">Email</label>
              <div class="input-wrapper">
                <i class="ph-bold ph-envelope-simple input-icon"></i>
                <input
                  v-model="email"
                  type="email"
                  class="form-input"
                  placeholder="example@domain.com"
                  required
                />
              </div>
            </div>

            <!-- Desktop inline submit button -->
            <div class="desktop-submit-wrap col-span-2">
              <button ref="submitBtnRef" type="submit" class="btn-submit" :disabled="loading">
                <span v-if="loading" class="spinner"></span>
                <template v-else>
                  {{ $t('login.signUpBtn') }} <i class="ph-bold ph-arrow-right"></i>
                </template>
              </button>
            </div>
          </div>

          <!-- Floating Bottom Action Bar for Mobile -->
          <div class="bottom-action-bar">
            <button type="submit" class="btn-submit" :disabled="loading">
              <span v-if="loading" class="spinner"></span>
              <template v-else>
                {{ mode === 'login' ? $t('login.signInBtn') : $t('login.signUpBtn') }}
                <i class="ph-bold ph-arrow-right"></i>
              </template>
            </button>
          </div>
        </form>
      </div>
    </div>

    <!-- Forgot / Reset Password Modal -->
    <div v-if="showForgotModal" class="forgot-modal-overlay" @click.self="showForgotModal = false">
      <div class="forgot-modal-card">
        <div class="forgot-modal-header">
          <div class="forgot-modal-title">Сброс пароля</div>
          <button type="button" class="btn-close-forgot" @click="showForgotModal = false">
            <i class="ph-bold ph-x"></i>
          </button>
        </div>

        <!-- Step 1: Request Code -->
        <form v-if="resetStep === 1" @submit.prevent="requestResetCode">
          <p class="forgot-desc">Введите ваш Email, чтобы получить код восстановления.</p>
          <div class="input-group mb-3">
            <label class="input-label">Email</label>
            <div class="input-wrapper">
              <i class="ph-bold ph-envelope-simple input-icon"></i>
              <input v-model="resetEmail" type="email" class="form-input" placeholder="example@domain.com" required />
            </div>
          </div>
          <div v-if="resetError" class="custom-alert error-alert mb-3">
            <span class="alert-text">{{ resetError }}</span>
          </div>
          <button type="submit" class="btn-submit" :disabled="resetLoading">
            <span v-if="resetLoading" class="spinner"></span>
            <template v-else>Получить код <i class="ph-bold ph-paper-plane-tilt"></i></template>
          </button>
        </form>

        <!-- Step 2: Enter Code & New Password -->
        <form v-else @submit.prevent="resetPasswordWithCode">
          <p class="forgot-desc">Код восстановления отправлен на <strong>{{ resetEmail }}</strong>.</p>
          <div v-if="resetSuccessMsg" class="custom-alert success-alert mb-3">
            <i class="ph-bold ph-check-circle me-1"></i>
            <span class="alert-text">{{ resetSuccessMsg }}</span>
          </div>
          <div class="input-group mb-3">
            <label class="input-label">8-значный код из Email</label>
            <div class="input-wrapper">
              <i class="ph-bold ph-key input-icon"></i>
              <input v-model="resetCode" type="text" class="form-input" placeholder="12345678" maxlength="8" required />
            </div>
          </div>
          <div class="input-group mb-3">
            <label class="input-label">Новый пароль</label>
            <div class="input-wrapper">
              <i class="ph-bold ph-lock-key input-icon"></i>
              <input v-model="newPassword" type="password" class="form-input" placeholder="••••••••" required />
            </div>
          </div>
          <div v-if="resetError" class="custom-alert error-alert mb-3">
            <span class="alert-text">{{ resetError }}</span>
          </div>
          <button type="submit" class="btn-submit mb-2" :disabled="resetLoading">
            <span v-if="resetLoading" class="spinner"></span>
            <template v-else>Сохранить новый пароль <i class="ph-bold ph-check"></i></template>
          </button>
          <div class="resend-row text-center mt-2">
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
import AppLogo from '../../components/AppLogo.vue'
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
  components: { LanguageSwitcher, AddressAutocomplete, AppLogo },
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

        if (start === end && start > 0) {
          const charBefore = target.value[start - 1]
          if (/\D/.test(charBefore)) {
            e.preventDefault()
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

      if (target.value.length > selectionPos) {
        nextTick(() => {
          let pos = selectionPos
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
      nextTick(() => {
        window.scrollTo({ top: 0, behavior: 'instant' })
        if (document.documentElement) document.documentElement.scrollTop = 0
        if (document.body) document.body.scrollTop = 0
      })
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
@import url('https://fonts.googleapis.com/css2?family=Nunito:wght@400;500;600;700;800;900&display=swap');

.split-layout {
  --brand-primary: #5c60f5; 
  --brand-light: #eef2ff;
  --success-main: #10b981; 
  
  --text-main: #0f172a; 
  --text-muted: #64748b;
  
  --bg-left: #f8fafc;
  --bg-right: #ffffff;
  
  --rad-sm: 8px;
  --rad-md: 12px;
  --rad-lg: 24px;
  --shadow-sm: 0 2px 8px rgba(0, 0, 0, 0.02);
  --shadow-card: 0 12px 40px rgba(15, 23, 42, 0.06);
  --shadow-bottom: 0 -4px 24px rgba(15, 23, 42, 0.06);
  --transition: all 0.2s ease;

  font-family: 'Nunito', sans-serif;
  color: var(--text-main);
  min-height: 100vh;
  width: 100%;
  display: flex;
  background: var(--bg-right);
  position: relative;
}

* {
  box-sizing: border-box;
}

/* --- App Header (Mobile) --- */
.app-header {
  display: none;
}

/* --- Layout Left (Presentation) --- */
.layout-left {
  flex: 1;
  background: linear-gradient(135deg, #f8fafc 0%, #eef2ff 100%);
  padding: 60px;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  position: relative;
  overflow: hidden;
  border-right: 1px solid rgba(0, 0, 0, 0.03);
}

.layout-left::before {
  content: '';
  position: absolute;
  top: -10%;
  left: -10%;
  width: 600px;
  height: 600px;
  background: radial-gradient(circle, rgba(92, 96, 245, 0.08) 0%, transparent 70%);
  border-radius: 50%;
  pointer-events: none;
}

.layout-left::after {
  content: '';
  position: absolute;
  bottom: -10%;
  right: -10%;
  width: 500px;
  height: 500px;
  background: radial-gradient(circle, rgba(16, 185, 129, 0.06) 0%, transparent 70%);
  border-radius: 50%;
  pointer-events: none;
}

.logo-wrapper {
  z-index: 1;
  transform: scale(1.6);
  transform-origin: left top;
  margin-bottom: 60px;
  height: 40px;
}

.brand-content {
  z-index: 1;
  max-width: 520px;
  margin-bottom: 10vh;
}

.brand-content h1 {
  font-size: 48px;
  font-weight: 800;
  line-height: 1.15;
  margin-bottom: 20px;
  color: var(--text-main);
  letter-spacing: -1px;
}

.brand-content p {
  font-size: 18px;
  color: var(--text-muted);
  line-height: 1.6;
}

/* --- Layout Right (Form) --- */
.layout-right {
  flex: 1;
  max-width: 720px;
  background: var(--bg-right);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 40px;
}

.form-container {
  width: 100%;
  max-width: 440px;
  transition: max-width 0.3s ease;
}

.form-container.is-register {
  max-width: 540px;
}

/* Form Header (Tabs & Lang) */
.form-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 40px;
}

.auth-tabs {
  display: flex;
  background: #f1f5f9;
  padding: 4px;
  border-radius: 99px;
}

.tab-btn {
  padding: 8px 24px;
  border: none;
  background: transparent;
  border-radius: 99px;
  font-family: inherit;
  font-size: 14px;
  font-weight: 700;
  color: var(--text-muted);
  cursor: pointer;
  transition: var(--transition);
}

.tab-btn:hover {
  color: var(--text-main);
}

.tab-btn.active {
  background: #ffffff;
  color: var(--text-main);
  box-shadow: var(--shadow-sm);
}

.desktop-lang-switch {
  display: flex;
}

.form-title {
  font-size: 32px;
  font-weight: 800;
  color: var(--text-main);
  margin-bottom: 32px;
  letter-spacing: -0.5px;
}

/* --- Alerts --- */
.custom-alert {
  padding: 12px 16px;
  border-radius: var(--rad-md);
  font-size: 14px;
  font-weight: 600;
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 24px;
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
}

.alert-icon {
  font-size: 20px;
  flex-shrink: 0;
}

/* --- Forms & Inputs --- */
.form-fields {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 20px;
}

.col-span-2 {
  grid-column: span 2;
}

.input-group {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.input-label {
  font-size: 11px;
  font-weight: 800;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.input-wrapper {
  position: relative;
  display: flex;
  align-items: center;
}

.input-icon {
  position: absolute;
  left: 16px;
  color: var(--text-muted);
  font-size: 18px;
  pointer-events: none;
  transition: var(--transition);
}

.form-input {
  width: 100%;
  padding: 14px 16px 14px 44px;
  background: #f8fafc;
  border: 1px solid transparent;
  border-radius: var(--rad-md);
  font-family: inherit;
  font-size: 15px;
  font-weight: 500;
  color: var(--text-main);
  transition: var(--transition);
  outline: none;
}

.form-input:focus {
  background: #ffffff;
  border-color: var(--brand-primary);
  box-shadow: 0 0 0 4px rgba(92, 96, 245, 0.1);
}

.form-input:focus ~ .input-icon {
  color: var(--brand-primary);
}

.form-input::placeholder {
  color: #94a3b8;
}

.no-icon {
  padding-left: 16px;
}

/* Address Autocomplete Deep Styling */
:deep(.address-label) {
  font-size: 11px;
  font-weight: 800;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin-bottom: 6px;
}

:deep(.address-input) {
  width: 100%;
  padding: 14px 16px 14px 44px;
  background: #f8fafc;
  border: 1px solid transparent;
  border-radius: var(--rad-md);
  font-family: inherit;
  font-size: 15px;
  font-weight: 500;
  color: var(--text-main);
  transition: var(--transition);
  outline: none;
}

:deep(.address-input:focus) {
  background: #ffffff;
  border-color: var(--brand-primary);
  box-shadow: 0 0 0 4px rgba(92, 96, 245, 0.1);
}

:deep(.address-icon) {
  position: absolute;
  left: 16px;
  color: var(--text-muted);
  font-size: 18px;
  pointer-events: none;
}

:deep(.address-hint) {
  font-size: 11px;
  color: var(--text-muted);
  margin-top: 4px;
}

/* Role Selector Radio Cards */
.role-selector {
  display: flex;
  gap: 12px;
}

.role-card {
  flex: 1;
  cursor: pointer;
}

.role-card input[type='radio'] {
  display: none;
}

.role-option {
  padding: 14px;
  border-radius: var(--rad-md);
  border: 2px solid #f1f5f9;
  background: #ffffff;
  transition: var(--transition);
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  font-weight: 700;
  font-size: 15px;
  color: var(--text-muted);
}

.role-option i {
  font-size: 20px;
}

.role-card:hover .role-option {
  border-color: #cbd5e1;
  color: var(--text-main);
}

.role-card input[type='radio']:checked + .role-option {
  border-color: var(--brand-primary);
  background: var(--brand-light);
  color: var(--brand-primary);
}

/* Forgot Password Link */
.forgot-link {
  font-size: 13px;
  font-weight: 700;
  color: var(--brand-primary);
  text-decoration: none;
  align-self: flex-end;
  transition: var(--transition);
  margin-top: -4px;
}

.forgot-link:hover {
  color: #4338ca;
  text-decoration: underline;
}

/* Submit Button */
.btn-submit {
  width: 100%;
  padding: 16px;
  background: var(--brand-primary);
  color: white;
  border: none;
  border-radius: var(--rad-md);
  font-family: inherit;
  font-size: 16px;
  font-weight: 800;
  cursor: pointer;
  transition: var(--transition);
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  box-shadow: 0 4px 12px rgba(92, 96, 245, 0.2);
}

.btn-submit:hover {
  transform: translateY(-2px);
  box-shadow: 0 8px 20px rgba(92, 96, 245, 0.3);
}

.btn-submit:disabled {
  opacity: 0.7;
  cursor: not-allowed;
  transform: none;
}

.desktop-submit-wrap {
  margin-top: 12px;
}

.bottom-action-bar {
  display: none;
}

.mobile-only {
  display: none;
}

.divider {
  height: 1px;
  background: rgba(0, 0, 0, 0.04);
  margin: 4px 0;
}

.spinner {
  width: 20px;
  height: 20px;
  border: 3px solid rgba(255, 255, 255, 0.3);
  border-top-color: #ffffff;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

/* --- Forgot Password Modal --- */
.forgot-modal-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
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
  background: #ffffff;
  border-radius: var(--rad-lg);
  width: 100%;
  max-width: 440px;
  box-shadow: var(--shadow-card);
  padding: 32px;
  position: relative;
  animation: slideUp 0.3s cubic-bezier(0.16, 1, 0.3, 1);
}

.forgot-modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.forgot-modal-title {
  font-size: 22px;
  font-weight: 800;
  color: var(--text-main);
  letter-spacing: -0.5px;
}

.forgot-desc {
  font-size: 14px;
  color: var(--text-muted);
  margin-bottom: 20px;
  line-height: 1.5;
}

.btn-close-forgot {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  border: none;
  background: #f1f5f9;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  color: var(--text-muted);
  cursor: pointer;
  transition: var(--transition);
}

.btn-close-forgot:hover {
  background: #e2e8f0;
  color: var(--text-main);
}

.btn-link-resend {
  background: none;
  border: none;
  color: var(--brand-primary);
  font-size: 13px;
  font-weight: 700;
  cursor: pointer;
  padding: 4px 8px;
}

.btn-link-resend:disabled {
  color: var(--text-muted);
  cursor: not-allowed;
}

@keyframes fadeIn {
  from { opacity: 0; }
  to { opacity: 1; }
}

@keyframes slideUp {
  from { opacity: 0; transform: translateY(16px); }
  to { opacity: 1; transform: translateY(0); }
}

/* --- MOBILE RESPONSIVE MEDIA QUERIES --- */
@media (max-width: 900px) {
  .split-layout {
    flex-direction: column;
    min-height: 100vh;
  }

  .layout-left {
    display: none;
  }

  .app-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 16px 20px;
    background: #ffffff;
    position: sticky;
    top: 0;
    z-index: 100;
    border-bottom: 1px solid rgba(0, 0, 0, 0.04);
  }

  .mobile-lang-switch {
    display: flex;
  }

  .desktop-lang-switch {
    display: none;
  }

  .layout-right {
    max-width: 100%;
    padding: 24px 20px 120px 20px; /* Padding at bottom for sticky action bar */
    align-items: flex-start;
  }

  .form-container,
  .form-container.is-register {
    max-width: 100%;
  }

  .form-header {
    margin-bottom: 24px;
  }

  .auth-tabs {
    width: 100%;
  }

  .tab-btn {
    flex: 1;
    text-align: center;
    padding: 10px;
  }

  .form-title {
    font-size: 28px;
    margin-bottom: 24px;
  }

  .form-grid {
    grid-template-columns: 1fr;
    gap: 16px;
  }

  .col-span-2 {
    grid-column: span 1;
  }

  .mobile-only {
    display: block;
  }

  .desktop-submit-wrap {
    display: none;
  }

  .bottom-action-bar {
    position: fixed;
    bottom: 0;
    left: 0;
    right: 0;
    background: rgba(255, 255, 255, 0.95);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    padding: 16px 20px 32px 20px;
    border-top: 1px solid rgba(0, 0, 0, 0.04);
    box-shadow: var(--shadow-bottom);
    z-index: 100;
    display: flex;
    justify-content: center;
  }

  .bottom-action-bar .btn-submit {
    max-width: 480px;
    padding: 18px;
  }
}
</style>
