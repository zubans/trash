<template>
  <div class="customer-dashboard-container">
    <div class="main-wrapper">
      <!-- Profile Header Card -->
      <div class="card header-card">
        <div class="top-actions">
          <button type="button" class="btn-icon-text" title="Профиль / Адреса" @click="showProfileModal = true">
            <i class="fa-solid fa-user-plus"></i>
          </button>
          <button type="button" class="btn-icon-text" title="Выйти" @click="handleLogout">
            <i class="fa-solid fa-arrow-right-from-bracket"></i>
          </button>
        </div>

        <div class="profile-section">
          <div class="profile-row">
            <div class="avatar" @click="showProfileModal = true">
              <i class="fa-solid fa-user"></i>
            </div>
            <div class="profile-info">
              <div class="phone-row">
                <span class="phone-number" @click="showProfileModal = true">{{ phone || '79207050707' }}</span>
                <span class="badge-verified">Верифицирован</span>
              </div>
              <span class="role-text">Личный кабинет заказчика</span>
              <a href="#" class="address-link" @click.prevent="showProfileModal = true">
                <i class="fa-solid fa-location-dot"></i> Управление адресами
              </a>
            </div>
          </div>
        </div>

        <div class="divider"></div>

        <div class="finance-section">
          <div class="lang-selector-wrapper">
            <LanguageSwitcher />
          </div>
          <div class="balance-block">
            <div class="balance-amount">{{ currencySymbol }} {{ Number(balance).toFixed(2) }}</div>
            <div class="balance-label">Баланс</div>
          </div>
          <button type="button" class="btn-topup" @click="showTopUpModal = true">
            <i class="fa-regular fa-credit-card"></i> Запросить пополнение кошелька
          </button>
        </div>
      </div>

      <!-- Update Banner if any -->
      <update-banner />

      <!-- Alerts -->
      <va-alert v-if="successMsg" color="success" class="mb-3" closeable @dismissed="successMsg = ''">
        {{ successMsg }}
      </va-alert>
      <va-alert v-if="errorMsg" color="danger" class="mb-3" closeable @dismissed="errorMsg = ''">
        {{ errorMsg }}
      </va-alert>

      <!-- Main Action: Create Order -->
      <button type="button" class="btn-create-order" @click="openCreateOrderModal">
        <i class="fa-solid fa-cart-plus"></i> Создать заказ
      </button>

      <!-- Active Orders Card -->
      <div class="card">
        <div class="card-header">
          <div class="card-title">
            <i class="fa-solid fa-clipboard-list"></i> Активные заказы ({{ activeOrders.length }})
          </div>
          <button type="button" class="btn-refresh" title="Обновить" @click="fetchOrders">
            <i class="fa-solid fa-rotate-right"></i>
          </button>
        </div>

        <div v-if="activeOrders.length === 0" class="empty-state">
          <p>{{ $t('customer.noActiveOrders') }}</p>
        </div>

        <table v-else class="responsive-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>ТИП ЗАКАЗА</th>
              <th>ЦЕНА</th>
              <th>СТАТУС</th>
              <th>УПРАВЛЕНИЕ</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="order in activeOrders" :key="order.id">
              <td data-label="ID">
                <a href="#" class="id-link" @click.prevent="openOrderDetails(order)">#{{ order.id.slice(0, 8) }}</a>
              </td>
              <td data-label="ТИП ЗАКАЗА">{{ formatOrderType(order) }}</td>
              <td data-label="ЦЕНА" class="price">{{ Number(order.hold_amount).toFixed(2) }} {{ currencySymbol }}</td>
              <td data-label="СТАТУС" class="status">
                {{ order.status === 'ASSIGNED' ? 'НАЗНАЧЕН' : 'ПОИСК' }}
              </td>
              <td data-label="УПРАВЛЕНИЕ">
                <div class="actions-group">
                  <button type="button" class="action-btn action-info" title="Подробнее" @click="openOrderDetails(order)">
                    <i class="fa-solid fa-info"></i>
                  </button>
                  <button
                    v-if="order.status === 'ASSIGNED'"
                    type="button"
                    class="action-btn action-chat position-relative"
                    title="Чат"
                    @click="openChat(order)"
                  >
                    <i class="fa-solid fa-comment-dots"></i>
                  </button>
                  <button
                    v-if="order.status === 'ASSIGNED'"
                    type="button"
                    class="action-btn action-check"
                    title="Подтвердить"
                    @click="confirmOrder(order.id)"
                  >
                    <i class="fa-solid fa-check"></i>
                  </button>
                  <button
                    v-if="order.status === 'SEARCHING' || order.status === 'ASSIGNED'"
                    type="button"
                    class="action-btn action-close"
                    title="Отменить"
                    @click="cancelOrder(order.id)"
                  >
                    <i class="fa-solid fa-xmark"></i>
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- Order History Card -->
      <div class="card history-card">
        <div class="card-header cursor-pointer" @click="isHistoryCollapsed = !isHistoryCollapsed">
          <div class="card-title">
            <i class="fa-solid fa-clock-rotate-left"></i> История заказов ({{ historyOrders.length }})
            <i :class="['fa-solid', isHistoryCollapsed ? 'fa-chevron-down' : 'fa-chevron-up']" style="font-size: 12px; margin-left: 8px;"></i>
          </div>
        </div>

        <div v-if="!isHistoryCollapsed">
          <div v-if="historyOrders.length === 0" class="empty-state">
            <p>{{ $t('customer.noHistoryOrders') }}</p>
          </div>

          <table v-else class="responsive-table">
            <thead>
              <tr>
                <th>ID</th>
                <th>ТИП ЗАКАЗА</th>
                <th>ЦЕНА</th>
                <th>СТАТУС</th>
                <th>УПРАВЛЕНИЕ</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="order in historyOrders" :key="order.id">
                <td data-label="ID">
                  <a href="#" class="id-link muted-id" @click.prevent="openOrderDetails(order)">#{{ order.id.slice(0, 8) }}</a>
                </td>
                <td data-label="ТИП ЗАКАЗА">{{ formatOrderType(order) }}</td>
                <td data-label="ЦЕНА" class="price text-muted-price">{{ Number(order.final_amount || order.hold_amount).toFixed(2) }} {{ currencySymbol }}</td>
                <td data-label="СТАТУС" class="status muted-status">
                  {{ order.status === 'COMPLETED' ? 'ЗАВЕРШЁН' : 'ОТМЕНЁН' }}
                </td>
                <td data-label="УПРАВЛЕНИЕ">
                  <div class="actions-group">
                    <button type="button" class="action-btn action-info" title="Подробнее" @click="openOrderDetails(order)">
                      <i class="fa-solid fa-info"></i>
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- Order Details Modal -->
      <OrderDetailsModal
        v-model="showOrderDetailsModal"
        :selected-order-details="selectedOrderDetails"
        :currency-symbol="currencySymbol"
        :format-order-type="formatOrderType"
        :get-status-color="getStatusColor"
        :format-date-full="formatDateFull"
      />

      <!-- Create Order Modal -->
      <CreateOrderModal
        v-model="showCreateOrderModal"
        v-model:selected-category-id="selectedCategoryId"
        v-model:selected-sub-category-id="selectedSubCategoryId"
        v-model:selected-variant-id="selectedVariantId"
        v-model:is-urgent="isUrgent"
        v-model:is-asap="isAsap"
        :order-address="orderAddress"
        :order-lat="orderLat"
        :order-lon="orderLon"
        :geocode-error="geocodeError"
        :category-options="categoryOptions"
        :sub-category-options="subCategoryOptions"
        :variant-options="variantOptions"
        :is-auction-selected="isAuctionSelected"
        :selected-price="selectedPrice"
        :currency-symbol="currencySymbol"
        :creating-order="creatingOrder"
        @submit-order="submitOrder"
      />

      <!-- Top-up Modal -->
      <va-modal
        v-model="showTopUpModal"
        :title="$t('customer.requestWalletTopUp')"
        hide-default-actions
      >
        <div class="p-2">
          <va-form @submit.prevent="submitTopUp">
            <va-input
              v-model.number="topUpAmount"
              type="number"
              :label="$t('customer.amountWithCurrency')"
              class="mb-4"
              min="1"
              required
            />
            <va-button type="submit" block :loading="submitting">
              {{ $t('customer.submitRequest') }}
            </va-button>
          </va-form>
        </div>
      </va-modal>

      <!-- Customer Profile Modal -->
      <CustomerProfileModal
        v-model="showProfileModal"
        v-model:new-address-input="newAddressInput"
        :is-verified="true"
        :customer-addresses="customerAddresses"
        :default-address="defaultAddress"
        @set-active-address="setActiveAddress"
        @add-new-address="addNewAddress"
        @remove-address="removeAddress"
      />
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../../stores/auth-store'
import UpdateBanner from '../../components/UpdateBanner.vue'
import LanguageSwitcher from '../../components/LanguageSwitcher.vue'
import OrderDetailsModal from './components/OrderDetailsModal.vue'
import CreateOrderModal from './components/CreateOrderModal.vue'
import CustomerProfileModal from './components/CustomerProfileModal.vue'
import api from '../../services/api'
import { getServiceCategories, type ServiceNode } from '../../api/services'

