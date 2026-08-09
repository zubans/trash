<template>
  <div class="active-orders">
    <h1 class="va-h3 mb-4">{{ $t('admin.activeOrders') }}</h1>

    <va-data-table :items="orders" :columns="columns" :loading="loading">
      <template #cell(status)="{ value }">
        <va-badge :color="getStatusColor(value)">{{ value }}</va-badge>
      </template>

      <template #cell(hold_amount)="{ value }">
        <strong>{{ currencySymbol }}{{ Number(value).toFixed(2) }}</strong>
      </template>

      <template #cell(executor_phone)="{ value }">
        {{ value || '-' }}
      </template>

      <template #cell(address)="{ value }">
        <span class="text-sm">{{ value || '-' }}</span>
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
  name: 'ActiveOrders',
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
      { key: 'service_variant_name', label: t('admin.serviceType') },
      { key: 'is_urgent', label: t('admin.urgent') },
      { key: 'is_asap', label: t('admin.asap') },
      { key: 'status', label: t('admin.status') },
      { key: 'hold_amount', label: t('admin.holdAmount') },
      { key: 'address', label: t('admin.address') },
      { key: 'created_at', label: t('admin.createdAt') },
    ])

    const fetchOrders = async () => {
      loading.value = true
      try {
        const response = await api.get('/admin/orders/active')
        orders.value = response.data || []
      } catch (err) {
        console.error('Error fetching active orders:', err)
      } finally {
        loading.value = false
      }
    }

    const getStatusColor = (status: string) => {
      switch (status) {
        case 'SEARCHING':
          return 'warning'
        case 'ASSIGNED':
          return 'info'
        case 'COMPLETED':
          return 'success'
        case 'CANCELED':
          return 'danger'
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
      fetchOrders()
    })

    return {
      orders,
      loading,
      columns,
      currencySymbol,
      getStatusColor,
      formatDate,
    }
  },
})
</script>

<style scoped>
.active-orders {
  padding: 10px;
}
</style>
