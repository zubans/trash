<template>
  <div class="orders-page">
    <div class="admin-card">
      <!-- Действия страницы; заголовок выводит шапка раскладки -->
      <div class="page-header">
        <button
          type="button"
          class="btn-primary"
          :disabled="total === 0 || exporting"
          @click="exportCsv"
        >
          <i class="ph-bold" :class="exporting ? 'ph-spinner' : 'ph-export'"></i>
          {{ exporting ? 'Готовим файл…' : 'Экспорт CSV' }}
        </button>
      </div>

      <!-- Панель инструментов -->
      <div class="toolbar">
        <select v-model="statusFilter" class="filter-btn" name="status">
          <option v-for="g in statusGroups" :key="g.value" :value="g.value">{{ g.label }}</option>
        </select>

        <div class="search-box">
          <i class="ph-bold ph-magnifying-glass"></i>
          <input v-model="searchQuery" type="text" placeholder="Поиск по телефону или ID..." />
        </div>

        <select v-model="serviceFilter" class="filter-btn">
          <option value="">Все услуги</option>
          <option v-for="name in serviceOptions" :key="name" :value="name">{{ name }}</option>
        </select>

        <select v-model="periodFilter" class="filter-btn">
          <option value="">Все периоды</option>
          <option v-for="p in periodOptions" :key="p.value" :value="p.value">{{ p.label }}</option>
        </select>

        <span class="toolbar-count">
          {{ rangeLabel }}
        </span>
      </div>

      <p v-if="actionMsg" class="action-msg" :class="{ error: actionIsError }">{{ actionMsg }}</p>

      <!-- Таблица -->
      <div class="grid-table">
        <div class="grid-row grid-header">
          <button type="button" class="th sortable" @click="toggleSort('service')">
            Тип услуги <i class="ph-bold" :class="sortIcon('service')"></i>
          </button>
          <button type="button" class="th sortable" @click="toggleSort('customer')">
            Заказчик <i class="ph-bold" :class="sortIcon('customer')"></i>
          </button>
          <button type="button" class="th sortable" @click="toggleSort('executor')">
            Исполнитель <i class="ph-bold" :class="sortIcon('executor')"></i>
          </button>
          <button type="button" class="th sortable" @click="toggleSort('final_amount')">
            Сумма <i class="ph-bold" :class="sortIcon('final_amount')"></i>
          </button>
          <div class="th">Адрес</div>
          <button type="button" class="th sortable" @click="toggleSort('status')">
            Статус <i class="ph-bold" :class="sortIcon('status')"></i>
          </button>
          <button type="button" class="th sortable" @click="toggleSort('date')">
            Дата <i class="ph-bold" :class="sortIcon('date')"></i>
          </button>
          <div class="th"></div>
        </div>

        <div v-for="o in orders" :key="o.id" class="grid-row grid-item">
          <div class="cell">
            <div class="service-title">{{ o.service_variant_name || '—' }}</div>
            <div class="tags-container">
              <span v-if="o.is_urgent" class="badge urgent">
                <i class="ph-bold ph-lightning"></i> {{ $t('users.urgent') }}
              </span>
              <span v-if="o.is_asap" class="badge asap">
                <i class="ph-bold ph-timer"></i> {{ $t('users.asap') }}
              </span>
              <span v-if="!o.is_urgent && !o.is_asap" class="badge standard">Обычный</span>
            </div>
          </div>

          <div class="cell">
            <div class="phone-wrapper">
              <div class="phone-icon customer"><i class="ph-bold ph-user"></i></div>
              <span class="phone-number">{{ o.customer_phone ? formatPhoneMask(o.customer_phone) : '—' }}</span>
            </div>
          </div>

          <div class="cell">
            <div v-if="o.executor_phone" class="phone-wrapper">
              <div class="phone-icon executor"><i class="ph-bold ph-wrench"></i></div>
              <span class="phone-number">{{ formatPhoneMask(o.executor_phone) }}</span>
            </div>
            <span v-else class="muted">—</span>
          </div>

          <div class="cell">
            <span class="amount" :class="{ free: isFree(o) }">{{ amountLabel(o) }}</span>
          </div>

          <div class="cell">
            <span class="address-text" :title="o.address || '—'">{{ splitAddress(o.address).main }}</span>
            <span v-if="splitAddress(o.address).sub" class="address-sub">{{ splitAddress(o.address).sub }}</span>
          </div>

          <div class="cell">
            <span class="status-badge" :class="o.status">{{ statusLabel(o.status) }}</span>
          </div>

          <div class="cell">
            <span class="date-main">{{ formatDay(eventAt(o)) }}</span>
            <span class="date-time">{{ formatTime(eventAt(o)) }}</span>
          </div>

          <div class="cell">
            <button
              v-if="canEdit && canReturnToWork(o.status)"
              type="button"
              class="btn-return"
              :disabled="returning === o.id"
              @click="returnToWork(o)"
            >
              <i class="ph-bold ph-arrow-counter-clockwise"></i>
              Вернуть в работу
            </button>
          </div>
        </div>

        <div v-if="loading" class="table-note">Загрузка…</div>
        <div v-else-if="orders.length === 0 && hasFilters" class="table-note">
          Ничего не найдено по заданным фильтрам
        </div>
        <div v-else-if="orders.length === 0" class="table-note">Заказов нет</div>
      </div>

      <!-- Постраничная навигация -->
      <div v-if="totalPages > 1" class="table-footer">
        <button type="button" class="page-btn" :disabled="page === 1" @click="goToPage(page - 1)">
          <i class="ph-bold ph-caret-left"></i> Назад
        </button>
        <span class="page-info">Страница {{ page }} из {{ totalPages }}</span>
        <button type="button" class="page-btn" :disabled="page === totalPages" @click="goToPage(page + 1)">
          Вперёд <i class="ph-bold ph-caret-right"></i>
        </button>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, onMounted, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '../../stores/auth-store'
