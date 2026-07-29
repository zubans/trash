<template>
  <div class="transactions-history">
    <h1 class="va-h3 mb-4">{{ $t('transactions.title') }}</h1>

    <!-- Transactions Table -->
    <va-data-table :items="transactions" :columns="columns" :loading="loading">
      <template #cell(type)="{ value }">
        <va-badge :color="getTypeColor(value)">{{ value }}</va-badge>
      </template>

      <template #cell(amount)="{ value }">
        <strong>{{ currencySymbol }}{{ Number(value).toFixed(2) }}</strong>
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
import { defineComponent, ref, onMounted, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../../stores/auth-store'
import api from '../../services/api'

export default defineComponent({
  name: 'TransactionHistory',
  setup() {
    const { t } = useI18n()
    const authStore = useAuthStore()
    const transactions = ref([])
    const loading = ref(false)

    const currencySymbol = computed(() => {
      return authStore.currency === 'RUB' ? '₽' : '$'
    })

    const columns = [
      { key: 'user_phone', label: t('transactions.userPhone') },
      { key: 'type', label: t('transactions.type') },
      { key: 'amount', label: t('transactions.amount') },
      { key: 'order_id', label: t('transactions.orderId') },
      { key: 'admin_id', label: t('transactions.adminId') },
      { key: 'created_at', label: t('transactions.processedAt') },
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
