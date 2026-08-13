<template>
  <div class="customer-dashboard">
    <!-- Header: phone + balance -->
    <CustomerHeader
      :phone="phone"
      :balance="balance"
      :currency-symbol="currencySymbol"
      @logout="handleLogout"
    />

    <update-banner />

    <!-- Alert messages -->
    <va-alert v-if="successMsg" color="success" class="mb-3" closeable @dismissed="successMsg = ''">
      {{ successMsg }}
    </va-alert>
    <va-alert v-if="errorMsg" color="danger" class="mb-3" closeable @dismissed="errorMsg = ''">
      {{ errorMsg }}
    </va-alert>

    <!-- Top-up button (small, left aligned) & Image Debug Button -->
    <div class="mb-3 d-flex gap-2">
      <va-button color="primary" outline size="small" @click="showTopUpModal = true">
        <va-icon name="payment" class="mr-1" /> {{ $t('customer.requestWalletTopUp') }}
      </va-button>
      <va-button v-if="isDebug" color="warning" outline size="small" @click="showDebugImgModal = true">
        🐞 Тест Картинок (HTTP & HTTPS)
      </va-button>
    </div>

    <!-- Create order button (large) -->
    <div class="mb-4">
      <va-button color="success" block size="large" @click="openCreateOrderModal">
        <va-icon name="shopping_cart" class="mr-2" /> {{ $t('customer.createOrder') }}
      </va-button>
    </div>

    <!-- Active Orders table -->
    <va-card class="shadow-card mb-4">
      <div class="d-flex justify-content-between align-items-center mb-3 p-3 pb-0">
        <h3 class="va-h6 m-0 text-primary font-bold d-flex align-items-center">
          <va-icon name="pending_actions" class="mr-2" /> {{ $t('customer.activeOrders') }}
        </h3>
        <va-button icon="refresh" color="secondary" size="small" flat @click="fetchOrders" />
      </div>

      <div v-if="activeOrders.length === 0" class="text-center py-4">
        <va-icon name="inbox" size="medium" color="secondary" class="mb-2" />
        <p class="text-secondary text-sm m-0">{{ $t('customer.noActiveOrders') }}</p>
      </div>

      <va-data-table
        v-else
        :items="activeOrders"
        :columns="orderColumns"
        striped
        hoverable
      >
        <template #cell(id)="{ rowData }">
          <span class="font-bold text-sm cursor-pointer text-primary" @click="openOrderDetails(rowData)">#{{ rowData.id.slice(0, 8) }}</span>
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
              color="primary"
              flat
              size="small"
              @click="openOrderDetails(rowData)"
            >
              <va-icon name="info" />
            </va-button>
            <va-button
              v-if="rowData.status === 'ASSIGNED'"
              color="info"
              outline
              size="small"
              class="position-relative"
              @click="openChat(rowData)"
            >
              <va-icon name="chat" />
              <span v-if="unreadOrderIDs.has(rowData.id)" class="yellow-unread-dot"></span>
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

    <!-- Order History table (Collapsible Component) -->
    <OrderHistoryCard
      :history-orders="historyOrders"
      :columns="orderColumns"
      :currency-symbol="currencySymbol"
      :format-order-type="formatOrderType"
      :get-status-color="getStatusColor"
      @open-details="openOrderDetails"
    />

    <!-- Order Details Modal -->
    <OrderDetailsModal
      v-model="showOrderDetailsModal"
      :order="selectedOrderDetails"
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
      @submit="submitOrder"
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

    <!-- Top Floating Toast Notification for Incoming Messages -->
    <div
      v-if="chatToast"
      class="chat-toast-floating p-3 rounded-lg shadow-lg d-flex align-items-center justify-content-between cursor-pointer"
      @click="openChatByToast"
    >
      <div class="d-flex align-items-center gap-3">
        <div class="toast-chat-icon">💬</div>
        <div>
          <div class="font-bold text-sm">{{ chatToast.title }}</div>
          <div class="text-xs opacity-90">{{ chatToast.text }}</div>
        </div>
      </div>
      <button class="toast-close-btn text-white ml-3" @click.stop="chatToast = null">✕</button>
    </div>

    <!-- Telegram-Style Sliding Chat Panel Component -->
    <ChatDrawer
      v-model:chat-text="chatText"
      :selected-chat-order="selectedChatOrder"
      :chat-messages="chatMessages"
      :current-user-id="authStore.userID"
      :recipient-title="$t('common.chatWithExecutor')"
      recipient-initials="🚛"
      :recipient-role-label="$t('common.executor')"
      :chat-locked="chatLocked"
      :sending-chat="sendingChat"
      :uploading-file="uploadingFile"
      :show-attach-menu="showAttachMenu"
      :chat-error="chatError"
      :get-image-src="getImageSrc"
      :is-image-attachment="isImageAttachment"
      @close="closeChat"
      @send-message="sendChatMessage"
      @toggle-attach-menu="toggleAttachMenu"
      @trigger-camera="triggerCamera"
      @trigger-gallery="triggerGallery"
      @trigger-doc="triggerDoc"
      @file-selected="onFileSelected"
      @delete-message="(id) => deleteMessage(id, $t('customer.confirmDeleteMessage'))"
      @preview-image="openImagePreview"
      @img-error="onChatImgError"
    />

    <!-- Common Image Preview Modal -->
    <ImagePreviewModal
      v-model="showImagePreviewModal"
      :image-url="previewImageUrl"
      @error="onPreviewModalImgError"
    />

    <!-- Common Debug Images Modal -->
    <ImageDebugModal
      v-model="showDebugImgModal"
      :testing-fetch="testingFetch"
      :http-fetch-result="httpFetchResult"
      :https-fetch-result="httpsFetchResult"
      :http-blob-url="httpBlobUrl"
      :https-blob-url="httpsBlobUrl"
      @run-diagnostics="runFetchDiagnostics"
    />
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../../stores/auth-store'
import { Capacitor } from '@capacitor/core'
import { Camera, CameraResultType, CameraSource } from '@capacitor/camera'
import UpdateBanner from '../../components/UpdateBanner.vue'
import CustomerHeader from './components/CustomerHeader.vue'
import OrderHistoryCard from './components/OrderHistoryCard.vue'
import CreateOrderModal from './components/CreateOrderModal.vue'
import OrderDetailsModal from './components/OrderDetailsModal.vue'
import ChatDrawer from '../../components/common/ChatDrawer.vue'
import ImagePreviewModal from '../../components/common/ImagePreviewModal.vue'
import ImageDebugModal from '../../components/common/ImageDebugModal.vue'
import { useChat } from '../../composables/useChat'
import { useImageBlobFallback } from '../../composables/useImageBlobFallback'
import { useImagePreviewModal } from '../../composables/useImagePreviewModal'
import api, { formatApiError, isDebug } from '../../services/api'
import { compressImage } from '../../utils/imageCompressor'
import {
  getServiceCategories,
  getServiceCategoryChildren,
  getCategoryVariants,
  type ServiceNode,
} from '../../api/services'

