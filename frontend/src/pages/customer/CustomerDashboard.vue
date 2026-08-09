<template>
  <div class="customer-dashboard">
    <!-- Header: phone + balance -->
    <div class="dashboard-header mb-4">
      <div class="d-flex justify-content-between align-items-center">
        <div>
          <h1 class="va-h5 m-0">{{ phone }}</h1>
          <span class="text-secondary text-sm">{{ $t('customer.title') }}</span>
        </div>
        <div class="text-right">
          <LanguageSwitcher class="mb-2" />
          <div class="balance-amount">{{ currencySymbol }}{{ Number(balance).toFixed(2) }}</div>
          <div class="text-secondary text-xs">{{ $t('customer.balance') }}</div>
          <va-button color="danger" outline size="small" class="mt-2" @click="handleLogout">
            <va-icon name="logout" class="mr-1" /> {{ $t('app.logout') }}
          </va-button>
        </div>
      </div>
    </div>

    <update-banner />

    <!-- Alert messages -->
    <va-alert v-if="successMsg" color="success" class="mb-3" closeable @dismissed="successMsg = ''">
      {{ successMsg }}
    </va-alert>
    <va-alert v-if="errorMsg" color="danger" class="mb-3" closeable @dismissed="errorMsg = ''">
      {{ errorMsg }}
    </va-alert>

    <!-- Top-up button (small, left aligned) -->
    <div class="mb-3">
      <va-button color="primary" outline size="small" @click="showTopUpModal = true">
        <va-icon name="payment" class="mr-1" /> {{ $t('customer.requestWalletTopUp') }}
      </va-button>
    </div>

    <!-- Create order button (large) -->
    <div class="mb-4">
      <va-button color="success" block size="large" @click="openCreateOrderModal">
        <va-icon name="shopping_cart" class="mr-2" /> {{ $t('customer.createOrder') }}
      </va-button>
    </div>

    <!-- Orders table -->
    <va-card class="shadow-card">
      <div class="d-flex justify-content-between align-items-center mb-3 p-3 pb-0">
        <h3 class="va-h6 m-0">{{ $t('customer.yourOrders') }}</h3>
        <va-button icon="refresh" color="secondary" size="small" flat @click="fetchOrders" />
      </div>

      <div v-if="orders.length === 0" class="text-center py-5">
        <va-icon name="inbox" size="large" color="secondary" class="mb-3" />
        <p class="text-secondary">{{ $t('customer.noOrders') }}</p>
      </div>

      <va-data-table
        v-else
        :items="orders"
        :columns="orderColumns"
        striped
        hoverable
      >
        <template #cell(id)="{ rowData }">
          <span class="font-bold text-sm">#{{ rowData.id.slice(0, 8) }}</span>
        </template>

        <template #cell(type)="{ rowData }">
          {{ formatOrderType(rowData) }}
        </template>

        <template #cell(hold_amount)="{ value }">
          <strong>{{ currencySymbol }}{{ Number(value).toFixed(2) }}</strong>
        </template>

        <template #cell(status)="{ value }">
          <va-badge :color="getStatusColor(value)">{{ value }}</va-badge>
        </template>

        <template #cell(actions)="{ rowData }">
          <div class="d-flex gap-1">
            <va-button
              v-if="rowData.status === 'ASSIGNED'"
              color="info"
              outline
              size="small"
              @click="openChat(rowData)"
            >
              <va-icon name="chat" />
            </va-button>
            <va-button
              v-if="rowData.status === 'ASSIGNED'"
              color="success"
              size="small"
              @click="confirmOrder(rowData.id)"
            >
              <va-icon name="check" />
            </va-button>
            <va-button
              v-if="rowData.status === 'SEARCHING' || rowData.status === 'ASSIGNED'"
              color="danger"
              outline
              size="small"
              @click="cancelOrder(rowData.id)"
            >
              <va-icon name="close" />
            </va-button>
          </div>
        </template>
      </va-data-table>
    </va-card>

    <!-- Create Order Modal -->
    <va-modal
      v-model="showCreateOrderModal"
      :title="$t('customer.createNewOrder')"
      hide-default-actions
    >
      <div class="p-2">
        <div class="mb-4">
          <div class="text-secondary text-sm mb-2">
            {{ $t('customer.pickupAddress') }}
          </div>
          <div class="font-medium mb-2">{{ orderAddress }}</div>
          <div class="text-secondary text-xs">
            {{ $t('customer.addressChangeHint') }}
          </div>
          <div v-if="orderLat !== null && orderLon !== null" class="text-secondary text-xs mt-2">
            {{ $t('customer.coordinates') }}: {{ orderLat.toFixed(5) }}, {{ orderLon.toFixed(5) }}
          </div>
          <div v-if="geocodeError" class="text-danger text-xs mt-2">
            {{ geocodeError }}
          </div>
        </div>

        <div class="mb-4">
          <va-select
            v-model="selectedCategoryId"
            :options="categoryOptions"
            :label="$t('customer.category')"
            text-by="label"
            value-by="value"
            track-by="value"
            class="mb-2"
          />
          <va-select
            v-if="subCategoryOptions.length > 0"
            v-model="selectedSubCategoryId"
            :options="subCategoryOptions"
            :label="$t('customer.subCategory')"
            text-by="label"
            value-by="value"
            track-by="value"
            class="mb-2"
          />
          <va-select
            v-if="variantOptions.length > 0"
            v-model="selectedVariantId"
            :options="variantOptions"
            :label="$t('customer.serviceVariant')"
            text-by="label"
            value-by="value"
            track-by="value"
            class="mb-2"
          />
          <div v-if="!isAuctionSelected" class="d-flex gap-2 mt-2">
            <va-checkbox v-model="isUrgent" :label="$t('customer.urgent')" />
            <va-checkbox v-model="isAsap" :label="$t('customer.asap')" />
          </div>
          <div class="text-secondary text-sm mt-2">
            {{ $t('customer.price') }}: <strong class="text-primary">{{ currencySymbol }}{{ Number(selectedPrice).toFixed(2) }}</strong>
          </div>
        </div>

        <va-button color="success" block :loading="creatingOrder" @click="submitOrder">
          {{ $t('customer.createOrder') }}
        </va-button>
      </div>
    </va-modal>

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

    <!-- Sliding Chat Panel -->
    <div :class="['chat-panel shadow-lg', { open: selectedChatOrder }]">
      <div class="chat-header d-flex justify-content-between align-items-center bg-primary text-white p-3">
        <div>
          <h4 class="m-0 text-white font-bold text-sm">{{ $t('customer.orderChatTitle', { id: selectedChatOrder?.id.slice(0, 8) }) }}</h4>
          <span class="text-xs opacity-75">{{ $t('customer.chatSubtitle') }}</span>
        </div>
        <va-button color="warning" size="small" flat @click="closeChat">{{ $t('common.close') }}</va-button>
      </div>

      <div class="chat-messages p-3 flex-grow-1" ref="messagesContainer">
        <div v-if="chatLocked" class="text-center py-2 mb-3 bg-danger-light text-danger rounded text-xs">
          {{ $t('customer.chatLocked') }}
        </div>

        <div
          v-for="msg in chatMessages"
          :key="msg.id"
          :class="['message-bubble mb-2 p-2 rounded', msg.sender_id === authStore.userID ? 'my-message ml-auto bg-primary text-white' : 'their-message mr-auto bg-light']"
        >
          <div class="text-xs opacity-75 mb-1" v-if="msg.sender_id !== authStore.userID">{{ $t('common.executor') }}</div>
          <div class="text-sm message-text">{{ msg.text }}</div>
          <div class="text-xxs text-right mt-1 opacity-75">{{ formatTime(msg.created_at) }}</div>
        </div>
      </div>

      <div class="chat-input-area p-3 bg-white border-top">
        <va-form @submit.prevent="sendChatMessage" class="d-flex">
          <va-input
            v-model="chatText"
            :placeholder="$t('customer.typeMessage')"
            class="flex-grow-1 mr-2"
            :disabled="chatLocked"
            required
          />
          <va-button type="submit" color="primary" :disabled="chatLocked">{{ $t('customer.send') }}</va-button>
        </va-form>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, onMounted, onUnmounted, computed, nextTick, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../../stores/auth-store'
