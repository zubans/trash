<template>
  <div class="completed-orders">
    <div class="admin-card">
      <!-- Page header -->
      <div class="page-header">
        <h1 class="page-title">{{ $t('users.completedOrders') }}</h1>
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

      <!-- Toolbar -->
      <div class="toolbar">
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

      <!-- Table -->
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
          <button type="button" class="th sortable" @click="toggleSort('completed_at')">
            Завершен <i class="ph-bold" :class="sortIcon('completed_at')"></i>
          </button>
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
            <span class="date-main">{{ formatDay(o.completed_at) }}</span>
            <span class="date-time">{{ formatTime(o.completed_at) }}</span>
          </div>
        </div>

        <div v-if="loading" class="table-note">Загрузка…</div>
        <div v-else-if="orders.length === 0 && hasFilters" class="table-note">
          Ничего не найдено по заданным фильтрам
        </div>
        <div v-else-if="orders.length === 0" class="table-note">Выполненных заказов пока нет</div>
      </div>

      <!-- Pagination -->
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
import { useAuthStore } from '../../stores/auth-store'
import api from '../../services/api'
import { formatPhoneMask } from '../../utils/phoneMask'

interface CompletedOrder {
  id: string
  customer_phone?: string
  executor_phone?: string
  service_variant_name?: string
  is_urgent?: boolean
  is_asap?: boolean
  final_amount?: number
  address?: string
  completed_at?: string
}

export default defineComponent({
  name: 'CompletedOrders',
  setup() {
    const authStore = useAuthStore()
    const orders = ref<CompletedOrder[]>([])
    const loading = ref(false)

    const searchQuery = ref('')
    const serviceFilter = ref('')
    const periodFilter = ref('')
    const sortKey = ref('completed_at')
    const sortDesc = ref(true)
    const page = ref(1)
    const total = ref(0)
    const exporting = ref(false)
    const serviceOptions = ref<string[]>([])
    const periodKeys = ref<string[]>([])

    const PAGE_SIZE = 50
    // The server refuses more than this per request, so a full export walks
    // pages instead of asking for everything at once.
    const MAX_PAGE_SIZE = 200

    const currencySymbol = computed(() => (authStore.currency === 'RUB' ? '₽' : '$'))

    const isFree = (o: CompletedOrder) => Number(o.final_amount || 0) === 0

    const amountLabel = (o: CompletedOrder) =>
      isFree(o) ? 'Бесплатно' : `${Number(o.final_amount).toFixed(2)} ${currencySymbol.value}`

    // Addresses are composed as "Город, Улица, д. X, кв. Y", so the building
    // and flat split off cleanly onto their own line.
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

    // Periods arrive as YYYY-MM, which sorts chronologically as text.
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
        const response = await api.get('/admin/orders/completed', {
          params: queryParams(PAGE_SIZE, (page.value - 1) * PAGE_SIZE),
        })
        orders.value = response.data?.orders || []
        total.value = response.data?.total || 0
        serviceOptions.value = response.data?.services || []
        periodKeys.value = response.data?.periods || []
      } catch (err) {
        console.error('Error fetching completed orders:', err)
      } finally {
        loading.value = false
      }
    }

    // Filtering and sorting happen in SQL, so any change restarts at page one
    // and refetches rather than reshuffling the rows already on screen.
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

    const csvCell = (value: string) => `"${String(value ?? '').replace(/"/g, '""')}"`

    const exportCsv = async () => {
      if (exporting.value) return
      exporting.value = true
      try {
        // The export covers the whole filtered set, so it pages through the
        // server instead of dumping the 50 rows currently rendered.
        const rows: CompletedOrder[] = []
        let offset = 0
        do {
          const response = await api.get('/admin/orders/completed', {
            params: queryParams(MAX_PAGE_SIZE, offset),
          })
          const batch: CompletedOrder[] = response.data?.orders || []
          total.value = response.data?.total ?? total.value
          rows.push(...batch)
          if (batch.length < MAX_PAGE_SIZE) break
          offset += MAX_PAGE_SIZE
        } while (offset < total.value)

        const header = ['ID', 'Услуга', 'Срочно', 'ASAP', 'Заказчик', 'Исполнитель', 'Сумма', 'Адрес', 'Завершён']
        const body = rows.map((o) => [
          o.id,
          o.service_variant_name || '',
          o.is_urgent ? 'да' : 'нет',
          o.is_asap ? 'да' : 'нет',
          o.customer_phone ? formatPhoneMask(o.customer_phone) : '',
          o.executor_phone ? formatPhoneMask(o.executor_phone) : '',
          Number(o.final_amount || 0).toFixed(2),
          o.address || '',
          o.completed_at ? `${formatDay(o.completed_at)} ${formatTime(o.completed_at)}` : '',
        ])
        // Semicolons and a BOM: this opens in Russian Excel without an import step.
        const csv = [header, ...body].map((row) => row.map(csvCell).join(';')).join('\r\n')
        const blob = new Blob(['\ufeff' + csv], { type: 'text/csv;charset=utf-8;' })
        const url = URL.createObjectURL(blob)
        const link = document.createElement('a')
        link.href = url
        link.download = `completed-orders-${new Date().toISOString().slice(0, 10)}.csv`
        link.click()
        URL.revokeObjectURL(url)
      } catch (err) {
        console.error('Error exporting completed orders:', err)
        alert('Не удалось выгрузить CSV')
      } finally {
        exporting.value = false
      }
    }

    onMounted(fetchOrders)

    return {
      orders,
      loading,
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
      currencySymbol,
      isFree,
      amountLabel,
      splitAddress,
      formatDay,
      formatTime,
      formatPhoneMask,
      toggleSort,
      sortIcon,
      goToPage,
      exportCsv,
    }
  },
})
</script>

