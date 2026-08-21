<template>
  <div v-if="show" class="modal-overlay" @click.self="show = false">
    <div class="modal-card">
      <!-- Кнопка закрытия -->
      <button type="button" class="btn-close" aria-label="Закрыть" @click="show = false">
        <i class="ph ph-x"></i>
      </button>

      <div v-if="selectedOrderDetails" class="modal-body-content">
        <!-- 1. Название, Статус и Цена -->
        <div class="order-header">
          <div class="title-row">
            <h1 class="main-title">{{ getCategoryTitle(selectedOrderDetails) }}</h1>
            <span :class="['status-pill', getStatusBadgeClass(selectedOrderDetails.status)]">
              {{ selectedOrderDetails.status }}
            </span>
          </div>
          <div class="meta-row">
            <div class="order-type">
              <i class="ph-fill ph-package"></i> {{ getVariantTitle(selectedOrderDetails) }}
            </div>
            <div class="order-price">
              {{ Number(selectedOrderDetails.final_amount || selectedOrderDetails.hold_amount).toFixed(2) }} {{ currencySymbol }}
            </div>
          </div>
        </div>

        <div class="divider"></div>

        <!-- 2. Даты в одну строку (grid 1fr 1fr) -->
        <div class="dates-grid">
          <div class="date-block">
            <span class="label">{{ $t('customer.created') }}</span>
            <span class="value">{{ formatDateFull(selectedOrderDetails.created_at) }}</span>
          </div>
          <div v-if="selectedOrderDetails.assigned_at" class="date-block">
            <span class="label">{{ $t('customer.assignedAt') }}</span>
            <span class="value">{{ formatDateFull(selectedOrderDetails.assigned_at) }}</span>
          </div>
          <div v-else-if="selectedOrderDetails.completed_at" class="date-block">
            <span class="label">{{ $t('customer.completedAt') }}</span>
            <span class="value">{{ formatDateFull(selectedOrderDetails.completed_at) }}</span>
          </div>
          <div v-else-if="selectedOrderDetails.canceled_at" class="date-block">
            <span class="label">{{ $t('customer.canceledAt') }}</span>
            <span class="value">{{ formatDateFull(selectedOrderDetails.canceled_at) }}</span>
          </div>
        </div>

        <div class="divider"></div>

        <!-- 3. Адрес -->
        <div v-if="selectedOrderDetails.address" class="address-block">
          <span class="label">{{ $t('customer.pickupAddress') }}</span>
          <div class="address-content">
            <i class="ph-fill ph-map-pin address-icon"></i>
            <span class="address-text">{{ selectedOrderDetails.address }}</span>
          </div>
        </div>

        <!-- Блок отзыва (если уже проставлен) -->
        <div v-if="existingReview" class="review-display-box mt-3">
          <div class="review-display-header">
            <div class="review-stars">
              <i
                v-for="star in 5"
                :key="star"
                :class="[star <= existingReview.rating ? 'ph-fill ph-star active' : 'ph ph-star']"
              ></i>
            </div>
            <span class="review-date">{{ formatDateFull(existingReview.created_at) }}</span>
          </div>
          <div v-if="existingReview.tags && existingReview.tags.length > 0" class="review-tags-row">
            <span v-for="tag in existingReview.tags" :key="tag" class="review-tag-badge">
              {{ tag }}
            </span>
          </div>
          <div v-if="existingReview.comment" class="review-comment-text">
            «{{ existingReview.comment }}»
          </div>
        </div>

        <!-- 4. Исполнитель (Компактная карточка) -->
        <div class="executor-card">
          <div class="exec-left">
            <div class="exec-avatar">
              <i class="ph-fill ph-user"></i>
            </div>
            <div class="exec-info">
              <span class="exec-label">{{ $t('customer.executorDetails') }}</span>
              <span class="exec-name">
                {{ selectedOrderDetails.executor_name || $t('customer.notAssigned') }}
              </span>
            </div>
          </div>
        </div>

        <!-- Действия / кнопки -->
        <div class="modal-actions-row">
          <button
            v-if="role === 'CUSTOMER' && (selectedOrderDetails.status === 'SEARCHING' || selectedOrderDetails.status === 'ASSIGNED')"
            type="button"
            class="btn-danger-action"
            @click="confirmCancelOrder"
          >
            <i class="ph ph-trash"></i> Отменить заказ
          </button>
          <button
            v-if="role === 'EXECUTOR' && selectedOrderDetails.status === 'ASSIGNED'"
            type="button"
            class="btn-danger-action"
            @click="confirmRejectOrder"
          >
            <i class="ph ph-x-circle"></i> Отказаться от заказа
          </button>
          <button
            v-if="selectedOrderDetails.status === 'COMPLETED'"
            type="button"
            :class="['btn-review-action', { disabled: hasReviewed }]"
            :disabled="hasReviewed"
            @click="!hasReviewed && $emit('open-review-modal', selectedOrderDetails)"
          >
            <i class="ph-fill ph-star"></i>
            {{ hasReviewed ? 'Оценка выставлена' : 'Оценить выполнение' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, computed, watch, onMounted } from 'vue'
import { checkMyOrderReview, type OrderReview } from '../../../api/review'

export default defineComponent({
  name: 'OrderDetailsModal',
  props: {
    modelValue: { type: Boolean, required: true },
    selectedOrderDetails: { type: Object, default: null },
    currencySymbol: { type: String, default: '₽' },
    formatOrderType: { type: Function, required: true },
    getStatusColor: { type: Function, required: true },
    formatDateFull: { type: Function, required: true },
    role: { type: String, default: 'CUSTOMER' },
  },
  emits: ['update:modelValue', 'cancel-order', 'reject-order', 'open-review-modal'],
  setup(props, { emit }) {
    const show = computed({
      get: () => props.modelValue,
      set: (val) => emit('update:modelValue', val),
    })

    watch(show, (val) => {
      if (val) {
        document.body.style.overflow = 'hidden'
      } else {
        document.body.style.overflow = ''
      }
    }, { immediate: true })

    const hasReviewed = ref(false)
    const existingReview = ref<OrderReview | null>(null)

    const fetchOrderReview = async (orderId: string) => {
      hasReviewed.value = false
      existingReview.value = null
      if (!orderId) return
      try {
        const res = await checkMyOrderReview(orderId)
        if (res && res.has_reviewed && res.review) {
          hasReviewed.value = true
          existingReview.value = res.review
        }
      } catch (err) {
        console.warn('Failed to check review for order:', orderId, err)
      }
    }

    watch(
      () => props.selectedOrderDetails,
      (order) => {
        if (order && order.status === 'COMPLETED') {
          fetchOrderReview(order.id)
        } else {
          hasReviewed.value = false
          existingReview.value = null
        }
      },
      { immediate: true }
    )

    const confirmCancelOrder = () => {
      if (!props.selectedOrderDetails) return
      if (confirm('Вы уверены, что хотите отменить этот заказ?')) {
        emit('cancel-order', props.selectedOrderDetails.id)
        show.value = false
      }
    }

    const confirmRejectOrder = () => {
      if (!props.selectedOrderDetails) return
      if (confirm('Вы уверены, что хотите отказаться от выполнения этого заказа?')) {
        emit('reject-order', props.selectedOrderDetails.id)
        show.value = false
      }
    }

    const getStatusBadgeClass = (status: string) => {
      switch (status) {
        case 'SEARCHING': return 'status-searching'
        case 'ASSIGNED': return 'status-assigned'
        case 'EXECUTED': return 'status-executed'
        case 'COMPLETED': return 'status-completed'
        case 'CANCELED': return 'status-canceled'
        default: return 'status-default'
      }
    }

    const getCategoryTitle = (order: any) => {
      if (!order) return 'Вывоз мусора'
      const formatted = props.formatOrderType(order)
      if (typeof formatted === 'string' && formatted.includes('(')) {
        return formatted.split('(')[0].trim()
      }
      return 'Вывоз мусора'
    }

    const getVariantTitle = (order: any) => {
      if (!order) return 'Услуга'
      const formatted = props.formatOrderType(order)
      if (typeof formatted === 'string' && formatted.includes('(')) {
        const match = formatted.match(/\(([^)]+)\)/)
        if (match && match[1]) return match[1].trim()
      }
      return formatted
    }

    onMounted(() => {
      // Phosphor icons loaded globally
    })

    return {
      show,
      hasReviewed,
      existingReview,
      confirmCancelOrder,
      confirmRejectOrder,
      getStatusBadgeClass,
      getCategoryTitle,
      getVariantTitle,
    }
  },
})
</script>

