<template>
  <div v-if="show" class="modal-overlay" @click.self="show = false">
    <div class="modal-card">
      <!-- Шапка -->
      <div class="modal-header">
        <div class="modal-title">{{ $t('customer.createNewOrder') }}</div>
        <button type="button" class="btn-close" aria-label="Закрыть" @click="show = false">
          <i class="ph ph-x"></i>
        </button>
      </div>

      <!-- Адрес -->
      <div class="address-box">
        <div class="address-icon"><i class="ph-fill ph-map-pin"></i></div>
        <div class="address-text">
          {{ orderAddress || 'Адрес не указан' }}
          <div v-if="geocodeError" class="text-danger text-xs mt-1">
            {{ geocodeError }}
          </div>
        </div>
      </div>

      <form @submit.prevent="$emit('submitOrder')">
        <!-- ШАГ 1: Овальные кнопки (Категория) -->
        <div class="form-section">
          <div class="section-label">1. Категория</div>
          <div class="style-oval-group">
            <label v-for="cat in categoryOptions" :key="cat.value" class="style-oval-label">
              <input
                type="radio"
                name="category"
                class="custom-radio-input"
                :value="cat.value"
                :checked="categoryIdProxy === cat.value"
                @change="categoryIdProxy = cat.value"
              />
              <div class="style-oval-btn">
                <i :class="getCategoryIcon(cat.label)"></i> {{ cat.label }}
              </div>
            </label>
          </div>
        </div>

        <!-- ШАГ 2: Прямоугольные кнопки (Детали) -->
        <div v-if="subCategoryOptions && subCategoryOptions.length > 0" class="form-section">
          <div class="section-label">2. Детали</div>
          <div class="style-rect-group">
            <label v-for="sub in subCategoryOptions" :key="sub.value" class="style-rect-label">
              <input
                type="radio"
                name="subcategory"
                class="custom-radio-input"
                :value="sub.value"
                :checked="subCategoryIdProxy === sub.value"
                @change="subCategoryIdProxy = sub.value"
              />
              <div class="style-rect-btn">{{ sub.label }}</div>
            </label>
          </div>
        </div>

        <!-- ШАГ 3: Список с радиобатонами (Вид услуги) -->
        <div v-if="variantOptions && variantOptions.length > 0" class="form-section">
          <div class="section-label">3. Вид услуги</div>
          <div class="style-list-group">
            <label v-for="variant in variantOptions" :key="variant.value" class="style-list-label">
              <input
                type="radio"
                name="service_variant"
                class="custom-radio-input"
                :value="variant.value"
                :checked="variantIdProxy === variant.value"
                @change="variantIdProxy = variant.value"
              />
              <div class="style-list-row">
                <span>{{ variant.label }}</span>
                <div class="radio-circle"></div>
              </div>
            </label>
          </div>
        </div>

        <!-- Дополнительные параметры: Срочность -->
        <div v-if="selectedVariantId && !isAuctionSelected" class="form-section mt-2">
          <div class="section-label">Скорость исполнения</div>
          <div class="speed-options-group">
            <label class="speed-option-label">
              <input
                type="checkbox"
                class="custom-radio-input"
                :checked="isUrgent"
                @change="$emit('update:isUrgent', ($event.target as HTMLInputElement).checked)"
              />
              <div class="style-rect-btn speed-btn">
                <i class="ph ph-clock-countdown"></i> {{ $t('customer.urgent') }}
              </div>
            </label>
          </div>
        </div>

        <!-- Поле Комментарий к заказу -->
        <div class="form-section mt-3">
          <div class="section-label">Комментарий к заказу (необязательно)</div>
          <textarea
            :value="orderComment"
            class="form-input comment-textarea"
            rows="3"
            placeholder="Укажите детали (подъезд, этаж, домофон, особые пожелания)..."
            @input="$emit('update:orderComment', ($event.target as HTMLInputElement).value)"
          ></textarea>
        </div>

        <!-- Футер (Цена внутри кнопки) -->
        <div class="modal-footer">
          <button type="button" class="btn-cancel" @click="show = false">Отмена</button>
          <button
            type="submit"
            class="btn-submit"
            :disabled="creatingOrder || !selectedVariantId"
          >
            <span v-if="creatingOrder" class="spinner-sm"></span>
            <template v-else>
              Заказать
              <span class="price-divider">|</span>
              {{ selectedVariantId ? `${Number(selectedPrice).toFixed(0)} ${currencySymbol}` : '-- ₽' }}
            </template>
          </button>
        </div>
      </form>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, computed, onMounted, watch } from 'vue'
import type { PropType } from 'vue'

