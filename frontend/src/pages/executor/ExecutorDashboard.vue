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
        <va-card class="p-4 mb-4 shadow-card">
          <h3 class="va-h5 mb-4 text-primary d-flex align-items-center">
            <va-icon name="account_circle" class="mr-2" /> {{ $t('executor.accountDetails') }}
          </h3>
          <div class="info-list">
            <div class="info-item mb-3">
              <span class="info-label">{{ $t('customer.phone') }}</span>
              <span class="info-val">{{ phone }}</span>
            </div>
            <div class="info-item mb-3">
              <span class="info-label">{{ $t('customer.status') }}</span>
              <span class="info-val">
                <va-badge color="success">{{ status }}</va-badge>
              </span>
            </div>
          </div>
                                                                                                                                                                                                                                                                                                                                                                                                             <div class="balance-box mt-4 p-3 text-center">
            <span class="balance-label d-block text-secondary text-sm mb-1">{{ $t('executor.yourWalletBalance') }}</span>
            <span class="balance-amount">{{ Number(balance).toFixed(2) }} РУБ</span>
          </div>
        </va-card>

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
                <span class="info-val">{{ activeShift.duration_hours }} hours</span>
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
                <span class="info-val text-xs">${{ Number(activeShift.fine_amount).toFixed(2) }}</span>
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
              <va-button type="submit" block :loading="startingShift">
                {{ $t('executor.startShift') }}
              </va-button>
            </va-form>
          </div>

          <div v-else class="active-shift-container">
            <div class="info-list mb-4">
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
                <span class="info-val">{{ activeShift.duration_hours }} hours</span>
              </div>
              <div class="info-item mb-2">
                <span class="info-label">{{ $t('executor.startedAt') }}</span>
                <span class="info-val text-xs">{{ formatDate(activeShift.started_at) }}</span>
              </div>
              <div class="info-item mb-2">
                <span class="info-label">{{ $t('executor.plannedEnd') }}</span>
                <span class="info-val text-xs">{{ formatDate(activeShift.planned_end_at) }}</span>
              </div>
              <div v-if="activeShift.actual_end_at" class="info-item mb-2">
                <span class="info-label">{{ $t('executor.actualEnd') }}</span>
                <span class="info-val text-xs">{{ formatDate(activeShift.actual_end_at) }}</span>
              </div>
              <div v-if="activeShift.status === 'ACTIVE'" class="info-item mb-2">
                <span class="info-label">{{ $t('executor.elapsed') }}</span>
                <span class="info-val text-xs">{{ formatDuration(activeShift.started_at) }}</span>
              </div>
              <div v-if="activeShift.fine_amount > 0" class="info-item mb-2">
                <span class="info-label">{{ $t('executor.fine') || 'Штраф' }}</span>
                <span class="info-val text-xs">${{ Number(activeShift.fine_amount).toFixed(2) }}</span>
              </div>
            </div>

            <va-button
              v-if="activeShift.status === 'ACTIVE'"
              color="warning"
              block
              :loading="endingShiftEarly"
              class="mb-3"
              @click="earlyEndShift"
            >
              {{ $t('executor.endShiftEarly') }}
            </va-button>

            <va-alert v-if="activeShift.status === 'PENALIZED'" color="danger" class="mb-0">
              {{ activeShift.actual_end_at ? $t('executor.shiftEndedEarly', { amount: '$' + Number(activeShift.fine_amount).toFixed(2) }) : $t('executor.shiftPenalized') }}
            </va-alert>
          </div>
        </va-card>
      </div>

      <!-- Right Column: GPS Simulator, Assigned Orders, & Auctions Bidding -->
      <div class="col-md-7">
        <!-- Telemetry Simulator -->
        <va-card v-if="activeShift && activeShift.status === 'ACTIVE'" class="p-4 mb-4 shadow-card">
          <h3 class="va-h5 mb-4 text-primary d-flex align-items-center">
            <va-icon name="gps_fixed" class="mr-2" /> {{ $t('executor.gpsTelemetrySimulator') }}
          </h3>
          <p class="text-secondary text-sm mb-4">
            {{ $t('executor.gpsTelemetryDescription') }}
          </p>

          <!-- Current location status -->
          <div class="bg-light rounded p-3 mb-4">
            <div class="d-flex justify-content-between align-items-center mb-2">
              <span class="text-secondary text-sm">{{ $t('executor.currentCoordinates') }}</span>
              <va-badge
                :color="locationPermission === 'granted' ? 'success' : locationPermission === 'denied' ? 'danger' : 'warning'"
                size="small"
              >
                {{ locationPermission === 'granted' ? 'OK' : locationPermission === 'denied' ? t('executor.locationPermissionDenied') : '...' }}
              </va-badge>
            </div>
            <div class="font-mono text-sm">
              <span v-if="currentLat !== null && currentLon !== null">
                {{ currentLat.toFixed(5) }}, {{ currentLon.toFixed(5) }}
              </span>
              <span v-else class="text-secondary">—</span>
            </div>
            <div v-if="lastSentAt" class="text-xs text-secondary mt-1">
              {{ $t('executor.lastSentAt') }}: {{ formatTime(lastSentAt) }}
            </div>
          </div>

          <!-- Manual coordinates fallback -->
          <div v-if="locationPermission === 'denied'" class="mb-4">
            <va-button
              color="primary"
              outline
              block
              @click="openMapPicker"
            >
              <va-icon name="map" class="mr-2" /> {{ $t('executor.showOnMap') }}
            </va-button>
          </div>
          <div class="mb-4">
            <va-button
              color="secondary"
              outline
              block
              @click="openMapPicker"
            >
              <va-icon name="edit_location" class="mr-2" /> {{ $t('executor.specifyCoordinatesManually') }}
            </va-button>
          </div>

          <!-- Nearby Orders -->
          <div class="mt-4 pt-4 border-top">
            <h4 class="va-h6 text-secondary mb-2">{{ $t('executor.nearbyOrders') }}</h4>
            <va-button
              color="success"
              outline
              size="small"
              :loading="searchingNearby"
              @click="findNearbyOrders"
              class="mb-3"
            >
              {{ $t('executor.findNearbyOrders') }}
            </va-button>

            <div v-if="nearbyOrders.length === 0" class="text-xs text-secondary text-center py-2">
              {{ $t('executor.noAvailableOrders') }}
            </div>
            <div v-else class="nearby-orders-list">
              <va-card
                v-for="order in nearbyOrders"
                :key="order.id"
                class="order-item-card p-2 mb-2"
                outlined
              >
                <div class="d-flex justify-content-between align-items-start">
                  <div>
                    <div class="font-bold text-sm">#{{ order.id.slice(0, 8) }}</div>
                    <div class="text-xs text-secondary">{{ formatOrderType(order) }}</div>
                    <div class="text-xs text-secondary" v-if="order.address">{{ order.address }}</div>
                  </div>
                  <div class="text-right">
                    <strong class="text-primary">${{ Number(order.hold_amount).toFixed(2) }}</strong>
                    <va-button
                      color="success"
                      size="small"
                      class="d-block mt-1"
                      @click="acceptOrder(order.id)"
                    >
                      {{ $t('executor.acceptOrder') }}
                    </va-button>
                  </div>
                </div>
              </va-card>
            </div>
          </div>

          <!-- Local logs list of sent coordinates -->
          <div v-if="telemetryLogs.length > 0" class="mt-4">
            <h4 class="va-h6 text-secondary mb-2">{{ $t('executor.recentTelemetrySent') }}</h4>
            <div class="telemetry-log-list p-2 bg-light rounded text-xs">
              <div
                v-for="(log, idx) in telemetryLogs"
                :key="idx"
                class="d-flex justify-content-between align-items-center mb-1 py-1 border-bottom"
              >
                <span>{{ log.time }}: {{ log.lat.toFixed(4) }}, {{ log.lon.toFixed(4) }}</span>
                <va-badge :color="log.isInside ? 'success' : 'danger'" size="small">
                  {{ log.isInside ? 'INSIDE' : 'OUTSIDE' }}
                </va-badge>
              </div>
            </div>
          </div>
        </va-card>

        <!-- Map picker modal -->
        <va-modal
          v-model="mapPickerVisible"
          :title="$t('executor.specifyCoordinatesManually')"
          hide-default-actions
          size="large"
        >
          <div id="executor-map" class="executor-map"></div>
          <template #footer>
            <div class="d-flex justify-content-end gap-3">
              <va-button color="secondary" outline @click="mapPickerVisible = false">
                {{ $t('common.cancel') }}
              </va-button>
              <va-button color="primary" @click="applyMapCoordinates">
                {{ $t('common.save') }}
              </va-button>
            </div>
          </template>
        </va-modal>

        <!-- Assigned Orders -->
        <va-card class="p-4 mb-4 shadow-card">
          <h3 class="va-h5 mb-4 text-primary d-flex align-items-center">
            <va-icon name="assignment" class="mr-2" /> {{ $t('executor.assignedOrders') }}
          </h3>

          <div v-if="assignedOrders.length === 0" class="text-center py-5">
            <va-icon name="hourglass_empty" size="large" color="secondary" class="mb-3 spin-slow" />
            <p class="text-secondary mb-0">{{ $t('executor.waitingForMatching') }}</p>
            <span class="text-xs text-secondary">{{ $t('executor.keepShiftActive') }}</span>
          </div>

          <div v-else class="orders-list">
            <va-card 
              v-for="order in assignedOrders" 
              :key="order.id" 
              class="order-item-card p-3 mb-3"
              outlined
            >
              <div class="d-flex justify-content-between align-items-start mb-2">
                <div>
                  <span class="font-bold text-sm">{{ $t('customer.orderId') }}{{ order.id.slice(0, 8) }}...</span>
                  <span v-if="order.is_downgraded" class="ml-2">
                    <va-badge color="danger">{{ $t('customer.slaDowngraded') }}</va-badge>
                  </span>
                  <div class="text-xs text-secondary mt-1">
                    Customer: {{ order.customer_phone }}
                  </div>
                </div>
                <va-badge color="info">
                  {{ order.status }}
                </va-badge>
              </div>

              <div class="row text-sm mt-3">
                <div class="col-6">
                  <strong>{{ $t('customer.serviceType') }}:</strong> {{ order.service_variant ? localizedName(order.service_variant) : order.service_variant_id }}
                </div>
                <div class="col-6" v-if="order.is_urgent || order.is_asap">
                  <strong>{{ $t('customer.urgent') }}:</strong> {{ order.is_asap ? $t('customer.asap') : $t('customer.urgent') }}
                </div>
                <div class="col-6 mt-1">
                  <strong>{{ $t('executor.payout') }}:</strong> ${{ Number(order.hold_amount).toFixed(2) }}
                </div>
                <div class="col-6 mt-1" v-if="order.deadline_at">
                  <strong>{{ $t('executor.deadline') }}:</strong> {{ formatDate(order.deadline_at) }}
                </div>
                <div class="col-12 mt-1" v-if="order.photo_url">
                  <strong>{{ $t('customer.photo') }}:</strong> <a :href="order.photo_url" target="_blank" class="text-primary text-xs truncate">{{ order.photo_url }}</a>
                </div>
              </div>

              <div class="d-flex justify-content-end mt-3">
                <va-button color="info" outline size="small" @click="openChat(order)">
                  <va-icon name="chat" class="mr-1" /> {{ $t('common.chat') }}
                </va-button>
              </div>
            </va-card>
          </div>
        </va-card>

        <!-- Available Construction Auctions -->
        <va-card class="p-4 shadow-card">
          <h3 class="va-h5 mb-4 text-primary d-flex align-items-center">
            <va-icon name="gavel" class="mr-2" /> {{ $t('executor.openConstructionAuctions') }}
          </h3>
          <p class="text-secondary text-sm mb-4">
            {{ $t('executor.bidDescription') }}
          </p>

          <div v-if="availableOrders.length === 0" class="text-center py-5">
            <va-icon name="explore" size="large" color="secondary" class="mb-3" />
            <p class="text-secondary mb-0">{{ $t('executor.noAvailableOrders') }}</p>
          </div>

          <div v-else class="orders-list">
            <va-card 
              v-for="order in availableOrders" 
              :key="order.id" 
              class="order-item-card p-3 mb-3"
              outlined
            >
              <div class="d-flex justify-content-between align-items-start mb-2">
                <div>
                  <span class="font-bold text-sm">{{ $t('customer.orderId') }}{{ order.id.slice(0, 8) }}...</span>
                  <div class="text-xs text-secondary mt-1">
                    Customer: {{ order.customer_phone }}
                  </div>
                </div>
                <va-badge color="warning">{{ $t('orderStatus.SEARCHING') }}</va-badge>
              </div>

              <div class="row text-sm mt-3">
                <div class="col-12" v-if="order.photo_url">
                  <strong>{{ $t('customer.photo') }}:</strong> <a :href="order.photo_url" target="_blank" class="text-primary text-xs truncate">{{ order.photo_url }}</a>
                </div>
              </div>

              <!-- Place bid form -->
              <div class="bid-form mt-3 p-3 bg-light rounded" v-if="activeShift && activeShift.status === 'ACTIVE'">
                <h5 class="text-xs font-bold text-secondary mb-2">{{ $t('executor.offerYourPrice') }}</h5>
                <va-form @submit.prevent="submitBid(order.id)" class="d-flex align-items-center">
                  <va-input
                    v-model.number="bidsInputs[order.id]"
                    type="number"
                    min="1"
                    placeholder="300"
                    class="flex-grow-1 mr-2"
                    required
                  >
                    <template #prependInner>
                      <va-icon name="attach_money" />
                    </template>
                  </va-input>
                  <va-button type="submit" size="small">{{ $t('executor.placeBid') }}</va-button>
                </va-form>
              </div>
              <div v-else class="text-xs text-danger text-center mt-3 py-1 bg-light rounded">
                {{ $t('executor.startShiftToBid') }}
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
          <h4 class="m-0 text-white font-bold text-sm">{{ $t('customer.orderChatTitle', { id: selectedChatOrder?.id.slice(0, 8) }) }}</h4>
          <span class="text-xs opacity-75">{{ $t('executor.chatSubtitle') }}</span>
        </div>
        <va-button color="warning" size="small" flat @click="closeChat">{{ $t('common.close') }}</va-button>
      </div>

      <div class="chat-messages p-3 flex-grow-1" ref="messagesContainer">
        <div v-if="chatLocked" class="text-center py-2 mb-3 bg-danger-light text-danger rounded text-xs">
          {{ $t('customer.chatLocked') }}
        </div>
        <div v-else-if="chatError" class="text-center py-2 mb-3 bg-danger-light text-danger rounded text-xs">
          {{ chatError }}
        </div>

        <div
          v-for="msg in chatMessages"
          :key="msg.id"
          :class="['message-bubble mb-2 p-2 rounded', msg.sender_id === authStore.userID ? 'my-message ml-auto bg-primary text-white' : 'their-message mr-auto bg-light']"
        >
          <div class="text-xs opacity-75 mb-1" v-if="msg.sender_id !== authStore.userID">{{ $t('common.customer') }}</div>
          <div class="text-sm message-text">{{ msg.text }}</div>
          <div class="text-xxs text-right mt-1 opacity-75">{{ formatTime(msg.created_at) }}</div>
        </div>
      </div>

      <div class="chat-input-area p-3 bg-white border-top">
        <div class="d-flex">
          <input
            v-model="chatText"
            :placeholder="$t('customer.typeMessage')"
            class="flex-grow-1 mr-2 p-2"
            style="border: 1px solid #cbd5e0; border-radius: 4px;"
            @keyup.enter="sendChatMessage"
          />
          <button
            type="button"
            style="padding: 8px 16px; background: #2c82e0; color: white; border: none; border-radius: 4px;"
            @click="sendChatMessage"
          >
            {{ sendingChat ? '...' : $t('customer.send') }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, onMounted, onUnmounted, nextTick, computed, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Geolocation } from '@capacitor/geolocation'
