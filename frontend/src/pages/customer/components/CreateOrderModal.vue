<template>
  <va-modal
    v-model="show"
    hide-default-actions
    max-width="500px"
  >
    <template #header>
      <div class="d-flex justify-content-between align-items-center w-100 p-3 pb-0">
        <h3 class="va-h5 m-0 font-bold">{{ $t('customer.createNewOrder') }}</h3>
        <button type="button" class="btn-close-modal border-0 bg-transparent text-secondary cursor-pointer" @click="show = false">
          ✕
        </button>
      </div>
    </template>

    <div class="p-2">
      <div class="mb-4">
        <div class="text-secondary text-sm mb-2">
          {{ $t('customer.pickupAddress') }}
        </div>
        <div class="font-medium mb-2">{{ orderAddress }}</div>
        <div class="text-secondary text-xs">
          {{ $t('customer.addressChangeHint') }}
        </div>
        <div v-if="orderLat !== null && orderLon !== null" class="text-secondary text-xs mt-2">
          {{ $t('customer.coordinates') }}: {{ orderLat.toFixed(5) }}, {{ orderLon.toFixed(5) }}
        </div>
        <div v-if="geocodeError" class="text-danger text-xs mt-2">
          {{ geocodeError }}
        </div>
      </div>

      <div class="mb-4">
        <va-select
          :model-value="selectedCategoryId"
          @update:model-value="$emit('update:selectedCategoryId', $event)"
          :options="categoryOptions"
          :label="$t('customer.category')"
          text-by="label"
          value-by="value"
          track-by="value"
          class="mb-2"
        />
        <va-select
          v-if="subCategoryOptions.length > 0"
          :model-value="selectedSubCategoryId"
          @update:model-value="$emit('update:selectedSubCategoryId', $event)"
          :options="subCategoryOptions"
          :label="$t('customer.subCategory')"
          text-by="label"
          value-by="value"
          track-by="value"
          class="mb-2"
        />
        <va-select
          v-if="variantOptions.length > 0"
          :model-value="selectedVariantId"
          @update:model-value="$emit('update:selectedVariantId', $event)"
          :options="variantOptions"
          :label="$t('customer.serviceVariant')"
          text-by="label"
          value-by="value"
          track-by="value"
          class="mb-2"
        />
        <div v-if="!isAuctionSelected" class="d-flex gap-2 mt-2">
          <va-checkbox
            :model-value="isUrgent"
            @update:model-value="$emit('update:isUrgent', $event)"
            :label="$t('customer.urgent')"
          />
          <va-checkbox
            :model-value="isAsap"
            @update:model-value="$emit('update:isAsap', $event)"
            :label="$t('customer.asap')"
          />
        </div>
        <div class="text-secondary text-sm mt-2">
          {{ $t('customer.price') }}: <strong class="text-primary">{{ currencySymbol }}{{ Number(selectedPrice).toFixed(2) }}</strong>
        </div>
      </div>

      <div class="d-flex gap-2">
        <va-button color="secondary" outline block @click="show = false">
          {{ $t('common.cancel') }}
        </va-button>
        <va-button color="success" block :loading="creatingOrder" @click="$emit('submitOrder')">
          {{ $t('customer.createOrder') }}
        </va-button>
      </div>
    </div>
  </va-modal>
</template>

<script lang="ts">
import { defineComponent, computed } from 'vue'

export default defineComponent({
  name: 'CreateOrderModal',
  props: {
    modelValue: { type: Boolean, required: true },
    orderAddress: { type: String, default: '' },
    orderLat: { type: Number, default: null },
    orderLon: { type: Number, default: null },
    geocodeError: { type: String, default: '' },
    selectedCategoryId: { type: String, default: '' },
    selectedSubCategoryId: { type: String, default: '' },
    selectedVariantId: { type: String, default: '' },
    categoryOptions: { type: Array as () => any[], default: () => [] },
    subCategoryOptions: { type: Array as () => any[], default: () => [] },
    variantOptions: { type: Array as () => any[], default: () => [] },
    isAuctionSelected: { type: Boolean, default: false },
    isUrgent: { type: Boolean, default: false },
    isAsap: { type: Boolean, default: false },
    selectedPrice: { type: Number, default: 0 },
    currencySymbol: { type: String, default: '₽' },
    creatingOrder: { type: Boolean, default: false },
  },
  emits: [
    'update:modelValue',
    'update:selectedCategoryId',
    'update:selectedSubCategoryId',
    'update:selectedVariantId',
    'update:isUrgent',
    'update:isAsap',
    'submitOrder',
  ],
  setup(props, { emit }) {
    const show = computed({
      get: () => props.modelValue,
      set: (val) => emit('update:modelValue', val),
    })

    return { show }
  },
})
</script>

<style scoped>
.btn-close-modal {
  font-size: 18px;
  opacity: 0.7;
  transition: opacity 0.15s;
}
.btn-close-modal:hover {
  opacity: 1;
}
</style>