<style scoped>
@import url('https://fonts.googleapis.com/css2?family=Outfit:wght@300;400;500;600;700&family=JetBrains+Mono:wght@500;700&display=swap');

.modal-overlay {
  --bg-body: #e2e8f0;
  --surface-card: #ffffff;
  
  --text-main: #0f172a;
  --text-muted: #64748b;
  
  --brand-primary: #5c60f5;
  --brand-light: #eef2ff;
  
  --success-main: #10b981;
  --success-bg: #ecfdf5;
  
  --info-main: #0ea5e9;
  --info-bg: #e0f2fe;
  
  --rad-md: 16px;
  --rad-lg: 24px;
  
  --transition: all 0.2s ease-in-out;

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
  color: var(--text-main);
}

.modal-card {
  background: var(--surface-card);
  border-radius: var(--rad-lg);
  width: 100%;
  max-width: 400px;
  padding: 24px 20px;
  box-shadow: 0 20px 40px -10px rgba(0,0,0,0.1);
  display: flex;
  flex-direction: column;
  gap: 20px;
  position: relative;
  max-height: 90vh;
  overflow-y: auto;
  animation: slideUp 0.3s ease-out;
}

@keyframes slideUp {
  from { opacity: 0; transform: translateY(16px); }
  to { opacity: 1; transform: translateY(0); }
}

