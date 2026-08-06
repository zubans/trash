<template>
  <div class="completed-orders">
    <h1 class="va-h3 mb-4">{{ $t('admin.completedOrders') }}</h1>

    <va-data-table :items="orders" :columns="columns" :loading="loading">
      <template #cell(final_amount)="{ value }">
        <strong>{{ currencySymbol }}{{ Number(value).toFixed(2) }}</strong>
      </template>

      <template #cell(executor_phone)="{ value }">
        {{ value || '-' }}
      </template>

      <template #cell(address)="{ value }">
        <span class="text-sm">{{ value || '-' }}</span>
      </template>

      <template #cell(completed_at)="{ value }">
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
  name: 'CompletedOrders',
  setup() {
    const { t } = useI18n()
    const authStore = useAuthStore()
    const orders = ref([])
    const loading = ref(false)

    const currencySymbol = computed(() => {
      return authStore.currency === 'RUB' ? '₽' : '$'
    })

    const columns = computed(() => [
      { key: 'customer_phone', label: t('admin.customerPhone') },
      { key: 'executor_phone', label: t('admin.executorPhone') },
      { key: 'volume_type', label: t('admin.volumeType') },
      { key: 'speed_tariff', label: t('admin.speedTariff') },
      { key: 'final_amount', label: t('admin.finalAmount') },
      { key: 'address', label: t('admin.address') },
      { key: 'completed_at', label: t('admin.completedAt') },
    ])

    const fetchOrders = async () => {
      loading.value = true
      try {
        const response = await api.get('/admin/orders/completed')
        orders.value = response.data || []
      } catch (err) {
        console.error('Error fetching completed orders:', err)
      } finally {
        loading.value = false
      }
    }

    const formatDate = (dateStr: string) => {
      if (!dateStr) return '-'
      const d = new Date(dateStr)
      return d.toLocaleString()
    }

    onMounted(() => {
      fetchOrders()
    })

    return {
      orders,
      loading,
      columns,
      currencySymbol,
      formatDate,
    }
  },
})
</script>

<style scoped>
.completed-orders {
  padding: 10px;
}
</style>
