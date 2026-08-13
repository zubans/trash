<template>
  <div class="modern-dashboard-page">
    <div class="dashboard">
      <!-- Top Navigation Bar -->
      <header class="top-bar">
        <h1 class="greeting">Кабинет</h1>
        <div class="top-controls">
          <div class="lang-switch-wrapper">
            <LanguageSwitcher />
          </div>
          <button type="button" class="icon-btn" title="Уведомления">
            <i class="ph ph-bell"></i>
          </button>
          <button type="button" class="icon-btn" title="Выход" @click="handleLogout">
            <i class="ph ph-sign-out"></i>
          </button>
        </div>
      </header>

      <!-- Bento Grid (Profile + Wallet) -->
      <div class="bento-grid">
        <!-- Profile Card -->
        <div class="bento-card">
          <div class="profile-content">
            <div class="avatar-modern" @click="showProfileModal = true">
              <i class="ph ph-user"></i>
            </div>
            <div class="user-details">
              <div class="user-role">
                Заказчик <span class="badge-verified">Верифицирован</span>
              </div>
              <div class="user-phone" @click="showProfileModal = true">{{ formattedPhone }}</div>
              <a href="#" class="address-btn" @click.prevent="showProfileModal = true">
                <i class="ph ph-map-pin"></i> Управление адресами
              </a>
            </div>
          </div>
        </div>

        <!-- Wallet Card -->
        <div class="bento-card wallet-card">
          <div class="wallet-header">
            <span>Ваш баланс</span>
            <i class="ph ph-wallet" style="font-size: 20px;"></i>
          </div>
          <div class="balance-val">
            {{ Number(balance).toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) }}
            <span class="currency">{{ currencySymbol }}</span>
          </div>
          <button type="button" class="btn-topup" @click="showTopUpModal = true">
            <i class="ph ph-plus-circle"></i> Пополнить
          </button>
        </div>
      </div>

      <!-- Update Banner -->
      <update-banner />

      <!-- Alerts -->
      <va-alert v-if="successMsg" color="success" class="mb-2" closeable @dismissed="successMsg = ''">
        {{ successMsg }}
      </va-alert>
      <va-alert v-if="errorMsg" color="danger" class="mb-2" closeable @dismissed="errorMsg = ''">
        {{ errorMsg }}
      </va-alert>

      <!-- Main Action: Create Order -->
      <button type="button" class="btn-primary-glow" @click="openCreateOrderModal">
        <i class="ph ph-plus" style="font-size: 18px; font-weight: bold;"></i> Создать заказ
      </button>

      <!-- Active Orders -->
      <div class="bento-card">
        <div class="section-header">
          <h2 class="section-title">
            <div class="title-icon"><i class="ph ph-package"></i></div>
            Активные заказы <span v-if="activeOrders.length" class="order-count">({{ activeOrders.length }})</span>
          </h2>
          <button type="button" class="icon-btn" title="Обновить" @click="fetchOrders">
            <i class="ph ph-arrows-clockwise"></i>
          </button>
        </div>

        <div v-if="activeOrders.length === 0" class="empty-orders-state">
          <p>{{ $t('customer.noActiveOrders') }}</p>
        </div>

        <div v-else class="orders-list">
          <div v-for="order in activeOrders" :key="order.id" class="order-row">
            <div class="o-icon"><i class="ph ph-box-arrow-up"></i></div>
            <div class="o-main">
              <div class="o-title">{{ formatOrderType(order) }}</div>
              <div class="o-id" @click="openOrderDetails(order)">ID: #{{ order.id.slice(0, 8) }}</div>
            </div>
            <div class="o-price">{{ Number(order.hold_amount).toFixed(2) }} {{ currencySymbol }}</div>
            <div :class="['o-status', order.status === 'ASSIGNED' ? 'assigned' : 'searching']">
              {{ order.status === 'ASSIGNED' ? 'Назначен' : 'Поиск' }}
            </div>
            <div class="o-actions">
              <button
                type="button"
                class="act-btn"
                title="Детали"
                @click="openOrderDetails(order)"
              >
                <i class="ph ph-info"></i>
              </button>
              <button
                v-if="order.status === 'ASSIGNED'"
                type="button"
                class="act-btn primary"
                title="Чат"
                @click="openChat(order)"
              >
                <i class="ph ph-chat-centered-text"></i>
              </button>
              <button
                v-if="order.status === 'ASSIGNED'"
                type="button"
                class="act-btn success"
                title="Принять"
                @click="confirmOrder(order.id)"
              >
                <i class="ph ph-check"></i>
              </button>
              <button
                v-if="order.status === 'SEARCHING' || order.status === 'ASSIGNED'"
                type="button"
                class="act-btn danger"
                title="Отменить"
                @click="cancelOrder(order.id)"
              >
                <i class="ph ph-x"></i>
              </button>
            </div>
          </div>
        </div>
      </div>

      <!-- History Orders -->
      <div class="bento-card history-section">
        <div class="section-header cursor-pointer" @click="isHistoryCollapsed = !isHistoryCollapsed">
          <h2 class="section-title" style="color: var(--text-muted); font-size: 16px;">
            <div class="title-icon"><i class="ph ph-clock-counter-clockwise"></i></div>
            История заказов <span style="font-size: 13px; font-weight: normal; margin-left: 4px;">({{ historyOrders.length }})</span>
          </h2>
          <button type="button" class="icon-btn">
            <i :class="['ph', isHistoryCollapsed ? 'ph-caret-down' : 'ph-caret-up']"></i>
          </button>
        </div>

        <div v-if="!isHistoryCollapsed">
          <div v-if="historyOrders.length === 0" class="empty-orders-state">
            <p>{{ $t('customer.noHistoryOrders') }}</p>
          </div>

          <div v-else class="orders-list">
            <div v-for="order in historyOrders" :key="order.id" class="order-row history-row">
              <div class="o-icon" style="background: #F4F4F5; border: none;">
                <i :class="['ph', order.status === 'COMPLETED' ? 'ph-check-circle' : 'ph-x-circle']" :style="{ color: order.status === 'COMPLETED' ? '#10B981' : '#EF4444' }"></i>
              </div>
              <div class="o-main">
                <div class="o-title" style="color: var(--text-muted);">{{ formatOrderType(order) }}</div>
                <div class="o-id">#{{ order.id.slice(0, 8) }}</div>
              </div>
              <div class="o-price" style="color: var(--text-muted);">{{ Number(order.final_amount || order.hold_amount).toFixed(2) }} {{ currencySymbol }}</div>
              <div class="o-status" style="background: transparent; padding: 0;">
                {{ order.status === 'COMPLETED' ? 'Завершен' : 'Отменен' }}
              </div>
              <div class="o-actions">
                <button type="button" class="act-btn" title="Детали" @click="openOrderDetails(order)">
                  <i class="ph ph-info"></i>
                </button>
              </div>
            </div>
          </div>
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

    const phone = ref('79207050707')
    const balance = ref(1980.00)
    const currencySymbol = computed(() => (authStore.currency === 'RUB' ? '₽' : '$'))

    const formattedPhone = computed(() => {
      const p = phone.value || '79207050707'
      if (p.length === 11) {
        return `${p[0]} ${p.slice(1, 4)} ${p.slice(4, 7)} ${p.slice(7, 9)} ${p.slice(9, 11)}`
      }
      return p
    })

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

    const loadPhosphorIcons = () => {
      if (!document.getElementById('phosphor-icons-script')) {
        const script = document.createElement('script')
        script.id = 'phosphor-icons-script'
        script.src = 'https://unpkg.com/@phosphor-icons/web'
        document.head.appendChild(script)
      }
    }

    const fetchProfile = async () => {
      try {
        const response = await api.get('/customer/profile')
        if (response.data) {
          if (response.data.phone) phone.value = response.data.phone
          if (response.data.balance !== undefined) balance.value = response.data.balance
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
      return order.service_variant?.code || 'Большой обычный'
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
      loadPhosphorIcons()
      await Promise.all([fetchProfile(), fetchOrders()])
    })

    return {
      phone,
      formattedPhone,
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
@import url('https://fonts.googleapis.com/css2?family=Plus+Jakarta+Sans:wght@400;500;600;700&display=swap');

.modern-dashboard-page {
  --bg-body: #F4F4F5;
  --bg-card: #FFFFFF;
  --text-main: #09090B;
  --text-muted: #71717A;
  --border-light: #E4E4E7;
  
  --brand-primary: #4F46E5;
  --brand-primary-hover: #4338CA;
  --success-bg: #D1FAE5;
  --success-text: #059669;
  
  --radius-lg: 24px;
  --radius-md: 16px;
  --radius-sm: 10px;
  
  --shadow-soft: 0 4px 40px -10px rgba(0, 0, 0, 0.05);
  --transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);

  font-family: 'Plus Jakarta Sans', sans-serif;
  background-color: var(--bg-body);
  color: var(--text-main);
  line-height: 1.5;
  padding: 40px 20px;
  min-height: 100vh;
}

.dashboard {
  max-width: 1040px;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

/* Навигация сверху */
.top-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 4px;
}

.greeting {
  font-size: 24px;
  font-weight: 700;
  letter-spacing: -0.5px;
  margin: 0;
  color: var(--text-main);
}

.top-controls {
  display: flex;
  align-items: center;
  gap: 8px;
}

.lang-switch-wrapper {
  margin-right: 4px;
}

.icon-btn {
  background: var(--bg-card);
  border: 1px solid var(--border-light);
  color: var(--text-main);
  width: 40px;
  height: 40px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  cursor: pointer;
  transition: var(--transition);
}

.icon-btn:hover {
  background: #F4F4F5;
  transform: translateY(-2px);
}

/* Bento Grid */
.bento-grid {
  display: grid;
  grid-template-columns: 2fr 1fr;
  gap: 20px;
}

.bento-card {
  background: var(--bg-card);
  border-radius: var(--radius-lg);
  padding: 24px;
  border: 1px solid rgba(228, 228, 231, 0.8);
  box-shadow: var(--shadow-soft);
  position: relative;
  overflow: hidden;
}

/* Профиль */
.profile-content {
  display: flex;
  align-items: center;
  gap: 20px;
  height: 100%;
}

.avatar-modern {
  width: 80px;
  height: 80px;
  background: linear-gradient(135deg, #E0E7FF, #C7D2FE);
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 36px;
  color: #4F46E5;
  border: 4px solid #FFF;
  box-shadow: 0 0 0 1px var(--border-light);
  cursor: pointer;
  transition: var(--transition);
}

.avatar-modern:hover {
  transform: scale(1.03);
}

.user-details {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.user-phone {
  font-size: 24px;
  font-weight: 700;
  letter-spacing: -0.5px;
  cursor: pointer;
}

.user-role {
  color: var(--text-muted);
  font-weight: 500;
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
}

.badge-verified {
  background: var(--success-bg);
  color: var(--success-text);
  padding: 2px 8px;
  border-radius: 99px;
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.address-btn {
  margin-top: 8px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--brand-primary);
  font-weight: 600;
  font-size: 13px;
  text-decoration: none;
  width: fit-content;
  padding: 6px 14px;
  background: #EEF2FF;
  border-radius: 99px;
  transition: var(--transition);
}

.address-btn:hover {
  background: #E0E7FF;
}

/* Кошелек */
.wallet-card {
  background: linear-gradient(145deg, #09090B, #18181B);
  color: #FFF;
  border: none;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
}

.wallet-card::before {
  content: '';
  position: absolute;
  top: -50%;
  right: -50%;
  width: 200px;
  height: 200px;
  background: radial-gradient(circle, rgba(79,70,229,0.3) 0%, transparent 70%);
  border-radius: 50%;
}

.wallet-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  color: #A1A1AA;
  font-size: 13px;
  font-weight: 500;
  position: relative;
  z-index: 1;
}

.balance-val {
  font-size: 32px;
  font-weight: 700;
  margin-top: 8px;
  letter-spacing: -1px;
  position: relative;
  z-index: 1;
}

.currency {
  font-size: 20px;
  color: #A1A1AA;
  font-weight: 500;
}

.btn-topup {
  background: rgba(255, 255, 255, 0.1);
  backdrop-filter: blur(10px);
  border: 1px solid rgba(255,255,255,0.1);
  color: #FFF;
  padding: 10px 16px;
  border-radius: var(--radius-sm);
  font-weight: 600;
  font-size: 14px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  cursor: pointer;
  transition: var(--transition);
  margin-top: 20px;
  position: relative;
  z-index: 1;
}

.btn-topup:hover {
  background: rgba(255, 255, 255, 0.2);
}

/* Кнопка создания */
.btn-primary-glow {
  background: var(--brand-primary);
  color: #FFF;
  border: none;
  padding: 16px 32px;
  border-radius: var(--radius-md);
  font-size: 15px;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  cursor: pointer;
  box-shadow: 0 8px 24px -6px rgba(79, 70, 229, 0.4);
  transition: var(--transition);
  width: 100%;
}

.btn-primary-glow:hover {
  background: var(--brand-primary-hover);
  transform: translateY(-2px);
  box-shadow: 0 12px 28px -6px rgba(79, 70, 229, 0.5);
}

/* --- КОМПАКТНЫЕ СПИСКИ ЗАКАЗОВ --- */
.section-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.section-title {
  font-size: 18px;
  font-weight: 700;
  display: flex;
  align-items: center;
  gap: 10px;
  letter-spacing: -0.5px;
  margin: 0;
}

.title-icon {
  background: #F4F4F5;
  padding: 6px;
  border-radius: var(--radius-sm);
  color: var(--text-main);
  display: flex;
}

.orders-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.empty-orders-state {
  text-align: center;
  padding: 24px 0;
  color: var(--text-muted);
  font-size: 14px;
}

.order-row {
  display: flex;
  align-items: center;
  padding: 10px 16px;
  border-radius: var(--radius-md);
  border: 1px solid var(--border-light);
  background: #FAFAFA;
  transition: var(--transition);
}

.order-row:hover {
  background: #FFF;
  border-color: #D4D4D8;
  box-shadow: 0 4px 12px rgba(0,0,0,0.03);
}

.o-icon {
  width: 40px;
  height: 40px;
  border-radius: 10px;
  background: #FFF;
  border: 1px solid var(--border-light);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
  color: var(--text-muted);
  margin-right: 12px;
  flex-shrink: 0;
}

.o-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.o-title {
  font-weight: 700;
  font-size: 15px;
  line-height: 1.2;
}

.o-id {
  font-size: 12px;
  color: var(--text-muted);
  font-family: monospace;
  cursor: pointer;
}

.o-price {
  font-size: 15px;
  font-weight: 700;
  margin-right: 16px;
  min-width: 80px;
  text-align: right;
}

.o-status {
  background: #F3F4F6;
  color: #4B5563;
  padding: 4px 10px;
  border-radius: 99px;
  font-size: 12px;
  font-weight: 600;
  margin-right: 16px;
  display: flex;
  align-items: center;
  gap: 6px;
  white-space: nowrap;
}

.o-status.assigned {
  background: #ECFDF5;
  color: #059669;
}

.o-status.assigned::before {
  content: '';
  width: 6px;
  height: 6px;
  background: #059669;
  border-radius: 50%;
}

.o-actions {
  display: flex;
  gap: 6px;
}

.act-btn {
  width: 32px;
  height: 32px;
  border-radius: 8px;
  background: #FFF;
  border: 1px solid var(--border-light);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  color: var(--text-muted);
  transition: var(--transition);
  font-size: 16px;
}

.act-btn:hover {
  background: #F4F4F5;
  color: var(--text-main);
}

.act-btn.primary:hover {
  background: var(--brand-primary);
  color: white;
  border-color: var(--brand-primary);
}

.act-btn.danger:hover {
  background: #EF4444;
  color: white;
  border-color: #EF4444;
}

.act-btn.success:hover {
  background: #10B981;
  color: white;
  border-color: #10B981;
}

.history-section {
  opacity: 0.85;
}

.cursor-pointer {
  cursor: pointer;
}

/* =======================================
   АДАПТИВ (МОБИЛЬНЫЕ УСТРОЙСТВА)
   ======================================= */
@media (max-width: 900px) {
  .bento-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 600px) {
  .modern-dashboard-page {
    padding: 16px 12px;
  }

  .bento-card {
    padding: 20px;
  }

  .profile-content {
    flex-direction: column;
    text-align: center;
    gap: 12px;
  }

  .user-role {
    justify-content: center;
  }

  .address-btn {
    margin: 8px auto 0;
  }

  /* Grid для мобилок: 2 строки */
  .order-row {
    display: grid;
    grid-template-columns: 40px 1fr auto;
    grid-template-rows: auto auto;
    gap: 6px 12px;
    padding: 12px;
    background: #FFF;
  }

  .o-icon {
    grid-column: 1;
    grid-row: 1 / 3;
    margin: 0;
  }

  .o-main {
    grid-column: 2;
    grid-row: 1;
    justify-content: center;
  }

  .o-price {
    grid-column: 3;
    grid-row: 1;
    margin: 0;
    text-align: right;
  }

  .o-status {
    grid-column: 2;
    grid-row: 2;
    margin: 0;
    width: fit-content;
    padding: 2px 8px;
    font-size: 11px;
  }

  .o-actions {
    grid-column: 3;
    grid-row: 2;
    justify-content: flex-end;
  }
}
</style>
