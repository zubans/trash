<template>
  <div class="topup-requests">
    <!-- Table Toolbar -->
    <div class="table-toolbar mb-4">
      <div class="search-box">
        <i class="ph ph-magnifying-glass"></i>
        <input
          v-model="searchQuery"
          type="text"
          placeholder="Поиск по телефону..."
        />
      </div>

      <div class="filters">
        <button class="btn-filter" title="Обновить" @click="fetchRequests">
          <i class="ph-bold ph-arrows-clockwise"></i>
        </button>
      </div>
    </div>

    <!-- Requests Table -->
    <va-data-table :items="filteredRequests" :columns="columns" :loading="loading" class="mb-4">
      <template #cell(user_phone)="{ value }">
        <div class="cell-phone">
          <div class="user-avatar">U</div>
          <span>{{ value }}</span>
        </div>
      </template>

      <template #cell(amount)="{ value }">
        <span class="cell-amount">{{ currencySymbol }}{{ Number(value).toFixed(2) }}</span>
      </template>

      <template #cell(status)="{ value }">
        <span v-if="value === 'PENDING'" class="status-pill warning">{{ $t('topups.pending') }}</span>
        <span v-else-if="value === 'APPROVED'" class="status-pill success">{{ $t('topups.approved') }}</span>
        <span v-else class="status-pill danger">{{ $t('topups.rejected') }}</span>
      </template>

      <template #cell(created_at)="{ value }">
        <div class="cell-date">
          <span>{{ formatDate(value).split(',')[0] }}</span>
          <span class="date-time">{{ formatDate(value).split(',')[1] || '' }}</span>
        </div>
      </template>

      <template #cell(actions)="{ rowData }">
        <div v-if="rowData.status === 'PENDING'" class="cell-actions">
          <button class="btn-icon approve" title="Одобрить" @click="confirmAction(rowData, 'APPROVE')">
            <i class="ph-bold ph-check"></i>
          </button>
          <button class="btn-icon reject" title="Отклонить" @click="confirmAction(rowData, 'REJECT')">
            <i class="ph-bold ph-x"></i>
          </button>
        </div>
        <span v-else class="empty-dash">-</span>
      </template>
    </va-data-table>

    <!-- Confirmation Modal -->
    <va-modal
      v-model="showConfirm"
      :title="modalTitle"
      :message="$t('topups.confirmMessage')"
      :ok-text="$t('common.confirm')"
      :cancel-text="$t('common.cancel')"
      @ok="executeAction"
    />
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, onMounted, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../../stores/auth-store'
import api from '../../services/api'

export default defineComponent({
  name: 'TopUpRequests',
  setup() {
    const { t } = useI18n()
    const authStore = useAuthStore()
    const requests = ref<any[]>([])
    const loading = ref(false)

    const currencySymbol = computed(() => {
      return authStore.currency === 'RUB' ? '₽' : '$'
    })

    // Modal Control
    const showConfirm = ref(false)
    const selectedRequest = ref<any>(null)
    const actionType = ref<'APPROVE' | 'REJECT'>('APPROVE')
    const modalTitle = ref('')

    const columns = [
      { key: 'user_phone', label: t('topups.userPhone') },
      { key: 'amount', label: t('topups.amount') },
      { key: 'status', label: t('topups.status') },
      { key: 'created_at', label: t('topups.requestedAt') },
      { key: 'actions', label: t('topups.actions') },
    ]

    const fetchRequests = async () => {
      loading.value = true
      try {
        const response = await api.get('/admin/finances/topups')
        requests.value = response.data || []
      } catch (err) {
        console.error('Error fetching requests:', err)
      } finally {
        loading.value = false
      }
    }

    const confirmAction = (req: any, type: 'APPROVE' | 'REJECT') => {
      selectedRequest.value = req
      actionType.value = type
      modalTitle.value = type === 'APPROVE' ? t('topups.confirmApprove') : t('topups.confirmReject')
      showConfirm.value = true
    }

    const executeAction = async () => {
      if (!selectedRequest.value) return
      const reqId = selectedRequest.value.id
      const endpoint = actionType.value === 'APPROVE' ? 'approve' : 'reject'

      try {
        await api.post(`/admin/finances/topups/${reqId}/${endpoint}`)
        fetchRequests() // Reload
      } catch (err: any) {
        alert(err.response?.data || t('topups.operationFailed'))
      } finally {
        selectedRequest.value = null
        showConfirm.value = false
      }
    }

    const searchQuery = ref('')

    const filteredRequests = computed(() => {
      if (!searchQuery.value.trim()) return requests.value
      const q = searchQuery.value.toLowerCase().trim()
      return requests.value.filter((r: any) =>
        (r.user_phone || '').toLowerCase().includes(q)
      )
    })

    const formatDate = (dateStr: string) => {
      if (!dateStr) return '-'
      const d = new Date(dateStr)
      return d.toLocaleString()
    }

    onMounted(() => {
      fetchRequests()
    })

    return {
      requests,
      searchQuery,
      filteredRequests,
      loading,
      columns,
      currencySymbol,
      showConfirm,
      modalTitle,
      fetchRequests,
      confirmAction,
      executeAction,
      formatDate,
    }
  },
})
</script>

