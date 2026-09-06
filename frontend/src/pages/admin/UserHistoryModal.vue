<template>
  <va-modal
    :model-value="modelValue"
    :title="title"
    size="large"
    hide-default-actions
    @update:model-value="$emit('update:modelValue', $event)"
  >
    <div class="history">
      <p class="history-sub">
        {{ user?.phone }}
        <span v-if="fullName"> · {{ fullName }}</span>
      </p>

      <!-- Две вкладки в одном окне: администратор, открывший проводки, почти
           всегда следом смотрит заказы, и закрывать окно ради этого незачем. -->
      <div class="tabs">
        <button
          type="button"
          class="tab"
          :class="{ active: tab === 'transactions' }"
          @click="switchTo('transactions')"
        >
          Проводки<span v-if="tab === 'transactions' && total"> · {{ total }}</span>
        </button>
        <button
          type="button"
          class="tab"
          :class="{ active: tab === 'orders' }"
          @click="switchTo('orders')"
        >
          Заказы<span v-if="tab === 'orders' && total"> · {{ total }}</span>
        </button>
      </div>

      <p v-if="errorMsg" class="alert error">{{ errorMsg }}</p>

      <div class="table-scroll">
        <table v-if="tab === 'transactions'" class="history-table">
          <thead>
            <tr>
              <th>Дата</th>
              <th>Тип</th>
              <th class="num">Сумма</th>
              <th>Заказ</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="tx in transactions" :key="tx.id">
              <td class="nowrap">{{ formatDate(tx.created_at) }}</td>
              <td>{{ typeLabel(tx.type) }}</td>
              <!-- Знак берётся из direction, посчитанного на сервере: суммы в
                   таблице все положительные, направление живёт в типе. -->
              <td class="num" :class="signClass(tx.direction)">
                {{ formatSigned(tx.amount, tx.direction) }}
              </td>
              <td class="mono">{{ tx.order_id ? tx.order_id.slice(0, 8) : '—' }}</td>
            </tr>
            <tr v-if="!loading && !transactions.length">
              <td colspan="4" class="empty">Проводок нет.</td>
            </tr>
          </tbody>
        </table>

        <table v-else class="history-table">
          <thead>
            <tr>
              <th>Дата</th>
              <th>Услуга</th>
              <th>Роль</th>
              <th>Статус</th>
              <th class="num">Сумма</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="order in orders" :key="order.id">
              <td class="nowrap">{{ formatDate(order.created_at) }}</td>
              <td>
                {{ order.service_variant_name }}
                <div v-if="order.address" class="muted">{{ order.address }}</div>
              </td>
              <td>{{ roleIn(order) }}</td>
              <td>{{ statusLabel(order.status) }}</td>
              <td class="num">{{ formatAmount(order.final_amount ?? order.hold_amount) }}</td>
            </tr>
            <tr v-if="!loading && !orders.length">
              <td colspan="5" class="empty">Заказов нет.</td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="history-foot">
        <span class="muted">
          <template v-if="total">Показано {{ shown }} из {{ total }}</template>
        </span>
        <div class="foot-actions">
          <button
            v-if="shown < total"
            type="button"
            class="btn-more"
            :disabled="loading"
            @click="loadMore"
          >
            Показать ещё
          </button>
          <button type="button" class="btn-close" @click="$emit('update:modelValue', false)">
            Закрыть
          </button>
        </div>
      </div>
    </div>
  </va-modal>
</template>

<script lang="ts">
import { computed, defineComponent, PropType, ref, watch } from 'vue'
import {
  getUserOrders,
  getUserTransactions,
  type UserOrder,
  type UserTransaction,
} from '../../api/user-history'

const PAGE_SIZE = 20

const TYPE_LABELS: Record<string, string> = {
  TOP_UP: 'Пополнение',
  WITHDRAWAL: 'Вывод',
  WITHDRAWAL_HOLD: 'Вывод: удержание',
  WITHDRAWAL_PAID: 'Вывод: выплата',
  HOLD: 'Удержание по заказу',
  PAYMENT: 'Оплата заказа',
  REWARD: 'Вознаграждение',
  REFUND: 'Возврат',
  FINE: 'Штраф',
  TIP: 'Чаевые',
  TIP_REWARD: 'Чаевые исполнителю',
  COMMISSION: 'Комиссия платформы',
  BONUS: 'Бонус',
}

const STATUS_LABELS: Record<string, string> = {
  SEARCHING: 'в поиске',
  ASSIGNED: 'у исполнителя',
  EXECUTED: 'выполнен',
  COMPLETED: 'завершён',
  CANCELED: 'отменён',
}

