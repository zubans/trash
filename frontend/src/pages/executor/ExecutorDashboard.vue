<template>
  <div class="premium-dashboard-page">
    <div class="container">
      <!-- Шапка -->
      <header class="header">
        <div class="logo">
          <AppLogo />
        </div>
        <button type="button" class="hamburger-btn position-relative" title="Меню" @click="menuOpen = true">
          <i class="ph-bold ph-list"></i>
          <span v-if="hasUnreadSupport" class="support-unread-dot"></span>
        </button>
      </header>

      <!-- --- Сайдбар (выдвижное меню) --- -->
      <div :class="['sidebar-overlay', { open: menuOpen }]" @click="menuOpen = false"></div>
      <aside :class="['sidebar', { open: menuOpen }]">
        <div class="sidebar-header">
          <h2>Меню</h2>
          <button type="button" class="close-btn" title="Закрыть" @click="menuOpen = false">
            <i class="ph-bold ph-x"></i>
          </button>
        </div>
        <nav class="sidebar-nav">
          <button type="button" class="nav-item position-relative" @click="menuOpen = false; openSupportChat()">
            <i class="ph-fill ph-headset"></i> Поддержка
            <span v-if="hasUnreadSupport" class="support-unread-dot nav-dot"></span>
          </button>
          <button type="button" class="nav-item" @click="menuOpen = false; $router.push('/executor/profile')">
            <i class="ph-fill ph-user-circle"></i> Профиль
          </button>
          <div v-if="authStore.switchableRoles.length > 1" class="lang-control">
            <span>Роль</span>
            <RoleSwitcher />
          </div>
          <div class="lang-control">
            <span>Язык приложения</span>
            <LanguageSwitcher />
          </div>
          <button type="button" class="nav-item logout" @click="menuOpen = false; handleLogout()">
            <i class="ph-bold ph-sign-out"></i> Выйти из аккаунта
          </button>
        </nav>
      </aside>

      <!-- Toast Container -->
      <div class="toast-container">
        <!-- Chat / Support Toast Notification -->
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

      <!-- Сетка Профиль и Кошелек -->
      <div class="premium-grid">
        <!-- Компактный профиль исполнителя -->
        <div class="profile-card clickable-profile" @click="$router.push('/executor/profile')">
          <div class="avatar-wrap">
            <div class="avatar"><i class="ph ph-user"></i></div>
            <div class="status-dot"></div>
          </div>
          <div class="profile-info">
            <div class="profile-phone-row">
              <div class="profile-phone">{{ phone || '79997454656' }}</div>
              <div v-if="isVerified" class="verified-badge" title="Верифицирован"><i class="ph-fill ph-check-circle"></i></div>
            </div>
            <div v-if="fullName" class="profile-fullname">{{ fullName }}</div>
            <div class="badge-brand">
              <i class="ph-fill ph-user font-bold"></i> Мой профиль
            </div>
          </div>
        </div>

        <!-- Компактный баланс исполнителя -->
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
            <button type="button" class="btn-balance" @click="openWithdrawalModal">
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
                  <div class="o-title item-title">
                    <span class="font-bold text-base">{{ getOrderTitles(order).category }}</span>
                    <span class="text-xs text-muted ms-1">({{ getOrderTitles(order).variant }})</span>
                  </div>
                  <div v-if="order.address" class="item-subtitle"><i class="ph-fill ph-map-pin me-1"></i>{{ order.address }}</div>
                  <div v-if="order.comment" class="item-comment-line"><i class="ph-fill ph-chat-teardrop-text me-1 text-primary"></i>«{{ order.comment }}»</div>
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
                  :class="['btn-action primary chat-btn position-relative', { active: selectedChatOrder && selectedChatOrder.id === order.id }]"
                  @click="toggleChat(order)"
                >
                  <i class="ph-fill ph-chat-circle-dots"></i>
                  <span v-if="unreadOrderIDs.has(order.id)" class="yellow-unread-dot"></span>
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
                      <span v-if="msg.sender_id === currentUserId" class="msg-status-icon" :title="getMessageStatusTitle(msg.status)">
                        <i v-if="msg.status === 'read'" class="ph-bold ph-checks read-receipt"></i>
                        <i v-else-if="msg.status === 'delivered'" class="ph-bold ph-checks delivered-receipt"></i>
                        <i v-else class="ph-bold ph-check sent-receipt"></i>
                      </span>
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
            <div class="order-summary list-item-compact review">
              <div class="item-left-group cursor-pointer" @click="openOrderDetails(order)">
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
                  <span v-if="unreadOrderIDs.has(order.id)" class="yellow-unread-dot"></span>
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
                      <span v-if="msg.sender_id === currentUserId" class="msg-status-icon" :title="getMessageStatusTitle(msg.status)">
                        <i v-if="msg.status === 'read'" class="ph-bold ph-checks read-receipt"></i>
                        <i v-else-if="msg.status === 'delivered'" class="ph-bold ph-checks delivered-receipt"></i>
                        <i v-else class="ph-bold ph-check sent-receipt"></i>
                      </span>
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
        </div>

        <!-- Интерактивный виджет карты заказов поблизости -->
        <div class="map-widget mb-3" title="Открыть карту" @click="openMapPicker">
          <!-- Анимация геопозиции -->
          <div class="map-user-dot"></div>
          <!-- Фейковые метки заказов -->
          <div class="map-order-pin p1"><i class="ph-bold ph-package"></i></div>
          <div class="map-order-pin p2"><i class="ph-bold ph-package"></i></div>

          <!-- Градиент и текст -->
          <div class="map-overlay">
            <div class="map-content-row">
              <div class="map-text">
                <div class="map-title">Карта заказов</div>
                <div class="map-subtitle"><i class="ph-fill ph-navigation-arrow"></i> Геопозиция активна</div>
              </div>
              <button type="button" class="btn-expand-map" @click.stop="openMapPicker">
                <i class="ph-bold ph-map-trifold"></i> Карта
              </button>
            </div>
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
                  <div class="o-title item-title">
                    <span class="font-bold text-base">{{ getOrderTitles(order).category }}</span>
                    <span class="text-xs text-muted ms-1">({{ getOrderTitles(order).variant }})</span>
                  </div>
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

    <!-- Withdrawal Modal -->
    <div v-if="showWithdrawalModal" class="topup-modal-overlay" @click.self="showWithdrawalModal = false">
      <div class="topup-modal-card">
        <div class="topup-modal-header">
          <div class="topup-modal-title">Запрос на вывод средств</div>
          <button type="button" class="btn-close-topup" aria-label="Закрыть" @click="showWithdrawalModal = false">
            <i class="ph ph-x"></i>
          </button>
        </div>

        <form @submit.prevent="submitWithdrawal">
          <div class="form-group mb-4">
            <label class="form-label">Сумма вывода</label>
            <div class="input-wrapper">
              <input
                v-model.number="withdrawalAmount"
                type="number"
                class="form-input"
                min="1"
                :max="balance ?? 0"
                required
              />
              <i class="ph ph-currency-rub input-icon"></i>
            </div>
            <div class="quick-amounts">
              <button type="button" class="amount-pill" @click="withdrawalAmount = (Number(withdrawalAmount) || 0) + 500">+ 500 ₽</button>
              <button type="button" class="amount-pill" @click="withdrawalAmount = (Number(withdrawalAmount) || 0) + 1000">+ 1 000 ₽</button>
              <button type="button" class="amount-pill" :disabled="!balanceLoaded" @click="withdrawalAmount = balance ?? 0">Всё ({{ Number(balance ?? 0).toFixed(2) }} ₽)</button>
            </div>
          </div>

          <button type="submit" class="btn-submit-topup" :disabled="submittingWithdrawal || !balanceLoaded || withdrawalAmount <= 0 || withdrawalAmount > (balance ?? 0)">
            <span v-if="submittingWithdrawal" class="spinner-sm"></span>
            <template v-else>
              Отправить заявку <i class="ph-bold ph-paper-plane-tilt"></i>
            </template>
          </button>
        </form>
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

    <!-- Executor Profile Modal -->
    <ExecutorProfileModal
      v-model="showProfileModal"
      :phone="phone"
      :full-name="fullName"
      :user-email="userEmail"
      :status="status"
      :base-address="baseAddress"
      @email-updated="userEmail = $event"
      @address-updated="baseAddress = $event"
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
    <!-- Modal Поддержка -->
    <SupportChatModal v-model:show="showSupportChatModal" />
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { Capacitor } from '@capacitor/core'
import { Camera, CameraResultType, CameraSource } from '@capacitor/camera'
import { cameraPromptLabels } from '../../utils/cameraLabels'
import { Geolocation } from '@capacitor/geolocation'
import { useAuthStore } from '../../stores/auth-store'
import UpdateBanner from '../../components/UpdateBanner.vue'
import LanguageSwitcher from '../../components/LanguageSwitcher.vue'
import RoleSwitcher from '../../components/RoleSwitcher.vue'
import AppLogo from '../../components/AppLogo.vue'
import OrderDetailsModal from '../customer/components/OrderDetailsModal.vue'
import ReviewModal from '../customer/components/ReviewModal.vue'
import ExecutorMapModal from './components/ExecutorMapModal.vue'
import ExecutorProfileModal from './components/ExecutorProfileModal.vue'
import SupportChatModal from '../../components/SupportChatModal.vue'
import api, { buildChatWebSocketUrl, resolveFileUrl, pollIntervalMs, getRefreshToken } from '../../services/api'
import { checkMyOrderReview, type OrderReview } from '../../api/review'
import { compressImage } from '../../utils/imageCompressor'
import { getServiceVariants, type ServiceNode } from '../../api/services'

