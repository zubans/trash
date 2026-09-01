<template>
  <div class="user-list">
    <div class="admin-table-card mb-4">
      <!-- Table Toolbar -->
      <div class="table-toolbar">
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

      <!-- Modern Grid Table -->
      <div class="grid-table">
        <!-- Headers -->
        <div class="grid-row grid-header">
          <div>Пользователь</div>
          <div>Роль</div>
          <div>Баланс</div>
          <div>Адрес</div>
          <div>Статус</div>
          <div>Регистрация</div>
          <div class="cell-actions">Действия</div>
        </div>

        <!-- Rows -->
        <div v-for="u in users" :key="u.id" class="grid-row grid-item">
          <div class="cell-user">
            <div class="avatar" :class="getAvatarClass(u.role)">
              {{ getAvatarChar(u.role) }}
            </div>
            <div class="user-info">
              <span class="user-phone">{{ u.phone }}</span>
              <span class="user-name" :title="formatFullName(u)">{{ formatFullName(u) }}</span>
              <span class="user-birth-date">{{ formatBirthDate(u.birth_date) }}</span>
            </div>
          </div>

          <div class="cell-role">
            <span
              v-for="r in (u.roles && u.roles.length ? u.roles : [u.role])"
              :key="r"
              class="role-chip"
              :class="getRoleClass(r)"
            >{{ r }}</span>
          </div>

          <div class="cell-amount">
            <span class="balance">{{ currencySymbol }}{{ Number(u.balance || 0).toFixed(2) }}</span>
          </div>

          <div class="cell-address">
            <span class="address" :class="{ empty: !u.address }" :title="u.address || '—'">{{ u.address || '—' }}</span>
          </div>

          <div class="cell-status">
            <div class="status-main" :class="{ banned: u.status !== 'ACTIVE' }">
              <span class="dot"></span>{{ u.status }}
            </div>
            <div class="status-verify" :class="{ verified: u.is_verified }">
              <i :class="u.is_verified ? 'ph-fill ph-check-circle' : 'ph ph-warning-circle'"></i>
              {{ u.is_verified ? 'Верифицирован' : 'Не верифицирован' }}
            </div>
          </div>

          <div class="cell-date">
            <span class="date-main">{{ formatDay(u.created_at) }}</span>
            <span class="date-time">{{ formatTime(u.created_at) }}</span>
          </div>

          <div class="cell-actions">
            <!-- Fast Top Up Icon Button -->
            <button
              class="btn-ghost topup"
              data-tooltip="Пополнить"
              @click="openTopUpModal(u)"
            >
              <i class="ph-bold ph-plus-circle"></i>
            </button>

            <!-- Kebab Dropdown Menu -->
            <div class="dropdown-wrapper">
              <button class="btn-ghost" data-tooltip="Меню">
                <i class="ph-bold ph-dots-three-vertical"></i>
              </button>
              <div class="dropdown-menu">
                <button class="dropdown-item" @click="openNameModal(u)">
                  <i class="ph-bold ph-user"></i> Личные данные
                </button>
                <button class="dropdown-item" @click="openTopUpModal(u)">
                  <i class="ph-bold ph-wallet"></i> Пополнить баланс
                </button>
                <button class="dropdown-item" @click="openRolesModal(u)">
                  <i class="ph-bold ph-user-gear"></i> Роли
                </button>
                <button class="dropdown-item" @click="openAddressModal(u)">
                  <i class="ph-bold ph-map-pin"></i> Редактировать адрес
                </button>
                <div class="menu-divider"></div>
                <button class="dropdown-item" @click="toggleUserVerified(u)">
                  <i class="ph-bold" :class="u.is_verified ? 'ph-seal-warning' : 'ph-seal-check'"></i>
                  {{ u.is_verified ? 'Снять верификацию' : 'Верифицировать' }}
                </button>
                <div class="menu-divider"></div>
                <button
                  v-if="u.status === 'ACTIVE'"
                  class="dropdown-item danger"
                  @click="toggleUserStatus(u)"
                >
                  <i class="ph-bold ph-prohibit"></i> Заблокировать
                </button>
                <button
                  v-else
                  class="dropdown-item"
                  @click="toggleUserStatus(u)"
                >
                  <i class="ph-bold ph-check"></i> Активировать
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Mobile card list (shown instead of the table on phones) -->
    <div class="user-cards mb-4">
      <div v-for="u in users" :key="u.id" class="user-card">
        <div class="uc-top">
          <div class="uc-avatar" :class="getAvatarClass(u.role)">{{ getAvatarChar(u.role) }}</div>
          <div class="uc-id">
            <div class="uc-phone">{{ u.phone }}</div>
            <div class="uc-name">{{ formatFullName(u) }}</div>
          </div>
          <button class="uc-menu-btn" :class="{ open: cardMenuId === u.id }" @click="cardMenuId = cardMenuId === u.id ? null : u.id">
            <i class="ph-bold ph-dots-three-vertical"></i>
          </button>
        </div>

        <div class="uc-chips">
          <span
            v-for="r in (u.roles && u.roles.length ? u.roles : [u.role])"
            :key="r"
            class="role-chip"
            :class="getRoleClass(r)"
          >{{ r }}</span>
          <span class="status-pill" :class="u.status === 'ACTIVE' ? 'success' : 'danger'">{{ u.status }}</span>
          <span class="verify-chip" :class="u.is_verified ? 'verified' : 'unverified'">
            <i :class="u.is_verified ? 'ph-fill ph-seal-check' : 'ph ph-seal'"></i>
            {{ u.is_verified ? 'Верифицирован' : 'Не верифицирован' }}
          </span>
        </div>

        <div class="uc-meta">
          <div class="uc-meta-row">
            <span>Баланс</span><b>{{ currencySymbol }}{{ Number(u.balance || 0).toFixed(2) }}</b>
          </div>
          <div class="uc-meta-row">
            <span>Адрес</span><b class="uc-addr">{{ u.address || '—' }}</b>
          </div>
          <div class="uc-meta-row">
            <span>Регистрация</span><b>{{ formatDay(u.created_at) }}</b>
          </div>
        </div>

        <div class="uc-quick">
          <button class="uc-quick-btn topup" @click="openTopUpModal(u)">
            <i class="ph-bold ph-plus-circle"></i> Пополнить
          </button>
        </div>

        <!-- Expandable action sheet -->
        <div v-if="cardMenuId === u.id" class="uc-actions">
          <button @click="openNameModal(u); cardMenuId = null"><i class="ph-bold ph-user"></i> ФИО</button>
          <button @click="openRolesModal(u); cardMenuId = null"><i class="ph-bold ph-user-gear"></i> Роли</button>
          <button @click="openAddressModal(u); cardMenuId = null"><i class="ph-bold ph-map-pin"></i> Адрес</button>
          <button @click="toggleUserVerified(u); cardMenuId = null">
            <i class="ph-bold" :class="u.is_verified ? 'ph-seal-warning' : 'ph-seal-check'"></i>
            {{ u.is_verified ? 'Снять верификацию' : 'Верифицировать' }}
          </button>
          <button class="danger" @click="toggleUserStatus(u); cardMenuId = null">
            <i class="ph-bold" :class="u.status === 'ACTIVE' ? 'ph-prohibit' : 'ph-check'"></i>
            {{ u.status === 'ACTIVE' ? 'Заблокировать' : 'Активировать' }}
          </button>
        </div>
      </div>
      <div v-if="users.length === 0" class="uc-empty">Пользователи не найдены</div>
    </div>

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

    <!-- Multi-role Modal -->
    <va-modal
      v-model="showRolesModal"
      title="Роли пользователя"
      :ok-text="$t('users.save')"
      :cancel-text="$t('users.cancel')"
      @ok="saveRoles"
      @cancel="closeRolesModal"
    >
      <p class="mb-2">{{ $t('users.user') }}: <strong>{{ selectedUser?.phone }}</strong></p>
      <p class="mb-2 text-secondary">Пользователь может иметь несколько ролей одновременно.</p>
      <div class="roles-check-list">
        <label v-for="role in allRoles" :key="role.value" class="roles-check-row">
          <input type="checkbox" :value="role.value" v-model="newRoles" />
          <span>{{ role.label }}</span>
        </label>
      </div>
      <p v-if="newRoles.length === 0" class="roles-warn">Выберите хотя бы одну роль.</p>
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
      <p v-if="selectedUser?.address" class="mb-2 text-secondary">
        {{ $t('users.address') }}: <strong>{{ selectedUser.address }}</strong>
      </p>
      <AddressAutocomplete
        v-model="newAddressStruct"
        :label="$t('users.newAddress')"
        placeholder="Начните вводить адрес..."
      />
    </va-modal>

    <!-- Personal Details Modal -->
    <va-modal
      v-model="showNameModal"
      title="Редактировать личные данные"
      :ok-text="$t('users.save')"
      :cancel-text="$t('users.cancel')"
      @ok="saveName"
      @cancel="closeNameModal"
    >
      <p class="mb-3">{{ $t('users.user') }}: <strong>{{ selectedUser?.phone }}</strong></p>
      <div class="mb-3">
        <va-input
          v-model="newLastName"
          label="Фамилия"
          required
        />
      </div>
      <div class="mb-3">
        <va-input
          v-model="newFirstName"
          label="Имя"
          required
        />
      </div>
      <div class="mb-3">
        <va-input
          v-model="newPatronymic"
          label="Отчество"
          required
        />
      </div>
      <div class="mb-3">
        <va-input
          v-model="newBirthDate"
          type="date"
          label="Дата рождения"
          :max="maxBirthDate"
          required
        />
      </div>
    </va-modal>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, onMounted, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../../stores/auth-store'
