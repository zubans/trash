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

      <!-- Toast Notifications Container -->
      <div class="toast-container">
        <!-- Success Toast -->
        <div v-if="successMsg" class="toast success">
          <div class="toast-icon">
            <i class="ph-bold ph-check"></i>
          </div>
          <div class="toast-content">
            <div class="toast-title">Успешно</div>
            <div class="toast-message">{{ successMsg }}</div>
          </div>
          <button type="button" class="toast-close" @click="successMsg = ''">
            <i class="ph ph-x"></i>
          </button>
        </div>

        <!-- Error Toast -->
        <div v-if="errorMsg" class="toast error">
          <div class="toast-icon">
            <i class="ph-bold ph-warning"></i>
          </div>
          <div class="toast-content">
            <div class="toast-title">Ошибка</div>
            <div class="toast-message">{{ errorMsg }}</div>
          </div>
          <button type="button" class="toast-close" @click="errorMsg = ''">
            <i class="ph ph-x"></i>
          </button>
        </div>
      </div>

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
          <div
            v-for="order in activeOrders"
            :key="order.id"
            :class="['order-row', { 'chat-open': openChatOrderId === order.id }]"
          >
            <!-- Ultra-compact Summary Row -->
            <div class="order-summary list-item-compact cursor-pointer" @click="openOrderDetails(order)">
              <div class="item-left-group">
                <div :class="['o-icon item-icon', order.is_urgent ? 'orange' : 'purple']">
                  <i :class="['ph-fill', order.is_urgent ? 'ph-rocket-launch' : 'ph-package']"></i>
                </div>
                <div class="o-info item-text-stack">
                  <div class="item-price-top">{{ Number(order.hold_amount).toFixed(2) }} {{ currencySymbol }}</div>
                  <div class="o-title item-title">{{ formatOrderType(order) }}</div>
                </div>
              </div>
              <div class="o-actions item-actions" @click.stop>
                <button
                  v-if="order.status === 'ASSIGNED' || order.status === 'EXECUTED'"
                  type="button"
                  :class="['btn-action primary chat-btn', { active: openChatOrderId === order.id }]"
                  title="Чат"
                  @click="toggleChat(order)"
                >
                  <i class="ph-fill ph-chat-circle-dots"></i>
                </button>
                <button
                  v-if="order.status === 'EXECUTED'"
                  type="button"
                  class="btn-action success confirm-btn"
                  title="Подтвердить выполнение и закрыть заказ"
                  @click="confirmOrder(order.id)"
                >
                  <i class="ph-bold ph-check"></i>
                </button>
              </div>
            </div>

            <!-- Inline Accordion Chat Area -->
            <div v-if="openChatOrderId === order.id" class="inline-chat">
              <!-- Hidden File Input for Image Attachments -->
              <input
                ref="chatFileInputRef"
                type="file"
                accept="image/*"
                style="display: none"
                @change="onChatFileSelected"
              />

              <div ref="chatContainerRef" class="chat-msgs">
                <div v-if="chatMessages.length === 0" class="text-center text-muted text-sm py-3">
                  Сообщений пока нет. Напишите первым!
                </div>
                <div
                  v-for="msg in chatMessages"
                  :key="msg.id"
                  :class="['msg-container', msg.sender_id === currentUserId ? 'outgoing' : 'incoming']"
                >
                  <!-- Actions (Edit/Delete for outgoing user messages only, hidden for system messages) -->
                  <div v-if="msg.sender_id === currentUserId && !isSystemMessage(msg)" class="msg-actions">
                    <button type="button" class="action-icon-btn" title="Редактировать" @click="startEditMessage(msg)">
                      <i class="ph ph-pencil-simple"></i>
                    </button>
                    <button type="button" class="action-icon-btn danger" title="Удалить" @click="deleteChatMessage(msg.id)">
                      <i class="ph ph-trash"></i>
                    </button>
                  </div>

                  <div class="msg-content">
                    <div :class="['bubble', { 'has-attachment': isImageAttachment(msg) }]">
                      <!-- Image Attachment -->
                      <div v-if="isImageAttachment(msg)" class="chat-img-wrapper mb-1">
                        <img
                          :src="getImageSrc(msg)"
                          alt="Фото"
                          class="msg-image"
                          @error="onChatImgError(msg.file_url || msg.content)"
                          @click="openImagePreview(getImageSrc(msg))"
                        />
                      </div>
                      <span v-if="msg.text || msg.content" class="msg-text">{{ msg.text || msg.content }}</span>
                    </div>

                    <div class="msg-meta">
                      <span>{{ formatMessageTime(msg.created_at) }}</span>
                      <span v-if="msg.updated_at" class="msg-edited">(изменено)</span>
                      <i v-if="msg.sender_id === currentUserId" :class="['ph-bold', msg.status === 'read' ? 'ph-checks read-receipt' : 'ph-check']"></i>
                    </div>
                  </div>
                </div>
              </div>

              <!-- Chat Input Area with Editing indicator -->
              <div class="chat-input-area">
                <div v-if="editingMessageId" class="edit-banner d-flex justify-content-between align-items-center mb-2 px-3 py-1 bg-light rounded border text-xs">
                  <span><i class="ph ph-pencil-simple me-1"></i> Редактирование сообщения</span>
                  <button type="button" class="btn-close-sm border-0 bg-transparent cursor-pointer" @click="cancelEditMessage">&times;</button>
                </div>
                <form class="input-group" @submit.prevent="sendChatMessage">
                  <button
                    type="button"
                    class="btn-attach"
                    title="Прикрепить фото"
                    :disabled="uploadingChatFile || !!editingMessageId"
                    @click="triggerImageSelect"
                  >
                    <i v-if="uploadingChatFile" class="ph ph-spinner spinner"></i>
                    <i v-else class="ph-bold ph-image"></i>
                  </button>
                  <input
                    v-model="chatInputText"
                    type="text"
                    class="inline-input"
                    :placeholder="editingMessageId ? 'Измените текст сообщения...' : 'Написать сообщение исполнителю...'"
                  />
                  <button type="submit" class="btn-inline-send" :disabled="!chatInputText.trim() && !uploadingChatFile">
                    <i :class="['ph-bold', editingMessageId ? 'ph-check' : 'ph-paper-plane-tilt']"></i>
                  </button>
                </form>
              </div>
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
            class="order-pill cursor-pointer"
            style="box-shadow: none; border-color: rgba(0,0,0,0.05); background: transparent;"
            @click="openOrderDetails(order)"
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
            <div class="op-status" style="background: transparent; color: var(--text-muted); display: flex; flex-direction: column; align-items: flex-end; gap: 2px;">
              <span>{{ order.status === 'COMPLETED' ? 'Завершен' : 'Отменен' }}</span>
              <span v-if="orderReviewsMap[order.id]" class="review-status-badge" title="Оценка отправлена">
                <i class="ph-fill ph-star" style="color: #f59e0b; font-size: 11px;"></i>
                <span>{{ orderReviewsMap[order.id].rating }}/5</span>
              </span>
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
        @cancel-order="cancelOrder"
        @open-review-modal="openReviewModal"
      />

      <!-- Review Modal -->
      <ReviewModal
        v-model="showReviewModal"
        :order-id="reviewTargetOrderId"
        role="CUSTOMER"
        @reviewed="onReviewSubmitted"
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

      <!-- Image Preview Modal -->
      <div v-if="showImagePreviewModal" class="img-preview-overlay" @click.self="showImagePreviewModal = false">
        <div class="img-preview-card">
          <button type="button" class="btn-close-preview" aria-label="Закрыть" @click="showImagePreviewModal = false">
            <i class="ph ph-x"></i>
          </button>
          <img :src="previewImageUrl" alt="Предпросмотр" class="img-preview-full" />
        </div>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { Capacitor } from '@capacitor/core'
