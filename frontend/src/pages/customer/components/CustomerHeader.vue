<template>
  <div class="dashboard-header mb-4">
    <div class="d-flex justify-content-between align-items-center">
      <div class="cursor-pointer user-select-none" @click="$emit('openProfileModal')">
        <div class="d-flex align-items-center gap-2">
          <h1 class="va-h5 m-0 font-bold text-primary">{{ phone }}</h1>
          <span
            v-if="isVerified"
            class="blue-verified-badge d-inline-flex align-items-center justify-content-center"
            title="Пользователь верифицирован"
          >
            ✓
          </span>
        </div>
        <div class="d-flex align-items-center gap-2 mt-1">
          <span class="text-secondary text-sm">{{ $t('customer.title') }}</span>
          <span v-if="isVerified" class="badge bg-info text-white text-xxs font-bold px-2 py-1 rounded-pill">
            Верифицирован
          </span>
          <span class="text-xs text-primary font-medium underline">
            (Управление адресами)
          </span>
        </div>
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
    isVerified: { type: Boolean, default: true },
  },
  emits: ['logout', 'openProfileModal'],
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

.blue-verified-badge {
  width: 18px;
  height: 18px;
  background-color: #1da1f2;
  color: #ffffff;
  border-radius: 50%;
  font-size: 11px;
  font-weight: 900;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 2px 4px rgba(29, 161, 242, 0.4);
}

@media (max-width: 576px) {
  .balance-amount {
    font-size: 1.4rem;
  }
}
</style>
