<template>
  <div class="customer-dashboard">
    <div class="dashboard-header mb-5">
      <div class="d-flex justify-content-between align-items-center">
        <div>
          <h1 class="va-h3 m-0">Customer Dashboard</h1>
          <span class="text-secondary text-sm">Manage profile, wallets, and orders</span>
        </div>
        <va-button color="danger" outline size="small" @click="handleLogout">
          <va-icon name="logout" class="mr-2" /> Logout
        </va-button>
      </div>
    </div>

    <!-- Alert messages -->
    <va-alert v-if="successMsg" color="success" class="mb-4" closeable @dismissed="successMsg = ''">
      {{ successMsg }}
    </va-alert>
    <va-alert v-if="errorMsg" color="danger" class="mb-4" closeable @dismissed="errorMsg = ''">
      {{ errorMsg }}
    </va-alert>

    <div class="row g-4">
      <!-- Left Column: Profile, Top-up, and Order Creation -->
      <div class="col-md-5">
        <!-- Profile Card -->
        <va-card class="p-4 mb-4 shadow-card">
          <h3 class="va-h5 mb-4 text-primary d-flex align-items-center">
            <va-icon name="account_circle" class="mr-2" /> Account Details
          </h3>
          <div class="info-list">
            <div class="info-item mb-3">
              <span class="info-label">Phone</span>
              <span class="info-val">{{ phone }}</span>
            </div>
            <div class="info-item mb-3">
              <span class="info-label">Account Status</span>
              <span class="info-val">
                <va-badge color="success">{{ status }}</va-badge>
              </span>
            </div>
          </div>

          <div class="balance-box mt-4 p-3 text-center">
            <span class="balance-label d-block text-secondary text-sm mb-1">Available Balance</span>
            <span class="balance-amount">${{ Number(balance).toFixed(2) }}</span>
          </div>
        </va-card>

        <!-- Top-up Card -->
        <va-card class="p-4 mb-4 shadow-card">
          <h3 class="va-h5 mb-4 text-primary d-flex align-items-center">
            <va-icon name="payment" class="mr-2" /> Request Wallet Top-Up
          </h3>
          <p class="text-secondary text-sm mb-4">
            Enter the amount to add. An administrator will verify and approve your request.
          </p>

          <va-form @submit.prevent="submitTopUp">
            <va-input
              v-model.number="topUpAmount"
              type="number"
              label="Amount ($)"
              placeholder="100"
              min="1"
              step="any"
              class="mb-4"
              required
            >
              <template #prependInner>
                <va-icon name="attach_money" />
              </template>
            </va-input>
            
            <va-button type="submit" block :loading="submitting">
              Submit Request
            </va-button>
          </va-form>
        </va-card>

        <!-- Create Order Card -->
        <va-card class="p-4 mb-4 shadow-card">
          <h3 class="va-h5 mb-4 text-primary d-flex align-items-center">
            <va-icon name="shopping_cart" class="mr-2" /> Create New Order
          </h3>
          <p class="text-secondary text-sm mb-4">
            Place an order for waste removal. Funds will be held from your wallet balance.
          </p>

          <va-form @submit.prevent="submitOrder">
            <va-select
              v-model="orderVolume"
              :options="volumeOptions"
              label="Volume Type"
              class="mb-4"
              required
            />
            
            <va-select
              v-if="orderVolume !== 'CONSTRUCTION'"
              v-model="orderTariff"
              :options="tariffOptions"
              label="Speed Tariff"
              class="mb-4"
              required
            />

            <va-input
              v-if="orderVolume === 'CONSTRUCTION'"
              v-model="orderPhoto"
              label="Waste Photo URL"
              placeholder="https://example.com/photo.jpg"
              class="mb-4"
              required
            >
              <template #prependInner>
                <va-icon name="image" />
              </template>
            </va-input>

            <va-input
              v-model="orderGeo"
              label="Delivery Coordinates (lat, lon)"
              placeholder="55.7558, 37.6173"
              class="mb-4"
              required
            >
              <template #prependInner>
                <va-icon name="location_on" />
              </template>
            </va-input>

            <div class="price-preview mb-4 p-3 text-center" v-if="orderVolume !== 'CONSTRUCTION'">
              <span class="price-label d-block text-secondary text-sm mb-1">Estimated Hold Amount</span>
              <span class="price-amount-preview text-primary font-bold">${{ Number(estimatedPrice).toFixed(2) }}</span>
            </div>
            
            <div class="price-preview mb-4 p-3 text-center" v-else>
              <span class="price-label d-block text-secondary text-sm mb-1">Estimated Hold Amount</span>
              <span class="price-amount-preview text-warning font-bold">Bidding / Auction</span>
            </div>

            <va-button type="submit" block :loading="creatingOrder">
              Create Order
            </va-button>
          </va-form>
        </va-card>
      </div>

      <!-- Right Column: Active Orders & History -->
      <div class="col-md-7">
        <va-card class="p-4 shadow-card">
          <div class="d-flex justify-content-between align-items-center mb-4">
            <h3 class="va-h5 m-0 text-primary d-flex align-items-center">
              <va-icon name="list_alt" class="mr-2" /> Your Orders
            </h3>
            <va-button icon="refresh" color="secondary" size="small" flat @click="fetchOrders" />
          </div>

          <div v-if="orders.length === 0" class="text-center py-5">
            <va-icon name="inbox" size="large" color="secondary" class="mb-3" />
            <p class="text-secondary">No orders placed yet.</p>
          </div>

          <div v-else class="orders-list">
            <va-card 
              v-for="order in orders" 
              :key="order.id" 
              class="order-item-card p-3 mb-3"
              outlined
            >
              <div class="d-flex justify-content-between align-items-start mb-2">
                <div>
                  <span class="order-id font-bold text-sm">Order #{{ order.id.slice(0, 8) }}...</span>
                  <span v-if="order.is_downgraded" class="ml-2">
                    <va-badge color="danger">SLA Downgraded</va-badge>
                  </span>
                  <div class="text-xs text-secondary mt-1">
                    Created: {{ formatDate(order.created_at) }}
                  </div>
                </div>
                <va-badge :color="getStatusColor(order.status)" class="text-uppercase">
                  {{ order.status }}
                </va-badge>
              </div>

              <div class="row text-sm mb-3">
                <div class="col-6">
                  <strong>Volume:</strong> {{ order.volume_type }}
                </div>
                <div class="col-6">
                  <strong>Tariff:</strong> {{ order.speed_tariff }}
                </div>
                <div class="col-6 mt-1">
                  <strong>Hold:</strong> ${{ Number(order.hold_amount).toFixed(2) }}
                </div>
                <div class="col-6 mt-1" v-if="order.executor_phone">
                  <strong>Executor:</strong> {{ order.executor_phone }}
                </div>
                <div class="col-12 mt-1" v-if="order.photo_url">
                  <strong>Photo:</strong> <a :href="order.photo_url" target="_blank" class="text-primary text-xs truncate">{{ order.photo_url }}</a>
                </div>
              </div>

              <!-- Bids for this order (Auctions) -->
              <div v-if="order.volume_type === 'CONSTRUCTION' && order.status === 'SEARCHING'" class="bids-section mt-3 p-2 bg-light rounded">
                <span class="text-xs font-bold text-secondary d-block mb-2">Received Bids ({{ (bidsMap[order.id] || []).length }}):</span>
                <div v-if="!(bidsMap[order.id] || []).length" class="text-xs text-secondary text-center py-2">
                  No bids placed yet
                </div>
                <div v-else>
                  <div 
                    v-for="bid in bidsMap[order.id]" 
                    :key="bid.id" 
                    class="d-flex justify-content-between align-items-center mb-1 py-1 border-bottom"
                  >
                    <span class="text-xs">Executor {{ bid.executor_phone }} offers <strong>${{ bid.offered_price }}</strong></span>
                    <va-button color="success" size="small" @click="acceptBid(bid.id)">Accept</va-button>
                  </div>
                </div>
              </div>

              <!-- Action Buttons -->
              <div class="d-flex justify-content-end gap-2 mt-2">
                <va-button 
                  v-if="order.status === 'ASSIGNED'" 
                  color="info" 
                  outline 
                  size="small" 
                  @click="openChat(order)"
                  class="mr-2"
                >
                  <va-icon name="chat" class="mr-1" /> Chat
                </va-button>
                <va-button 
                  v-if="order.status === 'SEARCHING' || order.status === 'ASSIGNED'" 
                  color="danger" 
                  outline 
                  size="small" 
                  @click="cancelOrder(order.id)"
                  class="mr-2"
                >
                  Cancel Order
                </va-button>
                <va-button 
                  v-if="order.status === 'ASSIGNED'" 
                  color="success" 
                  size="small" 
                  @click="confirmOrder(order.id)"
                >
                  Confirm Delivery
                </va-button>
              </div>
            </va-card>
          </div>
        </va-card>
      </div>
    </div>

    <!-- Sliding Chat Panel -->
    <div :class="['chat-panel shadow-lg', { open: selectedChatOrder }]">
      <div class="chat-header d-flex justify-content-between align-items-center bg-primary text-white p-3">
        <div>
          <h4 class="m-0 text-white font-bold text-sm">Order #{{ selectedChatOrder?.id.slice(0, 8) }} Chat</h4>
          <span class="text-xs opacity-75">Direct connection with Executor</span>
        </div>
        <va-button color="warning" size="small" flat @click="closeChat">Close</va-button>
      </div>

      <div class="chat-messages p-3 flex-grow-1" ref="messagesContainer">
        <div v-if="chatLocked" class="text-center py-2 mb-3 bg-danger-light text-danger rounded text-xs">
          Chat session locked (Order completed/cancelled)
        </div>

        <div 
          v-for="msg in chatMessages" 
          :key="msg.id" 
          :class="['message-bubble mb-2 p-2 rounded', msg.sender_id === authStore.userID ? 'my-message ml-auto bg-primary text-white' : 'their-message mr-auto bg-light']"
        >
          <div class="text-xs opacity-75 mb-1" v-if="msg.sender_id !== authStore.userID">Executor</div>
          <div class="text-sm message-text">{{ msg.text }}</div>
          <div class="text-xxs text-right mt-1 opacity-75">{{ formatTime(msg.created_at) }}</div>
        </div>
      </div>

      <div class="chat-input-area p-3 bg-white border-top">
        <va-form @submit.prevent="sendChatMessage" class="d-flex">
          <va-input 
            v-model="chatText" 
            placeholder="Type your message..." 
            class="flex-grow-1 mr-2" 
            :disabled="chatLocked"
            required
          />
          <va-button type="submit" color="primary" :disabled="chatLocked">Send</va-button>
        </va-form>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, onMounted, onUnmounted, computed, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../../stores/auth-store'
