<template>
  <div class="premium-dashboard-page">
    <div class="container">
      <!-- Шапка -->
      <header class="glass-header">
        <div class="logo-text">
          <i class="ph-fill ph-planet" style="color: var(--accent-main);"></i> Кабинет
        </div>
        <div class="top-actions">
          <div class="lang-switch-wrapper">
            <LanguageSwitcher />
          </div>
          <button type="button" class="btn-glass" title="Выход" @click="handleLogout">
            <i class="ph ph-sign-out"></i>
          </button>
        </div>
      </header>

      <!-- Сетка Профиль + Кошелек -->
      <div class="premium-grid">
        <!-- Карточка профиля (Светлая, матовое стекло) -->
        <div class="surface-card">
          <div class="profile-row">
            <div class="avatar-glow" @click="showProfileModal = true">
              <i class="ph ph-user"></i>
            </div>
            <div class="profile-info">
              <div class="role-badge">Верифицированный заказчик</div>
              <h2 @click="showProfileModal = true">{{ formattedPhone }}</h2>
              <a href="#" class="link-elegant" @click.prevent="showProfileModal = true">
                <i class="ph ph-map-pin"></i> Мои адреса
              </a>
            </div>
          </div>
        </div>

        <!-- Кошелек (Темная премиум-карта) -->
        <div class="wallet-card">
          <div class="wallet-inner">
            <div class="w-label">Доступный баланс</div>
            <div class="w-balance">
              {{ Number(balance).toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) }}
              <span class="w-currency">{{ currencySymbol }}</span>
            </div>
          </div>
          <button type="button" class="btn-topup-blur" @click="showTopUpModal = true">
            <i class="ph ph-plus-circle"></i> Пополнить счет
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

      <!-- Большая сочная кнопка -->
      <button type="button" class="create-order-btn" @click="openCreateOrderModal">
        <i class="ph-bold ph-plus"></i> Создать новый заказ
      </button>

      <!-- Блок заказов -->
      <div>
        <div class="d-flex justify-content-between align-items-center mb-3">
          <h2 class="section-title m-0">Активные заказы <span v-if="activeOrders.length" class="text-muted text-sm">({{ activeOrders.length }})</span></h2>
          <button type="button" class="btn-glass" style="width:40px; height:40px; font-size:18px;" title="Обновить" @click="fetchOrders">
            <i class="ph ph-arrows-clockwise"></i>
          </button>
        </div>

        <div v-if="activeOrders.length === 0" class="empty-orders-state">
          <p>{{ $t('customer.noActiveOrders') }}</p>
        </div>

        <div v-else class="orders-stack">
          <div v-for="order in activeOrders" :key="order.id" class="order-pill">
            <div :class="['op-icon', order.is_urgent ? 'orange' : 'purple']">
              <i :class="['ph', order.is_urgent ? 'ph-rocket-launch' : 'ph-package']"></i>
            </div>
            <div class="op-info">
              <div class="op-title">{{ formatOrderType(order) }}</div>
              <div class="op-id cursor-pointer" @click="openOrderDetails(order)">#{{ order.id.slice(0, 8) }}</div>
            </div>
            <div class="op-price">{{ Number(order.hold_amount).toFixed(2) }} {{ currencySymbol }}</div>
            <div class="op-status">
              {{ order.status === 'ASSIGNED' ? 'Назначен' : 'Поиск' }}
            </div>
            <div class="op-actions">
              <button
                type="button"
                class="btn-elegant"
                title="Детали"
                @click="openOrderDetails(order)"
              >
                <i class="ph ph-info"></i>
              </button>
              <button
                v-if="order.status === 'ASSIGNED'"
                type="button"
                class="btn-elegant primary"
                title="Чат"
                @click="openChat(order)"
              >
                <i class="ph ph-chat-centered-text"></i>
              </button>
              <button
                v-if="order.status === 'ASSIGNED'"
                type="button"
                class="btn-elegant"
                title="Принять"
                @click="confirmOrder(order.id)"
              >
                <i class="ph ph-check"></i>
              </button>
              <button
                v-if="order.status === 'SEARCHING' || order.status === 'ASSIGNED'"
                type="button"
                class="btn-elegant danger"
                title="Отменить"
                @click="cancelOrder(order.id)"
              >
                <i class="ph ph-x"></i>
              </button>
            </div>
          </div>
        </div>
      </div>

      <!-- История -->
      <div style="margin-top: 12px;">
        <div class="cursor-pointer" @click="isHistoryCollapsed = !isHistoryCollapsed">
          <h2 class="section-title" style="font-size: 18px;">
            <i class="ph ph-clock-counter-clockwise"></i> История заказов ({{ historyOrders.length }})
            <i :class="['ph', isHistoryCollapsed ? 'ph-caret-down' : 'ph-caret-up']" style="font-size: 14px; margin-left: 6px;"></i>
          </h2>
        </div>

        <div v-if="!isHistoryCollapsed" class="orders-stack mt-3">
          <div v-if="historyOrders.length === 0" class="empty-orders-state">
            <p>{{ $t('customer.noHistoryOrders') }}</p>
          </div>

          <div
            v-for="order in historyOrders"
            :key="order.id"
            class="order-pill"
            style="box-shadow: none; border-color: rgba(0,0,0,0.05); background: transparent;"
          >
            <div class="op-icon" style="background: #e2e8f0; width: 40px; height: 40px; border-radius: 12px;">
              <i :class="['ph', order.status === 'COMPLETED' ? 'ph-check-circle' : 'ph-x-circle']"></i>
            </div>
            <div class="op-info">
              <div class="op-title" style="color: var(--text-muted);">{{ formatOrderType(order) }}</div>
              <div class="op-id">#{{ order.id.slice(0, 8) }}</div>
            </div>
            <div class="op-price" style="color: var(--text-muted); font-size: 16px;">
              {{ Number(order.final_amount || order.hold_amount).toFixed(2) }} {{ currencySymbol }}
            </div>
            <div class="op-status" style="background: transparent; color: var(--text-muted);">
              {{ order.status === 'COMPLETED' ? 'Завершен' : 'Отменен' }}
            </div>
            <div class="op-actions">
              <button type="button" class="btn-elegant" title="Детали" @click="openOrderDetails(order)">
                <i class="ph ph-info"></i>
              </button>
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
      <div v-if="showTopUpModal" class="topup-modal-overlay" @click.self="showTopUpModal = false">
        <div class="topup-modal-card">
          <div class="topup-modal-header">
            <div class="topup-modal-title">Запрос на пополнение</div>
            <button type="button" class="btn-close-topup" aria-label="Закрыть" @click="showTopUpModal = false">
              <i class="ph ph-x"></i>
            </button>
          </div>

          <form @submit.prevent="submitTopUp">
            <div class="form-group mb-4">
              <label class="form-label">Сумма</label>
              <div class="input-wrapper">
                <!-- Порядок элементов важен для селектора соседства в CSS -->
                <input
                  v-model.number="topUpAmount"
                  type="number"
                  class="form-input"
                  min="1"
                  required
                />
                <i class="ph ph-currency-rub input-icon"></i>
              </div>
              <div class="quick-amounts">
                <button type="button" class="amount-pill" @click="topUpAmount = (Number(topUpAmount) || 0) + 500">+ 500 ₽</button>
                <button type="button" class="amount-pill" @click="topUpAmount = (Number(topUpAmount) || 0) + 1000">+ 1 000 ₽</button>
                <button type="button" class="amount-pill" @click="topUpAmount = (Number(topUpAmount) || 0) + 5000">+ 5 000 ₽</button>
              </div>
            </div>

            <button type="submit" class="btn-submit-topup" :disabled="submitting">
              <span v-if="submitting" class="spinner-sm"></span>
              <template v-else>
                Отправить запрос <i class="ph-bold ph-paper-plane-tilt"></i>
              </template>
            </button>
          </form>
        </div>
      </div>

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
@import url('https://fonts.googleapis.com/css2?family=Outfit:wght@300;400;500;600;700&display=swap');

