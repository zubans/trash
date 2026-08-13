<template>
  <div v-if="show" class="modal-overlay" @click.self="show = false">
    <!-- Modal Card -->
    <div class="modal-card">
      <!-- Header -->
      <div class="modal-header">
        <div class="modal-title">{{ $t('customer.createNewOrder') }}</div>
        <button type="button" class="btn-close" aria-label="Закрыть" @click="show = false">
          <i class="ph ph-x"></i>
        </button>
      </div>

      <!-- Address Info Box -->
      <div class="info-box">
        <div class="info-label">
          <i class="ph-fill ph-map-pin"></i> {{ $t('customer.pickupAddress') }}
        </div>
        <div class="info-address">{{ orderAddress || 'Адрес не указан' }}</div>
        <div class="info-note">{{ $t('customer.addressChangeHint') }}</div>
        <div v-if="orderLat !== null && orderLon !== null" class="info-note text-xs mt-1">
          {{ $t('customer.coordinates') }}: {{ orderLat.toFixed(5) }}, {{ orderLon.toFixed(5) }}
        </div>
        <div v-if="geocodeError" class="text-danger text-xs mt-1">
          {{ geocodeError }}
        </div>
      </div>

      <form @submit.prevent="$emit('submitOrder')">
        <!-- Category Selector -->
        <div class="form-group">
          <label class="form-label">{{ $t('customer.category') }}</label>
          <div class="select-wrapper">
            <select
              v-model="categoryIdProxy"
              class="form-select"
            >
              <option value="" disabled>Выберите категорию</option>
              <option v-for="opt in categoryOptions" :key="opt.value" :value="opt.value">
                {{ opt.label }}
              </option>
            </select>
            <i class="ph ph-caret-down select-icon"></i>
          </div>
        </div>

        <!-- SubCategory Selector (if available) -->
        <div v-if="subCategoryOptions && subCategoryOptions.length > 0" class="form-group">
          <label class="form-label">{{ $t('customer.subCategory') }}</label>
          <div class="select-wrapper">
            <select
              v-model="subCategoryIdProxy"
              class="form-select"
            >
              <option value="" disabled>Выберите подкатегорию</option>
              <option v-for="opt in subCategoryOptions" :key="opt.value" :value="opt.value">
                {{ opt.label }}
              </option>
            </select>
            <i class="ph ph-caret-down select-icon"></i>
          </div>
        </div>

        <!-- Service Variant Selector (if available) -->
        <div v-if="variantOptions && variantOptions.length > 0" class="form-group">
          <label class="form-label">{{ $t('customer.serviceVariant') }}</label>
          <div class="select-wrapper">
            <select
              v-model="variantIdProxy"
              class="form-select"
            >
              <option value="" disabled>Выберите вариант услуги</option>
              <option v-for="opt in variantOptions" :key="opt.value" :value="opt.value">
                {{ opt.label }}
              </option>
            </select>
            <i class="ph ph-caret-down select-icon"></i>
          </div>
        </div>

        <!-- Urgency Checkbox Pills -->
        <div v-if="!isAuctionSelected" class="form-group">
          <div class="checkbox-group">
            <label class="custom-checkbox">
              <input
                type="checkbox"
                :checked="isUrgent"
                @change="$emit('update:isUrgent', ($event.target as HTMLInputElement).checked)"
              />
              <div class="checkbox-pill">
                <i class="ph ph-clock-countdown"></i> {{ $t('customer.urgent') }}
              </div>
            </label>

            <label class="custom-checkbox">
              <input
                type="checkbox"
                :checked="isAsap"
                @change="$emit('update:isAsap', ($event.target as HTMLInputElement).checked)"
              />
              <div class="checkbox-pill">
                <i class="ph ph-lightning"></i> ASAP
              </div>
            </label>
          </div>
        </div>

        <!-- Total Price Row -->
        <div class="price-row">
          <div class="price-label">{{ $t('customer.price') }}:</div>
          <div class="price-value">{{ Number(selectedPrice).toFixed(2) }} {{ currencySymbol }}</div>
        </div>

        <!-- Footer Buttons -->
        <div class="modal-footer">
          <button type="button" class="btn btn-cancel" @click="show = false">
            {{ $t('common.cancel') }}
          </button>
          <button type="submit" class="btn btn-submit" :disabled="creatingOrder">
            <span v-if="creatingOrder" class="spinner-sm"></span>
            <template v-else>
              <i class="ph-bold ph-plus"></i> {{ $t('customer.createOrder') }}
            </template>
          </button>
        </div>
      </form>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, computed, onMounted } from 'vue'

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

    const categoryIdProxy = computed({
      get: () => props.selectedCategoryId || '',
      set: (val) => emit('update:selectedCategoryId', val),
    })

    const subCategoryIdProxy = computed({
      get: () => props.selectedSubCategoryId || '',
      set: (val) => emit('update:selectedSubCategoryId', val),
    })

    const variantIdProxy = computed({
      get: () => props.selectedVariantId || '',
      set: (val) => emit('update:selectedVariantId', val),
    })

    const loadPhosphorIcons = () => {
      if (!document.getElementById('phosphor-icons-script')) {
        const script = document.createElement('script')
        script.id = 'phosphor-icons-script'
        script.src = 'https://unpkg.com/@phosphor-icons/web'
        document.head.appendChild(script)
      }
    }

    onMounted(() => {
      loadPhosphorIcons()
    })

    return {
      show,
      categoryIdProxy,
      subCategoryIdProxy,
      variantIdProxy,
    }
  },
})
</script>

