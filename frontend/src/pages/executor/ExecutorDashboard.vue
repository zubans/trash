<template>
  <div class="premium-dashboard-page">
    <div class="container">
      <!-- Шапка -->
      <header class="header">
        <div class="logo">
          <i class="ph-fill ph-planet"></i>
          <div style="display: flex; flex-direction: column;">
            <span>Кабинет</span>
            <span style="font-weight: 500; color: var(--text-muted); font-size: 14px; line-height: 1;">исполнителя</span>
          </div>
        </div>
        <div class="header-controls">
          <div class="lang-switch-wrapper">
            <LanguageSwitcher />
          </div>
          <button type="button" class="control-icon" :title="$t('common.logout')" @click="handleLogout">
            <i class="ph-bold ph-sign-out"></i>
          </button>
        </div>
      </header>

      <!-- Уведомления -->
      <div v-if="successMsg" class="toast-alert success">
        <i class="ph-fill ph-check-circle"></i>
        <span>{{ successMsg }}</span>
        <button class="btn-toast-close" @click="successMsg = ''">&times;</button>
      </div>
      <div v-if="errorMsg" class="toast-alert danger">
        <i class="ph-fill ph-warning-circle"></i>
        <span>{{ errorMsg }}</span>
        <button class="btn-toast-close" @click="errorMsg = ''">&times;</button>
      </div>

      <!-- Сетка Профиль и Кошелек -->
      <div class="premium-grid">
        <!-- Компактный профиль исполнителя -->
        <div class="profile-card">
          <div class="avatar-wrap">
            <div class="avatar"><i class="ph ph-user"></i></div>
            <div class="status-dot"></div>
          </div>
          <div class="profile-info">
            <div class="profile-phone-row">
              <div class="profile-phone">{{ phone || '79997454656' }}</div>
              <div class="verified-badge" title="Верифицирован"><i class="ph-fill ph-check-circle"></i></div>
            </div>
            <div class="badge-brand"><i class="ph-fill ph-check-circle"></i> Статус: {{ status }}</div>
          </div>
        </div>

        <!-- Компактный баланс исполнителя -->
        <div class="balance-card">
          <div class="bc-label">Доступный баланс</div>
          <div class="balance-bottom-row">
            <div class="bc-value">
              {{ Number(balance).toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) }}
              <span class="bc-currency">{{ currencySymbol }}</span>
            </div>
            <button type="button" class="btn-balance" @click="openFinancialHistoryModal">
              <i class="ph-bold ph-arrow-up-right"></i> Вывести
            </button>
          </div>
        </div>
      </div>

      <!-- Смена (Ультра-компактная) -->
      <div class="shift-bar">
        <div class="shift-info-group">
          <div class="shift-icon"><i class="ph-bold ph-clock"></i></div>
          <div class="shift-text-stack">
            <div class="shift-title">
              {{ activeShift && activeShift.status === 'ACTIVE' ? $t('executor.shiftActive') : $t('executor.shiftClosed') }}
            </div>
            <div class="shift-subtitle">
              <template v-if="activeShift && activeShift.status === 'ACTIVE'">
                {{ $t('executor.shiftRemaining', { timer: shiftCountdown || '00:00:00', end: formatDate(activeShift.planned_end_at) }) }}
              </template>
              <template v-else>
                {{ $t('executor.openNewShiftHint') }}
              </template>
            </div>
          </div>
        </div>
        <div class="shift-actions">
          <template v-if="!activeShift || activeShift.status !== 'ACTIVE'">
            <select v-model="shiftDuration" class="shift-select">
              <option v-for="d in durationOptions" :key="d" :value="d">{{ d }} ч.</option>
            </select>
            <button type="button" class="btn-start-shift" :disabled="startingShift" @click="startShift">
              <span v-if="startingShift" class="spinner-sm"></span>
              <template v-else>Открыть <i class="ph-bold ph-caret-right"></i></template>
            </button>
          </template>
          <template v-else>
            <button type="button" class="btn-start-shift danger" :disabled="endingShiftEarly" @click="earlyEndShift">
              <span v-if="endingShiftEarly" class="spinner-sm"></span>
              <template v-else>{{ $t('executor.endShiftEarly') }}</template>
            </button>
          </template>
        </div>
      </div>

      <!-- Назначенные заказы -->
      <div>
        <div class="section-header">
          <h2 class="section-title">Назначенные заказы <span class="section-count">({{ activeAssignedOrders.length }})</span></h2>
          <button type="button" class="btn-header-action" @click="showExecutorMapModal = true">
            <i class="ph-bold ph-map-trifold"></i> 10км / 2км
          </button>
          <button type="button" class="btn-header-action btn-refresh" title="Обновить" @click="fetchAssignedOrders">
            <i class="ph-bold ph-arrows-clockwise"></i>
          </button>
        </div>

        <div v-if="activeAssignedOrders.length === 0" class="empty-state">
          Ожидание назначения заказов в вашей смене
        </div>

        <div v-else class="orders-stack">
          <div
            v-for="order in activeAssignedOrders"
            :key="order.id"
            :class="['order-row', { 'chat-open': selectedChatOrder && selectedChatOrder.id === order.id }]"
          >
            <div class="order-summary list-item-compact">
              <div class="item-left-group cursor-pointer" @click="openOrderDetails(order)">
                <div class="o-icon item-icon">
                  <i class="ph-fill ph-package"></i>
                </div>
                <div class="o-info item-text-stack">
                  <div class="item-price-top">{{ Number(order.final_amount || order.hold_amount).toFixed(2) }} {{ currencySymbol }}</div>
                  <div class="o-title item-title">{{ formatOrderType(order) }}</div>
                  <div v-if="order.address" class="item-subtitle"><i class="ph-fill ph-map-pin me-1"></i>{{ order.address }}</div>
                </div>
              </div>
              <div class="o-actions item-actions" @click.stop>
                <button
                  type="button"
                  class="btn-action success"
                  :title="$t('executor.executed')"
                  @click="markOrderAsExecuted(order.id)"
                >
                  <i class="ph-bold ph-check"></i>
                </button>
                <button
                  type="button"
                  :class="['btn-action primary chat-btn', { active: selectedChatOrder && selectedChatOrder.id === order.id }]"
                  @click="toggleChat(order)"
                >
                  <i class="ph-fill ph-chat-circle-dots"></i>
                </button>
              </div>
            </div>

            <!-- Встроенный чат гармошка -->
            <div v-if="selectedChatOrder && selectedChatOrder.id === order.id" class="inline-chat">
              <input
                ref="chatFileInputRef"
                type="file"
                accept="image/*"
                style="display: none;"
                @change="onChatFileSelected"
              />

              <div ref="chatContainerRef" class="chat-msgs">
                <div v-if="chatMessages.length === 0" class="text-center text-muted text-sm py-3">
                  Сообщений пока нет. Напишите заказчику!
                </div>
                <div
                  v-for="msg in chatMessages"
                  :key="msg.id"
                  :class="['msg-container', msg.sender_id === currentUserId ? 'outgoing' : 'incoming']"
                >
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
                      <div v-if="isImageAttachment(msg)" class="chat-img-wrapper mb-1">
                        <img
                          :src="getImageSrc(msg)"
                          alt="Фото"
                          class="msg-image"
                          @error="onChatImgError(msg)"
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

              <!-- Chat Input Area -->
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
                    :placeholder="editingMessageId ? 'Измените текст сообщения...' : 'Напишите сообщение...'"
                    class="inline-input"
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

      <!-- Заказы на проверке -->
      <div v-if="pendingVerificationOrders.length > 0">
        <div class="section-header">
          <h2 class="section-title" style="color: var(--warning-main);">Заказы на проверке <span class="section-count">({{ pendingVerificationOrders.length }})</span></h2>
        </div>

        <div class="orders-stack">
          <div
            v-for="order in pendingVerificationOrders"
            :key="order.id"
            :class="['order-row', { 'chat-open': selectedChatOrder && selectedChatOrder.id === order.id }]"
          >
            <div class="order-summary list-item-compact review cursor-pointer" @click="toggleChat(order)">
              <div class="item-left-group">
                <div class="item-icon"><i class="ph-fill ph-hourglass-high"></i></div>
                <div class="item-text-stack">
                  <div class="item-price-top">{{ Number(order.final_amount || order.hold_amount).toFixed(2) }} {{ currencySymbol }}</div>
                  <div class="item-title">{{ formatOrderType(order) }}</div>
                  <div class="item-subtitle">Ожидает подтверждения</div>
                </div>
              </div>
              <div class="item-actions" @click.stop>
                <button
                  type="button"
                  :class="['btn-action primary chat-btn', { active: selectedChatOrder && selectedChatOrder.id === order.id }]"
                  @click="toggleChat(order)"
                >
                  <i class="ph-fill ph-chat-circle-dots"></i>
                </button>
              </div>
            </div>

            <!-- Inline chat inside pending verification order -->
            <div v-if="selectedChatOrder && selectedChatOrder.id === order.id" class="inline-chat">
              <input
                ref="chatFileInputRef"
                type="file"
                accept="image/*"
                style="display: none;"
                @change="onChatFileSelected"
              />

              <div ref="chatContainerRef" class="chat-msgs">
                <div v-if="chatMessages.length === 0" class="text-center text-muted text-sm py-3">
                  Сообщений пока нет. Напишите заказчику!
                </div>
                <div
                  v-for="msg in chatMessages"
                  :key="msg.id"
                  :class="['msg-container', msg.sender_id === currentUserId ? 'outgoing' : 'incoming']"
                >
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
                      <div v-if="isImageAttachment(msg)" class="chat-img-wrapper mb-1">
                        <img
                          :src="getImageSrc(msg)"
                          alt="Фото"
                          class="msg-image"
                          @error="onChatImgError(msg)"
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

              <!-- Chat Input Area -->
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
                    :placeholder="editingMessageId ? 'Измените текст сообщения...' : 'Напишите сообщение...'"
                    class="inline-input"
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

      <!-- Заказы поблизости (GPS) -->
      <div>
        <div class="section-header">
          <h2 class="section-title">{{ $t('executor.nearbyOrders') }}</h2>
          <button type="button" class="btn-header-action btn-refresh" title="Обновить" @click="updateCurrentPosition(true)">
            <i class="ph-bold ph-arrows-clockwise"></i>
          </button>
        </div>

        <div class="list-item-compact" style="margin-bottom: 8px;">
          <div class="item-left-group">
            <div class="item-icon" style="background: #f1f5f9; color: var(--text-muted);"><i class="ph-fill ph-crosshair"></i></div>
            <div class="item-text-stack" style="width: 100%;">
              <div class="item-subtitle" style="color: var(--text-muted);">{{ $t('executor.currentCoordinates') }}</div>
              <input type="text" class="gps-input" :value="`${currentLat.toFixed(5)}, ${currentLon.toFixed(5)}`" readonly />
            </div>
          </div>
          <div class="item-actions">
            <button type="button" class="btn-action" style="background: #f1f5f9; color: var(--text-muted);" :title="$t('common.save')" @click="openMapPicker">
              <i class="ph-bold ph-pencil-simple"></i>
            </button>
          </div>
        </div>

        <div v-if="availableOrders.length === 0" class="empty-state">
          {{ $t('executor.noAvailableOrders') }}
        </div>
        <div v-else class="orders-stack">
          <div v-for="order in availableOrders" :key="order.id" class="order-row">
            <div class="order-summary list-item-compact">
              <div class="item-left-group cursor-pointer" @click="openOrderDetails(order)">
                <div class="o-icon item-icon">
                  <i class="ph-fill ph-package"></i>
                </div>
                <div class="o-info item-text-stack">
                  <div class="item-price-top">{{ Number(order.hold_amount).toFixed(2) }} {{ currencySymbol }}</div>
                  <div class="o-title item-title">{{ formatOrderType(order) }}</div>
                  <div v-if="order.address" class="item-subtitle"><i class="ph-fill ph-map-pin me-1"></i>{{ order.address }}</div>
                </div>
              </div>
              <div class="o-actions item-actions">
                <button type="button" class="btn-action success" :title="$t('common.accept')" @click="acceptOrder(order.id)">
                  <i class="ph-bold ph-check"></i>
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- Финансовая история -->
      <div style="margin-top: 8px;">
        <div
          class="section-header cursor-pointer"
          @click="isHistoryCollapsed = !isHistoryCollapsed"
        >
          <i class="ph-bold ph-clock-counter-clockwise" style="color: var(--text-muted); font-size: 18px;"></i>
          <h2 class="section-title" style="font-size: 15px;">{{ $t('executor.financialHistoryTitle') }} <span class="section-count">({{ executorHistoryOrders.length }})</span></h2>
          <i :class="['ph-bold', isHistoryCollapsed ? 'ph-caret-down' : 'ph-caret-up']" style="color: var(--text-muted);"></i>
        </div>

        <div v-if="!isHistoryCollapsed" class="orders-stack">
          <div v-if="executorHistoryOrders.length === 0" class="empty-state">
            {{ $t('customer.noHistoryOrders') }}
          </div>
          <div
            v-for="order in executorHistoryOrders"
            :key="order.id"
            class="list-item-compact history-item cursor-pointer"
            @click="openOrderDetails(order)"
          >
            <div class="item-left-group">
              <div class="item-icon"><i class="ph-bold ph-check-circle"></i></div>
              <div class="item-text-stack">
                <div class="item-title" style="font-size: 14px;">{{ formatOrderType(order) }}</div>
                <div class="item-subtitle" style="font-family: inherit;">#{{ order?.id ? order.id.slice(0, 8) : '---' }}<span v-if="order.address"> • {{ order.address }}</span></div>
              </div>
            </div>
            <div>
              <div class="history-price">{{ Number(order.final_amount || order.hold_amount).toFixed(2) }} {{ currencySymbol }}</div>
              <div class="history-status">
                {{ $t('orderStatus.' + order.status, order.status) }}
                <span v-if="executorReviewsMap[order.id]" class="review-status-badge ms-1" title="Оценка клиента">
                  <i class="ph-fill ph-star" style="color: #f59e0b; font-size: 11px;"></i>
                  <span>{{ executorReviewsMap[order.id].rating }}/5</span>
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Order Details Modal for Executor -->
    <OrderDetailsModal
      v-model="showOrderDetailsModal"
      :selected-order-details="selectedOrderDetails"
      :currency-symbol="currencySymbol"
      :format-order-type="formatOrderType"
      :get-status-color="getStatusColor"
      :format-date-full="formatDate"
      role="EXECUTOR"
      @reject-order="rejectAssignedOrder"
      @open-review-modal="openReviewModal"
    />

    <!-- Review Modal for Executor -->
    <ReviewModal
      v-model="showReviewModal"
      :order-id="reviewTargetOrderId"
      role="EXECUTOR"
      @reviewed="onReviewSubmitted"
    />

    <!-- Executor Map Modal -->
    <ExecutorMapModal
      v-model="showExecutorMapModal"
      :current-lat="currentLat || 55.7558"
      :current-lon="currentLon || 37.6173"
      :currency-symbol="currencySymbol"
      @order-accepted="onMapOrderAccepted"
      @location-changed="onMapLocationChanged"
    />

    <!-- Image Preview Modal -->
    <div v-if="showImagePreviewModal" class="img-preview-overlay" @click="showImagePreviewModal = false">
      <div class="img-preview-card" @click.stop>
        <button type="button" class="btn-close-preview" @click="showImagePreviewModal = false">
          <i class="ph ph-x"></i>
        </button>
        <img :src="previewImageUrl" class="img-preview-full" alt="Full Preview" />
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
import OrderDetailsModal from '../customer/components/OrderDetailsModal.vue'
import ReviewModal from '../customer/components/ReviewModal.vue'
import ExecutorMapModal from './components/ExecutorMapModal.vue'
import api, { buildChatWebSocketUrl, resolveFileUrl } from '../../services/api'
import { checkMyOrderReview, type OrderReview } from '../../api/review'
import { compressImage } from '../../utils/imageCompressor'
import { getServiceVariants, type ServiceNode } from '../../api/services'

