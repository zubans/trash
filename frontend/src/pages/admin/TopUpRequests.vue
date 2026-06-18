<template>
  <div class="topup-requests">
    <h1 class="va-h3 mb-4">Balance Top-Up Requests</h1>

    <!-- Requests Table -->
    <va-data-table :items="requests" :columns="columns" :loading="loading" class="mb-4">
      <template #cell(amount)="{ value }">
        <strong>${{ Number(value).toFixed(2) }}</strong>
      </template>

      <template #cell(status)="{ value }">
        <va-badge v-if="value === 'PENDING'" color="warning">Pending</va-badge>
        <va-badge v-else-if="value === 'APPROVED'" color="success">Approved</va-badge>
        <va-badge v-else color="danger">Rejected</va-badge>
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
            Approve
          </va-button>
          <va-button
            color="danger"
            size="small"
            @click="confirmAction(rowData, 'REJECT')"
          >
            Reject
          </va-button>
        </div>
        <span v-else>-</span>
      </template>
    </va-data-table>

    <!-- Confirmation Modal -->
    <va-modal
      v-model="showConfirm"
      :title="modalTitle"
      message="Are you sure you want to proceed? This action cannot be undone."
      ok-text="Confirm"
      cancel-text="Cancel"
      @ok="executeAction"
    />
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, onMounted } from 'vue'
import api from '../../services/api'

export default defineComponent({
  name: 'TopUpRequests',
  setup() {
    const requests = ref([])
    const loading = ref(false)

    // Modal Control
    const showConfirm = ref(false)
    const selectedRequest = ref<any>(null)
    const actionType = ref<'APPROVE' | 'REJECT'>('APPROVE')
    const modalTitle = ref('')

    const columns = [
      { key: 'user_phone', label: 'User Phone' },
      { key: 'amount', label: 'Amount' },
      { key: 'status', label: 'Status' },
      { key: 'created_at', label: 'Requested At' },
      { key: 'actions', label: 'Actions' },
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
      modalTitle.value = type === 'APPROVE' ? 'Approve Top-Up' : 'Reject Top-Up'
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
        alert(err.response?.data || 'Operation failed')
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
.topup-requests {
  padding: 10px;
}
.actions-container {
  display: flex;
  gap: 8px;
}
</style>