<style scoped>
@import url('https://fonts.googleapis.com/css2?family=Outfit:wght@300;400;500;600;700&display=swap');

/* --- Modal Overlay --- */
.modal-overlay {
  --bg-base: #f8f9fa;
  --surface-card: rgba(255, 255, 255, 0.92);
  --surface-input: rgba(255, 255, 255, 0.7);
  
  --text-title: #0f172a;
  --text-body: #334155;
  --text-muted: #8b98a5;
  
  --accent-main: #6366f1;
  --accent-glow: rgba(99, 102, 241, 0.4);
  
  --rad-sm: 12px;
  --rad-md: 20px;
  --rad-lg: 32px;
  
  --shadow-float: 0 20px 50px -10px rgba(15, 23, 42, 0.1), 
                  0 1px 3px rgba(15, 23, 42, 0.05),
                  inset 0 1px 0 rgba(255,255,255,1);
  
  --transition: all 0.4s cubic-bezier(0.16, 1, 0.3, 1);

  position: fixed;
  top: 0; left: 0; right: 0; bottom: 0;
  background: rgba(15, 23, 42, 0.4);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
  z-index: 1050;
  animation: fadeIn 0.3s ease-out;
  font-family: 'Outfit', sans-serif;
  color: var(--text-body);
}

@keyframes fadeIn {
  from { opacity: 0; }
  to { opacity: 1; }
}

/* --- Modal Card --- */
.modal-card {
  background: var(--surface-card);
  backdrop-filter: blur(24px);
  -webkit-backdrop-filter: blur(24px);
  border: 1px solid rgba(255, 255, 255, 0.8);
  border-radius: var(--rad-lg);
  width: 100%;
  max-width: 480px;
  box-shadow: var(--shadow-float);
  padding: 32px;
  position: relative;
  animation: slideUp 0.4s cubic-bezier(0.16, 1, 0.3, 1);
  max-height: 90vh;
  overflow-y: auto;
}

@keyframes slideUp {
  from { opacity: 0; transform: translateY(20px) scale(0.95); }
  to { opacity: 1; transform: translateY(0) scale(1); }
}

/* --- Header --- */
.modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 24px;
}

.modal-title {
  font-size: 24px;
  font-weight: 700;
  color: var(--text-title);
  letter-spacing: -0.5px;
}

.btn-close {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  border: 1px solid rgba(255,255,255,0.8);
  background: rgba(255,255,255,0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  color: var(--text-muted);
  cursor: pointer;
  transition: var(--transition);
}

.btn-close:hover {
  background: #ffffff;
  color: #ef4444;
  box-shadow: 0 4px 12px rgba(0,0,0,0.05);
  transform: rotate(90deg);
}

/* --- Info Box (Address) --- */
.info-box {
  background: rgba(99, 102, 241, 0.04);
  border: 1px solid rgba(99, 102, 241, 0.1);
  border-radius: var(--rad-md);
  padding: 16px 20px;
  margin-bottom: 24px;
  position: relative;
  overflow: hidden;
}

.info-box::before {
  content: '';
  position: absolute;
  left: 0; top: 0; bottom: 0; width: 4px;
  background: var(--accent-main);
}

.info-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--accent-main);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin-bottom: 4px;
  display: flex;
  align-items: center;
  gap: 6px;
}