/* Кнопка закрытия */
.btn-close {
  position: absolute;
  top: 16px; right: 16px;
  width: 32px; height: 32px; border-radius: 50%;
  background: #f8fafc; border: 1px solid rgba(0,0,0,0.05);
  display: flex; align-items: center; justify-content: center;
  font-size: 16px; color: var(--text-muted); cursor: pointer;
  transition: var(--transition);
  z-index: 10;
}

.btn-close:hover {
  background: #f1f5f9;
  color: #ef4444;
}

.modal-body-content {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

/* --- 1. Шапка (Название, Статус, Цена) --- */
.order-header {
  display: flex; flex-direction: column; gap: 8px;
  padding-right: 32px; /* Место под крестик */
}

.title-row {
  display: flex; align-items: center; gap: 10px; flex-wrap: wrap;
}

.main-title {
  font-size: 20px; font-weight: 700; color: var(--text-main); line-height: 1.2; letter-spacing: -0.5px;
}

.status-pill {
  padding: 4px 10px; border-radius: 99px;
  font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.5px;
}

.status-searching {
  background: #fef3c7; color: #d97706;
}

.status-assigned {
  background: #e0e7ff; color: #4f46e5;
}

.status-executed {
  background: var(--info-bg); color: var(--info-main);
}

.status-completed {
  background: var(--success-bg); color: var(--success-main);
}

.status-canceled {
  background: #fef2f2; color: #ef4444;
}

.status-default {
  background: #f1f5f9; color: #64748b;
}

.meta-row {
  display: flex; align-items: center; justify-content: space-between;
  margin-top: 4px;
}

.order-type {
  font-size: 14px; font-weight: 500; color: var(--text-muted);
  display: flex; align-items: center; gap: 6px;
}

.order-price {
  font-size: 20px; font-weight: 700; color: var(--brand-primary);
  font-family: 'JetBrains Mono', monospace; letter-spacing: -0.5px;
}

/* --- Разделитель --- */
.divider {
  height: 1px; background: rgba(0,0,0,0.06); width: 100%;
}

/* --- 2. Даты (В одну строку) --- */
.dates-grid {
  display: grid; grid-template-columns: 1fr 1fr; gap: 16px;
}

.date-block { display: flex; flex-direction: column; gap: 4px; }
.label { font-size: 11px; font-weight: 700; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px; }
.value { font-family: 'JetBrains Mono', monospace; font-size: 13px; font-weight: 500; color: var(--text-main); }

/* --- 3. Адрес --- */
.address-block { display: flex; flex-direction: column; gap: 6px; }
.address-content {
  display: flex; align-items: flex-start; gap: 10px;
}
.address-icon {
  color: var(--brand-primary); font-size: 18px; margin-top: 2px;
}
.address-text {
  font-size: 14px; font-weight: 500; color: var(--text-main); line-height: 1.4;
}

/* --- 4. Информация об исполнителе (Компактная) --- */
.executor-card {
  background: #f8fafc; border: 1px solid rgba(0,0,0,0.06);
  border-radius: var(--rad-md); padding: 12px 16px;
  display: flex; align-items: center; justify-content: space-between; gap: 12px;
}

.exec-left { display: flex; align-items: center; gap: 12px; flex: 1; min-width: 0; }

.exec-avatar {
  width: 40px; height: 40px; border-radius: 12px;
  background: #e2e8f0; color: var(--text-muted);
  display: flex; align-items: center; justify-content: center; font-size: 20px; flex-shrink: 0;
}

.exec-info { display: flex; flex-direction: column; overflow: hidden; }
.exec-label { font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px;}
.exec-name { font-size: 15px; font-weight: 700; color: var(--text-main); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

/* Review Display Box */
.review-display-box {
  background: #f8fafc;
  border: 1px solid rgba(99, 102, 241, 0.15);
  border-radius: var(--rad-md);
  padding: 12px 16px;
}

.review-display-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 6px;
}

.review-stars {
  display: flex;
  gap: 4px;
  color: #cbd5e1;
  font-size: 14px;
}

.review-stars .ph-star.active {
  color: #f59e0b;
}

.review-date {
  font-size: 11px;
  color: var(--text-muted);
}

.review-tags-row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 6px;
}

.review-tag-badge {
  background: #ffffff;
  border: 1px solid rgba(99, 102, 241, 0.2);
  color: #4f46e5;
  font-size: 11px;
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 99px;
}

.review-comment-text {
  font-size: 13px;
  font-style: italic;
  color: var(--text-main);
  line-height: 1.4;
}

/* Действия */
.modal-actions-row {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 4px;
}

.btn-danger-action {
  padding: 12px 16px;
  border-radius: 12px;
  background: #fef2f2;
  color: #ef4444;
  border: 1px solid rgba(239, 68, 68, 0.2);
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  transition: var(--transition);
}

.btn-danger-action:hover {
  background: #ef4444;
  color: #ffffff;
}

.btn-review-action {
  padding: 12px 16px;
  border-radius: 12px;
  background: var(--brand-light);
  color: var(--brand-primary);
  border: 1px solid rgba(92, 96, 245, 0.2);
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  transition: var(--transition);
}

.btn-review-action:hover:not(.disabled) {
  background: var(--brand-primary);
  color: #ffffff;
}

.btn-review-action.disabled {
  background: #f1f5f9;
  color: #94a3b8;
  border-color: #e2e8f0;
  cursor: not-allowed;
  opacity: 0.8;
}
</style>