import LanguageSwitcher from '../../components/LanguageSwitcher.vue'
import UpdateBanner from '../../components/UpdateBanner.vue'
import api from '../../services/api'
import { getServiceCategories, getServiceCategoryChildren, type ServiceNode } from '../../api/services'

export default defineComponent({
  name: 'CustomerDashboard',
  components: { LanguageSwitcher, UpdateBanner },
  setup() {
    const router = useRouter()
    const { t, locale } = useI18n()
    const authStore = useAuthStore()

    const phone = ref('')
    const balance = ref(0)

    const topUpAmount = ref(100)
    const submitting = ref(false)
    const successMsg = ref('')
    const errorMsg = ref('')

    // Orders state
    const orders = ref<any[]>([])
    const creatingOrder = ref(false)

    // Default address for the user
    const defaultAddress = ref('Москва, ул. Тверская, д. 1')
    const orderAddress = ref(defaultAddress.value)
    const orderLat = ref<number | null>(null)
    const orderLon = ref<number | null>(null)
    const geocoding = ref(false)
    const geocodeError = ref('')

    // Service catalog selection
    const serviceCategories = ref<ServiceNode[]>([])
    const subCategories = ref<ServiceNode[]>([])
    const serviceVariants = ref<ServiceNode[]>([])
    const selectedCategoryId = ref<string | null>(null)
    const selectedSubCategoryId = ref<string | null>(null)
    const selectedVariantId = ref<string | null>(null)
    const isUrgent = ref(false)
    const isAsap = ref(false)

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

    const currencySymbol = computed(() => {
      return authStore.currency === 'RUB' ? '₽' : '$'
    })

    // Bids cache
    const bidsMap = ref<Record<string, any[]>>({})

    // Chat state
    const selectedChatOrder = ref<any>(null)
    const chatMessages = ref<any[]>([])
    const chatText = ref('')
    const chatLocked = ref(false)
    const ws = ref<WebSocket | null>(null)
    const messagesContainer = ref<any>(null)

    // Modals
    const showCreateOrderModal = ref(false)
    const showTopUpModal = ref(false)

    const orderColumns = [
      { key: 'id', label: 'ID' },
      { key: 'type', label: t('customer.orderType') },
      { key: 'hold_amount', label: t('customer.price') },
      { key: 'status', label: t('customer.status') },
      { key: 'actions', label: '' },
    ]

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
        await fetchBidsForSearchingOrders()
      } catch (err) {
        console.error('Failed to fetch orders:', err)
      }
    }

    const fetchBidsForSearchingOrders = async () => {
      for (const order of orders.value) {
        if (order.service_variant?.is_auction && order.status === 'SEARCHING') {
          try {
            const response = await api.get(`/customer/orders/${order.id}/bids`)
            bidsMap.value[order.id] = response.data || []
          } catch (err) {
            console.error('Failed to fetch bids:', err)
          }
        }
      }
    }

    const openCreateOrderModal = async () => {
      orderAddress.value = defaultAddress.value
      orderLat.value = null
      orderLon.value = null
      geocodeError.value = ''
      showCreateOrderModal.value = true
      await geocodeAddress()
    }

    const geocodeAddress = async () => {
      geocoding.value = true
      geocodeError.value = ''
      orderLat.value = null
      orderLon.value = null
      try {
        const response = await api.get('/geo/geocode', { params: { q: orderAddress.value } })
        orderLat.value = response.data.lat
        orderLon.value = response.data.lon
        orderAddress.value = response.data.address || orderAddress.value
      } catch (err: any) {
        geocodeError.value = err.response?.data || t('customer.topUpError')
      } finally {
        geocoding.value = false
      }
    }

    const submitTopUp = async () => {
      successMsg.value = ''
      errorMsg.value = ''
      submitting.value = true
      try {
        await api.post('/customer/finances/topup', { amount: topUpAmount.value })
        successMsg.value = t('customer.topUpSuccess', { amount: topUpAmount.value.toFixed(2) })
        topUpAmount.value = 100
        showTopUpModal.value = false
        await fetchProfile()
      } catch (err: any) {
        errorMsg.value = err.response?.data || t('customer.topUpError')
        console.error(err)
      } finally {
        submitting.value = false
      }
    }

    const submitOrder = async () => {
      successMsg.value = ''
      errorMsg.value = ''
      creatingOrder.value = true
      try {
        if (!selectedVariantId.value) {
          errorMsg.value = t('customer.errorInvalidOrderType')
          return
        }
        const payload: any = {
          service_variant_id: selectedVariantId.value,
          is_urgent: !isAuctionSelected.value && isUrgent.value,
          is_asap: !isAuctionSelected.value && isAsap.value,
          address: orderAddress.value,
        }
        if (orderLat.value !== null && orderLon.value !== null) {
          payload.lat = orderLat.value
          payload.lon = orderLon.value
        }
        await api.post('/customer/orders', payload)
        successMsg.value = t('customer.successOrderCreated')
        showCreateOrderModal.value = false
        await fetchProfile()
        await fetchOrders()
      } catch (err: any) {
        errorMsg.value = err.response?.data || t('customer.errorOrderCreated')
        console.error(err)
      } finally {
        creatingOrder.value = false
      }
    }

    const confirmOrder = async (orderId: string) => {
      successMsg.value = ''
      errorMsg.value = ''
      try {
        await api.post(`/customer/orders/${orderId}/confirm`)
        successMsg.value = t('customer.successDeliveryConfirmed')
        await fetchProfile()
        await fetchOrders()
        if (selectedChatOrder.value && selectedChatOrder.value.id === orderId) {
          chatLocked.value = true
        }
      } catch (err: any) {
        errorMsg.value = err.response?.data || t('customer.errorDeliveryConfirmed')
        console.error(err)
      }
    }

    const cancelOrder = async (orderId: string) => {
      successMsg.value = ''
      errorMsg.value = ''
      try {
        await api.post(`/customer/orders/${orderId}/cancel`)
        successMsg.value = t('customer.successOrderCancelled')
        await fetchProfile()
        await fetchOrders()
        if (selectedChatOrder.value && selectedChatOrder.value.id === orderId) {
          chatLocked.value = true
        }
      } catch (err: any) {
        errorMsg.value = err.response?.data || t('customer.errorOrderCancelled')
        console.error(err)
      }
    }

    const acceptBid = async (bidId: string) => {
      successMsg.value = ''
      errorMsg.value = ''
      try {
        await api.post(`/customer/bids/${bidId}/accept`)
        successMsg.value = t('customer.successBidAccepted')
        await fetchProfile()
        await fetchOrders()
      } catch (err: any) {
        errorMsg.value = err.response?.data || t('customer.errorBidAccepted')
        console.error(err)
      }
    }

    const formatOrderType = (order: any) => {
      const variant = order.service_variant
      if (!variant) return order.service_variant_id
      const name = localizedName(variant)
      if (order.is_asap) return `${name} (${t('customer.asap')})`
      if (order.is_urgent) return `${name} (${t('customer.urgent')})`
      return name
    }

    // Chat operations
    const openChat = async (order: any) => {
      selectedChatOrder.value = order
      chatMessages.value = []
      chatLocked.value = false

      // Load history
      try {
        const response = await api.get(`/chats/${order.id}/messages`)
        chatMessages.value = response.data || []
        scrollToBottom()
      } catch (err) {
        console.error(err)
      }

      // Open websocket
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
      const apiBaseUrl = import.meta.env.VITE_API_URL || 'http://backend:8080'
      const wsHost = apiBaseUrl.replace('http://', '').replace('https://', '')
      const wsUrl = `${protocol}//${wsHost}/chats/${order.id}/ws?token=${encodeURIComponent(authStore.token)}`

      if (ws.value) {
        ws.value.close()
      }

      ws.value = new WebSocket(wsUrl)
      ws.value.onmessage = (event) => {
        const data = JSON.parse(event.data)
        if (data.type === 'system' && data.action === 'lock') {
          chatLocked.value = true
        } else if (data.type === 'system' && data.action === 'downgrade') {
          order.is_urgent = data.is_urgent
          order.is_asap = data.is_asap
          order.final_amount = data.final_amount
          order.is_downgraded = true
        } else if (data.type === 'error') {
          console.warn(data.message)
        } else {
          chatMessages.value.push(data)
          scrollToBottom()
        }
      }
    }

    const sendChatMessage = () => {
      if (!chatText.value.trim() || !ws.value || chatLocked.value) return
      ws.value.send(JSON.stringify({ text: chatText.value }))
      chatText.value = ''
    }

    const closeChat = () => {
      selectedChatOrder.value = null
      if (ws.value) {
        ws.value.close()
        ws.value = null
      }
    }

    const scrollToBottom = () => {
      nextTick(() => {
        if (messagesContainer.value) {
          messagesContainer.value.scrollTop = messagesContainer.value.scrollHeight
        }
      })
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

    const formatTime = (dateStr: string) => {
      if (!dateStr) return ''
      const d = new Date(dateStr)
      return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    }

    const handleLogout = async () => {
      try {
        await api.post('/logout')
      } catch (e) {
        console.error(e)
      } finally {
        authStore.logout()
        router.push('/login')
      }
    }

    watch(selectedCategoryId, async (id) => {
      selectedSubCategoryId.value = null
      selectedVariantId.value = null
      serviceVariants.value = []
      if (!id) {
        subCategories.value = []
        return
      }
      const children = await getServiceCategoryChildren(id)
      const categories = children.filter((c) => c.node_type === 'CATEGORY')
      const variants = children.filter((c) => c.node_type === 'VARIANT')
      if (categories.length > 0) {
        subCategories.value = categories
      } else {
        subCategories.value = []
        serviceVariants.value = variants
      }
    })

    watch(selectedSubCategoryId, async (id) => {
      selectedVariantId.value = null
      if (!id) {
        serviceVariants.value = []
        return
      }
      serviceVariants.value = await getCategoryVariants(id)
    })

    watch(selectedVariantId, () => {
      isUrgent.value = false
      isAsap.value = false
    })

    let intervalId: any = null

    onMounted(async () => {
      fetchProfile()
      serviceCategories.value = await getServiceCategories()
      fetchOrders()
      intervalId = setInterval(() => {
        fetchProfile()
        fetchOrders()
      }, 5000)
    })

    onUnmounted(() => {
      if (intervalId) clearInterval(intervalId)
      if (ws.value) ws.value.close()
    })

    return {
      authStore,
      phone,
      balance,
      topUpAmount,
      submitting,
      successMsg,
      errorMsg,
      orders,
      creatingOrder,
      defaultAddress,
      serviceCategories,
      subCategories,
      serviceVariants,
      selectedCategoryId,
      selectedSubCategoryId,
      selectedVariantId,
      isUrgent,
      isAsap,
      selectedVariant,
      selectedPrice,
      isAuctionSelected,
      categoryOptions,
      subCategoryOptions,
      variantOptions,
      localizedName,
      bidsMap,
      selectedChatOrder,
      chatMessages,
      chatText,
      chatLocked,
      messagesContainer,
      showCreateOrderModal,
      showTopUpModal,
      orderColumns,
      orderAddress,
      orderLat,
      orderLon,
      geocoding,
      geocodeError,
      geocodeAddress,
      fetchProfile,
      fetchOrders,
      submitTopUp,
      submitOrder,
      confirmOrder,
      cancelOrder,
      acceptBid,
      openCreateOrderModal,
      openChat,
      sendChatMessage,
      closeChat,
      formatOrderType,
      getStatusColor,
      formatTime,
      handleLogout,
    }
  },
})
</script>

