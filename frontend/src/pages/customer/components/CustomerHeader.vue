<template>
  <div class="dashboard-header mb-4">
    <div class="d-flex justify-content-between align-items-center">
      <!-- Clickable User Profile Box -->
      <div
        class="profile-clickable-box p-2 px-3 rounded-lg border transition-all cursor-pointer user-select-none"
        @click="$emit('openProfileModal')"
      >
        <div class="d-flex align-items-center gap-2">
          <h1 class="va-h5 m-0 font-bold text-primary phone-title">{{ phone }}</h1>
          <span
            v-if="isVerified"
            class="blue-verified-badge"
            title="Пользователь верифицирован"
          >
            ✓
          </span>
        </div>

        <div class="d-flex align-items-center gap-2 mt-1">
          <span class="text-secondary text-xs">{{ $t('customer.title') }}</span>
          <span v-if="isVerified" class="badge bg-success text-white text-xxs font-bold px-2 py-0-5 rounded-pill">
            Верифицирован
          </span>
          <span class="text-xs text-primary font-bold hover-underline d-flex align-items-center gap-1">
            <va-icon name="edit_location" size="small" /> (Управление адресами)
          </span>
        </div>
      </div>

      <!-- Right Header Actions -->
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
  background: #ffffff;
  border-radius: 12px;
  padding: 16px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.05);
}

.profile-clickable-box {
  background: #f8fafc;
  border: 1px solid #e2e8f0;
}

.profile-clickable-box:hover {
  background: #edf2f7;
  border-color: #cbd5e0;
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.06);
}

.phone-title {
  font-size: 1.4rem;
  letter-spacing: 0.5px;
}

.balance-amount {
  font-size: 1.8rem;
  font-weight: 800;
  color: #2b6cb0;
  line-height: 1.2;
}

.blue-verified-badge {
  width: 20px;
  height: 20px;
  background-color: #1da1f2;
  color: #ffffff;
  border-radius: 50%;
  font-size: 12px;
  font-weight: 900;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 2px 5px rgba(29, 161, 242, 0.45);
}

.hover-underline:hover {
  text-decoration: underline;
}

.py-0-5 {
  padding-top: 2px;
  padding-bottom: 2px;
}

@media (max-width: 576px) {
  .balance-amount {
    font-size: 1.4rem;
  }
  .phone-title {
    font-size: 1.2rem;
  }
}
</style>
