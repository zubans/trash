<template>
  <div class="profile-page-wrapper">
    <div class="profile-container">
      <!-- Верхняя навигационная шапка -->
      <div class="top-nav">
        <button type="button" class="btn-back" @click="goBack">
          <i class="ph-bold ph-arrow-left"></i>
          Вернуться на главную
        </button>
        <div class="page-header-title">
          <i class="ph-fill ph-user-gear icon-title"></i>
          Профиль исполнителя
        </div>
      </div>

      <div class="profile-card">
        <!-- Баннер с телефоном и статусом -->
        <div class="user-phone-banner mb-4">
          <div class="banner-avatar">
            <i class="ph ph-user"></i>
          </div>
          <div class="banner-info">
            <div class="banner-phone-row">
              <span class="banner-phone">{{ phone || '7 999 745 46 56' }}</span>
              <span v-if="userAge > 0" class="age-badge ms-2">{{ userAge }} {{ getAgeWord(userAge) }}</span>
            </div>
            <div v-if="fullName" class="banner-fullname">{{ fullName }}</div>
          </div>
        </div>

        <!-- Раздел даты рождения -->
        <div class="section-header">
          <div class="section-title">
            <i class="ph-fill ph-cake" style="color: #ec4899;"></i>
            Дата рождения и возраст
          </div>
          <div class="section-subtitle">Определяет доступ к услугам с возрастным цензом</div>
        </div>

        <div class="email-box mb-4">
          <div class="input-wrapper">
            <input
              v-model="birthDateInput"
              type="date"
              class="form-input"
            />
            <button type="button" class="btn-save-email" :disabled="savingBirthDate || !birthDateInput || birthDateInput === currentBirthDate" @click="saveBirthDate">
              <span v-if="savingBirthDate" class="spinner-sm"></span>
              <template v-else>Сохранить</template>
            </button>
          </div>
          <div v-if="userAge > 0" class="mt-2 text-sm text-secondary">
            Ваш возраст: <strong>{{ userAge }} {{ getAgeWord(userAge) }}</strong>
          </div>
          <div v-if="birthDateMsg" class="email-msg-text" :class="{ error: birthDateMsgIsError }">
            {{ birthDateMsg }}
          </div>
        </div>

        <!-- Управление почтой -->
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

        <!-- Раздел смены пароля -->
        <div class="section-header">
          <div class="section-title">
            <i class="ph-fill ph-lock-key" style="color: #f59e0b;"></i>
            Безопасность и пароль
          </div>
          <div class="section-subtitle">Смена пароля для входа в кабинет</div>
        </div>

        <div class="password-box mb-4">
          <div class="form-group mb-3">
            <label class="form-label">Текущий пароль</label>
            <input v-model="oldPassword" type="password" class="form-input" placeholder="••••••••" />
          </div>
          <div class="form-group mb-3">
            <label class="form-label">Новый пароль</label>
            <input v-model="newPassword" type="password" class="form-input" placeholder="Не менее 6 символов" />
          </div>
          <div class="form-group mb-3">
            <label class="form-label">Подтверждение нового пароля</label>
            <input v-model="confirmPassword" type="password" class="form-input" placeholder="Повторите новый пароль" />
          </div>
          <button type="button" class="btn-save-email" :disabled="changingPassword || !oldPassword || !newPassword" @click="changePassword">
            <span v-if="changingPassword" class="spinner-sm"></span>
            <template v-else>Обновить пароль</template>
          </button>
          <div v-if="pwdMsg" class="email-msg-text" :class="{ error: pwdMsgIsError }">
            {{ pwdMsg }}
          </div>
        </div>

        <!-- Подвал с действиями -->
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
import api, { storeSession } from '../../services/api'
import { useAuthStore } from '../../stores/auth-store'