.info-address {
  font-size: 16px;
  font-weight: 600;
  color: var(--text-title);
  margin-bottom: 6px;
}

.info-note {
  font-size: 13px;
  color: var(--text-muted);
  line-height: 1.4;
}

/* --- Form Groups --- */
.form-group {
  margin-bottom: 24px;
}

.form-label {
  display: block;
  font-size: 13px;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin-bottom: 10px;
  padding-left: 4px;
}

.select-wrapper {
  position: relative;
}

.form-select {
  width: 100%;
  appearance: none;
  -webkit-appearance: none;
  -moz-appearance: none;
  padding: 16px 48px 16px 20px;
  border-radius: 16px;
  background-color: var(--surface-input);
  border: 1.5px solid rgba(255, 255, 255, 0.8);
  font-family: inherit;
  font-size: 15px;
  color: var(--text-title);
  font-weight: 600;
  cursor: pointer;
  transition: var(--transition);
  box-shadow: inset 0 2px 4px rgba(0,0,0,0.01);
}

.form-select option {
  background-color: #ffffff;
  color: #0f172a;
  padding: 12px 16px;
  font-weight: 500;
}

.form-select:focus {
  outline: none;
  border-color: var(--accent-main);
  background-color: #ffffff;
  box-shadow: 0 0 0 4px rgba(99, 102, 241, 0.1);
}

.select-icon {
  position: absolute;
  right: 20px;
  top: 50%;
  transform: translateY(-50%);
  font-size: 18px;
  color: var(--text-muted);
  pointer-events: none;
}

/* Checkboxes (Pill Style) */
.checkbox-group {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
}

.custom-checkbox {
  position: relative;
  display: inline-flex;
  align-items: center;
  cursor: pointer;
}

.custom-checkbox input {
  position: absolute;
  opacity: 0;
  cursor: pointer;
  height: 0;
  width: 0;
}

.checkbox-pill {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 20px;
  border-radius: 99px;
  background: var(--surface-input);
  border: 1.5px solid rgba(255,255,255,0.8);
  font-size: 14px;
  font-weight: 600;
  color: var(--text-body);
  transition: var(--transition);
}

.checkbox-pill i {
  font-size: 18px;
  color: var(--text-muted);
  transition: var(--transition);
}

.custom-checkbox input:checked ~ .checkbox-pill {
  background: #e0e7ff;
  border-color: var(--accent-main);
  color: var(--accent-main);
}

.custom-checkbox input:checked ~ .checkbox-pill i {
  color: var(--accent-main);
}

.custom-checkbox:hover .checkbox-pill {
  background: #ffffff;
  transform: translateY(-1px);
}

/* --- Price Row --- */
.price-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 32px;
  margin-bottom: 24px;
  padding-top: 24px;
  border-top: 1px solid rgba(0,0,0,0.06);
}

.price-label {
  font-size: 15px;
  font-weight: 600;
  color: var(--text-muted);
}

.price-value {
  font-size: 28px;
  font-weight: 700;
  color: var(--text-title);
  letter-spacing: -0.5px;
}

/* --- Footer --- */
.modal-footer {
  display: flex;
  gap: 12px;
}

.btn {
  padding: 16px;
  border-radius: 16px;
  font-size: 16px;
  font-weight: 600;
  cursor: pointer;
  transition: var(--transition);
  border: none;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
}

.btn-cancel {
  flex: 1;
  background: rgba(15, 23, 42, 0.05);
  color: var(--text-body);
}

.btn-cancel:hover {
  background: rgba(15, 23, 42, 0.1);
  color: var(--text-title);
}

.btn-submit {
  flex: 2;
  background: linear-gradient(135deg, #6366f1, #4f46e5);
  color: white;
  box-shadow: 0 10px 24px -6px var(--accent-glow);
}

.btn-submit:hover {
  transform: translateY(-2px);
  box-shadow: 0 15px 30px -6px rgba(99, 102, 241, 0.6);
}

.btn-submit:disabled {
  opacity: 0.7;
  cursor: not-allowed;
}

.spinner-sm {
  width: 18px;
  height: 18px;
  border: 2.5px solid rgba(255,255,255,0.3);
  border-top-color: #ffffff;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

/* Responsive */
@media (max-width: 480px) {
  .modal-card {
    padding: 24px;
    border-radius: 28px;
  }
  .modal-footer {
    flex-direction: column-reverse;
  }
  .btn {
    width: 100%;
  }
}
</style>
