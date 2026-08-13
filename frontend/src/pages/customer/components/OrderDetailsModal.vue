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

        <!-- Блок исполнителя -->
        <div class="executor-box">
          <div class="executor-icon">
            <i class="ph-fill ph-user"></i>
          </div>
          <div class="executor-info">
            <div class="executor-title">{{ $t('customer.executorDetails') }}</div>
            <div v-if="selectedOrderDetails.executor_id || selectedOrderDetails.executor_phone" class="executor-details">
              <div v-if="selectedOrderDetails.executor_phone" class="executor-phone">
                📱 {{ selectedOrderDetails.executor_phone }}
              </div>
              <div v-if="selectedOrderDetails.executor_id" class="executor-id">
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
        <button type="button" class="btn-cancel" @click="show = false">
          {{ $t('common.close') }}
        </button>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, computed, onMounted } from 'vue'

export default defineComponent({
  name: 'OrderDetailsModal',
  props: {
    modelValue: { type: Boolean, required: true },
    selectedOrderDetails: { type: Object, default: null },
    currencySymbol: { type: String, default: '₽' },
    formatOrderType: { type: Function, required: true },
    getStatusColor: { type: Function, required: true },
    formatDateFull: { type: Function, required: true },
  },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    const show = computed({
      get: () => props.modelValue,
      set: (val) => emit('update:modelValue', val),
    })

    const getStatusBadgeClass = (status: string) => {
      switch (status) {
        case 'SEARCHING': return 'status-searching'
        case 'ASSIGNED': return 'status-assigned'
        case 'COMPLETED': return 'status-completed'
        case 'CANCELED': return 'status-canceled'
        default: return 'status-default'
      }
    }

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
  justify-content: flex-end;
  border-top: 1px solid rgba(0,0,0,0.06);
  padding-top: 24px;
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