import { Capacitor } from '@capacitor/core'
import L from 'leaflet'
import { useAuthStore } from '../../stores/auth-store'
import api, { buildChatWebSocketUrl, formatApiError } from '../../services/api'
import type { ServiceNode } from '../../api/services'

export default defineComponent({
  name: 'ExecutorDashboard',
  components: {},
  setup() {
    const router = useRouter()
    const { t, locale } = useI18n()
    const authStore = useAuthStore()

    const localizedName = (node?: ServiceNode) =>
      node?.name[locale.value] || node?.name['ru'] || node?.code || ''

    const formatOrderType = (order: any) => {
      const variant = order.service_variant
      if (!variant) return order.service_variant_id
      const name = localizedName(variant)
      if (order.is_asap) return `${name} (${t('customer.asap')})`
      if (order.is_urgent) return `${name} (${t('customer.urgent')})`
      return name
    }

    const phone = ref('')
    const balance = ref(0)
    const status = ref('ACTIVE')

    // Shift state
    const activeShift = ref<any>(null)
    const shiftDuration = ref(1)
    const durationOptions = [1, 3, 5]
    const startingShift = ref(false)
    const endingShiftEarly = ref(false)
    const earlyExitPenalty = ref(50)

    // Automatic GPS location state
    const currentLat = ref<number | null>(null)
    const currentLon = ref<number | null>(null)
    const locationPermission = ref<'granted' | 'denied' | 'prompt' | null>(null)
    const sendingLocation = ref(false)
    const lastSentAt = ref<string | null>(null)
    const telemetryLogs = ref<{time: string, lat: number, lon: number, isInside: boolean}[]>([])
    const locationSendIntervalSeconds = ref(5)
    let locationIntervalId: any = null
    let locationWatchId: any = null

    // Map picker state
    const mapPickerVisible = ref(false)
    const mapInstance = ref<L.Map | null>(null)
    const mapMarker = ref<L.Marker | null>(null)
    const manualLat = ref<number | null>(null)
    const manualLon = ref<number | null>(null)

    // Assigned Orders
    const assignedOrders = ref<any[]>([])

    // Open/Searching Construction Orders
    const availableOrders = ref<any[]>([])
    const bidsInputs = ref<Record<string, number>>({})

    // Nearby standard/large orders
    const nearbyOrders = ref<any[]>([])
    const searchingNearby = ref(false)

    // Chat state
    const selectedChatOrder = ref<any>(null)
    const chatMessages = ref<any[]>([])
    const chatText = ref('')
    const chatLocked = ref(false)
    const ws = ref<WebSocket | null>(null)
    const wsConnected = ref(false)
    const chatError = ref('')
    const sendingChat = ref(false)
    const messagesContainer = ref<any>(null)
    let chatPollIntervalId: any = null

    const successMsg = ref('')
    const errorMsg = ref('')

    const fetchProfile = async () => {
      try {
        const response = await api.get('/customer/profile')
        if (response.data) {
          phone.value = response.data.phone
          balance.value = response.data.balance
          status.value = response.data.status
        }
      } catch (err) {
        console.error('Failed to load executor profile:', err)
      }
    }

    const fetchActiveShift = async () => {
      try {
        const response = await api.get('/executor/shifts/active')
        activeShift.value = response.data || null
      } catch (err) {
        console.error('Failed to fetch active shift:', err)
      }
    }

    const fetchAssignedOrders = async () => {
      try {
        const response = await api.get('/executor/orders/assigned')
        assignedOrders.value = response.data || []
      } catch (err) {
        console.error('Failed to fetch assigned orders:', err)
      }
    }

    const fetchAvailableOrders = async () => {
      try {
        const response = await api.get('/executor/orders/available')
        availableOrders.value = response.data || []
        
        // Populate default bids if empty
        availableOrders.value.forEach(order => {
          if (!bidsInputs.value[order.id]) {
            bidsInputs.value[order.id] = 300
          }
        })
      } catch (err) {
        console.error('Failed to fetch available orders:', err)
      }
    }

    const startShift = async () => {
      successMsg.value = ''
      errorMsg.value = ''
      startingShift.value = true
      try {
        const response = await api.post('/executor/shifts', {
          duration_hours: Number(shiftDuration.value),
        })
        successMsg.value = t('executor.successShiftStarted')
        activeShift.value = response.data
        await fetchAssignedOrders()
      } catch (err: any) {
        errorMsg.value = err.response?.data || t('executor.errorShiftStarted')
        console.error(err)
      } finally {
        startingShift.value = false
      }
    }

    const fetchSettings = async () => {
      try {
        const response = await api.get('/settings')
        if (response.data) {
          if (response.data.shift_early_exit_penalty) {
            earlyExitPenalty.value = Number(response.data.shift_early_exit_penalty)
          }
          const interval = Number(response.data.executor_location_send_interval_seconds)
          if (!isNaN(interval) && interval >= 1) {
            locationSendIntervalSeconds.value = interval
          }
        }
      } catch (err) {
        console.error('Failed to fetch public settings:', err)
      }
    }

    watch(activeShift, (shift) => {
      if (shift?.status === 'ACTIVE') {
        startLocationPolling()
      } else {
        stopLocationPolling()
      }
    }, { immediate: true })

    watch(locationSendIntervalSeconds, () => {
      if (activeShift.value?.status === 'ACTIVE') {
        startLocationPolling()
      }
    })

    const earlyEndShift = async () => {
      successMsg.value = ''
      errorMsg.value = ''
      const confirmed = confirm(t('executor.endShiftEarlyConfirm', { amount: 'РУБ' + Number(earlyExitPenalty.value).toFixed(2) }))
      if (!confirmed) return

      endingShiftEarly.value = true
      try {
        const response = await api.post('/executor/shifts/early-end')
        activeShift.value = response.data
        successMsg.value = t('executor.shiftEndedEarly', { amount: 'РУБ' + Number(activeShift.value.fine_amount).toFixed(2) })
        await fetchProfile()
      } catch (err: any) {
        errorMsg.value = err.response?.data || t('executor.errorShiftStarted')
        console.error(err)
      } finally {
        endingShiftEarly.value = false
      }
    }

    const effectiveLat = computed(() => manualLat.value ?? currentLat.value)
    const effectiveLon = computed(() => manualLon.value ?? currentLon.value)

    const checkLocationPermission = async () => {
      try {
        if (Capacitor.isNativePlatform()) {
          const permission = await Geolocation.requestPermissions()
          locationPermission.value = permission.location as any
          return permission.location === 'granted'
        }
        // Browser: rely on getCurrentPosition success/failure.
        return true
      } catch (err) {
        console.error('Failed to check location permission:', err)
        locationPermission.value = 'denied'
        return false
      }
    }

    const updateCurrentPosition = async (silent = false) => {
      try {
        if (Capacitor.isNativePlatform()) {
          const position = await Geolocation.getCurrentPosition({
            enableHighAccuracy: true,
            timeout: 10000,
          })
          currentLat.value = position.coords.latitude
          currentLon.value = position.coords.longitude
          locationPermission.value = 'granted'
        } else if (navigator.geolocation) {
          const position = await new Promise<GeolocationPosition>((resolve, reject) => {
            navigator.geolocation.getCurrentPosition(resolve, reject, {
              enableHighAccuracy: true,
              timeout: 10000,
            })
          })
          currentLat.value = position.coords.latitude
          currentLon.value = position.coords.longitude
          locationPermission.value = 'granted'
        }
      } catch (err: any) {
        if (!silent) {
          errorMsg.value = err.message || t('executor.errorLocationSubmitted')
        }
        if (Capacitor.isNativePlatform()) {
          locationPermission.value = 'denied'
        }
        console.error('Failed to get current position:', err)
      }
    }

    const sendLocation = async () => {
      const lat = effectiveLat.value
      const lon = effectiveLon.value
      if (lat == null || lon == null) {
        console.warn('[ExecutorDashboard] skip sendLocation: coordinates unavailable')
        return
      }
      if (activeShift.value?.status !== 'ACTIVE') {
        return
      }

      successMsg.value = ''
      errorMsg.value = ''
      sendingLocation.value = true
      try {
        const response = await api.post('/executor/shifts/location', {
          latitude: Number(lat),
          longitude: Number(lon),
        })
        const isInside = response.data?.is_inside ?? true

        telemetryLogs.value.unshift({
          time: new Date().toLocaleTimeString(),
          lat,
          lon,
          isInside,
        })
        if (telemetryLogs.value.length > 5) {
          telemetryLogs.value.pop()
        }

        lastSentAt.value = new Date().toISOString()
        successMsg.value = t('executor.successLocationSubmitted', {
          status: isInside ? t('executor.inside') : t('executor.outside'),
        })
      } catch (err: any) {
        errorMsg.value = err.response?.data || t('executor.errorLocationSubmitted')
        console.error(err)
      } finally {
        sendingLocation.value = false
      }
    }

    const startLocationPolling = async () => {
      await updateCurrentPosition(true)
      await sendLocation()

      if (locationIntervalId) {
        clearInterval(locationIntervalId)
      }
      const intervalMs = Math.max(1000, Number(locationSendIntervalSeconds.value) * 1000)
      locationIntervalId = setInterval(async () => {
        await updateCurrentPosition(true)
        await sendLocation()
      }, intervalMs)
    }

    const stopLocationPolling = () => {
      if (locationIntervalId) {
        clearInterval(locationIntervalId)
        locationIntervalId = null
      }
      if (locationWatchId) {
        // Capacitor geolocation watch is not used here; interval polling is used instead.
        locationWatchId = null
      }
    }

    const openMapPicker = () => {
      const lat = effectiveLat.value ?? 51.7305
      const lon = effectiveLon.value ?? 36.1936
      manualLat.value = lat
      manualLon.value = lon
      mapPickerVisible.value = true
      nextTick(() => {
        if (!mapInstance.value) {
          mapInstance.value = L.map('executor-map').setView([lat, lon], 14)
          L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
            attribution: '&copy; OpenStreetMap contributors',
          }).addTo(mapInstance.value)
          mapInstance.value.on('click', (e: L.LeafletMouseEvent) => {
            manualLat.value = e.latlng.lat
            manualLon.value = e.latlng.lng
            updateMapMarker()
          })
        } else {
          mapInstance.value.setView([lat, lon], 14)
        }
        updateMapMarker()
      })
    }

    const updateMapMarker = () => {
      if (!mapInstance.value || manualLat.value == null || manualLon.value == null) return
      if (mapMarker.value) {
        mapMarker.value.setLatLng([manualLat.value, manualLon.value])
      } else {
        mapMarker.value = L.marker([manualLat.value, manualLon.value]).addTo(mapInstance.value)
      }
    }

    const applyMapCoordinates = () => {
      if (manualLat.value != null && manualLon.value != null) {
        currentLat.value = null
        currentLon.value = null
        mapPickerVisible.value = false
      }
    }

    const findNearbyOrders = async () => {
      successMsg.value = ''
      errorMsg.value = ''
      const lat = effectiveLat.value
      const lon = effectiveLon.value
      if (lat == null || lon == null) {
        errorMsg.value = t('executor.errorLocationSubmitted')
        return
      }
      searchingNearby.value = true
      try {
        const response = await api.get('/executor/orders/nearby', {
          params: {
            lat,
            lon,
            radius: 2000,
          },
        })
        nearbyOrders.value = response.data || []
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Failed to fetch nearby orders'
        console.error(err)
      } finally {
        searchingNearby.value = false
      }
    }

    const acceptOrder = async (orderId: string) => {
      successMsg.value = ''
      errorMsg.value = ''
      try {
        await api.post(`/executor/orders/${orderId}/accept`)
        successMsg.value = 'Order accepted successfully!'
        await fetchAssignedOrders()
        await findNearbyOrders()
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Failed to accept order'
        console.error(err)
      }
    }

    const submitBid = async (orderId: string) => {
      successMsg.value = ''
      errorMsg.value = ''
      const price = bidsInputs.value[orderId]
      try {
        await api.post(`/executor/orders/${orderId}/bids`, {
          offered_price: Number(price),
        })
        successMsg.value = `Placed bid of $${price} successfully!`
      } catch (err: any) {
        errorMsg.value = err.response?.data || t('executor.errorBidSubmitted')
        console.error(err)
      }
    }

    const isNative = Capacitor.isNativePlatform()

    // Chat operations
    const openChat = async (order: any) => {
      selectedChatOrder.value = order
      chatMessages.value = []
      chatLocked.value = false
      wsConnected.value = false
      chatError.value = ''

      // Load history (with timeout so the native HTTP bridge can't stall forever).
      try {
        chatMessages.value = await fetchChatMessages(order.id)
        scrollToBottom()
      } catch (err) {
        console.error('[ExecutorDashboard] failed to load chat history:', err)
        chatError.value = t('executor.errorChatHistory')
      }

      // Open websocket. The URL is resolved from the active API base URL so that
      // native Android builds use the plain HTTP mobile port (8089) while the
      // web build continues to use the HTTPS port (8080).
      const wsUrl = buildChatWebSocketUrl(order.id, authStore.token)

      if (ws.value) {
        ws.value.close()
        ws.value = null
      }

      ws.value = new WebSocket(wsUrl)
      ws.value.onopen = () => {
        wsConnected.value = true
        chatError.value = ''
      }
      ws.value.onerror = () => {
        chatError.value = t('executor.errorChatConnection')
      }
      ws.value.onclose = () => {
        wsConnected.value = false
      }
      ws.value.onmessage = (event) => {
        const data = JSON.parse(event.data)
        if (data.type === 'system' && data.action === 'lock') {
          chatLocked.value = true
        } else if (data.type === 'system' && data.action === 'downgrade') {
          // Live SLA tariff downgrade sync
          order.is_urgent = data.is_urgent
          order.is_asap = data.is_asap
          order.final_amount = data.final_amount
          order.is_downgraded = true
        } else if (data.type === 'error') {
          console.warn(data.message)
        } else {
          const exists = chatMessages.value.some((m: any) => m.id === data.id)
          if (!exists) {
            chatMessages.value.push(data)
            scrollToBottom()
          }
        }
      }

      // Native polling fallback: pull history on a recursive timer so incoming
      // messages show up even if the WebSocket bridge is broken in the WebView.
      // Start polling IMMEDIATELY (first poll now, then every 3s) so messages
      // arrive without waiting for the first timer tick.
      if (isNative) {
        scheduleChatPoll(order.id, /* immediate= */ true)
      }
    }

    const sendChatMessage = async (event?: Event) => {
      if (event) {
        event.preventDefault()
        event.stopPropagation()
      }
      const text = chatText.value.trim()
      if (!text || chatLocked.value || !selectedChatOrder.value) return

      const orderID = selectedChatOrder.value.id

      // Primary path: send over WebSocket. The server broadcasts the saved
      // message back to all room clients (including this one), so the sender's
      // own message appears via onmessage with a stable id (deduped).
      if (ws.value && ws.value.readyState === WebSocket.OPEN) {
        try {
          ws.value.send(JSON.stringify({ text }))
          chatText.value = ''
          chatError.value = ''
          return
        } catch (err) {
          console.warn('[ExecutorDashboard] ws.send failed, falling back to HTTP:', err)
          // fall through to HTTP fallback below
        }
      }

      // Fallback path: REST endpoint (used when WS is not connected yet or
      // unavailable, e.g. on mobile WebViews where the bridge swallows ws.send).
      const url = `/chats/${orderID}/messages`
      sendingChat.value = true
      chatError.value = ''
      try {
        const response = await api.post(url, { text })
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
        const fallback = t('executor.errorChatConnection')
        chatError.value = formatApiError(err, fallback)
      } finally {
        sendingChat.value = false
      }
    }

    // Fetch chat history with cache-busting so mobile WebViews/CapacitorHttp
    // never return a stale response. The timeout guarantees the polling promise
    // always settles (resolves or rejects) even when the native HTTP bridge
    // stalls, so the recursive setTimeout never gets stuck.
    const fetchChatMessages = async (orderID: string) => {
      const response = await api.get(`/chats/${orderID}/messages`, {
        params: { _t: Date.now() },
        headers: { 'Cache-Control': 'no-cache, no-store, must-revalidate' },
        timeout: 5000,
      })
      return response.data || []
    }

    const pollChatMessages = async (orderID: string) => {
      if (!selectedChatOrder.value) return
      try {
        const incoming = await fetchChatMessages(orderID)
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
        // Silent: polling errors should not spam the UI
        console.warn('[ExecutorDashboard] poll chat messages failed:', err)
      }
    }

    const scheduleChatPoll = (orderID: string, immediate = false) => {
      if (chatPollIntervalId) clearTimeout(chatPollIntervalId)
      const tick = async () => {
        await pollChatMessages(orderID)
        // Re-schedule only while the chat panel for this order is still open.
        if (selectedChatOrder.value && selectedChatOrder.value.id === orderID) {
          chatPollIntervalId = setTimeout(tick, 3000)
        }
      }
      if (immediate) {
        // First poll right away so the user sees new messages without a 3s delay.
        tick()
      } else {
        chatPollIntervalId = setTimeout(tick, 3000)
      }
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
      wsConnected.value = false
      chatError.value = ''
    }

    const scrollToBottom = () => {
      nextTick(() => {
        if (messagesContainer.value) {
          messagesContainer.value.scrollTop = messagesContainer.value.scrollHeight
        }
      })
    }

    const getShiftStatusColor = (status: string) => {
      switch (status) {
        case 'ACTIVE': return 'success'
        case 'PENALIZED': return 'danger'
        case 'COMPLETED': return 'secondary'
        default: return 'primary'
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

    const formatDuration = (dateStr: string) => {
      if (!dateStr) return ''
      const start = new Date(dateStr).getTime()
      const diff = Date.now() - start
      if (diff < 0) return ''
      const hours = Math.floor(diff / 3600000)
      const minutes = Math.floor((diff % 3600000) / 60000)
      return `${hours}h ${minutes}m`
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

    onMounted(async () => {
      fetchProfile()
      await fetchSettings()
      await fetchActiveShift()
      fetchAssignedOrders()
      fetchAvailableOrders()

      // Request location permission and fetch current coordinates.
      await checkLocationPermission()
      if (locationPermission.value === 'granted') {
        await updateCurrentPosition(true)
      }

      intervalId = setInterval(() => {
        fetchProfile()
        fetchActiveShift()
        fetchAssignedOrders()
        fetchAvailableOrders()
      }, 5000)
    })

    onUnmounted(() => {
      if (intervalId) clearInterval(intervalId)
      if (chatPollIntervalId) {
        clearTimeout(chatPollIntervalId)
        chatPollIntervalId = null
      }
      stopLocationPolling()
      if (ws.value) {
        ws.value.close()
        ws.value = null
      }
      if (mapInstance.value) {
        mapInstance.value.remove()
        mapInstance.value = null
      }
    })

    return {
      authStore,
      phone,
      balance,
      status,
      activeShift,
      shiftDuration,
      durationOptions,
      startingShift,
      endingShiftEarly,
      earlyExitPenalty,
      currentLat,
      currentLon,
      locationPermission,
      sendingLocation,
      lastSentAt,
      telemetryLogs,
      assignedOrders,
      availableOrders,
      bidsInputs,
      nearbyOrders,
      searchingNearby,
      findNearbyOrders,
      acceptOrder,
      localizedName,
      formatOrderType,
      selectedChatOrder,
      chatMessages,
      chatText,
      chatLocked,
      wsConnected,
      chatError,
      sendingChat,
      messagesContainer,
      successMsg,
      errorMsg,
      startShift,
      earlyEndShift,
      sendLocation,
      submitBid,
      openChat,
      sendChatMessage,
      closeChat,
      getShiftStatusColor,
      formatDate,
      formatTime,
      formatDuration,
      handleLogout,
      mapPickerVisible,
      openMapPicker,
      applyMapCoordinates,
      effectiveLat,
      effectiveLon,
    }
  },
})
</script>

<style scoped>
.executor-dashboard {
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

.order-item-card {
  border: 1px solid #e2e8f0;
  border-radius: 8px;
  background: #ffffff;
  transition: all 0.2s ease;
}

.order-item-card:hover {
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.05);
}

.bg-light {
  background-color: #f8fafc;
}

.bg-danger-light {
  background-color: #fff5f5;
}

.bg-warning-light {
  background-color: #fffbeb;
}

.text-warning {
  color: #d97706;
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
  border-radius: 16px 16px 2px 16px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
}

.their-message {
  border-radius: 16px 16px 16px 2px;
  background-color: #e8f0fe !important;
  color: #1a1a2e !important;
  border: 1px solid #c4d8f0;
  border-left: 3px solid #4a90d9;
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.08);
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

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.spin-slow {
  animation: spin 8s linear infinite;
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
.text-secondary {
  color: #718096;
}

.font-mono {
  font-family: ui-monospace, SFMono-Regular, "SF Mono", Consolas, "Liberation Mono", Menlo, monospace;
}

.executor-map {
  width: 100%;
  height: 400px;
  border-radius: 8px;
  z-index: 1;
}

.gap-3 {
  gap: 12px;
}
</style>
