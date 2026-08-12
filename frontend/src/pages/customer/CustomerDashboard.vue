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

    <!-- Order History table -->
    <va-card class="shadow-card mb-4">
      <div class="d-flex justify-content-between align-items-center mb-3 p-3 pb-0">
        <h3 class="va-h6 m-0 text-secondary font-bold d-flex align-items-center">
          <va-icon name="history" class="mr-2" /> {{ $t('customer.orderHistory') }}
        </h3>
      </div>

      <div v-if="historyOrders.length === 0" class="text-center py-4">
        <va-icon name="folder_off" size="medium" color="secondary" class="mb-2" />
        <p class="text-secondary text-sm m-0">{{ $t('customer.noHistoryOrders') }}</p>
      </div>

      <va-data-table
        v-else
        :items="historyOrders"
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

        <template #cell(hold_amount)="{ rowData }">
          <strong>{{ currencySymbol }}{{ Number(rowData.final_amount || rowData.hold_amount).toFixed(2) }}</strong>
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
          </div>
        </template>
      </va-data-table>
    </va-card>

    <!-- Order Details Modal / Card -->
    <va-modal
      v-model="showOrderDetailsModal"
      :title="$t('customer.orderDetails')"
      hide-default-actions
    >
      <div v-if="selectedOrderDetails" class="p-2">
        <div class="d-flex justify-content-between align-items-center mb-3">
          <h4 class="va-h6 font-bold m-0">#{{ selectedOrderDetails.id }}</h4>
          <va-badge :color="getStatusColor(selectedOrderDetails.status)">
            {{ selectedOrderDetails.status }}
          </va-badge>
        </div>

        <div class="info-list bg-light p-3 rounded mb-3">
          <div class="info-item mb-2">
            <span class="text-secondary text-xs d-block">{{ $t('customer.orderType') }}</span>
            <span class="font-bold text-sm">{{ formatOrderType(selectedOrderDetails) }}</span>
          </div>
          <div class="info-item mb-2">
            <span class="text-secondary text-xs d-block">{{ $t('customer.created') }}</span>
            <span class="text-sm">{{ formatDateFull(selectedOrderDetails.created_at) }}</span>
          </div>
          <div v-if="selectedOrderDetails.assigned_at" class="info-item mb-2">
            <span class="text-secondary text-xs d-block">{{ $t('customer.assignedAt') }}</span>
            <span class="text-sm">{{ formatDateFull(selectedOrderDetails.assigned_at) }}</span>
          </div>
          <div v-if="selectedOrderDetails.completed_at" class="info-item mb-2">
            <span class="text-secondary text-xs d-block">{{ $t('customer.completedAt') }}</span>
            <span class="text-sm">{{ formatDateFull(selectedOrderDetails.completed_at) }}</span>
          </div>
          <div v-if="selectedOrderDetails.canceled_at" class="info-item mb-2">
            <span class="text-secondary text-xs d-block">{{ $t('customer.canceledAt') }}</span>
            <span class="text-sm">{{ formatDateFull(selectedOrderDetails.canceled_at) }}</span>
          </div>
          <div class="info-item mb-2">
            <span class="text-secondary text-xs d-block">{{ $t('customer.totalAmount') }}</span>
            <span class="font-bold text-primary text-base">
              {{ currencySymbol }}{{ Number(selectedOrderDetails.final_amount || selectedOrderDetails.hold_amount).toFixed(2) }}
            </span>
          </div>
          <div v-if="selectedOrderDetails.address" class="info-item mb-2">
            <span class="text-secondary text-xs d-block">{{ $t('customer.pickupAddress') }}</span>
            <span class="text-sm">{{ selectedOrderDetails.address }}</span>
          </div>
        </div>

        <!-- Executor details -->
        <div class="executor-card-box border p-3 rounded mb-3">
          <h5 class="va-h6 text-primary mb-2 text-xs uppercase tracking-wide font-bold">
            <va-icon name="person" class="mr-1" /> {{ $t('customer.executorDetails') }}
          </h5>
          <div v-if="selectedOrderDetails.executor_id || selectedOrderDetails.executor_phone">
            <div class="text-sm font-bold" v-if="selectedOrderDetails.executor_phone">
              📱 {{ selectedOrderDetails.executor_phone }}
            </div>
            <div class="text-xs text-secondary mt-1">
              ID: {{ selectedOrderDetails.executor_id }}
            </div>
          </div>
          <div v-else class="text-xs text-secondary italic">
            {{ $t('customer.notAssigned') }}
          </div>
        </div>

        <div class="d-flex justify-content-end">
          <va-button color="secondary" @click="showOrderDetailsModal = false">
            {{ $t('common.close') }}
          </va-button>
        </div>
      </div>
    </va-modal>

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

    <!-- Top Floating Toast Notification for Incoming Messages -->
    <div
      v-if="chatToast"
      class="chat-top-toast shadow-lg p-3 rounded-lg d-flex align-items-center cursor-pointer"
      @click="openChatByToast"
    >
      <div class="toast-chat-icon mr-3">💬</div>
      <div class="flex-grow-1 overflow-hidden">
        <div class="font-bold text-xs text-white">{{ chatToast.title }}</div>
        <div class="text-xs text-white-75 truncate">{{ chatToast.text }}</div>
      </div>
      <button type="button" class="toast-close-btn ml-2 text-white" @click.stop="chatToast = null">✕</button>
    </div>

    <!-- Sliding Chat Panel (Telegram Style) -->
    <div :class="['chat-panel shadow-lg', { open: selectedChatOrder }]">
      <div class="chat-header d-flex align-items-center bg-telegram text-white p-2 px-3">
        <div class="telegram-avatar mr-3">
          {{ (selectedChatOrder?.id?.slice(0, 2) || '').toUpperCase() }}
        </div>
        <div class="flex-grow-1 overflow-hidden">
          <h4 class="m-0 text-white font-bold text-sm truncate">
            {{ $t('customer.orderChatTitle', { id: selectedChatOrder?.id?.slice(0, 8) || '' }) }}
          </h4>
          <span class="text-xxs text-online d-flex align-items-center">
            <span class="online-dot mr-1"></span> {{ $t('customer.chatSubtitle') }}
          </span>
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
          
          <!-- Attachment rendering -->
          <div v-if="msg.file_url" class="telegram-attachment mb-2">
            <div v-if="isImageAttachment(msg)" class="attachment-image-wrapper">
              <img
                :src="resolveFileUrl(msg.file_url)"
                class="attachment-img rounded-lg shadow-sm cursor-pointer"
                alt="photo"
                referrerpolicy="no-referrer"
                crossorigin="anonymous"
                @click="openImagePreview(resolveFileUrl(msg.file_url))"
                @error="(e) => console.error('[DEBUG-IMG-ERROR]', { rawUrl: msg.file_url, resolvedUrl: resolveFileUrl(msg.file_url), isNative: Capacitor.isNativePlatform(), err: e })"
              />
              <div v-if="isDebug" class="text-xxs text-warning bg-dark p-1 rounded mt-1 overflow-auto max-w-xs style-mono">
                [DEBUG] URL: {{ resolveFileUrl(msg.file_url) }}
              </div>
            </div>
            <div v-else class="attachment-doc-wrapper p-2 bg-white-10 rounded d-flex align-items-center">
              <span class="doc-icon mr-2">📄</span>
              <div class="flex-grow-1 overflow-hidden">
                <a :href="resolveFileUrl(msg.file_url)" target="_blank" download class="font-bold text-xs text-white truncate d-block">
                  {{ msg.file_name || 'document' }}
                </a>
                <span class="text-xxs opacity-75" v-if="msg.file_size">{{ formatFileSize(msg.file_size) }}</span>
              </div>
              <a :href="resolveFileUrl(msg.file_url)" target="_blank" download class="btn-download ml-2">⬇</a>
            </div>
          </div>

          <div v-if="msg.text" class="telegram-text">{{ msg.text }}</div>
          <div class="telegram-meta d-flex align-items-center justify-content-between">
            <div class="d-flex align-items-center gap-1">
              <span class="telegram-time">{{ formatTime(msg.created_at) }}</span>
              <span v-if="msg.sender_id === authStore.userID" class="telegram-ticks-status" :title="getMessageStatusTitle(msg.status)">
                <span v-if="msg.status === 'read'" class="ticks-read">✓✓</span>
                <span v-else-if="msg.status === 'delivered'" class="ticks-delivered">✓✓</span>
                <span v-else class="ticks-sent">✓</span>
              </span>
            </div>
            <button
              v-if="msg.sender_id === authStore.userID"
              type="button"
              class="btn-delete-msg border-0 bg-transparent text-danger p-0 ml-2"
              :title="$t('customer.deleteMessage')"
              @click.stop="deleteMessage(msg.id)"
            >
              🗑️
            </button>
          </div>
        </div>
      </div>

      <!-- File Attachment Options Menu / Input area -->
      <div class="chat-input-area p-2 bg-white border-top">
        <div v-if="uploadingFile" class="text-xs text-primary mb-2 d-flex align-items-center">
          <span class="spinner-border spinner-border-sm mr-2"></span> {{ $t('customer.uploadingFile') }}
        </div>

        <!-- Attachment Choice Menu Modal for Mobile -->
        <div v-if="showAttachMenu" class="attach-menu-dropdown p-2 mb-2 bg-light rounded border d-flex gap-2 justify-content-around">
          <button type="button" class="btn btn-sm btn-outline-primary text-xs" @click="triggerCamera">
            📸 {{ $t('customer.takePhoto') }}
          </button>
          <button type="button" class="btn btn-sm btn-outline-success text-xs" @click="triggerGallery">
            🖼️ {{ $t('customer.chooseFromGallery') }}
          </button>
          <button type="button" class="btn btn-sm btn-outline-secondary text-xs" @click="triggerDoc">
            📄 {{ $t('customer.selectDocument') }}
          </button>
        </div>

        <div class="d-flex align-items-center telegram-input-row">
          <!-- Attachment Clip Button -->
          <button
            type="button"
            class="telegram-attach-btn mr-1 text-white"
            :disabled="chatLocked || uploadingFile"
            :title="$t('customer.attachFile')"
            @click="toggleAttachMenu"
          >
            📎
          </button>

          <input
            ref="fileInputRef"
            type="file"
            accept="*/*"
            style="display: none;"
            @change="onFileSelected"
          />
          <input
            ref="galleryInputRef"
            type="file"
            accept="image/*"
            style="display: none;"
            @change="onFileSelected"
          />
          <input
            ref="cameraInputRef"
            type="file"
            accept="image/*"
            capture="environment"
            style="display: none;"
            @change="onFileSelected"
          />

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
            <svg viewBox="0 0 24 24" width="20" height="20" fill="currentColor">
              <path d="M2.01 21L23 12 2.01 3 2 10l15 2-15 2z" />
            </svg>
          </button>
        </div>
        <div v-if="chatError" class="text-danger text-xs mt-2">{{ chatError }}</div>
      </div>
    </div>

    <!-- Image Preview Modal -->
    <va-modal
      v-model="showImagePreviewModal"
      hide-default-actions
      max-width="500px"
      fixed-layout
      class="image-preview-modal-wrapper"
    >
      <div class="text-center p-3">
        <img :src="previewImageUrl" class="img-preview-content rounded shadow-lg" alt="preview" referrerpolicy="no-referrer" crossorigin="anonymous" />
        <div class="mt-3 text-right">
          <va-button color="secondary" @click="showImagePreviewModal = false">
            {{ $t('common.close') }}
          </va-button>
        </div>
      </div>
    </va-modal>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, onMounted, onUnmounted, computed, nextTick, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Capacitor } from '@capacitor/core'
