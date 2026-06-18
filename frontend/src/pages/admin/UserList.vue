<template>
  <div class="user-list">
    <h1 class="va-h3 mb-4">User Management</h1>

    <!-- Filters and Search -->
    <div class="row g-3 mb-4 align-items-end">
      <div class="col-md-4">
        <va-input v-model="searchQuery" placeholder="Search by phone..." label="Search" @input="debouncedFetch" />
      </div>
      <div class="col-md-3">
        <va-select v-model="selectedRole" :options="roleOptions" label="Role" @update:modelValue="fetchUsers" />
      </div>
      <div class="col-md-3">
        <va-select v-model="selectedStatus" :options="statusOptions" label="Status" @update:modelValue="fetchUsers" />
      </div>
      <div class="col-md-2">
        <va-button color="secondary" outline @click="clearFilters">Clear</va-button>
      </div>
    </div>

    <!-- Data Table -->
    <va-data-table :items="users" :columns="columns" :loading="loading" class="mb-4">
      <template #cell(balance)="{ value }">
        <strong>${{ Number(value).toFixed(2) }}</strong>
      </template>

      <template #cell(created_at)="{ value }">
        {{ formatDate(value) }}
      </template>

      <template #cell(actions)="{ rowData }">
        <va-button
          v-if="rowData.status === 'ACTIVE'"
          color="danger"
          size="small"
          @click="toggleUserStatus(rowData)"
        >
          Ban
        </va-button>
        <va-button
          v-else
          color="success"
          size="small"
          @click="toggleUserStatus(rowData)"
        >
          Activate
        </va-button>
      </template>
    </va-data-table>

    <!-- Pagination -->
    <div class="d-flex justify-content-between align-items-center">
      <span>Total: {{ totalUsers }} users</span>
      <va-pagination
        v-model="page"
        :pages="totalPages"
        :visible-pages="5"
        @update:modelValue="fetchUsers"
      />
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, onMounted, computed } from 'vue'
import api from '../../services/api'

export default defineComponent({
  name: 'UserList',
  setup() {
    const users = ref([])
    const totalUsers = ref(0)
    const page = ref(1)
    const limit = ref(10)
    const loading = ref(false)

    // Search and Filters
    const searchQuery = ref('')
    const selectedRole = ref('')
    const selectedStatus = ref('')

    const roleOptions = [
      { text: 'All', value: '' },
      { text: 'Customer', value: 'CUSTOMER' },
      { text: 'Executor', value: 'EXECUTOR' },
      { text: 'Admin', value: 'ADMIN' },
    ]

    const statusOptions = [
      { text: 'All', value: '' },
      { text: 'Active', value: 'ACTIVE' },
      { text: 'Banned', value: 'BANNED' },
    ]

    const columns = [
      { key: 'phone', label: 'Phone', sortable: true },
      { key: 'role', label: 'Role', sortable: true },
      { key: 'balance', label: 'Balance', sortable: true },
      { key: 'status', label: 'Status', sortable: true },
      { key: 'created_at', label: 'Joined At', sortable: true },
      { key: 'actions', label: 'Actions' },
    ]

    const totalPages = computed(() => Math.ceil(totalUsers.value / limit.value) || 1)

    const fetchUsers = async () => {
      loading.value = true
      try {
        const response = await api.get('/admin/users', {
          params: {
            page: page.value,
            limit: limit.value,
            search: searchQuery.value,
            role: typeof selectedRole.value === 'object' ? (selectedRole.value as any).value : selectedRole.value,
            status: typeof selectedStatus.value === 'object' ? (selectedStatus.value as any).value : selectedStatus.value,
          },
        })
        users.value = response.data.users || []
        totalUsers.value = response.data.total || 0
      } catch (err) {
        console.error('Error fetching users:', err)
      } finally {
        loading.value = false
      }
    }

    let debounceTimeout: any = null
    const debouncedFetch = () => {
      clearTimeout(debounceTimeout)
      debounceTimeout = setTimeout(() => {
        page.value = 1
        fetchUsers()
      }, 300)
    }

    const clearFilters = () => {
      searchQuery.value = ''
      selectedRole.value = ''
      selectedStatus.value = ''
      page.value = 1
      fetchUsers()
    }

    const toggleUserStatus = async (user: any) => {
      const newStatus = user.status === 'ACTIVE' ? 'BANNED' : 'ACTIVE'
      try {
        await api.post(`/admin/users/${user.id}/status`, { status: newStatus })
        user.status = newStatus // reactive update
      } catch (err) {
        alert('Failed to update user status')
        console.error(err)
      }
    }

    const formatDate = (dateStr: string) => {
      if (!dateStr) return '-'
      const d = new Date(dateStr)
      return d.toLocaleString()
    }

    onMounted(() => {
      fetchUsers()
    })

    return {
      users,
      totalUsers,
      page,
      limit,
      loading,
      searchQuery,
      selectedRole,
      selectedStatus,
      roleOptions,
      statusOptions,
      columns,
      totalPages,
      fetchUsers,
      debouncedFetch,
      clearFilters,
      toggleUserStatus,
      formatDate,
    }
  },
})
</script>

<style scoped>
.user-list {
  padding: 10px;
}
.row {
  display: flex;
  flex-wrap: wrap;
  gap: 15px;
}
.col-md-4 {
  flex: 0 0 calc(33.333% - 10px);
}
.col-md-3 {
  flex: 0 0 calc(25% - 11px);
}
.col-md-2 {
  flex: 0 0 calc(16.666% - 12px);
}
.d-flex {
  display: flex;
}
.justify-content-between {
  justify-content: space-between;
}
.align-items-center {
  align-items: center;
}
</style>
