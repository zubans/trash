<template>
  <div class="dashboard-header mb-4">
    <div class="d-flex justify-content-between align-items-center">
      <div>
        <h1 class="va-h5 m-0">{{ phone }}</h1>
        <span class="text-secondary text-sm">{{ $t('customer.title') }}</span>
      </div>
      <div class="text-right">
        <LanguageSwitcher class="mb-2" />
        <div class="balance-amount">{{ currencySymbol }}{{ Number(balance).toFixed(2) }}</div>
        <div class="text-secondary text-xs">{{ $t('customer.balance') }}</div>
        <va-button color="danger" outline size="small" class="mt-2" @click="$emit('logout')">
          <va-icon name="logout" class="mr-1" /> {{ $t('app.logout') }}
        </va-button>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent } from 'vue'
import LanguageSwitcher from '../../../components/LanguageSwitcher.vue'

export default defineComponent({
  name: 'CustomerHeader',
  components: { LanguageSwitcher },
  props: {
    phone: { type: String, default: '' },
    balance: { type: Number, default: 0 },
    currencySymbol: { type: String, default: '₽' },
  },
  emits: ['logout'],
})
</script>

<style scoped>
.dashboard-header {
  background: #fff;
  border-radius: 12px;
  padding: 16px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.05);
}

.balance-amount {
  font-size: 1.8rem;
  font-weight: 800;
  color: #2b6cb0;
  line-height: 1.2;
}
</style>