export default defineComponent({
  name: 'ExecutorDashboard',
  components: {
    UpdateBanner,
    LanguageSwitcher,
    ExecutorMapModal,
    OrderDetailsModal,
    ReviewModal,
  },
  setup() {
    const router = useRouter()
    const authStore = useAuthStore()

    const phone = ref('')
    const balance = ref(0)
    const status = ref('ACTIVE')

    const currencySymbol = computed(() => (authStore.currency === 'RUB' ? '₽' : '$'))

    const successMsg = ref('')
    const errorMsg = ref('')

    // Auto dismiss toasts
    let successTimer: any = null
    let errorTimer: any = null
    watch(successMsg, (val) => {
      if (val) {
        if (successTimer) clearTimeout(successTimer)
        successTimer = setTimeout(() => { successMsg.value = '' }, 5000)
      }
    })
    watch(errorMsg, (val) => {
      if (val) {
        if (errorTimer) clearTimeout(errorTimer)
        errorTimer = setTimeout(() => { errorMsg.value = '' }, 5000)
      }
    })

    // Shift state
    const activeShift = ref<any>(null)
    const shiftDuration = ref(1)
    const durationOptions = [1, 3, 5]
    const startingShift = ref(false)
    const endingShiftEarly = ref(false)
    const shiftCountdown = ref('')
    let countdownIntervalId: any = null

    // Orders state
    const assignedOrders = ref<any[]>([])
    const availableOrders = ref<any[]>([])
    const executorHistoryOrders = ref<any[]>([])
    const executorReviewsMap = ref<Record<string, OrderReview>>({})
    const isHistoryCollapsed = ref(false)

    const activeAssignedOrders = computed(() =>
      assignedOrders.value.filter((o) => o.status === 'ASSIGNED')
    )

    const pendingVerificationOrders = computed(() =>
      assignedOrders.value.filter((o) => o.status === 'EXECUTED')
    )

    // Location state
    const currentLat = ref(55.7558)
    const currentLon = ref(37.6173)

    // Order Details Modal state
    const showOrderDetailsModal = ref(false)
    const selectedOrderDetails = ref<any>(null)

    const openOrderDetails = (order: any) => {
      selectedOrderDetails.value = order
      showOrderDetailsModal.value = true
    }

    const rejectAssignedOrder = async (orderId: string) => {
      try {
        await api.post(`/executor/orders/${orderId}/reject`)
        successMsg.value = 'Вы отказались от заказа'
        showOrderDetailsModal.value = false
        fetchAssignedOrders()
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Ошибка отказа от заказа'
      }
    }

    // Review Modal state
    const showReviewModal = ref(false)
    const reviewTargetOrderId = ref('')

    const openReviewModal = (order: any) => {
      reviewTargetOrderId.value = order.id
      showReviewModal.value = true
    }

    const onReviewSubmitted = () => {
      successMsg.value = 'Отзыв о заказчике успешно отправлен!'
      showReviewModal.value = false
      fetchHistoryOrders()
    }

    // Map modal state
    const showExecutorMapModal = ref(false)

    // Chat state
    const selectedChatOrder = ref<any>(null)
    const chatMessages = ref<any[]>([])
    const chatInputText = ref('')
    const chatContainerRef = ref<any>(null)
    const chatFileInputRef = ref<HTMLInputElement | null>(null)
    const uploadingChatFile = ref(false)
    const showImagePreviewModal = ref(false)
    const previewImageUrl = ref('')

    const currentUserId = computed(() => authStore.userID)

    const fetchProfile = async () => {
      if (authStore.user) {
        phone.value = authStore.user.phone || authStore.phone || ''
        balance.value = authStore.user.balance || authStore.balance || 0
        status.value = authStore.user.status || authStore.status || 'ACTIVE'
      }
      try {
        const res = await api.get('/auth/me')
        if (res.data) {
          phone.value = res.data.phone || phone.value
          balance.value = res.data.balance ?? balance.value
          status.value = res.data.status || status.value
        }
      } catch (err) {
        // Fallback to authStore
        if (authStore.user) {
          phone.value = authStore.user.phone || ''
          balance.value = authStore.user.balance || 0
        }
      }
    }

    const fetchActiveShift = async () => {
      try {
        const res = await api.get('/executor/shifts/active')
        activeShift.value = res.data
        updateShiftCountdown()
      } catch (err) {
        activeShift.value = null
      }
    }

    const updateShiftCountdown = () => {
      if (!activeShift.value || activeShift.value.status !== 'ACTIVE' || !activeShift.value.planned_end_at) {
        shiftCountdown.value = ''
        return
      }
      const end = new Date(activeShift.value.planned_end_at).getTime()
      const diff = end - Date.now()
      if (diff <= 0) {
        shiftCountdown.value = '00:00:00'
        return
      }
      const h = Math.floor(diff / 3600000).toString().padStart(2, '0')
      const m = Math.floor((diff % 3600000) / 60000).toString().padStart(2, '0')
      const s = Math.floor((diff % 60000) / 1000).toString().padStart(2, '0')
      shiftCountdown.value = `${h}:${m}:${s}`
    }

    const startShift = async () => {
      startingShift.value = true
      try {
        await api.post('/executor/shifts', { duration_hours: shiftDuration.value })
        successMsg.value = 'Смена успешно открыта!'
        await fetchActiveShift()
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Ошибка открытия смены'
      } finally {
        startingShift.value = false
      }
    }

    const earlyEndShift = async () => {
      if (!confirm('Вы уверены, что хотите завершить смену досрочно?')) return
      endingShiftEarly.value = true
      try {
        await api.post('/executor/shifts/early-end')
        successMsg.value = 'Смена завершена'
        await fetchActiveShift()
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Ошибка завершения смены'
      } finally {
        endingShiftEarly.value = false
      }
    }

    const fetchAssignedOrders = async () => {
      try {
        const res = await api.get('/executor/orders/assigned')
        assignedOrders.value = res.data || []
      } catch (err) {
        console.error(err)
      }
    }

    const fetchAvailableOrders = async () => {
      try {
        const res = await api.get('/executor/orders/nearby', {
          params: { lat: currentLat.value, lon: currentLon.value, radius_meters: 5000 },
        })
        availableOrders.value = res.data || []
      } catch (err) {
        console.error(err)
      }
    }

    const fetchReviewsForExecutorHistory = async () => {
      const completed = executorHistoryOrders.value.filter((o) => o.status === 'COMPLETED')
      for (const order of completed) {
        if (!executorReviewsMap.value[order.id]) {
          try {
            const res = await checkMyOrderReview(order.id)
            if (res && res.has_reviewed && res.review) {
              executorReviewsMap.value[order.id] = res.review
            }
          } catch (err) {
            // ignore
          }
        }
      }
    }

    const fetchHistoryOrders = async () => {
      try {
        const res = await api.get('/executor/history')
        executorHistoryOrders.value = res.data?.orders || res.data || []
        fetchReviewsForExecutorHistory()
      } catch (err) {
        console.error(err)
      }
    }

    const acceptOrder = async (orderId: string) => {
      try {
        await api.post(`/executor/orders/${orderId}/accept`)
        successMsg.value = 'Заказ принят в работу!'
        await fetchAssignedOrders()
        await fetchAvailableOrders()
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Ошибка принятия заказа'
      }
    }

    const markOrderAsExecuted = async (orderId: string) => {
      try {
        await api.post(`/executor/orders/${orderId}/execute`)
        successMsg.value = 'Статус заказа изменен на "Исполнил"! Заказчику отправлено уведомление.'
        await fetchAssignedOrders()
        if (selectedChatOrder.value && selectedChatOrder.value.id === orderId) {
          fetchChatMessages(orderId)
        }
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Ошибка изменения статуса заказа'
      }
    }

    const updateCurrentPosition = (force = false) => {
      if (navigator.geolocation) {
        navigator.geolocation.getCurrentPosition(
          (pos) => {
            currentLat.value = pos.coords.latitude
            currentLon.value = pos.coords.longitude
            if (force) fetchAvailableOrders()
          },
          (err) => console.warn(err)
        )
      }
    }

    const openMapPicker = () => {
      showExecutorMapModal.value = true
    }

    const onMapOrderAccepted = () => {
      successMsg.value = 'Заказ взят на карте!'
      fetchAssignedOrders()
      fetchProfile()
    }

    const onMapLocationChanged = (pos: { lat: number; lon: number }) => {
      currentLat.value = pos.lat
      currentLon.value = pos.lon
      successMsg.value = 'Рабочая локация обновлена'
      fetchAvailableOrders()
    }

    // Chat Logic
    const editingMessageId = ref<string | null>(null)

    const isSystemMessage = (msg: any) => {
      if (!msg) return false
      if (msg.file_type === 'system' || msg.type === 'system') return true
      const text = msg.text || msg.content || ''
      return text.includes('Исполнитель отметил(а)') || text.includes('Исполнитель отметила(ся)') || text.startsWith('📦')
    }

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
      if (!selectedChatOrder.value || !messageId) return
      if (!confirm('Удалить сообщение?')) return
      try {
        await api.delete(`/chats/${selectedChatOrder.value.id}/messages/${messageId}`)
        chatMessages.value = chatMessages.value.filter((m: any) => m.id !== messageId)
      } catch (err: any) {
        console.error('[ExecutorDashboard] delete message error:', err)
        errorMsg.value = 'Ошибка удаления сообщения'
      }
    }

    const ws = ref<WebSocket | null>(null)
    let chatPollTimer: any = null

    const closeInlineChat = () => {
      selectedChatOrder.value = null
      chatMessages.value = []
      chatInputText.value = ''
      cancelEditMessage()
      if (ws.value) {
        ws.value.close()
        ws.value = null
      }
      if (chatPollTimer) {
        clearInterval(chatPollTimer)
        chatPollTimer = null
      }
    }

    const toggleChat = (order: any) => {
      if (selectedChatOrder.value && selectedChatOrder.value.id === order.id) {
        closeInlineChat()
      } else {
        closeInlineChat()
        selectedChatOrder.value = order
        fetchChatMessages(order.id)
      }
    }

    const fetchChatMessages = async (orderId: string) => {
      try {
        const res = await api.get(`/chats/${orderId}/messages`)
        chatMessages.value = res.data || []
        scrollToBottom()
      } catch (err) {
        console.error(err)
      }

      // Setup WebSocket connection if not already connected
      if (!ws.value && selectedChatOrder.value) {
        try {
          const wsUrl = buildChatWebSocketUrl(orderId, authStore.token)
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
                  scrollToBottom()
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
    }

    const sendChatMessage = async () => {
      if (!chatInputText.value.trim() || !selectedChatOrder.value) return
      const text = chatInputText.value.trim()

      if (editingMessageId.value) {
        const msgId = editingMessageId.value
        try {
          const res = await api.put(`/chats/${selectedChatOrder.value.id}/messages/${msgId}`, { text })
          const idx = chatMessages.value.findIndex((m: any) => m.id === msgId)
          if (idx !== -1 && res.data) {
            chatMessages.value[idx] = res.data
          }
        } catch (err: any) {
          console.error('[ExecutorDashboard] edit message failed:', err)
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
        const res = await api.post(`/chats/${selectedChatOrder.value.id}/messages`, { text })
        if (res.data) {
          const exists = chatMessages.value.some((m) => m.id === res.data.id)
          if (!exists) {
            chatMessages.value.push(res.data)
            scrollToBottom()
          }
        }
      } catch (err: any) {
        errorMsg.value = 'Ошибка отправки сообщения'
      }
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

          if (photo.webPath && selectedChatOrder.value) {
            uploadingChatFile.value = true
            const response = await fetch(photo.webPath)
            const blob = await response.blob()
            let file = new File([blob], `photo_${Date.now()}.${photo.format || 'jpg'}`, { type: `image/${photo.format || 'jpeg'}` })
            const compressedBlob = await compressImage(file)

            const formData = new FormData()
            formData.append('file', compressedBlob, file.name)
            await api.post(`/chats/${selectedChatOrder.value.id}/upload`, formData, {
              headers: { 'Content-Type': 'multipart/form-data' },
            })
            fetchChatMessages(selectedChatOrder.value.id)
          }
        } catch (err: any) {
          console.warn('[ExecutorDashboard] Camera capture error/cancel:', err)
        } finally {
          uploadingChatFile.value = false
        }
      } else {
        const el = chatFileInputRef.value
        if (Array.isArray(el)) {
          (el[0] as HTMLInputElement)?.click()
        } else if (el) {
          el.click()
        }
      }
    }

    const onChatFileSelected = async (event: Event) => {
      const target = event.target as HTMLInputElement
      if (!target.files || target.files.length === 0 || !selectedChatOrder.value) return
      const file = target.files[0]
      uploadingChatFile.value = true
      try {
        const compressedBlob = await compressImage(file)
        const formData = new FormData()
        formData.append('file', compressedBlob, file.name || 'photo.jpg')
        await api.post(`/chats/${selectedChatOrder.value.id}/upload`, formData, {
          headers: { 'Content-Type': 'multipart/form-data' },
        })
        fetchChatMessages(selectedChatOrder.value.id)
      } catch (err: any) {
        errorMsg.value = 'Ошибка загрузки фото'
      } finally {
        uploadingChatFile.value = false
        target.value = ''
      }
    }

    const blobImageCache = ref<Record<string, string>>({})

    const isImageAttachment = (msg: any) => {
      return msg.file_url || (msg.content && msg.content.startsWith('/uploads/'))
    }

    const getImageSrc = (msg: any) => {
      const path = msg.file_url || msg.content
      if (!path) return ''
      if (blobImageCache.value[path]) {
        return blobImageCache.value[path]
      }
      return resolveFileUrl(path)
    }

    const openImagePreview = (url: string) => {
      previewImageUrl.value = url
      showImagePreviewModal.value = true
    }

    const onChatImgError = async (msg: any) => {
      const path = msg?.file_url || msg?.content
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
        console.warn('[ExecutorDashboard] fetch blob fallback failed for:', fullUrl, err)
      }
    }

    const scrollToBottom = () => {
      nextTick(() => {
        if (chatContainerRef.value) {
          const el = Array.isArray(chatContainerRef.value) ? chatContainerRef.value[0] : chatContainerRef.value
          if (el) el.scrollTop = el.scrollHeight
        }
      })
    }

    const openFinancialHistoryModal = () => {
      fetchHistoryOrders()
    }

    const serviceVariantsMap = ref<Record<string, ServiceNode>>({})

    const fetchServiceVariants = async () => {
      try {
        const variants = await getServiceVariants()
        const map: Record<string, ServiceNode> = {}
        for (const v of variants) {
          map[v.id] = v
        }
        serviceVariantsMap.value = map
      } catch (err) {
        console.error('Failed to load service variants:', err)
      }
    }

    const formatOrderType = (order: any) => {
      if (order?.service_variant) {
        const nameObj = order.service_variant.name
        if (nameObj && typeof nameObj === 'object') {
          return nameObj['ru'] || nameObj['en'] || order.service_variant.code || ''
        }
      }
      if (order?.service_variant_id && serviceVariantsMap.value[order.service_variant_id]) {
        const node = serviceVariantsMap.value[order.service_variant_id]
        if (node.name && typeof node.name === 'object') {
          return node.name['ru'] || node.name['en'] || node.code || ''
        }
      }
      return 'Заказ вывоза мусора'
    }

    const getStatusColor = (statusStr: string) => {
      switch (statusStr) {
        case 'SEARCHING': return '#f59e0b'
        case 'ASSIGNED': return '#3b82f6'
        case 'COMPLETED': return '#10b981'
        case 'CANCELED': return '#ef4444'
        default: return '#64748b'
      }
    }

    const formatDate = (dateStr: string) => {
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
      fetchServiceVariants()
      fetchProfile()
      fetchActiveShift()
      fetchAssignedOrders()
      fetchAvailableOrders()
      fetchHistoryOrders()
      updateCurrentPosition()

      countdownIntervalId = setInterval(updateShiftCountdown, 1000)
      intervalId = setInterval(() => {
        fetchActiveShift()
        fetchAssignedOrders()
        fetchAvailableOrders()
      }, 5000)
    })

    onUnmounted(() => {
      closeInlineChat()
      if (intervalId) clearInterval(intervalId)
      if (countdownIntervalId) clearInterval(countdownIntervalId)
      if (successTimer) clearTimeout(successTimer)
      if (errorTimer) clearTimeout(errorTimer)
    })

    return {
      phone,
      balance,
      status,
      currencySymbol,
      successMsg,
      errorMsg,
      activeShift,
      shiftDuration,
      durationOptions,
      startingShift,
      endingShiftEarly,
      shiftCountdown,
      assignedOrders,
      activeAssignedOrders,
      pendingVerificationOrders,
      availableOrders,
      executorHistoryOrders,
      executorReviewsMap,
      isHistoryCollapsed,
      currentLat,
      currentLon,
      showExecutorMapModal,
      showOrderDetailsModal,
      selectedOrderDetails,
      openOrderDetails,
      rejectAssignedOrder,
      showReviewModal,
      reviewTargetOrderId,
      openReviewModal,
      onReviewSubmitted,
      selectedChatOrder,
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
      showImagePreviewModal,
      previewImageUrl,
      currentUserId,
      fetchAssignedOrders,
      startShift,
      earlyEndShift,
      acceptOrder,
      markOrderAsExecuted,
      updateCurrentPosition,
      openMapPicker,
      onMapOrderAccepted,
      onMapLocationChanged,
      toggleChat,
      sendChatMessage,
      triggerImageSelect,
      onChatFileSelected,
      isImageAttachment,
      getImageSrc,
      openImagePreview,
      onChatImgError,
      openFinancialHistoryModal,
      formatOrderType,
      getStatusColor,
      formatDate,
      handleLogout,
    }
  },
})
</script>

