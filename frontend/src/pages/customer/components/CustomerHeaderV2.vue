<template>
  <div class="header-card p-4 rounded-2xl bg-white shadow-sm border mb-4">
    <div class="d-flex justify-content-between align-items-center flex-wrap gap-4">
      <!-- Left: Avatar, Phone, Badges -->
      <div class="d-flex align-items-center gap-4">
        <!-- Big Avatar Box -->
        <div class="avatar-square-box" @click="$emit('openProfileModal')">
          <va-icon name="person" size="48px" color="#94a3b8" />
        </div>

        <!-- Phone & Info -->
        <div>
          <div class="d-flex align-items-center gap-2">
            <h2 class="user-phone font-bold m-0 cursor-pointer text-dark" @click="$emit('openProfileModal')">
              {{ phone }}
            </h2>
            <i v-if="isVerified" class="ph-fill ph-check-circle" style="font-size: 1.2rem; color: #10b981;" title="Верифицирован"></i>
          </div>

          <div class="text-secondary text-sm mt-1">
            Личный кабинет заказчика
          </div>

          <div class="mt-2">
            <button
              type="button"
              class="btn-address-link text-primary font-medium border-0 bg-transparent p-0 cursor-pointer d-inline-flex align-items-center gap-1"
              @click="$emit('openProfileModal')"
            >
              <span class="icon-pin">📍</span> (Управление адресами)
            </button>
          </div>
        </div>
      </div>

      <!-- Center-Right: Language Selector & Balance -->
      <div class="d-flex align-items-center gap-4">
        <div class="divider-vertical d-none d-md-block"></div>

        <div class="text-left">
          <div class="language-dropdown-wrapper mb-2">
            <LanguageSwitcher />
          </div>
          
          <div class="balance-display font-bold text-primary">
            {{ currencySymbol }} {{ Number(balance).toFixed(2) }}
          </div>
          <div class="text-secondary text-xs">Баланс</div>
        </div>
      </div>

      <!-- Far Right: Top-Up Request & Logout -->
      <div class="d-flex flex-column align-items-end gap-2">
        <div class="d-flex align-items-center gap-2">
          <button type="button" class="btn-topup-gray" @click="$emit('openTopUpModal')">
            💳 Запросить пополнение кошелька
          </button>

          <button type="button" class="btn-logout-icon" title="Выйти" @click="$emit('logout')">
            <va-icon name="logout" size="small" />
            <span>Выйти</span>
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent } from 'vue'
import LanguageSwitcher from '../../../components/LanguageSwitcher.vue'

export default defineComponent({
  name: 'CustomerHeaderV2',
  components: { LanguageSwitcher },
  props: {
    phone: { type: String, default: '' },
    balance: { type: Number, default: 0 },
    currencySymbol: { type: String, default: '₽' },
    isVerified: { type: Boolean, default: false },
  },
  emits: ['logout', 'openProfileModal', 'openTopUpModal'],
})
</script>

<style scoped>
.header-card {
  border-radius: 16px;
  border: 1px solid #e2e8f0;
  background: #ffffff;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.03);
}

.avatar-square-box {
  width: 90px;
  height: 90px;
  border-radius: 12px;
  background: #e2e8f0;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: opacity 0.15s ease;
}

.avatar-square-box:hover {
  opacity: 0.9;
}

.user-phone {
  font-size: 1.6rem;
  letter-spacing: 0.5px;
  color: #0f172a;
}

.verified-pill-badge {
  background: #e6f4ea;
  color: #137333;
  border: 1px solid #ceead6;
  border-radius: 16px;
  padding: 3px 12px;
  font-size: 0.8rem;
  font-weight: 600;
}

.btn-address-link {
  font-size: 0.875rem;
  color: #2563eb;
  transition: color 0.15s ease;
}

.btn-address-link:hover {
  color: #1d4ed8 !important;
  text-decoration: underline;
}

.divider-vertical {
  width: 1px;
  height: 60px;
  background: #e2e8f0;
}

.balance-display {
  font-size: 1.6rem;
  line-height: 1.1;
  color: #2563eb;
}

.btn-topup-gray {
  background: #64748b;
  color: #ffffff;
  border: none;
  border-radius: 8px;
  padding: 8px 16px;
  font-size: 0.85rem;
  font-weight: 600;
  cursor: pointer;
  transition: background 0.15s ease;
}

.btn-topup-gray:hover {
  background: #475569;
}

.btn-logout-icon {
  background: #f1f5f9;
  border: 1px solid #cbd5e0;
  border-radius: 8px;
  padding: 6px 12px;
  font-size: 0.85rem;
  font-weight: 600;
  color: #475569;
  display: flex;
  align-items: center;
  gap: 4px;
  cursor: pointer;
  transition: all 0.15s ease;
}

.btn-logout-icon:hover {
  background: #e2e8f0;
  color: #0f172a;
}
</style>
