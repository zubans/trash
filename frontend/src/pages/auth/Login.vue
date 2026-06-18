<template>
  <div class="login-wrapper">
    <div class="background-decorations">
      <div class="shape shape-1"></div>
      <div class="shape shape-2"></div>
    </div>
    
    <va-card class="auth-card">
      <!-- Tabs header -->
      <div class="tabs-header">
        <button 
          :class="['tab-btn', { active: mode === 'login' }]"
          @click="mode = 'login'"
        >
          Sign In
        </button>
        <button 
          :class="['tab-btn', { active: mode === 'register' }]"
          @click="mode = 'register'"
        >
          Register
        </button>
      </div>

      <div class="card-content p-4">
        <!-- Title -->
        <h2 class="auth-title text-center mb-4">
          {{ mode === 'login' ? 'Welcome Back' : 'Create Account' }}
        </h2>

        <!-- Alert messages -->
        <transition name="fade">
          <div v-if="error" class="custom-alert error-alert mb-4">
            <span class="material-icons alert-icon">error_outline</span>
            <span class="alert-text">{{ error }}</span>
          </div>
        </transition>

        <transition name="fade">
          <div v-if="message" class="custom-alert success-alert mb-4">
            <span class="material-icons alert-icon">check_circle_outline</span>
            <span class="alert-text">{{ message }}</span>
          </div>
        </transition>

        <!-- Forms -->
        <va-form @submit.prevent="handleSubmit">
          <div class="form-group mb-3">
            <label class="form-label">Phone Number</label>
            <div class="input-wrapper">
              <span class="material-icons input-icon">phone</span>
              <input 
                v-model="phone" 
                type="tel" 
                placeholder="79999999999" 
                class="custom-input" 
                required 
              />
            </div>
          </div>

          <div class="form-group mb-4">
            <label class="form-label">Password</label>
            <div class="input-wrapper">
              <span class="material-icons input-icon">lock</span>
              <input 
                v-model="password" 
                type="password" 
                placeholder="••••••••" 
                class="custom-input" 
                required 
              />
            </div>
          </div>

          <button type="submit" class="submit-btn" :disabled="loading">
            <span v-if="loading" class="spinner"></span>
            <span v-else>{{ mode === 'login' ? 'Sign In' : 'Sign Up' }}</span>
          </button>
        </va-form>
      </div>
    </va-card>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../../stores/auth-store'
import api from '../../services/api'

// Helper to decode JWT token
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
  setup() {
    const router = useRouter()
    const authStore = useAuthStore()
    
    const mode = ref<'login' | 'register'>('login')
    const phone = ref('')
    const password = ref('')
    const error = ref('')
    const message = ref('')
    const loading = ref(false)

    watch(mode, () => {
      error.value = ''
      message.value = ''
    })

    const handleSubmit = async () => {
      error.value = ''
      message.value = ''
      loading.value = true

      try {
        if (mode.value === 'login') {
          // Authentication
          const response = await api.post('/login', {
            phone: phone.value,
            password: password.value,
          })
          const token = response.data.token
          const claims = parseJwt(token)

          if (!claims) {
            error.value = 'Failed to parse user session. Try again.'
            return
          }

          authStore.login(token, claims.role, claims.phone)

          // Role-based redirection
          if (claims.role === 'ADMIN') {
            router.push('/admin')
          } else if (claims.role === 'CUSTOMER') {
            router.push('/customer')
          } else if (claims.role === 'EXECUTOR') {
            router.push('/executor')
          } else {
            error.value = 'Role not supported.'
          }
        } else {
          // Registration
          await api.post('/register', {
            phone: phone.value,
            password: password.value,
          })
          message.value = 'Registration successful! You can now log in.'
          mode.value = 'login'
          password.value = ''
        }
      } catch (err: any) {
        if (err.response && err.response.data) {
          error.value = typeof err.response.data === 'string' ? err.response.data : 'Request failed.'
        } else {
          error.value = 'Network error. Please try again.'
        }
      } finally {
        loading.value = false
      }
    }

    return {
      mode,
      phone,
      password,
      error,
      message,
      loading,
      handleSubmit,
    }
  },
})
</script>

<style scoped>
.login-wrapper {
  position: relative;
  display: flex;
  justify-content: center;
  align-items: center;
  min-height: 100vh;
  background: radial-gradient(circle at 10% 20%, rgb(17, 26, 41) 0%, rgb(8, 11, 20) 100%);
  overflow: hidden;
}

/* Background animated shapes */
.background-decorations {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  z-index: 1;
}

.shape {
  position: absolute;
  border-radius: 50%;
  filter: blur(80px);
}