import api from '../../services/api'
import AddressAutocomplete, { StructuredAddress } from '../../components/AddressAutocomplete.vue'

export default defineComponent({
  name: 'UserList',
  components: { AddressAutocomplete },
  setup() {
    const { t } = useI18n()
    const authStore = useAuthStore()

    const users = ref<any[]>([])
    // Which mobile card's action sheet is open (null = none).
    const cardMenuId = ref<string | null>(null)
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

    const toggleUserVerified = async (user: any) => {
      const newVerified = !user.is_verified
      try {
        await api.post(`/admin/users/${user.id}/verified`, { verified: newVerified })
        user.is_verified = newVerified
      } catch (err: any) {
        alert(err.response?.data || 'Ошибка обновления верификации')
        console.error(err)
      }
    }

    const showRoleModal = ref(false)
    const selectedUser = ref<any>(null)
    const newRole = ref<{ text: string; value: string } | string>('CUSTOMER')

    // Multi-role editing.
    const allRoles = [
      { value: 'CUSTOMER', label: 'Заказчик' },
      { value: 'EXECUTOR', label: 'Исполнитель' },
      { value: 'MODERATOR', label: 'Модератор' },
      { value: 'ADMIN', label: 'Администратор' },
    ]
    const showRolesModal = ref(false)
    const newRoles = ref<string[]>([])

    const openRolesModal = (user: any) => {
      selectedUser.value = user
      newRoles.value = (user.roles && user.roles.length ? [...user.roles] : [user.role]).filter(Boolean)
      showRolesModal.value = true
    }

    const closeRolesModal = () => {
      showRolesModal.value = false
      selectedUser.value = null
      newRoles.value = []
    }

    const saveRoles = async () => {
      if (!selectedUser.value) return
      if (newRoles.value.length === 0) {
        alert('Выберите хотя бы одну роль')
        return
      }
      try {
        await api.post(`/admin/users/${selectedUser.value.id}/roles`, { roles: newRoles.value })
        selectedUser.value.roles = [...newRoles.value]
        // Keep the primary role coherent if it was removed.
        if (!newRoles.value.includes(selectedUser.value.role)) {
          selectedUser.value.role = newRoles.value[0]
        }
        closeRolesModal()
      } catch (err: any) {
        alert(err.response?.data || 'Ошибка обновления ролей')
        console.error(err)
      }
    }

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
    const newAddressStruct = ref<StructuredAddress | null>(null)

    const showNameModal = ref(false)
    const newLastName = ref('')
    const newFirstName = ref('')
    const newPatronymic = ref('')
    const newBirthDate = ref('')
    const maxBirthDate = new Date().toISOString().slice(0, 10)

    const formatFullName = (user: any) => {
      if (!user) return '-'
      const parts = [user.last_name, user.first_name, user.patronymic].filter((p: string) => p && p.trim())
      return parts.length > 0 ? parts.join(' ') : '-'
    }

    // The listing serialises birth_date as a timestamp; both the date input and
    // the cell want the plain day.
    const toDateInput = (value?: string) => (value ? String(value).slice(0, 10) : '')

    const formatBirthDate = (value?: string) => {
      const day = toDateInput(value)
      if (!day) return 'дата рождения не указана'
      const [year, month, date] = day.split('-')
      return `${date}.${month}.${year}`
    }

    const openNameModal = (user: any) => {
      selectedUser.value = user
      newLastName.value = user.last_name || ''
      newFirstName.value = user.first_name || ''
      newPatronymic.value = user.patronymic || ''
      newBirthDate.value = toDateInput(user.birth_date)
      showNameModal.value = true
    }

    const closeNameModal = () => {
      showNameModal.value = false
      selectedUser.value = null
      newLastName.value = ''
      newFirstName.value = ''
      newPatronymic.value = ''
      newBirthDate.value = ''
    }

    const saveName = async () => {
      if (!selectedUser.value) return
      if (!newLastName.value.trim() || !newFirstName.value.trim() || !newPatronymic.value.trim()) {
        alert('Заполните все поля (Фамилия, Имя, Отчество)')
        return
      }
      if (!newBirthDate.value) {
        alert('Укажите дату рождения')
        return
      }
      try {
        // The birth date goes first: it is the field the server can reject, and
        // saving it before the name keeps a refusal from leaving half the form
        // applied.
        if (newBirthDate.value !== toDateInput(selectedUser.value.birth_date)) {
          await api.post(`/admin/users/${selectedUser.value.id}/birth-date`, {
            birth_date: newBirthDate.value,
          })
          selectedUser.value.birth_date = newBirthDate.value
        }
        await api.post(`/admin/users/${selectedUser.value.id}/name`, {
          last_name: newLastName.value.trim(),
          first_name: newFirstName.value.trim(),
          patronymic: newPatronymic.value.trim(),
        })
        selectedUser.value.last_name = newLastName.value.trim()
        selectedUser.value.first_name = newFirstName.value.trim()
        selectedUser.value.patronymic = newPatronymic.value.trim()
        closeNameModal()
      } catch (err: any) {
        alert(err.response?.data || 'Ошибка обновления личных данных')
        console.error(err)
      }
    }

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
      // Start empty so the admin picks a fresh suggestion from the register; the
      // current address is shown above the field for reference.
      newAddressStruct.value = null
      showAddressModal.value = true
    }

    const closeAddressModal = () => {
      showAddressModal.value = false
      selectedUser.value = null
      newAddress.value = ''
      newAddressStruct.value = null
    }

    const saveAddress = async () => {
      if (!selectedUser.value) return
      const chosen = newAddressStruct.value
      if (!chosen || !chosen.value?.trim()) {
        alert(t('users.addressRequired'))
        return
      }
      try {
        // Send the composed line the register produced; the backend stores it as
        // the customer's pickup address.
        await api.post(`/admin/users/${selectedUser.value.id}/address`, { address: chosen.value.trim() })
        selectedUser.value.address = chosen.value.trim()
        closeAddressModal()
      } catch (err: any) {
        alert(err.response?.data || t('users.updateAddressError'))
        console.error(err)
      }
    }

    // Split into day and time explicitly. The cell used to render
    // toLocaleString().split(','), which puts the whole timestamp in one line
    // wherever the browser locale does not separate them with a comma.
    const formatDay = (dateStr: string) => {
      if (!dateStr) return '—'
      return new Date(dateStr).toLocaleDateString('ru-RU')
    }

    const formatTime = (dateStr: string) => {
      if (!dateStr) return ''
      return new Date(dateStr).toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })
    }

    onMounted(() => {
      fetchUsers()
    })

    const getAvatarClass = (role: string) => {
      switch (role) {
        case 'CUSTOMER': return 'customer'
        case 'EXECUTOR': return 'executor'
        case 'MODERATOR': return 'moderator'
        case 'ADMIN': return 'admin'
        default: return 'customer'
      }
    }

    // Role chips are coloured by role, so a moderator reads differently from a
    // plain executor at a glance. Shared with the mobile cards.
    const getRoleClass = (role: string) => getAvatarClass(role)

    const getAvatarChar = (role: string) => {
      return (role || 'U').charAt(0).toUpperCase()
    }

    return {
      users,
      cardMenuId,
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
      getAvatarClass,
      getAvatarChar,
      fetchUsers,
      debouncedFetch,
      clearFilters,
      toggleUserStatus,
      toggleUserVerified,
      formatDay,
      formatTime,
      getRoleClass,
      showRoleModal,
      showTopUpModal,
      showAddressModal,
      showNameModal,
      selectedUser,
      newRole,
      topUpAmount,
      newAddress,
      newAddressStruct,
      newLastName,
      newFirstName,
      newPatronymic,
      formatFullName,
      openRoleModal,
      closeRoleModal,
      saveRole,
      allRoles,
      showRolesModal,
      newRoles,
      openRolesModal,
      closeRolesModal,
      saveRoles,
      openTopUpModal,
      closeTopUpModal,
      submitTopUp,
      openAddressModal,
      closeAddressModal,
      saveAddress,
      openNameModal,
      closeNameModal,
      saveName,
      newBirthDate,
      maxBirthDate,
      formatBirthDate,
    }
  },
})
</script>

