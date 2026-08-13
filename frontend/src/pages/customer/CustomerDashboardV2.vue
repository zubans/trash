<template>
  <div class="customer-dashboard-v2 container py-4">
    <!-- Header V2 -->
    <CustomerHeaderV2
      :phone="phone"
      :balance="balance"
      :currency-symbol="currencySymbol"
      :is-verified="isVerified"
      @logout="handleLogout"
      @open-profile-modal="showProfileModal = true"
      @open-top-up-modal="showTopUpModal = true"
    />

    <update-banner />

    <!-- Alert messages -->
    <va-alert v-if="successMsg" color="success" class="mb-3" closeable @dismissed="successMsg = ''">
      {{ successMsg }}
    </va-alert>
    <va-alert v-if="errorMsg" color="danger" class="mb-3" closeable @dismissed="errorMsg = ''">
      {{ errorMsg }}
    </va-alert>

    <!-- Large Green Create Order Button -->
    <div class="mb-4">
      <button type="button" class="btn-create-order-v2 w-100" @click="openCreateOrderModal">
        🛒 Создать заказ
      </button>
    </div>

    <!-- Active Orders Table Card (Exact screenshot style) -->
    <div class="card shadow-sm border-0 rounded-2xl mb-4 bg-white overflow-hidden">
      <div class="d-flex justify-content-between align-items-center p-4 border-bottom">
        <div class="d-flex align-items-center gap-2">
          <span class="text-primary font-bold">📋 Активные заказы</span>
        </div>
        <button type="button" class="btn-refresh-icon border-0 bg-transparent text-secondary" title="Обновить" @click="fetchOrders">
          ↻
        </button>
      </div>

      <div v-if="activeOrders.length === 0" class="text-center py-5 text-secondary">
        <p class="text-secondary text-sm m-0">{{ $t('customer.noActiveOrders') }}</p>
      </div>

      <div v-else class="table-responsive p-4 pt-2">
        <table class="table align-middle text-sm m-0">
          <thead>
            <tr class="text-secondary text-xs uppercase tracking-wider">
              <th>ID</th>
              <th>ТИП ЗАКАЗА</th>
              <th>ЦЕНА</th>
              <th>СТАТУС</th>
              <th class="text-end">УПРАВЛЕНИЕ</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="order in activeOrders" :key="order.id">
              <td class="font-bold text-primary cursor-pointer" @click="openOrderDetails(order)">
                #{{ order.id.slice(0, 8) }}
              </td>
              <td>{{ formatOrderType(order) }}</td>
              <td class="font-bold text-dark">{{ Number(order.hold_amount).toFixed(2) }} ₽</td>
              <td class="font-bold uppercase text-xs">{{ order.status === 'ASSIGNED' ? 'НАЗНАЧЕН' : 'ПОИСК' }}</td>
              <td class="text-end">
                <div class="d-flex align-items-center justify-content-end gap-1">
                  <button type="button" class="btn-round-blue" title="Подробнее" @click="openOrderDetails(order)">ℹ</button>
                  <button
                    v-if="order.status === 'ASSIGNED'"
                    type="button"
                    class="btn-round-blue position-relative"
                    title="Чат"
                    @click="openChat(order)"
                  >
                    💬
                    <span v-if="unreadOrderIDs.has(order.id)" class="yellow-unread-dot"></span>
                  </button>
                  <button
                    v-if="order.status === 'ASSIGNED'"
                    type="button"
                    class="btn-round-blue text-success"
                    title="Подтвердить"
                    @click="confirmOrder(order.id)"
                  >
                    ✓
                  </button>
                  <button
                    v-if="order.status === 'SEARCHING' || order.status === 'ASSIGNED'"
                    type="button"
                    class="btn-round-blue text-danger"
                    title="Отменить"
                    @click="cancelOrder(order.id)"
                  >
                    ✕
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Order History V2 -->
    <OrderHistoryCardV2
      :history-orders="historyOrders"
      :order-columns="orderColumns"
      :currency-symbol="currencySymbol"
      :format-order-type="formatOrderType"
      :get-status-color="getStatusColor"
      @open-order-details="openOrderDetails"
    />

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

    <!-- Customer Profile Modal -->
    <CustomerProfileModal
      v-model="showProfileModal"
      v-model:new-address-input="newAddressInput"
      :is-verified="isVerified"
      :customer-addresses="customerAddresses"
      :default-address="defaultAddress"
      @set-active-address="setActiveAddress"
      @add-new-address="addNewAddress"
      @remove-address="removeAddress"
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

    <!-- Chat Drawer Component -->
    <div :class="['chat-panel', { open: !!selectedChatOrder }]" :style="chatPanelStyle">
      <div class="chat-header p-3 d-flex align-items-center justify-content-between bg-telegram text-white">
        <div class="d-flex align-items-center gap-2 overflow-hidden">
          <div class="telegram-avatar">
            {{ selectedChatOrder?.executor_phone ? selectedChatOrder.executor_phone.slice(-2) : 'EX' }}
          </div>
          <div>
            <div class="font-bold text-sm truncate">
              {{ selectedChatOrder?.executor_phone || $t('common.executor') }}
            </div>
            <div class="text-xxs text-online d-flex align-items-center gap-1">
              <span class="online-dot"></span> {{ $t('customer.chatOnline') }}
            </div>
          </div>
        </div>
        <button type="button" class="btn-close-chat" @click="closeChat">
          ✕
        </button>
      </div>

      <div class="chat-messages telegram-bg p-3 flex-grow-1" ref="messagesContainer">
        <div v-if="chatLocked" class="text-center py-2 mb-3 bg-danger-light text-danger rounded-lg text-xs font-semibold shadow-sm">
          {{ $t('customer.chatLocked') }}
        </div>

        <div
          v-for="msg in chatMessages"
          :key="msg.id"
          :class="['telegram-bubble', msg.sender_id === authStore.userID ? 'my-telegram-msg ml-auto' : 'their-telegram-msg mr-auto']"
        >
          <div class="telegram-sender" v-if="msg.sender_id !== authStore.userID">{{ $t('common.executor') }}</div>
          
          <div v-if="msg.file_url" class="telegram-attachment mb-2">
            <div v-if="isImageAttachment(msg)" class="attachment-image-wrapper">
              <img
                :src="getImageSrc(msg.file_url)"
                class="attachment-img rounded-lg shadow-sm cursor-pointer"
                alt="photo"
                @click="openImagePreview(getImageSrc(msg.file_url))"
                @error="onChatImgError(msg.file_url)"
              />
            </div>
            <div v-else class="attachment-doc-wrapper p-2 bg-white-10 rounded d-flex align-items-center">
              <span class="doc-icon mr-2">📄</span>
              <div class="flex-grow-1 overflow-hidden">
                <a :href="resolveFileUrl(msg.file_url)" target="_blank" download class="font-bold text-xs text-white truncate d-block">
                  {{ msg.file_name || 'document' }}
                </a>
              </div>
              <a :href="resolveFileUrl(msg.file_url)" target="_blank" download class="btn-download ml-2">⬇</a>
            </div>
          </div>

          <div v-if="msg.text" class="telegram-text">{{ msg.text }}</div>
        </div>
      </div>

      <div class="chat-input-area p-2 bg-white border-top">
        <div class="d-flex align-items-center telegram-input-row">
          <input
            v-model="chatText"
            :placeholder="$t('customer.typeMessage')"
            class="telegram-input flex-grow-1 p-2 px-3"
            :disabled="chatLocked || uploadingFile"
            @keyup.enter="sendChatMessage"
          />
          <button
            type="button"
            class="telegram-send-btn ml-2"
            :disabled="chatLocked || uploadingFile || (!chatText.trim() && !uploadingFile)"
            @click="sendChatMessage"
          >
            ➤
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, onMounted, onUnmounted, computed, watch, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Capacitor } from '@capacitor/core'
import { useAuthStore } from '../../stores/auth-store'
import LanguageSwitcher from '../../components/LanguageSwitcher.vue'
import UpdateBanner from '../../components/UpdateBanner.vue'
import api, { buildChatWebSocketUrl, resolveFileUrl, formatApiError, isDebug } from '../../services/api'
import { NativeWebSocket } from '../../plugins/native-websocket'
import {
  getServiceCategories,
  getServiceCategoryChildren,
  type ServiceNode,
} from '../../api/services'

