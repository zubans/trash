<template>
  <div v-if="show" class="modal-overlay" @click.self="show = false">
    <div class="modal-card">
      <!-- Шапка карточки -->
      <div class="modal-header">
        <button type="button" class="btn-close" aria-label="Закрыть" @click="show = false">
          <i class="ph ph-x"></i>
        </button>
        <div class="overline">Карточка заказа</div>
        <div v-if="selectedOrderDetails" class="order-title-row">
          <div class="order-id">#{{ selectedOrderDetails.id }}</div>
          <div :class="['status-badge', getStatusBadgeClass(selectedOrderDetails.status)]">
            {{ selectedOrderDetails.status }}
          </div>
        </div>
      </div>

      <div v-if="selectedOrderDetails">
        <!-- Сетка деталей заказа -->
        <div class="details-grid">
          <div class="detail-item">
            <div class="detail-label">{{ $t('customer.orderType') }}</div>
            <div class="detail-value">{{ formatOrderType(selectedOrderDetails) }}</div>
          </div>
          
          <div class="detail-item">
            <div class="detail-label">{{ $t('customer.created') }}</div>
            <div class="detail-value">{{ formatDateFull(selectedOrderDetails.created_at) }}</div>
          </div>

          <div v-if="selectedOrderDetails.assigned_at" class="detail-item">
            <div class="detail-label">{{ $t('customer.assignedAt') }}</div>
            <div class="detail-value">{{ formatDateFull(selectedOrderDetails.assigned_at) }}</div>
          </div>

          <div v-if="selectedOrderDetails.completed_at" class="detail-item">
            <div class="detail-label">{{ $t('customer.completedAt') }}</div>
            <div class="detail-value">{{ formatDateFull(selectedOrderDetails.completed_at) }}</div>
          </div>

          <div v-if="selectedOrderDetails.canceled_at" class="detail-item">
            <div class="detail-label">{{ $t('customer.canceledAt') }}</div>
            <div class="detail-value">{{ formatDateFull(selectedOrderDetails.canceled_at) }}</div>
          </div>

          <div v-if="selectedOrderDetails.address" class="detail-item full-width">
            <div class="detail-label">{{ $t('customer.pickupAddress') }}</div>
            <div class="detail-value">{{ selectedOrderDetails.address }}</div>
          </div>

          <div class="detail-item full-width">
            <div class="detail-label">{{ $t('customer.totalAmount') }}</div>
            <div class="detail-value price-value">
              {{ Number(selectedOrderDetails.final_amount || selectedOrderDetails.hold_amount).toFixed(2) }} {{ currencySymbol }}
            </div>
          </div>
        </div>

        <!-- Блок отзыва (если уже проставлен) -->
        <div v-if="existingReview" class="review-display-box">
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

        <!-- Блок исполнителя -->
        <div class="executor-box">
          <div class="executor-icon">
            <i class="ph-fill ph-user"></i>
          </div>
          <div class="executor-info">
            <div class="executor-title">{{ $t('customer.executorDetails') }}</div>
            <div v-if="selectedOrderDetails.executor_id || selectedOrderDetails.executor_phone || selectedOrderDetails.executor_name" class="executor-details">
              <div v-if="selectedOrderDetails.executor_name" class="executor-name font-bold">
                👤 {{ selectedOrderDetails.executor_name }}
              </div>
              <div v-if="selectedOrderDetails.executor_phone" class="executor-phone">
                📱 {{ selectedOrderDetails.executor_phone }}
              </div>
              <div v-if="selectedOrderDetails.executor_id" class="executor-id text-xs text-muted">
                ID: {{ selectedOrderDetails.executor_id }}
              </div>
            </div>
            <div v-else class="executor-status">
              {{ $t('customer.notAssigned') }}
            </div>
          </div>
        </div>
      </div>

      <!-- Футер -->
      <div class="modal-footer">
        <button
          v-if="selectedOrderDetails && role === 'CUSTOMER' && (selectedOrderDetails.status === 'SEARCHING' || selectedOrderDetails.status === 'ASSIGNED')"
          type="button"
          class="btn-danger-action"
          @click="confirmCancelOrder"
        >
          <i class="ph ph-trash"></i> Отменить заказ
        </button>
        <button
          v-if="selectedOrderDetails && role === 'EXECUTOR' && selectedOrderDetails.status === 'ASSIGNED'"
          type="button"
          class="btn-danger-action"
          @click="confirmRejectOrder"
        >
          <i class="ph ph-x-circle"></i> Отказаться от заказа
        </button>
        <button
          v-if="selectedOrderDetails && selectedOrderDetails.status === 'COMPLETED'"
          type="button"
          :class="['btn-review-action', { disabled: hasReviewed }]"
          :disabled="hasReviewed"
          @click="!hasReviewed && $emit('open-review-modal', selectedOrderDetails)"
        >
          <i class="ph-fill ph-star"></i>
          {{ hasReviewed ? 'Оценка выставлена' : 'Оценить выполнение' }}
        </button>
        <button type="button" class="btn-cancel" @click="show = false">
          {{ $t('common.close') }}
        </button>
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

    onMounted(() => {
      // Phosphor icons are loaded globally in index.html
    })

    return {
      show,
      hasReviewed,
      existingReview,
      confirmCancelOrder,
      confirmRejectOrder,
      getStatusBadgeClass,
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
  max-width: 520px;
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

/* Header */
.modal-header {
  margin-bottom: 24px;
  position: relative;
}

.btn-close {
  position: absolute;
  top: -8px;
  right: -8px;
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

.overline {
  font-size: 12px;
  font-weight: 700;
  color: var(--accent-main);
  text-transform: uppercase;
  letter-spacing: 1px;
  margin-bottom: 12px;
}

.order-title-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}

.order-id {
  font-size: 18px;
  font-weight: 700;
  color: var(--text-title);
  word-break: break-all;
  line-height: 1.3;
  font-family: monospace;
  background: rgba(15, 23, 42, 0.04);
  padding: 4px 8px;
  border-radius: 8px;
  border: 1px solid rgba(15, 23, 42, 0.05);
}

.status-badge {
  padding: 6px 12px;
  border-radius: 99px;
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.5px;
  white-space: nowrap;
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 4px;
}

.status-searching {
  background: rgba(245, 158, 11, 0.1);
  color: #d97706;
  border: 1px solid rgba(245, 158, 11, 0.2);
}

.status-searching::before {
  content: '';
  display: block;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
  animation: pulse 1.5s infinite;
}

.status-assigned {
  background: rgba(99, 102, 241, 0.1);
  color: #4f46e5;
  border: 1px solid rgba(99, 102, 241, 0.2);
}

.status-executed {
  background: rgba(14, 165, 233, 0.1);
  color: #0284c7;
  border: 1px solid rgba(14, 165, 233, 0.2);
}

.status-completed {
  background: rgba(16, 185, 129, 0.1);
  color: #059669;
  border: 1px solid rgba(16, 185, 129, 0.2);
}

.status-canceled {
  background: rgba(239, 68, 68, 0.1);
  color: #dc2626;
  border: 1px solid rgba(239, 68, 68, 0.2);
}

.status-default {
  background: rgba(148, 163, 184, 0.1);
  color: #475569;
  border: 1px solid rgba(148, 163, 184, 0.2);
}

@keyframes pulse {
  0% { opacity: 1; }
  50% { opacity: 0.4; }
  100% { opacity: 1; }
}

/* Details Grid */
.details-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 20px;
  margin-bottom: 28px;
}