export default defineComponent({
  name: 'CustomerDashboardV2',
  components: {
    UpdateBanner,
    LanguageSwitcher,
    OrderDetailsModal,
    CreateOrderModal,
    CustomerProfileModal,
  },
  setup() {
    const router = useRouter()
    const authStore = useAuthStore()

    const phone = ref('')
    const balance = ref(0)
    const currencySymbol = computed(() => (authStore.currency === 'RUB' ? '₽' : '$'))

    const successMsg = ref('')
    const errorMsg = ref('')

    // Addresses
    const defaultAddress = ref('Москва, ул. Тверская, д. 1')
    const customerAddresses = ref<any[]>([
      { address: 'Москва, ул. Тверская, д. 1' }
    ])
    const newAddressInput = ref('')

    // Orders
    const orders = ref<any[]>([])
    const isHistoryCollapsed = ref(false)

    // Modals
    const showCreateOrderModal = ref(false)
    const showOrderDetailsModal = ref(false)
    const showTopUpModal = ref(false)
    const showProfileModal = ref(false)
    const selectedOrderDetails = ref<any>(null)
    const topUpAmount = ref<number>(100)
    const submitting = ref(false)
    const creatingOrder = ref(false)

    // Catalog & Order Create
    const serviceCategories = ref<ServiceNode[]>([])
    const selectedCategoryId = ref<string | null>(null)
    const selectedSubCategoryId = ref<string | null>(null)
    const selectedVariantId = ref<string | null>(null)
    const isUrgent = ref(false)
    const isAsap = ref(false)
    const orderAddress = ref(defaultAddress.value)
    const orderLat = ref<number | null>(null)
    const orderLon = ref<number | null>(null)
    const geocodeError = ref('')

    const activeOrders = computed(() => {
      return orders.value.filter((o) => ['SEARCHING', 'ASSIGNED'].includes(o.status))
    })

    const historyOrders = computed(() => {
      return orders.value.filter((o) => ['COMPLETED', 'CANCELED'].includes(o.status))
    })

    const categoryOptions = computed(() => {
      return serviceCategories.value.map((c) => ({ label: c.code, value: c.id }))
    })

    const subCategoryOptions = computed(() => [])
    const variantOptions = computed(() => [])
    const isAuctionSelected = computed(() => false)
    const selectedPrice = computed(() => 0)

    const fetchProfile = async () => {
      try {
        const response = await api.get('/customer/profile')
        if (response.data) {
          phone.value = response.data.phone
          balance.value = response.data.balance
          if (response.data.address) {
            defaultAddress.value = response.data.address
            customerAddresses.value = [{ address: response.data.address }]
          }
        }
      } catch (err) {
        console.error('Failed to load profile:', err)
      }
    }

    const fetchOrders = async () => {
      try {
        const response = await api.get('/customer/orders')
        orders.value = response.data || []
      } catch (err) {
        console.error('Failed to fetch orders:', err)
      }
    }

    const openCreateOrderModal = async () => {
      showCreateOrderModal.value = true
      try {
        serviceCategories.value = await getServiceCategories()
      } catch (err) {
        console.error('Failed to load categories:', err)
      }
    }

    const submitOrder = async () => {
      creatingOrder.value = true
      try {
        await api.post('/customer/orders', {
          service_variant_id: selectedVariantId.value,
          address: orderAddress.value,
          is_urgent: isUrgent.value,
          is_asap: isAsap.value,
        })
        successMsg.value = 'Заказ успешно создан'
        showCreateOrderModal.value = false
        await fetchOrders()
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Ошибка создания заказа'
      } finally {
        creatingOrder.value = false
      }
    }

    const confirmOrder = async (orderId: string) => {
      try {
        await api.post(`/customer/orders/${orderId}/confirm`)
        successMsg.value = 'Заказ подтвержден'
        await fetchOrders()
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Ошибка подтверждения'
      }
    }

    const cancelOrder = async (orderId: string) => {
      try {
        await api.post(`/customer/orders/${orderId}/cancel`)
        successMsg.value = 'Заказ отменен'
        await fetchOrders()
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Ошибка отмены'
      }
    }

    const submitTopUp = async () => {
      submitting.value = true
      try {
        await api.post('/customer/finances/topup', { amount: topUpAmount.value })
        successMsg.value = 'Заявка отправлена'
        showTopUpModal.value = false
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Ошибка запроса'
      } finally {
        submitting.value = false
      }
    }

    const openOrderDetails = (order: any) => {
      selectedOrderDetails.value = order
      showOrderDetailsModal.value = true
    }

    const openChat = (order: any) => {
      console.log('Open chat for order', order)
    }

    const setActiveAddress = (addr: string) => {
      defaultAddress.value = addr
    }

    const addNewAddress = () => {
      if (!newAddressInput.value.trim()) return
      customerAddresses.value.push({ address: newAddressInput.value.trim() })
      defaultAddress.value = newAddressInput.value.trim()
      newAddressInput.value = ''
    }

    const removeAddress = (idx: number) => {
      customerAddresses.value.splice(idx, 1)
    }

    const formatOrderType = (order: any) => {
      return order.service_variant?.code || 'Услуга'
    }

    const getStatusColor = (status: string) => {
      return status === 'COMPLETED' ? 'success' : 'primary'
    }

    const formatDateFull = (dateStr: string) => {
      if (!dateStr) return ''
      return new Date(dateStr).toLocaleString('ru-RU')
    }

    const handleLogout = () => {
      authStore.logout()
      router.push('/login')
    }

    onMounted(async () => {
      await Promise.all([fetchProfile(), fetchOrders()])
    })

    return {
      phone,
      balance,
      currencySymbol,
      successMsg,
      errorMsg,
      defaultAddress,
      customerAddresses,
      newAddressInput,
      activeOrders,
      historyOrders,
      isHistoryCollapsed,
      showCreateOrderModal,
      showOrderDetailsModal,
      showTopUpModal,
      showProfileModal,
      selectedOrderDetails,
      topUpAmount,
      submitting,
      creatingOrder,
      selectedCategoryId,
      selectedSubCategoryId,
      selectedVariantId,
      isUrgent,
      isAsap,
      orderAddress,
      orderLat,
      orderLon,
      geocodeError,
      categoryOptions,
      subCategoryOptions,
      variantOptions,
      isAuctionSelected,
      selectedPrice,

      fetchOrders,
      openCreateOrderModal,
      submitOrder,
      confirmOrder,
      cancelOrder,
      submitTopUp,
      openOrderDetails,
      openChat,
      setActiveAddress,
      addNewAddress,
      removeAddress,
      formatOrderType,
      getStatusColor,
      formatDateFull,
      handleLogout,
    }
  },
})
</script>

