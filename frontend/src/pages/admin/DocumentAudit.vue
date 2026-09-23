<template>
  <div class="da-page">
    <div class="da-head">
      <div>
        <h2 class="da-title"><i class="ph-fill ph-eyes"></i> Аудит документов</h2>
        <p class="da-sub">
          Каждое обращение к паспорту пишется здесь: кто смотрел данные, кто смотрел фото, кто вносил и кто удалял.
          Само это право паспортных данных не открывает — только журнал.
        </p>
      </div>
      <button type="button" class="da-btn" :disabled="loading" @click="reload">
        <i class="ph-bold ph-arrows-clockwise"></i> Обновить
      </button>
    </div>

    <div class="da-filters">
      <input v-model="search" class="da-input" placeholder="Телефон — чей паспорт или кто смотрел" @keyup.enter="reload" />
      <select v-model="action" class="da-input" @change="reload">
        <option value="">Любое действие</option>
        <option value="VIEW">Смотрел данные</option>
        <option value="VIEW_PHOTO">Смотрел фото</option>
        <option value="WRITE">Вносил или правил</option>
        <option value="DELETE">Удалил</option>
      </select>
      <label class="da-check">
        <input v-model="hideSelf" type="checkbox" @change="reload" />
        Скрыть обращения к своему паспорту
      </label>
      <button v-if="userId" type="button" class="da-btn light" @click="clearUser">
        Показать всех
      </button>
    </div>

    <p v-if="error" class="da-error">{{ error }}</p>
    <p v-if="loading" class="da-muted">Загружаем…</p>
    <p v-else-if="items.length === 0" class="da-muted">Записей нет.</p>

    <table v-else class="da-table">
      <thead>
        <tr>
          <th>Когда</th>
          <th>Кто</th>
          <th>Что сделал</th>
          <th>Чей паспорт</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="row in items" :key="row.id" :class="{ self: row.self }">
          <td>{{ formatDate(row.created_at) }}</td>
          <td>
            <div class="da-who">{{ row.viewer_name || row.viewer_phone }}</div>
            <div class="da-muted small">{{ row.viewer_phone }} · {{ roleTitle(row.viewer_role) }}</div>
          </td>
          <td>
            <span class="da-tag" :class="tagClass(row.action)">{{ ACTIONS[row.action] }}</span>
            <span v-if="row.self" class="da-muted small"> свой паспорт</span>
          </td>
          <td>
            <button type="button" class="da-link" @click="onlyUser(row.user_id)">
              {{ row.user_name || row.user_phone || '—' }}
            </button>
            <div class="da-muted small">{{ row.user_phone }}</div>
          </td>
        </tr>
      </tbody>
    </table>

    <div v-if="total > limit" class="da-pager">
      <button type="button" class="da-btn light" :disabled="page === 1 || loading" @click="go(page - 1)">Назад</button>
      <span class="da-muted">{{ page }} из {{ pages }} · всего {{ total }}</span>
      <button type="button" class="da-btn light" :disabled="page >= pages || loading" @click="go(page + 1)">Вперёд</button>
    </div>
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { passportAccessLog, type PassportAccess } from '../../api/passport'

// Аудит обращений к документам
// (doc/implementation_plan_delivery_passport.md §3.5). Паспортных данных здесь
// нет и быть не должно: страница отвечает на вопрос «кто смотрел», а не «что
// написано в паспорте».
const ACTIONS: Record<string, string> = {
  VIEW: 'Смотрел данные',
  VIEW_PHOTO: 'Смотрел фото',
  WRITE: 'Вносил или правил',
  DELETE: 'Удалил',
}

const ROLES: Record<string, string> = {
  CUSTOMER: 'заказчик',
  EXECUTOR: 'исполнитель',
  MODERATOR: 'модератор',
  ADMIN: 'администратор',
}