import CustomerHeaderV2 from './components/CustomerHeaderV2.vue'
import OrderHistoryCardV2 from './components/OrderHistoryCardV2.vue'
import OrderDetailsModal from './components/OrderDetailsModal.vue'
import CreateOrderModal from './components/CreateOrderModal.vue'
import CustomerProfileModal from './components/CustomerProfileModal.vue'

export default defineComponent({
  name: 'CustomerDashboardV2',
  components: {
    LanguageSwitcher,
    UpdateBanner,
    CustomerHeaderV2,
    OrderHistoryCardV2,
    OrderDetailsModal,
    CreateOrderModal,
    CustomerProfileModal,
  },
  setup() {
    const router = useRouter()
    const { t, locale } = useI18n()
    const authStore = useAuthStore()

    const phone = ref('')
    const balance = ref(0)
    const isVerified = ref(true)
    const showProfileModal = ref(false)
    const showTopUpModal = ref(false)
    const showCreateOrderModal = ref(false)
    const showOrderDetailsModal = ref(false)
    const selectedOrderDetails = ref<any>(null)
    const topUpAmount = ref(100)
    const submitting = ref(false)
    const successMsg = ref('')
    const errorMsg = ref('')
    const orders = ref<any[]>([])

    const defaultAddress = ref('Москва, ул. Тверская, д. 1')
    const orderAddress = ref(defaultAddress.value)
    const orderLat = ref<number | null>(null)
    const orderLon = ref<number | null>(null)
    const geocodeError = ref('')
    const newAddressInput = ref('')

    const customerAddresses = ref<{ id?: string; address: string; is_default?: boolean }[]>([
      { address: 'Москва, ул. Тверская, д. 1', is_default: true }
    ])

    const setActiveAddress = (addr: string) => {
      defaultAddress.value = addr
      orderAddress.value = addr
      customerAddresses.value = customerAddresses.value.map(a => ({
        ...a,
        is_default: a.address === addr
      }))
    }

    const addNewAddress = () => {
      if (!newAddressInput.value.trim()) return
      if (customerAddresses.value.length >= 2) return
      customerAddresses.value.push({ address: newAddressInput.value.trim(), is_default: false })
      newAddressInput.value = ''
    }

    const removeAddress = (index: number) => {
      const removed = customerAddresses.value[index]
      customerAddresses.value.splice(index, 1)
      if (removed && removed.address === defaultAddress.value && customerAddresses.value.length > 0) {
        setActiveAddress(customerAddresses.value[0].address)
      }
    }

    const activeOrders = computed(() =>
      orders.value.filter((o) => o.status === 'SEARCHING' || o.status === 'ASSIGNED')
    )
    const historyOrders = computed(() =>
      orders.value.filter((o) => o.status === 'COMPLETED' || o.status === 'CANCELED')
    )

    const orderColumns = [
      { key: 'id', label: 'ID' },
      { key: 'type', label: t('customer.orderType') },
      { key: 'hold_amount', label: t('customer.price') },
      { key: 'status', label: t('customer.status') },
      { key: 'actions', label: '' },
    ]

    const serviceCategories = ref<ServiceNode[]>([])
    const subCategories = ref<ServiceNode[]>([])
    const serviceVariants = ref<ServiceNode[]>([])
    const selectedCategoryId = ref<string | null>(null)
    const selectedSubCategoryId = ref<string | null>(null)
    const selectedVariantId = ref<string | null>(null)
    const isUrgent = ref(false)
    const isAsap = ref(false)
    const creatingOrder = ref(false)

    const selectedVariant = computed(() =>
      serviceVariants.value.find((v) => v.id === selectedVariantId.value)
    )
    const isAuctionSelected = computed(() => selectedVariant.value?.is_auction)

    const localizedName = (node?: ServiceNode) =>
      node?.name[locale.value] || node?.name['ru'] || node?.code || ''

    const categoryOptions = computed(() =>
      serviceCategories.value.map((c) => ({ label: localizedName(c), value: c.id }))
    )
    const subCategoryOptions = computed(() =>
      subCategories.value.map((c) => ({ label: localizedName(c), value: c.id }))
    )
    const variantOptions = computed(() =>
      serviceVariants.value.map((v) => ({ label: localizedName(v), value: v.id }))
    )

    const selectedPrice = computed(() => {
      const variant = selectedVariant.value
      if (!variant || variant.base_price === undefined) return 0
      let price = variant.base_price
      if (isAuctionSelected.value) return 0
      if (isAsap.value) price *= 8
      else if (isUrgent.value) price *= 3
      return price
    })

    const currencySymbol = computed(() => (authStore.currency === 'RUB' ? '₽' : '$'))

    // Chat state & methods
    const selectedChatOrder = ref<any>(null)
    const chatMessages = ref<any[]>([])
    const chatText = ref('')
    const chatLocked = ref(false)
    const ws = ref<WebSocket | null>(null)
    const messagesContainer = ref<any>(null)
    const isNative = Capacitor.isNativePlatform()
    const uploadingFile = ref(false)
    const unreadOrderIDs = ref(new Set<string>())

    const fetchProfile = async () => {
      try {
        const response = await api.get('/customer/profile')
        if (response.data) {
          phone.value = response.data.phone
          balance.value = response.data.balance
          if (response.data.address) {
            defaultAddress.value = response.data.address
          }
        }
      } catch (err) {
        console.error('Failed to load profile details:', err)
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

    const handleLogout = async () => {
      try {
        await api.post('/logout')
      } catch (e) {
      } finally {
        authStore.logout()
        router.push('/login')
      }
    }

    const openCreateOrderModal = () => {
      showCreateOrderModal.value = true
    }

    const submitOrder = async () => {
      if (!selectedVariantId.value) return
      creatingOrder.value = true
      try {
        await api.post('/customer/orders', {
          service_variant_id: selectedVariantId.value,
          is_urgent: isUrgent.value,
          is_asap: isAsap.value,
          address: orderAddress.value,
          lat: orderLat.value,
          lon: orderLon.value,
        })
        showCreateOrderModal.value = false
        fetchOrders()
      } catch (err: any) {
        errorMsg.value = formatApiError(err, 'Failed to create order')
      } finally {
        creatingOrder.value = false
      }
    }

    const confirmOrder = async (id: string) => {
      try {
        await api.post(`/customer/orders/${id}/confirm`)
        fetchOrders()
      } catch (e) {}
    }

    const cancelOrder = async (id: string) => {
      try {
        await api.post(`/customer/orders/${id}/cancel`)
        fetchOrders()
      } catch (e) {}
    }

    const openOrderDetails = (order: any) => {
      selectedOrderDetails.value = order
      showOrderDetailsModal.value = true
    }

    const formatDateFull = (dateStr: string) => {
      if (!dateStr) return ''
      return new Date(dateStr).toLocaleString()
    }

    const formatOrderType = (order: any) => {
      const variant = order.service_variant
      if (!variant) return order.service_variant_id
      const name = localizedName(variant)
      if (order.is_asap) return `${name} (ASAP)`
      if (order.is_urgent) return `${name} (Срочно)`
      return name
    }

    const getStatusColor = (status: string) => {
      switch (status) {
        case 'SEARCHING': return 'warning'
        case 'ASSIGNED': return 'info'
        case 'COMPLETED': return 'success'
        case 'CANCELED': return 'danger'
        default: return 'secondary'
      }
    }

    const openChat = (order: any) => {
      selectedChatOrder.value = order
    }
    const closeChat = () => {
      selectedChatOrder.value = null
    }

    const sendChatMessage = () => {}
    const isImageAttachment = (msg: any) => false
    const getImageSrc = (path?: string) => ''
    const openImagePreview = (url: string) => {}
    const onChatImgError = (path?: string) => {}

    onMounted(async () => {
      await fetchProfile()
      await fetchOrders()
      try {
        serviceCategories.value = await getServiceCategories()
      } catch (e) {}
    })

    return {
      authStore,
      phone,
      balance,
      isVerified,
      showProfileModal,
      showTopUpModal,
      showCreateOrderModal,
      showOrderDetailsModal,
      selectedOrderDetails,
      topUpAmount,
      submitting,
      successMsg,
      errorMsg,
      orders,
      activeOrders,
      historyOrders,
      customerAddresses,
      defaultAddress,
      orderAddress,
      orderLat,
      orderLon,
      geocodeError,
      newAddressInput,
      setActiveAddress,
      addNewAddress,
      removeAddress,
      orderColumns,
      serviceCategories,
      subCategories,
      serviceVariants,
      selectedCategoryId,
      selectedSubCategoryId,
      selectedVariantId,
      isUrgent,
      isAsap,
      creatingOrder,
      selectedVariant,
      isAuctionSelected,
      categoryOptions,
      subCategoryOptions,
      variantOptions,
      selectedPrice,
      currencySymbol,
      selectedChatOrder,
      chatMessages,
      chatText,
      chatLocked,
      isNative,
      uploadingFile,
      unreadOrderIDs,
      fetchOrders,
      handleLogout,
      openCreateOrderModal,
      submitOrder,
      confirmOrder,
      cancelOrder,
      openOrderDetails,
      formatDateFull,
      formatOrderType,
      getStatusColor,
      openChat,
      closeChat,
      sendChatMessage,
      isImageAttachment,
      getImageSrc,
      openImagePreview,
      onChatImgError,
      submitTopUp: () => {},
      chatPanelStyle: {},
    }
  },
})
</script>

<style scoped>
.btn-create-order-v2 {
  background: #4d8b2c;
  color: #ffffff;
  border: none;
  border-radius: 12px;
  padding: 14px;
  font-size: 1.1rem;
  font-weight: 700;
  cursor: pointer;
  transition: background 0.15s ease;
}

.btn-create-order-v2:hover {
  background: #3e7223;
}

.btn-round-blue {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  border: none;
  background: #2563eb;
  color: #ffffff;
  font-size: 0.75rem;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}

.chat-panel {
  position: fixed;
  top: 0;
  right: 0;
  width: 420px;
  max-width: 100vw;
  height: 100%;
  background: #0f1826;
  z-index: 1000;
  transform: translateX(100%);
  transition: transform 0.28s cubic-bezier(0.2, 0, 0, 1);
  display: flex;
  flex-direction: column;
}

.chat-panel.open {
  transform: translateX(0);
}
</style>