export default defineComponent({
  name: 'UserHistoryModal',
  props: {
    modelValue: { type: Boolean, default: false },
    user: { type: Object as PropType<any | null>, default: null },
    // С какой вкладки открыть: пункты меню на карточке ведут каждый на свою.
    initialTab: { type: String as PropType<'transactions' | 'orders'>, default: 'transactions' },
  },
  emits: ['update:modelValue'],
  setup(props) {
    const tab = ref<'transactions' | 'orders'>(props.initialTab)
    const transactions = ref<UserTransaction[]>([])
    const orders = ref<UserOrder[]>([])
    const total = ref(0)
    const loading = ref(false)
    const errorMsg = ref('')

    const shown = computed(() =>
      tab.value === 'transactions' ? transactions.value.length : orders.value.length,
    )

    const fullName = computed(() => {
      const u = props.user
      if (!u) return ''
      return [u.last_name, u.first_name, u.patronymic].filter(Boolean).join(' ')
    })

    const title = computed(() =>
      tab.value === 'transactions' ? 'История проводок' : 'История заказов',
    )

    const load = async (append = false) => {
      if (!props.user) return
      loading.value = true
      errorMsg.value = ''
      try {
        const offset = append ? shown.value : 0
        if (tab.value === 'transactions') {
          const res = await getUserTransactions(props.user.id, { limit: PAGE_SIZE, offset })
          transactions.value = append ? [...transactions.value, ...res.transactions] : res.transactions
          total.value = res.total
        } else {
          const res = await getUserOrders(props.user.id, { limit: PAGE_SIZE, offset })
          orders.value = append ? [...orders.value, ...res.orders] : res.orders
          total.value = res.total
        }
      } catch (err: any) {
        const data = err?.response?.data
        errorMsg.value =
          (typeof data === 'string' ? data : data?.error) || 'Не удалось загрузить историю'
      } finally {
        loading.value = false
      }
    }

    const switchTo = (next: 'transactions' | 'orders') => {
      if (tab.value === next) return
      tab.value = next
      total.value = 0
      load()
    }

    const loadMore = () => load(true)

    // Окно переиспользуется между пользователями, поэтому при каждом открытии
    // списки сбрасываются: иначе на новом пользователе на мгновение видна чужая
    // история.
    watch(
      () => [props.modelValue, props.user?.id] as const,
      ([open]) => {
        if (!open) return
        tab.value = props.initialTab
        transactions.value = []
        orders.value = []
        total.value = 0
        load()
      },
    )

    const formatDate = (value: string) =>
      new Date(value).toLocaleString('ru-RU', { dateStyle: 'short', timeStyle: 'short' })

    const formatAmount = (value: number) => `${Number(value ?? 0).toFixed(2)} ₽`

    const formatSigned = (value: number, direction: number) => {
      const sign = direction > 0 ? '+' : direction < 0 ? '−' : ''
      return `${sign}${Number(value ?? 0).toFixed(2)} ₽`
    }

    const signClass = (direction: number) =>
      direction > 0 ? 'positive' : direction < 0 ? 'negative' : ''

    const typeLabel = (type: string) => TYPE_LABELS[type] || type
    const statusLabel = (status: string) => STATUS_LABELS[status] || status

    // Один и тот же человек мог быть в заказе и заказчиком, и исполнителем —
    // лента общая, поэтому роль подписывается у каждой строки.
    const roleIn = (order: UserOrder) => {
      if (!props.user) return ''
      if (order.customer_id === props.user.id) return 'заказчик'
      if (order.executor_id === props.user.id) return 'исполнитель'
      return '—'
    }

    return {
      tab,
      transactions,
      orders,
      total,
      shown,
      loading,
      errorMsg,
      fullName,
      title,
      switchTo,
      loadMore,
      formatDate,
      formatAmount,
      formatSigned,
      signClass,
      typeLabel,
      statusLabel,
      roleIn,
    }
  },
})
</script>

<style scoped>
.history-sub {
  color: #6b7280;
  font-size: 13px;
  margin: 0 0 12px;
}

.tabs {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}

.tab {
  border: 1px solid #e5e7eb;
  background: #fff;
  border-radius: 10px;
  padding: 6px 14px;
  font-size: 13px;
  font-weight: 600;
  color: #475569;
  cursor: pointer;
}

.tab.active {
  background: #111827;
  border-color: #111827;
  color: #fff;
}

.alert {
  padding: 10px 12px;
  border-radius: 10px;
  font-size: 13px;
  margin-bottom: 10px;
}

.alert.error {
  background: #fef2f2;
  color: #b91c1c;
}

/* История длиннее экрана — прокручивается таблица, а не всё окно. */
.table-scroll {
  max-height: 55vh;
  overflow: auto;
}

.history-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.history-table th {
  text-align: left;
  color: #6b7280;
  font-weight: 500;
  padding: 8px 10px;
  border-bottom: 1px solid #eef0f4;
  position: sticky;
  top: 0;
  background: #fff;
}

.history-table td {
  padding: 9px 10px;
  border-bottom: 1px solid #f5f6f8;
  vertical-align: top;
}

.history-table .num {
  text-align: right;
  white-space: nowrap;
}

.nowrap {
  white-space: nowrap;
}

.positive {
  color: #047857;
  font-weight: 600;
}

.negative {
  color: #b91c1c;
  font-weight: 600;
}

.mono {
  font-family: ui-monospace, monospace;
}

.muted {
  color: #9ca3af;
  font-size: 12px;
}

.empty {
  color: #9ca3af;
  text-align: center;
  padding: 18px;
}

.history-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-top: 12px;
  flex-wrap: wrap;
}

.foot-actions {
  display: flex;
  gap: 8px;
}

.btn-more,
.btn-close {
  border: 1px solid #e5e7eb;
  background: #fff;
  border-radius: 10px;
  padding: 7px 14px;
  font-size: 13px;
  cursor: pointer;
}

.btn-close {
  background: #111827;
  border-color: #111827;
  color: #fff;
}

.btn-more:disabled {
  opacity: 0.5;
  cursor: default;
}
</style>
