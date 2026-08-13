<template>
  <va-modal
    v-model="show"
    hide-default-actions
    max-width="560px"
    class="profile-modal-dialog"
  >
    <div class="profile-modal-content p-3">
      <!-- Modal Header -->
      <div class="d-flex justify-content-between align-items-center mb-4 pb-3 border-bottom">
        <div class="d-flex align-items-center gap-2">
          <span class="profile-avatar-icon">👤</span>
          <h3 class="va-h5 m-0 font-bold text-dark">Профиль заказчика</h3>
          <span
            v-if="isVerified"
            class="blue-verified-badge"
            title="Пользователь верифицирован"
          >
            ✓
          </span>
        </div>
        <button
          type="button"
          class="btn-close-modal"
          @click="show = false"
          aria-label="Close"
        >
          ✕
        </button>
      </div>

      <!-- Verification Status Card -->
      <div
        class="verification-card p-3 rounded-lg mb-4 d-flex align-items-center justify-content-between"
        :class="isVerified ? 'verified-bg' : 'unverified-bg'"
      >
        <div class="d-flex align-items-center gap-3">
          <span class="status-shield-icon">{{ isVerified ? '🛡️' : '📑' }}</span>
          <div>
            <div class="font-bold text-sm text-dark">
              {{ isVerified ? 'Пользователь верифицирован' : 'Статус верификации' }}
            </div>
            <div class="text-xs text-secondary mt-0-5">
              {{ isVerified ? 'Ваш паспорт проверен администратором системы' : 'Для верификации передайте данные администратору' }}
            </div>
          </div>
        </div>
        <span
          class="status-pill-badge font-bold text-xs px-3 py-1 rounded-pill"
          :class="isVerified ? 'bg-success text-white' : 'bg-secondary text-white'"
        >
          {{ isVerified ? 'Подтвержден' : 'Не верифицирован' }}
        </span>
      </div>

      <!-- Saved Addresses Section (Max 2 addresses) -->
      <div class="addresses-section mb-4">
        <div class="d-flex align-items-center justify-content-between mb-3">
          <h4 class="va-h6 font-bold m-0 text-dark d-flex align-items-center gap-2">
            <span>📍 Сохраненные адреса</span>
            <span class="badge bg-primary-subtle text-primary font-bold text-xs">
              {{ customerAddresses.length }}/2
            </span>
          </h4>
          <span class="text-xs text-secondary">Выберите активный для заказов</span>
        </div>

        <div v-if="customerAddresses.length === 0" class="empty-address-box p-3 text-center rounded border border-dashed mb-3 text-secondary text-xs">
          Нет сохраненных адресов. Добавьте адрес ниже.
        </div>

        <div
          v-for="(addr, idx) in customerAddresses"
          :key="idx"
          class="address-item-card p-3 mb-2 rounded-lg border d-flex align-items-center justify-content-between transition-all"
          :class="addr.address === defaultAddress ? 'active-address-card' : 'inactive-address-card'"
        >
          <div class="d-flex align-items-center gap-3 overflow-hidden mr-2">
            <input
              type="radio"
              :id="'addr-' + idx"
              name="active_address"
              :checked="addr.address === defaultAddress"
              @change="$emit('setActiveAddress', addr.address)"
              class="active-radio-input cursor-pointer"
            />
            <label :for="'addr-' + idx" class="m-0 cursor-pointer overflow-hidden">
              <div class="font-bold text-sm text-dark truncate">{{ addr.address }}</div>
              <span v-if="addr.address === defaultAddress" class="badge bg-success text-white text-xxs font-bold mt-1 d-inline-block">
                ✓ Активный для заказов
              </span>
            </label>
          </div>
          <button
            type="button"
            class="btn-delete-addr border-0 bg-transparent text-danger p-1 rounded hover-bg-danger-light cursor-pointer"
            title="Удалить адрес"
            @click="$emit('removeAddress', idx)"
          >
            🗑️
          </button>
        </div>

        <!-- Add new address form if < 2 -->
        <div v-if="customerAddresses.length < 2" class="add-address-form mt-3 p-3 bg-light rounded-lg border">
          <label class="text-xs font-bold text-secondary mb-1 d-block">Добавить новый адрес</label>
          <div class="d-flex gap-2">
            <input
              type="text"
              :value="newAddressInput"
              @input="$emit('update:newAddressInput', ($event.target as HTMLInputElement).value)"
              placeholder="г. Москва, ул. Ленина, д. 10"
              class="form-control form-control-sm text-sm"
              @keyup.enter="$emit('addNewAddress')"
            />
            <va-button
              color="primary"
              size="small"
              :disabled="!newAddressInput.trim()"
              @click="$emit('addNewAddress')"
            >
              ➕ Добавить
            </va-button>
          </div>
        </div>
        <div v-else class="limit-warning-banner p-2 px-3 rounded mt-2 bg-warning-light text-warning-dark text-xs d-flex align-items-center gap-2">
          <span>ℹ️</span>
          <span>Можно сохранить не более 2 адресов. Удалите один, чтобы добавить новый.</span>
        </div>
      </div>

      <!-- Action Footer -->
      <div class="d-flex justify-content-end pt-3 border-top">
        <va-button color="secondary" size="medium" @click="show = false">
          Закрыть
        </va-button>
      </div>
    </div>
  </va-modal>
</template>

<script lang="ts">
import { defineComponent, computed } from 'vue'

export default defineComponent({
  name: 'CustomerProfileModal',
  props: {
    modelValue: { type: Boolean, required: true },
    isVerified: { type: Boolean, default: true },
    customerAddresses: { type: Array as () => any[], default: () => [] },
    defaultAddress: { type: String, default: '' },
    newAddressInput: { type: String, default: '' },
  },
  emits: [
    'update:modelValue',
    'update:newAddressInput',
    'setActiveAddress',
    'addNewAddress',
    'removeAddress',
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
.profile-modal-content {
  color: #1a202c;
}

.profile-avatar-icon {
  font-size: 22px;
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

.btn-close-modal {
  width: 32px;
  height: 32px;
  border: none;
  background: #f1f5f9;
  border-radius: 50%;
  font-size: 16px;
  color: #64748b;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.15s ease;
}

.btn-close-modal:hover {
  background: #e2e8f0;
  color: #0f172a;
}

.verification-card {
  border: 1px solid transparent;
}

.verified-bg {
  background: #f0fdf4;
  border-color: #bbf7d0;
}

.unverified-bg {
  background: #f8fafc;
  border-color: #e2e8f0;
}

.status-shield-icon {
  font-size: 24px;
}

.active-address-card {
  background: #eff6ff;
  border-color: #3b82f6 !important;
  box-shadow: 0 2px 6px rgba(59, 130, 246, 0.12);
}

.inactive-address-card {
  background: #ffffff;
  border-color: #e2e8f0;
}

.inactive-address-card:hover {
  background: #f8fafc;
  border-color: #cbd5e0;
}

.active-radio-input {
  width: 18px;
  height: 18px;
  accent-color: #2563eb;
}

.bg-primary-subtle {
  background: #dbeafe;
  color: #1e40af;
}

.bg-warning-light {
  background: #fffbeb;
  border: 1px solid #fef3c7;
}

.text-warning-dark {
  color: #92400e;
}

.hover-bg-danger-light:hover {
  background: #fee2e2;
}

.form-control {
  border: 1px solid #cbd5e0;
  border-radius: 6px;
  padding: 6px 10px;
  outline: none;
}

.form-control:focus {
  border-color: #3b82f6;
  box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.2);
}
</style>