.premium-dashboard-page {
  --bg-base: #f8f9fa;
  --surface-card: rgba(255, 255, 255, 0.85);
  --surface-hover: rgba(255, 255, 255, 1);
  
  --text-title: #0f172a;
  --text-body: #334155;
  --text-muted: #8b98a5;
  
  --accent-main: #6366f1;
  --accent-glow: rgba(99, 102, 241, 0.4);
  
  --rad-sm: 12px;
  --rad-md: 20px;
  --rad-lg: 32px;
  
  --shadow-float: 0 10px 40px -10px rgba(15, 23, 42, 0.08), 
                  0 1px 3px rgba(15, 23, 42, 0.03),
                  inset 0 1px 0 rgba(255,255,255,1);
  
  --transition: all 0.4s cubic-bezier(0.16, 1, 0.3, 1);

  font-family: 'Outfit', sans-serif;
  background-color: var(--bg-base);
  background-image: 
      radial-gradient(at 0% 0%, rgba(99, 102, 241, 0.08) 0px, transparent 50%),
      radial-gradient(at 100% 0%, rgba(236, 72, 153, 0.05) 0px, transparent 50%),
      radial-gradient(at 100% 100%, rgba(14, 165, 233, 0.08) 0px, transparent 50%);
  background-attachment: fixed;
  color: var(--text-body);
  line-height: 1.5;
  padding: 40px 20px;
  min-height: 100vh;
}