export default defineComponent({
  name: 'CreateOrderModal',
  props: {
    modelValue: { type: Boolean, required: true },
    orderAddress: { type: String, default: '' },
    // Declared nullable because that is what the dashboard passes: these are
    // refs that start as null until a category is picked or an address is
    // geocoded. Typing them as plain String/Number made every binding a type
    // error, which nothing was checking.
    orderLat: { type: Number as PropType<number | null>, default: null },
    orderLon: { type: Number as PropType<number | null>, default: null },
    geocodeError: { type: String, default: '' },
    selectedCategoryId: { type: String as PropType<string | null>, default: null },
    selectedSubCategoryId: { type: String as PropType<string | null>, default: null },
    selectedVariantId: { type: String as PropType<string | null>, default: null },
    categoryOptions: { type: Array as () => any[], default: () => [] },
    subCategoryOptions: { type: Array as () => any[], default: () => [] },
    variantOptions: { type: Array as () => any[], default: () => [] },
    isAuctionSelected: { type: Boolean, default: false },
    isUrgent: { type: Boolean, default: false },
    orderComment: { type: String, default: '' },
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
    'update:orderComment',
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

    const getCategoryIcon = (label: string) => {
      const lower = (label || '').toLowerCase()
      if (lower.includes('мусор')) return 'ph-fill ph-trash'
      if (lower.includes('собак') || lower.includes('животн')) return 'ph-fill ph-dog'
      if (lower.includes('уборк') || lower.includes('клининг')) return 'ph-fill ph-sparkles'
      if (lower.includes('доставка') || lower.includes('курьер')) return 'ph-fill ph-truck'
      return 'ph-fill ph-wrench'
    }

    watch(show, (val) => {
      if (val) {
        document.body.style.overflow = 'hidden'
      } else {
        document.body.style.overflow = ''
      }
    }, { immediate: true })

    onMounted(() => {
    })

    return {
      show,
      categoryIdProxy,
      subCategoryIdProxy,
      variantIdProxy,
      getCategoryIcon,
    }
  },
})
</script>

<style scoped>
@import url('https://fonts.googleapis.com/css2?family=Outfit:wght@300;400;500;600;700&display=swap');

:root {
  --bg-body: #e2e8f0;
  --surface-card: #ffffff;
  --surface-input: #f8fafc;
  --text-main: #0f172a;
  --text-muted: #64748b;
  --brand-primary: #5c60f5;
  --brand-light: #eef2ff;
  --transition: all 0.2s ease-in-out;
}

.modal-overlay {
  position: fixed;
  top: 0; left: 0; right: 0; bottom: 0;
  background: rgba(15, 23, 42, 0.4);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
  z-index: 1050;
  font-family: 'Outfit', sans-serif;
  color: #0f172a;
}

/* --- Modal Container --- */
.modal-card {
  background: #ffffff;
  border-radius: 24px;
  width: 100%;
  max-width: 420px;
  padding: 20px;
  box-shadow: 0 20px 40px -10px rgba(0,0,0,0.1);
  display: flex;
  flex-direction: column;
  gap: 20px;
  max-height: 90vh;
  overflow-y: auto;
  animation: slideUp 0.3s cubic-bezier(0.16, 1, 0.3, 1);
}

@keyframes slideUp {
  from { opacity: 0; transform: translateY(16px); }
  to { opacity: 1; transform: translateY(0); }
}

/* --- Header --- */
.modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.modal-title {
  font-size: 20px;
  font-weight: 700;
  color: #0f172a;
  letter-spacing: -0.5px;
}

.btn-close {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  border: 1px solid rgba(0,0,0,0.05);
  background: #f8fafc;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 16px;
  color: #64748b;
  cursor: pointer;
  transition: all 0.2s ease-in-out;
}

.btn-close:hover {
  background: #f1f5f9;
  color: #0f172a;
}

/* --- Address Block --- */
.address-box {
  background: #f8fafc;
  border: 1px solid rgba(0,0,0,0.04);
  border-radius: 16px;
  padding: 12px 14px;
  display: flex;
  align-items: center;
  gap: 12px;
}

.address-icon {
  width: 32px;
  height: 32px;
  border-radius: 10px;
  background: #ffffff;
  color: #5c60f5;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  flex-shrink: 0;
  box-shadow: 0 2px 4px rgba(0,0,0,0.02);
}

.address-text {
  font-size: 13px;
  font-weight: 500;
  color: #0f172a;
  line-height: 1.3;
}

/* --- Form Sections --- */
.form-section {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin-bottom: 16px;
}