import api from '../../services/api'

export default defineComponent({
  name: 'CustomerDashboard',
  setup() {
    const router = useRouter()
    const authStore = useAuthStore()

    const phone = ref('')
    const balance = ref(0)
    const status = ref('ACTIVE')

    const topUpAmount = ref(100)
    const submitting = ref(false)
    const successMsg = ref('')
    const errorMsg = ref('')

    // Orders state
    const orders = ref<any[]>([])
    const orderVolume = ref('STANDARD')
    const orderTariff = ref('REGULAR')
    const orderGeo = ref('55.7558, 37.6173')
    const orderPhoto = ref('https://example.com/mock-construction.jpg')
    const creatingOrder = ref(false)

    const volumeOptions = ['STANDARD', 'LARGE', 'CONSTRUCTION']
    const tariffOptions = ['REGULAR', 'URGENT', 'ASAP']

    // Bids cache
    const bidsMap = ref<Record<string, any[]>>({})

    // Chat state
    const selectedChatOrder = ref<any>(null)
    const chatMessages = ref<any[]>([])
    const chatText = ref('')
    const chatLocked = ref(false)
    const ws = ref<WebSocket | null>(null)
    const messagesContainer = ref<any>(null)

    const estimatedPrice = computed(() => {
      let basePrice = 100.0
      if (orderVolume.value === 'LARGE') {
        basePrice = 200.0
      } else if (orderVolume.value === 'CONSTRUCTION') {
        basePrice = 500.0
      }

      let coeff = 1.0
      if (orderTariff.value === 'URGENT') {
        coeff = 3.0
      } else if (orderTariff.value === 'ASAP') {
        coeff = 8.0
      }

      return basePrice * coeff
    })

    const fetchProfile = async () => {
      try {
        const response = await api.get('/customer/profile')
        if (response.data) {
          phone.value = response.data.phone
          balance.value = response.data.balance
          status.value = response.data.status
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
        if (order.volume_type === 'CONSTRUCTION' && order.status === 'SEARCHING') {
          try {
            const response = await api.get(`/customer/orders/${order.id}/bids`)
            bidsMap.value[order.id] = response.data || []
          } catch (err) {
            console.error('Failed to fetch bids:', err)
          }
        }
      }
    }

    const submitTopUp = async () => {
      successMsg.value = ''
      errorMsg.value = ''
      submitting.value = true
      try {
        await api.post('/customer/finances/topup', { amount: topUpAmount.value })
        successMsg.value = `Wallet top-up request for $${topUpAmount.value.toFixed(2)} submitted successfully!`
        topUpAmount.value = 100
        await fetchProfile()
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Failed to submit top-up request.'
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
        if (orderVolume.value === 'CONSTRUCTION') {
          await api.post('/customer/orders/construction', {
            photo_url: orderPhoto.value,
            last_geo: orderGeo.value,
          })
        } else {
          await api.post('/customer/orders', {
            volume_type: orderVolume.value,
            speed_tariff: orderTariff.value,
            last_geo: orderGeo.value,
          })
        }
        successMsg.value = 'Order created successfully!'
        await fetchProfile()
        await fetchOrders()
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Failed to create order.'
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
        successMsg.value = 'Delivery confirmed and payment released!'
        await fetchProfile()
        await fetchOrders()
        if (selectedChatOrder.value && selectedChatOrder.value.id === orderId) {
          chatLocked.value = true
        }
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Failed to confirm order.'
        console.error(err)
      }
    }

    const cancelOrder = async (orderId: string) => {
      successMsg.value = ''
      errorMsg.value = ''
      try {
        await api.post(`/customer/orders/${orderId}/cancel`)
        successMsg.value = 'Order cancelled!'
        await fetchProfile()
        await fetchOrders()
        if (selectedChatOrder.value && selectedChatOrder.value.id === orderId) {
          chatLocked.value = true
        }
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Failed to cancel order.'
        console.error(err)
      }
    }

    const acceptBid = async (bidId: string) => {
      successMsg.value = ''
      errorMsg.value = ''
      try {
        await api.post(`/customer/bids/${bidId}/accept`)
        successMsg.value = 'Bid accepted and executor assigned successfully!'
        await fetchProfile()
        await fetchOrders()
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Failed to accept bid.'
        console.error(err)
      }
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
      const wsUrl = `${protocol}//${wsHost}/chats/${order.id}/ws`

      if (ws.value) {
        ws.value.close()
      }

      ws.value = new WebSocket(wsUrl)
      ws.value.onmessage = (event) => {
        const data = JSON.parse(event.data)
        if (data.type === 'system' && data.action === 'lock') {
          chatLocked.value = true
        } else if (data.type === 'system' && data.action === 'downgrade') {
          // Live SLA tariff downgrade sync
          order.speed_tariff = data.speed_tariff
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

    const formatDate = (dateStr: string) => {
      if (!dateStr) return ''
      const d = new Date(dateStr)
      return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) + ' ' + d.toLocaleDateString()
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

    let intervalId: any = null

    onMounted(() => {
      fetchProfile()
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
      status,
      topUpAmount,
      submitting,
      successMsg,
      errorMsg,
      orders,
      orderVolume,
      orderTariff,
      orderGeo,
      orderPhoto,
      volumeOptions,
      tariffOptions,
      estimatedPrice,
      creatingOrder,
      bidsMap,
      selectedChatOrder,
      chatMessages,
      chatText,
      chatLocked,
      messagesContainer,
      submitTopUp,
      submitOrder,
      confirmOrder,
      cancelOrder,
      acceptBid,
      openChat,
      sendChatMessage,
      closeChat,
      getStatusColor,
      formatDate,
      formatTime,
      handleLogout,
      fetchOrders,
    }
  },
})
</script>

<style scoped>
.customer-dashboard {
  max-width: 1200px;
  margin: 40px auto;
  padding: 0 20px;
  position: relative;
}

.shadow-card {
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.05);
  border-radius: 12px !important;
  padding: 24px !important;
}

.info-list {
  border-top: 1px solid #edf2f7;
  padding-top: 15px;
}

.info-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.info-label {
  color: #718096;
  font-weight: 500;
}

.info-val {
  font-weight: 600;
  color: #2d3748;
}

.balance-box {
  background: #ebf8ff;
  border: 1px solid #bee3f8;
  border-radius: 8px;
}

.balance-amount {
  font-size: 2.2rem;
  font-weight: 800;
  color: #2b6cb0;
}

.price-preview {
  background: #f7fafc;
  border: 1px dashed #e2e8f0;
  border-radius: 8px;
}

.price-amount-preview {
  font-size: 1.5rem;
  font-weight: 700;
  color: #2b6cb0;
}

.order-item-card {
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  background: #ffffff;
  transition: all 0.2s ease;
}

.order-item-card:hover {
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.05);
}

.order-id {
  color: #2d3748;
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

.border-bottom {
  border-bottom: 1px solid #edf2f7;
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

.truncate {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  display: block;
}

.row {
  display: flex;
  flex-wrap: wrap;
  margin-right: -15px;
  margin-left: -15px;
}

.col-md-5 {
  flex: 0 0 41.666667%;
  max-width: 41.666667%;
  padding: 0 15px;
  box-sizing: border-box;
}

.col-md-7 {
  flex: 0 0 58.333333%;
  max-width: 58.333333%;
  padding: 0 15px;
  box-sizing: border-box;
}

.col-6 {
  flex: 0 0 50%;
  max-width: 50%;
  padding: 0 8px;
  box-sizing: border-box;
}

.col-12 {
  flex: 0 0 100%;
  max-width: 100%;
  padding: 0 8px;
  box-sizing: border-box;
}

@media (max-width: 992px) {
  .col-md-5, .col-md-7 {
    flex: 0 0 100%;
    max-width: 100%;
    margin-bottom: 20px;
  }
  .chat-panel {
    width: 100%;
  }
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
.justify-content-end {
  justify-content: flex-end;
}
.align-items-center {
  align-items: center;
}
.align-items-start {
  align-items: flex-start;
}
.h-100 {
  height: 100%;
}
.mr-1 {
  margin-right: 4px;
}
.mr-2 {
  margin-right: 8px;
}
.ml-2 {
  margin-left: 8px;
}
.ml-auto {
  margin-left: auto;
}
.mr-auto {
  margin-right: auto;
}
.m-0 {
  margin: 0;
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
.mb-5 {
  margin-bottom: 24px;
}
.mt-1 {
  margin-top: 4px;
}
.mt-2 {
  margin-top: 8px;
}
.mt-3 {
  margin-top: 12px;
}
.mt-4 {
  margin-top: 16px;
}
.py-1 {
  padding-top: 4px;
  padding-bottom: 4px;
}
.py-2 {
  padding-top: 8px;
  padding-bottom: 8px;
}
.py-5 {
  padding-top: 32px;
  padding-bottom: 32px;
}
.p-2 {
  padding: 8px;
}
.p-3 {
  padding: 12px;
}
.border-top {
  border-top: 1px solid #edf2f7;
}
.font-bold {
  font-weight: 700;
}
.text-uppercase {
  text-transform: uppercase;
}
.text-xs {
  font-size: 0.75rem;
}
.text-xxs {
  font-size: 0.65rem;
}
</style>
