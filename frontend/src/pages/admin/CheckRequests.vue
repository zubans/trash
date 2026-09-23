<template>
  <div class="cr-page">
    <div class="cr-head">
      <div>
        <h2 class="cr-title"><i class="ph-fill ph-seal-check"></i> Заявки на статус «проверенный»</h2>
        <p class="cr-sub">
          Заявку ставит система, как только у человека есть паспорт и фото документа. Писать в поддержку для этого не
          нужно, поэтому очередь живёт здесь, а не в чатах.
        </p>
      </div>
      <button type="button" class="cr-refresh" :disabled="loading" @click="load">
        <i class="ph-bold ph-arrows-clockwise"></i> Обновить
      </button>
    </div>

    <p v-if="error" class="cr-error">{{ error }}</p>
    <p v-if="loading" class="cr-muted">Загружаем…</p>
    <p v-else-if="requests.length === 0" class="cr-muted">Заявок нет.</p>

    <table v-else class="cr-table">
      <thead>
        <tr>
          <th>Кто</th>
          <th>Заявка</th>
          <th>Фото</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="item in requests" :key="item.user_id">
          <td>
            <div class="cr-who">{{ item.name || item.phone }}</div>
            <div class="cr-muted small">{{ item.phone }} · {{ roleTitle(item.role) }}</div>
          </td>
          <td>
            <div>{{ formatDate(item.requested_at) }}</div>
            <div class="cr-muted small">{{ waiting(item.requested_at) }}</div>
          </td>
          <td>
            <span v-if="!item.has_photo" class="cr-tag warn">нет фото</span>
            <span v-else class="cr-tag" :class="originClass(item.photo_seal)">{{ originText(item.photo_seal) }}</span>
          </td>
          <td class="cr-actions">
            <button type="button" class="cr-open" @click="open(item)">Открыть паспорт</button>
          </td>
        </tr>
      </tbody>
    </table>

    <!-- Решение принимается там же, где паспорт: та же карточка, что в списке
         пользователей, сразу на вкладке «Паспорт». -->
    <UserHistoryModal v-model="showCard" :user="selected" initial-tab="passport" @update:modelValue="onCardClosed" />
  </div>
</template>

<script lang="ts">
import { defineComponent, onMounted, ref } from 'vue'
import api from '../../services/api'
import UserHistoryModal from './UserHistoryModal.vue'

// Очередь заявок на статус «проверенный»
// (doc/implementation_plan_delivery_passport.md §2).
interface CheckRequest {
  user_id: string
  phone: string
  name?: string
  role: string
  is_verified: boolean
  requested_at: string
  has_photo: boolean
  photo_seal?: 'VALID' | 'MISSING' | 'INVALID'
}

const ROLES: Record<string, string> = {
  CUSTOMER: 'заказчик',
  EXECUTOR: 'исполнитель',
  MODERATOR: 'модератор',
  ADMIN: 'администратор',
}

export default defineComponent({
  name: 'CheckRequests',
  components: { UserHistoryModal },
  setup() {
    const requests = ref<CheckRequest[]>([])
    const loading = ref(true)
    const error = ref('')
    const showCard = ref(false)
    const selected = ref<any | null>(null)

    const load = async () => {
      loading.value = true
      error.value = ''
      try {
        requests.value = (await api.get('/admin/check-requests')).data || []
      } catch (err: any) {
        error.value = err.response?.data?.message || 'Не удалось загрузить заявки'
      } finally {
        loading.value = false
      }
    }

    const open = (item: CheckRequest) => {
      selected.value = { id: item.user_id, phone: item.phone, last_name: item.name, role: item.role }
      showCard.value = true
    }

    // Карточку закрыли — решение могло быть принято, и заявки в очереди больше
    // нет: список перечитывается, а не правится на глаз.
    const onCardClosed = (value: boolean) => {
      if (!value) load()
    }

    const formatDate = (value: string) => new Date(value).toLocaleString('ru-RU')

    const waiting = (value: string) => {
      const days = Math.floor((Date.now() - new Date(value).getTime()) / 86400000)
      if (days < 1) return 'меньше суток'
      return `${days} ${days === 1 ? 'день' : days < 5 ? 'дня' : 'дней'} в очереди`
    }

    const roleTitle = (role: string) => ROLES[role] || role

    const originText = (seal?: string) =>
      seal === 'VALID' ? 'снято в приложении' : 'подписи приложения нет'
    const originClass = (seal?: string) => (seal === 'VALID' ? 'ok' : 'warn')

    onMounted(load)

    return {
      requests, loading, error, showCard, selected,
      load, open, onCardClosed, formatDate, waiting, roleTitle, originText, originClass,
    }
  },
})
</script>

<style scoped>
.cr-page {
  padding: 8px 4px 24px;
}
.cr-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
  flex-wrap: wrap;
  margin-bottom: 16px;
}
.cr-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 20px;
  font-weight: 700;
  color: #0f172a;
  margin: 0;
}
.cr-sub {
  margin: 6px 0 0;
  font-size: 13px;
  color: #64748b;
  max-width: 640px;
  line-height: 1.45;
}
.cr-refresh {
  border: 1px solid #e2e8f0;
  background: #fff;
  border-radius: 10px;
  padding: 8px 14px;
  font-size: 13px;
  font-weight: 600;
  color: #334155;
  cursor: pointer;
}
.cr-error {
  background: #fef2f2;
  color: #b91c1c;
  border-radius: 10px;
  padding: 8px 12px;
  font-size: 13px;
}
.cr-muted {
  color: #64748b;
  font-size: 13px;
}
.cr-muted.small {
  font-size: 12px;
}
.cr-table {
  width: 100%;
  border-collapse: collapse;
  background: #fff;
  border: 1px solid #e2e8f0;
  border-radius: 14px;
  overflow: hidden;
  font-size: 13px;
}
.cr-table th {
  text-align: left;
  padding: 10px 12px;
  background: #f8fafc;
  color: #475569;
  font-weight: 600;
}
.cr-table td {
  padding: 10px 12px;
  border-top: 1px solid #eef2f7;
  vertical-align: top;
}
.cr-who {
  font-weight: 600;
  color: #0f172a;
}
.cr-tag {
  border-radius: 999px;
  padding: 3px 10px;
  background: #f1f5f9;
  color: #475569;
  white-space: nowrap;
}
.cr-tag.ok {
  background: #dcfce7;
  color: #15803d;
}
.cr-tag.warn {
  background: #fef3c7;
  color: #b45309;
}
.cr-actions {
  text-align: right;
}
.cr-open {
  border: none;
  background: #0f766e;
  color: #fff;
  border-radius: 10px;
  padding: 8px 12px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
}
@media (max-width: 640px) {
  .cr-table,
  .cr-table thead,
  .cr-table tbody,
  .cr-table tr,
  .cr-table td {
    display: block;
    width: 100%;
  }
  .cr-table thead {
    display: none;
  }
  .cr-table tr {
    border-top: 1px solid #eef2f7;
    padding: 8px 0;
  }
  .cr-table td {
    border: none;
    padding: 4px 12px;
  }
  .cr-actions {
    text-align: left;
  }
}
</style>