.section-label {
  font-size: 11px;
  font-weight: 700;
  color: #64748b;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

/* Скрываем стандартные radio/checkbox */
.custom-radio-input {
  position: absolute;
  opacity: 0;
  cursor: pointer;
  height: 0;
  width: 0;
}

/* =========================================
   ВАРИАНТ 1: ОВАЛЬНЫЕ КНОПКИ (Категория)
   ========================================= */
.style-oval-group {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.style-oval-label {
  flex: 1;
  min-width: 120px;
  cursor: pointer;
}

.style-oval-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 10px 12px;
  background: #f8fafc;
  border: 1px solid rgba(0,0,0,0.06);
  border-radius: 99px;
  font-size: 13px;
  font-weight: 600;
  color: #64748b;
  transition: all 0.2s ease-in-out;
}

.style-oval-btn i {
  font-size: 16px;
}

.custom-radio-input:checked + .style-oval-btn {
  background: #eef2ff;
  border-color: #5c60f5;
  color: #5c60f5;
}

/* =========================================
   ВАРИАНТ 2: ПРЯМОУГОЛЬНЫЕ КНОПКИ (Детали)
   ========================================= */
.style-rect-group {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.style-rect-label {
  flex: 1;
  min-width: 100px;
  cursor: pointer;
}

.style-rect-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 10px 12px;
  background: #ffffff;
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  font-size: 13px;
  font-weight: 600;
  color: #0f172a;
  transition: all 0.2s ease-in-out;
}

.custom-radio-input:checked + .style-rect-btn {
  background: #0f172a;
  border-color: #0f172a;
  color: #ffffff;
}

/* Speed options group */
.speed-options-group {
  display: flex;
  gap: 8px;
}

.speed-option-label {
  flex: 1;
  cursor: pointer;
}

.speed-btn {
  background: #f8fafc;
  color: #64748b;
  border-radius: 12px;
}

.custom-radio-input:checked + .speed-btn {
  background: #eef2ff;
  border-color: #5c60f5;
  color: #5c60f5;
}

/* =========================================
   ВАРИАНТ 3: СПИСОК С РАДИОБАТОНАМИ (Услуги)
   ========================================= */
.style-list-group {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.style-list-label {
  cursor: pointer;
}

.style-list-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 14px;
  background: #ffffff;
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  font-size: 14px;
  font-weight: 500;
  color: #0f172a;
  transition: all 0.2s ease-in-out;
}

.radio-circle {
  width: 18px;
  height: 18px;
  border-radius: 50%;
  border: 2px solid #cbd5e1;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.2s ease-in-out;
}

.radio-circle::after {
  content: '';
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #5c60f5;
  transform: scale(0);
  transition: all 0.2s ease-in-out;
}

.custom-radio-input:checked + .style-list-row {
  border-color: #5c60f5;
  background: rgba(92, 96, 245, 0.02);
  font-weight: 600;
}

.custom-radio-input:checked + .style-list-row .radio-circle {
  border-color: #5c60f5;
}

.custom-radio-input:checked + .style-list-row .radio-circle::after {
  transform: scale(1);
}

/* --- Footer (Action Bar) --- */
.modal-footer {
  margin-top: 8px;
  display: flex;
  gap: 8px;
}

.btn-cancel {
  background: #f8fafc;
  color: #0f172a;
  border: 1px solid rgba(0,0,0,0.05);
  padding: 14px 20px;
  border-radius: 14px;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s ease-in-out;
}

.btn-cancel:hover {
  background: #f1f5f9;
}

/* Combined Submit + Price Button */
.btn-submit {
  flex: 1;
  background: #5c60f5;
  color: white;
  border: none;
  padding: 14px 20px;
  border-radius: 14px;
  font-size: 15px;
  font-weight: 600;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  box-shadow: 0 8px 16px rgba(92, 96, 245, 0.25);
  transition: all 0.2s ease-in-out;
}

.btn-submit:disabled {
  opacity: 0.5;
  cursor: not-allowed;
  box-shadow: none;
}

.btn-submit:not(:disabled):hover {
  background: #4f46e5;
  transform: translateY(-1px);
}

.price-divider {
  opacity: 0.5;
  margin: 0 4px;
  font-weight: 400;
}

.spinner-sm {
  width: 18px;
  height: 18px;
  border: 2px solid rgba(255,255,255,0.3);
  border-top-color: #ffffff;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

@media (max-width: 480px) {
  .modal-card {
    padding: 16px;
    border-radius: 20px;
  }
}
.comment-textarea {
  width: 100%;
  padding: 10px 14px;
  border-radius: 12px;
  border: 1px solid #cbd5e1;
  font-family: inherit;
  font-size: 14px;
  resize: vertical;
  outline: none;
  transition: all 0.2s ease;
  background: #f8fafc;
}

.comment-textarea:focus {
  border-color: #6366f1;
  background: #ffffff;
  box-shadow: 0 0 0 3px rgba(99, 102, 241, 0.15);
}
</style>

