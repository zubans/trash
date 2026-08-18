<template>
  <div class="user-list">
    <!-- Table Toolbar -->
    <div class="table-toolbar mb-4">
      <div class="search-box">
        <i class="ph ph-magnifying-glass"></i>
        <input
          v-model="searchQuery"
          type="text"
          :placeholder="$t('users.searchPlaceholder')"
          @input="debouncedFetch"
        />
      </div>

      <div class="filters">
        <div class="filter-select-wrapper">
          <select v-model="selectedRole" class="btn-filter-select" @change="fetchUsers">
            <option value="">Все роли</option>
            <option value="CUSTOMER">CUSTOMER</option>
            <option value="EXECUTOR">EXECUTOR</option>
            <option value="ADMIN">ADMIN</option>
          </select>
        </div>

        <div class="filter-select-wrapper">
          <select v-model="selectedStatus" class="btn-filter-select" @change="fetchUsers">
            <option value="">Все статусы</option>
            <option value="ACTIVE">ACTIVE</option>
            <option value="BLOCKED">BLOCKED</option>
          </select>
        </div>

        <button class="btn-filter" title="Сбросить" @click="clearFilters">
          <i class="ph-bold ph-arrows-clockwise"></i>
        </button>
      </div>
    </div>

    <!-- Data Table -->
    <va-data-table :items="users" :columns="columns" :loading="loading" class="mb-4 custom-grid-table">
      <template #cell(phone)="{ value, rowData }">
        <div class="cell-phone">
          <div class="user-avatar" :class="{ 'executor-av': rowData.role === 'EXECUTOR', 'admin-av': rowData.role === 'ADMIN' }">
            {{ (rowData.role || 'U').charAt(0) }}
          </div>
          <span>{{ value }}</span>
        </div>
      </template>

      <template #cell(balance)="{ value }">
        <span class="cell-amount">{{ currencySymbol }}{{ Number(value).toFixed(2) }}</span>
      </template>

      <template #cell(status)="{ value }">
        <span :class="['status-pill', value === 'ACTIVE' ? 'success' : 'danger']">
          {{ value }}
        </span>
      </template>

      <template #cell(address)="{ value }">
        <span class="cell-address">{{ value || '-' }}</span>
      </template>

      <template #cell(created_at)="{ value }">
        <div class="cell-date">
          <span>{{ formatDate(value).split(',')[0] }}</span>
          <span class="date-time">{{ formatDate(value).split(',')[1] || '' }}</span>
        </div>
      </template>

      <template #cell(actions)="{ rowData }">
        <div class="actions-container">
          <button
            class="btn-action btn-topup"
            title="Пополнить"
            @click="openTopUpModal(rowData)"
          >
            {{ $t('users.topUp') }}
          </button>
          <button
            class="btn-action btn-role"
            title="Роль"
            @click="openRoleModal(rowData)"
          >
            {{ $t('users.roleBtn') }}
          </button>
          <button
            class="btn-action btn-address"
            title="Адрес"
            @click="openAddressModal(rowData)"
          >
            {{ $t('users.addressBtn') }}
          </button>
          <button
            v-if="rowData.status === 'ACTIVE'"
            class="btn-action btn-ban"
            title="Забанить"
            @click="toggleUserStatus(rowData)"
          >
            {{ $t('users.ban') }}
          </button>
          <button
            v-else
            class="btn-action btn-activate"
            title="Активировать"
            @click="toggleUserStatus(rowData)"
          >
            {{ $t('users.activate') }}
          </button>
        </div>
      </template>
    </va-data-table>

    <!-- Pagination -->
    <div class="d-flex justify-content-between align-items-center mt-3">
      <span class="text-muted font-weight-500">{{ $t('users.total', { count: totalUsers }) }}</span>
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

.btn-filter-select {
  background: #ffffff;
  border: 1px solid rgba(0,0,0,0.08);
  border-radius: 10px;
  padding: 8px 16px;
  font-size: 13px;
  font-weight: 600;
  color: #0f172a;
  cursor: pointer;
  outline: none;
  font-family: inherit;
  transition: all 0.2s ease-in-out;
}

.btn-filter-select:hover {
  background: #f8fafc;
  border-color: rgba(0,0,0,0.15);
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

.user-avatar.executor-av {
  background: #fff7ed;
  color: #d97706;
}

.user-avatar.admin-av {
  background: #f3e8ff;
  color: #9333ea;
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

.status-pill.danger {
  background: #fef2f2;
  color: #ef4444;
}

.actions-container {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.btn-action {
  border: none;
  padding: 5px 12px;
  border-radius: 8px;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s ease-in-out;
}

.btn-topup {
  background: #5c60f5;
  color: #ffffff;
}

.btn-topup:hover {
  background: #4f46e5;
}

.btn-role {
  background: #3b82f6;
  color: #ffffff;
}

.btn-role:hover {
  background: #2563eb;
}

.btn-address {
  background: #64748b;
  color: #ffffff;
}

.btn-address:hover {
  background: #475569;
}

.btn-ban {
  background: #ef4444;
  color: #ffffff;
}

.btn-ban:hover {
  background: #dc2626;
}

.btn-activate {
  background: #10b981;
  color: #ffffff;
}

.btn-activate:hover {
  background: #059669;
}
</style>
