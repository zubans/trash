<template>
  <div class="transactions-history">
    <div class="admin-card">
      <!-- Шапка страницы -->
      <div class="page-header">
        <h1 class="page-title">{{ $t('transactions.title') }}</h1>
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

      <!-- Тулбар -->
      <div class="toolbar">
        <div class="search-box">
          <i class="ph-bold ph-magnifying-glass"></i>
          <input v-model="searchQuery" type="text" placeholder="Поиск по телефону или ID..." />
        </div>

        <select v-model="typeFilter" class="filter-btn">
          <option value="">Все типы</option>
          <option v-for="t in typeOptions" :key="t" :value="t">{{ typeLabel(t) }}</option>
        </select>

        <select v-model="periodFilter" class="filter-btn">
          <option value="">Все периоды</option>
          <option v-for="p in periodOptions" :key="p.value" :value="p.value">{{ p.label }}</option>
        </select>

        <span class="toolbar-count">{{ rangeLabel }}</span>
      </div>

      <!-- Таблица -->
      <div class="grid-table">
        <div class="grid-row grid-header">
          <button type="button" class="th sortable" @click="toggleSort('user')">
            {{ $t('transactions.userPhone') }} <i class="ph-bold" :class="sortIcon('user')"></i>
          </button>
          <button type="button" class="th sortable" @click="toggleSort('type')">
            {{ $t('transactions.type') }} <i class="ph-bold" :class="sortIcon('type')"></i>
          </button>
          <button type="button" class="th sortable" @click="toggleSort('amount')">
            {{ $t('transactions.amount') }} <i class="ph-bold" :class="sortIcon('amount')"></i>
          </button>
          <div class="th">Основание</div>
          <button type="button" class="th sortable" @click="toggleSort('created_at')">
            {{ $t('transactions.processedAt') }} <i class="ph-bold" :class="sortIcon('created_at')"></i>
          </button>
        </div>

        <div v-for="tx in transactions" :key="tx.id" class="grid-row grid-item">
          <div class="cell">
            <div class="phone-wrapper">
              <div class="phone-icon user"><i class="ph-bold ph-user"></i></div>
              <span class="phone-number">{{ tx.user_phone ? formatPhoneMask(tx.user_phone) : '—' }}</span>
            </div>
          </div>

          <div class="cell">
            <span class="badge" :class="directionClass(tx)">{{ typeLabel(tx.type) }}</span>
          </div>

          <div class="cell">
            <span class="amount" :class="directionClass(tx)">{{ amountLabel(tx) }}</span>
            <span v-if="tx.counterparty" class="counterparty">{{ tx.counterparty }}</span>
          </div>

          <div class="cell">
            <span v-if="tx.order_id" class="ref-main">
              <i class="ph-bold ph-package"></i> Заказ #{{ tx.order_id.slice(0, 8) }}
            </span>
            <span v-else-if="tx.admin_id" class="ref-main">
              <i class="ph-bold ph-user-gear"></i> Админ #{{ tx.admin_id.slice(0, 8) }}
            </span>
            <span v-else class="muted">—</span>
            <span class="ref-sub">#{{ tx.id.slice(0, 8) }}</span>
          </div>

          <div class="cell">
            <span class="date-main">{{ formatDay(tx.created_at) }}</span>
            <span class="date-time">{{ formatTime(tx.created_at) }}</span>
          </div>
        </div>

        <div v-if="loading" class="table-note">Загрузка…</div>
        <div v-else-if="transactions.length === 0 && hasFilters" class="table-note">
          Ничего не найдено по заданным фильтрам
        </div>
        <div v-else-if="transactions.length === 0" class="table-note">Проводок пока нет</div>
      </div>

      <!-- Пагинация -->
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

interface Transaction {
  id: string
  user_phone?: string
  order_id?: string
  admin_id?: string
  type: string
  amount?: number
  counterparty?: string
  /** +1 списание в плюс, -1 в минус, 0 — проводка баланс не двигает. */
  direction?: number
  created_at?: string
}