import { Camera, CameraResultType, CameraSource } from '@capacitor/camera'
import { useAuthStore } from '../../stores/auth-store'
import LanguageSwitcher from '../../components/LanguageSwitcher.vue'
import UpdateBanner from '../../components/UpdateBanner.vue'
import api, { buildChatWebSocketUrl, resolveFileUrl, formatApiError, isDebug } from '../../services/api'
import { NativeWebSocket } from '../../plugins/native-websocket'
import { compressImage } from '../../utils/imageCompressor'
import {
  getServiceCategories,
  getServiceCategoryChildren,
  getCategoryVariants,
  type ServiceNode,
} from '../../api/services'

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
    // Native-only fallback state (web uses pure WebSocket).
    const isNative = Capacitor.isNativePlatform()
    const sendingChat = ref(false)
    const chatError = ref('')
    let chatPollIntervalId: any = null
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

    const fileInputRef = ref<HTMLInputElement | null>(null)
    const galleryInputRef = ref<HTMLInputElement | null>(null)
    const cameraInputRef = ref<HTMLInputElement | null>(null)
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
                scrollToBottom()
              }
            }
            chatText.value = ''
          }
        } catch (err: any) {
          console.error('[CustomerDashboard] Camera capture error/cancel:', err)
        } finally {
          uploadingFile.value = false
        }
      } else {
        if (cameraInputRef.value) cameraInputRef.value.click()
      }
    }

    const triggerGallery = () => {
      showAttachMenu.value = false
      if (galleryInputRef.value) galleryInputRef.value.click()
    }

    const triggerDoc = () => {
      showAttachMenu.value = false
      if (fileInputRef.value) fileInputRef.value.click()
    }

    const isImageAttachment = (msg: any) => {
      if (!msg || !msg.file_url) return false
      if (msg.file_type === 'image') return true
      const url = msg.file_url.toLowerCase()
      return url.endsWith('.jpg') || url.endsWith('.jpeg') || url.endsWith('.png') || url.endsWith('.webp') || url.endsWith('.gif')
    }

    const formatFileSize = (bytes?: number) => {
      if (!bytes) return ''
      if (bytes < 1024) return bytes + ' B'
      if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
      return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
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
            scrollToBottom()
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

    const markChatAsRead = async (orderID: string) => {
      unreadOrderIDs.value.delete(orderID)
      try {
        await api.post(`/chats/${orderID}/read`)
      } catch (err) {
        console.warn('[CustomerDashboard] mark read failed:', err)
      }
      if (isNative) {
        try {
          await NativeWebSocket.send({ message: JSON.stringify({ type: 'read_ack' }) })
        } catch (e) {}
      } else if (ws.value && ws.value.readyState === WebSocket.OPEN) {
        try {
          ws.value.send(JSON.stringify({ type: 'read_ack' }))
        } catch (e) {}
      }
    }

    const sendDeliveryAck = () => {
      if (isNative) {
        try {
          NativeWebSocket.send({ message: JSON.stringify({ type: 'delivery_ack' }) })
        } catch (e) {}
      } else if (ws.value && ws.value.readyState === WebSocket.OPEN) {
        try {
          ws.value.send(JSON.stringify({ type: 'delivery_ack' }))
        } catch (e) {}
      }
    }

    const getMessageStatusTitle = (status?: string) => {
      if (status === 'read') return t('customer.statusRead')
      if (status === 'delivered') return t('customer.statusDelivered')
      return t('customer.statusSent')
    }

    // Image preview modal state
    const showImagePreviewModal = ref(false)
    const previewImageUrl = ref('')

    const openImagePreview = (url: string) => {
      if (!url) return
      previewImageUrl.value = url
      showImagePreviewModal.value = true
    }

    const deleteMessage = async (messageID: string) => {
      if (!selectedChatOrder.value || !messageID) return
      if (!confirm(t('customer.confirmDeleteMessage'))) return
      try {
        await api.delete(`/chats/${selectedChatOrder.value.id}/messages/${messageID}`)
        chatMessages.value = chatMessages.value.filter((m: any) => m.id !== messageID)
      } catch (err: any) {
        console.error('[CustomerDashboard] failed to delete message:', err)
        chatError.value = formatApiError(err, 'Failed to delete message')
      }
    }

    const handleIncomingChatMessage = (data: any, order: any) => {
      if (data.type === 'message_deleted') {
        chatMessages.value = chatMessages.value.filter((m: any) => m.id !== data.message_id)
        return
      }

      if (data.type === 'status_update') {
        const updateIds = new Set(data.message_ids || [])
        for (const m of chatMessages.value) {
          if (updateIds.has(m.id)) {
            m.status = data.status
          }
        }
        return
      }

      if (data.type === 'system' && data.action === 'lock') {
        chatLocked.value = true
        return
      }
      if (data.type === 'system' && data.action === 'downgrade') {
        order.is_urgent = data.is_urgent
        order.is_asap = data.is_asap
        order.final_amount = data.final_amount
        order.is_downgraded = true
        return
      }
      if (data.type === 'error') {
        console.warn(data.message)
        return
      }

      // Standard text message
      const exists = chatMessages.value.some((m: any) => m.id === data.id)
      if (!exists) {
        chatMessages.value.push(data)
        scrollToBottom()
      }

      // If message is from recipient (other user)
      if (data.sender_id !== authStore.userID) {
        sendDeliveryAck()
        if (!selectedChatOrder.value || selectedChatOrder.value.id !== order.id) {
          unreadOrderIDs.value.add(order.id)
          chatToast.value = {
            id: order.id,
            title: t('customer.newMessageTitle'),
            text: t('customer.newMessageToast', { id: order.id.slice(0, 8), text: data.text }),
            order,
          }
        } else {
          markChatAsRead(order.id)
        }
      }
    }

    const openChatByToast = () => {
      if (chatToast.value?.order) {
        openChat(chatToast.value.order)
        chatToast.value = null
      }
    }

    // Chat operations
    const openChat = async (order: any) => {
      selectedChatOrder.value = order
      chatMessages.value = []
      chatLocked.value = false
      chatError.value = ''

      // Mark unread dot as read
      markChatAsRead(order.id)

      // Load history (with timeout so the native HTTP bridge can't stall forever).
      try {
        const response = await api.get(`/chats/${order.id}/messages`, isNative ? {
          params: { _t: Date.now() },
          headers: { 'Cache-Control': 'no-cache, no-store, must-revalidate' },
          timeout: 5000,
        } : undefined)
        chatMessages.value = response.data || []
        scrollToBottom()
      } catch (err) {
        console.error(err)
        if (isNative) chatError.value = t('customer.errorChatHistory')
      }

      // Open websocket.
      const wsUrl = buildChatWebSocketUrl(order.id, authStore.token)

      if (isNative) {
        try {
          await NativeWebSocket.disconnect()
          await NativeWebSocket.addListener('onOpen', () => {
            chatError.value = ''
            sendDeliveryAck()
            markChatAsRead(order.id)
          })
          await NativeWebSocket.addListener('onMessage', (res) => {
            if (!res || !res.data) return
            const data = JSON.parse(res.data)
            handleIncomingChatMessage(data, order)
          })
          await NativeWebSocket.connect({ url: wsUrl })
        } catch (nativeErr) {
          console.warn('[CustomerDashboard] NativeWebSocket connection error:', nativeErr)
        }
      } else {
        if (ws.value) {
          ws.value.close()
          ws.value = null
        }

        ws.value = new WebSocket(wsUrl)
        ws.value.onopen = () => {
          sendDeliveryAck()
          markChatAsRead(order.id)
        }
        ws.value.onmessage = (event) => {
          const data = JSON.parse(event.data)
          handleIncomingChatMessage(data, order)
        }
      }

      // Start 2-second background polling loop so incoming messages arrive immediately.
      scheduleChatPoll(order.id)
    }

    const sendChatMessage = async (event?: Event) => {
      if (event) {
        event.preventDefault()
        event.stopPropagation()
      }
      if (!chatText.value.trim() || chatLocked.value) return
      const text = chatText.value.trim()

      if (isNative) {
        try {
          await NativeWebSocket.send({ message: JSON.stringify({ text }) })
          chatText.value = ''
          chatError.value = ''
          return
        } catch (err) {
          console.warn('[CustomerDashboard] NativeWebSocket send failed, falling back to REST:', err)
        }
      } else if (ws.value && ws.value.readyState === WebSocket.OPEN) {
        try {
          ws.value.send(JSON.stringify({ text }))
          chatText.value = ''
          chatError.value = ''
          return
        } catch (err) {
          console.warn('[CustomerDashboard] ws.send failed, falling back to HTTP:', err)
        }
      }

      // Native-only REST fallback when WS is not connected or send threw.
      if (!isNative) return
      const orderID = selectedChatOrder.value?.id
      if (!orderID) return
      sendingChat.value = true
      chatError.value = ''
      try {
        const response = await api.post(`/chats/${orderID}/messages`, { text })
        const savedMsg = response.data
        if (savedMsg) {
          const exists = chatMessages.value.some((m: any) => m.id === savedMsg.id)
          if (!exists) {
            chatMessages.value.push(savedMsg)
            scrollToBottom()
          }
        }
        chatText.value = ''
      } catch (err: any) {
        if (err.response?.status === 409) {
          chatLocked.value = true
        }
        chatError.value = formatApiError(err, t('customer.errorChatConnection'))
      } finally {
        sendingChat.value = false
      }
    }

    const pollChatMessages = async (orderID: string) => {
      if (!selectedChatOrder.value) return
      try {
        const response = await api.get(`/chats/${orderID}/messages`, {
          params: { _t: Date.now() },
          headers: { 'Cache-Control': 'no-cache, no-store, must-revalidate' },
          timeout: 4000,
        })
        const incoming = response.data || []
        const existingIds = new Set(chatMessages.value.map((m: any) => m.id))
        let added = false
        for (const m of incoming) {
          if (!existingIds.has(m.id)) {
            chatMessages.value.push(m)
            added = true
          }
        }
        if (added) scrollToBottom()
      } catch (err) {
        console.warn('[CustomerDashboard] poll chat messages failed:', err)
      }
    }

    const scheduleChatPoll = (orderID: string) => {
      if (chatPollIntervalId) clearTimeout(chatPollIntervalId)
      const tick = async () => {
        await pollChatMessages(orderID)
        if (selectedChatOrder.value && selectedChatOrder.value.id === orderID) {
          chatPollIntervalId = setTimeout(tick, 2000)
        }
      }
      chatPollIntervalId = setTimeout(tick, 2000)
    }

    const closeChat = () => {
      selectedChatOrder.value = null
      if (ws.value) {
        ws.value.close()
        ws.value = null
      }
      if (chatPollIntervalId) {
        clearTimeout(chatPollIntervalId)
        chatPollIntervalId = null
      }
      chatError.value = ''
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
      const children = await getServiceCategoryChildren(id)
      const variants = children.filter((c) => c.node_type === 'VARIANT')
      serviceVariants.value = variants
    })

    watch(selectedVariantId, () => {
      isUrgent.value = false
      isAsap.value = false
    })

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
      bidsMap,
      selectedChatOrder,
      chatMessages,
      chatText,
      chatLocked,
      isNative,
      isDebug,
      sendingChat,
      chatError,
      messagesContainer,
      showCreateOrderModal,
      showTopUpModal,
      showImagePreviewModal,
      previewImageUrl,
      openImagePreview,
      deleteMessage,
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
      unreadOrderIDs,
      chatToast,
      openChatByToast,
      getMessageStatusTitle,
      fileInputRef,
      galleryInputRef,
      cameraInputRef,
      showAttachMenu,
      uploadingFile,
      toggleAttachMenu,
      triggerCamera,
      triggerGallery,
      triggerDoc,
      formatFileSize,
      onFileSelected,
      resolveFileUrl,
      isImageAttachment,
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

