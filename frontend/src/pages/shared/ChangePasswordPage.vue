<template>
  <div class="pwd-page-wrapper">
    <div class="pwd-container">
      <div class="top-nav">
        <button type="button" class="btn-back" @click="goBack">
          <i class="ph-bold ph-arrow-left"></i>
          Назад
        </button>
      </div>

      <div class="pwd-card">
        <div class="section-header">
          <div class="section-title">
            <i class="ph-fill ph-lock-key" style="color: #f59e0b;"></i>
            Смена пароля
          </div>
          <div class="section-subtitle">После смены на других устройствах потребуется войти заново</div>
        </div>

        <div class="form-group">
          <label class="form-label">Текущий пароль</label>
          <input v-model="oldPassword" type="password" class="form-input" placeholder="••••••••" autocomplete="current-password" />
        </div>
        <div class="form-group">
          <label class="form-label">Новый пароль</label>
          <input v-model="newPassword" type="password" class="form-input" placeholder="Не менее 6 символов" autocomplete="new-password" />
        </div>
        <div class="form-group">
          <label class="form-label">Подтверждение нового пароля</label>
          <input v-model="confirmPassword" type="password" class="form-input" placeholder="Повторите новый пароль" autocomplete="new-password" />
        </div>

        <button type="button" class="btn-primary" :disabled="changing || !oldPassword || !newPassword" @click="changePassword">
          <span v-if="changing" class="spinner-sm"></span>
          <template v-else>Обновить пароль</template>
        </button>

        <div v-if="message" class="pwd-msg" :class="{ error: messageIsError }">{{ message }}</div>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref } from 'vue'
import { useRouter } from 'vue-router'
import api, { storeSession } from '../../services/api'

// Смена пароля — отдельная страница, а не раздел профиля: профиль про данные
// человека, а пароль — про вход, и путь к нему один для всех ролей. Ссылка живёт
// в меню приложения.
export default defineComponent({
  name: 'ChangePasswordPage',
  setup() {
    const router = useRouter()
    const oldPassword = ref('')
    const newPassword = ref('')
    const confirmPassword = ref('')
    const changing = ref(false)
    const message = ref('')
    const messageIsError = ref(false)

    const goBack = () => router.back()

    const changePassword = async () => {
      if (newPassword.value !== confirmPassword.value) {
        messageIsError.value = true
        message.value = 'Пароли не совпадают'
        return
      }
      changing.value = true
      message.value = ''
      messageIsError.value = false
      try {
        const res = await api.post('/user/change-password', {
          old_password: oldPassword.value,
          new_password: newPassword.value,
        })
        // Смена пароля завершает все сессии; ответ несёт свежую пару, чтобы это
        // устройство осталось в системе. Без её сохранения следующий запрос отдал
        // бы 401 и выбросил пользователя на экран входа.
        if (res.data?.token) {
          storeSession(res.data.token, res.data.refresh_token)
        }
        message.value = 'Пароль изменён. На других устройствах потребуется войти заново.'
        oldPassword.value = ''
        newPassword.value = ''
        confirmPassword.value = ''
      } catch (err: any) {
        messageIsError.value = true
        message.value = err.response?.data?.error || err.response?.data || 'Ошибка при смене пароля'
      } finally {
        changing.value = false
      }
    }

    return { oldPassword, newPassword, confirmPassword, changing, message, messageIsError, goBack, changePassword }
  },
})
</script>

<style scoped>
.pwd-page-wrapper {
  min-height: 100vh;
  background: #f8fafc;
  font-family: 'Outfit', sans-serif;
  padding: 32px 16px;
  color: #334155;
}
.pwd-container {
  max-width: 480px;
  margin: 0 auto;
}
.top-nav {
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
}
.pwd-card {
  background: #ffffff;
  border: 1px solid #e2e8f0;
  border-radius: 24px;
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.section-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 16px;
  font-weight: 700;
  color: #0f172a;
}
.section-subtitle {
  font-size: 13px;
  color: #64748b;
  margin-top: 4px;
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
.form-input {
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  padding: 11px 14px;
  font-size: 15px;
  color: #0f172a;
  font-family: inherit;
  background: #f8fafc;
}
.form-input:focus {
  outline: none;
  border-color: #6366f1;
  background: #fff;
}
.btn-primary {
  align-self: flex-start;
  border: none;
  border-radius: 12px;
  background: #6366f1;
  color: #fff;
  font-weight: 600;
  font-size: 14px;
  padding: 12px 20px;
  cursor: pointer;
}
.btn-primary:disabled {
  opacity: 0.6;
  cursor: default;
}
.pwd-msg {
  font-size: 13px;
  color: #15803d;
}
.pwd-msg.error {
  color: #dc2626;
}
@media (max-width: 640px) {
  .btn-primary {
    width: 100%;
    justify-content: center;
  }
}
</style>