<style scoped>
.user-list {
  display: flex;
  flex-direction: column;
}

/* Toolbar and table share one surface, so the filters read as part of the
   table rather than as a floating strip above it. */
.admin-table-card {
  background: #ffffff;
  border-radius: 16px;
  box-shadow: 0 4px 24px rgba(0, 0, 0, 0.03);
  overflow: hidden;
}

.table-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  padding: 20px 24px;
  border-bottom: 1px solid #e2e8f0;
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

.grid-table {
  width: 100%;
  display: flex;
  flex-direction: column;
  overflow-x: auto;
}

/* Sum of the fixed columns: narrower than this the table scrolls rather than
   crushing the address and status cells. */
.grid-row {
  min-width: 1100px;
}

.grid-row {
  display: grid;
  /* Пользователь | Роль | Баланс | Адрес | Статус | Регистрация | Действия.
     The last two are wider than the mockup's 120/80: at that size the uppercase
     headers "РЕГИСТРАЦИЯ" and "ДЕЙСТВИЯ" run into each other, and two 32px
     buttons do not fit an 80px column once the cell padding is counted. */
  grid-template-columns: minmax(250px, 1.5fr) 120px 110px minmax(200px, 1fr) 160px 140px 120px;
  align-items: center;
}

.grid-row > div {
  min-width: 0;
  padding: 16px 20px;
  display: flex;
  align-items: center;
  overflow: hidden;
}