/* Telegram Chat Design */
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
  box-shadow: -4px 0 20px rgba(0, 0, 0, 0.25);
}

.chat-panel.open {
  transform: translateX(0);
}

.bg-telegram {
  background: #517da2 !important; /* Telegram signature header blue */
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.15);
}

.telegram-avatar {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  background: #3e6587;
  color: white;
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 700;
  font-size: 13px;
}

.btn-close-chat {
  background: transparent;
  border: none;
  color: rgba(255, 255, 255, 0.85);
  font-size: 18px;
  padding: 4px 8px;
  cursor: pointer;
}

.btn-close-chat:hover {
  color: #ffffff;
}

.text-online {
  color: #7ce7ff;
}

.online-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background-color: #44e883;
  display: inline-block;
}

/* Telegram Wallpaper Background pattern */
.telegram-bg {
  background-color: #0e1621 !important;
  background-image: radial-gradient(#1c2938 1px, transparent 1px);
  background-size: 16px 16px;
  overflow-y: auto;
}

.telegram-bubble {
  max-width: 78%;
  padding: 8px 12px 6px 12px;
  position: relative;
  margin-bottom: 6px;
  font-size: 14.5px;
  line-height: 1.4;
  word-break: break-word;
}

.my-telegram-msg {
  background-color: #2b5278 !important; /* Telegram sender bubble blue */
  color: #f5f5f5 !important;
  border-radius: 14px 14px 2px 14px;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.2);
}