.container {
  max-width: 960px;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: 28px;
}

/* --- Header: Glassmorphism --- */
.glass-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}

.logo-text {
  font-size: 28px;
  font-weight: 700;
  color: var(--text-title);
  letter-spacing: -1px;
  display: flex;
  align-items: center;
  gap: 12px;
}

.top-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}

.btn-glass {
  background: rgba(255, 255, 255, 0.5);
  backdrop-filter: blur(10px);
  border: 1px solid rgba(255,255,255,0.8);
  width: 48px;
  height: 48px;
  border-radius: var(--rad-sm);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 22px;
  color: var(--text-title);
  cursor: pointer;
  box-shadow: 0 4px 12px rgba(0,0,0,0.03);
  transition: var(--transition);
}

.btn-glass:hover {
  background: rgba(255, 255, 255, 0.9);
  transform: translateY(-2px);
}

/* --- Grid --- */
.premium-grid {
  display: grid;
  grid-template-columns: 1.5fr 1fr;
  gap: 24px;
}

/* --- Profile Card --- */
.surface-card {
  background: var(--surface-card);
  backdrop-filter: blur(20px);
  border-radius: var(--rad-lg);
  padding: 32px;
  box-shadow: var(--shadow-float);
  border: 1px solid rgba(255, 255, 255, 0.6);
  display: flex;
  flex-direction: column;
  justify-content: center;
}

.profile-row {
  display: flex;
  align-items: center;
  gap: 24px;
}