.grid-header .cell-actions {
  justify-content: flex-end;
}

.grid-header {
  background: #f8fafc;
  border-bottom: 2px solid #e2e8f0;
  font-size: 11px;
  font-weight: 800;
  color: #64748b;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.grid-item {
  border-bottom: 1px solid #e2e8f0;
  transition: background 0.2s ease-in-out;
}

.grid-item:hover {
  background: #f8fafc;
}

/* Phone, name and birth date share one column: three lines about the same
   person read better together than split across two columns. */
.cell-user {
  gap: 16px;
}

.user-info {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.user-phone {
  font-size: 15px;
  font-weight: 800;
  color: #0f172a;
}

.user-name,
.user-birth-date {
  font-size: 13px;
  color: #64748b;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.user-birth-date {
  font-size: 12px;
}

.avatar {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 16px;
  font-weight: 800;
  flex-shrink: 0;
}

.avatar.customer {
  background: #eef2ff;
  color: #5c60f5;
}

.avatar.executor {
  background: #fffbeb;
  color: #f59e0b;
}

.avatar.moderator {
  background: #e0e7ff;
  color: #4338ca;
}

.avatar.admin {
  background: #f3e8ff;
  color: #d946ef;
}

/* Roles stack: a user with several of them stays inside a fixed column. */
.cell-role {
  flex-direction: column;
  align-items: flex-start;
  gap: 4px;
}

.role-chip {
  display: inline-flex;
  background: #f1f5f9;
  color: #0f172a;
  border-radius: 6px;
  padding: 4px 8px;
  font-size: 11px;
  font-weight: 800;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  width: fit-content;
}

.role-chip.customer {
  background: #eef2ff;
  color: #5c60f5;
}

.role-chip.moderator {
  background: #e0e7ff;
  color: #4338ca;
}

.role-chip.admin {
  background: #f3e8ff;
  color: #a21caf;
}

.roles-check-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin-top: 8px;
}