export default defineComponent({
  name: 'ExecutorDashboard',
  components: {
    UpdateBanner,
    LanguageSwitcher,
    RoleSwitcher,
    AppLogo,
    ExecutorMapModal,
    ExecutorProfileModal,
    OrderDetailsModal,
    ReviewModal,
    SupportChatModal,
  },
  setup() {
    const router = useRouter()
    const authStore = useAuthStore()

    const phone = ref('')
    const userEmail = ref('')
    const fullName = ref('')
    const baseAddress = ref('')
    // The balance is not kept here: it lives in the auth store, so every screen
    // shows the same value and a refresh benefits all of them at once. null
    // means "not loaded yet" and renders as a placeholder rather than 0.
    const balance = computed(() => authStore.balance)
    const balanceLoaded = computed(() => authStore.balance !== null)
    // The template has always rendered a "verified" badge on this value, and it
    // was never defined anywhere: the badge could not appear for anyone.
    const isVerified = computed(() => authStore.user?.is_verified ?? false)
    const status = ref('ACTIVE')
    const showProfileModal = ref(false)
    const menuOpen = ref(false)

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
    const isHistoryCollapsed = ref(true)

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
        // Refusing an assigned order is fined.
        authStore.fetchMe()
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

    // Withdrawal Modal state
    const showWithdrawalModal = ref(false)
    const withdrawalAmount = ref(0)
    const submittingWithdrawal = ref(false)

    // Chat State & Logic
    const selectedChatOrder = ref<any>(null)
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
        console.warn('[ExecutorDashboard] failed to fetch unread summary:', err)
      }
    }

    const openChatByToast = () => {
      if (!chatToast.value) return
      const isSupp = chatToast.value.isSupport
      const orderID = chatToast.value.orderID
      closeToast()
      if (isSupp) {
        openSupportChat()
      } else if (orderID) {
        const order = assignedOrders.value.find((o: any) => o.id === orderID) ||
                      executorHistoryOrders.value.find((o: any) => o.id === orderID)
        if (order) toggleChat(order)
      }
    }

    const markChatAsRead = async (orderID: string) => {
      unreadOrderIDs.value.delete(orderID)
      try {
        await api.post(`/chats/${orderID}/read`)
      } catch (err) {
        console.warn('[ExecutorDashboard] mark read failed:', err)
      }
      if (ws.value && ws.value.readyState === WebSocket.OPEN) {
        try {
          ws.value.send(JSON.stringify({ type: 'read_ack' }))
        } catch (e) {}
      }
    }

    const sendDeliveryAck = () => {
      if (ws.value && ws.value.readyState === WebSocket.OPEN) {
        try {
          ws.value.send(JSON.stringify({ type: 'delivery_ack' }))
        } catch (e) {}
      }
    }

    const getMessageStatusTitle = (status?: string) => {
      if (status === 'read') return 'Прочитано'
      if (status === 'delivered') return 'Доставлено, но не прочитано'
      return 'Отправлено'
    }

    const openWithdrawalModal = () => {
      withdrawalAmount.value = (balance.value ?? 0) > 0 ? (balance.value as number) : 0
      showWithdrawalModal.value = true
    }

    const submitWithdrawal = async () => {
      if (withdrawalAmount.value <= 0 || withdrawalAmount.value > (balance.value ?? 0) || submittingWithdrawal.value) return
      submittingWithdrawal.value = true
      try {
        await api.post('/finances/withdrawals', { amount: withdrawalAmount.value })
        successMsg.value = 'Заявка на вывод средства отправлена администратору!'
        showWithdrawalModal.value = false
        authStore.fetchMe()
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Ошибка отправки заявки на вывод'
      } finally {
        submittingWithdrawal.value = false
      }
    }

    const hasUnreadSupport = ref(false)
    const lastSupportMsgText = ref('')
    const showExecutorMapModal = ref(false)
    const showSupportChatModal = ref(false)

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
                id: 'support',
                title: 'Служба поддержки',
                text: lastMsg,
                isSupport: true,
              })
            }
          }
        }
      } catch (err) {}
    }

    watch([showOrderDetailsModal, showReviewModal, showWithdrawalModal, showExecutorMapModal, showImagePreviewModal], (modalStates) => {
      const isAnyModalOpen = modalStates.some(state => state === true)
      if (isAnyModalOpen) {
        document.body.style.overflow = 'hidden'
      } else {
        document.body.style.overflow = ''
      }
    })

    const currentUserId = computed(() => authStore.userID)

    const fetchProfile = async () => {
      // One call, one source of truth: the store fetches /auth/me and every
      // consumer of the balance updates with it.
      const me = await authStore.fetchMe()
      if (me) {
        phone.value = me.phone || phone.value
        userEmail.value = me.email || ''
        status.value = me.status || status.value
        fullName.value = authStore.fullName
      }
      try {
        // The executor's position comes from /executor/location, which is the
        // authoritative stored one — the same the map centres on and matching
        // measures against. It used to be parsed out of the profile's last_geo
        // string, which no longer exists.
        const locRes = await api.get('/executor/location')
        if (locRes.data?.has_location) {
          const lat = Number(locRes.data.lat)
          const lon = Number(locRes.data.lon)
          if (!isNaN(lat) && !isNaN(lon)) {
            currentLat.value = lat
            currentLon.value = lon
          }
        }
        const custProfRes = await api.get('/customer/profile')
        if (custProfRes.data && custProfRes.data.address) {
          baseAddress.value = custProfRes.data.address
        }
      } catch (err) {
        // Profile extras (base address, last position) are optional: failing to
        // load them must not disturb what is already on screen.
        console.warn('[ExecutorDashboard] failed to load profile extras', err)
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
        // Ending a shift early carries a penalty, so the balance just changed.
        authStore.fetchMe()
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
        // The server anchors the search to the executor's stored working
        // position; lat/lon are sent only as a fallback. `radius` (not
        // `radius_meters`) is the name the backend reads — otherwise it silently
        // falls back to a 2 km default.
        const res = await api.get('/executor/orders/nearby', {
          params: { lat: currentLat.value, lon: currentLon.value, radius: 5000 },
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
        const rawOrders = res.data?.orders || res.data || []
        executorHistoryOrders.value = rawOrders.slice().sort((a: any, b: any) => {
          const dateA = new Date(a.completed_at || a.canceled_at || a.created_at).getTime()
          const dateB = new Date(b.completed_at || b.canceled_at || b.created_at).getTime()
          return dateB - dateA
        })
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
        const rawErr = err.response?.data
        errorMsg.value = typeof rawErr === 'string' ? rawErr : (rawErr?.error || 'Ошибка принятия заказа')
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

    // Location reporting. POST /executor/shifts/location keeps the executor's
    // stored position fresh: it is what the map centres on and what automatic
    // matching measures distance against, so without it both work from the
    // position captured at registration.
    //
    // It stays off until an administrator enables tracking, because reporting a
    // position continuously is a decision about the executor's privacy and
    // battery, not a side effect of shipping the plumbing. The geofence it was
    // originally built for is gone; the setting name is kept so existing
    // installations do not silently change behaviour.
    const geofenceTrackingEnabled = ref(false)
    const geofenceIntervalSec = ref(60)
    let geofenceTimer: any = null

    const reportShiftLocation = async () => {
      if (!geofenceTrackingEnabled.value) return
      if (!activeShift.value || activeShift.value.status !== 'ACTIVE') return

      await updateCurrentPosition()
      if (currentLat.value === null || currentLon.value === null) return

      try {
        await api.post('/executor/shifts/location', {
          latitude: currentLat.value,
          longitude: currentLon.value,
        })
      } catch (err) {
        // A missed point is not worth interrupting the shift over; the next
        // tick tries again.
        console.warn('[geofence] failed to report location', err)
      }
    }

    const startGeofenceReporting = () => {
      stopGeofenceReporting()
      if (!geofenceTrackingEnabled.value) return
      geofenceTimer = setInterval(reportShiftLocation, geofenceIntervalSec.value * 1000)
    }

    const stopGeofenceReporting = () => {
      if (geofenceTimer) {
        clearInterval(geofenceTimer)
        geofenceTimer = null
      }
    }

    const updateCurrentPosition = async (force = false) => {
      try {
        if (Capacitor.isNativePlatform()) {
          const pos = await Geolocation.getCurrentPosition({
            enableHighAccuracy: false,
            timeout: 5000,
            maximumAge: 30000,
          })
          if (pos && pos.coords) {
            currentLat.value = pos.coords.latitude
            currentLon.value = pos.coords.longitude
            if (force) fetchAvailableOrders()
          }
        } else if (navigator.geolocation) {
          navigator.geolocation.getCurrentPosition(
            (pos) => {
              currentLat.value = pos.coords.latitude
              currentLon.value = pos.coords.longitude
              if (force) fetchAvailableOrders()
            },
            (err) => console.warn('[Geolocation web error]:', err),
            { timeout: 5000, maximumAge: 30000 }
          )
        }
      } catch (err) {
        console.warn('[Geolocation native error]:', err)
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
        markChatAsRead(order.id)
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
          const wsUrl = buildChatWebSocketUrl(orderId)
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
            ...cameraPromptLabels(),
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

    const getOrderTitles = (order: any) => {
      const variantName = formatOrderType(order)
      return { category: '', variant: variantName }
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
      return 'Заказ'
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
        await api.post('/logout', { refresh_token: getRefreshToken() })
      } catch (e) {
        console.error(e)
      } finally {
        authStore.logout()
        router.push('/login')
      }
    }

    let intervalId: any = null

    const onVisibilityChange = () => {
      if (document.visibilityState !== 'visible') return
      authStore.fetchMe()
      fetchActiveShift()
      fetchAssignedOrders()
      fetchUnreadSummary()
    }

    const loadGeofenceSettings = async () => {
      try {
        const res = await api.get('/settings')
        geofenceTrackingEnabled.value = res.data?.geofence_tracking_enabled === '1'
        const interval = Number(res.data?.executor_location_send_interval_seconds)
        if (Number.isFinite(interval) && interval >= 1) {
          geofenceIntervalSec.value = interval
        }
      } catch (err) {
        // Defaults keep reporting off, which is the safe direction.
        console.warn('[geofence] failed to read settings', err)
      }
    }

    onMounted(async () => {
      fetchServiceVariants()
      await loadGeofenceSettings()
      startGeofenceReporting()
      await fetchProfile()
      fetchActiveShift()
      fetchAssignedOrders()
      fetchAvailableOrders()
      fetchHistoryOrders()
      updateCurrentPosition()
      fetchUnreadSummary()
      checkSupportNotification()

      countdownIntervalId = setInterval(updateShiftCountdown, 1000)
      intervalId = setInterval(() => {
        fetchActiveShift()
        fetchAssignedOrders()
        fetchAvailableOrders()
        fetchUnreadSummary()
        checkSupportNotification()
        // The balance moves without any action from this screen: an order the
        // customer confirms, a fine, an approved withdrawal. Poll it with the
        // rest instead of leaving a number from screen-open time on display.
        authStore.fetchMe()
      }, pollIntervalMs)

      // A backgrounded WebView stops its timers, so whatever was on screen when
      // the app was suspended is stale on return.
      document.addEventListener('visibilitychange', onVisibilityChange)
    })

    onUnmounted(() => {
      stopGeofenceReporting()
      document.removeEventListener('visibilitychange', onVisibilityChange)
      closeInlineChat()
      if (intervalId) clearInterval(intervalId)
      if (countdownIntervalId) clearInterval(countdownIntervalId)
      if (successTimer) clearTimeout(successTimer)
      if (errorTimer) clearTimeout(errorTimer)
    })

    return {
      authStore,
      menuOpen,
      balanceLoaded,
      isVerified,
      showProfileModal,
      userEmail,
      baseAddress,
      phone,
      fullName,
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
      showSupportChatModal,
      hasUnreadSupport,
      openSupportChat,
      showOrderDetailsModal,
      selectedOrderDetails,
      openOrderDetails,
      rejectAssignedOrder,
      showReviewModal,
      reviewTargetOrderId,
      openReviewModal,
      onReviewSubmitted,
      showWithdrawalModal,
      withdrawalAmount,
      submittingWithdrawal,
      openWithdrawalModal,
      submitWithdrawal,
      unreadOrderIDs,
      chatToast,
      closeToast,
      fetchUnreadSummary,
      markChatAsRead,
      sendDeliveryAck,
      getMessageStatusTitle,
      openChatByToast,
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
      getOrderTitles,
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

/* Toast Notifications Styles */
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
  background: rgba(255, 255, 255, 0.95);
  backdrop-filter: blur(16px);
  -webkit-backdrop-filter: blur(16px);
  border: 1px solid rgba(255, 255, 255, 0.9);
  border-radius: 16px;
  padding: 16px 20px;
  width: 340px;
  max-width: calc(100vw - 48px);
  display: flex;
  align-items: flex-start;
  gap: 16px;
  box-shadow: 0 10px 30px -10px rgba(15, 23, 42, 0.15),
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
  font-size: 16px;
  flex-shrink: 0;
}

.toast-content {
  flex: 1;
  min-width: 0;
}

.toast-title {
  font-size: 14px;
  font-weight: 700;
  color: var(--text-title, #0f172a);
  margin-bottom: 2px;
}

.toast-message {
  font-size: 13px;
  color: var(--text-muted, #64748b);
  line-height: 1.4;
  word-break: break-word;
}

.toast-close {
  background: none;
  border: none;
  color: var(--text-muted, #94a3b8);
  font-size: 16px;
  cursor: pointer;
  padding: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: color 0.2s ease;
}

.toast-close:hover {
  color: var(--text-title, #0f172a);
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
.app-logo-icon { width: 28px; height: 28px; border-radius: 8px; object-fit: contain; }
.logo i { color: #5c60f5; font-size: 24px; }
.header-controls { display: flex; gap: 8px; align-items: center; }
.control-icon {
  width: 36px; height: 36px; background: #ffffff; border: 1px solid rgba(0,0,0,0.05); border-radius: 12px;
  display: flex; align-items: center; justify-content: center; font-size: 18px; color: var(--text-muted, #64748b);
  cursor: pointer; transition: all 0.2s ease;
}
.control-icon:hover { color: var(--text-title, #0f172a); border-color: rgba(0,0,0,0.1); }

/* --- Гамбургер + выдвижной сайдбар (по аналогии с заказчиком) --- */
.hamburger-btn {
  width: 44px; height: 44px; background: #ffffff; border: 1px solid rgba(0,0,0,0.05); border-radius: 14px;
  display: flex; align-items: center; justify-content: center; font-size: 22px; color: var(--text-title, #0f172a);
  cursor: pointer; transition: all 0.2s ease; box-shadow: 0 2px 10px rgba(15,23,42,0.04);
}
.hamburger-btn:hover { border-color: rgba(0,0,0,0.12); box-shadow: 0 4px 14px rgba(15,23,42,0.08); }
.hamburger-btn:active { transform: scale(0.95); }
.hamburger-btn .support-unread-dot { top: 8px; right: 8px; }

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
  /* The menu scrolls rather than being clipped by the fixed-height sidebar:
     min-height lets this flex child shrink below its content, and contained
     overscroll keeps the page behind the overlay still. */
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
.nav-item i { font-size: 22px; color: #6366f1; }
.nav-item:hover { background: #f8fafc; }
.nav-item:active { background: #f1f5f9; }
.nav-item.logout { color: #ef4444; margin-top: auto; }
.nav-item.logout i { color: #ef4444; }
.nav-item .nav-dot { top: 14px; left: 34px; right: auto; }

.lang-control {
  display: flex; justify-content: space-between; align-items: center;
  padding: 14px 16px; background: #f8fafc; border-radius: 16px; margin: 6px 0;
}
.lang-control span { font-weight: 700; font-size: 14px; color: var(--text-title, #0f172a); }

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

.btn-map-edit {
  background: var(--brand-light, #eef2ff);
  color: var(--brand-primary, #5c60f5);
  border: 1px solid rgba(92, 96, 245, 0.2);
  padding: 8px 14px;
  border-radius: 12px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 6px;
  white-space: nowrap;
  transition: var(--transition, all 0.2s ease);
}

.btn-map-edit:hover {
  background: var(--brand-primary, #5c60f5);
  color: #ffffff;
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

/* =========================================================
   МИНИ-КАРТА (Интерактивный виджет)
   ========================================================= */
.map-widget {
    position: relative; width: 100%; height: 130px; border-radius: 20px;
    overflow: hidden; box-shadow: var(--shadow-card, 0 4px 20px rgba(0, 0, 0, 0.04)); cursor: pointer; transition: all 0.2s ease-in-out;
    /* A stock photo of a map used to be fetched from images.unsplash.com on
       every render of this widget. It is decoration behind an overlay that
       covers most of it, so it is drawn locally: no third-party request, and
       nothing to allow through the content security policy. */
    background-color: #e2e8f0;
    background-image:
        linear-gradient(135deg, rgba(99, 102, 241, 0.10), rgba(16, 185, 129, 0.10)),
        repeating-linear-gradient(0deg, rgba(148, 163, 184, 0.25) 0 1px, transparent 1px 34px),
        repeating-linear-gradient(90deg, rgba(148, 163, 184, 0.25) 0 1px, transparent 1px 34px);
}
.map-widget:hover { transform: translateY(-2px); box-shadow: 0 8px 24px rgba(0,0,0,0.08); }

.map-overlay {
    position: absolute; inset: 0;
    background: linear-gradient(to top, rgba(255,255,255,1) 0%, rgba(255,255,255,0.7) 40%, rgba(255,255,255,0) 100%);
    display: flex; flex-direction: column; justify-content: flex-end; padding: 16px;
}

/* Имитация метки пользователя */
.map-user-dot {
    position: absolute; top: 40%; left: 50%; transform: translate(-50%, -50%);
    width: 16px; height: 16px; background: var(--brand-primary, #5c60f5);
    border: 3px solid #ffffff; border-radius: 50%; box-shadow: 0 2px 8px rgba(92, 96, 245, 0.5);
}
.map-user-dot::after {
    content: ''; position: absolute; inset: -10px; border-radius: 50%;
    background: var(--brand-primary, #5c60f5); opacity: 0.2; animation: pulse 2s infinite;
}
@keyframes pulse { 0% { transform: scale(0.5); opacity: 0.4; } 100% { transform: scale(1.5); opacity: 0; } }

/* Имитация заказов вокруг */
.map-order-pin {
    position: absolute; width: 24px; height: 24px; background: var(--warning-main, #f59e0b);
    color: white; border-radius: 8px; display: flex; align-items: center; justify-content: center;
    font-size: 12px; box-shadow: 0 4px 10px rgba(245, 158, 11, 0.4); border: 2px solid #ffffff;
}
.map-order-pin.p1 { top: 20%; left: 20%; }
.map-order-pin.p2 { top: 30%; right: 25%; }

.map-content-row { display: flex; justify-content: space-between; align-items: flex-end; z-index: 2; }
.map-text { display: flex; flex-direction: column; }
.map-title { font-size: 16px; font-weight: 700; color: var(--text-main, #0f172a); line-height: 1.2; }
.map-subtitle { font-size: 13px; font-weight: 600; color: var(--success-main, #10b981); margin-top: 2px; display: flex; align-items: center; gap: 4px; }

.btn-expand-map {
    background: var(--brand-light, #eef2ff); color: var(--brand-primary, #5c60f5); border: none; border-radius: 12px;
    padding: 8px 14px; font-size: 14px; font-weight: 700; display: flex; align-items: center; gap: 6px; cursor: pointer;
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

/* Top-up & Withdrawal Modal Styles */
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
  color: var(--text-title, #0f172a);
}

.topup-modal-card {
  background: rgba(255, 255, 255, 0.95);
  backdrop-filter: blur(24px);
  -webkit-backdrop-filter: blur(24px);
  border: 1px solid rgba(255, 255, 255, 0.8);
  border-radius: var(--rad-lg, 24px);
  width: 100%;
  max-width: 420px;
  box-shadow: 0 20px 40px -10px rgba(0,0,0,0.1);
  padding: 32px;
  position: relative;
  animation: slideUp 0.3s ease-out;
}

.topup-modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 24px;
}

.topup-modal-title {
  font-size: 22px;
  font-weight: 700;
  color: var(--text-title, #0f172a);
  letter-spacing: -0.5px;
}

.btn-close-topup {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  border: 1px solid rgba(0,0,0,0.05);
  background: #f8fafc;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  color: var(--text-muted, #64748b);
  cursor: pointer;
  transition: var(--transition, all 0.2s ease);
}

.btn-close-topup:hover {
  background: #ffffff;
  color: #ef4444;
}

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
  color: var(--text-muted, #64748b);
  pointer-events: none;
}

.topup-modal-card .form-input {
  width: 100%;
  padding: 18px 20px 18px 52px;
  border-radius: 16px;
  background: #f8fafc;
  border: 1.5px solid rgba(0, 0, 0, 0.08);
  font-family: inherit;
  font-size: 20px;
  color: var(--text-title, #0f172a);
  font-weight: 600;
  transition: var(--transition, all 0.2s ease);
}

.topup-modal-card .form-input:focus {
  outline: none;
  border-color: var(--accent-main, #5c60f5);
  background: #ffffff;
  box-shadow: 0 0 0 4px rgba(92, 96, 245, 0.1);
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
  background: #f1f5f9;
  border: 1px solid rgba(0,0,0,0.05);
  font-size: 13px;
  font-weight: 600;
  color: var(--text-title, #0f172a);
  cursor: pointer;
  transition: var(--transition, all 0.2s ease);
}

.amount-pill:hover {
  background: #e2e8f0;
}

.btn-submit-topup {
  width: 100%;
  padding: 18px;
  border-radius: 16px;
  background: var(--brand-primary, #5c60f5);
  color: #ffffff;
  border: none;
  font-size: 16px;
  font-weight: 700;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  margin-top: 24px;
  transition: var(--transition, all 0.2s ease);
}

.btn-submit-topup:hover:not(:disabled) {
  background: #4f46e5;
  box-shadow: 0 10px 25px -5px rgba(92, 96, 245, 0.4);
}

.btn-submit-topup:disabled {
  opacity: 0.6;
  cursor: not-allowed;
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
</style>