import api from '../../services/api'
import { formatPhoneMask } from '../../utils/phoneMask'
import {
  ORDER_STATUS_LABELS,
  RETURN_TO_WORK_CONFIRM,
  canReturnToWork,
  returnOrderToWork,
} from '../../api/admin-orders'

interface AdminOrderRow {
  id: string
  status: string
  customer_phone?: string
  executor_phone?: string
  service_variant_name?: string
  is_urgent?: boolean
  is_asap?: boolean
  hold_amount?: number
  final_amount?: number
  address?: string
  created_at?: string
  executed_at?: string
  completed_at?: string
  canceled_at?: string
}

// Группы статусов — те же, что понимает GET /admin/orders?status=.
const STATUS_GROUPS = [
  { value: 'active', label: 'Активные' },
  { value: 'review', label: 'На проверке' },
  { value: 'completed', label: 'Выполненные' },
  { value: 'canceled', label: 'Отменённые' },
  { value: 'all', label: 'Все заказы' },
]
const DEFAULT_GROUP = 'active'

export default defineComponent({
  name: 'AdminOrders',
  setup() {
    const authStore = useAuthStore()
    const route = useRoute()
    const router = useRouter()
    const orders = ref<AdminOrderRow[]>([])
    const loading = ref(false)

    const groupFromRoute = () => {
      const value = String(route.query.status || '')
      return STATUS_GROUPS.some((g) => g.value === value) ? value : DEFAULT_GROUP
    }

    const statusFilter = ref(groupFromRoute())
    const searchQuery = ref('')
    const serviceFilter = ref('')
    const periodFilter = ref('')
    const sortKey = ref('date')
    const sortDesc = ref(true)
    const page = ref(1)
    const total = ref(0)
    const exporting = ref(false)
    const serviceOptions = ref<string[]>([])
    const periodKeys = ref<string[]>([])
    const returning = ref('')
    const actionMsg = ref('')
    const actionIsError = ref(false)

    const canEdit = computed(() => authStore.can('orders.edit'))

    const PAGE_SIZE = 50
    // Сервер отказывает, если попросить больше за один запрос, поэтому полная
    // выгрузка обходит страницы, а не запрашивает всё разом.
    const MAX_PAGE_SIZE = 200

    const currencySymbol = computed(() => (authStore.currency === 'RUB' ? '₽' : '$'))

    const amountOf = (o: AdminOrderRow) => Number(o.final_amount ?? o.hold_amount ?? 0)

    const isFree = (o: AdminOrderRow) => amountOf(o) === 0

    const amountLabel = (o: AdminOrderRow) =>
      isFree(o) ? 'Бесплатно' : `${amountOf(o).toFixed(2)} ${currencySymbol.value}`

    const statusLabel = (status: string) => ORDER_STATUS_LABELS[status] || status

    // Дата последнего события — та же, по которой сервер сортирует и считает периоды.
    const eventAt = (o: AdminOrderRow) => o.completed_at || o.canceled_at || o.executed_at || o.created_at

    // Адреса собираются как «Город, Улица, д. X, кв. Y», поэтому дом и квартира
    // чисто отделяются на собственную строку.
    const splitAddress = (address?: string) => {
      const value = (address || '').trim()
      if (!value) return { main: '—', sub: '' }
      const at = value.indexOf(', д. ')
      if (at === -1) return { main: value, sub: '' }
      return { main: value.slice(0, at), sub: value.slice(at + 2) }
    }

    const formatDay = (dateStr?: string) =>
      dateStr ? new Date(dateStr).toLocaleDateString('ru-RU') : '—'

    const formatTime = (dateStr?: string) =>
      dateStr ? new Date(dateStr).toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' }) : ''

    const monthNames = [
      'Январь', 'Февраль', 'Март', 'Апрель', 'Май', 'Июнь',
      'Июль', 'Август', 'Сентябрь', 'Октябрь', 'Ноябрь', 'Декабрь',
    ]

    // Периоды приходят как YYYY-MM, что как текст сортируется хронологически.
    const periodOptions = computed(() =>
      periodKeys.value.map((value) => {
        const [year, month] = value.split('-')
        return { value, label: `${monthNames[Number(month) - 1]} ${year}` }
      }),
    )

    const totalPages = computed(() => Math.max(1, Math.ceil(total.value / PAGE_SIZE)))

    const hasFilters = computed(
      () => Boolean(searchQuery.value.trim() || serviceFilter.value || periodFilter.value),
    )

    const rangeLabel = computed(() => {
      if (total.value === 0) return 'Ничего не найдено'
      const from = (page.value - 1) * PAGE_SIZE + 1
      const to = Math.min(page.value * PAGE_SIZE, total.value)
      return `${from}–${to} из ${total.value}`
    })

    const queryParams = (limit: number, offset: number) => ({
      status: statusFilter.value,
      search: searchQuery.value.trim() || undefined,
      service: serviceFilter.value || undefined,
      period: periodFilter.value || undefined,
      sort: sortKey.value,
      order: sortDesc.value ? 'desc' : 'asc',
      limit,
      offset,
    })

    const fetchOrders = async () => {
      loading.value = true
      try {
        const response = await api.get('/admin/orders', {
          params: queryParams(PAGE_SIZE, (page.value - 1) * PAGE_SIZE),
        })
        orders.value = response.data?.orders || []
        total.value = response.data?.total || 0
        serviceOptions.value = response.data?.services || []
        periodKeys.value = response.data?.periods || []
      } catch (err) {
        console.error('Error fetching orders:', err)
      } finally {
        loading.value = false
      }
    }

    // Фильтрация и сортировка происходят в SQL, поэтому любое изменение начинает с
    // первой страницы и перезапрашивает, а не тасует уже выведенные строки.
    let searchTimer: ReturnType<typeof setTimeout> | undefined
    const reload = () => {
      page.value = 1
      fetchOrders()
    }
    watch([serviceFilter, periodFilter], reload)
    watch(searchQuery, () => {
      clearTimeout(searchTimer)
      searchTimer = setTimeout(reload, 300)
    })

    // Группа статусов живёт в адресе: пункт меню и ссылка «на проверке» ведут
    // сразу на нужный фильтр. Услуги и периоды у групп свои, поэтому выбранные
    // значения сбрасываются.
    watch(statusFilter, (value) => {
      if (route.query.status !== value) {
        router.replace({ query: { ...route.query, status: value } })
      }
      serviceFilter.value = ''
      periodFilter.value = ''
      actionMsg.value = ''
      reload()
    })
    watch(
      () => route.query.status,
      () => {
        statusFilter.value = groupFromRoute()
      },
    )

    const toggleSort = (key: string) => {
      if (sortKey.value === key) {
        sortDesc.value = !sortDesc.value
      } else {
        sortKey.value = key
        sortDesc.value = true
      }
      reload()
    }

    const sortIcon = (key: string) => {
      if (sortKey.value !== key) return 'ph-caret-up-down'
      return sortDesc.value ? 'ph-caret-down' : 'ph-caret-up'
    }

    const goToPage = (next: number) => {
      if (next < 1 || next > totalPages.value) return
      page.value = next
      fetchOrders()
    }

    const returnToWork = async (o: AdminOrderRow) => {
      if (returning.value || !window.confirm(RETURN_TO_WORK_CONFIRM)) return
      returning.value = o.id
      actionMsg.value = ''
      actionIsError.value = false
      try {
        await returnOrderToWork(o.id)
        actionMsg.value = 'Заказ возвращён в работу.'
        await fetchOrders()
      } catch (err: any) {
        actionIsError.value = true
        actionMsg.value =
          typeof err?.response?.data === 'string' && err.response.data
            ? err.response.data
            : 'Не удалось вернуть заказ в работу'
      } finally {
        returning.value = ''
      }
    }

    const csvCell = (value: string) => `"${String(value ?? '').replace(/"/g, '""')}"`

    const exportCsv = async () => {
      if (exporting.value) return
      exporting.value = true
      try {
        // Выгрузка покрывает весь отфильтрованный набор, поэтому обходит страницы
        // сервера, а не сбрасывает 50 строк, что сейчас на экране.
        const rows: AdminOrderRow[] = []
        let offset = 0
        do {
          const response = await api.get('/admin/orders', {
            params: queryParams(MAX_PAGE_SIZE, offset),
          })
          const batch: AdminOrderRow[] = response.data?.orders || []
          total.value = response.data?.total ?? total.value
          rows.push(...batch)
          if (batch.length < MAX_PAGE_SIZE) break
          offset += MAX_PAGE_SIZE
        } while (offset < total.value)

        const header = ['ID', 'Услуга', 'Срочно', 'ASAP', 'Заказчик', 'Исполнитель', 'Сумма', 'Адрес', 'Статус', 'Дата']
        const body = rows.map((o) => [
          o.id,
          o.service_variant_name || '',
          o.is_urgent ? 'да' : 'нет',
          o.is_asap ? 'да' : 'нет',
          o.customer_phone ? formatPhoneMask(o.customer_phone) : '',
          o.executor_phone ? formatPhoneMask(o.executor_phone) : '',
          amountOf(o).toFixed(2),
          o.address || '',
          statusLabel(o.status),
          eventAt(o) ? `${formatDay(eventAt(o))} ${formatTime(eventAt(o))}` : '',
        ])
        // Точка с запятой и BOM: так файл открывается в русском Excel без импорта.
        const csv = [header, ...body].map((row) => row.map(csvCell).join(';')).join('\r\n')
        const blob = new Blob(['\ufeff' + csv], { type: 'text/csv;charset=utf-8;' })
        const url = URL.createObjectURL(blob)
        const link = document.createElement('a')
        link.href = url
        link.download = `orders-${statusFilter.value}-${new Date().toISOString().slice(0, 10)}.csv`
        link.click()
        URL.revokeObjectURL(url)
      } catch (err) {
        console.error('Error exporting orders:', err)
        alert('Не удалось выгрузить CSV')
      } finally {
        exporting.value = false
      }
    }

    onMounted(fetchOrders)

    return {
      orders,
      loading,
      statusGroups: STATUS_GROUPS,
      statusFilter,
      searchQuery,
      serviceFilter,
      periodFilter,
      serviceOptions,
      periodOptions,
      total,
      page,
      totalPages,
      hasFilters,
      rangeLabel,
      exporting,
      returning,
      actionMsg,
      actionIsError,
      canEdit,
      isFree,
      amountLabel,
      statusLabel,
      eventAt,
      splitAddress,
      formatDay,
      formatTime,
      formatPhoneMask,
      canReturnToWork,
      toggleSort,
      sortIcon,
      goToPage,
      returnToWork,
      exportCsv,
    }
  },
})
</script>

