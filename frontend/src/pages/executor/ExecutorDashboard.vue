<template>
  <div class="premium-dashboard-page">
    <div class="container">
      <!-- Шапка -->
      <header class="glass-header">
        <div class="logo-text">
          <i class="ph-fill ph-planet" style="color: var(--accent-main);"></i>
          <span>{{ $t('executor.executorDashboardTitle') }}</span>
        </div>
        <div class="top-actions">
          <div class="lang-switch-wrapper">
            <LanguageSwitcher />
          </div>
          <button type="button" class="btn-glass" :title="$t('common.logout')" @click="handleLogout">
            <i class="ph ph-sign-out"></i>
          </button>
        </div>
      </header>

      <update-banner class="mb-4" />

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
        <!-- Карточка профиля исполнителя -->
        <div class="surface-card">
          <div class="profile-row">
            <div class="avatar-glow">
              <i class="ph ph-user"></i>
              <div class="online-dot" title="Онлайн"></div>
            </div>
            <div class="profile-info">
              <div class="role-badge">{{ $t('executor.verifiedExecutor') }}</div>
              <h2>{{ phone || '7 999 745 46 56' }}</h2>
              <div class="status-pill-badge" :class="status === 'ACTIVE' ? 'active' : 'inactive'">
                <i class="ph-fill ph-check-circle"></i> {{ $t('executor.statusPill', { status: status }) }}
              </div>
            </div>
          </div>
        </div>

        <!-- Кошелек исполнителя -->
        <div class="wallet-card" @click="openFinancialHistoryModal">
          <div class="wallet-inner">
            <div class="w-label">{{ $t('executor.availableBalance') }}</div>
            <div class="w-amount">
              {{ currencySymbol }}{{ Number(balance).toFixed(2) }}
            </div>
            <button type="button" class="btn-wallet-action">
              <i class="ph-bold ph-plus-circle"></i> {{ $t('executor.requestPayoutHistory') }}
            </button>
          </div>
        </div>
      </div>

      <!-- Панель управления сменой и карте -->
      <div class="shift-action-bar">
        <div class="shift-info">
          <div class="shift-icon">
            <i class="ph-bold ph-clock"></i>
          </div>
          <div class="shift-text">
            <div class="shift-title">
              {{ activeShift && activeShift.status === 'ACTIVE' ? $t('executor.shiftActive') : $t('executor.shiftClosed') }}
            </div>
            <div class="shift-timer">
              <template v-if="activeShift && activeShift.status === 'ACTIVE'">
                {{ $t('executor.shiftRemaining', { timer: shiftCountdown || '00:00:00', end: formatDate(activeShift.planned_end_at) }) }}
              </template>
              <template v-else>
                {{ $t('executor.openNewShiftHint') }}
              </template>
            </div>
          </div>
        </div>

        <div class="shift-controls">
          <div v-if="!activeShift || activeShift.status !== 'ACTIVE'" class="start-shift-group">
            <select v-model="shiftDuration" class="shift-select">
              <option v-for="d in durationOptions" :key="d" :value="d">{{ d }} {{ $t('common.hours') }}</option>
            </select>
            <button type="button" class="btn-action-primary" :disabled="startingShift" @click="startShift">
              <span v-if="startingShift" class="spinner-sm"></span>
              <template v-else>{{ $t('executor.openShift') }} <i class="ph-bold ph-play"></i></template>
            </button>
          </div>
          <button v-else type="button" class="btn-end-shift" :disabled="endingShiftEarly" @click="earlyEndShift">
            <span v-if="endingShiftEarly" class="spinner-sm"></span>
            <template v-else>{{ $t('executor.endShiftEarly') }}</template>
          </button>

          <button type="button" class="btn-map-trigger" @click="showExecutorMapModal = true">
            <i class="ph-bold ph-map-trifold"></i> {{ $t('executor.mapTitle') }}
          </button>
        </div>
      </div>

      <!-- Назначенные заказы -->
      <div class="section-container">
        <div class="section-title-row">
          <h3>{{ $t('executor.assignedOrders') }}</h3>
          <span class="count-badge">({{ assignedOrders.length }})</span>
          <button type="button" class="btn-icon-refresh" @click="fetchAssignedOrders">
            <i class="ph-bold ph-arrows-clockwise"></i>
          </button>
        </div>

        <div v-if="assignedOrders.length === 0" class="empty-state-card">
          <i class="ph ph-hourglass-empty"></i>
          <p>{{ $t('executor.waitingForAssignment') }}</p>
        </div>

        <div v-else class="orders-stack">
          <div
            v-for="order in assignedOrders"
            :key="order.id"
            :class="['order-row', { 'chat-open': selectedChatOrder && selectedChatOrder.id === order.id }]"
          >
            <div class="order-summary" @click="toggleChat(order)">
              <div class="o-icon">
                <i class="ph-fill ph-package"></i>
              </div>
              <div class="o-info">
                <div class="o-title">{{ formatOrderType(order) }}</div>
                <div class="o-subtitle">#{{ order?.id ? order.id.slice(0, 8) : '---' }} • {{ order.customer_phone || '' }}</div>
              </div>
              <div class="o-price">
                {{ currencySymbol }}{{ Number(order.final_amount || order.hold_amount).toFixed(2) }}
              </div>
              <div class="o-status">
                <span class="badge-status" :style="{ backgroundColor: getStatusColor(order.status) }">
                  {{ $t('orderStatus.' + order.status, order.status) }}
                </span>
              </div>
              <div class="o-actions" @click.stop>
                <button
                  v-if="order.status === 'ASSIGNED'"
                  type="button"
                  class="btn-action-execute"
                  :title="$t('executor.executed')"
                  @click="markOrderAsExecuted(order.id)"
                >
                  <i class="ph-bold ph-check-circle"></i> {{ $t('executor.executed') }}
                </button>
                <button
                  type="button"
                  :class="['btn-chat-toggle', { active: selectedChatOrder && selectedChatOrder.id === order.id }]"
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
                  :class="['msg', msg.sender_id === currentUserId ? 'outgoing' : 'incoming']"
                >
                  <div class="bubble">
                    <div v-if="isImageAttachment(msg)" class="chat-img-wrapper mb-1">
                      <img
                        :src="getImageSrc(msg)"
                        alt="Фото"
                        class="chat-img"
                        @error="onChatImgError(msg)"
                        @click="openImagePreview(getImageSrc(msg))"
                      />
                    </div>
                    <span v-if="msg.text || msg.content">{{ msg.text || msg.content }}</span>
                  </div>
                </div>
              </div>

              <div class="chat-input-area">
                <form class="input-group" @submit.prevent="sendChatMessage">
                  <button
                    type="button"
                    class="btn-attach"
                    title="Прикрепить фото"
                    :disabled="uploadingChatFile"
                    @click="triggerImageSelect"
                  >
                    <i v-if="uploadingChatFile" class="ph ph-spinner spinner"></i>
                    <i v-else class="ph-bold ph-image"></i>
                  </button>

                  <input
                    v-model="chatInputText"
                    type="text"
                    placeholder="Напишите сообщение..."
                    class="inline-input"
                  />

                  <button type="submit" class="btn-inline-send" :disabled="!chatInputText.trim() && !uploadingChatFile">
                    <i class="ph-bold ph-paper-plane-tilt"></i>
                  </button>
                </form>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- GPS и Заказы рядом -->
      <div class="section-container">
        <div class="section-title-row">
          <h3>{{ $t('executor.nearbyOrders') }}</h3>
          <button type="button" class="btn-icon-refresh" @click="updateCurrentPosition(true)">
            <i class="ph-bold ph-arrows-clockwise"></i>
          </button>
        </div>

        <div class="gps-card-bar">
          <div class="gps-icon">
            <i class="ph-fill ph-crosshair"></i>
          </div>
          <div class="gps-info">
            <div class="gps-label">{{ $t('executor.currentCoordinates') }}</div>
            <input type="text" class="gps-input-value" :value="`${currentLat.toFixed(5)}, ${currentLon.toFixed(5)}`" readonly />
          </div>
          <button type="button" class="btn-edit-gps" :title="$t('common.save')" @click="openMapPicker">
            <i class="ph-bold ph-pencil-simple"></i>
          </button>
        </div>

        <div v-if="availableOrders.length === 0" class="empty-state-card">
          <p>{{ $t('executor.noAvailableOrders') }}</p>
        </div>
        <div v-else class="orders-stack">
          <div v-for="order in availableOrders" :key="order.id" class="order-row">
            <div class="order-summary">
              <div class="o-icon">
                <i class="ph-fill ph-package"></i>
              </div>
              <div class="o-info">
                <div class="o-title">{{ formatOrderType(order) }}</div>
                <div class="o-subtitle">{{ order.address || '' }}</div>
              </div>
              <div class="o-price">
                {{ currencySymbol }}{{ Number(order.hold_amount).toFixed(2) }}
              </div>
              <div class="o-actions">
                <button type="button" class="btn-action-accept" @click="acceptOrder(order.id)">
                  {{ $t('common.accept') }} <i class="ph-bold ph-check"></i>
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- История смен и заказов -->
      <div class="section-container">
        <div
          class="section-title-row cursor-pointer"
          @click="isHistoryCollapsed = !isHistoryCollapsed"
        >
          <i class="ph-bold ph-clock-counter-clockwise" style="color: var(--text-muted); font-size: 20px;"></i>
          <h3>{{ $t('executor.financialHistoryTitle') }}</h3>
          <span class="count-badge">({{ executorHistoryOrders.length }})</span>
          <i :class="['ph-bold', isHistoryCollapsed ? 'ph-caret-down' : 'ph-caret-up']" style="margin-left: auto;"></i>
        </div>

        <div v-if="!isHistoryCollapsed" class="orders-stack">
          <div v-if="executorHistoryOrders.length === 0" class="empty-state-card">
            {{ $t('customer.noHistoryOrders') }}
          </div>
          <div
            v-for="order in executorHistoryOrders"
            :key="order.id"
            class="order-row history-row"
          >
            <div class="order-summary">
              <div class="o-icon history">
                <i class="ph-bold ph-check-circle"></i>
              </div>
              <div class="o-info">
                <div class="o-title gray">{{ formatOrderType(order) }}</div>
                <div class="o-subtitle">#{{ order?.id ? order.id.slice(0, 8) : '---' }}</div>
              </div>
              <div class="o-price gray">
                {{ currencySymbol }}{{ Number(order.final_amount || order.hold_amount).toFixed(2) }}
              </div>
              <div class="o-status">
                <span class="status-dot-gray">{{ $t('orderStatus.' + order.status, order.status) }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

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
import ExecutorMapModal from './components/ExecutorMapModal.vue'
import api, { buildChatWebSocketUrl, resolveFileUrl } from '../../services/api'
import { compressImage } from '../../utils/imageCompressor'
import { getServiceVariants, type ServiceNode } from '../../api/services'

export default defineComponent({
  name: 'ExecutorDashboard',
  components: {
    UpdateBanner,
    LanguageSwitcher,
    ExecutorMapModal,
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
    const isHistoryCollapsed = ref(false)

    // Location state
    const currentLat = ref(55.7558)
    const currentLon = ref(37.6173)

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

    const fetchHistoryOrders = async () => {
      try {
        const res = await api.get('/executor/history')
        executorHistoryOrders.value = res.data || []
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
    const toggleChat = (order: any) => {
      if (selectedChatOrder.value && selectedChatOrder.value.id === order.id) {
        selectedChatOrder.value = null
        chatMessages.value = []
      } else {
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
    }

    const sendChatMessage = async () => {
      if (!chatInputText.value.trim() || !selectedChatOrder.value) return
      const text = chatInputText.value.trim()
      chatInputText.value = ''
      try {
        await api.post(`/chats/${selectedChatOrder.value.id}/messages`, { text })
        fetchChatMessages(selectedChatOrder.value.id)
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
      availableOrders,
      executorHistoryOrders,
      isHistoryCollapsed,
      currentLat,
      currentLon,
      showExecutorMapModal,
      selectedChatOrder,
      chatMessages,
      chatInputText,
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

.top-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}

.btn-glass {
  background: #ffffff;
  border: 1px solid rgba(0,0,0,0.05);
  width: 40px;
  height: 40px;
  border-radius: var(--rad-sm);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
  color: var(--text-muted);
  cursor: pointer;
  box-shadow: 0 2px 8px rgba(0,0,0,0.02);
  transition: var(--transition);
}

.btn-glass:hover {
  color: var(--text-title);
  border-color: rgba(0,0,0,0.1);
}

/* --- Toast Alerts --- */
.toast-alert {
  padding: 16px 20px;
  border-radius: var(--rad-sm);
  display: flex;
  align-items: center;
  gap: 12px;
  font-weight: 500;
  box-shadow: var(--shadow-float);
  animation: slideDown 0.3s ease;
}

.toast-alert.success {
  background: #ecfdf5;
  color: #065f46;
  border: 1px solid #a7f3d0;
}

.toast-alert.danger {
  background: #fef2f2;
  color: #991b1b;
  border: 1px solid #fecaca;
}

.btn-toast-close {
  margin-left: auto;
  background: transparent;
  border: none;
  font-size: 20px;
  cursor: pointer;
  color: inherit;
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

@media (max-width: 768px) {
  .premium-grid { grid-template-columns: 1fr; }
  .profile-row { flex-direction: column; text-align: center; }
  .profile-info { align-items: center; }
  .shift-action-bar { flex-direction: column; align-items: stretch; text-align: center; }
  .shift-info { flex-direction: column; }
  .shift-controls { flex-direction: column; width: 100%; }
  .start-shift-group { width: 100%; }
  .btn-action-primary, .btn-end-shift, .btn-map-trigger { width: 100%; justify-content: center; }
}

@media (max-width: 600px) {
  .order-summary {
    flex-wrap: wrap;
    gap: 12px;
    padding: 16px;
  }
  .o-info {
    min-width: 140px;
  }
  .o-price {
    margin-left: auto;
  }
  .o-status {
    width: 100%;
    margin-top: 4px;
  }
  .o-actions {
    width: 100%;
    justify-content: flex-end;
    margin-top: 6px;
  }
}
</style>