export default defineComponent({
  name: 'CustomerDashboard',
  components: {
    UpdateBanner,
    CustomerHeader,
    OrderHistoryCard,
    CreateOrderModal,
    OrderDetailsModal,
    ChatDrawer,
    ImagePreviewModal,
    ImageDebugModal,
  },
  setup() {
    const router = useRouter()
    const { t, locale } = useI18n()
    const authStore = useAuthStore()
    const isNative = Capacitor.isNativePlatform()

    const phone = ref('')
    const balance = ref(0)

    // Composables
    const {
      selectedChatOrder,
      chatMessages,
      chatText,
      chatLocked,
      sendingChat,
      chatError,
      openChat: openChatComposable,
      sendChatMessage,
      closeChat,
      deleteMessage,
      markChatAsRead,
    } = useChat(isNative)

    const { isImageAttachment, getImageSrc, onChatImgError } = useImageBlobFallback()
    const {
      showImagePreviewModal,
      previewImageUrl,
      openImagePreview,
      onPreviewModalImgError,
    } = useImagePreviewModal()

    // Debug Modal State
    const showDebugImgModal = ref(false)
    const testingFetch = ref(false)
    const httpBlobUrl = ref('')
    const httpsBlobUrl = ref('')
    const httpFetchResult = ref<any>(null)
    const httpsFetchResult = ref<any>(null)

    const runFetchDiagnostics = async () => {
      testingFetch.value = true
      httpFetchResult.value = null
      httpsFetchResult.value = null
      httpBlobUrl.value = ''
      httpsBlobUrl.value = ''

      try {
        const res = await fetch('http://94.103.9.172:8089/uploads/chat/029c51c0-3bc9-4569-b49c-6247839105d0_1786616908.jpg')
        const blob = await res.blob()
        httpFetchResult.value = { status: res.status, statusText: res.statusText, ok: res.ok, size: blob.size }
        if (res.ok && blob.size > 0) {
          httpBlobUrl.value = URL.createObjectURL(blob)
        }
      } catch (err: any) {
        httpFetchResult.value = { status: 0, statusText: 'Failed', ok: false, size: 0, error: String(err) }
      }

      try {
        const res = await fetch('https://94.103.9.172:8443/uploads/chat/029c51c0-3bc9-4569-b49c-6247839105d0_1786616908.jpg')
        const blob = await res.blob()
        httpsFetchResult.value = { status: res.status, statusText: res.statusText, ok: res.ok, size: blob.size }
        if (res.ok && blob.size > 0) {
          httpsBlobUrl.value = URL.createObjectURL(blob)
        }
      } catch (err: any) {
        httpsFetchResult.value = { status: 0, statusText: 'Failed', ok: false, size: 0, error: String(err) }
      } finally {
        testingFetch.value = false
      }
    }

    const topUpAmount = ref(100)
    const submitting = ref(false)
    const successMsg = ref('')
    const errorMsg = ref('')

    // Orders state
    const orders = ref<any[]>([])
    const activeOrders = computed(() =>
      orders.value.filter((o) => o.status === 'SEARCHING' || o.status === 'ASSIGNED')
    )
    const historyOrders = computed(() =>
      orders.value.filter((o) => o.status === 'COMPLETED' || o.status === 'CANCELED')
    )
    const showOrderDetailsModal = ref(false)
    const selectedOrderDetails = ref<any>(null)

    const openOrderDetails = (order: any) => {
      selectedOrderDetails.value = order
      showOrderDetailsModal.value = true
    }

    const formatDateFull = (dateStr: string) => {
      if (!dateStr) return ''
      const d = new Date(dateStr)
      return d.toLocaleString([], { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
    }

    // New Order Modal State
    const showCreateOrderModal = ref(false)
    const creatingOrder = ref(false)
    const defaultAddress = ref('')
    const orderAddress = ref('')
    const orderLat = ref<number | null>(null)
    const orderLon = ref<number | null>(null)
    const geocoding = ref(false)
    const geocodeError = ref('')

    const serviceCategories = ref<ServiceNode[]>([])
    const selectedCategoryId = ref<string>('')
    const selectedSubCategoryId = ref<string>('')
    const selectedVariantId = ref<string>('')

    const isUrgent = ref(false)
    const isAsap = ref(false)

    const subCategories = computed<ServiceNode[]>(() => {
      if (!selectedCategoryId.value) return []
      return getServiceCategoryChildren(serviceCategories.value, selectedCategoryId.value)
    })

    const serviceVariants = computed<any[]>(() => {
      const parentId = selectedSubCategoryId.value || selectedCategoryId.value
      if (!parentId) return []
      return getCategoryVariants(serviceCategories.value, parentId)
    })

    const localizedName = (item: { name_ru?: string; name_en?: string; name_ka?: string }) => {
      if (!item) return ''
      const loc = locale.value
      if (loc === 'en' && item.name_en) return item.name_en
      if (loc === 'ka' && item.name_ka) return item.name_ka
      return item.name_ru || item.name_en || item.name_ka || ''
    }

    const categoryOptions = computed(() =>
      serviceCategories.value.map((c) => ({ label: localizedName(c), value: c.id }))
    )

    const subCategoryOptions = computed(() =>
      subCategories.value.map((c) => ({ label: localizedName(c), value: c.id }))
    )

    const variantOptions = computed(() =>
      serviceVariants.value.map((v) => {
        const title = localizedName(v)
        const price = v.is_auction
          ? t('customer.auction')
          : `${currencySymbol.value}${Number(v.base_price).toFixed(2)}`
        return { label: `${title} (${price})`, value: v.id }
      })
    )

    const selectedVariant = computed(() =>
      serviceVariants.value.find((v) => v.id === selectedVariantId.value)
    )

    const isAuctionSelected = computed(() => !!selectedVariant.value?.is_auction)

    const selectedPrice = computed(() => {
      const variant = selectedVariant.value
      if (!variant || variant.base_price === undefined) return 0
      let price = Number(variant.base_price)
      if (isAuctionSelected.value) return 0
      if (isUrgent.value) price += Number(variant.urgent_fee || 0)
      if (isAsap.value) price += Number(variant.asap_fee || 0)
      return price
    })

    const currencySymbol = computed(() => (authStore.currency === 'RUB' ? '₽' : '$'))

    watch(selectedCategoryId, () => {
      selectedSubCategoryId.value = ''
      selectedVariantId.value = ''
    })

    watch(selectedSubCategoryId, () => {
      selectedVariantId.value = ''
    })

    watch(selectedVariantId, () => {
      isUrgent.value = false
      isAsap.value = false
    })

    const orderColumns = [
      { key: 'id', label: t('customer.orderId') },
      { key: 'type', label: t('customer.orderType') },
      { key: 'hold_amount', label: t('customer.holdAmount') },
      { key: 'status', label: t('customer.status') },
      { key: 'actions', label: t('customer.actions') },
    ]

    const getStatusColor = (status: string) => {
      switch (status) {
        case 'SEARCHING': return 'warning'
        case 'ASSIGNED': return 'info'
        case 'COMPLETED': return 'success'
        case 'CANCELED': return 'danger'
        default: return 'secondary'
      }
    }

    const fetchProfile = async () => {
      try {
        const response = await api.get('/customer/profile')
        phone.value = response.data.phone
        balance.value = response.data.balance
        authStore.setCurrency(response.data.currency || 'USD')

        if (!defaultAddress.value) {
          const addrsRes = await api.get('/customer/addresses')
          const defaultAddr = (addrsRes.data || []).find((a: any) => a.is_default)
          if (defaultAddr) {
            defaultAddress.value = defaultAddr.address
          } else {
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

    const openCreateOrderModal = async () => {
      orderAddress.value = defaultAddress.value
      orderLat.value = null
      orderLon.value = null
      geocodeError.value = ''
      showCreateOrderModal.value = true
      try {
        serviceCategories.value = await getServiceCategories()
      } catch (err) {
        console.error('[CustomerDashboard] failed to load categories:', err)
      }
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

    const formatOrderType = (order: any) => {
      const variant = order.service_variant
      if (!variant) return order.service_variant_id
      const name = localizedName(variant)
      if (order.is_asap) return `${name} (${t('customer.asap')})`
      if (order.is_urgent) return `${name} (${t('customer.urgent')})`
      return name
    }

    const showAttachMenu = ref(false)
    const uploadingFile = ref(false)

    const toggleAttachMenu = () => {
      showAttachMenu.value = !showAttachMenu.value
    }

    const triggerCamera = async () => {
      showAttachMenu.value = false
      if (Capacitor.isNativePlatform()) {
        try {
          const photo = await Camera.getPhoto({
            quality: 85,
            allowEditing: false,
            resultType: CameraResultType.Uri,
            source: CameraSource.Camera,
          })

          if (photo.webPath && selectedChatOrder.value) {
            uploadingFile.value = true
            chatError.value = ''
            const response = await fetch(photo.webPath)
            const blob = await response.blob()
            let file = new File([blob], `photo_${Date.now()}.${photo.format || 'jpg'}`, { type: `image/${photo.format || 'jpeg'}` })
            file = await compressImage(file, 150, 300)

            const formData = new FormData()
            formData.append('file', file)
            if (chatText.value.trim()) {
              formData.append('text', chatText.value.trim())
            }

            const uploadRes = await api.post(`/chats/${selectedChatOrder.value.id}/upload`, formData, {
              headers: { 'Content-Type': 'multipart/form-data' },
            })
            if (uploadRes.data) {
              const exists = chatMessages.value.some((m: any) => m.id === uploadRes.data.id)
              if (!exists) {
                chatMessages.value.push(uploadRes.data)
              }
            }
            chatText.value = ''
          }
        } catch (err: any) {
          console.error('[CustomerDashboard] Camera capture error/cancel:', err)
        } finally {
          uploadingFile.value = false
        }
      }
    }

    const triggerGallery = () => {
      showAttachMenu.value = false
    }

    const triggerDoc = () => {
      showAttachMenu.value = false
    }

    const onFileSelected = async (event: Event) => {
      const target = event.target as HTMLInputElement
      if (!target.files || target.files.length === 0 || !selectedChatOrder.value) return
      let file = target.files[0]
      uploadingFile.value = true
      chatError.value = ''

      try {
        if (file.type.startsWith('image/')) {
          file = await compressImage(file, 150, 300)
        }

        const formData = new FormData()
        formData.append('file', file)
        if (chatText.value.trim()) {
          formData.append('text', chatText.value.trim())
        }

        const response = await api.post(`/chats/${selectedChatOrder.value.id}/upload`, formData, {
          headers: { 'Content-Type': 'multipart/form-data' },
        })

        const savedMsg = response.data
        if (savedMsg) {
          const exists = chatMessages.value.some((m: any) => m.id === savedMsg.id)
          if (!exists) {
            chatMessages.value.push(savedMsg)
          }
        }
        chatText.value = ''
      } catch (err: any) {
        console.error('[CustomerDashboard] file upload failed:', err)
        chatError.value = formatApiError(err, t('customer.errorUploadFile'))
      } finally {
        uploadingFile.value = false
        target.value = ''
      }
    }

    const unreadOrderIDs = ref(new Set<string>())
    const chatToast = ref<{ id: string; title: string; text: string; order: any } | null>(null)

    const fetchUnreadSummary = async () => {
      try {
        const response = await api.get('/chats/unread-summary')
        const ids = response.data?.unread_order_ids || []
        unreadOrderIDs.value = new Set(ids)
      } catch (err) {
        console.warn('[CustomerDashboard] failed to fetch unread summary:', err)
      }
    }

    const openChatByToast = () => {
      if (chatToast.value?.order) {
        openChat(chatToast.value.order)
        chatToast.value = null
      }
    }

    const openChat = (order: any) => {
      openChatComposable(order, unreadOrderIDs, t('common.executor'))
    }

    const handleLogout = () => {
      authStore.logout()
      router.push('/login')
    }

    let intervalId: any = null

    onMounted(async () => {
      await fetchProfile()
      await fetchOrders()
      await fetchUnreadSummary()
      try {
        serviceCategories.value = await getServiceCategories()
      } catch (err) {
        console.error('[CustomerDashboard] failed to load categories:', err)
      }
      intervalId = setInterval(() => {
        fetchProfile()
        fetchOrders()
      }, 5000)
    })

    onUnmounted(() => {
      if (intervalId) clearInterval(intervalId)
    })

    return {
      authStore,
      phone,
      balance,
      currencySymbol,
      topUpAmount,
      submitting,
      successMsg,
      errorMsg,
      orders,
      activeOrders,
      historyOrders,
      showOrderDetailsModal,
      selectedOrderDetails,
      openOrderDetails,
      formatDateFull,
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
      selectedChatOrder,
      chatMessages,
      chatText,
      chatLocked,
      isNative,
      isDebug,
      sendingChat,
      chatError,
      showCreateOrderModal,
      showTopUpModal,
      showDebugImgModal,
      testingFetch,
      httpBlobUrl,
      httpsBlobUrl,
      httpFetchResult,
      httpsFetchResult,
      runFetchDiagnostics,
      showImagePreviewModal,
      previewImageUrl,
      openImagePreview,
      onPreviewModalImgError,
      getImageSrc,
      onChatImgError,
      isImageAttachment,
      deleteMessage,
      orderColumns,
      orderAddress,
      orderLat,
      orderLon,
      geocoding,
      geocodeError,
      geocodeAddress,
      submitTopUp,
      submitOrder,
      confirmOrder,
      cancelOrder,
      openCreateOrderModal,
      openChat,
      sendChatMessage,
      closeChat,
      formatOrderType,
      getStatusColor,
      handleLogout,
      unreadOrderIDs,
      chatToast,
      openChatByToast,
      showAttachMenu,
      uploadingFile,
      toggleAttachMenu,
      triggerCamera,
      triggerGallery,
      triggerDoc,
      onFileSelected,
      fetchOrders,
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

.shadow-card {
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.05);
  border-radius: 12px !important;
}

.yellow-unread-dot {
  position: absolute;
  top: -2px;
  right: -2px;
  width: 9px;
  height: 9px;
  background-color: #f59e0b;
  border: 2px solid white;
  border-radius: 50%;
  display: inline-block;
}

.chat-toast-floating {
  position: fixed;
  top: 16px;
  right: 16px;
  z-index: 1050;
  max-width: 420px;
  background: #2b5278;
  color: white;
  border: 1px solid #3e6587;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
  animation: slide-down 0.3s cubic-bezier(0.16, 1, 0.3, 1);
}

.toast-chat-icon {
  font-size: 20px;
}

.toast-close-btn {
  background: transparent;
  border: none;
  font-size: 16px;
  cursor: pointer;
  opacity: 0.8;
}

@keyframes slide-down {
  from {
    transform: translateY(-100%);
    opacity: 0;
  }
  to {
    transform: translateY(0);
    opacity: 1;
  }
}
</style>
