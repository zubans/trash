<template>
  <va-modal
    v-model="show"
    :title="$t('customer.orderDetails')"
    hide-default-actions
  >
    <div v-if="selectedOrderDetails" class="p-2">
      <div class="d-flex justify-content-between align-items-center mb-3">
        <h4 class="va-h6 font-bold m-0">#{{ selectedOrderDetails.id }}</h4>
        <va-badge :color="getStatusColor(selectedOrderDetails.status)">
          {{ selectedOrderDetails.status }}
        </va-badge>
      </div>

      <div class="info-list bg-light p-3 rounded mb-3">
        <div class="info-item mb-2">
          <span class="text-secondary text-xs d-block">{{ $t('customer.orderType') }}</span>
          <span class="font-bold text-sm">{{ formatOrderType(selectedOrderDetails) }}</span>
        </div>
        <div class="info-item mb-2">
          <span class="text-secondary text-xs d-block">{{ $t('customer.created') }}</span>
          <span class="text-sm">{{ formatDateFull(selectedOrderDetails.created_at) }}</span>
        </div>
        <div v-if="selectedOrderDetails.assigned_at" class="info-item mb-2">
          <span class="text-secondary text-xs d-block">{{ $t('customer.assignedAt') }}</span>
          <span class="text-sm">{{ formatDateFull(selectedOrderDetails.assigned_at) }}</span>
        </div>
        <div v-if="selectedOrderDetails.completed_at" class="info-item mb-2">
          <span class="text-secondary text-xs d-block">{{ $t('customer.completedAt') }}</span>
          <span class="text-sm">{{ formatDateFull(selectedOrderDetails.completed_at) }}</span>
        </div>
        <div v-if="selectedOrderDetails.canceled_at" class="info-item mb-2">
          <span class="text-secondary text-xs d-block">{{ $t('customer.canceledAt') }}</span>
          <span class="text-sm">{{ formatDateFull(selectedOrderDetails.canceled_at) }}</span>
        </div>
        <div class="info-item mb-2">
          <span class="text-secondary text-xs d-block">{{ $t('customer.totalAmount') }}</span>
          <span class="font-bold text-primary text-base">
            {{ currencySymbol }}{{ Number(selectedOrderDetails.final_amount || selectedOrderDetails.hold_amount).toFixed(2) }}
          </span>
        </div>
        <div v-if="selectedOrderDetails.address" class="info-item mb-2">
          <span class="text-secondary text-xs d-block">{{ $t('customer.pickupAddress') }}</span>
          <span class="text-sm">{{ selectedOrderDetails.address }}</span>
        </div>
      </div>

      <!-- Executor details -->
      <div class="executor-card-box border p-3 rounded mb-3">
        <h5 class="va-h6 text-primary mb-2 text-xs uppercase tracking-wide font-bold">
          <va-icon name="person" class="mr-1" /> {{ $t('customer.executorDetails') }}
        </h5>
        <div v-if="selectedOrderDetails.executor_id || selectedOrderDetails.executor_phone">
          <div class="text-sm font-bold" v-if="selectedOrderDetails.executor_phone">
            📱 {{ selectedOrderDetails.executor_phone }}
          </div>
          <div class="text-xs text-secondary mt-1">
            ID: {{ selectedOrderDetails.executor_id }}
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
    selectedOrderDetails: { type: Object, default: null },
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
