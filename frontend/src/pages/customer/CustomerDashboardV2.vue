<template>
  <div class="premium-dashboard-page">
    <div class="container">
      <!-- Шапка -->
      <header class="header">
        <div class="logo">
          <AppLogo />
        </div>
        <!-- Кнопка вызова меню -->
        <button type="button" class="hamburger-btn position-relative" title="Меню" @click="menuOpen = true">
          <i class="ph-bold ph-list"></i>
          <span v-if="hasUnreadSupport" class="support-unread-dot"></span>
        </button>
      </header>

      <!-- --- Сайдбар (выдвижное меню) --- -->
      <div :class="['sidebar-overlay', { open: menuOpen }]" @click="menuOpen = false"></div>

      <aside :class="['sidebar', { open: menuOpen }]">
        <!-- Выход стоит наверху рядом с заголовком, как в админке: до него
             не нужно прокручивать меню. -->
        <div class="sidebar-header">
          <h2>Меню</h2>
          <div class="sidebar-header-actions">
            <button
              type="button"
              class="header-logout"
              title="Выйти из аккаунта"
              aria-label="Выйти из аккаунта"
              @click="menuOpen = false; handleLogout()"
            >
              <i class="ph-bold ph-sign-out"></i>
            </button>
            <button type="button" class="close-btn" title="Закрыть" @click="menuOpen = false">
              <i class="ph-bold ph-x"></i>
            </button>
          </div>
        </div>

        <nav class="sidebar-nav">
          <div class="nav-section">Аккаунт</div>
          <div class="nav-list">
            <button type="button" class="nav-item" @click="menuOpen = false; $router.push('/customer/profile')">
              <i class="ph-fill ph-user-circle"></i> Профиль и адреса
            </button>
          </div>

          <div class="nav-section">Помощь</div>
          <div class="nav-list">
            <button type="button" class="nav-item position-relative" @click="menuOpen = false; openSupportChat()">
              <i class="ph-fill ph-headset"></i> Поддержка
              <span v-if="hasUnreadSupport" class="support-unread-dot nav-dot"></span>
            </button>
          </div>

          <div class="sidebar-footer">
            <div v-if="authStore.switchableRoles.length > 1" class="lang-control">
              <span>Роль</span>
              <RoleSwitcher />
            </div>

            <div class="lang-control">
              <span>Язык приложения</span>
              <LanguageSwitcher />
            </div>
          </div>
        </nav>
      </aside>

      <!-- Сетка Профиль + Кошелек -->
      <div class="premium-grid">
        <!-- Компактный профиль -->
        <div class="profile-card cursor-pointer" @click="$router.push('/customer/profile')">
          <div class="avatar-wrap">
            <div class="avatar"><i class="ph ph-user"></i></div>
            <div class="status-dot"></div>
          </div>
          <div class="profile-info">
            <!-- Телефон + Галочка -->
            <div class="profile-phone-row">
              <div class="profile-phone">{{ formattedPhone || '7 920 705 07 07' }}</div>
              <div v-if="isVerified" class="verified-badge" title="Верифицирован">
                <i class="ph-fill ph-check-circle"></i>
              </div>
            </div>
            <div v-if="fullName" class="profile-fullname">{{ fullName }}</div>
            <div class="badge-brand" @click.stop="$router.push('/customer/profile')">
              <i class="ph-fill ph-map-pin"></i> Мои адреса
            </div>
          </div>
        </div>

        <!-- Компактный баланс -->
        <div class="balance-card">
          <div class="bc-label">Доступный баланс</div>
          <div class="balance-bottom-row">
            <div class="bc-value">
              <template v-if="balanceLoaded">
                {{ Number(balance).toLocaleString('ru-RU', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) }}
              </template>
              <span v-else class="bc-value-placeholder">—</span>
              <span class="bc-currency">{{ currencySymbol }}</span>
            </div>
            <button type="button" class="btn-balance" @click="showTopUpModal = true">
              <i class="ph-bold ph-plus"></i> Пополнить
            </button>
          </div>
        </div>
      </div>

      <!-- Контейнер всплывающих уведомлений -->
      <div class="toast-container">
        <!-- Всплывающее уведомление чата / поддержки -->
        <div
          v-if="chatToast"
          class="toast info chat-toast cursor-pointer"
          @click="openChatByToast"
        >
          <div class="toast-icon">
            <i class="ph-bold ph-chat-circle-dots"></i>
          </div>
          <div class="toast-content">
            <div class="toast-title">{{ chatToast.title }}</div>
            <div class="toast-message">{{ chatToast.text }}</div>
          </div>
          <button type="button" class="toast-close" @click.stop.prevent="closeToast">
            <i class="ph ph-x"></i>
          </button>
        </div>

        <!-- Уведомление об успехе -->
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

        <!-- Уведомление об ошибке -->
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
          <h2 class="section-title m-0">
            Активные заказы <span v-if="activeOrders.length" class="text-muted text-sm">({{ activeOrders.length }})</span>
            <RefreshingBadge :active="ordersRefreshing && !ordersLoading" />
          </h2>
        </div>

        <SkeletonList v-if="ordersLoading" :rows="2" :lines="3" />
        <div v-else-if="activeOrders.length === 0" class="empty-orders-state">
          <p>{{ $t('customer.noActiveOrders') }}</p>
        </div>

        <div v-else class="orders-stack">
          <div
            v-for="order in activeOrders"
            :key="order.id"
            :class="['order-row', { 'chat-open': openChatOrderId === order.id }]"
          >
            <!-- Ультракомпактная строка сводки -->
            <div class="order-summary list-item-compact cursor-pointer" @click="openOrderDetails(order)">
              <div class="item-left-group">
                <div :class="['o-icon item-icon', order.is_urgent ? 'orange' : 'purple']">
                  <i :class="['ph-fill', order.is_urgent ? 'ph-rocket-launch' : 'ph-package']"></i>
                </div>
                <div class="o-info item-text-stack">
                  <div class="item-price-top">{{ Number(order.hold_amount).toFixed(2) }} {{ currencySymbol }}</div>
                  <div class="o-title item-title order-title-stack">
                    <span class="order-title-main">{{ getOrderTitles(order).title }}</span>
                    <span v-if="getOrderTitles(order).subtitle" class="order-title-sub">{{ getOrderTitles(order).subtitle }}</span>
                  </div>
                  <div v-if="order.address" class="item-subtitle"><i class="ph-fill ph-map-pin me-1"></i>{{ order.address }}</div>
                </div>
              </div>
              <div class="o-actions item-actions" @click.stop>
                <button
                  v-if="order.status === 'ASSIGNED' || order.status === 'EXECUTED'"
                  type="button"
                  :class="['btn-action primary chat-btn position-relative', { active: openChatOrderId === order.id }]"
                  title="Чат"
                  @click="toggleChat(order)"
                >
                  <i class="ph-fill ph-chat-circle-dots"></i>
                  <span v-if="unreadOrderIDs.has(order.id)" class="yellow-unread-dot"></span>
                </button>
                <button
                  v-if="order.status === 'EXECUTED' || order.status === 'ASSIGNED'"
                  type="button"
                  class="btn-action success confirm-btn"
                  :title="order.status === 'EXECUTED' ? 'Подтвердить выполнение и закрыть заказ' : 'Принять заказ досрочно и закрыть его'"
                  @click="confirmOrder(order.id, order.status)"
                >
                  <i class="ph-bold ph-check"></i>
                </button>
              </div>
            </div>

            <!-- Встроенная область чата-аккордеона -->
            <div v-if="openChatOrderId === order.id" class="inline-chat">
              <!-- Скрытый файловый input для вложений-изображений -->
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
                  <!-- Действия (правка/удаление только для исходящих сообщений пользователя, скрыты для системных) -->
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
                      <!-- Вложение-изображение -->
                      <div
                        v-if="isImageAttachment(msg)"
                        :class="['chat-img-wrapper mb-1', { 'img-pending': isImagePending(msg) }]"
                      >
                        <img
                          :src="getImageSrc(msg)"
                          alt="Фото"
                          class="msg-image"
                          @load="markImageRendered(msg)"
                          @error="onChatImgError(msg.file_url || msg.content)"
                          @click="openImagePreview(getImageSrc(msg))"
                        />
                      </div>
                      <span v-if="msg.text || msg.content" class="msg-text">{{ msg.text || msg.content }}</span>
                    </div>

                    <div class="msg-meta">
                      <span>{{ formatMessageTime(msg.created_at) }}</span>
                      <span v-if="msg.updated_at" class="msg-edited">(изменено)</span>
                      <span v-if="msg.sender_id === currentUserId" class="msg-status-icon" :title="getMessageStatusTitle(msg.status)">
                        <i v-if="msg.status === 'read'" class="ph-bold ph-checks read-receipt"></i>
                        <i v-else-if="msg.status === 'delivered'" class="ph-bold ph-checks delivered-receipt"></i>
                        <i v-else class="ph-bold ph-check sent-receipt"></i>
                      </span>
                    </div>
                  </div>
                </div>
              </div>

              <!-- Область ввода чата с индикатором редактирования -->
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
          <SkeletonList v-if="ordersLoading" :rows="3" />
          <div v-else-if="historyOrders.length === 0" class="empty-orders-state">
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
              <div class="op-title order-title-stack" style="color: var(--text-muted);">
                <span class="order-title-main">{{ getOrderTitles(order).title }}</span>
                <span v-if="getOrderTitles(order).subtitle" class="order-title-sub">{{ getOrderTitles(order).subtitle }}</span>
              </div>
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

      <!-- Модальное окно деталей заказа -->
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

      <!-- Модальное окно отзыва -->
      <ReviewModal
        v-model="showReviewModal"
        :order-id="reviewTargetOrderId"
        role="CUSTOMER"
        :order-amount="reviewTargetOrderAmount"
        :balance="balance ?? 0"
        :currency-symbol="currencySymbol"
        @reviewed="onReviewSubmitted"
      />

      <!-- Модальное окно создания заказа -->
      <CreateOrderModal
        v-model="showCreateOrderModal"
        v-model:is-urgent="isUrgent"
        v-model:order-comment="orderComment"
        :order-address="orderAddress"
        :order-lat="orderLat"
        :order-lon="orderLon"
        :selected-variant-id="selectedVariantId"
        :catalog-items="catalogItemOptions"
        :catalog-path="catalogPathOptions"
        :catalog-loading="catalogLoading"
        :is-auction-selected="isAuctionSelected"
        :selected-price="selectedPrice"
        :currency-symbol="currencySymbol"
        :creating-order="creatingOrder"
        @open-node="openCatalogNode"
        @go-level="goToCatalogLevel"
        @submit-order="submitOrder"
      />

      <!-- Модальное окно пополнения -->
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

      <!-- Модальное окно профиля заказчика -->
      <CustomerProfileModal
        v-model="showProfileModal"
        v-model:new-address="newAddress"
        :address-error="addressError"
        :is-verified="false"
        :user-email="userEmail"
        :customer-addresses="customerAddresses"
        :default-address="defaultAddress"
        :editing-address-id="editingAddressId"
        @set-active-address="setActiveAddress"
        @add-new-address="addNewAddress"
        @remove-address="removeAddress"
        @edit-address="startEditAddress"
        @cancel-edit-address="cancelEditAddress"
      />

      <!-- Модальное окно просмотра изображения -->
      <div v-if="showImagePreviewModal" class="img-preview-overlay" @click.self="showImagePreviewModal = false">
        <div class="img-preview-card">
          <button type="button" class="btn-close-preview" aria-label="Закрыть" @click="showImagePreviewModal = false">
            <i class="ph ph-x"></i>
          </button>
          <img :src="previewImageUrl" alt="Предпросмотр" class="img-preview-full" />
        </div>
      </div>
      <!-- Модальное окно поддержки -->
      <SupportChatModal v-model:show="showSupportChatModal" />
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useScrollLock } from '../../composables/useScrollLock'
import { useChatSocket } from '../../composables/useChatSocket'
import type { StructuredAddress } from '../../components/AddressAutocomplete.vue'
import { useRouter } from 'vue-router'
import { Capacitor } from '@capacitor/core'
import { Camera, CameraResultType, CameraSource } from '@capacitor/camera'
import { cameraPromptLabels } from '../../utils/cameraLabels'
import { orderTitle, orderTitleLine } from '../../utils/orderTitle'
import { useAuthStore } from '../../stores/auth-store'
import UpdateBanner from '../../components/UpdateBanner.vue'
import LanguageSwitcher from '../../components/LanguageSwitcher.vue'
import RoleSwitcher from '../../components/RoleSwitcher.vue'
import AppLogo from '../../components/AppLogo.vue'
import OrderDetailsModal from './components/OrderDetailsModal.vue'
import CreateOrderModal from './components/CreateOrderModal.vue'
import CustomerProfileModal from './components/CustomerProfileModal.vue'
import ReviewModal from './components/ReviewModal.vue'
import SupportChatModal from '../../components/SupportChatModal.vue'
import SkeletonList from '../../components/SkeletonList.vue'
import RefreshingBadge from '../../components/RefreshingBadge.vue'
import api, { pollIntervalMs } from '../../services/api'
import { useCachedResource } from '../../composables/useCachedResource'
import { loadByPriority } from '../../utils/loadPriority'
import {
  orderImageSrc,
  cacheOrderImage,
  rememberOrderImages,
  releaseClosedOrderImages,
  preloadOrderImages,
  markImageRendered,
  isImagePending,
} from '../../services/orderImages'
import { checkMyOrderReview, type OrderReview } from '../../api/review'
import { compressImage } from '../../utils/imageCompressor'
import { getServiceCategories, getServiceCategoryChildren, type ServiceNode } from '../../api/services'

