<template>
  <div class="card shadow-sm border-0 rounded-2xl mb-4 bg-white overflow-hidden">
    <!-- Header with Toggle Button -->
    <div
      class="d-flex justify-content-between align-items-center p-4 cursor-pointer user-select-none border-bottom"
      @click="isHistoryCollapsed = !isHistoryCollapsed"
    >
      <div class="d-flex align-items-center gap-3">
        <h3 class="va-h6 m-0 font-bold text-dark">
          История заказов
        </h3>
        <span class="badge rounded-pill bg-primary-subtle text-primary font-bold px-3 py-1 text-xs">
          {{ historyOrders.length }}
        </span>
      </div>

      <button type="button" class="btn-toggle-collapse border-0 bg-transparent text-secondary font-medium text-xs d-flex align-items-center gap-1 cursor-pointer">
        <span>{{ isHistoryCollapsed ? 'Развернуть' : 'Свернуть' }}</span>
        <va-icon :name="isHistoryCollapsed ? 'expand_more' : 'expand_less'" size="small" />
      </button>
    </div>

    <!-- Collapsible Body -->
    <div v-if="!isHistoryCollapsed" class="p-4 pt-3">
      <!-- Summary Badges Bar -->
      <div class="summary-badges-row d-flex gap-3 mb-4 flex-wrap">
        <div class="summary-pill-card p-3 rounded-xl border d-flex align-items-center gap-3 bg-light-green">
          <span class="icon-circle bg-success text-white">✓</span>
          <div>
            <span class="d-block text-secondary text-xs font-medium">Завершённые</span>
            <span class="font-bold text-dark text-base">{{ completedCount }}</span>
          </div>
        </div>

        <div class="summary-pill-card p-3 rounded-xl border d-flex align-items-center gap-3 bg-light-red">
          <span class="icon-circle bg-danger text-white">✕</span>
          <div>
            <span class="d-block text-secondary text-xs font-medium">Отменённые</span>
            <span class="font-bold text-dark text-base">{{ canceledCount }}</span>
          </div>
        </div>

        <div class="summary-pill-card p-3 rounded-xl border d-flex align-items-center gap-3 bg-light-blue">
          <span class="icon-circle bg-primary text-white">📋</span>
          <div>
            <span class="d-block text-secondary text-xs font-medium">Всего заказов</span>
            <span class="font-bold text-dark text-base">{{ historyOrders.length }}</span>
          </div>
        </div>
      </div>

      <!-- Empty State -->
      <div v-if="historyOrders.length === 0" class="text-center py-5 text-secondary">
        <va-icon name="folder_off" size="large" color="secondary" class="mb-2" />
        <p class="text-secondary text-sm m-0">{{ $t('customer.noHistoryOrders') }}</p>
      </div>

      <!-- Table -->
      <div v-else class="table-responsive">
        <table class="table align-middle table-hover text-sm m-0">
          <thead>
            <tr class="text-secondary text-xs uppercase tracking-wider">
              <th>№ Заказа</th>
              <th>Тип заказа</th>
              <th>Цена</th>
              <th>Дата</th>
              <th>Статус</th>
              <th>Исполнитель</th>
              <th class="text-end">Действия</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="order in displayedOrders" :key="order.id">
              <td class="font-bold text-primary cursor-pointer" @click="$emit('openOrderDetails', order)">
                #{{ order.id.slice(0, 8) }}
              </td>
              <td>{{ formatOrderType(order) }}</td>
              <td class="font-bold text-dark">{{ currencySymbol }}{{ Number(order.final_amount || order.hold_amount).toFixed(2) }}</td>
              <td class="text-secondary text-xs">{{ formatDate(order.created_at) }}</td>
              <td>
                <div class="d-flex align-items-center gap-1 flex-wrap">
                  <span :class="['status-dot-badge', order.status === 'COMPLETED' ? 'dot-completed' : 'dot-canceled']">
                    ● {{ order.status === 'COMPLETED' ? 'Завершён' : 'Отменён' }}
                  </span>
                  <span v-if="orderReviewsMap[order.id]" class="review-pill-badge" title="Оценка вычислена">
                    ⭐ {{ orderReviewsMap[order.id].rating }}/5
                  </span>
                </div>
              </td>
              <td class="text-secondary text-xs">
                <span v-if="order.executor_phone">📱 {{ order.executor_phone }}</span>
                <span v-else-if="order.executor_id">ID: #{{ order.executor_id.slice(0, 8) }}</span>
                <span v-else class="italic">—</span>
              </td>
              <td class="text-end">
                <button
                  type="button"
                  class="btn-table-action"
                  @click="$emit('openOrderDetails', order)"
                >
                  👁️ Подробнее
                </button>
              </td>
            </tr>
          </tbody>
        </table>

        <!-- Show More Button -->
        <div v-if="historyOrders.length > visibleLimit" class="text-center mt-3">
          <button type="button" class="btn-show-more" @click="visibleLimit += 5">
            Показать ещё ∨
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, computed, watch } from 'vue'
import { checkMyOrderReview, type OrderReview } from '../../../api/review'

