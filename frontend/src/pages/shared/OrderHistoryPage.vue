<template>
  <div class="history-page">
    <div class="history-container">
      <div class="top-nav">
        <button type="button" class="btn-back" @click="goBack">
          <i class="ph-bold ph-arrow-left"></i>
          На главную
        </button>
        <h1 class="page-title">История заказов</h1>
      </div>

      <!-- Сводка: три числа без украшений. Пока грузится — не показываем
           нули, которые тут же сменятся настоящими числами. -->
      <div v-if="!loading" class="summary">
        <div class="summary-item">
          <span class="summary-value">{{ summary.completed }}</span>
          <span class="summary-label">выполнено</span>
        </div>
        <div class="summary-item">
          <span class="summary-value">{{ summary.canceled }}</span>
          <span class="summary-label">отменено</span>
        </div>
        <div class="summary-item">
          <span class="summary-value">{{ formatMoney(summary.completedAmount) }}</span>
          <span class="summary-label">{{ isExecutor ? 'по выполненным' : 'оплачено' }}</span>
        </div>
      </div>

      <div class="segmented" role="tablist">
        <button
          v-for="f in filters"
          :key="f.value"
          type="button"
          role="tab"
          class="segment"
          :class="{ active: filter === f.value }"
          :aria-selected="filter === f.value"
          @click="filter = f.value"
        >
          {{ f.label }}
        </button>
      </div>

      <SkeletonList v-if="loading" :rows="4" />

      <div v-else-if="loadError && !allOrders.length" class="state-note">
        Не удалось загрузить историю.
        <button type="button" class="link-btn" @click="reload">Повторить</button>
      </div>

      <div v-else-if="!groups.length" class="state-note">
        {{ emptyText }}
      </div>

      <template v-else>
        <section v-for="group in groups" :key="group.key" class="month">
          <h2 class="month-label">{{ group.label }}</h2>
          <div class="list">
            <button
              v-for="order in group.orders"
              :key="order.id"
              type="button"
              class="row"
              @click="openDetails(order)"
            >
              <div class="row-main">
                <div class="row-title">{{ titleOf(order).title }}</div>
                <div class="row-sub">
                  <span v-if="titleOf(order).subtitle">{{ titleOf(order).subtitle }} · </span>{{ formatClosedAt(order) }}
                </div>
              </div>
              <div class="row-side">
                <div class="row-amount" :class="{ muted: order.status === 'CANCELED' }">
                  {{ formatMoney(orderAmount(order)) }}
                </div>
                <div class="row-status">
                  <span v-if="reviews[order.id]" class="rating" title="Ваша оценка">
                    ★ {{ reviews[order.id].rating }}
                  </span>
                  <span :class="['status', order.status === 'COMPLETED' ? 'done' : 'canceled']">
                    {{ order.status === 'COMPLETED' ? 'Выполнен' : 'Отменён' }}
                  </span>
                </div>
              </div>
            </button>
          </div>
        </section>
      </template>
    </div>

    <OrderDetailsModal
      v-model="showDetails"
      :selected-order-details="selectedOrder"
      :currency-symbol="currencySymbol"
      :format-order-type="formatOrderType"
      :get-status-color="getStatusColor"
      :format-date-full="formatDateFull"
      @open-review-modal="openReview"
    />

    <ReviewModal
      v-model="showReview"
      :order-id="reviewOrderId"
      :role="role"
      :order-amount="reviewOrderAmount"
      :balance="authStore.balance ?? 0"
      :currency-symbol="currencySymbol"
      @reviewed="onReviewed"
    />
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, onMounted, PropType, ref } from 'vue'
import { useRouter } from 'vue-router'
import api from '../../services/api'
import { useAuthStore } from '../../stores/auth-store'
import { useCachedResource } from '../../composables/useCachedResource'
import { acceptPresentedOrders } from '../../components/order/cachedOrders'
import OrderDetailsModal from '../../components/order/OrderDetailsModal.vue'
import ReviewModal from '../customer/components/ReviewModal.vue'
import SkeletonList from '../../components/SkeletonList.vue'
import { checkMyOrderReview, type OrderReview } from '../../api/review'
import { orderTitle, orderTitleLine } from '../../utils/orderTitle'
import {
  closedAt,
  filterHistory,
  groupHistoryByMonth,
  orderAmount,
  summarizeHistory,
  type HistoryFilter,
  type HistoryOrder,
} from '../../utils/orderHistory'