<style scoped>
.completed-orders {
  display: flex;
  flex-direction: column;
}

.admin-card {
  background: #ffffff;
  border-radius: 16px;
  box-shadow: 0 4px 24px rgba(0, 0, 0, 0.03);
  overflow: hidden;
}

/* Page header */
.page-header {
  padding: 24px;
  border-bottom: 1px solid #e2e8f0;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
}

.page-title {
  font-size: 24px;
  font-weight: 900;
  letter-spacing: -0.5px;
  color: #0f172a;
  margin: 0;
}

.btn-primary {
  background: #5c60f5;
  color: #ffffff;
  border: none;
  padding: 8px 16px;
  border-radius: 8px;
  font-family: inherit;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 8px;
  white-space: nowrap;
  transition: all 0.2s ease-in-out;
}

.btn-primary:hover:not(:disabled) {
  background: #4b4fd6;
}

.btn-primary:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* Toolbar */
.toolbar {
  padding: 16px 24px;
  background: #f8fafc;
  border-bottom: 1px solid #e2e8f0;
  display: flex;
  gap: 12px;
  align-items: center;
  flex-wrap: wrap;
}

.search-box {
  display: flex;
  align-items: center;
  gap: 8px;
  background: #ffffff;
  padding: 8px 16px;
  border-radius: 8px;
  border: 1px solid #e2e8f0;
  width: 280px;
  transition: all 0.2s ease-in-out;
}

.search-box:focus-within {
  border-color: #5c60f5;
  box-shadow: 0 0 0 3px rgba(92, 96, 245, 0.1);
}

.search-box i {
  color: #94a3b8;
}

.search-box input {
  border: none;
  background: transparent;
  outline: none;
  font-family: inherit;
  font-size: 14px;
  color: #0f172a;
  width: 100%;
}