export default defineComponent({
  name: 'OrderHistoryCard',
  props: {
    historyOrders: { type: Array as () => any[], default: () => [] },
    orderColumns: { type: Array as () => any[], required: true },
    currencySymbol: { type: String, default: '₽' },
    formatOrderType: { type: Function, required: true },
    getStatusColor: { type: Function, required: true },
  },
  emits: ['openOrderDetails'],
  setup(props) {
    const isHistoryCollapsed = ref(true)
    const visibleLimit = ref(5)
    const orderReviewsMap = ref<Record<string, OrderReview>>({})

    const completedCount = computed(() =>
      props.historyOrders.filter((o) => o.status === 'COMPLETED').length
    )

    const canceledCount = computed(() =>
      props.historyOrders.filter((o) => o.status === 'CANCELED').length
    )

    const displayedOrders = computed(() =>
      props.historyOrders.slice(0, visibleLimit.value)
    )

    const fetchReviews = async () => {
      const completed = props.historyOrders.filter((o) => o.status === 'COMPLETED')
      for (const order of completed) {
        if (!orderReviewsMap.value[order.id]) {
          try {
            const res = await checkMyOrderReview(order.id)
            if (res && res.has_reviewed && res.review) {
              orderReviewsMap.value[order.id] = res.review
            }
          } catch (err) {
            // ignore
          }
        }
      }
    }

    watch(
      () => props.historyOrders,
      () => { fetchReviews() },
      { immediate: true, deep: true }
    )

    const formatDate = (dateStr?: string) => {
      if (!dateStr) return ''
      const d = new Date(dateStr)
      return d.toLocaleDateString('ru-RU', { day: 'numeric', month: 'short', year: 'numeric' })
    }

    return {
      isHistoryCollapsed,
      visibleLimit,
      orderReviewsMap,
      completedCount,
      canceledCount,
      displayedOrders,
      formatDate,
    }
  },
})
</script>

<style scoped>
.bg-primary-subtle {
  background: #eff6ff;
  color: #2563eb;
}

.summary-pill-card {
  min-width: 150px;
  flex: 1;
  border-radius: 12px;
}

.bg-light-green {
  background: #f0fdf4;
  border-color: #dcfce7 !important;
}

.bg-light-red {
  background: #fef2f2;
  border-color: #fee2e2 !important;
}

.bg-light-blue {
  background: #f8fafc;
  border-color: #e2e8f0 !important;
}

.icon-circle {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  font-size: 12px;
  font-weight: bold;
  display: flex;
  align-items: center;
  justify-content: center;
}

.table th {
  border-bottom: 1px solid #f1f5f9;
  font-weight: 600;
  padding: 12px;
}

.table td {
  padding: 14px 12px;
  border-bottom: 1px solid #f8fafc;
}

.status-dot-badge {
  font-size: 0.8rem;
  font-weight: 600;
}

.dot-completed {
  color: #16a34a;
}

.dot-canceled {
  color: #dc2626;
}

.review-pill-badge {
  font-size: 0.72rem;
  color: #92400e;
  background: #fef3c7;
  border: 1px solid #fde68a;
  border-radius: 9999px;
  padding: 1px 6px;
  font-weight: 600;
  white-space: nowrap;
}

.btn-table-action {
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  border-radius: 6px;
  padding: 4px 10px;
  font-size: 0.8rem;
  color: #475569;
  cursor: pointer;
  transition: all 0.15s ease;
}

.btn-table-action:hover {
  background: #f1f5f9;
  color: #0f172a;
}

.btn-show-more {
  background: transparent;
  border: none;
  color: #2563eb;
  font-size: 0.85rem;
  font-weight: 600;
  cursor: pointer;
}

.btn-show-more:hover {
  text-decoration: underline;
}
</style>