<style scoped>
@import url('https://fonts.googleapis.com/css2?family=Outfit:wght@300;400;500;600;700&family=JetBrains+Mono:wght@500;700&display=swap');

.premium-dashboard-page {
  --bg-base: #f3f5f9;
  --surface-card: #ffffff;
  
  --text-title: #0f172a;
  --text-body: #334155;
  --text-muted: #64748b;
  
  --accent-main: #5c60f5;
  --accent-light: #eef2ff;
  --dark-wallet-bg: linear-gradient(135deg, #1e1b4b, #3b2c6b);
  
  --success-main: #10b981;
  --success-bg: #ecfdf5;
  
  --rad-sm: 12px;
  --rad-md: 20px;
  --rad-lg: 28px;
  
  --shadow-float: 0 4px 24px rgba(0, 0, 0, 0.04);
  --transition: all 0.25s cubic-bezier(0.16, 1, 0.3, 1);

  font-family: 'Outfit', sans-serif;
  background-color: var(--bg-base);
  background-image: 
      radial-gradient(at 0% 0%, rgba(92, 96, 245, 0.05) 0px, transparent 40%),
      radial-gradient(at 100% 100%, rgba(236, 72, 153, 0.03) 0px, transparent 40%);
  background-attachment: fixed;
  color: var(--text-body);
  line-height: 1.5;
  padding: 32px 20px;
  min-height: 100vh;
}

.container {
  max-width: 960px;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: 24px;
}

/* --- Header --- */
.glass-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}