.detail-item.full-width {
  grid-column: 1 / -1;
}

.detail-label {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-muted);
  margin-bottom: 6px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.detail-value {
  font-size: 15px;
  font-weight: 500;
  color: var(--text-title);
  line-height: 1.4;
}

.price-value {
  font-size: 20px;
  font-weight: 700;
  color: var(--accent-main);
}

/* Executor Box */
.executor-box {
  background: rgba(15, 23, 42, 0.02);
  border: 1px dashed rgba(15, 23, 42, 0.15);
  border-radius: var(--rad-md);
  padding: 20px;
  margin-bottom: 32px;
  display: flex;
  align-items: center;
  gap: 16px;
}

.executor-icon {
  width: 48px;
  height: 48px;
  border-radius: 50%;
  background: rgba(15, 23, 42, 0.05);
  color: #94a3b8;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 24px;
}

.executor-info {
  flex: 1;
}

.executor-title {
  font-size: 16px;
  font-weight: 700;
  color: var(--text-title);
  margin-bottom: 2px;
}

.executor-status {
  font-size: 14px;
  color: var(--text-muted);
  font-weight: 500;
}

.executor-details {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-title);
}

.executor-phone {
  color: var(--accent-main);
}

.executor-id {
  font-size: 12px;
  color: var(--text-muted);
  font-weight: 400;
}