.their-telegram-msg {
  background-color: #182533 !important; /* Telegram receiver dark bubble */
  color: #e4ecf2 !important;
  border-radius: 14px 14px 14px 2px;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.2);
}

.telegram-sender {
  font-size: 12px;
  font-weight: 700;
  color: #6bb4e8;
  margin-bottom: 2px;
}

.telegram-attach-btn {
  background: transparent;
  border: none;
  font-size: 18px;
  cursor: pointer;
  padding: 2px 6px;
  opacity: 0.85;
  transition: opacity 0.15s ease;
}
.telegram-attach-btn:hover {
  opacity: 1;
}

.attachment-img {
  width: 100%;
  max-width: 260px;
  min-width: 120px;
  min-height: 100px;
  max-height: 240px;
  object-fit: cover;
  display: block;
  cursor: pointer;
  pointer-events: auto;
}

.attachment-doc-wrapper {
  background: rgba(255, 255, 255, 0.08);
  border: 1px solid rgba(255, 255, 255, 0.15);
}

.btn-download {
  color: #7ce7ff;
  text-decoration: none;
  font-weight: bold;
  font-size: 14px;
}

.telegram-text {
  font-size: 14px;
}

.telegram-meta {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 4px;
  margin-top: 2px;
}

.telegram-time {
  font-size: 10.5px;
  color: rgba(255, 255, 255, 0.55);
}

