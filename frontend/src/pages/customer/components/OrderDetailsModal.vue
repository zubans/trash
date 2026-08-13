<template>
  <va-modal
    v-model="show"
    :title="$t('customer.orderDetails')"
    hide-default-actions
  >
    <div v-if="order" class="p-2">
      <div class="d-flex justify-content-between align-items-center mb-3">
        <h4 class="va-h6 font-bold m-0">#{{ order.id }}</h4>
        <va-badge :color="getStatusColor(order.status)">
          {{ order.status }}
        </va-badge>
      </div>

      <div class="info-list bg-light p-3 rounded mb-3">
        <div class="info-item mb-2">
          <span class="text-secondary text-xs d-block">{{ $t('customer.orderType') }}</span>
          <span class="font-bold text-sm">{{ formatOrderType(order) }}</span>
        </div>
        <div class="info-item mb-2">
          <span class="text-secondary text-xs d-block">{{ $t('customer.created') }}</span>
          <span class="text-sm">{{ formatDateFull(order.created_at) }}</span>
        </div>
        <div v-if="order.assigned_at" class="info-item mb-2">
          <span class="text-secondary text-xs d-block">{{ $t('customer.assignedAt') }}</span>
          <span class="text-sm">{{ formatDateFull(order.assigned_at) }}</span>
        </div>
        <div v-if="order.completed_at" class="info-item mb-2">
          <span class="text-secondary text-xs d-block">{{ $t('customer.completedAt') }}</span>
          <span class="text-sm">{{ formatDateFull(order.completed_at) }}</span>
        </div>
        <div v-if="order.canceled_at" class="info-item mb-2">
          <span class="text-secondary text-xs d-block">{{ $t('customer.canceledAt') }}</span>
          <span class="text-sm">{{ formatDateFull(order.canceled_at) }}</span>
        </div>
        <div class="info-item mb-2">
          <span class="text-secondary text-xs d-block">{{ $t('customer.totalAmount') }}</span>
          <span class="font-bold text-primary text-base">
            {{ currencySymbol }}{{ Number(order.final_amount || order.hold_amount).toFixed(2) }}
          </span>
        </div>
        <div v-if="order.address" class="info-item mb-2">
          <span class="text-secondary text-xs d-block">{{ $t('customer.pickupAddress') }}</span>
          <span class="text-sm">{{ order.address }}</span>
        </div>
      </div>

      <!-- Executor details -->
      <div class="executor-card-box border p-3 rounded mb-3">
        <h5 class="va-h6 text-primary mb-2 text-xs uppercase tracking-wide font-bold">
          <va-icon name="person" class="mr-1" /> {{ $t('customer.executorDetails') }}
        </h5>
        <div v-if="order.executor_id || order.executor_phone">
          <div class="text-sm font-bold" v-if="order.executor_phone">
            📱 {{ order.executor_phone }}
          </div>
          <div class="text-xs text-secondary mt-1">
            ID: {{ order.executor_id }}
          </div>
        </div>
        <div v-else class="text-xs text-secondary italic">
          {{ $t('customer.notAssigned') }}
        </div>
      </div>

      <div class="d-flex justify-content-end">
        <va-button color="secondary" @click="show = false">
          {{ $t('common.close') }}
        </va-button>
      </div>
    </div>
  </va-modal>
</template>

<script lang="ts">
import { defineComponent, computed } from 'vue'

export default defineComponent({
  name: 'OrderDetailsModal',
  props: {
    modelValue: { type: Boolean, required: true },
    order: { type: Object, default: null },
    currencySymbol: { type: String, default: '₽' },
    formatOrderType: { type: Function, required: true },
    getStatusColor: { type: Function, required: true },
    formatDateFull: { type: Function, required: true },
  },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    const show = computed({
      get: () => props.modelValue,
      set: (val) => emit('update:modelValue', val),
    })

    return { show }
  },
})
</script>