.roles-check-row {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 14px;
  font-weight: 600;
  color: #0f172a;
  cursor: pointer;
}

.roles-warn {
  color: #dc2626;
  font-size: 12px;
  margin-top: 8px;
}

.balance {
  font-size: 15px;
  font-weight: 800;
  color: #0f172a;
  white-space: nowrap;
}

.address {
  font-size: 13px;
  color: #64748b;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.address.empty {
  color: #cbd5e1;
}

.cell-date {
  flex-direction: column;
  align-items: flex-start;
  justify-content: center;
}

.date-main {
  font-size: 14px;
  font-weight: 600;
  color: #0f172a;
}

.date-time {
  font-size: 12px;
  color: #64748b;
}

.status-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  font-weight: 600;
  color: #10b981;
}

.status-pill::before {
  content: '';
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
}

.status-pill.danger {
  color: #ef4444;
}

.cell-status {
  flex-direction: column;
  gap: 6px;
  align-items: flex-start;
  justify-content: center;
}

.status-main {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  font-weight: 800;
  color: #0f172a;
}

.status-main.banned {
  color: #ef4444;
}

.status-main .dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #10b981;
  flex-shrink: 0;
}

.status-main.banned .dot {
  background: #ef4444;
}

.status-verify {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  font-weight: 600;
  color: #64748b;
  white-space: nowrap;
}