.logo-text {
  font-size: 24px;
  font-weight: 700;
  color: var(--text-title);
  letter-spacing: -0.5px;
  display: flex;
  align-items: center;
  gap: 12px;
}

/* --- Header --- */
.header {
  display: flex; justify-content: space-between; align-items: center; margin-bottom: 4px;
}
.logo { display: flex; align-items: center; gap: 8px; font-size: 20px; font-weight: 700; color: var(--text-title, #0f172a); line-height: 1.1; }
.logo i { color: #5c60f5; font-size: 24px; }
.header-controls { display: flex; gap: 8px; align-items: center; }
.control-icon {
  width: 36px; height: 36px; background: #ffffff; border: 1px solid rgba(0,0,0,0.05); border-radius: 12px;
  display: flex; align-items: center; justify-content: center; font-size: 18px; color: var(--text-muted, #64748b);
  cursor: pointer; transition: all 0.2s ease;
}
.control-icon:hover { color: var(--text-title, #0f172a); border-color: rgba(0,0,0,0.1); }

/* --- Profile Card (New Compact Design) --- */
.profile-card {
  background: var(--surface-card, #ffffff);
  border-radius: var(--rad-lg, 24px);
  padding: 16px;
  box-shadow: var(--shadow-card, 0 4px 20px rgba(0, 0, 0, 0.04));
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 16px;
}

.avatar-wrap { position: relative; flex-shrink: 0; }
.avatar {
  width: 56px; height: 56px;
  background: #f1f5f9; border-radius: 16px;
  display: flex; align-items: center; justify-content: center;
  font-size: 24px; color: #cbd5e1;
}
.status-dot {
  position: absolute; bottom: -2px; right: -2px;
  width: 14px; height: 14px; border-radius: 50%;
  background: #10b981; border: 2px solid #ffffff;
}

.profile-info { display: flex; flex-direction: column; align-items: flex-start; gap: 6px; }

.profile-phone-row { display: flex; align-items: center; gap: 6px; }
.profile-phone { font-size: 20px; font-weight: 700; color: var(--text-title, #0f172a); letter-spacing: -0.5px; line-height: 1; }
.verified-badge { color: #10b981; font-size: 20px; display: flex; align-items: center; justify-content: center; }

.badge-brand {
  background: #eef2ff; color: #5c60f5;
  padding: 4px 12px; border-radius: 99px;
  font-size: 12px; font-weight: 600; display: inline-flex; align-items: center; gap: 4px;
}

/* --- Balance Card (New Compact Dark Design) --- */
.balance-card {
  background: linear-gradient(135deg, #1e1b4b, #3b2c6b);
  border-radius: var(--rad-lg, 24px);
  padding: 20px 16px;
  color: #ffffff;
  display: flex; flex-direction: column; gap: 4px;
  box-shadow: 0 12px 24px -8px rgba(30, 27, 75, 0.4);
  position: relative; overflow: hidden;
}
.bc-label { font-size: 11px; color: rgba(255,255,255,0.6); font-weight: 600; text-transform: uppercase; letter-spacing: 0.5px; }

.balance-bottom-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.bc-value { font-size: 32px; font-weight: 700; letter-spacing: -1px; display: flex; align-items: baseline; gap: 6px;}
.bc-currency { font-size: 20px; color: rgba(255,255,255,0.5); font-weight: 400; }

.btn-balance {
  background: rgba(255,255,255,0.1); border: 1px solid rgba(255,255,255,0.2);
  color: #ffffff; padding: 10px 14px; border-radius: 12px;
  font-size: 13px; font-weight: 600; display: flex; align-items: center; justify-content: center; gap: 6px;
  cursor: pointer;
  transition: all 0.2s ease;
}
.btn-balance:hover { background: rgba(255,255,255,0.2); }

/* --- Shift Control (Compact) --- */
.shift-bar {
  background: var(--surface-card, #ffffff);
  border-radius: var(--rad-md, 16px);
  padding: 12px 16px;
  box-shadow: var(--shadow-card, 0 4px 20px rgba(0, 0, 0, 0.04));
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  border-left: 4px solid #10b981;
}

.shift-info-group { display: flex; align-items: center; gap: 12px; flex: 1; min-width: 0; }

.shift-icon {
  width: 40px; height: 40px; border-radius: 12px; flex-shrink: 0;
  background: #ecfdf5; color: #10b981;
  display: flex; align-items: center; justify-content: center; font-size: 20px;
}

.shift-text-stack { display: flex; flex-direction: column; overflow: hidden; }
.shift-title { font-size: 15px; font-weight: 700; color: var(--text-title, #0f172a); line-height: 1.2; }
.shift-subtitle { font-size: 12px; color: var(--text-muted, #64748b); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

.shift-actions { display: flex; align-items: center; gap: 8px; flex-shrink: 0; }

.shift-select {
  background: #f3f5f9; border: 1px solid rgba(0,0,0,0.05); border-radius: 10px;
  padding: 8px 12px; font-family: inherit; font-size: 14px; font-weight: 600; color: var(--text-title, #0f172a); outline: none;
}

.btn-start-shift {
  background: #5c60f5; color: white; border: none; padding: 10px 16px; border-radius: 10px;
  font-size: 14px; font-weight: 600; display: flex; align-items: center; gap: 4px; box-shadow: 0 4px 12px rgba(92, 96, 245, 0.2);
  cursor: pointer;
}
.btn-start-shift.danger { background: #ef4444; }

/* --- Section Headers --- */
.section-header { display: flex; align-items: center; gap: 8px; margin-top: 8px; margin-bottom: 4px; }
.section-title { font-size: 16px; font-weight: 700; color: var(--text-title, #0f172a); flex: 1; margin: 0; }
.section-count { font-size: 16px; font-weight: 700; color: var(--text-muted, #64748b); }

.btn-header-action {
  height: 28px; border-radius: 8px; background: #ffffff; border: 1px solid rgba(0,0,0,0.05);
  display: flex; align-items: center; justify-content: center; color: var(--text-title, #0f172a); font-size: 12px; font-weight: 600; padding: 0 10px; gap: 4px;
  cursor: pointer;
}
.btn-refresh { width: 28px; padding: 0; color: var(--text-muted, #64748b); }

/* --- Grid --- */
.premium-grid {
  display: grid;
  grid-template-columns: 1.5fr 1fr;
  gap: 24px;
}

/* --- Profile Card --- */
.surface-card {
  background: var(--surface-card);
  border-radius: var(--rad-lg);
  padding: 32px;
  box-shadow: var(--shadow-float);
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
  position: relative;
  width: 80px;
  height: 80px;
  border-radius: 20px;
  background: #f1f5f9;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 32px;
  color: #cbd5e1;
  box-shadow: inset 0 2px 4px rgba(0,0,0,0.05);
}

.online-dot {
  position: absolute;
  bottom: -4px;
  right: -4px;
  width: 20px;
  height: 20px;
  border-radius: 50%;
  background: var(--success-main);
  border: 4px solid #ffffff;
}

.profile-info {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 4px;
}

.role-badge {
  font-size: 13px;
  color: var(--text-muted);
  font-weight: 500;
}

.profile-info h2 {
  font-size: 28px;
  font-weight: 700;
  color: var(--text-title);
  letter-spacing: -0.5px;
  line-height: 1;
  margin-bottom: 4px;
}

.status-pill-badge {
  padding: 6px 14px;
  border-radius: 99px;
  font-size: 13px;
  font-weight: 600;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.status-pill-badge.active {
  background: var(--accent-light);
  color: var(--accent-main);
}

.status-pill-badge.inactive {
  background: #f1f5f9;
  color: var(--text-muted);
}

/* --- Wallet Card --- */
.wallet-card {
  background: var(--dark-wallet-bg);
  border-radius: var(--rad-lg);
  padding: 32px;
  color: #ffffff;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  box-shadow: 0 16px 32px -12px rgba(30, 27, 75, 0.4);
  position: relative;
  overflow: hidden;
  cursor: pointer;
}

.wallet-card::after {
  content: '';
  position: absolute;
  top: -50%;
  right: -20%;
  width: 200px;
  height: 200px;
  background: rgba(255,255,255,0.05);
  filter: blur(40px);
  border-radius: 50%;
}

.w-label {
  font-size: 13px;
  color: rgba(255,255,255,0.6);
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.w-amount {
  font-size: 38px;
  font-weight: 700;
  margin-top: 8px;
  letter-spacing: -1px;
}

.btn-wallet-action {
  background: rgba(255,255,255,0.1);
  border: 1px solid rgba(255,255,255,0.2);
  color: #ffffff;
  padding: 12px;
  border-radius: var(--rad-md);
  font-size: 14px;
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  cursor: pointer;
  transition: var(--transition);
  margin-top: 20px;
  width: 100%;
}

.btn-wallet-action:hover {
  background: rgba(255,255,255,0.18);
}

/* --- Shift Action Bar --- */
.shift-action-bar {
  background: var(--surface-card);
  border-radius: var(--rad-md);
  padding: 20px 24px;
  box-shadow: var(--shadow-float);
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-left: 6px solid var(--success-main);
  gap: 16px;
  flex-wrap: wrap;
}

.shift-info {
  display: flex;
  align-items: center;
  gap: 16px;
}

.shift-icon {
  width: 48px;
  height: 48px;
  border-radius: 12px;
  background: var(--success-bg);
  color: var(--success-main);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 24px;
}

.shift-text {
  display: flex;
  flex-direction: column;
}

.shift-title {
  font-size: 16px;
  font-weight: 700;
  color: var(--text-title);
}

.shift-timer {
  font-family: 'JetBrains Mono', monospace;
  font-size: 13px;
  color: var(--text-muted);
  margin-top: 2px;
}

.shift-controls {
  display: flex;
  align-items: center;
  gap: 12px;
}

.start-shift-group {
  display: flex;
  align-items: center;
  gap: 8px;
}

.shift-select {
  padding: 12px;
  border-radius: 12px;
  border: 1px solid rgba(0,0,0,0.1);
  background: #ffffff;
  font-family: inherit;
  font-size: 14px;
  font-weight: 600;
}

.btn-action-primary {
  background: var(--accent-main);
  color: white;
  border: none;
  padding: 12px 20px;
  border-radius: 12px;
  font-weight: 600;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  transition: var(--transition);
}

.btn-action-primary:hover {
  background: #4f46e5;
}

.btn-end-shift {
  background: rgba(239, 68, 68, 0.1);
  color: #ef4444;
  border: none;
  padding: 12px 20px;
  border-radius: 12px;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  transition: var(--transition);
}

.btn-end-shift:hover {
  background: #ef4444;
  color: white;
}

.btn-map-trigger {
  background: #ffffff;
  border: 1px solid rgba(0,0,0,0.1);
  color: var(--text-title);
  padding: 12px 18px;
  border-radius: 12px;
  font-weight: 600;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  transition: var(--transition);
}

.btn-map-trigger:hover {
  border-color: var(--accent-main);
  color: var(--accent-main);
}

/* --- Section Container --- */
.section-container {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.section-title-row {
  display: flex;
  align-items: center;
  gap: 12px;
}

.section-title-row h3 {
  font-size: 18px;
  font-weight: 700;
  color: var(--text-title);
  margin: 0;
}

.count-badge {
  font-size: 18px;
  font-weight: 700;
  color: var(--text-muted);
}

.btn-icon-refresh {
  width: 32px;
  height: 32px;
  border-radius: 8px;
  background: #ffffff;
  border: 1px solid rgba(0,0,0,0.05);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted);
  cursor: pointer;
  box-shadow: 0 2px 4px rgba(0,0,0,0.02);
  transition: var(--transition);
}

.btn-icon-refresh:hover {
  color: var(--text-title);
  border-color: rgba(0,0,0,0.1);
}

.empty-state-card {
  background: rgba(255,255,255,0.5);
  border: 1px solid rgba(0,0,0,0.04);
  border-radius: var(--rad-md);
  padding: 32px;
  text-align: center;
  color: var(--text-muted);
  font-size: 14px;
}

/* --- Orders Stack --- */
.orders-stack {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.order-row {
  background: var(--surface-card);
  border-radius: var(--rad-md);
  box-shadow: var(--shadow-float);
  overflow: hidden;
  transition: var(--transition);
}

.order-summary {
  padding: 20px 24px;
  display: flex;
  align-items: center;
  gap: 16px;
  cursor: pointer;
}

.o-icon {
  width: 48px;
  height: 48px;
  border-radius: 14px;
  background: var(--accent-light);
  color: var(--accent-main);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 24px;
  flex-shrink: 0;
}

.o-icon.history {
  background: #f1f5f9;
  color: var(--text-muted);
}

.o-info {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.o-title {
  font-size: 16px;
  font-weight: 700;
  color: var(--text-title);
}

.o-title.gray {
  color: var(--text-muted);
}

.o-subtitle {
  font-family: 'JetBrains Mono', monospace;
  font-size: 13px;
  color: var(--text-muted);
}

.o-price {
  font-size: 16px;
  font-weight: 700;
  color: var(--text-title);
}

.o-price.gray {
  color: var(--text-muted);
  font-weight: 500;
}

.badge-status {
  padding: 4px 10px;
  border-radius: 99px;
  font-size: 12px;
  font-weight: 600;
  color: white;
}

.status-dot-gray {
  font-size: 13px;
  color: var(--text-muted);
}

.o-actions {
  display: flex;
  gap: 8px;
}

.btn-chat-toggle {
  width: 40px;
  height: 40px;
  border-radius: 12px;
  background: #f8fafc;
  border: none;
  color: var(--text-muted);
  font-size: 18px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: var(--transition);
}

.btn-chat-toggle:hover, .btn-chat-toggle.active {
  background: var(--accent-main);
  color: white;
}

.btn-action-accept {
  background: #ecfdf5;
  color: #10b981;
  border: 1px solid #a7f3d0;
  padding: 8px 16px;
  border-radius: 12px;
  font-weight: 600;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 6px;
  transition: var(--transition);
}

.btn-action-accept:hover {
  background: #10b981;
  color: white;
}

.btn-action-execute {
  background: linear-gradient(135deg, #10b981 0%, #059669 100%);
  color: #ffffff;
  border: none;
  padding: 8px 16px;
  border-radius: 12px;
  font-size: 13px;
  font-weight: 700;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 6px;
  box-shadow: 0 4px 12px rgba(16, 185, 129, 0.25);
  transition: all 0.2s ease;
}

.btn-action-execute:hover {
  background: linear-gradient(135deg, #059669 0%, #047857 100%);
  transform: translateY(-1px);
  box-shadow: 0 6px 16px rgba(16, 185, 129, 0.35);
}

/* --- GPS Bar --- */
.gps-card-bar {
  background: var(--surface-card);
  border-radius: var(--rad-md);
  padding: 16px 24px;
  box-shadow: var(--shadow-float);
  display: flex;
  align-items: center;
  gap: 16px;
}

.gps-icon {
  width: 44px;
  height: 44px;
  border-radius: 12px;
  background: #f1f5f9;
  color: var(--text-muted);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
}

.gps-info {
  flex: 1;
  display: flex;
  flex-direction: column;
}

.gps-label {
  font-size: 12px;
  color: var(--text-muted);
}

.gps-input-value {
  font-family: 'JetBrains Mono', monospace;
  font-size: 15px;
  font-weight: 600;
  color: var(--text-title);
  border: none;
  outline: none;
  background: transparent;
}

.btn-edit-gps {
  width: 36px;
  height: 36px;
  border-radius: 10px;
  background: #f8fafc;
  border: none;
  color: var(--text-muted);
  cursor: pointer;
}

/* --- Inline Chat Accordion --- */
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

.chat-empty {
  text-align: center;
  color: var(--text-muted);
  font-size: 13px;
  padding: 12px 0;
}

.msg {
  display: flex;
  flex-direction: column;
  max-width: 80%;
}

.msg.incoming { align-self: flex-start; }
.msg.outgoing { align-self: flex-end; }

.bubble {
  padding: 10px 14px;
  font-size: 14px;
  border-radius: 16px;
  line-height: 1.4;
}

.msg.incoming .bubble {
  background: #ffffff;
  border: 1px solid rgba(0,0,0,0.05);
  color: var(--text-title);
  border-bottom-left-radius: 4px;
}

.msg.outgoing .bubble {
  background: var(--accent-main);
  color: white;
  border-bottom-right-radius: 4px;
}

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
}

.chat-img-wrapper {
  max-width: 200px;
  border-radius: 12px;
  overflow: hidden;
  cursor: pointer;
}

.chat-img {
  width: 100%;
  max-height: 160px;
  object-fit: cover;
  display: block;
}

/* Image Preview Modal */
.img-preview-overlay {
  position: fixed;
  top: 0; left: 0; right: 0; bottom: 0;
  background: rgba(15, 23, 42, 0.85);
  backdrop-filter: blur(12px);
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
}

.img-preview-full {
  max-width: 100%;
  max-height: 85vh;
  border-radius: 16px;
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

@media (max-width: 768px) {
  .msg-actions { opacity: 1; transform: translateX(0); }
  .action-icon-btn { width: 28px; height: 28px; font-size: 14px; }
  .msg-image { max-width: 200px; }
  .premium-grid { grid-template-columns: 1fr; }
  .profile-row { flex-direction: column; text-align: center; }
  .profile-info { align-items: center; }
  .shift-action-bar { flex-direction: column; align-items: stretch; text-align: center; }
  .shift-info { flex-direction: column; }
  .shift-controls { flex-direction: column; width: 100%; }
  .start-shift-group { width: 100%; }
  .btn-action-primary, .btn-end-shift, .btn-map-trigger { width: 100%; justify-content: center; }
}

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

.btn-action {
  width: 36px;
  height: 36px;
  border-radius: 10px;
  border: none;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  cursor: pointer;
}
.btn-action.primary { background: #e0e7ff; color: #5c60f5; }
.btn-action.success { background: #ecfdf5; color: #10b981; }

/* Modifiers for Review & History */
.list-item-compact.review { border-left: 4px solid #f59e0b; }
.list-item-compact.review .item-icon { background: #fffbeb; color: #f59e0b; }
.list-item-compact.review .item-price-top { color: #f59e0b; }
.list-item-compact.review .item-subtitle { font-size: 11px; color: #f59e0b; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

.gps-input { flex: 1; border: none; outline: none; font-family: 'JetBrains Mono', monospace; font-size: 14px; color: var(--text-title, #0f172a); background: transparent; width: 100%; }

.empty-state {
  background: rgba(255,255,255,0.5); border: 1px dashed rgba(0,0,0,0.1); border-radius: var(--rad-md, 16px);
  padding: 16px; text-align: center; font-size: 13px; color: var(--text-muted, #64748b); font-weight: 500;
}

.history-item { opacity: 0.85; box-shadow: none; border: 1px solid rgba(0,0,0,0.03); margin-bottom: 8px; padding: 10px 16px; }
.history-item .item-icon { background: #f1f5f9; color: var(--text-muted, #64748b); width: 32px; height: 32px; font-size: 16px; border-radius: 10px; }
.history-status { font-size: 12px; font-weight: 500; color: var(--text-muted, #64748b); text-align: right; }
.history-price { font-size: 14px; font-weight: 700; color: var(--text-title, #0f172a); text-align: right; margin-bottom: 2px; }

@media (max-width: 600px) {
  .list-item-compact {
    flex-wrap: nowrap;
    padding: 10px 14px;
  }
}
</style>