<style scoped>
.customer-dashboard-container {
  font-family: 'Inter', sans-serif;
  background-color: #f3f4f6;
  color: #111827;
  line-height: 1.5;
  padding: 40px 20px;
  min-height: 100vh;
}

.main-wrapper {
  max-width: 1000px;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.card {
  background-color: #ffffff;
  border-radius: 12px;
  box-shadow: 0 1px 3px rgba(0,0,0,0.1);
  padding: 24px;
  position: relative;
}

/* Profile Top Card */
.header-card {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 30px;
}

.profile-section {
  display: flex;
  align-items: center;
  gap: 20px;
  flex: 1;
}

.profile-row {
  display: flex;
  gap: 16px;
  align-items: center;
  width: 100%;
}

.avatar {
  width: 80px;
  height: 80px;
  min-width: 80px;
  background-color: #e5e7eb;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 40px;
  color: #9ca3af;
  cursor: pointer;
  transition: opacity 0.2s;
}

.avatar:hover {
  opacity: 0.85;
}

.profile-info {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.phone-row {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.phone-number {
  font-size: 24px;
  font-weight: 700;
  cursor: pointer;
}

.badge-verified {
  background-color: #dcfce7;
  color: #166534;
  padding: 4px 10px;
  border-radius: 20px;
  font-size: 12px;
  font-weight: 500;
  border: 1px solid #bbf7d0;
}

.role-text {
  color: #6b7280;
  font-size: 14px;
}

.address-link {
  color: #4b5563;
  text-decoration: none;
  font-size: 14px;
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 4px;
  width: fit-content;
}

.address-link i {
  color: #3b82f6;
}

.address-link:hover {
  color: #2563eb;
  text-decoration: underline;
}

.divider {
  width: 1px;
  height: 80px;
  background-color: #e5e7eb;
  margin: 0 40px;
}

.finance-section {
  display: flex;
  flex-direction: column;
  gap: 12px;
  flex: 1;
}

.lang-selector-wrapper {
  width: fit-content;
}

.balance-amount {
  font-size: 28px;
  font-weight: 700;
  color: #2563eb;
  display: flex;
  align-items: baseline;
  gap: 4px;
  flex-wrap: wrap;
}

.balance-label {
  font-size: 13px;
  color: #6b7280;
}

.btn-topup {
  background-color: #788394;
  color: white;
  border: none;
  padding: 10px 16px;
  border-radius: 6px;
  font-size: 14px;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 8px;
  width: fit-content;
  transition: background-color 0.2s;
}

.btn-topup:hover {
  background-color: #64748b;
}

.top-actions {
  position: absolute;
  top: 24px;
  right: 24px;
  display: flex;
  gap: 8px;
}

.btn-icon-text {
  background-color: #e5e7eb;
  color: #4b5563;
  border: none;
  padding: 6px 12px;
  border-radius: 6px;
  font-size: 13px;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 500;
  transition: background-color 0.2s;
}

.btn-icon-text:hover {
  background-color: #d1d5db;
}

.btn-create-order {
  background-color: #5c9b42;
  color: white;
  border: none;
  padding: 16px;
  border-radius: 8px;
  font-size: 16px;
  font-weight: 600;
  cursor: pointer;
  width: 100%;
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 10px;
  transition: background-color 0.2s;
}

.btn-create-order:hover {
  background-color: #4e8636;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.card-title {
  font-size: 18px;
  font-weight: 600;
  color: #1e3a8a;
  display: flex;
  align-items: center;
  gap: 10px;
}

.btn-refresh {
  background: #f3f4f6;
  border: none;
  color: #6b7280;
  width: 32px;
  height: 32px;
  border-radius: 6px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background-color 0.2s;
}

.btn-refresh:hover {
  background-color: #e5e7eb;
  color: #111827;
}

.empty-state {
  text-align: center;
  padding: 32px 0;
  color: #6b7280;
  font-size: 14px;
}

.responsive-table {
  width: 100%;
  border-collapse: collapse;
}

.responsive-table th {
  text-align: left;
  font-size: 11px;
  text-transform: uppercase;
  color: #6b7280;
  font-weight: 600;
  padding: 12px 0;
  border-bottom: 1px solid #e5e7eb;
}

.responsive-table td {
  padding: 16px 0;
  font-size: 14px;
  border-bottom: 1px solid #f3f4f6;
}

.id-link {
  color: #3b82f6;
  font-weight: 500;
  text-decoration: none;
}

.id-link:hover {
  text-decoration: underline;
}

.muted-id {
  color: #9ca3af;
}

.price {
  font-weight: 600;
}

.text-muted-price {
  color: #6b7280;
}

.status {
  color: #6b7280;
  font-size: 13px;
  font-weight: 500;
}

.muted-status {
  color: #9ca3af;
}

.actions-group {
  display: flex;
  gap: 6px;
}

.action-btn {
  width: 32px;
  height: 32px;
  border-radius: 6px;
  border: none;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  color: white;
  transition: opacity 0.2s;
}

.action-btn:hover {
  opacity: 0.85;
}

.action-info { background-color: #cbd5e1; color: #334155; }
.action-chat { background-color: #cbd5e1; color: #334155; }
.action-check { background-color: #d1d5db; color: #16a34a; }
.action-close { background-color: #d1d5db; color: #dc2626; }

.history-card { opacity: 0.95; }
.history-card .card-title { color: #111827; }
.history-card table { opacity: 0.75; }

/* =========================================
   АДАПТИВ (МЕДИА-ЗАПРОСЫ) ДЛЯ МОБИЛОК
   ========================================= */
@media (max-width: 768px) {
  .customer-dashboard-container {
    padding: 16px 12px;
  }

  .main-wrapper {
    gap: 16px;
  }

  .card {
    padding: 16px;
  }

  /* Перестроение шапки */
  .header-card {
    flex-direction: column;
    align-items: stretch;
    padding: 20px 16px;
    gap: 20px;
  }

  .top-actions {
    position: relative;
    top: 0;
    right: 0;
    justify-content: flex-end;
    width: 100%;
    margin-bottom: -10px;
  }

  .profile-section {
    flex-direction: column;
    align-items: flex-start;
    gap: 16px;
  }

  .avatar {
    width: 60px;
    height: 60px;
    min-width: 60px;
    font-size: 28px;
  }

  .phone-number {
    font-size: 20px;
  }

  .divider {
    width: 100%;
    height: 1px;
    margin: 0;
  }

  .finance-section {
    width: 100%;
    align-items: flex-start;
  }

  .btn-topup {
    width: 100%;
    justify-content: center;
    padding: 12px;
  }

  /* Адаптивные таблицы - превращаем в карточки */
  .responsive-table, 
  .responsive-table thead, 
  .responsive-table tbody, 
  .responsive-table th, 
  .responsive-table td, 
  .responsive-table tr {
    display: block;
  }

  .responsive-table thead tr {
    position: absolute;
    top: -9999px;
    left: -9999px;
  }

  .responsive-table tr {
    border: 1px solid #e5e7eb;
    border-radius: 8px;
    padding: 12px;
    margin-bottom: 16px;
    background-color: #fafafa;
  }

  .responsive-table tr:last-child {
    margin-bottom: 0;
  }

  .responsive-table td {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 0;
    border: none;
    text-align: right;
  }

  .responsive-table td:not(:last-child) {
    border-bottom: 1px solid #f3f4f6;
  }

  .responsive-table td::before {
    content: attr(data-label);
    font-weight: 600;
    font-size: 11px;
    color: #6b7280;
    text-transform: uppercase;
    margin-right: 16px;
    text-align: left;
  }

  .actions-group {
    justify-content: flex-end;
    width: 100%;
  }

  .responsive-table td:last-child {
    justify-content: flex-end;
    padding-top: 12px;
  }

  .responsive-table td:last-child::before {
    display: none;
  }
}
</style>