.status-verify i {
  font-size: 14px;
}

.status-verify.verified {
  color: #10b981;
}

.verify-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  font-weight: 600;
  padding: 2px 8px;
  border-radius: 999px;
  white-space: nowrap;
}

.verify-chip i {
  font-size: 13px;
}

.verify-chip.verified {
  color: #10b981;
  background: #ecfdf5;
}

.verify-chip.unverified {
  color: #94a3b8;
  background: #f1f5f9;
}

/* Actions & Kebab Dropdown */
.cell-actions {
  display: flex;
  gap: 4px;
  padding-left: 8px;
  justify-content: flex-end;
  align-items: center;
  /* overflow:hidden from the shared cell rule would clip the kebab menu */
  overflow: visible;
}

.btn-ghost {
  width: 32px;
  height: 32px;
  border-radius: 6px;
  border: none;
  background: transparent;
  color: #64748b;
  font-size: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.2s ease-in-out;
  position: relative;
}

.btn-ghost:hover {
  background: #e2e8f0;
  color: #0f172a;
}

.btn-ghost.topup:hover {
  background: #eef2ff;
  color: #5c60f5;
}

.btn-ghost.danger:hover {
  background: #fef2f2;
  color: #ef4444;
}

/* Tooltip */
.btn-ghost::after {
  content: attr(data-tooltip);
  position: absolute;
  bottom: 100%;
  left: 50%;
  transform: translateX(-50%) translateY(-4px);
  background: #0f172a;
  color: white;
  padding: 4px 8px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 500;
  white-space: nowrap;
  opacity: 0;
  pointer-events: none;
  transition: all 0.2s ease-in-out;
  z-index: 10;
}

.btn-ghost:hover::after {
  opacity: 1;
  transform: translateX(-50%) translateY(-8px);
}

.dropdown-wrapper {
  position: relative;
}

.dropdown-menu {
  position: absolute;
  right: 0;
  top: 100%;
  z-index: 100;
  background: rgba(255,255,255,0.98);
  backdrop-filter: blur(12px);
  border: 1px solid rgba(0,0,0,0.08);
  border-radius: 12px;
  padding: 8px;
  box-shadow: 0 10px 40px -10px rgba(15, 23, 42, 0.15), 0 1px 3px rgba(15, 23, 42, 0.05);
  min-width: 200px;
  display: none;
  flex-direction: column;
  gap: 2px;
  transform-origin: top right;
  animation: scaleIn 0.2s ease-out;
}

@keyframes scaleIn {
  from { opacity: 0; transform: scale(0.95); }
  to { opacity: 1; transform: scale(1); }
}

.dropdown-wrapper:hover .dropdown-menu,
.dropdown-wrapper:focus-within .dropdown-menu {
  display: flex;
}