<style scoped>
/* Общий вид карточки-таблицы живёт в styles/admin-table.css: он делится с
   историей транзакций. Здесь остаётся только то, что есть на этой странице. */

.orders-page {
  display: flex;
  flex-direction: column;
}

.grid-row {
  /* Услуга | Заказчик | Исполнитель | Сумма | Адрес | Статус | Дата | Действие */
  grid-template-columns: minmax(200px, 1.2fr) 200px 200px 110px minmax(200px, 1.5fr) 130px 120px 150px;
  min-width: 1310px;
}

/* Услуга и метки */
.service-title {
  font-weight: 800;
  font-size: 15px;
  color: #0f172a;
  margin-bottom: 4px;
}

.tags-container {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.badge.urgent {
  background: #fee2e2;
  color: #ef4444;
}

.badge.asap {
  background: #fef3c7;
  color: #d97706;
}

.badge.standard {
  background: #f1f5f9;
  color: #64748b;
}

.phone-icon.customer {
  background: #eef2ff;
  color: #5c60f5;
}

.phone-icon.executor {
  background: #fffbeb;
  color: #f59e0b;
}

/* Адрес */
.address-text {
  font-size: 13px;
  font-weight: 600;
  color: #0f172a;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.address-sub {
  font-size: 12px;
  color: #64748b;
  margin-top: 2px;
}

/* Статус */
.status-badge {
  display: inline-block;
  padding: 3px 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 700;
  background: #f1f5f9;
  color: #475569;
}

.status-badge.SEARCHING { background: #fef3c7; color: #b45309; }
.status-badge.ASSIGNED { background: #e0f2fe; color: #0369a1; }
.status-badge.EXECUTED { background: #ede9fe; color: #6d28d9; }
.status-badge.DISPUTED { background: #fee2e2; color: #b91c1c; }
.status-badge.COMPLETED { background: #dcfce7; color: #15803d; }
.status-badge.CANCELED { background: #f1f5f9; color: #64748b; }

.btn-return {
  border: 1px solid #c4b5fd;
  background: #f5f3ff;
  color: #6d28d9;
  border-radius: 10px;
  padding: 6px 10px;
  font-size: 12px;
  font-weight: 700;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  white-space: nowrap;
}

.btn-return:disabled {
  opacity: 0.6;
  cursor: default;
}

.action-msg {
  margin: 0 0 12px;
  font-size: 13px;
  color: #15803d;
}

.action-msg.error {
  color: #b91c1c;
}
</style>