export default defineComponent({
  name: 'CustomerDashboardV2',
  components: {
    SkeletonList,
    RefreshingBadge,
    UpdateBanner,
    LanguageSwitcher,
    RoleSwitcher,
    AppLogo,
    OrderDetailsModal,
    CreateOrderModal,
    CustomerProfileModal,
    ReviewModal,
    SupportChatModal,
  },
  setup() {
    const router = useRouter()
    const authStore = useAuthStore()

    const phone = ref('79207050707')
    // Читает напрямую из хранилища авторизации: ни локальной копии, ни
    // подставной суммы вместо настоящего баланса, пока грузится профиль.
    const balance = computed(() => authStore.balance)
    const balanceLoaded = computed(() => authStore.balance !== null)
    // Шаблон всегда рисовал бейдж «верифицирован» по этому значению, а оно нигде не
    // было определено: бейдж не мог появиться ни у кого.
    const isVerified = computed(() => authStore.user?.is_verified ?? false)
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

    // Адреса
    const defaultAddress = ref('Москва, ул. Тверская, д. 1')
    const customerAddresses = ref<any[]>([
      { address: 'Москва, ул. Тверская, д. 1' }
    ])
    const newAddress = ref<StructuredAddress | null>(null)
    const addressSaving = ref(false)
    // Id сохранённого адреса, который сейчас редактируют (null — добавляется новый).
    const editingAddressId = ref<string | null>(null)
    const addressError = ref('')

    // Заказы.
    //
    // Список держится кэшируемым ресурсом: экран, открытый повторно, рисуется
    // последним известным списком сразу, а сеть уточняет его следом.
    // `ordersLoading` означает «показывать нечего» — только тогда на месте
    // списка стоит прелоадер; `ordersRefreshing` — тихая догрузка поверх уже
    // показанных заказов.
    // Отзывы к завершённым заказам — по запросу на заказ ради значка с оценкой
    // в истории. На первой загрузке экрана их откладывает очередь приоритетов
    // (история идёт последней); дальше они обновляются вместе со списком, а
    // уже полученные не перезапрашиваются.
    let historyReviewsDeferred = true

    // Предзагрузка фотографий откладывается до своей ступени в очереди
    // приоритетов: иначе она стартовала бы вместе с первым же списком заказов и
    // отбирала соединение у того, что пользователь видит на экране. Дальше она
    // идёт вместе с каждым обновлением списка — прогревает только новые заказы
    // и освобождает закрывшиеся.
    let imagePreloadDeferred = true

    const ordersResource = useCachedResource<any[]>({
      key: 'customer:orders',
      initial: [],
      fetcher: async () => (await api.get('/customer/orders')).data || [],
      onData: (orders) => {
        fetchUnreadSummary()
        checkSupportNotification()
        // Картинки живут ровно столько, сколько открыт заказ: подтверждённый
        // или отменённый уходит в историю, и держать его фотографии незачем.
        if (imagePreloadDeferred) releaseClosedOrderImages(orders)
        else void preloadOrderImages(orders)
        if (!historyReviewsDeferred) fetchReviewsForHistory()
      },
    })

    const loadHistoryReviews = () => {
      historyReviewsDeferred = false
      return fetchReviewsForHistory()
    }

    const startImagePreload = () => {
      imagePreloadDeferred = false
      return preloadOrderImages(orders.value)
    }
    const orders = ordersResource.data
    const ordersLoading = ordersResource.loading
    const ordersRefreshing = ordersResource.refreshing
    const isHistoryCollapsed = ref(false)
    const orderReviewsMap = ref<Record<string, OrderReview>>({})

    // Модальные окна
    const showCreateOrderModal = ref(false)
    const showOrderDetailsModal = ref(false)
    const showTopUpModal = ref(false)
    const showProfileModal = ref(false)
    const showSupportChatModal = ref(false)
    const menuOpen = ref(false)

    useScrollLock(() => (
      showCreateOrderModal.value ||
      showOrderDetailsModal.value ||
      showTopUpModal.value
    ))
    const selectedOrderDetails = ref<any>(null)
    const topUpAmount = ref<number>(100)
    const submitting = ref(false)
    const creatingOrder = ref(false)

    // Каталог и создание заказа
    const serviceCategories = ref<ServiceNode[]>([])
    // Каталог — дерево произвольной глубины, где категория и услуга могут стоять
    // рядом, поэтому выбор идёт по одному уровню за раз, а не предполагает цепочку
    // категория -> подкатегория -> услуга. catalogItems держит уровень, который
    // сейчас на экране, catalogPath — категории, открытые, чтобы до него дойти.
    const catalogItems = ref<ServiceNode[]>([])
    const catalogPath = ref<ServiceNode[]>([])
    const catalogLoading = ref(false)
    const selectedVariantId = ref<string | null>(null)
    const isUrgent = ref(false)
    const isAsap = ref(false)
    const orderComment = ref('')
    const orderAddress = ref(defaultAddress.value)
    const orderLat = ref<number | null>(null)
    const orderLon = ref<number | null>(null)

    const activeOrders = computed(() => {
      return orders.value.filter((o) => ['SEARCHING', 'ASSIGNED', 'EXECUTED'].includes(o.status))
    })

    const historyOrders = computed(() => {
      return orders.value
        .filter((o) => ['COMPLETED', 'CANCELED'].includes(o.status))
        .sort((a, b) => {
          const dateA = new Date(a.completed_at || a.canceled_at || a.created_at).getTime()
          const dateB = new Date(b.completed_at || b.canceled_at || b.created_at).getTime()
          return dateB - dateA
        })
    })

    const selectedVariant = computed(() =>
      catalogItems.value.find((v) => v.id === selectedVariantId.value)
    )

    const isAuctionSelected = computed(() => !!selectedVariant.value?.is_auction)

    const localizedName = (node?: ServiceNode) => {
      if (!node) return ''
      return node.name['ru'] || node.name['en'] || node.code || ''
    }

    const localizedDescription = (node?: ServiceNode) => {
      if (!node || !node.description) return ''
      return node.description['ru'] || node.description['en'] || ''
    }

    const catalogItemOptions = computed(() =>
      catalogItems.value.map((node) => ({
        id: node.id,
        label: localizedName(node),
        description: localizedDescription(node),
        node_type: node.node_type,
        base_price: node.base_price,
        is_auction: node.is_auction,
      }))
    )

    const catalogPathOptions = computed(() =>
      catalogPath.value.map((node) => ({ id: node.id, label: localizedName(node) }))
    )

    // Загружает потомков категории или корневой уровень, когда id равен null.
    const loadCatalogLevel = async (id: string | null) => {
      catalogLoading.value = true
      try {
        catalogItems.value = id ? await getServiceCategoryChildren(id) : await getServiceCategories()
      } catch (err) {
        console.error('Failed to load catalog level:', err)
        catalogItems.value = []
      } finally {
        catalogLoading.value = false
      }
    }

    // Категория опускает на уровень ниже; услуга просто выбирается.
    const openCatalogNode = async (item: { id: string; node_type: string }) => {
      if (item.node_type === 'VARIANT') {
        selectedVariantId.value = item.id
        return
      }
      const node = catalogItems.value.find((n) => n.id === item.id)
      if (!node) return
      selectedVariantId.value = null
      catalogPath.value = [...catalogPath.value, node]
      await loadCatalogLevel(node.id)
    }

    // index -1 — корневой уровень; всё остальное сохраняет хлебные крошки до него.
    const goToCatalogLevel = async (index: number) => {
      selectedVariantId.value = null
      if (index < 0) {
        catalogPath.value = []
        await loadCatalogLevel(null)
        return
      }
      catalogPath.value = catalogPath.value.slice(0, index + 1)
      const current = catalogPath.value[catalogPath.value.length - 1]
      await loadCatalogLevel(current ? current.id : null)
    }

    const selectedPrice = computed(() => {
      const variant = selectedVariant.value
      if (!variant || variant.base_price === undefined) return 0
      let price = variant.base_price
      if (isAuctionSelected.value) return 0
      if (isAsap.value) price *= 8
      else if (isUrgent.value) price *= 3
      return price
    })

    const userEmail = ref('')
    const fullName = ref('')

    // Профиль заказчика тоже кэшируется: до его загрузки экран показывает
    // адрес-заглушку, и на повторном открытии её не должно быть видно вовсе.
    const applyProfile = (data: any) => {
      if (!data) return
      if (data.phone) phone.value = data.phone
      // Профиль возвращает сохранённые адреса с координатами, с которыми их
      // выбирали (от адресного провайдера). Держим объекты целиком, чтобы заказ
      // отправлял сохранённые lat/lon; пересборка их здесь как { address }
      // теряла координаты и порождала заказы без
      // pickup_lat/pickup_lon.
      const addrs = Array.isArray(data.addresses) ? data.addresses : []
      if (addrs.length) {
        customerAddresses.value = addrs
        const def = addrs.find((a: any) => a.is_default) || addrs[0]
        defaultAddress.value = def.address
        orderAddress.value = def.address
      } else if (data.address) {
        defaultAddress.value = data.address
        orderAddress.value = data.address
        customerAddresses.value = [{ address: data.address }]
      }
    }

    const profileResource = useCachedResource<any>({
      key: 'customer:profile',
      initial: null,
      fetcher: async () => (await api.get('/customer/profile')).data,
      onData: (data) => applyProfile(data),
    })

    /**
     * `force` разделяет два повода перечитать профиль. После действия
     * пользователя (создан заказ, оставлены чаевые) нужен именно новый запрос:
     * ответ, отправленный до действия, о нём не знает. Опрос по таймеру и
     * первая загрузка экрана переиспользуют уже идущий запрос.
     */
    const fetchProfile = async (force = true) => {
      try {
        const meRes = await api.get('/auth/me')
        if (meRes.data) {
          if (meRes.data.email) userEmail.value = meRes.data.email
          const parts = [meRes.data.last_name, meRes.data.first_name, meRes.data.patronymic].filter((p: string) => p && p.trim())
          fullName.value = parts.join(' ')
        }
        // Держим общее состояние пользователя в такт с тем, что экран только что прочитал.
        authStore.fetchMe()
        await (force ? profileResource.reload() : profileResource.refresh())
      } catch (err) {
        console.error('Failed to load profile:', err)
      }
    }

    // Координаты для заказа берутся из сохранённого адреса, который выбрал
    // заказчик: DaData возвращает их вместе с подсказкой, и они хранятся при адресе
    // в профиле. Легаси-адрес без них оставляет пару null — тогда бэкенд разрешает
    // координаты по строке адреса при создании заказа, поэтому клиент не делает
    // отдельного похода за геокодированием.
    const applyStoredCoordinates = () => {
      const saved = customerAddresses.value.find((a: any) => a.address === orderAddress.value)
      orderLat.value = saved && saved.lat != null ? saved.lat : null
      orderLon.value = saved && saved.lon != null ? saved.lon : null
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
            // игнорируем
          }
        }
      }
    }

    // Обновление после действия пользователя: `reload` не присоединяется к
    // запросу, отправленному ДО действия, — тот ответ о нём не знает и вернул бы
    // список до нажатия. Опрос по таймеру зовёт `refresh`, а первую загрузку
    // экрана делает `ordersResource.load()` — с кэша.
    const fetchOrders = () => ordersResource.reload()

    const openCreateOrderModal = async () => {
      orderAddress.value = defaultAddress.value
      applyStoredCoordinates()
      selectedVariantId.value = null
      catalogPath.value = []
      catalogItems.value = []
      isUrgent.value = false
      isAsap.value = false
      orderComment.value = ''
      showCreateOrderModal.value = true
      await loadCatalogLevel(null)
      // Корневой уровень заодно служит справочником, по которому история заказов
      // разрешает родительскую категорию варианта.
      serviceCategories.value = catalogItems.value.filter((n) => n.node_type === 'CATEGORY')
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
          comment: orderComment.value,
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

    const confirmOrder = async (orderId: string, status?: string) => {
      // Подтверждение заказа, который исполнитель ещё не отметил выполненным,
      // закрывает его и сразу выплачивает, поэтому страхуем случай раннего одобрения.
      if (status === 'ASSIGNED' &&
          !confirm('Исполнитель ещё не отметил заказ выполненным. Подтвердить и закрыть заказ досрочно? Средства спишутся исполнителю.')) {
        return
      }
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
    // Оплаченная сумма заказа питает пресеты чаевых 5% / 10% в модалке.
    const reviewTargetOrderAmount = ref(0)

    const openReviewModal = (order: any) => {
      reviewTargetOrderId.value = order.id
      reviewTargetOrderAmount.value = Number(order.final_amount ?? order.hold_amount ?? 0)
      showOrderDetailsModal.value = false
      showReviewModal.value = true
    }

    const onReviewSubmitted = (payload?: { tipped?: boolean }) => {
      successMsg.value = payload?.tipped ? 'Спасибо за отзыв и чаевые!' : 'Спасибо за отзыв!'
      if (reviewTargetOrderId.value) {
        delete orderReviewsMap.value[reviewTargetOrderId.value]
      }
      fetchReviewsForHistory()
      // Чаевые меняют баланс, поэтому обновляем профиль вместе с отзывами.
      if (payload?.tipped) {
        fetchProfile()
      }
    }

    const openOrderDetails = (order: any) => {
      selectedOrderDetails.value = order
      showOrderDetailsModal.value = true
    }

    // Состояние и логика чата
    const openChatOrderId = ref<string | null>(null)
    const chatMessages = ref<any[]>([])
    const chatInputText = ref('')
    const chatContainerRef = ref<any>(null)
    const chatFileInputRef = ref<HTMLInputElement | null>(null)
    const uploadingChatFile = ref(false)
    const showImagePreviewModal = ref(false)
    const previewImageUrl = ref('')
    // Заказ, чей чат открыт: обработчик сообщений сокета переживает переоткрытие
    // соединения, поэтому берёт заказ отсюда, а не из замыкания одной попытки.
    const chatOrder = ref<any>(null)
    const chatSocket = useChatSocket({
      onOpen: ({ orderId, reconnected }) => {
        // Пока связи не было, чужие сообщения шли мимо сокета. Перечитываем
        // историю, иначе в ленте останется дыра ровно за время обрыва.
        if (!reconnected) return
        fetchChatMessages(orderId)
        markChatAsRead(orderId)
      },
      onMessage: (data) => handleChatEvent(data),
    })
    let chatPollTimer: any = null
    const unreadOrderIDs = ref(new Set<string>())
    const chatToast = ref<any>(null)
    let chatToastTimer: any = null

    const closeToast = () => {
      chatToast.value = null
      if (chatToastTimer) clearTimeout(chatToastTimer)
    }

    const setChatToast = (data: any) => {
      chatToast.value = data
      if (chatToastTimer) clearTimeout(chatToastTimer)
      chatToastTimer = setTimeout(() => {
        chatToast.value = null
      }, 5000)
    }

    const fetchUnreadSummary = async () => {
      try {
        const response = await api.get('/chats/unread-summary')
        const ids = response.data?.unread_order_ids || []
        unreadOrderIDs.value = new Set(ids)
      } catch (err) {
        console.warn('[CustomerDashboardV2] failed to fetch unread summary:', err)
      }
    }

    const markChatAsRead = async (orderID: string) => {
      unreadOrderIDs.value.delete(orderID)
      try {
        await api.post(`/chats/${orderID}/read`)
      } catch (err) {
        console.warn('[CustomerDashboardV2] mark read failed:', err)
      }
      chatSocket.send({ type: 'read_ack' })
    }

    const sendDeliveryAck = () => {
      chatSocket.send({ type: 'delivery_ack' })
    }

    const getMessageStatusTitle = (status?: string) => {
      if (status === 'read') return 'Прочитано'
      if (status === 'delivered') return 'Доставлено, но не прочитано'
      return 'Отправлено'
    }

    const hasUnreadSupport = ref(false)
    const lastSupportMsgText = ref('')

    watch(showSupportChatModal, (val) => {
      if (val) hasUnreadSupport.value = false
    })

    const openSupportChat = () => {
      hasUnreadSupport.value = false
      showSupportChatModal.value = true
    }

    const checkSupportNotification = async () => {
      try {
        const res = await api.get('/support/chat')
        if (res.data) {
          const unreadCount = res.data.unread_count || 0
          const lastMsg = res.data.last_message || ''
          if (unreadCount > 0) {
            hasUnreadSupport.value = true
            if (!showSupportChatModal.value && lastMsg && lastMsg !== lastSupportMsgText.value) {
              lastSupportMsgText.value = lastMsg
              setChatToast({
                title: 'Служба поддержки',
                text: lastMsg,
                isSupport: true,
              })
            }
          }
        }
      } catch (err) {}
    }

    const openChatByToast = () => {
      if (!chatToast.value) return
      const isSupp = chatToast.value.isSupport
      const orderObj = chatToast.value.order
      closeToast()
      if (isSupp) {
        openSupportChat()
      } else if (orderObj) {
        toggleChat(orderObj)
      }
    }

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

    // Прогретый блоб, если предзагрузка успела, иначе обычный URL с токеном.
    // Хранилище общее и реактивное: картинка, догревшаяся уже после отрисовки,
    // подменяет источник сама.
    const getImageSrc = (msg: any) => orderImageSrc(msg)

    // Тег <img> не смог загрузить картинку сам — забираем её через fetch и
    // подкладываем блобом. Тот же путь, которым идёт предзагрузка, поэтому
    // добытое остаётся в кэше заказа до его закрытия.
    const onChatImgError = (path?: string) => {
      if (!path) return
      const orderId = openChatOrderId.value
      if (!orderId) {
        markImageRendered(path)
        return
      }
      void cacheOrderImage(orderId, path).then((url) => {
        // Забрать не вышло — гасим заглушку. Вечно мерцающий прямоугольник
        // хуже честно не загрузившейся картинки.
        if (!url) markImageRendered(path)
      })
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
            ...cameraPromptLabels(),
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
      chatOrder.value = null
      chatMessages.value = []
      chatInputText.value = ''
      chatSocket.disconnect()
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

    const fetchChatMessages = async (orderId: string) => {
      try {
        const response = await api.get(`/chats/${orderId}/messages`)
        chatMessages.value = response.data || []
        // Всё, что заказ показал хоть раз, остаётся под рукой до его закрытия:
        // к фотографиям в чате возвращаются, пока заказ в работе.
        rememberOrderImages(orderId, chatMessages.value)
        scrollToChatBottom()
      } catch (err) {
        console.error('Failed to load chat messages:', err)
      }
    }

    const handleChatEvent = (data: any) => {
      const order = chatOrder.value
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
          if (order) rememberOrderImages(order.id, [data])
          scrollToChatBottom()
        }
        if (data.sender_id !== currentUserId.value && order) {
          sendDeliveryAck()
          if (openChatOrderId.value === order.id) {
            markChatAsRead(order.id)
          } else {
            unreadOrderIDs.value.add(order.id)
            setChatToast({
              id: order.id,
              title: 'Новое сообщение',
              text: data.text || 'Получено новое сообщение',
              order,
            })
          }
        }
      }
    }

    const toggleChat = async (order: any) => {
      if (openChatOrderId.value === order.id) {
        closeInlineChat()
        return
      }

      closeInlineChat()
      openChatOrderId.value = order.id
      chatOrder.value = order
      markChatAsRead(order.id)

      // 1. Получаем историю сообщений
      await fetchChatMessages(order.id)

      // 2. Открываем соединение WebSocket. Оно само поднимается после обрывов,
      // поэтому здесь остаётся только сказать, к какому заказу подключаться.
      chatSocket.connect(order.id)
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

      // Всегда шлём через REST — тем же путём, что и (работающий) чат поддержки.
      // ws.send() не даёт подтверждения доставки, а нативный WebView может его
      // проглотить — именно это ломало чат заказа; REST-эндпоинт сохраняет
      // сообщение и рассылает его в комнату, поэтому реалтайм всё равно работает.
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

    const setActiveAddress = async (addr: string) => {
      defaultAddress.value = addr
      orderAddress.value = addr
      try {
        await api.post('/user/address/default', { address: addr })
      } catch (err) {
        console.error('Failed to set the default address:', err)
      }
    }

    const startEditAddress = (addr: any) => {
      editingAddressId.value = addr.id
      addressError.value = ''
      // Заполняем поле сохранёнными частями, чтобы адрес можно было поправить на месте.
      newAddress.value = {
        value: addr.address,
        region: addr.region,
        city: addr.city,
        street: addr.street,
        house: addr.house,
        flat: addr.flat,
        fias_id: addr.fias_id,
        lat: addr.lat,
        lon: addr.lon,
        source: addr.source,
      }
    }

    const cancelEditAddress = () => {
      editingAddressId.value = null
      newAddress.value = null
      addressError.value = ''
    }

    // Раньше это меняло локальный массив и больше ничего, поэтому добавленный здесь
    // адрес пропадал при следующей загрузке, а удалённый здесь возвращался.
    const addNewAddress = async () => {
      const chosen = newAddress.value
      const editingId = editingAddressId.value
      if (!chosen || addressSaving.value) return
      if (!editingId && customerAddresses.value.length >= 2) return

      addressSaving.value = true
      addressError.value = ''
      try {
        const wasDefault = editingId
          ? customerAddresses.value.find((a: any) => a.id === editingId)?.address === defaultAddress.value
          : false

        // Редактирование заменяет сохранённый адрес: сначала убираем старую строку,
        // чтобы новая влезла в лимит двух адресов, затем сохраняем новые части.
        if (editingId) {
          await api.delete(`/user/address/${editingId}`)
        }

        const res = await api.post('/user/address', {
          address: chosen.value,
          region: chosen.region,
          city: chosen.city,
          street: chosen.street,
          house: chosen.house,
          flat: chosen.flat,
          fias_id: chosen.fias_id,
          lat: chosen.lat,
          lon: chosen.lon,
          source: chosen.source,
        })
        customerAddresses.value = res.data.addresses || []
        // Оставляем отредактированный адрес активным, если он был адресом по умолчанию;
        // совершенно новый адрес становится рабочим, как и прежде.
        if (!editingId || wasDefault) {
          defaultAddress.value = chosen.value
          orderAddress.value = chosen.value
          if (editingId && wasDefault) {
            await setActiveAddress(chosen.value)
          }
        }
        newAddress.value = null
        editingAddressId.value = null
      } catch (err: any) {
        addressError.value =
          err?.response?.data?.error || err?.response?.data || 'Не удалось сохранить адрес'
      } finally {
        addressSaving.value = false
      }
    }

    const removeAddress = async (idx: number) => {
      const target = customerAddresses.value[idx]
      if (!target?.id) return
      try {
        const res = await api.delete(`/user/address/${target.id}`)
        customerAddresses.value = res.data.addresses || []
        if (!customerAddresses.value.some((a: any) => a.address === defaultAddress.value)) {
          defaultAddress.value = customerAddresses.value[0]?.address || ''
          orderAddress.value = defaultAddress.value
        }
      } catch (err) {
        console.error('Failed to remove the address:', err)
      }
    }

    // Naming lives in utils/orderTitle so the executor dashboard shows an order
    // the same way this one does.
    const serviceLookup = computed(() => ({ categories: serviceCategories.value }))

    const getOrderTitles = (order: any) => orderTitle(order, serviceLookup.value)

    const formatOrderType = (order: any) => orderTitleLine(order, serviceLookup.value)

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

    watch(selectedVariantId, () => {
      isUrgent.value = false
      isAsap.value = false
    })

    let intervalId: any = null

    onMounted(async () => {
      // Кэш поднимается первым и синхронно — у обоих ресурсов сразу. Приоритет
      // ниже распределяет походы в сеть, а не показ: экран, открытый повторно,
      // рисуется прошлым состоянием целиком, ещё до первого запроса.
      ordersResource.hydrate()
      profileResource.hydrate()

      // Порядок сетевых запросов задаётся смыслом: на мобильной сети они делят
      // одно соединение, и запущенные разом означают, что главное приедет последним.
      await loadByPriority([
        // 1. То, ради чего экран открывают: заказы, профиль (баланс, адрес) и
        //    справочник категорий, по которому заказы получают свои названия.
        [
          ordersResource.refresh,
          () => fetchProfile(false),
          () => getServiceCategories().then((cats) => { serviceCategories.value = cats }),
        ],
        // 2. История — последней: оценки к завершённым заказам стоят по запросу
        //    на каждый заказ и нужны только значку с рейтингом.
        [loadHistoryReviews],
        // 3. Прогрев фотографий активных заказов. Идёт после всего, потому что
        //    это подготовка к будущему нажатию, а не содержимое экрана: чат
        //    открывают ради фотографий, и ждать их в момент открытия не должен
        //    никто. Самая тяжёлая по трафику часть — ей и уступать очередь.
        [startImagePreload],
      ])

      intervalId = setInterval(() => {
        fetchProfile(false)
        ordersResource.refresh()
      }, pollIntervalMs)
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
      authStore,
      userEmail,
      fullName,
      phone,
      formattedPhone,
      balance,
      // Вынесено наружу, чтобы шаблон отличал «ещё не загружено» от настоящего нуля.
      // Без этого v-if читает undefined, и карточка навсегда показывает прочерк.
      balanceLoaded,
      isVerified,
      currencySymbol,
      successMsg,
      errorMsg,
      defaultAddress,
      customerAddresses,
      newAddress,
      addressSaving,
      addressError,
      editingAddressId,
      startEditAddress,
      cancelEditAddress,
      activeOrders,
      historyOrders,
      ordersLoading,
      ordersRefreshing,
      isHistoryCollapsed,
      orderReviewsMap,
      showCreateOrderModal,
      showOrderDetailsModal,
      showTopUpModal,
      showProfileModal,
      showSupportChatModal,
      menuOpen,
      hasUnreadSupport,
      openSupportChat,
      selectedOrderDetails,
      topUpAmount,
      submitting,
      creatingOrder,
      selectedVariantId,
      isUrgent,
      isAsap,
      orderComment,
      orderAddress,
      orderLat,
      orderLon,
      catalogItemOptions,
      catalogPathOptions,
      catalogLoading,
      openCatalogNode,
      goToCatalogLevel,
      isAuctionSelected,
      selectedPrice,

      openChatOrderId,
      chatMessages,
      chatInputText,
      editingMessageId,
      unreadOrderIDs,
      chatToast,
      closeToast,
      fetchUnreadSummary,
      markChatAsRead,
      sendDeliveryAck,
      getMessageStatusTitle,
      openChatByToast,
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
      reviewTargetOrderAmount,
      openReviewModal,
      onReviewSubmitted,
      showImagePreviewModal,
      previewImageUrl,
      currentUserId,
      isImageAttachment,
      getImageSrc,
      markImageRendered,
      isImagePending,
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
      getOrderTitles,
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
  gap: 24px;
}

/* --- Шапка --- */
.header {
  display: flex; justify-content: space-between; align-items: center; margin-bottom: 4px;
}
.logo { display: flex; align-items: center; gap: 8px; font-size: 20px; font-weight: 700; color: var(--text-title, #0f172a); line-height: 1.1; }
.app-logo-icon { width: 28px; height: 28px; border-radius: 8px; object-fit: contain; }
.logo i { color: #5c60f5; font-size: 24px; }
/* Кнопка-гамбургер */
.hamburger-btn {
  width: 44px; height: 44px; background: #ffffff; border: 1px solid rgba(0,0,0,0.05); border-radius: 14px;
  display: flex; align-items: center; justify-content: center; font-size: 22px; color: var(--text-title, #0f172a);
  cursor: pointer; transition: all 0.2s ease; box-shadow: 0 2px 10px rgba(15,23,42,0.04);
}
.hamburger-btn:hover { border-color: rgba(0,0,0,0.12); box-shadow: 0 4px 14px rgba(15,23,42,0.08); }
.hamburger-btn:active { transform: scale(0.95); }
.hamburger-btn .support-unread-dot { top: 8px; right: 8px; }

/* --- Сайдбар --- */
.sidebar-overlay {
  position: fixed; inset: 0;
  background: rgba(15, 23, 42, 0.4);
  backdrop-filter: blur(4px); -webkit-backdrop-filter: blur(4px);
  z-index: 1000; opacity: 0; pointer-events: none;
  transition: opacity 0.3s ease;
}
.sidebar-overlay.open { opacity: 1; pointer-events: auto; }

.sidebar {
  position: fixed; top: 0; right: 0; bottom: 0;
  width: 85%; max-width: 340px;
  background: #ffffff; z-index: 1001;
  transform: translateX(100%);
  transition: transform 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  display: flex; flex-direction: column;
  box-shadow: -8px 0 32px rgba(15, 23, 42, 0.12);
  border-top-left-radius: 24px; border-bottom-left-radius: 24px;
}
.sidebar.open { transform: translateX(0); }

.sidebar-header {
  display: flex; justify-content: space-between; align-items: center;
  padding: 24px 20px; border-bottom: 1px solid #f1f5f9;
}
.sidebar-header h2 { font-size: 18px; font-weight: 800; color: var(--text-title, #0f172a); margin: 0; }
.close-btn {
  background: #f1f5f9; border: none; width: 34px; height: 34px; border-radius: 50%;
  display: flex; align-items: center; justify-content: center; font-size: 18px;
  color: var(--text-muted, #64748b); cursor: pointer; transition: background 0.2s ease;
}
.close-btn:hover { background: #e2e8f0; }

.sidebar-nav {
  padding: 16px; flex: 1;
  display: flex; flex-direction: column; gap: 6px;
  /* Меню прокручивается, а не обрезается боковой панелью фиксированной высоты:
     min-height позволяет этому flex-элементу сжаться меньше содержимого, а
     ограниченный overscroll держит страницу за оверлеем неподвижной. */
  min-height: 0; overflow-y: auto; overscroll-behavior: contain;
  -webkit-overflow-scrolling: touch;
  padding-bottom: calc(16px + env(safe-area-inset-bottom, 0px));
}
.nav-item {
  display: flex; align-items: center; gap: 12px;
  width: 100%; padding: 15px 16px; border: none; border-radius: 16px;
  background: transparent; text-align: left;
  color: var(--text-title, #0f172a); font-family: inherit; font-weight: 700; font-size: 15px;
  cursor: pointer; transition: background 0.2s ease;
}
.nav-item i { font-size: 22px; color: var(--accent-main, #6366f1); }
.nav-item:hover { background: #f8fafc; }
.nav-item:active { background: #f1f5f9; }
.nav-item .nav-dot { top: 14px; left: 34px; right: auto; }

.lang-control {
  display: flex; justify-content: space-between; align-items: center;
  padding: 14px 16px; background: #f8fafc; border-radius: 16px; margin: 6px 0;
}
.lang-control span { font-weight: 700; font-size: 14px; color: var(--text-title, #0f172a); }

/* --- Сетка --- */
.premium-grid {
  display: grid;
  grid-template-columns: 1.5fr 1fr;
  gap: 24px;
}

/* --- Карточка профиля (новый компактный дизайн) --- */
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

.profile-phone-row {
  display: flex; align-items: center; gap: 6px;
}
.profile-phone { font-size: 20px; font-weight: 700; color: var(--text-title, #0f172a); letter-spacing: -0.5px; line-height: 1; }
.verified-badge { color: #10b981; font-size: 20px; display: flex; align-items: center; justify-content: center; }

.badge-brand {
  background: #eef2ff; color: #5c60f5;
  padding: 4px 12px; border-radius: 99px;
  font-size: 12px; font-weight: 600; display: inline-flex; align-items: center; gap: 4px;
  cursor: pointer;
}

/* --- Карточка баланса (новый компактный тёмный дизайн) --- */
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

.bc-value-placeholder { opacity: 0.5; }

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

/* --- Гигантская кнопка действия --- */
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

/* --- Плавающий список заказов --- */
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

/* Стили модального окна пополнения */
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

/* Поле ввода и иконка внутри модалки */
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

/* Стили всплывающих уведомлений */
.toast-container {
  position: fixed;
  top: 84px;
  right: 24px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  z-index: 100000;
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

.toast.chat-toast {
  transition: transform 0.2s ease, box-shadow 0.2s ease;
}
.toast.chat-toast:hover {
  transform: translateY(-2px);
  box-shadow: 0 14px 35px -10px rgba(99, 102, 241, 0.25);
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

/* --- Стили встроенного чата-аккордеона --- */
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
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  max-height: 300px;
  overflow-y: auto;
  background: #f8fafc;
}

/* Пузыри сообщений — согласованы с чатом поддержки (SupportChatModal.vue),
   чтобы два чата читались как один продукт. Правила цвета нацелены на
   .msg-container — класс, который реально используется в разметке (раньше они
   целились в устаревший .msg, из-за чего пузыри оставались без фона). */
.bubble {
  padding: 10px 14px; font-size: 14px; border-radius: 18px; line-height: 1.4;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.04);
}
.msg-container.incoming .bubble {
  background: #ffffff; border: 1px solid rgba(0, 0, 0, 0.06); color: #0f172a;
  border-bottom-left-radius: 4px;
}
.msg-container.outgoing .bubble {
  background: #5c60f5; color: #ffffff; border-bottom-right-radius: 4px;
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

/* Модальное окно просмотра изображения */
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

/* --- Стили действий над сообщением и контейнера --- */
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
.read-receipt { color: var(--accent-main, #6366f1); font-size: 14px; }
.delivered-receipt { color: #64748b; font-size: 14px; }
.sent-receipt { color: #94a3b8; font-size: 13px; }

.yellow-unread-dot {
  position: absolute;
  top: -2px;
  right: -2px;
  width: 10px;
  height: 10px;
  background-color: #f59e0b;
  border: 2px solid #ffffff;
  border-radius: 50%;
  box-shadow: 0 0 6px rgba(245, 158, 11, 0.8);
  animation: pulse-dot 1.5s infinite;
}

@keyframes pulse-dot {
  0% { transform: scale(1); }
  50% { transform: scale(1.2); }
  100% { transform: scale(1); }
}

.chat-top-toast {
  background: linear-gradient(135deg, #3b82f6 0%, #1d4ed8 100%);
  color: white;
  border-radius: 12px;
  padding: 12px 16px;
  display: flex;
  align-items: center;
  gap: 12px;
  cursor: pointer;
  box-shadow: 0 10px 25px -5px rgba(59, 130, 246, 0.4);
  z-index: 10000;
  margin-bottom: 12px;
  animation: slide-down 0.3s ease-out;
}

.toast-chat-icon {
  font-size: 20px;
}

.toast-chat-content {
  flex: 1;
  min-width: 0;
}

.toast-chat-title {
  font-weight: 700;
  font-size: 13px;
  margin-bottom: 2px;
}

.toast-chat-text {
  font-size: 12px;
  opacity: 0.9;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* --- Стили ультракомпактного элемента заказа --- */
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
}

.btn-support-header {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 14px;
  border-radius: 12px;
  background: rgba(92, 96, 245, 0.1);
  color: var(--brand-primary, #5c60f5);
  border: 1px solid rgba(92, 96, 245, 0.2);
  font-family: inherit;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s ease;
}

.btn-support-header:hover {
  background: var(--brand-primary, #5c60f5);
  color: #ffffff;
  border-color: var(--brand-primary, #5c60f5);
}

.support-unread-dot {
  position: absolute;
  top: 4px;
  right: 4px;
  width: 9px;
  height: 9px;
  border-radius: 50%;
  background: #ef4444;
  box-shadow: 0 0 0 2px #ffffff;
}

.profile-fullname {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-title, #0f172a);
  margin-top: 2px;
}

/* Название заказа: категория крупно, конкретная услуга под ней. */
.order-title-stack {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.order-title-main {
  font-weight: 800;
  font-size: 15px;
  line-height: 1.25;
  color: inherit;
}
.order-title-sub {
  font-size: 12px;
  font-weight: 600;
  line-height: 1.2;
  color: var(--text-muted, #64748b);
}
/* .item-title truncates with an ellipsis; on a two-line stack that has to move
   to the lines themselves, or the flex box just clips them. */
.order-title-main,
.order-title-sub {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* --- Меню по образцу админской боковой панели --- */
.sidebar-header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

/* Выход — иконкой наверху, как в админке: подпись живёт в aria-label,
   потому что рядом с крестиком нет места для текста. */
.header-logout {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 34px;
  height: 34px;
  padding: 0;
  border: none;
  border-radius: 50%;
  background: #fef2f2;
  color: #ef4444;
  font-size: 18px;
  cursor: pointer;
  transition: background 0.2s ease;
}
.header-logout:hover { background: #fee2e2; }

.nav-section {
  font-size: 11px;
  font-weight: 700;
  color: #94a3b8;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin: 16px 0 8px 12px;
}
.nav-section:first-child { margin-top: 0; }

.nav-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.sidebar-footer {
  margin-top: auto;
  padding-top: 12px;
  border-top: 1px solid rgba(0, 0, 0, 0.06);
  display: flex;
  flex-direction: column;
  gap: 6px;
}
</style>