.dropdown-item {
  padding: 8px 12px;
  border-radius: 8px;
  border: none;
  background: transparent;
  font-family: inherit;
  font-size: 13px;
  font-weight: 500;
  color: #0f172a;
  display: flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  transition: all 0.2s ease-in-out;
  text-align: left;
  width: 100%;
}

.dropdown-item i {
  font-size: 16px;
  color: #64748b;
  transition: all 0.2s ease-in-out;
}

.dropdown-item:hover {
  background: #f8fafc;
  color: #5c60f5;
}

.dropdown-item:hover i {
  color: #5c60f5;
}

.dropdown-item.danger:hover {
  background: #fef2f2;
  color: #ef4444;
}

.dropdown-item.danger:hover i {
  color: #ef4444;
}

.menu-divider {
  height: 1px;
  background: rgba(0,0,0,0.06);
  margin: 4px 0;
}

/* Mobile card list: hidden on desktop, replaces the table on phones. */
.user-cards {
  display: none;
  flex-direction: column;
  gap: 12px;
}
.user-card {
  background: #ffffff;
  border: 1px solid #eef1f6;
  border-radius: 18px;
  padding: 14px;
  box-shadow: 0 4px 14px rgba(15, 23, 42, 0.04);
}
.uc-top {
  display: flex;
  align-items: center;
  gap: 12px;
}
.uc-avatar {
  width: 42px;
  height: 42px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 800;
  font-size: 16px;
  color: #fff;
  background: #6366f1;
  flex-shrink: 0;
}
.uc-id {
  flex: 1;
  min-width: 0;
}
.uc-phone {
  font-size: 15px;
  font-weight: 700;
  color: #0f172a;
}
.uc-name {
  font-size: 13px;
  color: #64748b;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.uc-menu-btn {
  width: 38px;
  height: 38px;
  border-radius: 10px;
  border: 1px solid #eef1f6;
  background: #f8fafc;
  color: #64748b;
  font-size: 18px;
  cursor: pointer;
  flex-shrink: 0;
}
.uc-menu-btn.open {
  background: #eef2ff;
  color: #4f46e5;
  border-color: #c7d2fe;
}
.uc-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 12px;
}
.uc-meta {
  margin-top: 12px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.uc-meta-row {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
  font-size: 13px;
}
.uc-meta-row span {
  color: #94a3b8;
  flex-shrink: 0;
}
.uc-meta-row b {
  color: #0f172a;
  font-weight: 600;
  text-align: right;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}
.uc-addr {
  white-space: nowrap;
}
.uc-quick {
  margin-top: 12px;
}
.uc-quick-btn {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 11px;
  border-radius: 12px;
  border: none;
  font-size: 14px;
  font-weight: 700;
  cursor: pointer;
  background: #ecfdf5;
  color: #059669;
}
.uc-actions {
  margin-top: 10px;
  padding-top: 10px;
  border-top: 1px dashed #e2e8f0;
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}
.uc-actions button {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 11px 12px;
  border-radius: 12px;
  border: 1px solid #eef1f6;
  background: #f8fafc;
  color: #334155;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  text-align: left;
}
.uc-actions button i {
  font-size: 16px;
  color: #6366f1;
}
.uc-actions button.danger {
  grid-column: 1 / -1;
  background: #fef2f2;
  color: #ef4444;
  border-color: #fee2e2;
}
.uc-actions button.danger i {
  color: #ef4444;
}
.uc-empty {
  text-align: center;
  color: #94a3b8;
  padding: 24px;
  font-size: 14px;
}

@media (max-width: 768px) {
  .table-toolbar {
    flex-direction: column;
    align-items: stretch;
    gap: 12px;
  }
  .search-box {
    width: 100%;
  }
  .filters {
    flex-wrap: wrap;
    width: 100%;
  }
  .filter-select-wrapper, .btn-filter-select {
    flex: 1;
    min-width: 120px;
  }
  /* Swap the wide table for cards; the toolbar keeps the surface to itself,
     so it must not keep the table's chrome. */
  .admin-table-card {
    background: transparent;
    box-shadow: none;
    border-radius: 0;
    overflow: visible;
  }
  .table-toolbar {
    padding: 0;
    border-bottom: none;
  }
  .grid-table {
    display: none;
  }
  .user-cards {
    display: flex;
  }
}
</style>
