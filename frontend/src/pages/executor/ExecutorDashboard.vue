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
            <span class="balance-amount">${{ Number(balance).toFixed(2) }}</span>
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

          <div class="row g-2 mb-4">
            <div class="col-6">
              <va-input
                v-model.number="latInput"
                type="number"
                step="any"
                :label="$t('executor.latitude')"
                required
              />
            </div>
            <div class="col-6">
              <va-input
                v-model.number="lonInput"
                type="number"
                step="any"
                :label="$t('executor.longitude')"
                required
              />
            </div>
          </div>

          <!-- Quick Presets -->
          <div class="d-flex mb-4">
            <va-button 
              color="info" 
              outline 
              size="small" 
              @click="setCoordinates(51.7916886, 36.1908417)"
              class="mr-2"
            >
              {{ $t('executor.insideGeofence') }}
            </va-button>
            <va-button 
              color="warning" 
              outline 
              size="small" 
              @click="setCoordinates(51.8500, 36.2500)"
            >
              {{ $t('executor.outsideGeofence') }}
            </va-button>
          </div>

          <div class="d-flex mb-4">
            <va-button 
              color="primary" 
              outline 
              size="small" 
              @click="getCurrentLocation"
              class="mr-2"
            >
              <va-icon name="my_location" class="mr-1" /> {{ $t('executor.myLocation') }}
            </va-button>
          </div>

          <va-button block :loading="sendingLocation" @click="sendLocation">
            {{ $t('executor.sendGPSLocation') }}
          </va-button>

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
                    <div class="text-xs text-secondary">{{ order.volume_type }} / {{ order.speed_tariff }}</div>
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
                  <strong>{{ $t('customer.volume') }}:</strong> {{ order.volume_type }}
                </div>
                <div class="col-6">
                  <strong>{{ $t('customer.tariff') }}:</strong> {{ order.speed_tariff }}
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
        <va-form @submit.prevent="sendChatMessage" class="d-flex">
          <va-input 
            v-model="chatText" 
            :placeholder="$t('customer.typeMessage')" 
            class="flex-grow-1 mr-2" 
            :disabled="chatLocked"
            required
          />
          <va-button type="submit" color="primary" :disabled="chatLocked">{{ $t('customer.send') }}</va-button>
        </va-form>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, onMounted, onUnmounted, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Geolocation } from '@capacitor/geolocation'
import { useAuthStore } from '../../stores/auth-store'
import api from '../../services/api'

export default defineComponent({
  name: 'ExecutorDashboard',
  setup() {
    const router = useRouter()
    const { t } = useI18n()
    const authStore = useAuthStore()

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

    // Location Simulator state
    // Default coordinates: Kursk, Generala Grigorova st., 38 (approx.)
    const latInput = ref(51.7305)
    const lonInput = ref(36.1936)
    const sendingLocation = ref(false)
    const telemetryLogs = ref<{time: string, lat: number, lon: number, isInside: boolean}[]>([])

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
    const messagesContainer = ref<any>(null)

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
        if (response.data && response.data.shift_early_exit_penalty) {
          earlyExitPenalty.value = Number(response.data.shift_early_exit_penalty)
        }
      } catch (err) {
        console.error('Failed to fetch public settings:', err)
      }
    }

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

    const setCoordinates = (lat: number, lon: number) => {
      latInput.value = lat
      lonInput.value = lon
    }

    const sendLocation = async () => {
      successMsg.value = ''
      errorMsg.value = ''
      sendingLocation.value = true
      try {
        const response = await api.post('/executor/shifts/location', {
          latitude: Number(latInput.value),
          longitude: Number(lonInput.value),
        })
        const isInside = response.data.is_inside
        
        telemetryLogs.value.unshift({
          time: new Date().toLocaleTimeString(),
          lat: latInput.value,
          lon: lonInput.value,
          isInside: isInside,
        })
        
        if (telemetryLogs.value.length > 5) {
          telemetryLogs.value.pop()
        }

        successMsg.value = t('executor.successLocationSubmitted', { status: isInside ? t('executor.inside') : t('executor.outside') })
        
        await fetchActiveShift()
        await fetchProfile()
      } catch (err: any) {
        errorMsg.value = err.response?.data || t('executor.errorLocationSubmitted')
        console.error(err)
      } finally {
        sendingLocation.value = false
      }
    }

    const requestLocationPermission = async () => {
      try {
        const permission = await Geolocation.requestPermissions()
        return permission.location === 'granted' || permission.location === 'prompt'
      } catch (err) {
        console.error('Failed to request location permission:', err)
        return false
      }
    }

    const getCurrentLocation = async () => {
      try {
        const permission = await Geolocation.requestPermissions()
        if (permission.location !== 'granted') {
          errorMsg.value = 'Location permission is required for this feature'
          return
        }

        const position = await Geolocation.getCurrentPosition({
          enableHighAccuracy: true,
          timeout: 10000,
        })
        latInput.value = position.coords.latitude
        lonInput.value = position.coords.longitude
        successMsg.value = `Location updated: ${latInput.value.toFixed(5)}, ${lonInput.value.toFixed(5)}`
      } catch (err: any) {
        // Fallback to the browser API if the Capacitor plugin is unavailable.
        if (navigator.geolocation) {
          navigator.geolocation.getCurrentPosition(
            (position) => {
              latInput.value = position.coords.latitude
              lonInput.value = position.coords.longitude
              successMsg.value = `Location updated: ${latInput.value.toFixed(5)}, ${lonInput.value.toFixed(5)}`
            },
            (browserErr) => {
              errorMsg.value = `Failed to get location: ${browserErr.message}`
            }
          )
        } else {
          errorMsg.value = err.message || 'Failed to get location'
        }
      }
    }

    const findNearbyOrders = async () => {
      successMsg.value = ''
      errorMsg.value = ''
      searchingNearby.value = true
      try {
        const response = await api.get('/executor/orders/nearby', {
          params: {
            lat: latInput.value,
            lon: lonInput.value,
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
      const wsUrl = `${protocol}//${wsHost}/chats/${order.id}/ws?token=${encodeURIComponent(authStore.token)}`

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

    onMounted(() => {
      fetchProfile()
      fetchSettings()
      fetchActiveShift()
      fetchAssignedOrders()
      fetchAvailableOrders()

      // Request location permission on the executor dashboard so the app can
      // update GPS coordinates and search for nearby orders.
      requestLocationPermission().catch((err) => {
        console.error('Location permission request failed:', err)
      })

      intervalId = setInterval(() => {
        fetchProfile()
        fetchActiveShift()
        fetchAssignedOrders()
        fetchAvailableOrders()
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
      activeShift,
      shiftDuration,
      durationOptions,
      startingShift,
      endingShiftEarly,
      earlyExitPenalty,
      latInput,
      lonInput,
      sendingLocation,
      telemetryLogs,
      assignedOrders,
      availableOrders,
      bidsInputs,
      nearbyOrders,
      searchingNearby,
      getCurrentLocation,
      findNearbyOrders,
      acceptOrder,
      selectedChatOrder,
      chatMessages,
      chatText,
      chatLocked,
      messagesContainer,
      successMsg,
      errorMsg,
      startShift,
      earlyEndShift,
      setCoordinates,
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
</style>