export default defineComponent({
  name: 'TransactionHistory',
  setup() {
    const authStore = useAuthStore()
    const transactions = ref<Transaction[]>([])
    const loading = ref(false)

    const searchQuery = ref('')
    const typeFilter = ref('')
    const periodFilter = ref('')
    const sortKey = ref('created_at')
    const sortDesc = ref(true)
    const page = ref(1)
    const total = ref(0)
    const exporting = ref(false)
    const typeOptions = ref<string[]>([])
    const periodKeys = ref<string[]>([])

    const PAGE_SIZE = 50
    const MAX_PAGE_SIZE = 200

    const currencySymbol = computed(() => (authStore.currency === 'RUB' ? '₽' : '$'))

    const TYPE_LABELS: Record<string, string> = {
      TOP_UP: 'Пополнение',
      HOLD: 'Удержание',
      PAYMENT: 'Оплата',
      REWARD: 'Вознаграждение',
      FINE: 'Штраф',
      REFUND: 'Возврат',
      WITHDRAWAL: 'Вывод',
      WITHDRAWAL_HOLD: 'Резерв вывода',
      WITHDRAWAL_PAID: 'Вывод выплачен',
      TIP: 'Чаевые',
      TIP_REWARD: 'Чаевые исполнителю',
      COMMISSION: 'Комиссия',
      COMMISSION_PAYOUT: 'Выплата комиссии',
      BONUS: 'Бонус',
    }

    const typeLabel = (type: string) => TYPE_LABELS[type] || type

    // Цвет идёт от направления проводки, а не от списка типов: соглашение о
    // знаках объявлено на бэкенде один раз и приезжает в поле direction.
    const directionClass = (tx: Transaction) => {
      if (!tx.direction) return 'neutral'
      return tx.direction > 0 ? 'credit' : 'debit'
    }

    const amountLabel = (tx: Transaction) => {
      const value = Number(tx.amount || 0).toFixed(2)
      const sign = tx.direction && tx.direction > 0 ? '+' : tx.direction && tx.direction < 0 ? '−' : ''
      return `${sign}${value} ${currencySymbol.value}`
    }

    const formatDay = (dateStr?: string) =>
      dateStr ? new Date(dateStr).toLocaleDateString('ru-RU') : '—'

    const formatTime = (dateStr?: string) =>
      dateStr ? new Date(dateStr).toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' }) : ''

    const monthNames = [
      'Январь', 'Февраль', 'Март', 'Апрель', 'Май', 'Июнь',
      'Июль', 'Август', 'Сентябрь', 'Октябрь', 'Ноябрь', 'Декабрь',
    ]

    const periodOptions = computed(() =>
      periodKeys.value.map((value) => {
        const [year, month] = value.split('-')
        return { value, label: `${monthNames[Number(month) - 1]} ${year}` }
      }),
    )

    const totalPages = computed(() => Math.max(1, Math.ceil(total.value / PAGE_SIZE)))

    const hasFilters = computed(
      () => Boolean(searchQuery.value.trim() || typeFilter.value || periodFilter.value),
    )

    const rangeLabel = computed(() => {
      if (total.value === 0) return 'Ничего не найдено'
      const from = (page.value - 1) * PAGE_SIZE + 1
      const to = Math.min(page.value * PAGE_SIZE, total.value)
      return `${from}–${to} из ${total.value}`
    })

    const queryParams = (limit: number, offset: number) => ({
      search: searchQuery.value.trim() || undefined,
      type: typeFilter.value || undefined,
      period: periodFilter.value || undefined,
      sort: sortKey.value,
      order: sortDesc.value ? 'desc' : 'asc',
      limit,
      offset,
    })

    const fetchTransactions = async () => {
      loading.value = true
      try {
        const response = await api.get('/admin/transactions', {
          params: queryParams(PAGE_SIZE, (page.value - 1) * PAGE_SIZE),
        })
        transactions.value = response.data?.transactions || []
        total.value = response.data?.total || 0
        typeOptions.value = response.data?.types || []
        periodKeys.value = response.data?.periods || []
      } catch (err) {
        console.error('Error fetching transactions:', err)
      } finally {
        loading.value = false
      }
    }

    // Фильтрация и сортировка идут в SQL, поэтому любое изменение возвращает на
    // первую страницу и перезапрашивает, а не тасует уже показанные строки.
    let searchTimer: ReturnType<typeof setTimeout> | undefined
    const reload = () => {
      page.value = 1
      fetchTransactions()
    }
    watch([typeFilter, periodFilter], reload)
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
      fetchTransactions()
    }

    const csvCell = (value: string) => `"${String(value ?? '').replace(/"/g, '""')}"`

    const exportCsv = async () => {
      if (exporting.value) return
      exporting.value = true
      try {
        // Выгрузка покрывает всю отфильтрованную выборку, поэтому обходит
        // страницы, а не сбрасывает 50 строк с экрана.
        const rows: Transaction[] = []
        let offset = 0
        do {
          const response = await api.get('/admin/transactions', {
            params: queryParams(MAX_PAGE_SIZE, offset),
          })
          const batch: Transaction[] = response.data?.transactions || []
          total.value = response.data?.total ?? total.value
          rows.push(...batch)
          if (batch.length < MAX_PAGE_SIZE) break
          offset += MAX_PAGE_SIZE
        } while (offset < total.value)

        const header = ['ID', 'Пользователь', 'Тип', 'Сумма', 'Направление', 'Системный счёт', 'ID заказа', 'ID админа', 'Обработано']
        const body = rows.map((tx) => [
          tx.id,
          tx.user_phone ? formatPhoneMask(tx.user_phone) : '',
          typeLabel(tx.type),
          Number(tx.amount || 0).toFixed(2),
          tx.direction && tx.direction > 0 ? 'приход' : tx.direction && tx.direction < 0 ? 'расход' : 'без движения',
          tx.counterparty || '',
          tx.order_id || '',
          tx.admin_id || '',
          tx.created_at ? `${formatDay(tx.created_at)} ${formatTime(tx.created_at)}` : '',
        ])
        // Точка с запятой и BOM: открывается в русском Excel без мастера импорта.
        const csv = [header, ...body].map((row) => row.map(csvCell).join(';')).join('\r\n')
        const blob = new Blob(['﻿' + csv], { type: 'text/csv;charset=utf-8;' })
        const url = URL.createObjectURL(blob)
        const link = document.createElement('a')
        link.href = url
        link.download = `transactions-${new Date().toISOString().slice(0, 10)}.csv`
        link.click()
        URL.revokeObjectURL(url)
      } catch (err) {
        console.error('Error exporting transactions:', err)
        alert('Не удалось выгрузить CSV')
      } finally {
        exporting.value = false
      }
    }

    onMounted(fetchTransactions)

    return {
      transactions,
      loading,
      searchQuery,
      typeFilter,
      periodFilter,
      typeOptions,
      periodOptions,
      total,
      page,
      totalPages,
      hasFilters,
      rangeLabel,
      exporting,
      currencySymbol,
      typeLabel,
      directionClass,
      amountLabel,
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
/* Общий вид карточки-таблицы живёт в styles/admin-table.css: он делится с
   выполненными заказами. Здесь остаётся только то, что есть на этой странице. */

.transactions-history {
  display: flex;
  flex-direction: column;
}

.grid-row {
  /* Пользователь | Тип | Сумма | Основание | Обработано */
  grid-template-columns: 220px 200px 180px minmax(200px, 1fr) 130px;
  min-width: 930px;
}

.phone-icon.user {
  background: #eef2ff;
  color: #5c60f5;
}

.badge.credit {
  background: #d1fae5;
  color: #047857;
}

.badge.debit {
  background: #fee2e2;
  color: #ef4444;
}

.badge.neutral {
  background: #f1f5f9;
  color: #64748b;
}

.amount.credit {
  color: #047857;
}

.amount.debit {
  color: #ef4444;
}

.amount.neutral {
  color: #64748b;
}

.counterparty {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.5px;
  color: #94a3b8;
  margin-top: 2px;
}

.ref-main {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 600;
  color: #0f172a;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.ref-sub {
  font-size: 12px;
  color: #94a3b8;
  margin-top: 2px;
}
</style>