import { Camera, CameraResultType, CameraSource } from '@capacitor/camera'
import { useAuthStore } from '../../stores/auth-store'
import UpdateBanner from '../../components/UpdateBanner.vue'
import LanguageSwitcher from '../../components/LanguageSwitcher.vue'
import OrderDetailsModal from './components/OrderDetailsModal.vue'
import CreateOrderModal from './components/CreateOrderModal.vue'
import CustomerProfileModal from './components/CustomerProfileModal.vue'
import ReviewModal from './components/ReviewModal.vue'
import api, { buildChatWebSocketUrl, resolveFileUrl } from '../../services/api'
import { checkMyOrderReview, type OrderReview } from '../../api/review'
import { compressImage } from '../../utils/imageCompressor'
import { getServiceCategories, getServiceCategoryChildren, type ServiceNode } from '../../api/services'

export default defineComponent({
  name: 'CustomerDashboardV2',
  components: {
    UpdateBanner,
    LanguageSwitcher,
    OrderDetailsModal,
    CreateOrderModal,
    CustomerProfileModal,
    ReviewModal,
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

    let successTimer: any = null
    let errorTimer: any = null

    watch(successMsg, (val) => {
      if (successTimer) clearTimeout(successTimer)
      if (val) {
        successTimer = setTimeout(() => {
          successMsg.value = ''
        }, 5000)
      }
    })

    watch(errorMsg, (val) => {
      if (errorTimer) clearTimeout(errorTimer)
      if (val) {
        errorTimer = setTimeout(() => {
          errorMsg.value = ''
        }, 5000)
      }
    })

    // Addresses
    const defaultAddress = ref('Москва, ул. Тверская, д. 1')
    const customerAddresses = ref<any[]>([
      { address: 'Москва, ул. Тверская, д. 1' }
    ])
    const newAddressInput = ref('')

    // Orders
    const orders = ref<any[]>([])
    const isHistoryCollapsed = ref(false)
    const orderReviewsMap = ref<Record<string, OrderReview>>({})

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
    const subCategories = ref<ServiceNode[]>([])
    const serviceVariants = ref<ServiceNode[]>([])
    const selectedCategoryId = ref<string | null>(null)
    const selectedSubCategoryId = ref<string | null>(null)
    const selectedVariantId = ref<string | null>(null)
    const isUrgent = ref(false)
    const isAsap = ref(false)
    const orderAddress = ref(defaultAddress.value)
    const orderLat = ref<number | null>(null)
    const orderLon = ref<number | null>(null)
    const geocoding = ref(false)
    const geocodeError = ref('')

    const activeOrders = computed(() => {
      return orders.value.filter((o) => ['SEARCHING', 'ASSIGNED', 'EXECUTED'].includes(o.status))
    })

    const historyOrders = computed(() => {
      return orders.value.filter((o) => ['COMPLETED', 'CANCELED'].includes(o.status))
    })

    const selectedVariant = computed(() =>
      serviceVariants.value.find((v) => v.id === selectedVariantId.value)
    )

    const isAuctionSelected = computed(() => !!selectedVariant.value?.is_auction)

    const localizedName = (node?: ServiceNode) => {
      if (!node) return ''
      return node.name['ru'] || node.name['en'] || node.code || ''
    }

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
            orderAddress.value = response.data.address
            customerAddresses.value = [{ address: response.data.address }]
          }
        }
      } catch (err) {
        console.error('Failed to load profile:', err)
      }
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
        geocodeError.value = err.response?.data || 'Не удалось геокодировать адрес'
      } finally {
        geocoding.value = false
      }
    }

    const fetchReviewsForHistory = async () => {
      const completed = orders.value.filter((o) => o.status === 'COMPLETED')
      for (const order of completed) {
        if (!orderReviewsMap.value[order.id]) {
          try {
            const res = await checkMyOrderReview(order.id)
            if (res && res.has_reviewed && res.review) {
              orderReviewsMap.value[order.id] = res.review
            }
          } catch (err) {
            // ignore
          }
        }
      }
    }

    const fetchOrders = async () => {
      try {
        const response = await api.get('/customer/orders')
        const newOrders = response.data || []
        // Update items in place or update orders if structure changed to prevent re-rendering active chat accordion
        orders.value = newOrders
        fetchReviewsForHistory()
      } catch (err) {
        console.error('Failed to fetch orders:', err)
      }
    }

    const openCreateOrderModal = async () => {
      orderAddress.value = defaultAddress.value
      orderLat.value = null
      orderLon.value = null
      geocodeError.value = ''
      selectedCategoryId.value = null
      selectedSubCategoryId.value = null
      selectedVariantId.value = null
      subCategories.value = []
      serviceVariants.value = []
      isUrgent.value = false
      isAsap.value = false
      showCreateOrderModal.value = true
      try {
        serviceCategories.value = await getServiceCategories()
      } catch (err) {
        console.error('Failed to load categories:', err)
      }
      await geocodeAddress()
    }

    const submitOrder = async () => {
      if (!selectedVariantId.value) {
        errorMsg.value = 'Выберите тип услуги'
        return
      }
      creatingOrder.value = true
      errorMsg.value = ''
      try {
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
        successMsg.value = 'Заказ успешно создан'
        showCreateOrderModal.value = false
        await fetchProfile()
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

    const showReviewModal = ref(false)
    const reviewTargetOrderId = ref('')

    const openReviewModal = (order: any) => {
      reviewTargetOrderId.value = order.id
      showOrderDetailsModal.value = false
      showReviewModal.value = true
    }

    const onReviewSubmitted = () => {
      successMsg.value = 'Спасибо за отзыв!'
      if (reviewTargetOrderId.value) {
        delete orderReviewsMap.value[reviewTargetOrderId.value]
      }
      fetchReviewsForHistory()
    }

    const openOrderDetails = (order: any) => {
      selectedOrderDetails.value = order
      showOrderDetailsModal.value = true
    }

    // Chat State & Logic
    const openChatOrderId = ref<string | null>(null)
    const chatMessages = ref<any[]>([])
    const chatInputText = ref('')
    const chatContainerRef = ref<any>(null)
    const chatFileInputRef = ref<HTMLInputElement | null>(null)
    const uploadingChatFile = ref(false)
    const blobImageCache = ref<Record<string, string>>({})
    const showImagePreviewModal = ref(false)
    const previewImageUrl = ref('')
    const ws = ref<WebSocket | null>(null)
    let chatPollTimer: any = null

    const currentUserId = computed(() => authStore.userID)

    const isSystemMessage = (msg: any) => {
      if (!msg) return false
      if (msg.file_type === 'system' || msg.type === 'system') return true
      const text = msg.text || msg.content || ''
      return text.includes('Исполнитель отметил(а)') || text.includes('Исполнитель отметила(ся)') || text.startsWith('📦')
    }

    const isImageAttachment = (msg: any) => {
      if (!msg) return false
      const path = msg.file_url || msg.content
      if (!path) return false
      if (msg.file_type === 'image') return true
      const url = path.toLowerCase()
      return url.endsWith('.jpg') || url.endsWith('.jpeg') || url.endsWith('.png') || url.endsWith('.webp') || url.endsWith('.gif') || url.startsWith('/uploads/')
    }

    const getImageSrc = (msg: any) => {
      const path = typeof msg === 'string' ? msg : (msg?.file_url || msg?.content)
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

    const openImagePreview = (url: string) => {
      if (!url) return
      previewImageUrl.value = url
      showImagePreviewModal.value = true
    }

    const triggerImageSelect = async () => {
      if (Capacitor.isNativePlatform()) {
        try {
          const photo = await Camera.getPhoto({
            quality: 85,
            allowEditing: false,
            resultType: CameraResultType.Uri,
            source: CameraSource.Prompt,
          })

          if (photo.webPath && openChatOrderId.value) {
            uploadingChatFile.value = true
            const response = await fetch(photo.webPath)
            const blob = await response.blob()
            let file = new File([blob], `photo_${Date.now()}.${photo.format || 'jpg'}`, { type: `image/${photo.format || 'jpeg'}` })
            file = await compressImage(file, 150, 300)

            const formData = new FormData()
            formData.append('file', file)
            if (chatInputText.value.trim()) {
              formData.append('text', chatInputText.value.trim())
            }

            const res = await api.post(`/chats/${openChatOrderId.value}/upload`, formData, {
              headers: { 'Content-Type': 'multipart/form-data' },
            })
            if (res.data) {
              const exists = chatMessages.value.some((m: any) => m.id === res.data.id)
              if (!exists) {
                chatMessages.value.push(res.data)
                scrollToChatBottom()
              }
            }
            chatInputText.value = ''
          }
        } catch (err: any) {
          console.warn('[CustomerDashboardV2] Camera capture error/cancel:', err)
        } finally {
          uploadingChatFile.value = false
        }
      } else {
        const el: any = chatFileInputRef.value
        if (!el) return
        if (Array.isArray(el)) {
          if (el[0]) el[0].click()
        } else if (typeof el.click === 'function') {
          el.click()
        }
      }
    }

    const onChatFileSelected = async (event: Event) => {
      const target = event.target as HTMLInputElement
      if (!target.files || target.files.length === 0 || !openChatOrderId.value) return
      let file = target.files[0]
      uploadingChatFile.value = true

      try {
        if (file.type.startsWith('image/')) {
          file = await compressImage(file, 150, 300)
        }

        const formData = new FormData()
        formData.append('file', file)
        if (chatInputText.value.trim()) {
          formData.append('text', chatInputText.value.trim())
        }

        const response = await api.post(`/chats/${openChatOrderId.value}/upload`, formData, {
          headers: { 'Content-Type': 'multipart/form-data' },
        })

        const savedMsg = response.data
        if (savedMsg) {
          const exists = chatMessages.value.some((m: any) => m.id === savedMsg.id)
          if (!exists) {
            chatMessages.value.push(savedMsg)
            scrollToChatBottom()
          }
        }
        chatInputText.value = ''
      } catch (err: any) {
        console.error('[CustomerDashboard] chat file upload failed:', err)
        errorMsg.value = 'Ошибка загрузки изображения'
      } finally {
        uploadingChatFile.value = false
        target.value = ''
      }
    }

    const scrollToChatBottom = () => {
      nextTick(() => {
        if (chatContainerRef.value) {
          if (Array.isArray(chatContainerRef.value)) {
            if (chatContainerRef.value[0]) {
              chatContainerRef.value[0].scrollTop = chatContainerRef.value[0].scrollHeight
            }
          } else {
            chatContainerRef.value.scrollTop = chatContainerRef.value.scrollHeight
          }
        }
      })
    }

    const closeInlineChat = () => {
      openChatOrderId.value = null
      chatMessages.value = []
      chatInputText.value = ''
      if (ws.value) {
        ws.value.close()
        ws.value = null
      }
      if (chatPollTimer) {
        clearInterval(chatPollTimer)
        chatPollTimer = null
      }
    }

    const editingMessageId = ref<string | null>(null)

    const startEditMessage = (msg: any) => {
      editingMessageId.value = msg.id
      chatInputText.value = msg.text || msg.content || ''
    }

    const cancelEditMessage = () => {
      editingMessageId.value = null
      chatInputText.value = ''
    }

    const formatMessageTime = (dateStr?: string) => {
      if (!dateStr) return ''
      const d = new Date(dateStr)
      return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    }

    const deleteChatMessage = async (messageId: string) => {
      if (!openChatOrderId.value || !messageId) return
      if (!confirm('Удалить сообщение?')) return
      try {
        await api.delete(`/chats/${openChatOrderId.value}/messages/${messageId}`)
        chatMessages.value = chatMessages.value.filter((m: any) => m.id !== messageId)
      } catch (err: any) {
        console.error('[CustomerDashboardV2] delete message error:', err)
        errorMsg.value = 'Ошибка удаления сообщения'
      }
    }

    const toggleChat = async (order: any) => {
      if (openChatOrderId.value === order.id) {
        closeInlineChat()
        return
      }

      closeInlineChat()
      openChatOrderId.value = order.id

      // 1. Fetch message history
      try {
        const response = await api.get(`/chats/${order.id}/messages`)
        chatMessages.value = response.data || []
        scrollToChatBottom()
      } catch (err) {
        console.error('Failed to load chat messages:', err)
      }

      // 2. Open WebSocket connection
      try {
        const wsUrl = buildChatWebSocketUrl(order.id, authStore.token)
        ws.value = new WebSocket(wsUrl)
        ws.value.onmessage = (event) => {
          try {
            const data = JSON.parse(event.data)
            if (data.type === 'status_update' && Array.isArray(data.message_ids)) {
              const updatedSet = new Set(data.message_ids)
              chatMessages.value = chatMessages.value.map((m: any) => {
                if (updatedSet.has(m.id)) {
                  return { ...m, status: data.status }
                }
                return m
              })
              return
            }
            if (data.type === 'message_deleted') {
              chatMessages.value = chatMessages.value.filter((m: any) => m.id !== data.message_id)
              return
            }
            if (data.type === 'message_edited') {
              const targetId = data.message_id || data.id
              const newText = data.text || data.message?.text
              const idx = chatMessages.value.findIndex((m: any) => m.id === targetId)
              if (idx !== -1) {
                chatMessages.value[idx] = {
                  ...chatMessages.value[idx],
                  text: newText,
                  updated_at: data.updated_at || new Date().toISOString(),
                }
              }
              return
            }
            if (data && (data.text || data.content || data.id)) {
              const exists = chatMessages.value.some((m) => m.id === data.id)
              if (!exists) {
                chatMessages.value.push(data)
                scrollToChatBottom()
              }
            }
          } catch (e) {
            console.error('WS message parse error:', e)
          }
        }
      } catch (e) {
        console.warn('WS connect failed:', e)
      }
    }

    const sendChatMessage = async () => {
      if (!chatInputText.value.trim() || !openChatOrderId.value) return
      const text = chatInputText.value.trim()

      if (editingMessageId.value) {
        const msgId = editingMessageId.value
        try {
          const res = await api.put(`/chats/${openChatOrderId.value}/messages/${msgId}`, { text })
          const idx = chatMessages.value.findIndex((m: any) => m.id === msgId)
          if (idx !== -1 && res.data) {
            chatMessages.value[idx] = res.data
          }
        } catch (err: any) {
          console.error('[CustomerDashboardV2] edit message failed:', err)
          errorMsg.value = 'Ошибка редактирования сообщения'
        } finally {
          cancelEditMessage()
        }
        return
      }

      chatInputText.value = ''

      if (ws.value && ws.value.readyState === WebSocket.OPEN) {
        ws.value.send(JSON.stringify({ text }))
        return
      }

      try {
        const res = await api.post(`/chats/${openChatOrderId.value}/messages`, { text })
        if (res.data) {
          const exists = chatMessages.value.some((m) => m.id === res.data.id)
          if (!exists) {
            chatMessages.value.push(res.data)
            scrollToChatBottom()
          }
        }
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Ошибка отправки сообщения'
      }
    }

    const setActiveAddress = (addr: string) => {
      defaultAddress.value = addr
      orderAddress.value = addr
    }

    const addNewAddress = () => {
      if (!newAddressInput.value.trim()) return
      customerAddresses.value.push({ address: newAddressInput.value.trim() })
      defaultAddress.value = newAddressInput.value.trim()
      orderAddress.value = newAddressInput.value.trim()
      newAddressInput.value = ''
    }

    const removeAddress = (idx: number) => {
      customerAddresses.value.splice(idx, 1)
    }

    const formatOrderType = (order: any) => {
      const variant = order.service_variant
      if (!variant) return 'Большой обычный'
      return localizedName(variant)
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
      await Promise.all([fetchProfile(), fetchOrders()])
      intervalId = setInterval(() => {
        fetchProfile()
        fetchOrders()
      }, 5000)
    })

    onUnmounted(() => {
      if (intervalId) {
        clearInterval(intervalId)
        intervalId = null
      }
      if (successTimer) clearTimeout(successTimer)
      if (errorTimer) clearTimeout(errorTimer)
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
      orderReviewsMap,
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

      openChatOrderId,
      chatMessages,
      chatInputText,
      editingMessageId,
      isSystemMessage,
      startEditMessage,
      cancelEditMessage,
      deleteChatMessage,
      formatMessageTime,
      chatContainerRef,
      chatFileInputRef,
      uploadingChatFile,
      showReviewModal,
      reviewTargetOrderId,
      openReviewModal,
      onReviewSubmitted,
      showImagePreviewModal,
      previewImageUrl,
      currentUserId,
      isImageAttachment,
      getImageSrc,
      onChatImgError,
      openImagePreview,
      triggerImageSelect,
      onChatFileSelected,
      toggleChat,
      sendChatMessage,

      fetchOrders,
      openCreateOrderModal,
      submitOrder,
      confirmOrder,
      cancelOrder,
      submitTopUp,
      openOrderDetails,
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

.review-status-badge {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: 11px;
  font-weight: 500;
  color: var(--text-muted);
  background: rgba(245, 158, 11, 0.08);
  border: 1px solid rgba(245, 158, 11, 0.2);
  border-radius: 12px;
  padding: 1px 6px;
  line-height: 1.2;
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

/* Toast Notifications Styles */
.toast-container {
  position: fixed;
  top: 24px;
  right: 24px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  z-index: 9999;
  pointer-events: none;
}

.toast {
  pointer-events: auto;
  background: rgba(255, 255, 255, 0.85);
  backdrop-filter: blur(16px);
  -webkit-backdrop-filter: blur(16px);
  border: 1px solid rgba(255, 255, 255, 0.9);
  border-radius: 16px;
  padding: 16px 20px;
  width: 340px;
  display: flex;
  align-items: flex-start;
  gap: 16px;
  box-shadow: 0 10px 30px -10px rgba(15, 23, 42, 0.1),
              inset 0 1px 0 rgba(255, 255, 255, 1);
  position: relative;
  overflow: hidden;
  animation: slideInRight 0.4s cubic-bezier(0.16, 1, 0.3, 1) forwards;
}

.toast::before {
  content: '';
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 4px;
}

@keyframes slideInRight {
  from { transform: translateX(120%); opacity: 0; }
  to { transform: translateX(0); opacity: 1; }
}

.toast.success::before { background: var(--success-main, #10b981); }
.toast.success .toast-icon { 
  color: var(--success-main, #10b981); 
  background: rgba(16, 185, 129, 0.1); 
}

.toast.error::before { background: #ef4444; }
.toast.error .toast-icon { 
  color: #ef4444; 
  background: rgba(239, 68, 68, 0.1); 
}

.toast.info::before { background: var(--accent-main, #6366f1); }
.toast.info .toast-icon { 
  color: var(--accent-main, #6366f1); 
  background: rgba(99, 102, 241, 0.1); 
}

.toast-icon {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  flex-shrink: 0;
}

.toast-content {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding-top: 2px;
}

.toast-title {
  font-size: 15px;
  font-weight: 700;
  color: var(--text-title);
  letter-spacing: -0.2px;
}

.toast-message {
  font-size: 14px;
  color: var(--text-muted);
  line-height: 1.4;
  word-break: break-word;
}

.toast-close {
  background: transparent;
  border: none;
  color: #94a3b8;
  font-size: 18px;
  cursor: pointer;
  padding: 4px;
  margin: -4px -4px 0 0;
  border-radius: 8px;
  transition: var(--transition);
}

.toast-close:hover {
  color: var(--text-title);
  background: rgba(15, 23, 42, 0.05);
}

@media (max-width: 480px) {
  .toast-container {
    top: 16px;
    right: 16px;
    left: 16px;
  }
  .toast {
    width: 100%;
    animation: slideInDown 0.4s cubic-bezier(0.16, 1, 0.3, 1) forwards;
  }
  @keyframes slideInDown {
    from { transform: translateY(-100%); opacity: 0; }
    to { transform: translateY(0); opacity: 1; }
  }
}

/* --- Inline Accordion Chat Styles --- */
.order-row {
  background: var(--surface-card);
  backdrop-filter: blur(24px);
  border-radius: 24px;
  border: 1px solid rgba(255,255,255,0.8);
  box-shadow: 0 10px 30px -10px rgba(0,0,0,0.05);
  overflow: hidden;
  transition: var(--transition);
}

.order-row:hover {
  box-shadow: 0 15px 40px -10px rgba(0,0,0,0.08);
  transform: translateY(-2px);
}

.order-row.chat-open {
  border-color: rgba(99, 102, 241, 0.3);
  transform: translateY(0);
}

.order-summary {
  padding: 20px 24px;
  display: flex;
  align-items: center;
  gap: 20px;
}

.o-icon {
  width: 48px; height: 48px; border-radius: 16px;
  background: #e0e7ff; color: #4f46e5;
  display: flex; align-items: center; justify-content: center; font-size: 24px;
}

.o-info { flex: 1; }
.o-title { font-size: 16px; font-weight: 700; color: var(--text-title); }
.o-id { font-size: 13px; color: var(--text-muted); font-family: monospace; }
.o-price { font-size: 18px; font-weight: 700; color: var(--text-title); margin-right: 20px;}

.o-actions { display: flex; gap: 8px; }

.btn-action {
  width: 40px; height: 40px; border-radius: 12px; border: none;
  background: #f1f5f9; color: var(--text-muted);
  display: flex; align-items: center; justify-content: center;
  font-size: 18px; cursor: pointer; transition: var(--transition);
}
.btn-action:hover { background: #e2e8f0; color: var(--text-title); }
.btn-action.danger:hover { background: #fee2e2; color: #ef4444; }

.btn-action.chat-btn { background: #e0e7ff; color: var(--accent-main); }
.btn-action.confirm-btn { background: #dcfce7; color: #15803d; }
.btn-action.confirm-btn:hover { background: #bbf7d0; color: #166534; }
.order-row.chat-open .btn-action.chat-btn { background: var(--accent-main); color: white; box-shadow: 0 4px 12px var(--accent-glow);}

.inline-chat {
  background: rgba(15, 23, 42, 0.02);
  border-top: 1px solid rgba(0,0,0,0.04);
  display: flex;
  flex-direction: column;
}

.chat-msgs {
  padding: 20px 24px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  max-height: 300px;
  overflow-y: auto;
}

.msg { display: flex; flex-direction: column; max-width: 80%; }
.msg.incoming { align-self: flex-start; }
.msg.outgoing { align-self: flex-end; }

.bubble { padding: 10px 14px; font-size: 14px; border-radius: 16px; line-height: 1.4;}
.msg.incoming .bubble { background: white; border: 1px solid rgba(0,0,0,0.05); color: var(--text-title); border-bottom-left-radius: 4px; }
.msg.outgoing .bubble { background: var(--accent-main); color: white; border-bottom-right-radius: 4px; }

.chat-input-area {
  padding: 16px 24px 24px 24px;
}

.input-group {
  display: flex; align-items: center; gap: 12px;
  background: #ffffff; border: 1px solid rgba(0,0,0,0.08); border-radius: 99px;
  padding: 6px 6px 6px 20px; transition: var(--transition);
}
.input-group:focus-within { border-color: var(--accent-main); box-shadow: 0 0 0 4px rgba(99, 102, 241, 0.1); }

.inline-input {
  flex: 1; border: none; outline: none; background: transparent;
  font-family: inherit; font-size: 14px; color: var(--text-title);
}
.inline-input::placeholder { color: #94a3b8; }

.btn-inline-send {
  width: 36px; height: 36px; border-radius: 50%; border: none;
  background: var(--accent-main); color: white; display: flex; align-items: center; justify-content: center;
  cursor: pointer; transition: var(--transition); flex-shrink: 0;
}
.btn-inline-send:hover { background: #4f46e5; }
.btn-inline-send:disabled { opacity: 0.5; cursor: not-allowed; }

.btn-attach {
  background: transparent;
  border: none;
  color: #94a3b8;
  font-size: 20px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 4px;
  border-radius: 50%;
  transition: var(--transition);
}

.btn-attach:hover {
  color: var(--accent-main);
  background: rgba(99, 102, 241, 0.08);
}

.chat-img-wrapper {
  max-width: 220px;
  border-radius: 12px;
  overflow: hidden;
  cursor: pointer;
}

.chat-img {
  width: 100%;
  max-height: 180px;
  object-fit: cover;
  display: block;
  border-radius: 12px;
  transition: transform 0.2s ease;
}

.chat-img:hover {
  transform: scale(1.02);
}

.spinner {
  animation: spin 1s linear infinite;
}

/* Image Preview Modal */
.img-preview-overlay {
  position: fixed;
  top: 0; left: 0; right: 0; bottom: 0;
  background: rgba(15, 23, 42, 0.85);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  z-index: 10000;
}

.img-preview-card {
  position: relative;
  max-width: 90vw;
  max-height: 90vh;
  display: flex;
  align-items: center;
  justify-content: center;
}

.img-preview-full {
  max-width: 100%;
  max-height: 85vh;
  border-radius: 16px;
  box-shadow: 0 20px 50px rgba(0,0,0,0.5);
  object-fit: contain;
}

.btn-close-preview {
  position: absolute;
  top: -16px;
  right: -16px;
  width: 40px;
  height: 40px;
  border-radius: 50%;
  border: 1px solid rgba(255,255,255,0.4);
  background: rgba(15, 23, 42, 0.6);
  color: white;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
  cursor: pointer;
  transition: var(--transition);
  z-index: 10001;
}

.btn-close-preview:hover {
  background: #ef4444;
  color: white;
  transform: scale(1.1);
}

/* --- Message Actions & Container Styling --- */
.msg-container {
  display: flex;
  align-items: center;
  gap: 8px;
  max-width: 85%;
  position: relative;
}
.msg-container.incoming { align-self: flex-start; flex-direction: row; }
.msg-container.outgoing { align-self: flex-end; flex-direction: row-reverse; }

.msg-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  opacity: 0.7;
  transition: all 0.2s cubic-bezier(0.16, 1, 0.3, 1);
}

.msg-container:hover .msg-actions,
.msg-container:focus-within .msg-actions {
  opacity: 1;
}

.action-icon-btn {
  width: 30px;
  height: 30px;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.9);
  backdrop-filter: blur(4px);
  border: 1px solid rgba(0,0,0,0.08);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted);
  font-size: 15px;
  cursor: pointer;
  transition: all 0.2s ease;
  box-shadow: 0 2px 8px rgba(0,0,0,0.04);
}
.action-icon-btn:hover { background: #ffffff; color: var(--text-title); transform: scale(1.1); }
.action-icon-btn.danger:hover { color: #ef4444; background: #fee2e2; border-color: #fca5a5; }

.msg-content { display: flex; flex-direction: column; }
.msg-container.outgoing .msg-content { align-items: flex-end; }
.msg-container.incoming .msg-content { align-items: flex-start; }

.msg-image {
  max-width: 260px;
  border-radius: 14px;
  object-fit: cover;
  display: block;
  border: 1px solid rgba(0,0,0,0.05);
}

.msg-meta {
  font-size: 11px;
  color: var(--text-muted);
  margin-top: 4px;
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 0 4px;
}
.msg-edited { font-style: italic; opacity: 0.8; }
.read-receipt { color: var(--accent-main, #6366f1); font-size: 13px; }

/* --- Ultra-compact Order Item Styles --- */
.list-item-compact {
  background: var(--surface-card, #ffffff);
  border-radius: var(--rad-md, 16px);
  padding: 12px 16px;
  box-shadow: var(--shadow-card, 0 4px 20px rgba(0, 0, 0, 0.04));
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.item-left-group {
  display: flex;
  align-items: center;
  gap: 12px;
  flex: 1;
  min-width: 0;
}

.item-icon {
  width: 40px;
  height: 40px;
  border-radius: 12px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
}

.item-text-stack {
  display: flex;
  flex-direction: column;
  gap: 2px;
  overflow: hidden;
}

.item-price-top {
  font-size: 13px;
  font-weight: 700;
  color: #f59e0b;
  line-height: 1;
}

.item-title {
  font-size: 15px;
  font-weight: 700;
  color: var(--text-title, #0f172a);
  line-height: 1.2;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.item-actions {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
}

.btn-action.primary { background: #e0e7ff; color: #5c60f5; }
.btn-action.success { background: #ecfdf5; color: #10b981; }

@media (max-width: 768px) {
  .msg-actions { opacity: 1; transform: translateX(0); }
  .action-icon-btn { width: 28px; height: 28px; font-size: 14px; }
  .msg-image { max-width: 200px; }
  .list-item-compact { flex-wrap: nowrap; padding: 10px 14px; }
  .order-summary { flex-wrap: nowrap; }
}</style>
