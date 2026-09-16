<template>
  <div v-if="visible" class="softban-overlay" role="alertdialog" aria-modal="true">
    <div class="softban-card">
      <div class="softban-icon"><i class="ph-fill ph-lock-key"></i></div>
      <h2 class="softban-title">Аккаунт заблокирован</h2>
      <p class="softban-text">
        Новые заказы недоступны. Чтобы разобраться с блокировкой, напишите в службу поддержки.
      </p>
      <p v-if="reason" class="softban-reason">Причина: {{ reason }}</p>

      <div v-if="balance !== null" class="softban-balance">
        Баланс: <strong>{{ Number(balance).toLocaleString('ru-RU', { minimumFractionDigits: 2 }) }} ₽</strong>
      </div>

      <div class="softban-actions">
        <button type="button" class="softban-btn primary" @click="showSupport = true">
          <i class="ph-bold ph-headset"></i> Написать в поддержку
        </button>
        <button type="button" class="softban-btn secondary" @click="openMail">
          <i class="ph-bold ph-envelope-simple"></i> Почта
        </button>
        <button type="button" class="softban-btn secondary" @click="dismissed = true">
          <i class="ph-bold ph-package"></i> Текущие заказы
        </button>
        <button type="button" class="softban-btn ghost" @click="logout">Выйти</button>
      </div>
    </div>
    <SupportChatModal v-model:show="showSupport" />
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth-store'
import api from '../services/api'
import SupportChatModal from './SupportChatModal.vue'

/**
 * Экран мягкого бана. Заблокированного пускают в приложение ради поддержки,
 * почты, баланса и уже взятых заказов — всё остальное сервер отклоняет. Экран
 * можно свернуть, чтобы довести текущие заказы; следующий отказ сервера с
 * кодом мягкого бана откроет его снова.
 */
export default defineComponent({
  name: 'SoftBanScreen',
  components: { SupportChatModal },
  setup() {
    const authStore = useAuthStore()
    const route = useRoute()
    const router = useRouter()
    const dismissed = ref(false)
    const showSupport = ref(false)
    const reason = ref('')

    const visible = computed(
      () => authStore.isSoftBanned && !dismissed.value && route.path !== '/mail' && route.path !== '/login',
    )
    const balance = computed(() => authStore.balance)

    // Новый отказ сервера разворачивает свёрнутый экран.
    watch(
      () => authStore.softBanReported,
      (reported) => {
        if (reported) dismissed.value = false
      },
    )
    watch(
      () => authStore.isSoftBanned,
      async (banned) => {
        if (!banned) return
        try {
          const res = await api.get('/me/penalty-status')
          reason.value = res.data?.soft_ban_reason || ''
        } catch {
          reason.value = ''
        }
      },
      { immediate: true },
    )

    const openMail = () => router.push('/mail')
    const logout = () => {
      authStore.logout()
      router.replace('/login')
    }

    return { visible, dismissed, showSupport, reason, balance, openMail, logout }
  },
})
</script>

<style scoped>
.softban-overlay {
  position: fixed;
  inset: 0;
  z-index: 1500;
  background: rgba(15, 23, 42, 0.72);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
}
.softban-card {
  background: #fff;
  border-radius: 22px;
  width: 100%;
  max-width: 400px;
  padding: 26px 22px 20px;
  text-align: center;
  box-shadow: 0 24px 48px -16px rgba(15, 23, 42, 0.45);
}
.softban-icon {
  width: 64px;
  height: 64px;
  margin: 0 auto 12px;
  border-radius: 18px;
  background: #fee2e2;
  color: #dc2626;
  font-size: 34px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.softban-title {
  margin: 0 0 8px;
  font-size: 21px;
  font-weight: 800;
  color: #0f172a;
}
.softban-text {
  margin: 0 0 10px;
  color: #475569;
  font-size: 14px;
  line-height: 1.45;
}
.softban-reason {
  margin: 0 0 10px;
  font-size: 13px;
  color: #7f1d1d;
  background: #fef2f2;
  border-radius: 10px;
  padding: 8px 10px;
}
.softban-balance {
  margin: 0 0 16px;
  font-size: 14px;
  color: #334155;
}
.softban-actions {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.softban-btn {
  height: 46px;
  border-radius: 12px;
  border: none;
  font-size: 15px;
  font-weight: 600;
  cursor: pointer;
  font-family: inherit;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
}
.softban-btn.primary {
  background: #4f46e5;
  color: #fff;
}
.softban-btn.secondary {
  background: #f1f5f9;
  color: #334155;
}
.softban-btn.ghost {
  background: transparent;
  color: #64748b;
}
</style>
