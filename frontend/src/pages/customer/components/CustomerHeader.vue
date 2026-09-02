<template>
  <div class="dashboard-header-container mb-4">
    <!-- Верхняя полоса: заголовок и правые элементы управления -->
    <div class="top-nav-bar d-flex justify-content-between align-items-center mb-3">
      <div>
        <h1 class="page-title m-0">Личный кабинет</h1>
        <p class="page-subtitle m-0">Добро пожаловать в ваш кабинет</p>
      </div>

      <div class="d-flex align-items-center gap-3">
        <LanguageSwitcher />
        <button type="button" class="btn-logout" @click="$emit('logout')">
          <va-icon name="logout" size="small" class="mr-1" /> Выйти
        </button>
      </div>
    </div>

    <!-- Основная карточка шапки -->
    <div class="header-card p-4 rounded-2xl bg-white shadow-sm border">
      <div class="d-flex justify-content-between align-items-center flex-wrap gap-3">
        <!-- Слева: сведения профиля -->
        <div class="d-flex align-items-center gap-3">
          <!-- Круг с иконкой аватара -->
          <div class="avatar-circle" @click="$emit('openProfileModal')">
            <va-icon name="person" size="large" color="primary" />
          </div>

          <!-- Телефон и бейджи -->
          <div>
            <div class="d-flex align-items-center gap-2">
              <h2 class="user-phone font-bold m-0 cursor-pointer" @click="$emit('openProfileModal')">
                {{ phone }}
              </h2>
              <i v-if="isVerified" class="ph-fill ph-check-circle text-emerald-500" style="font-size: 1.2rem; color: #10b981;" title="Верифицирован"></i>
            </div>

            <div class="mt-1">
              <button type="button" class="btn-address-link text-primary font-medium border-0 bg-transparent p-0 cursor-pointer" @click="$emit('openProfileModal')">
                📍 Управление адресами
              </button>
            </div>
          </div>
        </div>

        <!-- Справа: баланс и действие пополнения -->
        <div class="d-flex align-items-center gap-4">
          <div class="balance-block text-right">
            <span class="balance-label text-secondary text-xs d-block mb-1">Баланс</span>
            <span class="balance-value font-bold text-dark">{{ currencySymbol }} {{ Number(balance).toFixed(2) }}</span>
          </div>

          <button type="button" class="btn-topup-header" @click="$emit('openTopUpModal')">
            💳 Пополнить кошелёк
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
  name: 'CustomerHeader',
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
.page-title {
  font-size: 1.6rem;
  font-weight: 800;
  color: #0f172a;
}

.page-subtitle {
  font-size: 0.875rem;
  color: #64748b;
}

.btn-logout {
  background: transparent;
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  padding: 6px 14px;
  font-size: 0.875rem;
  font-weight: 600;
  color: #475569;
  cursor: pointer;
  transition: all 0.15s ease;
}

.btn-logout:hover {
  background: #f8fafc;
  color: #0f172a;
  border-color: #cbd5e0;
}

.header-card {
  border-radius: 16px;
  border: 1px solid #f1f5f9;
  background: #ffffff;
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.03);
}

.avatar-circle {
  width: 56px;
  height: 56px;
  border-radius: 50%;
  background: #eff6ff;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: transform 0.15s ease;
}

.avatar-circle:hover {
  transform: scale(1.04);
}

.user-phone {
  font-size: 1.35rem;
  color: #0f172a;
  letter-spacing: 0.3px;
}

.verified-status-tag {
  font-size: 0.8rem;
  font-weight: 600;
  color: #16a34a;
}

.check-dot {
  width: 16px;
  height: 16px;
  background: #22c55e;
  color: white;
  border-radius: 50%;
  font-size: 10px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-weight: 900;
}

.btn-address-link {
  font-size: 0.85rem;
  transition: color 0.15s ease;
}

.btn-address-link:hover {
  color: #1d4ed8 !important;
  text-decoration: underline;
}

.balance-label {
  font-size: 0.8rem;
  color: #64748b;
}

.balance-value {
  font-size: 1.5rem;
  color: #0f172a;
  line-height: 1;
}

.btn-topup-header {
  background: #2563eb;
  color: #ffffff;
  border: none;
  border-radius: 10px;
  padding: 10px 18px;
  font-size: 0.9rem;
  font-weight: 700;
  cursor: pointer;
  transition: background 0.15s ease;
}

.btn-topup-header:hover {
  background: #1d4ed8;
}
</style>