.avatar-glow {
  width: 88px;
  height: 88px;
  border-radius: 28px;
  background: linear-gradient(135deg, #f8fafc, #e2e8f0);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 40px;
  color: #94a3b8;
  box-shadow: inset 0 2px 4px rgba(255,255,255,0.8), 
              0 8px 16px rgba(0,0,0,0.05);
  position: relative;
  cursor: pointer;
  transition: var(--transition);
}

.avatar-glow:hover {
  transform: scale(1.03);
}

.avatar-glow::after {
  content: '';
  position: absolute;
  bottom: -2px;
  right: -2px;
  width: 20px;
  height: 20px;
  background: #10b981;
  border: 3px solid #fff;
  border-radius: 50%;
  box-shadow: 0 0 10px rgba(16, 185, 129, 0.4);
}

.profile-info h2 {
  font-size: 32px;
  font-weight: 700;
  color: var(--text-title);
  letter-spacing: -0.5px;
  margin-bottom: 4px;
  line-height: 1;
  cursor: pointer;
}

.role-badge {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 15px;
  font-weight: 500;
  color: var(--text-muted);
}

.link-elegant {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  margin-top: 16px;
  font-size: 14px;
  font-weight: 600;
  color: var(--accent-main);
  text-decoration: none;
  padding: 10px 20px;
  background: rgba(99, 102, 241, 0.08);
  border-radius: var(--rad-sm);
  transition: var(--transition);
}

.link-elegant:hover {
  background: rgba(99, 102, 241, 0.15);
}

/* --- Premium Wallet Card --- */
.wallet-card {
  background: linear-gradient(120deg, #0f172a 0%, #1e1b4b 50%, #312e81 100%);
  border-radius: var(--rad-lg);
  padding: 32px;
  color: white;
  position: relative;
  overflow: hidden;
  box-shadow: 0 20px 40px -10px rgba(30, 27, 75, 0.5);
  display: flex;
  flex-direction: column;
  justify-content: space-between;
}

.wallet-card::before {
  content: '';
  position: absolute;
  top: -20%;
  left: -10%;
  width: 150px;
  height: 150px;
  background: #6366f1;
  filter: blur(60px);
  border-radius: 50%;
  opacity: 0.5;
}

.wallet-card::after {
  content: '';
  position: absolute;
  bottom: -20%;
  right: -10%;
  width: 200px;
  height: 200px;
  background: #ec4899;
  filter: blur(60px);
  border-radius: 50%;
  opacity: 0.4;
}

.wallet-inner {
  position: relative;
  z-index: 1;
}

.w-label {
  font-size: 14px;
  color: rgba(255,255,255,0.7);
  font-weight: 500;
  letter-spacing: 0.5px;
  text-transform: uppercase;
}

.w-balance {
  font-size: 44px;
  font-weight: 700;
  letter-spacing: -1.5px;
  margin-top: 8px;
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.w-currency {
  font-size: 24px;
  color: rgba(255,255,255,0.6);
  font-weight: 400;
}

.btn-topup-blur {
  margin-top: 32px;
  background: rgba(255,255,255,0.1);
  backdrop-filter: blur(12px);
  border: 1px solid rgba(255,255,255,0.2);
  color: white;
  padding: 14px 24px;
  border-radius: var(--rad-sm);
  font-size: 15px;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  cursor: pointer;
  transition: var(--transition);
  position: relative;
  z-index: 1;
}

.btn-topup-blur:hover {
  background: rgba(255,255,255,0.2);
  transform: translateY(-2px);
}

/* --- Giant Action Button --- */
.create-order-btn {
  background: linear-gradient(135deg, #6366f1, #4f46e5);
  color: white;
  border: none;
  padding: 20px;
  border-radius: var(--rad-md);
  font-size: 18px;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  cursor: pointer;
  box-shadow: 0 15px 30px -10px var(--accent-glow);
  transition: var(--transition);
  width: 100%;
}

.create-order-btn:hover {
  transform: translateY(-3px) scale(1.01);
  box-shadow: 0 20px 40px -10px rgba(99, 102, 241, 0.6);
}

/* --- Floating Orders List --- */
.section-title {
  font-size: 22px;
  font-weight: 700;
  color: var(--text-title);
  margin: 0;
  display: flex;
  align-items: center;
  gap: 12px;
}

.empty-orders-state {
  text-align: center;
  padding: 32px 0;
  color: var(--text-muted);
  font-size: 15px;
}

.orders-stack {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.order-pill {
  background: var(--surface-card);
  backdrop-filter: blur(20px);
  border-radius: var(--rad-md);
  padding: 16px 24px;
  display: flex;
  align-items: center;
  box-shadow: 0 4px 20px rgba(0,0,0,0.03), inset 0 1px 0 rgba(255,255,255,0.8);
  border: 1px solid rgba(255,255,255,0.5);
  transition: var(--transition);
  position: relative;
  overflow: hidden;
}

.order-pill:hover {
  transform: translateY(-3px);
  background: var(--surface-hover);
  box-shadow: 0 12px 30px rgba(0,0,0,0.06), inset 0 1px 0 rgba(255,255,255,1);
}

.order-pill::before {
  content: '';
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 4px;
  background: var(--accent-main);
  opacity: 0;
  transition: var(--transition);
}

.order-pill:hover::before {
  opacity: 1;
}

.op-icon {
  width: 48px;
  height: 48px;
  border-radius: 16px;
  background: #f1f5f9;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 24px;
  color: #64748b;
  margin-right: 20px;
  flex-shrink: 0;
}

.op-icon.purple {
  background: #e0e7ff;
  color: #4f46e5;
}

.op-icon.orange {
  background: #ffedd5;
  color: #ea580c;
}

.op-info {
  flex: 1;
}

.op-title {
  font-size: 17px;
  font-weight: 600;
  color: var(--text-title);
  letter-spacing: -0.3px;
}

.op-id {
  font-size: 13px;
  color: var(--text-muted);
  margin-top: 2px;
}

.op-price {
  font-size: 18px;
  font-weight: 700;
  color: var(--text-title);
  margin-right: 32px;
}

.op-status {
  padding: 6px 14px;
  border-radius: 99px;
  font-size: 13px;
  font-weight: 600;
  background: #ecfdf5;
  color: #059669;
  margin-right: 32px;
  display: flex;
  align-items: center;
  gap: 6px;
  white-space: nowrap;
}

.op-status::before {
  content: '';
  width: 6px;
  height: 6px;
  background: currentColor;
  border-radius: 50%;
}

.op-actions {
  display: flex;
  gap: 8px;
}

.btn-elegant {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  border: none;
  background: #f8fafc;
  color: #64748b;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  cursor: pointer;
  transition: var(--transition);
}

.btn-elegant:hover {
  background: #f1f5f9;
  color: var(--text-title);
  transform: scale(1.05);
}

.btn-elegant.primary:hover {
  background: var(--accent-main);
  color: white;
}

.btn-elegant.danger:hover {
  background: #ef4444;
  color: white;
}

.cursor-pointer {
  cursor: pointer;
}

/* --- Адаптив для Android / PWA --- */
@media (max-width: 900px) {
  .premium-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 600px) {
  .premium-dashboard-page {
    padding: 16px 12px;
  }

  .container {
    gap: 16px;
  }

  .surface-card, .wallet-card {
    padding: 24px;
    border-radius: 24px;
  }

  .profile-row {
    flex-direction: column;
    text-align: center;
    gap: 16px;
  }

  .avatar-glow {
    width: 80px;
    height: 80px;
  }

  .profile-info h2 {
    font-size: 28px;
  }

  /* Специфичная сетка для карточек заказов на мобилках */
  .order-pill {
    padding: 16px;
    display: grid;
    grid-template-columns: 48px 1fr auto;
    grid-template-rows: auto auto;
    gap: 12px 16px;
  }

  .op-icon {
    grid-column: 1;
    grid-row: 1 / 3;
    margin: 0;
    align-self: center;
  }

  .op-info {
    grid-column: 2;
    grid-row: 1;
    align-self: end;
  }

  .op-price {
    grid-column: 3;
    grid-row: 1;
    margin: 0;
    text-align: right;
    align-self: end;
    font-size: 16px;
  }

  .op-status {
    grid-column: 2;
    grid-row: 2;
    margin: 0;
    width: fit-content;
    padding: 4px 10px;
    font-size: 12px;
    align-self: start;
  }

  .op-actions {
    grid-column: 3;
    grid-row: 2;
    justify-content: flex-end;
    align-self: start;
  }

  .btn-elegant {
    width: 36px;
    height: 36px;
    font-size: 16px;
  }
}

/* Top-up Modal Styles */
.topup-modal-overlay {
  position: fixed;
  top: 0; left: 0; right: 0; bottom: 0;
  background: rgba(15, 23, 42, 0.4);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
  z-index: 2000;
  animation: fadeIn 0.3s ease-out;
  font-family: 'Outfit', sans-serif;
  color: var(--text-body);
}

.topup-modal-card {
  background: rgba(255, 255, 255, 0.85);
  backdrop-filter: blur(24px);
  -webkit-backdrop-filter: blur(24px);
  border: 1px solid rgba(255, 255, 255, 0.8);
  border-radius: var(--rad-lg);
  width: 100%;
  max-width: 420px;
  box-shadow: var(--shadow-float);
  padding: 32px;
  position: relative;
  animation: slideUp 0.4s cubic-bezier(0.16, 1, 0.3, 1);
}

.topup-modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 24px;
}

.topup-modal-title {
  font-size: 22px;
  font-weight: 700;
  color: var(--text-title);
  letter-spacing: -0.5px;
}

.btn-close-topup {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  border: 1px solid rgba(255,255,255,0.8);
  background: rgba(255,255,255,0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  color: var(--text-muted);
  cursor: pointer;
  transition: var(--transition);
}

.btn-close-topup:hover {
  background: #ffffff;
  color: #ef4444;
  box-shadow: 0 4px 12px rgba(0,0,0,0.05);
  transform: rotate(90deg);
}

/* Form input & icon inside modal */
.topup-modal-card .input-wrapper {
  position: relative;
  display: flex;
  align-items: center;
  width: 100%;
}

.topup-modal-card .input-icon {
  position: absolute;
  left: 20px;
  font-size: 20px;
  color: var(--text-muted);
  pointer-events: none;
  transition: var(--transition);
}

.topup-modal-card .form-input {
  width: 100%;
  padding: 18px 20px 18px 52px;
  border-radius: 16px;
  background: var(--surface-input);
  border: 1.5px solid rgba(255, 255, 255, 0.8);
  font-family: inherit;
  font-size: 20px;
  color: var(--text-title);
  font-weight: 600;
  transition: var(--transition);
  box-shadow: inset 0 2px 4px rgba(0,0,0,0.01);
}

.topup-modal-card .form-input:focus {
  outline: none;
  border-color: var(--accent-main);
  background: #ffffff;
  box-shadow: 0 0 0 4px rgba(99, 102, 241, 0.1);
}

.topup-modal-card .form-input:focus + .input-icon {
  color: var(--accent-main);
}

.quick-amounts {
  display: flex;
  gap: 8px;
  margin-top: 12px;
  flex-wrap: wrap;
}

.amount-pill {
  padding: 8px 16px;
  border-radius: 99px;
  background: var(--surface-input);
  border: 1px solid rgba(255,255,255,0.8);
  font-size: 13px;
  font-weight: 600;
  color: var(--text-body);
  cursor: pointer;
  transition: var(--transition);
}

.amount-pill:hover {
  background: #e0e7ff;
  color: var(--accent-main);
  border-color: var(--accent-main);
  transform: translateY(-1px);
}

.btn-submit-topup {
  width: 100%;
  padding: 18px;
  border-radius: 16px;
  font-size: 16px;
  font-weight: 600;
  cursor: pointer;
  transition: var(--transition);
  border: none;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  background: linear-gradient(135deg, #6366f1, #4f46e5);
  color: white;
  box-shadow: 0 10px 24px -6px var(--accent-glow);
  margin-top: 32px;
}

.btn-submit-topup:hover {
  transform: translateY(-2px);
  box-shadow: 0 15px 30px -6px rgba(99, 102, 241, 0.6);
}
</style>