.shape-1 {
  width: 300px;
  height: 300px;
  background: rgba(49, 130, 206, 0.15);
  top: 15%;
  left: 10%;
  animation: move-1 20s infinite alternate;
}

.shape-2 {
  width: 400px;
  height: 400px;
  background: rgba(107, 70, 193, 0.12);
  bottom: 10%;
  right: 15%;
  animation: move-2 25s infinite alternate;
}

@keyframes move-1 {
  from { transform: translate(0, 0); }
  to { transform: translate(50px, 80px); }
}

@keyframes move-2 {
  from { transform: translate(0, 0); }
  to { transform: translate(-80px, -40px); }
}

/* Glassmorphic login card */
.auth-card {
  position: relative;
  width: 100%;
  max-width: 420px;
  background: rgba(255, 255, 255, 0.08) !important;
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 16px !important;
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.5);
  z-index: 5;
  overflow: hidden;
}

.tabs-header {
  display: flex;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
}

.tab-btn {
  flex: 1;
  background: none;
  border: none;
  padding: 15px 0;
  color: rgba(255, 255, 255, 0.6);
  font-size: 1rem;
  font-weight: bold;
  cursor: pointer;
  transition: all 0.3s;
}

.tab-btn:hover {
  color: #fff;
  background: rgba(255, 255, 255, 0.02);
}

.tab-btn.active {
  color: #3182ce;
  border-bottom: 2px solid #3182ce;
  background: rgba(49, 130, 206, 0.05);
}

.card-content {
  padding: 32px 36px;
}

.mb-3 {
  margin-bottom: 20px;
}

.mb-4 {
  margin-bottom: 28px;
}

.auth-title {
  color: #ffffff;
  font-size: 1.8rem;
  font-weight: 700;
  letter-spacing: -0.5px;
}

.form-label {
  display: block;
  font-size: 0.85rem;
  color: rgba(255, 255, 255, 0.7);
  margin-bottom: 8px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

/* Premium input controls */
.input-wrapper {
  position: relative;
  display: flex;
  align-items: center;
}

.input-icon {
  position: absolute;
  left: 12px;
  color: rgba(255, 255, 255, 0.4);
  font-size: 20px;
}

.custom-input {
  width: 100%;
  padding: 12px 12px 12px 42px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.15);
  border-radius: 8px;
  color: #fff;
  font-size: 1rem;
  transition: all 0.3s;
  outline: none;
}

.custom-input:focus {
  background: rgba(255, 255, 255, 0.08);
  border-color: #3182ce;
  box-shadow: 0 0 0 3px rgba(49, 130, 206, 0.25);
}

.custom-input::placeholder {
  color: rgba(255, 255, 255, 0.35);
}

/* Premium gradient button */
.submit-btn {
  display: flex;
  justify-content: center;
  align-items: center;
  width: 100%;
  padding: 14px;
  background: linear-gradient(135deg, #3182ce 0%, #553c9a 100%);
  border: none;
  border-radius: 8px;
  color: #fff;
  font-size: 1rem;
  font-weight: bold;
  cursor: pointer;
  transition: all 0.3s ease;
  box-shadow: 0 4px 15px rgba(49, 130, 206, 0.3);
}

.submit-btn:hover:not(:disabled) {
  transform: translateY(-2px);
  box-shadow: 0 6px 20px rgba(49, 130, 206, 0.4);
}

.submit-btn:active:not(:disabled) {
  transform: translateY(1px);
}

.submit-btn:disabled {
  opacity: 0.7;
  cursor: not-allowed;
}

/* Custom Alert Boxes */
.custom-alert {
  display: flex;
  align-items: center;
  padding: 12px 16px;
  border-radius: 8px;
  font-size: 0.9rem;
}

.error-alert {
  background: rgba(229, 62, 62, 0.15);
  border: 1px solid rgba(229, 62, 62, 0.3);
  color: #feb2b2;
}

.success-alert {
  background: rgba(56, 161, 105, 0.15);
  border: 1px solid rgba(56, 161, 105, 0.3);
  color: #c6f6d5;
}

.alert-icon {
  margin-right: 10px;
  font-size: 20px;
}

.alert-text {
  flex: 1;
}

/* Transition Animations */
.fade-enter-active, .fade-leave-active {
  transition: opacity 0.3s ease;
}
.fade-enter-from, .fade-leave-to {
  opacity: 0;
}

/* Spinner */
.spinner {
  width: 20px;
  height: 20px;
  border: 3px solid rgba(255, 255, 255, 0.3);
  border-radius: 50%;
  border-top-color: #fff;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}
</style>
