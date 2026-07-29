<template>
  <div class="user-list">
    <h1 class="va-h3 mb-4">{{ $t('users.title') }}</h1>

    <!-- Filters and Search -->
    <div class="row g-3 mb-4 align-items-end">
      <div class="col-md-4">
        <va-input v-model="searchQuery" :placeholder="$t('users.searchPlaceholder')" :label="$t('users.search')" @input="debouncedFetch" />
      </div>
      <div class="col-md-3">
        <va-select v-model="selectedRole" :options="roleOptions" :label="$t('users.role')" @update:modelValue="fetchUsers" />
      </div>
      <div class="col-md-3">
        <va-select v-model="selectedStatus" :options="statusOptions" :label="$t('users.status')" @update:modelValue="fetchUsers" />
      </div>
      <div class="col-md-2">
        <va-button color="secondary" outline @click="clearFilters">{{ $t('users.clear') }}</va-button>
      </div>
    </div>

    <!-- Data Table -->
    <va-data-table :items="users" :columns="columns" :loading="loading" class="mb-4">
      <template #cell(balance)="{ value }">
        <strong>{{ currencySymbol }}{{ Number(value).toFixed(2) }}</strong>
      </template>

      <template #cell(address)="{ value }">
        <span class="text-sm">{{ value || '-' }}</span>
      </template>

      <template #cell(created_at)="{ value }">
        {{ formatDate(value) }}
      </template>

      <template #cell(actions)="{ rowData }">
        <div class="actions-container">
          <va-button
            color="primary"
            size="small"
            class="mr-2"
            @click="openTopUpModal(rowData)"
          >
            {{ $t('users.topUp') }}
          </va-button>
          <va-button
            color="info"
            size="small"
            class="mr-2"
            @click="openRoleModal(rowData)"
          >
            {{ $t('users.roleBtn') }}
          </va-button>
          <va-button
            color="secondary"
            size="small"
            class="mr-2"
            @click="openAddressModal(rowData)"
          >
            {{ $t('users.addressBtn') }}
          </va-button>
          <va-button
            v-if="rowData.status === 'ACTIVE'"
            color="danger"
            size="small"
            @click="toggleUserStatus(rowData)"
          >
            {{ $t('users.ban') }}
          </va-button>
          <va-button
            v-else
            color="success"
            size="small"
            @click="toggleUserStatus(rowData)"
          >
            {{ $t('users.activate') }}
          </va-button>
        </div>
      </template>
    </va-data-table>

    <!-- Pagination -->
    <div class="d-flex justify-content-between align-items-center">
      <span>{{ $t('users.total', { count: totalUsers }) }}</span>
      <va-pagination
        v-model="page"
        :pages="totalPages"
        :visible-pages="5"
        @update:modelValue="fetchUsers"
      />
    </div>

    <!-- Change Role Modal -->
    <va-modal
      v-model="showRoleModal"
      :title="$t('users.changeRoleTitle')"
      :ok-text="$t('users.save')"
      :cancel-text="$t('users.cancel')"
      @ok="saveRole"
      @cancel="closeRoleModal"
    >
      <p class="mb-2">{{ $t('users.user') }}: <strong>{{ selectedUser?.phone }}</strong></p>
      <va-select
        v-model="newRole"
        :options="editableRoleOptions"
        :label="$t('users.newRole')"
      />
    </va-modal>

    <!-- Top Up Balance Modal -->
    <va-modal
      v-model="showTopUpModal"
      :title="$t('users.topUpTitle')"
      :ok-text="$t('users.topUp')"
      :cancel-text="$t('users.cancel')"
      @ok="submitTopUp"
      @cancel="closeTopUpModal"
    >
      <p class="mb-2">{{ $t('users.user') }}: <strong>{{ selectedUser?.phone }}</strong></p>
      <va-input
        v-model.number="topUpAmount"
        type="number"
        :label="$t('users.amount')"
        min="0.01"
        step="0.01"
        required
      />
    </va-modal>

    <!-- Change Address Modal -->
    <va-modal
      v-model="showAddressModal"
      :title="$t('users.changeAddressTitle')"
      :ok-text="$t('users.save')"
      :cancel-text="$t('users.cancel')"
      @ok="saveAddress"
      @cancel="closeAddressModal"
    >
      <p class="mb-2">{{ $t('users.user') }}: <strong>{{ selectedUser?.phone }}</strong></p>
      <va-input
        v-model="newAddress"
        :label="$t('users.newAddress')"
        required
      />
    </va-modal>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, onMounted, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../../stores/auth-store'
import api from '../../services/api'

