<template>
  <div class="card shadow-sm border-0 rounded-2xl mb-4 bg-white overflow-hidden">
    <!-- Header with Toggle Button -->
    <div
      class="d-flex justify-content-between align-items-center p-4 cursor-pointer user-select-none border-bottom"
      @click="isHistoryCollapsed = !isHistoryCollapsed"
    >
      <div class="d-flex align-items-center gap-2">
        <span class="text-primary font-bold">🕒 История заказов ({{ historyOrders.length }})</span>
      </div>

      <button type="button" class="btn-toggle border-0 bg-transparent text-secondary font-medium text-xs d-flex align-items-center gap-1 cursor-pointer">
        <va-icon :name="isHistoryCollapsed ? 'expand_more' : 'expand_less'" size="small" />
      </button>
    </div>

    <!-- Collapsible Body -->
    <div v-if="!isHistoryCollapsed" class="p-4 pt-3">
      <div v-if="historyOrders.length === 0" class="text-center py-4 text-secondary">
        <p class="text-secondary text-sm m-0">{{ $t('customer.noHistoryOrders') }}</p>
      </div>

      <div v-else class="table-responsive">
        <table class="table align-middle text-sm m-0">
          <thead>
            <tr class="text-secondary text-xs uppercase tracking-wider">
              <th>ID</th>
              <th>ТИП ЗАКАЗА</th>
              <th>ЦЕНА</th>
              <th>СТАТУС</th>
              <th class="text-end">УПРАВЛЕНИЕ</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="order in historyOrders" :key="order.id" class="opacity-75">
              <td class="font-bold text-secondary cursor-pointer" @click="$emit('openOrderDetails', order)">
                #{{ order.id.slice(0, 8) }}
              </td>
              <td class="text-secondary">{{ formatOrderType(order) }}</td>
              <td class="font-bold text-secondary">{{ Number(order.final_amount || order.hold_amount).toFixed(2) }} ₽</td>
              <td class="text-secondary uppercase font-bold text-xs">{{ order.status === 'COMPLETED' ? 'НАЗНАЧЕН' : 'ОТМЕНЕН' }}</td>
              <td class="text-end">
                <div class="d-flex align-items-center justify-content-end gap-1">
                  <button type="button" class="btn-round-gray" @click="$emit('openOrderDetails', order)">ℹ</button>
                  <button type="button" class="btn-round-gray">💬</button>
                  <button type="button" class="btn-round-gray text-success">✓</button>
                  <button type="button" class="btn-round-gray text-danger">✕</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref } from 'vue'

export default defineComponent({
  name: 'OrderHistoryCardV2',
  props: {
    historyOrders: { type: Array as () => any[], default: () => [] },
    orderColumns: { type: Array as () => any[], required: true },
    currencySymbol: { type: String, default: '₽' },
    formatOrderType: { type: Function, required: true },
    getStatusColor: { type: Function, required: true },
  },
  emits: ['openOrderDetails'],
  setup() {
    const isHistoryCollapsed = ref(true)
    return { isHistoryCollapsed }
  },
})
</script>

<style scoped>
.btn-round-gray {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  border: 1px solid #cbd5e0;
  background: #f1f5f9;
  color: #64748b;
  font-size: 0.75rem;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}
</style>