type Role = 'EXECUTOR' | 'CUSTOMER'

const FILTERS: { value: HistoryFilter; label: string }[] = [
  { value: 'all', label: 'Все' },
  { value: 'completed', label: 'Выполненные' },
  { value: 'canceled', label: 'Отменённые' },
]

// История одна на обе роли: разный только источник заказов и куда вернуться.
const SOURCES: Record<Role, { key: string; url: string; home: string }> = {
  EXECUTOR: { key: 'executor:history:orders', url: '/executor/history', home: '/executor' },
  // У заказчика тот же запрос и тот же кэш, что у главного экрана: история
  // открывается сразу с уже известными заказами.
  CUSTOMER: { key: 'customer:orders', url: '/customer/orders', home: '/customer' },
}

export default defineComponent({
  name: 'OrderHistoryPage',
  components: { OrderDetailsModal, ReviewModal, SkeletonList },
  props: {
    role: { type: String as PropType<Role>, required: true },
  },
  setup(props) {
    const router = useRouter()
    const authStore = useAuthStore()
    const source = SOURCES[props.role] || SOURCES.CUSTOMER
    const isExecutor = props.role === 'EXECUTOR'

    const filter = ref<HistoryFilter>('all')
    const reviews = ref<Record<string, OrderReview>>({})

    const currencySymbol = computed(() => (authStore.currency === 'RUB' ? '₽' : '$'))

    // Оценки подгружаются после списка и только для выполненных: отменённый
    // заказ не оценивают.
    const loadReviews = async (orders: HistoryOrder[]) => {
      for (const order of orders) {
        if (order.status !== 'COMPLETED' || reviews.value[order.id]) continue
        try {
          const res = await checkMyOrderReview(order.id)
          if (res?.has_reviewed && res.review) reviews.value[order.id] = res.review
        } catch {
          /* оценка — украшение строки, без неё история всё равно читается */
        }
      }
    }

    const resource = useCachedResource<HistoryOrder[]>({
      key: source.key,
      initial: [],
      acceptCached: acceptPresentedOrders,
      fetcher: async () => {
        // Исполнителю приходит {orders, transactions}, заказчику — массив. Всё,
        // что не список, читается как пустая история, а не кладётся в кэш.
        const data = (await api.get(source.url)).data
        if (Array.isArray(data)) return data
        return Array.isArray(data?.orders) ? data.orders : []
      },
      onData: (orders) => loadReviews(orders),
    })

    const allOrders = resource.data
    const loading = resource.loading
    const loadError = resource.error

    const summary = computed(() => summarizeHistory(allOrders.value))
    const groups = computed(() => groupHistoryByMonth(filterHistory(allOrders.value, filter.value)))

    const emptyText = computed(() => {
      if (filter.value === 'completed') return 'Выполненных заказов пока нет.'
      if (filter.value === 'canceled') return 'Отменённых заказов нет.'
      return 'Здесь появятся выполненные и отменённые заказы.'
    })

    const titleOf = (order: HistoryOrder) => orderTitle(order)
    const formatOrderType = (order: HistoryOrder) => orderTitleLine(order)

    const formatMoney = (value: number) =>
      `${value.toLocaleString('ru-RU', { minimumFractionDigits: 0, maximumFractionDigits: 2 })} ${currencySymbol.value}`

    const formatClosedAt = (order: HistoryOrder) => {
      const value = closedAt(order)
      if (!value) return ''
      return new Date(value).toLocaleString('ru-RU', {
        day: 'numeric',
        month: 'short',
        hour: '2-digit',
        minute: '2-digit',
      })
    }

    const formatDateFull = (value: string) =>
      value ? new Date(value).toLocaleString('ru-RU', { dateStyle: 'medium', timeStyle: 'short' }) : ''

    const getStatusColor = (status: string) => {
      switch (status) {
        case 'COMPLETED': return '#16a34a'
        case 'CANCELED': return '#94a3b8'
        default: return '#64748b'
      }
    }

    const showDetails = ref(false)
    const selectedOrder = ref<HistoryOrder | undefined>(undefined)
    const openDetails = (order: HistoryOrder) => {
      selectedOrder.value = order
      showDetails.value = true
    }

    const showReview = ref(false)
    const reviewOrderId = ref('')
    const reviewOrderAmount = ref(0)
    const openReview = (order: HistoryOrder) => {
      reviewOrderId.value = order.id
      reviewOrderAmount.value = orderAmount(order)
      showDetails.value = false
      showReview.value = true
    }

    const onReviewed = (payload?: { tipped?: boolean }) => {
      showReview.value = false
      delete reviews.value[reviewOrderId.value]
      // Отзыв меняет действия в карточке заказа, чаевые — баланс.
      resource.reload()
      if (payload?.tipped) authStore.fetchMe()
    }

    const reload = () => resource.reload()
    const goBack = () => router.push(source.home)

    onMounted(() => resource.load())

    return {
      authStore,
      isExecutor,
      filters: FILTERS,
      filter,
      reviews,
      currencySymbol,
      allOrders,
      loading,
      loadError,
      summary,
      groups,
      emptyText,
      titleOf,
      formatOrderType,
      formatMoney,
      formatClosedAt,
      formatDateFull,
      getStatusColor,
      orderAmount,
      showDetails,
      selectedOrder,
      openDetails,
      showReview,
      reviewOrderId,
      reviewOrderAmount,
      openReview,
      onReviewed,
      reload,
      goBack,
    }
  },
})
</script>