export default defineComponent({
  name: 'ExecutorProfilePage',
  setup() {
    const router = useRouter()
    const authStore = useAuthStore()

    const phone = ref('')
    const status = ref('Активный')
    const userEmail = ref('')
    const emailInput = ref('')

    const savingEmail = ref(false)
    const emailMsg = ref('')
    const emailMsgIsError = ref(false)

    const oldPassword = ref('')
    const newPassword = ref('')
    const confirmPassword = ref('')
    const changingPassword = ref(false)
    const pwdMsg = ref('')
    const pwdMsgIsError = ref(false)

    const fullName = ref('')
    const birthDateInput = ref('')
    const currentBirthDate = ref('')
    const userAge = ref(0)
    const savingBirthDate = ref(false)
    const birthDateMsg = ref('')
    const birthDateMsgIsError = ref(false)

    const getAgeWord = (age: number) => {
      const last = age % 10
      const lastTwo = age % 100
      if (lastTwo >= 11 && lastTwo <= 19) return 'лет'
      if (last === 1) return 'год'
      if (last >= 2 && last <= 4) return 'года'
      return 'лет'
    }

    const currentEmail = computed(() => userEmail.value)

    const fetchUserProfile = async () => {
      try {
        const meRes = await api.get('/auth/me').catch(() => null)
        if (meRes?.data) {
          const parts = [meRes.data.last_name, meRes.data.first_name, meRes.data.patronymic].filter((p: string) => p && p.trim())
          fullName.value = parts.join(' ')
          if (meRes.data.birth_date) {
            birthDateInput.value = meRes.data.birth_date
            currentBirthDate.value = meRes.data.birth_date
          }
          if (meRes.data.age !== undefined) {
            userAge.value = meRes.data.age
          }
        }
        const res = await api.get('/user/profile')
        phone.value = res.data.phone || authStore.phone || ''
        status.value = res.data.status || 'ACTIVE'
        userEmail.value = res.data.email || ''
        emailInput.value = userEmail.value
      } catch (err) {
        console.error('Failed to load executor profile:', err)
      }
    }

    const saveEmail = async () => {
      if (!emailInput.value || emailInput.value === userEmail.value || savingEmail.value) return
      savingEmail.value = true
      emailMsg.value = ''
      emailMsgIsError.value = false
      try {
        await api.post('/user/email', { email: emailInput.value })
        emailMsg.value = 'Ссылка для подтверждения отправлена на ' + emailInput.value + '. Почта изменится после перехода по ссылке.'
      } catch (err: any) {
        emailMsgIsError.value = true
        emailMsg.value = err.response?.data?.error || err.response?.data || 'Ошибка обновления Email'
      } finally {
        savingEmail.value = false
      }
    }

    const saveBirthDate = async () => {
      if (!birthDateInput.value || birthDateInput.value === currentBirthDate.value || savingBirthDate.value) return
      savingBirthDate.value = true
      birthDateMsg.value = ''
      birthDateMsgIsError.value = false
      try {
        const res = await api.post('/user/birth-date', { birth_date: birthDateInput.value })
        currentBirthDate.value = birthDateInput.value
        if (res.data?.age !== undefined) {
          userAge.value = res.data.age
        }
        birthDateMsg.value = 'Дата рождения успешно сохранена!'
      } catch (err: any) {
        birthDateMsgIsError.value = true
        birthDateMsg.value = err.response?.data?.error || err.response?.data || 'Ошибка сохранения даты рождения'
      } finally {
        savingBirthDate.value = false
      }
    }

    const changePassword = async () => {
      if (newPassword.value !== confirmPassword.value) {
        pwdMsgIsError.value = true
        pwdMsg.value = 'Пароли не совпадают'
        return
      }
      changingPassword.value = true
      pwdMsg.value = ''
      pwdMsgIsError.value = false
      try {
        const res = await api.post('/user/change-password', {
          old_password: oldPassword.value,
          new_password: newPassword.value
        })
        // Смена пароля завершает все сессии; ответ несёт свежую пару, чтобы это
        // устройство осталось в системе. Без её сохранения следующий запрос отдал бы
        // 401 и выбросил пользователя на экран входа.
        if (res.data?.token) {
          storeSession(res.data.token, res.data.refresh_token)
        }
        pwdMsg.value = 'Пароль изменён. На других устройствах потребуется войти заново.'
        oldPassword.value = ''
        newPassword.value = ''
        confirmPassword.value = ''
      } catch (err: any) {
        pwdMsgIsError.value = true
        pwdMsg.value = err.response?.data?.error || err.response?.data || 'Ошибка при смене пароля'
      } finally {
        changingPassword.value = false
      }
    }

    const goBack = () => {
      router.push('/executor')
    }

    onMounted(() => {
      fetchUserProfile()
    })

    return {
      phone,
      fullName,
      birthDateInput,
      currentBirthDate,
      userAge,
      savingBirthDate,
      birthDateMsg,
      birthDateMsgIsError,
      getAgeWord,
      saveBirthDate,
      status,
      userEmail,
      emailInput,
      savingEmail,
      emailMsg,
      emailMsgIsError,
      oldPassword,
      newPassword,
      confirmPassword,
      changingPassword,
      pwdMsg,
      pwdMsgIsError,
      currentEmail,
      saveEmail,
      changePassword,
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

/* Баннер с телефоном пользователя */
.user-phone-banner {
  display: flex;
  align-items: center;
  gap: 16px;
  background: #f1f5f9;
  border-radius: 16px;
  padding: 20px;
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
.banner-fullname {
  font-size: 13px;
  font-weight: 600;
  color: #64748b;
  margin-top: 2px;
}

.age-badge {
  background: #fef3c7;
  color: #d97706;
  font-size: 11px;
  font-weight: 700;
  padding: 2px 7px;
  border-radius: 6px;
}

/* Заголовок раздела */
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

/* Блок почты и пароля */
.email-box, .password-box {
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  border-radius: 16px;
  padding: 16px;
}

.form-group {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.form-label {
  font-size: 13px;
  font-weight: 600;
  color: #475569;
}

.input-wrapper {
  display: flex;
  gap: 12px;
}

.form-input {
  width: 100%;
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
  .input-wrapper { flex-direction: column; }
  .btn-save-email { width: 100%; justify-content: center; }
  .btn-back-home { width: 100%; justify-content: center; }
}
</style>