/* Footer */
.modal-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  border-top: 1px solid rgba(0,0,0,0.06);
  padding-top: 24px;
}

.btn-danger-action {
  padding: 14px 20px;
  border-radius: 14px;
  background: rgba(239, 68, 68, 0.1);
  color: #ef4444;
  border: 1px solid rgba(239, 68, 68, 0.2);
  font-size: 15px;
  font-weight: 600;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 8px;
  transition: var(--transition);
}

.btn-danger-action:hover {
  background: #ef4444;
  color: #ffffff;
  box-shadow: 0 8px 20px -4px rgba(239, 68, 68, 0.4);
}

.btn-review-action {
  padding: 14px 20px;
  border-radius: 14px;
  background: #eef2ff;
  color: #4f46e5;
  border: 1px solid rgba(99, 102, 241, 0.2);
  font-size: 15px;
  font-weight: 600;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 8px;
  transition: var(--transition);
}

.btn-review-action:hover:not(.disabled) {
  background: #6366f1;
  color: #ffffff;
  box-shadow: 0 8px 20px -4px rgba(99, 102, 241, 0.4);
}

.btn-review-action.disabled {
  background: #f1f5f9;
  color: #94a3b8;
  border-color: #e2e8f0;
  cursor: not-allowed;
  opacity: 0.8;
}

/* Review Display Box */
.review-display-box {
  background: linear-gradient(135deg, rgba(238, 242, 255, 0.6), rgba(243, 244, 246, 0.8));
  border: 1px solid rgba(99, 102, 241, 0.15);
  border-radius: 20px;
  padding: 16px 20px;
  margin-bottom: 20px;
}

.review-display-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
}

.review-stars {
  display: flex;
  gap: 4px;
  color: #cbd5e1;
  font-size: 16px;
}

.review-stars .ph-star.active {
  color: #f59e0b;
}

.review-date {
  font-size: 12px;
  color: var(--text-muted);
}

.review-tags-row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 8px;
}

.review-tag-badge {
  background: #ffffff;
  border: 1px solid rgba(99, 102, 241, 0.2);
  color: #4f46e5;
  font-size: 12px;
  font-weight: 600;
  padding: 4px 10px;
  border-radius: 99px;
}

.review-comment-text {
  font-size: 14px;
  font-style: italic;
  color: var(--text-title);
  line-height: 1.4;
}

.btn-cancel {
  padding: 14px 28px;
  border-radius: 14px;
  background: rgba(15, 23, 42, 0.05);
  color: var(--text-body);
  border: none;
  font-size: 15px;
  font-weight: 600;
  cursor: pointer;
  transition: var(--transition);
}

.btn-cancel:hover {
  background: rgba(15, 23, 42, 0.1);
  color: var(--text-title);
}

@media (max-width: 480px) {
  .modal-card {
    padding: 24px;
    border-radius: 28px;
  }
  .order-title-row {
    flex-direction: column;
    gap: 12px;
  }
  .details-grid {
    grid-template-columns: 1fr;
    gap: 16px;
  }
  .modal-footer {
    justify-content: stretch;
  }
  .btn-cancel {
    width: 100%;
  }
  .order-id {
    font-size: 14px;
  }
}
</style>
