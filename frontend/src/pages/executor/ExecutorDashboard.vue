<template>
  <div class="executor-dashboard">
    <div class="dashboard-header mb-5">
      <div class="d-flex justify-content-between align-items-center">
        <div>
          <h1 class="va-h3 m-0">{{ $t('executor.title') }}</h1>
          <span class="text-secondary text-sm">{{ $t('executor.subtitle') }}</span>
        </div>
        <va-button color="danger" outline size="small" @click="handleLogout">
          <va-icon name="logout" class="mr-2" /> {{ $t('app.logout') }}
        </va-button>
      </div>
    </div>

    <update-banner class="mb-4" />

    <!-- Alert messages -->
    <va-alert v-if="successMsg" color="success" class="mb-4" closeable @dismissed="successMsg = ''">
      {{ successMsg }}
    </va-alert>
    <va-alert v-if="errorMsg" color="danger" class="mb-4" closeable @dismissed="errorMsg = ''">
      {{ errorMsg }}
    </va-alert>

    <div class="row g-4">
      <!-- Left Column: Profile & Shift Controls -->
      <div class="col-md-5">
        <!-- Profile Card -->
        <ExecutorProfileCard
          :phone="phone"
          :status="status"
          :balance="balance"
          :currency-symbol="currencySymbol"
          @open-financial-history="openFinancialHistoryModal"
        />

        <!-- Shift Controls Card -->
        <va-card class="p-4 mb-4 shadow-card">
          <h3 class="va-h5 mb-4 text-primary d-flex align-items-center">
            <va-icon name="schedule" class="mr-2" /> {{ $t('executor.shiftStatus') }}
          </h3>

          <div v-if="!activeShift || activeShift.status !== 'ACTIVE'" class="no-shift-container">
            <div v-if="activeShift" class="info-list mb-4">
              <div class="info-item mb-2">
                <span class="info-label">{{ $t('executor.shiftStatus') }}</span>
                <span>
                  <va-badge :color="getShiftStatusColor(activeShift.status)" class="text-uppercase">
                    {{ activeShift.status }}
                  </va-badge>
                </span>
              </div>
              <div class="info-item mb-2">
                <span class="info-label">{{ $t('executor.duration') }}</span>
                <span class="info-val">{{ activeShift.duration_hours }} {{ $t('common.hours') }}</span>
              </div>
              <div class="info-item mb-2">
                <span class="info-label">{{ $t('executor.startedAt') }}</span>
                <span class="info-val text-xs">{{ formatDate(activeShift.started_at) }}</span>
              </div>
              <div class="info-item mb-2">
                <span class="info-label">{{ $t('executor.actualEnd') }}</span>
                <span class="info-val text-xs">{{ formatDate(activeShift.actual_end_at) }}</span>
              </div>
              <div v-if="activeShift.fine_amount > 0" class="info-item mb-2">
                <span class="info-label">{{ $t('executor.fine') }}</span>
                <span class="info-val text-xs">{{ currencySymbol }}{{ Number(activeShift.fine_amount).toFixed(2) }}</span>
              </div>
            </div>

            <p class="text-secondary text-sm mb-4">
              {{ $t('executor.noActiveShift') }}
            </p>
            <va-form @submit.prevent="startShift">
              <va-select
                v-model="shiftDuration"
                :options="durationOptions"
                :label="$t('executor.shiftDurationHours')"
                class="mb-4"
                required
              />
              <va-button type="submit" color="success" block :loading="startingShift">
                {{ $t('executor.startShift') }}
              </va-button>
            </va-form>
          </div>

          <div v-else class="active-shift-container">
            <div class="info-list mb-4">
              <div class="info-item mb-2">
                <span class="info-label">{{ $t('executor.shiftStatus') }}</span>
                <span>
                  <va-badge color="success" class="text-uppercase">{{ activeShift.status }}</va-badge>
                </span>
              </div>
              <div class="info-item mb-2">
                <span class="info-label">{{ $t('executor.duration') }}</span>
                <span class="info-val">{{ activeShift.duration_hours }} {{ $t('common.hours') }}</span>
              </div>
              <div class="info-item mb-2">
                <span class="info-label">{{ $t('executor.elapsedTime') }}</span>
                <span class="info-val font-mono font-bold text-primary">{{ formatDuration(elapsedSeconds) }}</span>
              </div>
              <div class="info-item mb-2">
                <span class="info-label">{{ $t('executor.startedAt') }}</span>
                <span class="info-val text-xs">{{ formatDate(activeShift.started_at) }}</span>
              </div>
            </div>

            <!-- GPS Location Status inside active shift -->
            <div class="location-status-box mb-4 p-3 bg-light rounded">
              <div class="d-flex align-items-center justify-content-between mb-2">
                <span class="text-xs font-bold text-secondary text-uppercase d-flex align-items-center">
                  <va-icon name="location_on" class="mr-1 text-danger" size="small" />
                  {{ $t('executor.gpsLocation') }}
                </span>
                <va-button size="x-small" color="primary" flat @click="openMapPicker">
                  🗺️ {{ $t('executor.setMapPin') }}
                </va-button>
              </div>
              <div v-if="effectiveLat !== null && effectiveLon !== null" class="text-xs font-mono text-dark">
                {{ effectiveLat.toFixed(5) }}, {{ effectiveLon.toFixed(5) }}
                <span v-if="isLocationSimulated" class="badge badge-warning text-xxs ml-1">Pin</span>
                <span v-else class="badge badge-info text-xxs ml-1">GPS</span>
              </div>
              <div v-else class="text-xs text-secondary italic">
                {{ $t('executor.locatingGps') }}
              </div>
            </div>

            <div class="d-flex gap-2">
              <va-button color="danger" outline block :loading="endingShift" @click="endShift">
                {{ $t('executor.endShiftNormally') }}
              </va-button>
              <va-button color="warning" flat block :loading="endingShift" @click="earlyEndShift">
                {{ $t('executor.endShiftEarly') }}
              </va-button>
            </div>
          </div>
        </va-card>

        <!-- Active Assigned Orders for Executor -->
        <va-card class="p-4 mb-4 shadow-card">
          <h3 class="va-h5 mb-4 text-primary d-flex align-items-center">
            <va-icon name="assignment" class="mr-2" /> {{ $t('executor.assignedOrder') }}
          </h3>
          <div v-if="myActiveOrders.length === 0" class="text-secondary text-sm py-3 text-center">
            {{ $t('executor.noAssignedOrders') }}
          </div>
          <div v-else>
            <div v-for="order in myActiveOrders" :key="order.id" class="border rounded p-3 mb-3 bg-light">
              <div class="d-flex justify-content-between align-items-center mb-2">
                <span class="font-bold text-sm">#{{ order.id.slice(0, 8) }}</span>
                <va-badge color="info">{{ order.status }}</va-badge>
              </div>
              <div class="text-xs text-secondary mb-1">
                {{ $t('executor.type') }}: <strong>{{ order.service_variant?.name_ru || order.service_variant_id }}</strong>
              </div>
              <div class="text-xs text-secondary mb-2" v-if="order.address">
                {{ $t('customer.pickupAddress') }}: <strong>{{ order.address }}</strong>
              </div>
              <div class="text-xs font-bold text-primary mb-3">
                {{ $t('executor.payout') }}: {{ currencySymbol }}{{ Number(order.final_amount || order.hold_amount).toFixed(2) }}
              </div>
              <div class="d-flex gap-2">
                <va-button
                  color="primary"
                  size="small"
                  class="flex-grow-1 position-relative"
                  @click="openChat(order)"
                >
                  <va-icon name="chat" class="mr-1" /> {{ $t('common.chatWithCustomer') }}
                  <span v-if="unreadOrderIDs.has(order.id)" class="yellow-unread-dot"></span>
                </va-button>
                <va-button
                  color="success"
                  size="small"
                  :loading="completingOrder === order.id"
                  @click="completeOrder(order.id)"
                >
                  <va-icon name="check" class="mr-1" /> {{ $t('executor.completeOrder') }}
                </va-button>
              </div>
            </div>
          </div>
        </va-card>
      </div>

      <!-- Right Column: Available Orders Stream -->
      <div class="col-md-7">
        <va-card class="p-4 shadow-card">
          <div class="d-flex justify-content-between align-items-center mb-4">
            <h3 class="va-h5 m-0 text-primary d-flex align-items-center">
              <va-icon name="list_alt" class="mr-2" /> {{ $t('executor.availableOrders') }}
            </h3>
            <va-button icon="refresh" color="secondary" size="small" flat @click="fetchAvailableOrders" />
          </div>

          <div v-if="availableOrders.length === 0" class="text-center py-5">
            <va-icon name="inbox" size="large" color="secondary" class="mb-3" />
            <p class="text-secondary text-sm m-0">{{ $t('executor.noOrdersAvailable') }}</p>
          </div>

          <div v-else class="orders-list">
            <div
              v-for="order in availableOrders"
              :key="order.id"
              class="order-card p-3 mb-3 border rounded shadow-sm hover-shadow transition"
            >
              <div class="d-flex justify-content-between align-items-start mb-2">
                <div>
                  <h4 class="va-h6 font-bold m-0 text-dark">
                    {{ order.service_variant ? order.service_variant.name_ru : order.service_variant_id }}
                  </h4>
                  <span class="text-secondary text-xs">ID: #{{ order.id.slice(0, 8) }}</span>
                </div>
                <div class="text-right">
                  <span class="payout-amount font-bold text-success d-block">
                    {{ currencySymbol }}{{ Number(order.final_amount || order.hold_amount).toFixed(2) }}
                  </span>
                  <va-badge color="warning" class="text-uppercase text-xxs">
                    {{ order.status }}
                  </va-badge>
                </div>
              </div>

              <div class="order-details-grid my-3 text-xs">
                <div v-if="order.address" class="detail-item mb-1">
                  <span class="text-secondary mr-1">📍 {{ $t('customer.pickupAddress') }}:</span>
                  <strong class="text-dark">{{ order.address }}</strong>
                </div>
                <div v-if="order.lat && order.lon" class="detail-item mb-1">
                  <span class="text-secondary mr-1">🌐 {{ $t('customer.coordinates') }}:</span>
                  <span class="font-mono text-dark">{{ order.lat.toFixed(4) }}, {{ order.lon.toFixed(4) }}</span>
                </div>
                <div class="detail-item">
                  <span class="text-secondary mr-1">⏱️ {{ $t('customer.created') }}:</span>
                  <span class="text-dark">{{ formatDate(order.created_at) }}</span>
                </div>
              </div>

              <div class="d-flex justify-content-between align-items-center pt-2 border-top">
                <div class="badges-row d-flex gap-2">
                  <va-badge v-if="order.is_urgent" color="danger" size="small">{{ $t('customer.urgent') }}</va-badge>
                  <va-badge v-if="order.is_asap" color="info" size="small">{{ $t('customer.asap') }}</va-badge>
                  <va-badge v-if="order.service_variant?.is_auction" color="primary" size="small">{{ $t('customer.auction') }}</va-badge>
                </div>

                <div>
                  <div v-if="order.service_variant?.is_auction" class="d-flex gap-2 align-items-center">
                    <va-input
                      v-model.number="bidAmounts[order.id]"
                      type="number"
                      size="small"
                      placeholder="Ставка ₽"
                      style="width: 100px;"
                    />
                    <va-button
                      color="primary"
                      size="small"
                      :loading="submittingBid === order.id"
                      @click="submitBid(order.id)"
                    >
                      {{ $t('executor.submitBid') }}
                    </va-button>
                  </div>
                  <va-button
                    v-else
                    color="success"
                    size="small"
                    :loading="acceptingOrder === order.id"
                    @click="acceptOrder(order.id)"
                  >
                    {{ $t('executor.acceptOrder') }}
                  </va-button>
                </div>
              </div>
            </div>
          </div>
        </va-card>
      </div>
    </div>

    <!-- Map Coordinate Picker Modal -->
    <va-modal
      v-model="mapPickerVisible"
      :title="$t('executor.chooseLocationOnMap')"
      hide-default-actions
      max-width="650px"
    >
      <div class="p-2">
        <p class="text-xs text-secondary mb-3">
          {{ $t('executor.clickOnMapToSetLocation') }}
        </p>
        <div id="executor-leaflet-map" style="height: 320px; width: 100%;" class="rounded border mb-3"></div>
        <div class="d-flex justify-content-between align-items-center">
          <div class="text-xs font-mono">
            <span v-if="pickerLat !== null && pickerLon !== null">
              Lat: {{ pickerLat.toFixed(5) }}, Lon: {{ pickerLon.toFixed(5) }}
            </span>
          </div>
          <div class="d-flex gap-2">
            <va-button color="secondary" @click="mapPickerVisible = false">
              {{ $t('common.close') }}
            </va-button>
            <va-button color="primary" :disabled="pickerLat === null" @click="applyMapCoordinates">
              {{ $t('executor.applyLocation') }}
            </va-button>
          </div>
        </div>
      </div>
    </va-modal>

    <!-- Financial History Modal -->
    <va-modal
      v-model="showFinancialHistoryModal"
      :title="$t('executor.financialHistory')"
      hide-default-actions
      max-width="700px"
    >
      <div class="p-3">
        <div v-if="financialTransactions.length === 0" class="text-center py-4 text-secondary text-sm">
          {{ $t('executor.noFinancialHistory') }}
        </div>
        <div v-else class="table-responsive">
          <table class="table table-sm text-xs align-middle">
            <thead>
              <tr>
                <th>Дата</th>
                <th>Тип</th>
                <th>Сумма</th>
                <th>Баланс после</th>
                <th>Описание</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="tx in financialTransactions" :key="tx.id">
                <td>{{ formatDate(tx.created_at) }}</td>
                <td>
                  <va-badge :color="tx.amount >= 0 ? 'success' : 'danger'" class="text-xxs">
                    {{ tx.type || (tx.amount >= 0 ? 'Пополнение' : 'Списание') }}
                  </va-badge>
                </td>
                <td :class="['font-bold', tx.amount >= 0 ? 'text-success' : 'text-danger']">
                  {{ tx.amount >= 0 ? '+' : '' }}{{ currencySymbol }}{{ Number(tx.amount).toFixed(2) }}
                </td>
                <td class="font-mono">{{ currencySymbol }}{{ Number(tx.balance_after).toFixed(2) }}</td>
                <td class="text-secondary">{{ tx.description || '-' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="text-right mt-3">
          <va-button color="secondary" @click="showFinancialHistoryModal = false">
            {{ $t('common.close') }}
          </va-button>
        </div>
      </div>
    </va-modal>

    <!-- Top Floating Toast Notification for Incoming Messages -->
    <div
      v-if="chatToast"
      class="chat-toast-floating p-3 rounded-lg shadow-lg d-flex align-items-center justify-content-between cursor-pointer"
      @click="openChatByToast"
    >
      <div class="toast-chat-icon mr-3">💬</div>
      <div class="flex-grow-1 overflow-hidden">
        <div class="font-bold text-xs text-white">{{ chatToast.title }}</div>
        <div class="text-xs text-white-75 truncate">{{ chatToast.text }}</div>
      </div>
      <button type="button" class="toast-close-btn ml-2 text-white" @click.stop="chatToast = null">✕</button>
    </div>

    <!-- Telegram-Style Sliding Chat Panel Component -->
    <ChatDrawer
      v-model:chat-text="chatText"
      :selected-chat-order="selectedChatOrder"
      :chat-messages="chatMessages"
      :current-user-id="authStore.userID"
      :recipient-title="$t('common.chatWithCustomer')"
      recipient-initials="👤"
      :recipient-role-label="$t('common.customer')"
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
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../../stores/auth-store'
import { Capacitor } from '@capacitor/core'
import { Geolocation } from '@capacitor/geolocation'
import { Camera, CameraResultType, CameraSource } from '@capacitor/camera'
import UpdateBanner from '../../components/UpdateBanner.vue'
import ExecutorProfileCard from './components/ExecutorProfileCard.vue'
import ChatDrawer from '../../components/common/ChatDrawer.vue'
import ImagePreviewModal from '../../components/common/ImagePreviewModal.vue'
import { useChat } from '../../composables/useChat'
import { useImageBlobFallback } from '../../composables/useImageBlobFallback'
import { useImagePreviewModal } from '../../composables/useImagePreviewModal'
import api, { formatApiError, isDebug } from '../../services/api'
import { compressImage } from '../../utils/imageCompressor'

export default defineComponent({
  name: 'ExecutorDashboard',
  components: {
    UpdateBanner,
    ExecutorProfileCard,
    ChatDrawer,
    ImagePreviewModal,
  },
  setup() {
    const router = useRouter()
    const { t } = useI18n()
    const authStore = useAuthStore()
    const isNative = Capacitor.isNativePlatform()

    const phone = ref('')
    const status = ref('ACTIVE')
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

    const successMsg = ref('')
    const errorMsg = ref('')

    // Shifts
    const activeShift = ref<any>(null)
    const shiftDuration = ref(8)
    const durationOptions = [4, 8, 12]
    const startingShift = ref(false)
    const endingShift = ref(false)
    const elapsedSeconds = ref(0)
    let shiftTimer: any = null

    // Orders
    const availableOrders = ref<any[]>([])
    const myActiveOrders = ref<any[]>([])
    const acceptingOrder = ref<string | null>(null)
    const completingOrder = ref<string | null>(null)
    const bidAmounts = ref<Record<string, number>>({})
    const submittingBid = ref<string | null>(null)

    // GPS & Map
    const currentLat = ref<number | null>(null)
    const currentLon = ref<number | null>(null)
    const simulatedLat = ref<number | null>(null)
    const simulatedLon = ref<number | null>(null)
    const mapPickerVisible = ref(false)
    const pickerLat = ref<number | null>(null)
    const pickerLon = ref<number | null>(null)

    const effectiveLat = computed(() => simulatedLat.value ?? currentLat.value)
    const effectiveLon = computed(() => simulatedLon.value ?? currentLon.value)
    const isLocationSimulated = computed(() => simulatedLat.value !== null && simulatedLon.value !== null)

    const currencySymbol = computed(() => (authStore.currency === 'RUB' ? '₽' : '$'))

    // Financial History Modal
    const showFinancialHistoryModal = ref(false)
    const financialTransactions = ref<any[]>([])

    const openFinancialHistoryModal = async () => {
      showFinancialHistoryModal.value = true
      try {
        const res = await api.get('/executor/finances/history')
        financialTransactions.value = res.data || []
      } catch (err) {
        console.error('[ExecutorDashboard] failed to fetch financial history:', err)
      }
    }

    const fetchProfile = async () => {
      try {
        const res = await api.get('/executor/profile')
        phone.value = res.data.phone
        status.value = res.data.status || 'ACTIVE'
        balance.value = res.data.balance || 0
        authStore.setCurrency(res.data.currency || 'USD')
      } catch (err) {
        console.error('[ExecutorDashboard] failed to fetch profile:', err)
      }
    }

    const fetchActiveShift = async () => {
      try {
        const res = await api.get('/executor/shifts/active')
        activeShift.value = res.data || null
        if (activeShift.value && activeShift.value.status === 'ACTIVE') {
          startShiftTimer()
        } else {
          stopShiftTimer()
        }
      } catch (err) {
        activeShift.value = null
        stopShiftTimer()
      }
    }

    const startShiftTimer = () => {
      if (shiftTimer) clearInterval(shiftTimer)
      updateElapsedSeconds()
      shiftTimer = setInterval(() => {
        updateElapsedSeconds()
      }, 1000)
    }

    const stopShiftTimer = () => {
      if (shiftTimer) {
        clearInterval(shiftTimer)
        shiftTimer = null
      }
      elapsedSeconds.value = 0
    }

    const updateElapsedSeconds = () => {
      if (!activeShift.value || !activeShift.value.started_at) {
        elapsedSeconds.value = 0
        return
      }
      const startMs = new Date(activeShift.value.started_at).getTime()
      const nowMs = Date.now()
      elapsedSeconds.value = Math.max(0, Math.floor((nowMs - startMs) / 1000))
    }

    const startShift = async () => {
      successMsg.value = ''
      errorMsg.value = ''
      startingShift.value = true
      try {
        const res = await api.post('/executor/shifts/start', { duration_hours: shiftDuration.value })
        activeShift.value = res.data
        successMsg.value = t('executor.shiftStartedSuccess')
        startShiftTimer()
        await fetchAvailableOrders()
      } catch (err: any) {
        errorMsg.value = formatApiError(err, t('executor.errorStartShift'))
      } finally {
        startingShift.value = false
      }
    }

    const endShift = async () => {
      successMsg.value = ''
      errorMsg.value = ''
      endingShift.value = true
      try {
        const res = await api.post('/executor/shifts/end', { force_early: false })
        activeShift.value = res.data
        successMsg.value = t('executor.shiftEndedSuccess')
        stopShiftTimer()
        await fetchProfile()
      } catch (err: any) {
        errorMsg.value = formatApiError(err, t('executor.errorEndShift'))
      } finally {
        endingShift.value = false
      }
    }

    const earlyEndShift = async () => {
      if (!confirm(t('executor.confirmEarlyEnd'))) return
      successMsg.value = ''
      errorMsg.value = ''
      endingShift.value = true
      try {
        const res = await api.post('/executor/shifts/end', { force_early: true })
        activeShift.value = res.data
        successMsg.value = t('executor.shiftEndedEarlySuccess')
        stopShiftTimer()
        await fetchProfile()
      } catch (err: any) {
        errorMsg.value = formatApiError(err, t('executor.errorEndShift'))
      } finally {
        endingShift.value = false
      }
    }

    const fetchAvailableOrders = async () => {
      try {
        const res = await api.get('/executor/orders/available')
        availableOrders.value = res.data || []
      } catch (err) {
        console.error('[ExecutorDashboard] failed to fetch available orders:', err)
      }
    }

    const fetchMyActiveOrders = async () => {
      try {
        const res = await api.get('/executor/orders/my-active')
        myActiveOrders.value = res.data || []
      } catch (err) {
        console.error('[ExecutorDashboard] failed to fetch my active orders:', err)
      }
    }

    const acceptOrder = async (orderId: string) => {
      successMsg.value = ''
      errorMsg.value = ''
      acceptingOrder.value = orderId
      try {
        await api.post(`/executor/orders/${orderId}/accept`)
        successMsg.value = t('executor.successOrderAccepted')
        await fetchAvailableOrders()
        await fetchMyActiveOrders()
      } catch (err: any) {
        errorMsg.value = formatApiError(err, t('executor.errorAcceptOrder'))
      } finally {
        acceptingOrder.value = null
      }
    }

    const submitBid = async (orderId: string) => {
      const amount = bidAmounts.value[orderId]
      if (!amount || amount <= 0) {
        errorMsg.value = t('executor.errorInvalidBidAmount')
        return
      }
      successMsg.value = ''
      errorMsg.value = ''
      submittingBid.value = orderId
      try {
        await api.post(`/executor/orders/${orderId}/bids`, { amount })
        successMsg.value = t('executor.successBidSubmitted')
        bidAmounts.value[orderId] = 0
        await fetchAvailableOrders()
      } catch (err: any) {
        errorMsg.value = formatApiError(err, t('executor.errorSubmitBid'))
      } finally {
        submittingBid.value = null
      }
    }

    const completeOrder = async (orderId: string) => {
      successMsg.value = ''
      errorMsg.value = ''
      completingOrder.value = orderId
      try {
        await api.post(`/executor/orders/${orderId}/complete`)
        successMsg.value = t('executor.successOrderCompleted')
        await fetchMyActiveOrders()
        await fetchProfile()
        if (selectedChatOrder.value && selectedChatOrder.value.id === orderId) {
          chatLocked.value = true
        }
      } catch (err: any) {
        errorMsg.value = formatApiError(err, t('executor.errorCompleteOrder'))
      } finally {
        completingOrder.value = null
      }
    }

    const sendLocation = async (lat: number, lon: number) => {
      try {
        await api.post('/executor/location', { lat, lon })
      } catch (err) {
        console.warn('[ExecutorDashboard] failed to send location:', err)
      }
    }

    const updateGpsLocation = async () => {
      if (simulatedLat.value !== null && simulatedLon.value !== null) {
        await sendLocation(simulatedLat.value, simulatedLon.value)
        return
      }
      try {
        const pos = await Geolocation.getCurrentPosition({ enableHighAccuracy: true, timeout: 10000 })
        currentLat.value = pos.coords.latitude
        currentLon.value = pos.coords.longitude
        await sendLocation(pos.coords.latitude, pos.coords.longitude)
      } catch (err) {
        console.warn('[ExecutorDashboard] GPS error:', err)
      }
    }

    let leafletMap: any = null
    let leafletMarker: any = null

    const openMapPicker = () => {
      pickerLat.value = effectiveLat.value || 55.7558
      pickerLon.value = effectiveLon.value || 37.6173
      mapPickerVisible.value = true
      nextTick(() => {
        initLeafletMap()
      })
    }

    const initLeafletMap = () => {
      const L = (window as any).L
      if (!L) return
      const container = document.getElementById('executor-leaflet-map')
      if (!container) return

      if (leafletMap) {
        leafletMap.remove()
        leafletMap = null
      }

      const initialLat = pickerLat.value || 55.7558
      const initialLon = pickerLon.value || 37.6173

      leafletMap = L.map(container).setView([initialLat, initialLon], 13)
      L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
        maxZoom: 19,
      }).addTo(leafletMap)

      leafletMarker = L.marker([initialLat, initialLon], { draggable: true }).addTo(leafletMap)

      leafletMarker.on('dragend', (e: any) => {
        const pos = e.target.getLatLng()
        pickerLat.value = pos.lat
        pickerLon.value = pos.lng
      })

      leafletMap.on('click', (e: any) => {
        pickerLat.value = e.latlng.lat
        pickerLon.value = e.latlng.lng
        leafletMarker.setLatLng(e.latlng)
      })
    }

    const applyMapCoordinates = async () => {
      if (pickerLat.value !== null && pickerLon.value !== null) {
        simulatedLat.value = pickerLat.value
        simulatedLon.value = pickerLon.value
        await sendLocation(simulatedLat.value, simulatedLon.value)
        mapPickerVisible.value = false
      }
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
          console.error('[ExecutorDashboard] Camera capture error/cancel:', err)
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
        console.error('[ExecutorDashboard] file upload failed:', err)
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
        console.warn('[ExecutorDashboard] failed to fetch unread summary:', err)
      }
    }

    const openChatByToast = () => {
      if (chatToast.value?.order) {
        openChat(chatToast.value.order)
        chatToast.value = null
      }
    }

    const openChat = (order: any) => {
      openChatComposable(order, unreadOrderIDs, t('common.customer'))
    }

    const handleLogout = () => {
      authStore.logout()
      router.push('/login')
    }

    const getShiftStatusColor = (s: string) => {
      switch (s) {
        case 'ACTIVE': return 'success'
        case 'FINISHED_NORMAL': return 'secondary'
        case 'FINISHED_EARLY': return 'warning'
        default: return 'info'
      }
    }

    const formatDate = (dateStr?: string) => {
      if (!dateStr) return '-'
      const d = new Date(dateStr)
      return d.toLocaleString([], { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
    }

    const formatDuration = (secs: number) => {
      const h = Math.floor(secs / 3600)
      const m = Math.floor((secs % 3600) / 60)
      const s = secs % 60
      return `${h.toString().padStart(2, '0')}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`
    }

    let pollInterval: any = null

    onMounted(async () => {
      await fetchProfile()
      await fetchActiveShift()
      await fetchAvailableOrders()
      await fetchMyActiveOrders()
      await fetchUnreadSummary()
      await updateGpsLocation()

      pollInterval = setInterval(async () => {
        await fetchActiveShift()
        await fetchAvailableOrders()
        await fetchMyActiveOrders()
        await updateGpsLocation()
      }, 5000)
    })

    onUnmounted(() => {
      if (pollInterval) clearInterval(pollInterval)
      stopShiftTimer()
    })

    return {
      authStore,
      phone,
      status,
      balance,
      currencySymbol,
      successMsg,
      errorMsg,
      activeShift,
      shiftDuration,
      durationOptions,
      startingShift,
      endingShift,
      elapsedSeconds,
      availableOrders,
      myActiveOrders,
      acceptingOrder,
      completingOrder,
      bidAmounts,
      submittingBid,
      showFinancialHistoryModal,
      financialTransactions,
      openFinancialHistoryModal,
      selectedChatOrder,
      chatMessages,
      chatText,
      chatLocked,
      isNative,
      isDebug,
      sendingChat,
      chatError,
      showImagePreviewModal,
      previewImageUrl,
      openImagePreview,
      onPreviewModalImgError,
      getImageSrc,
      onChatImgError,
      isImageAttachment,
      deleteMessage,
      startShift,
      endShift,
      earlyEndShift,
      fetchAvailableOrders,
      fetchMyActiveOrders,
      acceptOrder,
      submitBid,
      completeOrder,
      openChat,
      sendChatMessage,
      closeChat,
      handleLogout,
      getShiftStatusColor,
      formatDate,
      formatDuration,
      mapPickerVisible,
      openMapPicker,
      applyMapCoordinates,
      pickerLat,
      pickerLon,
      effectiveLat,
      effectiveLon,
      isLocationSimulated,
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
    }
  },
})
</script>

<style scoped>
.executor-dashboard {
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

<style>
/* Hide Leaflet map attribution footer control */
.leaflet-control-attribution {
  display: none !important;
}
</style>
