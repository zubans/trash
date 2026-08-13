<template>
  <va-card class="shadow-card mb-4">
    <div
      class="d-flex justify-content-between align-items-center p-3 cursor-pointer user-select-none"
      @click="isCollapsed = !isCollapsed"
    >
      <h3 class="va-h6 m-0 text-secondary font-bold d-flex align-items-center">
        <va-icon name="history" class="mr-2" /> {{ $t('customer.orderHistory') }}
        <span class="text-xs text-secondary font-normal ml-2">({{ historyOrders.length }})</span>
      </h3>
      <va-button flat size="small" color="secondary">
        <va-icon :name="isCollapsed ? 'expand_more' : 'expand_less'" />
      </va-button>
    </div>

    <div v-if="!isCollapsed">
      <div v-if="historyOrders.length === 0" class="text-center py-4">
        <va-icon name="folder_off" size="medium" color="secondary" class="mb-2" />
        <p class="text-secondary text-sm m-0">{{ $t('customer.noHistoryOrders') }}</p>
      </div>

      <va-data-table
        v-else
        :items="historyOrders"
        :columns="columns"
        striped
        hoverable
      >
        <template #cell(id)="{ rowData }">
          <span class="font-bold text-sm cursor-pointer text-primary" @click="$emit('openDetails', rowData)">#{{ rowData.id.slice(0, 8) }}</span>
        </template>

        <template #cell(type)="{ rowData }">
          {{ formatOrderType(rowData) }}
        </template>

        <template #cell(hold_amount)="{ rowData }">
          <strong>{{ currencySymbol }}{{ Number(rowData.final_amount || rowData.hold_amount).toFixed(2) }}</strong>
        </template>

        <template #cell(status)="{ value }">
          <va-badge :color="getStatusColor(value)">{{ value }}</va-badge>
        </template>

        <template #cell(actions)="{ rowData }">
          <div class="d-flex gap-1">
            <va-button
              color="primary"
              flat
              size="small"
              @click="$emit('openDetails', rowData)"
            >
              <va-icon name="info" />
            </va-button>
          </div>
        </template>
      </va-data-table>
    </div>
  </va-card>
</template>

<script lang="ts">
import { defineComponent, ref } from 'vue'

export default defineComponent({
  name: 'OrderHistoryCard',
  props: {
    historyOrders: { type: Array as () => any[], default: () => [] },
    columns: { type: Array as () => any[], required: true },
    currencySymbol: { type: String, default: '₽' },
    formatOrderType: { type: Function, required: true },
    getStatusColor: { type: Function, required: true },
  },
  emits: ['openDetails'],
  setup() {
    const isCollapsed = ref(true)
    return { isCollapsed }
  },
})
</script>
