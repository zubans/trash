<template>
  <va-card class="p-4 mb-4 shadow-card">
    <h3 class="va-h5 mb-4 text-primary d-flex align-items-center">
      <va-icon name="account_circle" class="mr-2" /> {{ $t('executor.accountDetails') }}
    </h3>
    <div class="info-list">
      <div class="info-item mb-3">
        <span class="info-label">{{ $t('customer.phone') }}</span>
        <span class="info-val">{{ phone }}</span>
      </div>
      <div class="info-item mb-3">
        <span class="info-label">{{ $t('customer.status') }}</span>
        <span class="info-val">
          <va-badge color="success">{{ status }}</va-badge>
        </span>
      </div>
    </div>

    <div class="balance-box mt-4 p-3 text-center cursor-pointer hover-shadow transition" @click="$emit('openFinancialHistory')">
      <span class="balance-label d-block text-secondary text-sm mb-1 d-flex align-items-center justify-content-center">
        <va-icon name="account_balance_wallet" class="mr-1 text-primary" size="small" />
        {{ $t('executor.yourWalletBalance') }}
        <va-icon name="history" class="ml-1 text-secondary" size="small" />
      </span>
      <span class="balance-amount">{{ currencySymbol }}{{ Number(balance).toFixed(2) }}</span>
      <span class="text-xxs text-primary d-block mt-1">({{ $t('common.details') }})</span>
    </div>
  </va-card>
</template>

<script lang="ts">
import { defineComponent } from 'vue'

export default defineComponent({
  name: 'ExecutorProfileCard',
  props: {
    phone: { type: String, default: '' },
    status: { type: String, default: '' },
    balance: { type: Number, default: 0 },
    currencySymbol: { type: String, default: '₽' },
  },
  emits: ['openFinancialHistory'],
})
</script>

<style scoped>
.shadow-card {
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.05);
  border-radius: 12px !important;
}

.info-list .info-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  border-bottom: 1px dashed #eee;
  padding-bottom: 8px;
}

.balance-box {
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  border-radius: 8px;
}

.balance-amount {
  font-size: 1.8rem;
  font-weight: 800;
  color: #2b6cb0;
  line-height: 1.2;
}
</style>