export default defineComponent({
  name: 'UserList',
  setup() {
    const { t } = useI18n()
    const authStore = useAuthStore()

    const users = ref([])
    const totalUsers = ref(0)
    const page = ref(1)
    const limit = ref(10)
    const loading = ref(false)

    const searchQuery = ref('')
    const selectedRole = ref('')
    const selectedStatus = ref('')

    const roleOptions = computed(() => [
      { text: t('roles.all'), value: '' },
      { text: t('roles.customer'), value: 'CUSTOMER' },
      { text: t('roles.executor'), value: 'EXECUTOR' },
      { text: t('roles.admin'), value: 'ADMIN' },
    ])

    const editableRoleOptions = computed(() => [
      { text: t('roles.customer'), value: 'CUSTOMER' },
      { text: t('roles.executor'), value: 'EXECUTOR' },
      { text: t('roles.admin'), value: 'ADMIN' },
    ])

    const statusOptions = computed(() => [
      { text: t('statuses.all'), value: '' },
      { text: t('statuses.active'), value: 'ACTIVE' },
      { text: t('statuses.banned'), value: 'BANNED' },
    ])

    const columns = computed(() => [
      { key: 'phone', label: t('users.phone'), sortable: true },
      { key: 'role', label: t('users.role'), sortable: true },
      { key: 'balance', label: t('users.balance'), sortable: true },
      { key: 'address', label: t('users.address'), sortable: true },
      { key: 'status', label: t('users.status'), sortable: true },
      { key: 'created_at', label: t('users.joinedAt'), sortable: true },
      { key: 'actions', label: t('users.actions') },
    ])

    const currencySymbol = computed(() => {
      return authStore.currency === 'RUB' ? '₽' : '$'
    })

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
        user.status = newStatus
      } catch (err) {
        alert(t('users.updateStatusError'))
        console.error(err)
      }
    }

    const showRoleModal = ref(false)
    const selectedUser = ref<any>(null)
    const newRole = ref<{ text: string; value: string } | string>('CUSTOMER')

    const openRoleModal = (user: any) => {
      selectedUser.value = user
      const option = editableRoleOptions.value.find((o) => o.value === user.role)
      newRole.value = option || user.role
      showRoleModal.value = true
    }

    const closeRoleModal = () => {
      showRoleModal.value = false
      selectedUser.value = null
    }

    const saveRole = async () => {
      if (!selectedUser.value) return
      const roleValue = typeof newRole.value === 'object' ? (newRole.value as any).value : newRole.value
      try {
        await api.post(`/admin/users/${selectedUser.value.id}/role`, { role: roleValue })
        selectedUser.value.role = roleValue
        closeRoleModal()
      } catch (err: any) {
        alert(err.response?.data || t('users.updateRoleError'))
        console.error(err)
      }
    }

    const showTopUpModal = ref(false)
    const topUpAmount = ref(0)
    const showAddressModal = ref(false)
    const newAddress = ref('')

    const openTopUpModal = (user: any) => {
      selectedUser.value = user
      topUpAmount.value = 100
      showTopUpModal.value = true
    }

    const closeTopUpModal = () => {
      showTopUpModal.value = false
      selectedUser.value = null
      topUpAmount.value = 0
    }

    const submitTopUp = async () => {
      if (!selectedUser.value || !topUpAmount.value || topUpAmount.value <= 0) {
        alert(t('users.positiveAmount'))
        return
      }
      try {
        await api.post(`/admin/users/${selectedUser.value.id}/balance`, { amount: topUpAmount.value })
        selectedUser.value.balance = (selectedUser.value.balance || 0) + topUpAmount.value
        closeTopUpModal()
      } catch (err: any) {
        alert(err.response?.data || t('users.topUpError'))
        console.error(err)
      }
    }

    const openAddressModal = (user: any) => {
      selectedUser.value = user
      newAddress.value = user.address || ''
      showAddressModal.value = true
    }

    const closeAddressModal = () => {
      showAddressModal.value = false
      selectedUser.value = null
      newAddress.value = ''
    }

    const saveAddress = async () => {
      if (!selectedUser.value) return
      if (!newAddress.value.trim()) {
        alert(t('users.addressRequired'))
        return
      }
      try {
        await api.post(`/admin/users/${selectedUser.value.id}/address`, { address: newAddress.value.trim() })
        selectedUser.value.address = newAddress.value.trim()
        closeAddressModal()
      } catch (err: any) {
        alert(err.response?.data || t('users.updateAddressError'))
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
      editableRoleOptions,
      statusOptions,
      columns,
      currencySymbol,
      totalPages,
      fetchUsers,
      debouncedFetch,
      clearFilters,
      toggleUserStatus,
      formatDate,
      showRoleModal,
      showTopUpModal,
      showAddressModal,
      selectedUser,
      newRole,
      topUpAmount,
      newAddress,
      openRoleModal,
      closeRoleModal,
      saveRole,
      openTopUpModal,
      closeTopUpModal,
      submitTopUp,
      openAddressModal,
      closeAddressModal,
      saveAddress,
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
.actions-container {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
</style>
