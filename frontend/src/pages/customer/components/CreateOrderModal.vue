<template>
  <div v-if="show" class="modal-overlay" @click.self="show = false">
    <div class="modal-card">
      <!-- Header -->
      <div class="modal-header">
        <div class="modal-title">{{ $t('customer.createNewOrder') }}</div>
        <button type="button" class="btn-close" aria-label="Закрыть" @click="show = false">
          <i class="ph ph-x"></i>
        </button>
      </div>

      <!-- Address Box -->
      <div class="info-box">
        <div class="info-icon"><i class="ph-fill ph-map-pin"></i></div>
        <div class="info-text">
          <div class="info-address">{{ orderAddress || 'Адрес не указан' }}</div>
          <div v-if="geocodeError" class="text-danger text-xs mt-1">
            {{ geocodeError }}
          </div>
        </div>
      </div>

      <form @submit.prevent="$emit('submitOrder')">
        <!-- ШАГ 1: КАТЕГОРИЯ (Показываем с помощью segmented control) -->
        <div class="form-group">
          <label class="form-label">1. Выберите категорию</label>
          <div class="segmented-control">
            <label v-for="cat in categoryOptions" :key="cat.value" class="segment">
              <input
                type="radio"
                name="category"
                :value="cat.value"
                :checked="categoryIdProxy === cat.value"
                @change="categoryIdProxy = cat.value"
              />
              <span>{{ cat.label }}</span>
            </label>
          </div>
        </div>

        <!-- ШАГ 2: ПОДКАТЕГОРИЯ (Появляется плавно, если есть подкатегории) -->
        <div
          v-if="subCategoryOptions && subCategoryOptions.length > 0"
          class="form-group step-block visible"
        >
          <label class="form-label">2. Уточните детали</label>
          <div class="pills-group">
            <label v-for="sub in subCategoryOptions" :key="sub.value" class="pill">
              <input
                type="radio"
                name="subcategory"
                :value="sub.value"
                :checked="subCategoryIdProxy === sub.value"
                @change="subCategoryIdProxy = sub.value"
              />
              <span>{{ sub.label }}</span>
            </label>
          </div>
        </div>

        <!-- ШАГ 3: ВИД УСЛУГИ (Появляется плавно, если есть варианты) -->
        <div
          v-if="variantOptions && variantOptions.length > 0"
          class="form-group step-block visible"
        >
          <label class="form-label">3. Вид услуги</label>
          <div class="radio-cards">
            <label
              v-for="variant in variantOptions"
              :key="variant.value"
              class="radio-card"
            >
              <input
                type="radio"
                name="service_variant"
                :value="variant.value"
                :checked="variantIdProxy === variant.value"
                @change="variantIdProxy = variant.value"
              />
              <div class="rc-circle"></div>
              <div class="rc-content">{{ variant.label }}</div>
            </label>
          </div>
        </div>

        <!-- Дополнительные параметры: Срочность / ASAP -->
        <div v-if="selectedVariantId && !isAuctionSelected" class="form-group step-block visible mt-3">
          <label class="form-label">Скорость исполнения</label>
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

        <!-- Итоговая цена -->
        <div class="price-row">
          <div class="price-label">Предварительная цена:</div>
          <div class="price-value" :style="{ color: selectedVariantId ? 'var(--accent-main)' : 'var(--text-title)' }">
            {{ selectedVariantId ? `${Number(selectedPrice).toFixed(2)} ${currencySymbol}` : '-- ₽' }}
          </div>
        </div>

        <!-- Футер -->
        <div class="modal-footer">
          <button type="button" class="btn btn-cancel" @click="show = false">
            Отмена
          </button>
          <button
            type="submit"
            :class="['btn btn-submit', { active: !!selectedVariantId && !creatingOrder }]"
            :disabled="creatingOrder || !selectedVariantId"
          >
            <span v-if="creatingOrder" class="spinner-sm"></span>
            <template v-else>
              <i class="ph-bold ph-plus"></i> Создать заказ
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

.modal-overlay {
  --bg-base: #f8f9fa;
  --surface-card: rgba(255, 255, 255, 0.85);
  --surface-input: rgba(255, 255, 255, 0.6);
  
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
  
  --transition: all 0.3s cubic-bezier(0.16, 1, 0.3, 1);

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
  font-family: 'Outfit', sans-serif;
  color: var(--text-body);
}

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

