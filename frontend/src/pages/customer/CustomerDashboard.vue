<template>
  <div class="customer-dashboard">
    <!-- Header: phone + balance -->
    <CustomerHeader
      :phone="userPhone"
      :balance="customerBalance"
      :currency-symbol="currencySymbol"
      :is-verified="isVerified"
      @logout="logout"
      @open-profile-modal="$router.push('/customer/profile')"
      @open-top-up-modal="showTopUpModal = true"
    />

    <!-- Alert messages -->
    <va-alert v-if="successMsg" color="success" class="mb-3" closeable @dismissed="successMsg = ''">
      {{ successMsg }}
    </va-alert>
    <va-alert v-if="errorMsg" color="danger" class="mb-3" closeable @dismissed="errorMsg = ''">
      {{ errorMsg }}
    </va-alert>

    <!-- Main Action Buttons Row -->
    <div class="row g-3 mb-4">
      <div class="col-md-6">
        <button type="button" class="btn-action-outline w-100" @click="showTopUpModal = true">
          💳 {{ $t('customer.requestWalletTopUp') }}
        </button>
      </div>
      <div class="col-md-6">
        <button type="button" class="btn-action-solid-green w-100" @click="openCreateOrderModal">
          ➕ {{ $t('customer.createOrder') }}
        </button>
      </div>
    </div>

    <!-- Active Orders Table Card -->
    <div class="card shadow-sm border-0 rounded-2xl mb-4 bg-white overflow-hidden">
      <div class="d-flex justify-content-between align-items-center p-4 border-bottom">
        <div class="d-flex align-items-center gap-2">
          <h3 class="va-h6 m-0 font-bold text-dark">
            {{ $t('customer.activeOrders') }}
          </h3>
          <span class="badge rounded-pill bg-primary-subtle text-primary font-bold px-3 py-1 text-xs">
            {{ activeOrders.length }}
          </span>
        </div>
        <button type="button" class="btn-refresh" title="Обновить" @click="fetchOrders">
          ↻
        </button>
      </div>

      <div v-if="activeOrders.length === 0" class="text-center py-5 text-secondary">
        <va-icon name="inbox" size="large" color="secondary" class="mb-2" />
        <p class="text-secondary text-sm m-0">{{ $t('customer.noActiveOrders') }}</p>
      </div>

      <div v-else class="table-responsive p-4 pt-2">
        <table class="table align-middle table-hover text-sm m-0">
          <thead>
            <tr class="text-secondary text-xs uppercase tracking-wider">
              <th>№ Заказа</th>
              <th>Тип заказа</th>
              <th>Цена</th>
              <th>Статус</th>
              <th>Исполнитель</th>
              <th class="text-end">Действия</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="order in activeOrders" :key="order.id">
              <td class="font-bold text-primary cursor-pointer" @click="openOrderDetails(order)">
                #{{ order.id.slice(0, 8) }}
              </td>
              <td>{{ formatOrderType(order) }}</td>
              <td class="font-bold text-dark">{{ currencySymbol }}{{ Number(order.hold_amount).toFixed(2) }}</td>
              <td>
                <span :class="['status-pill-badge', order.status === 'ASSIGNED' ? 'pill-assigned' : 'pill-searching']">
                  ● {{ order.status === 'ASSIGNED' ? 'Назначен исполнитель' : 'Поиск исполнителя' }}
                </span>
              </td>
              <td class="text-secondary text-xs">
                <div v-if="order.executor_phone" class="d-flex align-items-center gap-1 font-medium text-dark">
                  👤 Исполнитель
                </div>
                <div v-if="order.executor_phone" class="text-xxs text-secondary">
                  📱 {{ order.executor_phone }}
                </div>
                <span v-else class="italic text-secondary">Не назначен</span>
              </td>
              <td class="text-end">
                <div class="d-flex align-items-center justify-content-end gap-2">
                  <button
                    v-if="order.status === 'ASSIGNED'"
                    type="button"
                    class="btn-chat-action position-relative"
                    @click="openChat(order)"
                  >
                    💬 Чат
                    <span v-if="unreadOrderIDs.has(order.id)" class="yellow-unread-dot"></span>
                  </button>

                  <button
                    type="button"
                    class="btn-table-action"
                    @click="openOrderDetails(order)"
                  >
                    👁️ Подробнее
                  </button>

                  <button
                    v-if="order.status === 'ASSIGNED'"
                    type="button"
                    class="btn-icon-success"
                    title="Подтвердить выполнение"
                    @click="confirmOrder(order.id)"
                  >
                    ✓
                  </button>

                  <button
                    v-if="order.status === 'SEARCHING' || order.status === 'ASSIGNED'"
                    type="button"
                    class="btn-icon-danger"
                    title="Отменить заказ"
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

    <!-- Order History table (Collapsible) -->
    <OrderHistoryCard
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
      :is-verified="isVerified"
      :customer-addresses="customerAddresses"
      :default-address="defaultAddress"
      @set-active-address="setActiveAddress"
      @add-new-address="addNewAddress"
      @remove-address="removeAddress"
    />

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
                :src="getImageSrc(msg.file_url)"
                class="attachment-img rounded-lg shadow-sm cursor-pointer"
                alt="photo"
                @click="openImagePreview(getImageSrc(msg.file_url))"
                @error="onChatImgError(msg.file_url)"
              />
              <div v-if="isDebug" class="text-xxs text-warning bg-dark p-1 rounded mt-1 overflow-auto max-w-xs style-mono">
                [DEBUG] URL: {{ getImageSrc(msg.file_url) }}
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
        <img :src="previewImageUrl" class="img-preview-content rounded shadow-lg" alt="preview" @error="onPreviewModalImgError" />
        <div class="mt-3 text-right">
          <va-button color="secondary" @click="showImagePreviewModal = false">
            {{ $t('common.close') }}
          </va-button>
        </div>
      </div>
    </va-modal>

    <!-- Debug Images Modal (HTTP & HTTPS test) -->
    <va-modal
      v-model="showDebugImgModal"
      title="🐞 Расширенная Диагностика Загрузки (Android)"
      hide-default-actions
      max-width="650px"
    >
      <div class="p-3">
        <div class="mb-3 text-center">
          <va-button color="primary" size="small" :loading="testingFetch" @click="runFetchDiagnostics">
            🔄 Запустить тест сетевых ответов (fetch / blob / base64)
          </va-button>
        </div>

        <h5 class="va-h6 mb-1 text-primary">1. HTTP (порт 8089)</h5>
        <div class="text-xs text-secondary mb-2 break-all">
          URL: <code>http://94.103.9.172:8089/uploads/chat/029c51c0-3bc9-4569-b49c-6247839105d0_1786616908.jpg</code>
        </div>
        <div class="border p-2 rounded text-center mb-2 bg-dark">
          <img
            v-if="!httpBlobUrl"
            src="http://94.103.9.172:8089/uploads/chat/029c51c0-3bc9-4569-b49c-6247839105d0_1786616908.jpg"
            style="max-width: 100%; max-height: 140px; object-fit: contain;"
            alt="HTTP Test"
            @error="httpImgError = true"
            @load="httpImgError = false"
          />
          <img
            v-else
            :src="httpBlobUrl"
            style="max-width: 100%; max-height: 140px; object-fit: contain;"
            alt="HTTP Blob Test"
          />
          <div v-if="httpImgError && !httpBlobUrl" class="text-danger text-xs mt-1">❌ Ошибка тега &lt;img&gt; (Cleartext/CORS блог)</div>
          <div v-else-if="!httpBlobUrl" class="text-success text-xs mt-1">✅ Тег &lt;img&gt; успешно отобразил!</div>
        </div>
        <div v-if="httpFetchResult" class="p-2 mb-3 bg-secondary rounded text-xs style-mono text-white overflow-auto max-h-32">
          <strong>Fetch результат HTTP:</strong><br/>
          Status: {{ httpFetchResult.status }} {{ httpFetchResult.statusText }}<br/>
          OK: {{ httpFetchResult.ok }} | Size: {{ httpFetchResult.size }} bytes<br/>
          Err: {{ httpFetchResult.error || 'нет' }}
        </div>

        <h5 class="va-h6 mb-1 text-primary">2. HTTPS (порт 8443 - SSL)</h5>
        <div class="text-xs text-secondary mb-2 break-all">
          URL: <code>https://94.103.9.172:8443/uploads/chat/029c51c0-3bc9-4569-b49c-6247839105d0_1786616908.jpg</code>
        </div>
        <div class="border p-2 rounded text-center mb-2 bg-dark">
          <img
            v-if="!httpsBlobUrl"
            src="https://94.103.9.172:8443/uploads/chat/029c51c0-3bc9-4569-b49c-6247839105d0_1786616908.jpg"
            style="max-width: 100%; max-height: 140px; object-fit: contain;"
            alt="HTTPS Test"
            @error="httpsImgError = true"
            @load="httpsImgError = false"
          />
          <img
            v-else
            :src="httpsBlobUrl"
            style="max-width: 100%; max-height: 140px; object-fit: contain;"
            alt="HTTPS Blob Test"
          />
          <div v-if="httpsImgError && !httpsBlobUrl" class="text-danger text-xs mt-1">❌ Ошибка тега &lt;img&gt; (SSL Cert / Self-signed блог)</div>
          <div v-else-if="!httpsBlobUrl" class="text-success text-xs mt-1">✅ Тег &lt;img&gt; успешно отобразил!</div>
        </div>
        <div v-if="httpsFetchResult" class="p-2 mb-3 bg-secondary rounded text-xs style-mono text-white overflow-auto max-h-32">
          <strong>Fetch результат HTTPS:</strong><br/>
          Status: {{ httpsFetchResult.status }} {{ httpsFetchResult.statusText }}<br/>
          OK: {{ httpsFetchResult.ok }} | Size: {{ httpsFetchResult.size }} bytes<br/>
          Err: {{ httpsFetchResult.error || 'нет' }}
        </div>

        <div class="text-right">
          <va-button color="secondary" @click="showDebugImgModal = false">
            Закрыть
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