<style scoped>
.customer-dashboard {
  max-width: 1200px;
  margin: 20px auto;
  padding: 0 16px;
  position: relative;
}

.dashboard-header {
  background: #fff;
  border-radius: 12px;
  padding: 16px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.05);
}

.balance-amount {
  font-size: 1.8rem;
  font-weight: 800;
  color: #2b6cb0;
  line-height: 1.2;
}

.shadow-card {
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.05);
  border-radius: 12px !important;
}

/* Chat panel sliding out from the right */
.chat-panel {
  position: fixed;
  top: 0;
  right: 0;
  width: 400px;
  height: 100%;
  background: rgba(255, 255, 255, 0.95);
  backdrop-filter: blur(10px);
  z-index: 1000;
  transform: translateX(100%);
  transition: transform 0.3s cubic-bezier(0.16, 1, 0.3, 1);
  display: flex;
  flex-direction: column;
}

.chat-panel.open {
  transform: translateX(0);
}

.chat-messages {
  flex-grow: 1;
  overflow-y: auto;
}

.message-bubble {
  max-width: 80%;
  clear: both;
}

.my-message {
  border-radius: 12px 12px 0 12px;
}

.their-message {
  border-radius: 12px 12px 12px 0;
}

.bg-light {
  background-color: #f8fafc;
}

