<template>
  <div class="active-shifts">
    <h1 class="va-h3 mb-4">{{ $t('admin.activeShifts') }}</h1>

    <va-data-table :items="shifts" :columns="columns" :loading="loading">
      <template #cell(status)="{ value }">
        <va-badge :color="getStatusColor(value)">{{ value }}</va-badge>
      </template>

      <template #cell(started_at)="{ value }">
        {{ formatDate(value) }}
      </template>

      <template #cell(planned_end_at)="{ value }">
        {{ formatDate(value) }}
      </template>
    </va-data-table>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, onMounted, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import api from '../../services/api'

export default defineComponent({
  name: 'ActiveShifts',
  setup() {
    const { t } = useI18n()
    const shifts = ref([])
    const loading = ref(false)

    const columns = computed(() => [
      { key: 'executor_phone', label: t('admin.executorPhone') },
      { key: 'duration_hours', label: t('admin.durationHours') },
      { key: 'started_at', label: t('admin.startedAt') },
      { key: 'planned_end_at', label: t('admin.plannedEndAt') },
      { key: 'status', label: t('admin.status') },
    ])

    const fetchShifts = async () => {
      loading.value = true
      try {
        const response = await api.get('/admin/shifts/active')
        shifts.value = response.data || []
      } catch (err) {
        console.error('Error fetching active shifts:', err)
      } finally {
        loading.value = false
      }
    }

    const getStatusColor = (status: string) => {
      switch (status) {
        case 'ACTIVE':
          return 'success'
        case 'COMPLETED':
          return 'info'
        case 'PENALIZED':
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
      fetchShifts()
    })

    return {
      shifts,
      loading,
      columns,
      getStatusColor,
      formatDate,
    }
  },
})
</script>

<style scoped>
.active-shifts {
  padding: 10px;
}
</style>