/* Info Box */
.info-box {
  background: rgba(99, 102, 241, 0.04);
  border: 1px solid rgba(99, 102, 241, 0.1);
  border-radius: var(--rad-md);
  padding: 16px;
  margin-bottom: 24px;
  display: flex;
  align-items: center;
  gap: 16px;
}

.info-icon {
  width: 40px;
  height: 40px;
  border-radius: 12px;
  background: rgba(99, 102, 241, 0.1);
  color: var(--accent-main);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
  flex-shrink: 0;
}

.info-text {
  flex: 1;
}

.info-address {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-title);
  line-height: 1.3;
}

.info-coords {
  font-size: 12px;
  color: #a1a1aa;
  font-family: monospace;
  margin-top: 2px;
}

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

/* Step Block Animation */
.step-block {
  opacity: 0;
  transform: translateY(-10px);
  transition: opacity 0.3s ease, transform 0.3s ease;
}

.step-block.visible {
  opacity: 1;
  transform: translateY(0);
  animation: popIn 0.3s forwards;
}

@keyframes popIn {
  from { opacity: 0; transform: translateY(-10px); }
  to { opacity: 1; transform: translateY(0); }
}

/* Segmented Control (Шаг 1) */
.segmented-control {
  display: flex;
  background: rgba(15, 23, 42, 0.04);
  padding: 4px;
  border-radius: 16px;
  gap: 4px;
}

.segment {
  flex: 1;
  position: relative;
  cursor: pointer;
}

.segment input {
  position: absolute;
  opacity: 0;
}

.segment span {
  display: block;
  text-align: center;
  padding: 12px 8px;
  border-radius: 12px;
  font-size: 14px;
  font-weight: 600;
  color: var(--text-muted);
  transition: var(--transition);
}

.segment input:checked + span {
  background: #ffffff;
  color: var(--accent-main);
  box-shadow: 0 2px 8px rgba(0,0,0,0.04);
}

/* Pills Group (Шаг 2) */
.pills-group {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.pill {
  position: relative;
  cursor: pointer;
}

.pill input {
  position: absolute;
  opacity: 0;
}

.pill span {
  display: inline-block;
  padding: 10px 18px;
  border-radius: 99px;
  background: var(--surface-input);
  border: 1.5px solid rgba(255,255,255,0.8);
  font-size: 14px;
  font-weight: 500;
  color: var(--text-body);
  transition: var(--transition);
}

.pill input:checked + span {
  background: #e0e7ff;
  border-color: var(--accent-main);
  color: var(--accent-main);
  font-weight: 600;
}

.pill:hover span {
  background: #ffffff;
}

/* Radio Cards (Шаг 3) */
.radio-cards {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.radio-card {
  display: flex;
  align-items: center;
  padding: 14px 16px;
  background: var(--surface-input);
  border: 1.5px solid rgba(255,255,255,0.8);
  border-radius: 16px;
  cursor: pointer;
  transition: var(--transition);
  position: relative;
}

.radio-card input {
  position: absolute;
  opacity: 0;
}

.rc-circle {
  width: 20px;
  height: 20px;
  border-radius: 50%;
  border: 2px solid #cbd5e1;
  margin-right: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: var(--transition);
}

.rc-circle::after {
  content: '';
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: var(--accent-main);
  transform: scale(0);
  transition: var(--transition);
}

.radio-card:hover {
  background: #ffffff;
  box-shadow: 0 4px 12px rgba(0,0,0,0.03);
}

.radio-card input:checked ~ .rc-circle {
  border-color: var(--accent-main);
}

.radio-card input:checked ~ .rc-circle::after {
  transform: scale(1);
}

.radio-card input:checked {
  background: rgba(99, 102, 241, 0.03);
}

.rc-content {
  flex: 1;
  font-size: 15px;
  font-weight: 500;
  color: var(--text-title);
}

/* Checkboxes (Speed) */
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

/* Price Row */
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
  font-size: 16px;
  font-weight: 600;
  color: var(--text-muted);
}

.price-value {
  font-size: 28px;
  font-weight: 700;
  color: var(--text-title);
  letter-spacing: -0.5px;
  transition: color 0.3s;
}

/* Footer Buttons */
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
  opacity: 0.5;
  pointer-events: none;
}

.btn-submit.active {
  opacity: 1;
  pointer-events: auto;
}

.btn-submit.active:hover {
  transform: translateY(-2px);
  box-shadow: 0 15px 30px -6px rgba(99, 102, 241, 0.6);
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