.bg-danger-light {
  background-color: #fff5f5;
}

.rounded {
  border-radius: 8px;
}

.d-flex {
  display: flex;
}

.flex-column {
  flex-direction: column;
}

.flex-grow-1 {
  flex-grow: 1;
}

.justify-content-between {
  justify-content: space-between;
}

.align-items-center {
  align-items: center;
}

.m-0 {
  margin: 0;
}

.mr-1 {
  margin-right: 4px;
}

.mr-2 {
  margin-right: 8px;
}

.ml-auto {
  margin-left: auto;
}

.mb-1 {
  margin-bottom: 4px;
}

.mb-2 {
  margin-bottom: 8px;
}

.mb-3 {
  margin-bottom: 12px;
}

.mb-4 {
  margin-bottom: 16px;
}

.mt-1 {
  margin-top: 4px;
}

.mt-2 {
  margin-top: 8px;
}

.p-2 {
  padding: 8px;
}

.p-3 {
  padding: 12px;
}

.text-right {
  text-align: right;
}

.text-secondary {
  color: #718096;
}

.text-primary {
  color: #2b6cb0;
}

.text-xs {
  font-size: 0.75rem;
}

.text-sm {
  font-size: 0.875rem;
}

.text-xxs {
  font-size: 0.65rem;
}

.font-bold {
  font-weight: 700;
}

.border-top {
  border-top: 1px solid #edf2f7;
}

@media (max-width: 992px) {
  .chat-panel {
    width: 100%;
  }
}

@media (max-width: 576px) {
  .balance-amount {
    font-size: 1.4rem;
  }
}
</style>