.telegram-ticks-status {
  font-size: 11px;
  font-weight: bold;
}
.ticks-sent {
  color: rgba(255, 255, 255, 0.45);
}
.ticks-delivered {
  color: rgba(255, 255, 255, 0.65);
}
.ticks-read {
  color: #5bb3f0;
}

/* Yellow unread badge dot */
.position-relative {
  position: relative;
}
.yellow-unread-dot {
  position: absolute;
  top: -3px;
  right: -3px;
  width: 10px;
  height: 10px;
  background-color: #f59e0b;
  border: 2px solid #ffffff;
  border-radius: 50%;
  box-shadow: 0 0 6px rgba(245, 158, 11, 0.8);
  animation: pulse-dot 1.5s infinite;
}

@keyframes pulse-dot {
  0% { transform: scale(0.95); box-shadow: 0 0 0 0 rgba(245, 158, 11, 0.7); }
  70% { transform: scale(1.1); box-shadow: 0 0 0 6px rgba(245, 158, 11, 0); }
  100% { transform: scale(0.95); box-shadow: 0 0 0 0 rgba(245, 158, 11, 0); }
}

/* Floating Top Toast Notification */
.chat-top-toast {
  position: fixed;
  top: 16px;
  left: 50%;
  transform: translateX(-50%);
  width: 90%;
  max-width: 420px;
  background: #2b5278;
  color: white;
  z-index: 1050;
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
.toast-close-btn:hover {
  opacity: 1;
}

@keyframes slide-down {
  from { transform: translate(-50%, -100%); opacity: 0; }
  to { transform: translate(-50%, 0); opacity: 1; }
}

.telegram-input-row {
  background: #17212b;
  border-radius: 20px;
  padding: 4px 6px;
  border: 1px solid #242f3d;
}

.telegram-input {
  background: transparent;
  border: none;
  color: #f5f5f5;
  outline: none;
  font-size: 14px;
}

.telegram-input::placeholder {
  color: #708499;
}

.telegram-send-btn {
  background: #5288c1;
  color: white;
  border: none;
  width: 34px;
  height: 34px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: background 0.15s ease;
}

.telegram-send-btn:hover {
  background: #4779ad;
}

.telegram-send-btn:disabled {
  background: #242f3d;
  color: #4e5d6d;
}

.bg-light {
  background-color: #f8fafc;
}

.bg-danger-light {
  background-color: #fff5f5;
}

.text-danger {
  color: #c53030;
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

.img-preview-content {
  max-width: 100%;
  max-height: 70vh;
  object-fit: contain;
  margin: 0 auto;
}
</style>

<style>
/* Global override to ensure image preview modal always sits on top of high z-index side drawers (chat-panel z-index: 1000) */
.image-preview-modal-wrapper .va-modal__overlay,
.image-preview-modal-wrapper .va-modal__container {
  z-index: 10000 !important;
}
</style>