export default defineComponent({
  name: 'DocumentAudit',
  setup() {
    const route = useRoute()
    const router = useRouter()
    const items = ref<PassportAccess[]>([])
    const total = ref(0)
    const page = ref(1)
    const limit = 50
    const loading = ref(true)
    const error = ref('')
    const search = ref('')
    const action = ref('')
    const hideSelf = ref(false)
    // Ссылка из карточки пользователя приходит с user_id: аудит открывается
    // сразу по одному человеку.
    const userId = ref(typeof route.query.user_id === 'string' ? route.query.user_id : '')

    const pages = computed(() => Math.max(1, Math.ceil(total.value / limit)))

    const load = async () => {
      loading.value = true
      error.value = ''
      try {
        const data = await passportAccessLog({
          page: page.value,
          limit,
          search: search.value.trim() || undefined,
          action: action.value || undefined,
          hide_self: hideSelf.value ? '1' : undefined,
          user_id: userId.value || undefined,
        })
        items.value = data.items
        total.value = data.total
      } catch (err: any) {
        error.value = err.response?.data?.message || 'Не удалось загрузить журнал'
      } finally {
        loading.value = false
      }
    }

    const reload = () => {
      page.value = 1
      load()
    }

    const go = (next: number) => {
      page.value = next
      load()
    }

    const onlyUser = (id: string) => {
      userId.value = id
      void router.replace({ query: { ...route.query, user_id: id } })
      reload()
    }

    const clearUser = () => {
      userId.value = ''
      const query = { ...route.query }
      delete query.user_id
      void router.replace({ query })
      reload()
    }

    const formatDate = (value: string) => new Date(value).toLocaleString('ru-RU')
    const roleTitle = (role: string) => ROLES[role] || role
    const tagClass = (value: string) =>
      value === 'DELETE' ? 'danger' : value === 'WRITE' ? 'warn' : ''

    onMounted(load)

    return {
      items, total, page, pages, limit, loading, error, search, action, hideSelf, userId,
      ACTIONS, reload, go, onlyUser, clearUser, formatDate, roleTitle, tagClass,
    }
  },
})
</script>

<style scoped>
.da-page {
  padding: 8px 4px 24px;
}
.da-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
  flex-wrap: wrap;
  margin-bottom: 16px;
}
.da-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 20px;
  font-weight: 700;
  color: #0f172a;
  margin: 0;
}
.da-sub {
  margin: 6px 0 0;
  font-size: 13px;
  color: #64748b;
  max-width: 680px;
  line-height: 1.45;
}
.da-filters {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  align-items: center;
  margin-bottom: 14px;
}
.da-input {
  border: 1px solid #e2e8f0;
  border-radius: 10px;
  padding: 8px 12px;
  font-size: 13px;
  background: #fff;
  min-width: 200px;
}
.da-check {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: #475569;
}
.da-btn {
  border: 1px solid #e2e8f0;
  background: #fff;
  border-radius: 10px;
  padding: 8px 14px;
  font-size: 13px;
  font-weight: 600;
  color: #334155;
  cursor: pointer;
}
.da-btn.light {
  background: #f8fafc;
}
.da-error {
  background: #fef2f2;
  color: #b91c1c;
  border-radius: 10px;
  padding: 8px 12px;
  font-size: 13px;
}
.da-muted {
  color: #64748b;
  font-size: 13px;
}
.da-muted.small {
  font-size: 12px;
}
.da-table {
  width: 100%;
  border-collapse: collapse;
  background: #fff;
  border: 1px solid #e2e8f0;
  border-radius: 14px;
  overflow: hidden;
  font-size: 13px;
}
.da-table th {
  text-align: left;
  padding: 10px 12px;
  background: #f8fafc;
  color: #475569;
  font-weight: 600;
}
.da-table td {
  padding: 10px 12px;
  border-top: 1px solid #eef2f7;
  vertical-align: top;
}
.da-table tr.self {
  color: #64748b;
}
.da-who {
  font-weight: 600;
  color: #0f172a;
}
.da-tag {
  border-radius: 999px;
  padding: 3px 10px;
  background: #f1f5f9;
  color: #475569;
  white-space: nowrap;
}
.da-tag.warn {
  background: #fef3c7;
  color: #b45309;
}
.da-tag.danger {
  background: #fee2e2;
  color: #b91c1c;
}
.da-link {
  border: none;
  background: none;
  padding: 0;
  font: inherit;
  font-weight: 600;
  color: #0f766e;
  cursor: pointer;
  text-align: left;
}
.da-pager {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 14px;
}
@media (max-width: 640px) {
  .da-table,
  .da-table thead,
  .da-table tbody,
  .da-table tr,
  .da-table td {
    display: block;
    width: 100%;
  }
  .da-table thead {
    display: none;
  }
  .da-table tr {
    border-top: 1px solid #eef2f7;
    padding: 8px 0;
  }
  .da-table td {
    border: none;
    padding: 4px 12px;
  }
}
</style>
