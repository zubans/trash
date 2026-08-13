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
                                                                                                                                                                                                                                                                                                                                                                                                             <div class="balance-box mt-4 p-3 text-center cursor-pointer hover-shadow transition" @click="openFinancialHistoryModal">
            <span class="balance-label d-block text-secondary text-sm mb-1 d-flex align-items-center justify-content-center">
              <va-icon name="account_balance_wallet" class="mr-1 text-primary" size="small" />
              {{ $t('executor.yourWalletBalance') }}
              <va-icon name="history" class="ml-1 text-secondary" size="small" />
            </span>
            <span class="balance-amount">{{ currencySymbol }}{{ Number(balance).toFixed(2) }}</span>
            <span class="text-xxs text-primary d-block mt-1">({{ $t('common.details') }})</span>
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
              <va-button type="submit" block :loading="startingShift">
                {{ $t('executor.startShift') }}
              </va-button>
            </va-form>
          </div>

          <div v-else class="active-shift-container">
            <div class="info-list mb-4">
              <div class="info-item mb-2">
                <span class="info-label">{{ $t('executor.shiftStatus') }}</span>
                <div class="d-flex align-items-center gap-2">
                  <va-badge :color="getShiftStatusColor(activeShift.status)" class="text-uppercase">
                    {{ activeShift.status }}
                  </va-badge>
                  <span v-if="shiftCountdown" class="font-mono text-xs font-bold text-danger bg-danger-light px-2 py-1 rounded">
                    ⏳ {{ shiftCountdown }}
                  </span>
                </div>
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
                <span class="info-label">{{ $t('executor.plannedEnd') }}</span>
                <span class="info-val text-xs">{{ formatDate(activeShift.planned_end_at) }}</span>
              </div>
              <div class="info-item mb-2">
                <span class="info-label">{{ $t('executor.timeLeft') }}</span>
                <span class="font-bold text-danger text-sm">{{ shiftCountdown || '00:00:00' }}</span>
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
                <span class="info-label">{{ $t('executor.fine') }}</span>
                <span class="info-val text-xs">{{ currencySymbol }}{{ Number(activeShift.fine_amount).toFixed(2) }}</span>
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
              {{ activeShift.actual_end_at ? $t('executor.shiftEndedEarly', { amount: currencySymbol + Number(activeShift.fine_amount).toFixed(2) }) : $t('executor.shiftPenalized') }}
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
                    <strong class="text-primary">{{ currencySymbol }}{{ Number(order.hold_amount).toFixed(2) }}</strong>
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
                    {{ $t('common.customer') }}: {{ order.customer_phone }}
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
                  <strong>{{ $t('executor.payout') }}:</strong> {{ currencySymbol }}{{ Number(order.hold_amount).toFixed(2) }}
                </div>
                <div class="col-6 mt-1" v-if="order.deadline_at">
                  <strong>{{ $t('executor.deadline') }}:</strong> {{ formatDate(order.deadline_at) }}
                </div>
                <div class="col-12 mt-1" v-if="order.photo_url">
                  <strong>{{ $t('customer.photo') }}:</strong> <a :href="order.photo_url" target="_blank" class="text-primary text-xs truncate">{{ order.photo_url }}</a>
                </div>
              </div>

              <div class="d-flex justify-content-between align-items-center mt-3">
                <va-button color="danger" outline size="small" @click="refuseOrder(order)">
                  <va-icon name="block" class="mr-1" /> {{ $t('executor.refuseOrder') }}
                </va-button>
                <va-button color="info" outline size="small" class="position-relative" @click="openChat(order)">
                  <va-icon name="chat" class="mr-1" /> {{ $t('common.chat') }}
                  <span v-if="unreadOrderIDs.has(order.id)" class="yellow-unread-dot"></span>
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

    <!-- Financial & Order History Modal -->
    <va-modal
      v-model="showFinancialHistoryModal"
      :title="$t('executor.financialHistoryTitle')"
      size="large"
      hide-default-actions
    >
      <div class="p-2">
        <va-tabs v-model="historyTab" grow class="mb-4">
          <template #tabs>
            <va-tab name="orders">{{ $t('executor.historyOrdersTab') }}</va-tab>
            <va-tab name="transactions">{{ $t('executor.historyTxsTab') }}</va-tab>
          </template>
        </va-tabs>

        <!-- Tab 1: Orders History -->
        <div v-if="historyTab === 'orders'">
          <div v-if="executorHistoryOrders.length === 0" class="text-center py-4 text-secondary">
            {{ $t('customer.noHistoryOrders') }}
          </div>
          <va-data-table
            v-else
            :items="executorHistoryOrders"
            :columns="historyOrderColumns"
            striped
            hoverable
          >
            <template #cell(id)="{ rowData }">
              <span class="font-bold text-xs">#{{ rowData.id.slice(0, 8) }}</span>
            </template>
            <template #cell(type)="{ rowData }">
              {{ formatOrderType(rowData) }}
            </template>
            <template #cell(amount)="{ rowData }">
              <strong class="text-success">+{{ currencySymbol }}{{ Number(rowData.final_amount || rowData.hold_amount).toFixed(2) }}</strong>
            </template>
            <template #cell(status)="{ value }">
              <va-badge :color="getStatusColor(value)">{{ value }}</va-badge>
            </template>
          </va-data-table>
        </div>

        <!-- Tab 2: Financial Transactions -->
        <div v-else-if="historyTab === 'transactions'">
          <div v-if="executorTransactions.length === 0" class="text-center py-4 text-secondary">
            {{ $t('executor.noHistoryTxs') }}
          </div>
          <va-data-table
            v-else
            :items="executorTransactions"
            :columns="historyTxColumns"
            striped
            hoverable
          >
            <template #cell(type)="{ value }">
              <va-badge :color="getTxTypeColor(value)">{{ formatTxType(value) }}</va-badge>
            </template>
            <template #cell(amount)="{ rowData }">
              <span :class="['font-bold', isPositiveTx(rowData.type) ? 'text-success' : 'text-danger']">
                {{ isPositiveTx(rowData.type) ? '+' : '-' }}{{ currencySymbol }}{{ Number(rowData.amount).toFixed(2) }}
              </span>
            </template>
            <template #cell(created_at)="{ value }">
              <span class="text-xs text-secondary">{{ formatDateFull(value) }}</span>
            </template>
          </va-data-table>
        </div>

        <div class="d-flex justify-content-end mt-4">
          <va-button color="secondary" @click="showFinancialHistoryModal = false">
            {{ $t('common.close') }}
          </va-button>
        </div>
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

    <!-- Sliding Chat Panel (Telegram Style) with Swipe-to-Dismiss -->
    <div
      :class="['chat-panel shadow-lg', { open: selectedChatOrder }]"
      :style="chatPanelStyle"
      @touchstart="handleTouchStart"
      @touchmove="handleTouchMove"
      @touchend="handleTouchEnd"
    >
      <div class="chat-header d-flex align-items-center bg-telegram text-white p-2 px-3">
        <div class="telegram-avatar mr-3">
          {{ (selectedChatOrder?.id?.slice(0, 2) || '').toUpperCase() }}
        </div>
        <div class="flex-grow-1 overflow-hidden">
          <h4 class="m-0 text-white font-bold text-sm truncate">
            {{ $t('customer.orderChatTitle', { id: selectedChatOrder?.id?.slice(0, 8) || '' }) }}
          </h4>
          <span class="text-xxs text-online d-flex align-items-center">
            <span class="online-dot mr-1"></span> {{ $t('executor.chatSubtitle') }}
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
        <div v-else-if="chatError" class="text-center py-2 mb-3 bg-danger-light text-danger rounded-lg text-xs font-semibold shadow-sm">
          {{ chatError }}
        </div>

        <div
          v-for="msg in chatMessages"
          :key="msg.id"
          :class="['telegram-bubble', msg.sender_id === authStore.userID ? 'my-telegram-msg ml-auto' : 'their-telegram-msg mr-auto']"
        >
          <div class="telegram-sender" v-if="msg.sender_id !== authStore.userID">{{ $t('common.customer') }}</div>

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
import { defineComponent, ref, onMounted, onUnmounted, nextTick, computed, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { Geolocation } from '@capacitor/geolocation'
import { Capacitor } from '@capacitor/core'
import { Camera, CameraResultType, CameraSource } from '@capacitor/camera'
import L from 'leaflet'
import { useAuthStore } from '../../stores/auth-store'
import api, { buildChatWebSocketUrl, resolveFileUrl, formatApiError, isDebug } from '../../services/api'
import { NativeWebSocket } from '../../plugins/native-websocket'
import { compressImage } from '../../utils/imageCompressor'
import type { ServiceNode } from '../../api/services'

export default defineComponent({
  name: 'ExecutorDashboard',
  components: {},
  setup() {
    const router = useRouter()
    const { t, locale } = useI18n()
    const authStore = useAuthStore()

    const currencySymbol = computed(() => {
      return authStore.currency === 'RUB' ? '₽' : '$'
    })

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

    // Live countdown timer state
    const shiftCountdown = ref('')
    let countdownIntervalId: any = null

    const updateShiftCountdown = () => {
      if (!activeShift.value || activeShift.value.status !== 'ACTIVE' || !activeShift.value.planned_end_at) {
        shiftCountdown.value = ''
        return
      }
      const plannedEnd = new Date(activeShift.value.planned_end_at).getTime()
      const now = new Date().getTime()
      const diffMs = plannedEnd - now

      if (diffMs <= 0) {
        shiftCountdown.value = '00:00:00'
        fetchActiveShift()
        return
      }

      const totalSec = Math.floor(diffMs / 1000)
      const hours = Math.floor(totalSec / 3600)
      const minutes = Math.floor((totalSec % 3600) / 60)
      const seconds = totalSec % 60

      const pad = (n: number) => n.toString().padStart(2, '0')
      shiftCountdown.value = `${pad(hours)}:${pad(minutes)}:${pad(seconds)}`
    }

    // Financial History & Order History Modal state
    const showFinancialHistoryModal = ref(false)
    const historyTab = ref<'orders' | 'transactions'>('orders')
    const executorHistoryOrders = ref<any[]>([])
    const executorTransactions = ref<any[]>([])

    const historyOrderColumns = [
      { key: 'id', label: 'ID' },
      { key: 'type', label: t('customer.orderType') },
      { key: 'amount', label: t('executor.payout') },
      { key: 'status', label: t('customer.status') },
    ]

    const historyTxColumns = [
      { key: 'type', label: t('executor.txType') },
      { key: 'amount', label: t('executor.txAmount') },
      { key: 'created_at', label: t('executor.txDate') },
    ]

    const openFinancialHistoryModal = async () => {
      showFinancialHistoryModal.value = true
      try {
        const response = await api.get('/executor/history')
        if (response.data) {
          executorHistoryOrders.value = response.data.orders || []
          executorTransactions.value = response.data.transactions || []
        }
      } catch (err) {
        console.error('Failed to fetch executor history:', err)
      }
    }

    const isPositiveTx = (type: string) => {
      return type === 'REWARD' || type === 'TOP_UP' || type === 'REFUND' || type === 'PAYMENT'
    }

    const formatTxType = (type: string) => {
      switch (type) {
        case 'REWARD': return t('executor.reward')
        case 'FINE': return t('executor.fine')
        case 'TOP_UP': return t('executor.topup')
        case 'REFUND': return t('executor.refund')
        case 'HOLD': return t('executor.hold')
        default: return type
      }
    }

    const getTxTypeColor = (type: string) => {
      switch (type) {
        case 'REWARD': return 'success'
        case 'FINE': return 'danger'
        case 'TOP_UP': return 'primary'
        case 'REFUND': return 'info'
        case 'HOLD': return 'warning'
        default: return 'secondary'
      }
    }

    const formatDateFull = (dateStr: string) => {
      if (!dateStr) return ''
      const d = new Date(dateStr)
      return d.toLocaleString([], { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
    }

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

    const refuseOrder = async (order: any) => {
      successMsg.value = ''
      errorMsg.value = ''
      const penalty = Number(order.hold_amount || 0) * 0.5
      const confirmText = t('executor.refuseOrderConfirm', { amount: currencySymbol.value + penalty.toFixed(2) })
      if (!confirm(confirmText)) return

      try {
        await api.post(`/executor/orders/${order.id}/reject`)
        successMsg.value = t('executor.successOrderRefused')
        await fetchProfile()
        await fetchAssignedOrders()
      } catch (err: any) {
        errorMsg.value = formatApiError(err) || t('executor.errorOrderRefused')
      }
    }

    // Touch swipe-to-close handlers for sliding chat panel
    const touchStartX = ref<number | null>(null)
    const touchCurrentX = ref<number | null>(null)

    const handleTouchStart = (e: TouchEvent) => {
      if (e.touches.length === 1) {
        touchStartX.value = e.touches[0].clientX
        touchCurrentX.value = e.touches[0].clientX
      }
    }

    const handleTouchMove = (e: TouchEvent) => {
      if (touchStartX.value !== null && e.touches.length === 1) {
        const currentX = e.touches[0].clientX
        if (currentX > touchStartX.value) {
          touchCurrentX.value = currentX
        }
      }
    }

    const handleTouchEnd = () => {
      if (touchStartX.value !== null && touchCurrentX.value !== null) {
        const deltaX = touchCurrentX.value - touchStartX.value
        if (deltaX > 100) {
          // Swiped right over 100px: close chat
          closeChat()
        }
      }
      touchStartX.value = null
      touchCurrentX.value = null
    }

    const chatPanelStyle = computed(() => {
      if (touchStartX.value !== null && touchCurrentX.value !== null) {
        const deltaX = Math.max(0, touchCurrentX.value - touchStartX.value)
        return {
          transform: `translateX(${deltaX}px)`,
          transition: 'none'
        }
      }
      return {}
    })

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
      const confirmed = confirm(t('executor.endShiftEarlyConfirm', { amount: currencySymbol.value + Number(earlyExitPenalty.value).toFixed(2) }))
      if (!confirmed) return

      endingShiftEarly.value = true
      try {
        const response = await api.post('/executor/shifts/early-end')
        activeShift.value = response.data
        successMsg.value = t('executor.shiftEndedEarly', { amount: currencySymbol.value + Number(activeShift.value.fine_amount).toFixed(2) })
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
        // Do not set successMsg for silent background location sync to avoid spamming the UI alert
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
          console.error('[ExecutorDashboard] Camera capture error/cancel:', err)
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
        console.warn('[ExecutorDashboard] fetch blob fallback failed for:', fullUrl, err)
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

    const markChatAsRead = async (orderID: string) => {
      unreadOrderIDs.value.delete(orderID)
      try {
        await api.post(`/chats/${orderID}/read`)
      } catch (err) {
        console.warn('[ExecutorDashboard] mark read failed:', err)
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
        console.error('[ExecutorDashboard] failed to delete message:', err)
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
      wsConnected.value = false
      chatError.value = ''

      // Mark unread dot as read
      markChatAsRead(order.id)

      // Load history (with timeout so the native HTTP bridge can't stall forever).
      try {
        const response = await api.get(`/chats/${order.id}/messages`, {
          params: { _t: Date.now() },
          headers: { 'Cache-Control': 'no-cache, no-store, must-revalidate' },
          timeout: 4000,
        })
        chatMessages.value = response.data || []
        scrollToBottom()
      } catch (err) {
        console.error('[ExecutorDashboard] failed to load chat history:', err)
        chatError.value = t('executor.errorChatHistory')
      }

      // Open websocket (Native OkHttp WebSocket on Android for 100% bypass of WebView restrictions and self-signed TLS support).
      const wsUrl = buildChatWebSocketUrl(order.id, authStore.token)

      if (isNative) {
        try {
          await NativeWebSocket.disconnect()
          await NativeWebSocket.addListener('onOpen', () => {
            wsConnected.value = true
            chatError.value = ''
            sendDeliveryAck()
            markChatAsRead(order.id)
          })
          await NativeWebSocket.addListener('onMessage', (res) => {
            if (!res || !res.data) return
            const data = JSON.parse(res.data)
            handleIncomingChatMessage(data, order)
          })
          await NativeWebSocket.addListener('onError', (err) => {
            console.warn('[NativeWebSocket] error:', err)
          })
          await NativeWebSocket.connect({ url: wsUrl })
        } catch (nativeErr) {
          console.warn('[ExecutorDashboard] NativeWebSocket connection error:', nativeErr)
        }
      } else {
        if (ws.value) {
          ws.value.close()
          ws.value = null
        }
        ws.value = new WebSocket(wsUrl)
        ws.value.onopen = () => {
          wsConnected.value = true
          chatError.value = ''
          sendDeliveryAck()
          markChatAsRead(order.id)
        }
        ws.value.onerror = () => {
          chatError.value = t('executor.errorChatConnection')
        }
        ws.value.onclose = () => {
          wsConnected.value = false
        }
        ws.value.onmessage = (event) => {
          const data = JSON.parse(event.data)
          handleIncomingChatMessage(data, order)
        }
      }

      scheduleChatPoll(order.id)
    }

    const sendChatMessage = async (event?: Event) => {
      if (event) {
        event.preventDefault()
        event.stopPropagation()
      }
      const text = chatText.value.trim()
      if (!text || chatLocked.value || !selectedChatOrder.value) return

      const orderID = selectedChatOrder.value.id

      // Primary path: send over NativeWebSocket on native, or Web WebSocket on web
      if (isNative) {
        try {
          await NativeWebSocket.send({ message: JSON.stringify({ text }) })
          chatText.value = ''
          chatError.value = ''
          return
        } catch (err) {
          console.warn('[ExecutorDashboard] NativeWebSocket send failed, falling back to REST:', err)
        }
      } else if (ws.value && ws.value.readyState === WebSocket.OPEN) {
        try {
          ws.value.send(JSON.stringify({ text }))
          chatText.value = ''
          chatError.value = ''
          return
        } catch (err) {
          console.warn('[ExecutorDashboard] ws.send failed, falling back to HTTP:', err)
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

    // Native polling fallback helper to guarantee messages always arrive
    // even when WebView WS socket drops or is throttled.
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
        console.warn('[ExecutorDashboard] poll chat messages failed:', err)
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
      return `${hours} ч. ${minutes} мин.`
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
      await fetchUnreadSummary()

      // Request location permission and fetch current coordinates.
      await checkLocationPermission()
      if (locationPermission.value === 'granted') {
        await updateCurrentPosition(true)
      }

      updateShiftCountdown()
      countdownIntervalId = setInterval(updateShiftCountdown, 1000)

      intervalId = setInterval(() => {
        fetchProfile()
        fetchActiveShift()
        fetchAssignedOrders()
        fetchAvailableOrders()
        fetchUnreadSummary()
      }, 5000)
    })

    onUnmounted(() => {
      if (intervalId) clearInterval(intervalId)
      if (countdownIntervalId) clearInterval(countdownIntervalId)
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
      currencySymbol,
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
      shiftCountdown,
      showFinancialHistoryModal,
      historyTab,
      executorHistoryOrders,
      executorTransactions,
      historyOrderColumns,
      historyTxColumns,
      openFinancialHistoryModal,
      isPositiveTx,
      formatTxType,
      getTxTypeColor,
      formatDateFull,
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
      refuseOrder,
      handleTouchStart,
      handleTouchMove,
      handleTouchEnd,
      chatPanelStyle,
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
      unreadOrderIDs,
      chatToast,
      openChatByToast,
      showImagePreviewModal,
      previewImageUrl,
      openImagePreview,
      getImageSrc,
      onChatImgError,
      deleteMessage,
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
      isDebug,
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

.telegram-sender {
  font-size: 12px;
  font-weight: 700;
  color: #6bb4e8;
  margin-bottom: 2px;
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

/* Hide Leaflet map attribution footer control */
.leaflet-control-attribution {
  display: none !important;
}
</style>