<style scoped>
.topup-requests {
  display: flex;
  flex-direction: column;
}

.table-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
}

.search-box {
  display: flex;
  align-items: center;
  gap: 12px;
  background: #f8fafc;
  border: 1px solid rgba(0, 0, 0, 0.04);
  border-radius: 12px;
  padding: 10px 16px;
  width: 320px;
  transition: all 0.2s ease-in-out;
}

.search-box:focus-within {
  background: #ffffff;
  border-color: #5c60f5;
  box-shadow: 0 0 0 3px rgba(92, 96, 245, 0.1);
}

.search-box i {
  color: #64748b;
  font-size: 18px;
}

.search-box input {
  border: none;
  background: transparent;
  outline: none;
  font-family: inherit;
  font-size: 14px;
  color: #0f172a;
  width: 100%;
}

.filters {
  display: flex;
  gap: 8px;
}

.btn-filter {
  background: #ffffff;
  border: 1px solid rgba(0,0,0,0.08);
  border-radius: 10px;
  width: 36px;
  height: 36px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #0f172a;
  cursor: pointer;
  transition: all 0.2s ease-in-out;
}

.btn-filter:hover {
  background: #f8fafc;
  border-color: rgba(0,0,0,0.15);
}

.cell-phone {
  font-size: 15px;
  font-weight: 600;
  color: #0f172a;
  display: flex;
  align-items: center;
  gap: 12px;
}

.user-avatar {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: #eef2ff;
  color: #5c60f5;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
  font-weight: 700;
  flex-shrink: 0;
}

.cell-amount {
  font-family: 'JetBrains Mono', monospace;
  font-size: 15px;
  font-weight: 700;
  color: #0f172a;
}

.cell-date {
  font-family: 'JetBrains Mono', monospace;
  font-size: 13px;
  color: #64748b;
  display: flex;
  flex-direction: column;
}

.date-time {
  font-size: 11px;
  opacity: 0.7;
}

.status-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  border-radius: 99px;
  font-size: 12px;
  font-weight: 600;
}

.status-pill::before {
  content: '';
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
}

.status-pill.success {
  background: #ecfdf5;
  color: #10b981;
}

.status-pill.warning {
  background: #fffbeb;
  color: #f59e0b;
}

.status-pill.danger {
  background: #fef2f2;
  color: #ef4444;
}

.cell-actions {
  display: flex;
  gap: 8px;
  justify-content: flex-start;
}

.btn-icon {
  width: 32px;
  height: 32px;
  border-radius: 8px;
  border: none;
  background: transparent;
  color: #64748b;
  font-size: 18px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.2s ease-in-out;
}

.btn-icon.approve:hover {
  background: #ecfdf5;
  color: #10b981;
}

.btn-icon.reject:hover {
  background: #fef2f2;
  color: #ef4444;
}

.empty-dash {
  color: #64748b;
  font-weight: 500;
}

@media (max-width: 768px) {
  .table-toolbar {
    flex-direction: column;
    align-items: stretch;
    gap: 12px;
  }
  .search-box {
    width: 100%;
  }
  .topup-requests {
    overflow-x: auto;
    -webkit-overflow-scrolling: touch;
  }
}
</style>
