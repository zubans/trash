<template>
  <div class="transactions-history">
    <h1 class="va-h3 mb-4">Transaction History</h1>

    <!-- Transactions Table -->
    <va-data-table :items="transactions" :columns="columns" :loading="loading">
      <template #cell(type)="{ value }">
        <va-badge :color="getTypeColor(value)">{{ value }}</va-badge>
      </template>

      <template #cell(amount)="{ value }">
        <strong>${{ Number(value).toFixed(2) }}</strong>
      </template>

      <template #cell(order_id)="{ value }">
        <span class="text-secondary text-truncate d-inline-block" style="max-width: 100px;">
          {{ value || '-' }}
        </span>
      </template>

      <template #cell(admin_id)="{ value }">
        <span class="text-secondary text-truncate d-inline-block" style="max-width: 100px;">
          {{ value || '-' }}
        </span>
      </template>

      <template #cell(created_at)="{ value }">
        {{ formatDate(value) }}
      </template>
    </va-data-table>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, onMounted } from 'vue'
import api from '../../services/api'

export default defineComponent({
  name: 'TransactionHistory',
  setup() {
    const transactions = ref([])
    const loading = ref(false)

    const columns = [
      { key: 'user_phone', label: 'User Phone' },
      { key: 'type', label: 'Type' },
      { key: 'amount', label: 'Amount' },
      { key: 'order_id', label: 'Order ID' },
      { key: 'admin_id', label: 'Admin ID' },
      { key: 'created_at', label: 'Processed At' },
    ]

    const fetchTransactions = async () => {
      loading.value = true
      try {
        const response = await api.get('/admin/transactions')
        transactions.value = response.data || []
      } catch (err) {
        console.error('Error fetching transactions:', err)
      } finally {
        loading.value = false
      }
    }

    const getTypeColor = (type: string) => {
      switch (type) {
        case 'TOP_UP':
          return 'success'
        case 'HOLD':
          return 'info'
        case 'PAYMENT':
          return 'primary'
        case 'REWARD':
          return 'success'
        case 'FINE':
          return 'danger'
        case 'REFUND':
          return 'secondary'
        default:
          return 'gray'
      }
    }

    const formatDate = (dateStr: string) => {
      if (!dateStr) return '-'
      const d = new Date(dateStr)
      return d.toLocaleString()
    }

    onMounted(() => {
      fetchTransactions()
    })

    return {
      transactions,
      loading,
      columns,
      getTypeColor,
      formatDate,
    }
  },
})
</script>

<style scoped>
.transactions-history {
  padding: 10px;
}
</style>
