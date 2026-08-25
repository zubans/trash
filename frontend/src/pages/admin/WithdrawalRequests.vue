<template>
  <div class="withdrawal-requests">
    <h1 class="va-h3 mb-4">{{ $t('withdrawals.title') }}</h1>

    <!-- Requests Table -->
    <va-data-table :items="requests" :columns="columns" :loading="loading" class="mb-4">
      <template #cell(amount)="{ value }">
        <strong style="color: #ef4444;">{{ currencySymbol }}{{ Number(value).toFixed(2) }}</strong>
      </template>

      <template #cell(status)="{ value }">
        <va-badge v-if="value === 'PENDING'" color="warning">{{ $t('topups.pending') }}</va-badge>
        <va-badge v-else-if="value === 'APPROVED'" color="success">{{ $t('topups.approved') }}</va-badge>
        <va-badge v-else color="danger">{{ $t('topups.rejected') }}</va-badge>
      </template>

      <template #cell(created_at)="{ value }">
        {{ formatDate(value) }}
      </template>

      <template #cell(actions)="{ rowData }">
        <div v-if="rowData.status === 'PENDING'" class="actions-container">
          <va-button
            color="success"
            size="small"
            class="mr-2"
            @click="confirmAction(rowData, 'APPROVE')"
          >
            {{ $t('withdrawals.approve') }}
          </va-button>
          <va-button
            color="danger"
            size="small"
            @click="confirmAction(rowData, 'REJECT')"
          >
            {{ $t('withdrawals.reject') }}
          </va-button>
        </div>
        <span v-else>-</span>
      </template>
    </va-data-table>

    <!-- Confirmation Modal -->
    <va-modal
      v-model="showConfirm"
      :title="modalTitle"
      :message="$t('withdrawals.confirmMessage')"
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
  name: 'WithdrawalRequests',
  setup() {
    const { t } = useI18n()
    const authStore = useAuthStore()
    const requests = ref([])
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
      { key: 'user_phone', label: t('withdrawals.userPhone') },
      { key: 'amount', label: t('withdrawals.amount') },
      { key: 'status', label: t('withdrawals.status') },
      { key: 'created_at', label: t('withdrawals.requestedAt') },
      { key: 'actions', label: t('withdrawals.actions') },
    ]

    const fetchRequests = async () => {
      loading.value = true
      try {
        const response = await api.get('/admin/finances/withdrawals')
        requests.value = response.data || []
      } catch (err) {
        console.error('Error fetching withdrawal requests:', err)
      } finally {
        loading.value = false
      }
    }

    const confirmAction = (req: any, type: 'APPROVE' | 'REJECT') => {
      selectedRequest.value = req
      actionType.value = type
      modalTitle.value = type === 'APPROVE' ? t('withdrawals.confirmApprove') : t('withdrawals.confirmReject')
      showConfirm.value = true
    }

    const executeAction = async () => {
      if (!selectedRequest.value) return
      const reqId = selectedRequest.value.id
      const endpoint = actionType.value === 'APPROVE' ? 'approve' : 'reject'

      try {
        await api.post(`/admin/finances/withdrawals/${reqId}/${endpoint}`)
        fetchRequests() // Reload
      } catch (err: any) {
        alert(err.response?.data || t('withdrawals.operationFailed'))
      } finally {
        selectedRequest.value = null
        showConfirm.value = false
      }
    }

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
      loading,
      columns,
      currencySymbol,
      showConfirm,
      modalTitle,
      confirmAction,
      executeAction,
      formatDate,
    }
  },
})
</script>

<style scoped>
.withdrawal-requests {
  padding: 4px;
}

.actions-container {
  display: flex;
  gap: 4px;
}

@media (max-width: 768px) {
  .withdrawal-requests {
    overflow-x: auto;
    -webkit-overflow-scrolling: touch;
  }
}
</style>