<style scoped>
.history-page {
  min-height: 100vh;
  background: #f6f7fb;
  padding: 16px;
  padding-bottom: calc(24px + env(safe-area-inset-bottom));
}

.history-container {
  max-width: 640px;
  margin: 0 auto;
}

.top-nav {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
}

.btn-back {
  border: none;
  background: #fff;
  border-radius: 10px;
  padding: 8px 12px;
  font-size: 14px;
  color: #475569;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.page-title {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
  color: #0f172a;
}

.summary {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  background: #fff;
  border-radius: 14px;
  padding: 14px 4px;
  margin-bottom: 12px;
}

.summary-item {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 2px;
  min-width: 0;
}

.summary-item + .summary-item {
  border-left: 1px solid #f1f5f9;
}

.summary-value {
  font-size: 17px;
  font-weight: 600;
  color: #0f172a;
  white-space: nowrap;
}

.summary-label {
  font-size: 12px;
  color: #94a3b8;
}

.segmented {
  display: flex;
  background: #eceef3;
  border-radius: 10px;
  padding: 3px;
  margin-bottom: 16px;
}

.segment {
  flex: 1;
  border: none;
  background: transparent;
  border-radius: 8px;
  padding: 7px 0;
  font-size: 13px;
  font-weight: 500;
  color: #64748b;
  cursor: pointer;
  font-family: inherit;
}

.segment.active {
  background: #fff;
  color: #0f172a;
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.08);
}

.month + .month {
  margin-top: 18px;
}

.month-label {
  margin: 0 0 8px 4px;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: #94a3b8;
}

.list {
  background: #fff;
  border-radius: 14px;
  overflow: hidden;
}

.row {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 14px;
  border: none;
  background: transparent;
  text-align: left;
  cursor: pointer;
  font-family: inherit;
}

.row + .row {
  border-top: 1px solid #f1f5f9;
}

.row:active {
  background: #f8fafc;
}

.row-main {
  min-width: 0;
}

.row-title {
  font-size: 15px;
  font-weight: 500;
  color: #0f172a;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.row-sub {
  margin-top: 2px;
  font-size: 13px;
  color: #94a3b8;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.row-side {
  flex-shrink: 0;
  text-align: right;
}

.row-amount {
  font-size: 15px;
  font-weight: 600;
  color: #0f172a;
  white-space: nowrap;
}

.row-amount.muted {
  color: #94a3b8;
  font-weight: 500;
}

.row-status {
  margin-top: 2px;
  font-size: 12px;
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  white-space: nowrap;
}

.status.done {
  color: #16a34a;
}

.status.canceled {
  color: #94a3b8;
}

.rating {
  color: #b45309;
}

.state-note {
  background: #fff;
  border-radius: 14px;
  padding: 28px 16px;
  text-align: center;
  font-size: 14px;
  color: #64748b;
}

.link-btn {
  border: none;
  background: none;
  color: #4f46e5;
  font: inherit;
  cursor: pointer;
  padding: 0 0 0 4px;
}
</style>