.filter-btn {
  background: #ffffff;
  border: 1px solid #e2e8f0;
  padding: 8px 16px;
  border-radius: 8px;
  font-family: inherit;
  font-size: 14px;
  font-weight: 600;
  color: #0f172a;
  cursor: pointer;
  outline: none;
  max-width: 220px;
  transition: all 0.2s ease-in-out;
}

.filter-btn:hover {
  background: #f1f5f9;
}

.toolbar-count {
  margin-left: auto;
  font-size: 13px;
  font-weight: 600;
  color: #64748b;
  white-space: nowrap;
}

/* Grid */
.grid-table {
  width: 100%;
  display: flex;
  flex-direction: column;
  overflow-x: auto;
}

.grid-row {
  display: grid;
  /* Услуга | Заказчик | Исполнитель | Сумма | Адрес | Завершен */
  grid-template-columns: minmax(200px, 1.2fr) 220px 220px 120px minmax(200px, 1.5fr) 130px;
  min-width: 1110px;
}

.grid-header {
  background: #ffffff;
  border-bottom: 2px solid #e2e8f0;
  font-size: 11px;
  font-weight: 800;
  color: #64748b;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

/* Header cells are buttons where the column can be sorted, so the rule has to
   reach any element, not just div. */
.grid-header > .th {
  padding: 16px 20px;
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  font: inherit;
  color: inherit;
  letter-spacing: inherit;
  text-transform: inherit;
  text-align: left;
  background: none;
  border: none;
}

.grid-header > .sortable {
  cursor: pointer;
  transition: color 0.2s ease-in-out;
}

.grid-header > .sortable:hover {
  color: #0f172a;
}

.grid-header .ph-caret-up-down {
  opacity: 0.4;
}

.grid-item {
  border-bottom: 1px solid #e2e8f0;
  transition: background 0.2s ease-in-out;
}

.grid-item:hover {
  background: #f8fafc;
}

.cell {
  padding: 16px 20px;
  display: flex;
  flex-direction: column;
  justify-content: center;
  overflow: hidden;
  min-width: 0;
}

/* Service and tags */
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

.badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  border-radius: 6px;
  font-size: 11px;
  font-weight: 800;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  white-space: nowrap;
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

/* Participants */
.phone-wrapper {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.phone-icon {
  width: 24px;
  height: 24px;
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  flex-shrink: 0;
}

.phone-icon.customer {
  background: #eef2ff;
  color: #5c60f5;
}

.phone-icon.executor {
  background: #fffbeb;
  color: #f59e0b;
}

.phone-number {
  font-size: 14px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.muted {
  color: #cbd5e1;
  font-weight: 700;
}

/* Amount */
.amount {
  font-weight: 900;
  font-size: 15px;
  white-space: nowrap;
}

.amount.free {
  color: #64748b;
  font-weight: 700;
}

/* Address */
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

/* Date */
.date-main {
  font-size: 14px;
  font-weight: 700;
  color: #0f172a;
}

.date-time {
  font-size: 12px;
  font-weight: 600;
  color: #64748b;
  margin-top: 2px;
}

.table-footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 16px;
  padding: 16px 20px;
  border-top: 1px solid #e2e8f0;
}

.page-info {
  font-size: 13px;
  font-weight: 600;
  color: #64748b;
}

.page-btn {
  background: #ffffff;
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  padding: 8px 14px;
  font-family: inherit;
  font-size: 13px;
  font-weight: 600;
  color: #0f172a;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 6px;
  transition: all 0.2s ease-in-out;
}

.page-btn:hover:not(:disabled) {
  background: #f1f5f9;
}

.page-btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.table-note {
  padding: 32px 24px;
  text-align: center;
  font-size: 14px;
  font-weight: 600;
  color: #64748b;
}

@media (max-width: 768px) {
  .page-header,
  .toolbar {
    padding: 16px;
  }
  .search-box,
  .filter-btn {
    width: 100%;
    max-width: none;
  }
  .toolbar-count {
    margin-left: 0;
  }
}
</style>
