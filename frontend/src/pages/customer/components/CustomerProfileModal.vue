<template>
  <va-modal
    v-model="show"
    hide-default-actions
    max-width="550px"
  >
    <template #header>
      <div class="d-flex justify-content-between align-items-center w-100 p-3 pb-0">
        <h3 class="va-h5 m-0 font-bold d-flex align-items-center gap-2">
          👤 Профиль заказчика
          <span
            v-if="isVerified"
            class="blue-verified-badge"
            title="Пользователь верифицирован"
          >
            ✓
          </span>
        </h3>
        <button type="button" class="btn-close-modal border-0 bg-transparent text-secondary cursor-pointer" @click="show = false">
          ✕
        </button>
      </div>
    </template>

    <div class="p-3">
      <!-- Verification Status Banner -->
      <div class="p-3 rounded mb-4 d-flex align-items-center justify-content-between" :class="isVerified ? 'bg-primary-light text-primary' : 'bg-light text-secondary'">
        <div class="d-flex align-items-center gap-2">
          <span class="text-xl">{{ isVerified ? '🛡️' : '📑' }}</span>
          <div>
            <div class="font-bold text-sm">
              {{ isVerified ? 'Пользователь верифицирован' : 'Статус верификации' }}
            </div>
            <div class="text-xs opacity-85">
              {{ isVerified ? 'Ваш паспорт проверен администратором системы' : 'Для верификации передайте данные администратору' }}
            </div>
          </div>
        </div>
        <va-badge :color="isVerified ? 'info' : 'secondary'">
          {{ isVerified ? 'Подтвержден' : 'Не верифицирован' }}
        </va-badge>
      </div>

      <!-- Saved Addresses Section (Max 2 addresses) -->
      <div class="mb-4">
        <h4 class="va-h6 font-bold mb-2 text-dark d-flex align-items-center justify-content-between">
          <span>📍 Сохраненные адреса ({{ customerAddresses.length }}/2)</span>
          <span class="text-xs text-secondary font-normal">Выберите активный</span>
        </h4>

        <div v-if="customerAddresses.length === 0" class="text-xs text-secondary italic mb-2">
          Нет сохраненных адресов. Добавьте адрес ниже.
        </div>

        <div v-for="(addr, idx) in customerAddresses" :key="idx" class="border rounded p-3 mb-2 d-flex align-items-center justify-content-between" :class="addr.address === defaultAddress ? 'bg-light border-primary' : ''">
          <div class="d-flex align-items-center gap-2 overflow-hidden mr-2">
            <input
              type="radio"
              name="active_address"
              :checked="addr.address === defaultAddress"
              @change="$emit('setActiveAddress', addr.address)"
              class="cursor-pointer"
            />
            <div class="truncate">
              <div class="font-bold text-xs">{{ addr.address }}</div>
              <span v-if="addr.address === defaultAddress" class="badge bg-success text-white text-xxs mt-1">Активный для заказов</span>
            </div>
          </div>
          <va-button color="danger" flat size="small" icon="delete" @click="$emit('removeAddress', idx)" />
        </div>

        <!-- Add new address form if < 2 -->
        <div v-if="customerAddresses.length < 2" class="mt-3">
          <va-input
            :model-value="newAddressInput"
            @update:model-value="$emit('update:newAddressInput', $event)"
            placeholder="Введите новый адрес"
            class="mb-2"
          />
          <va-button color="primary" outline size="small" :disabled="!newAddressInput.trim()" @click="$emit('addNewAddress')">
            ➕ Добавить адрес
          </va-button>
        </div>
        <div v-else class="text-xs text-warning mt-2">
          ℹ️ Можно сохранить не более 2 адресов. Удалите один, чтобы добавить новый.
        </div>
      </div>

      <div class="text-right mt-3">
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

.bg-primary-light {
  background-color: #ebf8ff;
  border: 1px solid #bee3f8;
}

.btn-close-modal {
  font-size: 18px;
  opacity: 0.7;
  transition: opacity 0.15s;
}
.btn-close-modal:hover {
  opacity: 1;
}
</style>