import CustomerHeader from './components/CustomerHeader.vue'
import OrderHistoryCard from './components/OrderHistoryCard.vue'
import OrderDetailsModal from './components/OrderDetailsModal.vue'
import CreateOrderModal from './components/CreateOrderModal.vue'
import CustomerProfileModal from './components/CustomerProfileModal.vue'

export default defineComponent({
  name: 'CustomerDashboard',
  components: {
    LanguageSwitcher,
    UpdateBanner,
    CustomerHeader,
    OrderHistoryCard,
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

    const showDebugImgModal = ref(false)
    const httpImgError = ref(false)
    const httpsImgError = ref(false)
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

      // 1. Test HTTP
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

      // 2. Test HTTPS
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
    const isHistoryCollapsed = ref(true)
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

    const blobImageCache = ref<Record<string, string>>({})

    const getImageSrc = (path?: string) => {
      if (!path) return ''
      if (blobImageCache.value[path]) {
        return blobImageCache.value[path]
      }
      return resolveFileUrl(path)
    }

    const onChatImgError = async (path?: string) => {
      if (!path || blobImageCache.value[path]) return
      const fullUrl = resolveFileUrl(path)
      try {
        const res = await fetch(fullUrl)
        if (res.ok) {
          const blob = await res.blob()
          if (blob.size > 0) {
            blobImageCache.value[path] = URL.createObjectURL(blob)
          }
        }
      } catch (err) {
        console.warn('[CustomerDashboard] fetch blob fallback failed for:', fullUrl, err)
      }
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

    const openImagePreview = async (url: string) => {
      if (!url) return
      previewImageUrl.value = url
      showImagePreviewModal.value = true

      if (!url.startsWith('blob:')) {
        onPreviewModalImgError()
      }
    }

    const onPreviewModalImgError = async () => {
      if (!previewImageUrl.value || previewImageUrl.value.startsWith('blob:')) return
      try {
        const res = await fetch(previewImageUrl.value)
        if (res.ok) {
          const blob = await res.blob()
          if (blob.size > 0) {
            previewImageUrl.value = URL.createObjectURL(blob)
          }
        }
      } catch (e) {
        console.warn('[CustomerDashboard] modal preview fetch fallback failed:', e)
      }
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
      isHistoryCollapsed,
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
      showDebugImgModal,
      httpImgError,
      httpsImgError,
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

.btn-action-outline {
  background: #ffffff;
  border: 2px solid #2563eb;
  color: #2563eb;
  border-radius: 12px;
  padding: 14px 20px;
  font-size: 1rem;
  font-weight: 700;
  cursor: pointer;
  transition: all 0.15s ease;
  box-shadow: 0 2px 6px rgba(37, 99, 235, 0.08);
}

.btn-action-outline:hover {
  background: #eff6ff;
  border-color: #1d4ed8;
  color: #1d4ed8;
}

.btn-action-solid-green {
  background: #16a34a;
  border: 2px solid #16a34a;
  color: #ffffff;
  border-radius: 12px;
  padding: 14px 20px;
  font-size: 1rem;
  font-weight: 700;
  cursor: pointer;
  transition: all 0.15s ease;
  box-shadow: 0 4px 12px rgba(22, 163, 74, 0.2);
}

.btn-action-solid-green:hover {
  background: #15803d;
  border-color: #15803d;
}

.btn-refresh {
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  width: 32px;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 16px;
  color: #64748b;
  cursor: pointer;
  transition: all 0.15s ease;
}

.btn-refresh:hover {
  background: #e2e8f0;
  color: #0f172a;
}

.status-pill-badge {
  font-size: 0.8rem;
  font-weight: 600;
  padding: 4px 12px;
  border-radius: 20px;
  display: inline-block;
}

.pill-assigned {
  background: #eff6ff;
  color: #2563eb;
}

.pill-searching {
  background: #fffbeb;
  color: #d97706;
}

.btn-chat-action {
  background: #f8fafc;
  border: 1px solid #cbd5e0;
  border-radius: 8px;
  padding: 6px 12px;
  font-size: 0.825rem;
  font-weight: 600;
  color: #334155;
  cursor: pointer;
  transition: all 0.15s ease;
}

.btn-chat-action:hover {
  background: #e2e8f0;
  color: #0f172a;
}

.btn-table-action {
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  padding: 6px 12px;
  font-size: 0.825rem;
  font-weight: 500;
  color: #475569;
  cursor: pointer;
  transition: all 0.15s ease;
}

.btn-table-action:hover {
  background: #f1f5f9;
  color: #0f172a;
}

.btn-icon-success {
  width: 30px;
  height: 30px;
  border-radius: 6px;
  border: none;
  background: #dcfce7;
  color: #15803d;
  font-weight: bold;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}

.btn-icon-success:hover {
  background: #bbf7d0;
}

.btn-icon-danger {
  width: 30px;
  height: 30px;
  border-radius: 6px;
  border: none;
  background: #fee2e2;
  color: #b91c1c;
  font-weight: bold;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}

.btn-icon-danger:hover {
  background: #fca5a5;
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
