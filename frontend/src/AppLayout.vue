<template>
  <div class="admin-app">
    <!-- Затемняющая подложка мобильной боковой панели -->
    <div
      v-if="!sidebarMinimized && isMobile"
      class="sidebar-backdrop"
      @click="sidebarMinimized = true"
    ></div>

    <!-- Премиальная боковая панель -->
    <aside :class="['sidebar', { 'minimized': sidebarMinimized }]">
      <div class="logo">
        <AppLogo :hide-text="sidebarMinimized && !isMobile" />
        <!-- Только иконка: рядом с логотипом нет места для подписи, поэтому
             доступное имя берётся из aria-label, а не из видимого текста. -->
        <button
          class="logo-logout"
          type="button"
          title="Выйти из аккаунта"
          aria-label="Выйти из аккаунта"
          @click="doLogout"
        >
          <i class="ph-bold ph-sign-out"></i>
        </button>
      </div>

      <!-- Прокручивается только навигация: логотип остаётся на месте, а нижний
           блок под ним — досягаемым, в чём весь смысл на телефоне. -->
      <div class="sidebar-scroll">
        <div v-if="(!sidebarMinimized || isMobile) && showManagementSection" class="nav-section">Управление</div>
        <div class="nav-list">
          <router-link v-if="can('users.view')" to="/admin/users" class="nav-item" :class="{ active: currentRouteName === 'admin-users' }" @click="closeSidebarOnMobile">
            <i class="ph ph-users"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.users') }}</span>
          </router-link>

          <router-link v-if="can('roles.view')" to="/admin/roles" class="nav-item" :class="{ active: currentRouteName === 'admin-roles' }" @click="closeSidebarOnMobile">
            <i class="ph ph-shield-check"></i>
            <span v-if="!sidebarMinimized || isMobile">Роли и права</span>
          </router-link>

          <router-link v-if="can('support_chats.view')" to="/admin/support-chats" class="nav-item" :class="{ active: currentRouteName === 'admin-support-chats' }" @click="closeSidebarOnMobile">
            <div class="nav-icon-wrap">
              <i class="ph ph-chats-teardrop"></i>
              <span v-if="unreadSupportCount > 0 && sidebarMinimized && !isMobile" class="nav-dot-badge"></span>
            </div>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.supportChats') }}</span>
            <span v-if="unreadSupportCount > 0 && (!sidebarMinimized || isMobile)" class="nav-badge">{{ unreadSupportCount }}</span>
          </router-link>

          <router-link v-if="can('topups.view')" to="/admin/topups" class="nav-item" :class="{ active: currentRouteName === 'admin-topups' }" @click="closeSidebarOnMobile">
            <i class="ph-fill ph-wallet"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.topups') }}</span>
          </router-link>

          <router-link v-if="can('withdrawals.view')" to="/admin/withdrawals" class="nav-item" :class="{ active: currentRouteName === 'admin-withdrawals' }" @click="closeSidebarOnMobile">
            <i class="ph ph-bank"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.withdrawals') }}</span>
          </router-link>

          <router-link v-if="can('commission.view')" to="/admin/commission" class="nav-item" :class="{ active: currentRouteName === 'admin-commission' }" @click="closeSidebarOnMobile">
            <i class="ph ph-percent"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.commission') }}</span>
          </router-link>

          <router-link v-if="can('transactions.view')" to="/admin/transactions" class="nav-item" :class="{ active: currentRouteName === 'admin-transactions' }" @click="closeSidebarOnMobile">
            <i class="ph ph-arrows-left-right"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.transactions') }}</span>
          </router-link>

          <router-link v-if="can('reconciliation.view')" to="/admin/reconciliation" class="nav-item" :class="{ active: currentRouteName === 'admin-reconciliation' }" @click="closeSidebarOnMobile">
            <i class="ph ph-scales"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.reconciliation') }}</span>
          </router-link>

          <!-- Инциденты стоят рядом со сверкой намеренно: сверка ищет
               разъехавшиеся книги, а это — то, что код успел зажать до того,
               как они разъехались. -->
          <router-link v-if="can('incidents.view')" to="/admin/incidents" class="nav-item" :class="{ active: currentRouteName === 'admin-incidents' }" @click="closeSidebarOnMobile">
            <i class="ph ph-warning-octagon"></i>
            <span v-if="!sidebarMinimized || isMobile">Инциденты</span>
          </router-link>

          <router-link v-if="can('broadcasts.view')" to="/admin/broadcasts" class="nav-item" :class="{ active: currentRouteName === 'admin-broadcasts' }" @click="closeSidebarOnMobile">
            <i class="ph ph-megaphone"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.broadcasts') }}</span>
          </router-link>

          <router-link v-if="can('mail.view')" to="/admin/mail" class="nav-item" :class="{ active: currentRouteName === 'admin-mail' }" @click="closeSidebarOnMobile">
            <div class="nav-icon-wrap">
              <i class="ph ph-envelope-simple"></i>
              <span v-if="unreadMailCount > 0 && sidebarMinimized && !isMobile" class="nav-dot-badge"></span>
            </div>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.internalMail') }}</span>
            <span v-if="unreadMailCount > 0 && (!sidebarMinimized || isMobile)" class="nav-badge">{{ unreadMailCount }}</span>
          </router-link>
        </div>

        <div v-if="(!sidebarMinimized || isMobile) && showSystemSection" class="nav-section">Система</div>
        <div class="nav-list">
          <router-link v-if="can('shifts.view')" to="/admin/shifts" class="nav-item" :class="{ active: currentRouteName === 'admin-shifts' }" @click="closeSidebarOnMobile">
            <i class="ph ph-clock-user"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.activeShifts') }}</span>
          </router-link>

          <router-link v-if="can('orders.view')" to="/admin/orders" class="nav-item" :class="{ active: currentRouteName === 'admin-orders' }" @click="closeSidebarOnMobile">
            <i class="ph ph-package"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.orders') }}</span>
          </router-link>

          <router-link v-if="can('service_catalog.view')" to="/admin/service-catalog" class="nav-item" :class="{ active: currentRouteName === 'admin-service-catalog' }" @click="closeSidebarOnMobile">
            <i class="ph ph-list-dashes"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.serviceCatalog') }}</span>
          </router-link>

          <router-link v-if="can('achievements.view')" to="/admin/achievements" class="nav-item" :class="{ active: currentRouteName === 'admin-achievements' }" @click="closeSidebarOnMobile">
            <i class="ph ph-trophy"></i>
            <span v-if="!sidebarMinimized || isMobile">Ачивки</span>
          </router-link>

          <router-link v-if="can('gifts.view')" to="/admin/gifts" class="nav-item" :class="{ active: currentRouteName === 'admin-gifts' }" @click="closeSidebarOnMobile">
            <i class="ph ph-gift"></i>
            <span v-if="!sidebarMinimized || isMobile">Подарки</span>
          </router-link>

          <router-link v-if="can('checks.view')" to="/admin/check-requests" class="nav-item" :class="{ active: currentRouteName === 'admin-check-requests' }" @click="closeSidebarOnMobile">
            <i class="ph ph-seal-check"></i>
            <span v-if="!sidebarMinimized || isMobile">Заявки на проверку</span>
          </router-link>

          <router-link v-if="can('document_audit.view')" to="/admin/document-audit" class="nav-item" :class="{ active: currentRouteName === 'admin-document-audit' }" @click="closeSidebarOnMobile">
            <i class="ph ph-eyes"></i>
            <span v-if="!sidebarMinimized || isMobile">Аудит документов</span>
          </router-link>

          <router-link v-if="can('escalations.view')" to="/admin/escalations" class="nav-item" :class="{ active: currentRouteName === 'admin-escalations' }" @click="closeSidebarOnMobile">
            <i class="ph ph-shield-warning"></i>
            <span v-if="!sidebarMinimized || isMobile">Модерация проверок</span>
          </router-link>

          <router-link v-if="can('disputes.view')" to="/admin/disputes" class="nav-item" :class="{ active: currentRouteName === 'admin-disputes' }" @click="closeSidebarOnMobile">
            <i class="ph ph-scales"></i>
            <span v-if="!sidebarMinimized || isMobile">Споры</span>
          </router-link>

          <router-link v-if="can('watermarks.view')" to="/admin/watermark-symbols" class="nav-item" :class="{ active: currentRouteName === 'admin-watermark-symbols' }" @click="closeSidebarOnMobile">
            <i class="ph ph-hand-peace"></i>
            <span v-if="!sidebarMinimized || isMobile">Символы подтверждения</span>
          </router-link>

          <router-link v-if="can('settings.view')" to="/admin/settings" class="nav-item" :class="{ active: currentRouteName === 'admin-settings' }" @click="closeSidebarOnMobile">
            <i class="ph ph-gear"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.settings') }}</span>
          </router-link>
        </div>

        <!-- Магазин — свой раздел: товары, заказы и выручка охраняются тремя
             разными правами, потому что их ведут разные люди. -->
        <div v-if="(!sidebarMinimized || isMobile) && showShopSection" class="nav-section">{{ $t('shop.admin.sections') }}</div>
        <div class="nav-list">
          <router-link v-if="can('shop.view')" to="/admin/shop/products" class="nav-item" :class="{ active: currentRouteName === 'admin-shop-products' || currentRouteName === 'admin-shop-pickup-points' }" @click="closeSidebarOnMobile">
            <i class="ph ph-storefront"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('shop.admin.products') }}</span>
          </router-link>

          <router-link v-if="can('perk_rules.view')" to="/admin/shop/perk-rules" class="nav-item" :class="{ active: currentRouteName === 'admin-shop-perk-rules' }" @click="closeSidebarOnMobile">
            <i class="ph ph-function"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('shop.admin.rules.menu') }}</span>
          </router-link>

          <router-link v-if="can('shop_orders.view')" to="/admin/shop/orders" class="nav-item" :class="{ active: currentRouteName === 'admin-shop-orders' }" @click="closeSidebarOnMobile">
            <div class="nav-icon-wrap">
              <i class="ph ph-shopping-bag"></i>
              <span v-if="paidShopOrders > 0 && sidebarMinimized && !isMobile" class="nav-dot-badge"></span>
            </div>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('shop.admin.orders') }}</span>
            <span v-if="paidShopOrders > 0 && (!sidebarMinimized || isMobile)" class="nav-badge">{{ paidShopOrders }}</span>
          </router-link>

          <router-link v-if="can('shop_revenue.view')" to="/admin/shop/revenue" class="nav-item" :class="{ active: currentRouteName === 'admin-shop-revenue' }" @click="closeSidebarOnMobile">
            <i class="ph ph-cash-register"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('shop.admin.revenue') }}</span>
          </router-link>
        </div>
      </div>

      <!-- Язык живёт здесь; выход — наверху рядом с логотипом. В свёрнутом виде
           весь нижний блок убирается, чтобы не осталась пустая полоса с
           рамкой. -->
      <div v-if="!sidebarMinimized || isMobile" class="sidebar-footer">
        <div class="sidebar-lang">
          <span>Язык</span>
          <LanguageSwitcher />
        </div>
      </div>
    </aside>

    <!-- Область основного содержимого -->
    <main class="main-wrapper">
      <!-- Элементы управления верхней шапки -->
      <header class="top-header">
        <div class="d-flex align-items-center gap-3">
          <button class="btn-toggle-sidebar" @click="sidebarMinimized = !sidebarMinimized">
            <i class="ph ph-list"></i>
          </button>
          <h1 class="page-title">{{ pageTitle }}</h1>
        </div>

        <div class="header-controls">
          <div class="control-pill user-pill">
            <i class="ph-fill ph-user-circle"></i>
            <span class="user-phone-text">{{ phone || '7 999 999 99 99' }}</span>
          </div>
        </div>
      </header>

      <!-- Контейнер слота представления -->
      <div :class="['page-card', { 'page-card--flush': flushPage, 'page-card--bare': barePage }]">
        <router-view />
      </div>
    </main>
  </div>
</template>

<script lang="ts">
import { defineComponent, computed, ref, onMounted, onUnmounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from './stores/auth-store'
import api from './services/api'
import { adminGetMailUnread } from './api/mail'
import { adminCountPaid } from './api/shop'
import { useI18n } from './i18n'
import LanguageSwitcher from './components/LanguageSwitcher.vue'
import AppLogo from './components/AppLogo.vue'

// Карта «имя роута → ключ pageTitles.*». Заголовок живёт в локалях, чтобы
// шапка переключалась вместе с языком интерфейса.
const PAGE_TITLE_KEYS: Record<string, string> = {
  'admin-users': 'users',
  'admin-roles': 'roles',
  'admin-support-chats': 'supportChats',
  'admin-topups': 'topups',
  'admin-withdrawals': 'withdrawals',
  'admin-commission': 'commission',
  'admin-transactions': 'transactions',
  'admin-reconciliation': 'reconciliation',
  'admin-broadcasts': 'broadcasts',
  'admin-mail': 'mail',
  'admin-shifts': 'shifts',
  'admin-orders': 'orders',
  'admin-service-catalog': 'serviceCatalog',
  'admin-escalations': 'escalations',
  'admin-disputes': 'disputes',
  'admin-watermark-symbols': 'watermarkSymbols',
  'admin-achievements': 'achievements',
  'admin-gifts': 'gifts',
  'admin-incidents': 'incidents',
  'admin-service-scripts-help': 'serviceScriptsHelp',
  'admin-settings': 'settings',
  'admin-shop-products': 'shopProducts',
  'admin-shop-pickup-points': 'shopPickupPoints',
  'admin-shop-orders': 'shopOrders',
  'admin-shop-revenue': 'shopRevenue',
}

export default defineComponent({
  name: 'AppLayout',
  components: { LanguageSwitcher, AppLogo },
  setup() {
    const router = useRouter()
    const route = useRoute()
    const authStore = useAuthStore()
    const { t } = useI18n()

    const windowWidth = ref(window.innerWidth)
    const isMobile = computed(() => windowWidth.value < 768)
    const sidebarMinimized = ref(window.innerWidth < 768)
    const unreadSupportCount = ref(0)
    const unreadMailCount = ref(0)
    // Оплаченные покупки, которые ещё никто не взял в работу.
    const paidShopOrders = ref(0)
    let unreadTimer: any = null

    // can решает, показывать ли пункт меню. Права приходят с /auth/me и
    // повторяют то, что охраняет маршруты на бэкенде, поэтому спрятанный пункт
    // и запрещённый запрос — это всегда одно и то же право.
    const can = (permission: string) => authStore.can(permission)

    // Заголовок группы прячется вместе с последним её пунктом, иначе у роли с
    // одним разделом остались бы подписи над пустотой.
    const showManagementSection = computed(() =>
      ['users.view', 'roles.view', 'support_chats.view', 'topups.view', 'withdrawals.view',
       'commission.view', 'transactions.view', 'reconciliation.view', 'incidents.view',
       'broadcasts.view', 'mail.view'].some(can),
    )
    const showShopSection = computed(() => ['shop.view', 'perk_rules.view', 'shop_orders.view', 'shop_revenue.view'].some(can))
    const showSystemSection = computed(() =>
      ['shifts.view', 'orders.view', 'service_catalog.view', 'achievements.view',
       'gifts.view', 'checks.view', 'document_audit.view', 'escalations.view', 'disputes.view',
       'watermarks.view', 'settings.view'].some(can),
    )

    // Ответы пользователей во внутренней почте. Считается тем же редким
    // опросом, что и поддержка: письмо ждёт ответа часами, а не секундами.
    const fetchUnreadMail = async () => {
      if (!can('mail.view')) return
      try {
        unreadMailCount.value = await adminGetMailUnread()
      } catch (err) {}
    }

    const fetchPaidShopOrders = async () => {
      if (!can('shop_orders.view')) return
      try {
        paidShopOrders.value = await adminCountPaid()
      } catch (err) {}
    }

    const fetchUnreadSupport = async () => {
      // Без права на чаты поддержки бейджа нет, а значит нет и опроса: иначе
      // роль без доступа получала бы 403 каждые 15 секунд.
      if (!can('support_chats.view')) return
      try {
        const res = await api.get('/admin/support/unread-summary')
        if (res.data && res.data.unread_count !== undefined) {
          unreadSupportCount.value = res.data.unread_count
        }
      } catch (err) {}
    }

    const handleResize = () => {
      windowWidth.value = window.innerWidth
      if (window.innerWidth < 768) {
        sidebarMinimized.value = true
      }
    }

    onMounted(() => {
      window.addEventListener('resize', handleResize)
      window.addEventListener('support-unread-updated', fetchUnreadSupport)
      fetchUnreadSupport()
      fetchUnreadMail()
      fetchPaidShopOrders()
      // 15 с, а не 3: этот бейдж считает непрочитанные сообщения поддержки по всем
      // чатам, а это скан таблицы сообщений на сервере. Ответ поддержки — не то, о
      // чём админу нужно узнать в течение трёх секунд, а платила за это каждая
      // открытая вкладка админки, постоянно.
      unreadTimer = setInterval(() => {
        fetchUnreadSupport()
        fetchUnreadMail()
        fetchPaidShopOrders()
      }, 15000)
    })

    onUnmounted(() => {
      window.removeEventListener('resize', handleResize)
      window.removeEventListener('support-unread-updated', fetchUnreadSupport)
      if (unreadTimer) clearInterval(unreadTimer)
    })

    const closeSidebarOnMobile = () => {
      if (window.innerWidth < 768) {
        sidebarMinimized.value = true
      }
    }

    const phone = computed(() => authStore.phone)
    const currentRouteName = computed(() => route.name)

    // Страницы-приложения (чаты, почта) сами заведуют своей геометрией:
    // meta.flush снимает с листа поля и отдаёт им всю поверхность.
    const flushPage = computed(() => !!route.meta.flush)

    // Страницы со своей карточной системой (панели на сером фоне):
    // meta.bare убирает белый лист целиком, иначе их белые карточки
    // лежали бы на белом — «карточка в карточке» без видимой границы.
    const barePage = computed(() => !!route.meta.bare)

    const pageTitle = computed(() => {
      const key = PAGE_TITLE_KEYS[route.name as string]
      return t(key ? `pageTitles.${key}` : 'pageTitles.default')
    })

    const doLogout = async () => {
      try {
        await api.post('/logout')
      } catch (e) {
        console.error('Logout error blacklisting token', e)
      } finally {
        authStore.logout()
        router.push('/login')
      }
    }

    return {
      can,
      showShopSection,
      paidShopOrders,
      showManagementSection,
      showSystemSection,
      phone,
      isMobile,
      unreadSupportCount,
      unreadMailCount,
      currentRouteName,
      flushPage,
      barePage,
      sidebarMinimized,
      pageTitle,
      closeSidebarOnMobile,
      doLogout,
    }
  },
})
</script>

<style scoped>
:root {
  --bg-body: #f3f5f9;
  --surface-card: #ffffff;
  --surface-sidebar: #ffffff;
  --text-main: #0f172a;
  --text-muted: #64748b;
  --brand-primary: #5c60f5;
  --brand-light: #eef2ff;
  --danger-main: #ef4444;
  --danger-bg: #fef2f2;
}

.admin-app {
  display: flex;
  height: 100vh;
  width: 100vw;
  font-family: 'Outfit', sans-serif;
  background-color: #f3f5f9;
  background-image:
    radial-gradient(at 0% 0%, rgba(92, 96, 245, 0.05) 0px, transparent 40%),
    radial-gradient(at 100% 100%, rgba(236, 72, 153, 0.03) 0px, transparent 40%);
  background-attachment: fixed;
  color: #0f172a;
  overflow: hidden;
}

/* Стили боковой панели */
.sidebar {
  width: 260px;
  background: #ffffff;
  border-right: 1px solid rgba(0, 0, 0, 0.04);
  display: flex;
  flex-direction: column;
  padding: 24px 16px;
  z-index: 10;
  transition: all 0.3s ease;
  flex-shrink: 0;
}

.sidebar.minimized {
  width: 80px;
  padding: 24px 12px;
}

.logo {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 32px;
  padding: 0 4px;
}

/* Выход, закреплён в верхнем ряду рядом с логотипом. */
.logo-logout {
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  width: 36px;
  height: 36px;
  padding: 0;
  border: none;
  border-radius: 10px;
  background: transparent;
  color: #ef4444;
  font-size: 20px;
  cursor: pointer;
  transition: background 0.2s ease;
}

.logo-logout:hover {
  background: #fef2f2;
}

/* В свёрнутом виде на рейке шириной 80px нет места для двух элементов рядом,
   поэтому кнопка уходит под знак логотипа, а не выдавливается из виду. */
.sidebar.minimized .logo {
  flex-direction: column;
  gap: 12px;
  justify-content: center;
}

.logo-container {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  user-select: none;
}

.logo-svg {
  width: 36px;
  height: 36px;
  flex-shrink: 0;
}

.text-block {
  display: flex;
  flex-direction: column;
  line-height: 1;
}

.text-top {
  font-size: 17px;
  font-weight: 800;
  color: #0f172a;
  letter-spacing: -0.5px;
  margin-left: 1px;
}

.text-bottom {
  font-size: 13px;
  font-weight: 700;
  color: #5c60f5;
  letter-spacing: 0.3px;
}

/* Прокручиваемая часть боковой панели.
   Панель — flex-колонка фиксированной высоты, поэтому без этого навигация
   просто переполняла её, а overflow:hidden у .admin-app обрезал всё, что не
   влезло. На телефоне это уводило нижний блок — язык и «Выйти из аккаунта» —
   за пределы экрана без возможности до него доскроллить. */
.sidebar-scroll {
  flex: 1;
  /* Без этого flex-элемент отказывается сжиматься меньше своего содержимого,
     и переполнение осталось бы на панели вместо того, чтобы переехать сюда. */
  min-height: 0;
  overflow-y: auto;
  /* Прокрутка до конца меню не должна начинать тащить страницу за открытым
     оверлеем. */
  overscroll-behavior: contain;
  -webkit-overflow-scrolling: touch;
  scrollbar-width: thin;
  scrollbar-color: rgba(148, 163, 184, 0.5) transparent;
}

.sidebar-scroll::-webkit-scrollbar {
  width: 6px;
}

.sidebar-scroll::-webkit-scrollbar-thumb {
  background: rgba(148, 163, 184, 0.5);
  border-radius: 999px;
}

.sidebar-scroll::-webkit-scrollbar-track {
  background: transparent;
}

.nav-section {
  font-size: 11px;
  font-weight: 700;
  color: #94a3b8;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin: 16px 0 8px 12px;
}

.nav-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.nav-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  border-radius: 12px;
  color: #64748b;
  font-size: 15px;
  font-weight: 500;
  text-decoration: none;
  transition: all 0.2s ease-in-out;
  cursor: pointer;
}

.nav-item:hover {
  background: #f8fafc;
  color: #0f172a;
}

.nav-item.active {
  background: rgba(99, 102, 241, 0.15);
  color: #818cf8;
}

.nav-icon-wrap {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}

.nav-badge {
  background: #ef4444;
  color: #ffffff;
  font-size: 11px;
  font-weight: 700;
  padding: 2px 8px;
  border-radius: 12px;
  margin-left: auto;
  box-shadow: 0 2px 6px rgba(239, 68, 68, 0.4);
  animation: pulseBadge 2s infinite;
}

@keyframes pulseBadge {
  0%, 100% { transform: scale(1); }
  50% { transform: scale(1.08); }
}

.nav-dot-badge {
  position: absolute;
  top: -2px;
  right: -2px;
  width: 9px;
  height: 9px;
  background-color: #ef4444;
  border: 2px solid #ffffff;
  border-radius: 50%;
}

.nav-item i {
  font-size: 20px;
}

/* Низ боковой панели: переключатель языка, прижат к нижнему краю. */
.sidebar-footer {
  margin-top: auto;
  padding-top: 12px;
  border-top: 1px solid rgba(0, 0, 0, 0.06);
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.sidebar-lang {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 14px;
  background: #f8fafc;
  border-radius: 12px;
}
.sidebar-lang span {
  font-size: 13px;
  font-weight: 600;
  color: #64748b;
}

/* Обёртка основного содержимого */
.main-wrapper {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow-y: auto;
  padding: 32px 40px;
  gap: 24px;
}

/* Верхняя шапка */
.top-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.btn-toggle-sidebar {
  background: #ffffff;
  border: 1px solid rgba(0,0,0,0.05);
  border-radius: 10px;
  width: 40px;
  height: 40px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
  color: #0f172a;
  cursor: pointer;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.02);
}

.page-title {
  font-size: 28px;
  font-weight: 700;
  color: #0f172a;
  letter-spacing: -0.5px;
  margin: 0;
}

.header-controls {
  display: flex;
  gap: 16px;
  align-items: center;
}

.control-pill {
  background: #ffffff;
  border: 1px solid rgba(0,0,0,0.05);
  border-radius: 99px;
  height: 40px;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 16px;
  font-size: 14px;
  font-weight: 600;
  color: #0f172a;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.02);
}

.user-pill i {
  color: #5c60f5;
  font-size: 20px;
}

.btn-logout {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  background: #fef2f2;
  color: #ef4444;
  border: none;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
  cursor: pointer;
  transition: all 0.2s ease-in-out;
}

.btn-logout:hover {
  background: #fee2e2;
  transform: scale(1.05);
}

/* Карточка страницы */
.page-card {
  background: #ffffff;
  border-radius: 24px;
  box-shadow: 0 4px 24px rgba(15, 23, 42, 0.04);
  padding: 28px;
  min-height: calc(100vh - 160px);
}

/* Страницы, приносящие свою карточку, раньше рисовали «карточку в карточке»:
   двойная тень, двойные скругления и поля 28 + 24px. Теперь такая карточка
   сама становится поверхностью: растягивается на весь лист отрицательными
   полями ровно на его паддинг (поэтому прокрутки это не добавляет — карточка
   встаёт точно по краю padding-области), повторяет скругление листа, а тень
   остаётся одна. */
.page-card > :deep(* > .admin-card),
.page-card > :deep(* > .admin-table-card),
.page-card > :deep(* > .va-card) {
  margin: -28px;
  border-radius: 24px;
  box-shadow: none;
}

/* Чаты и почта — полноэкранные приложения: белая рамка с полями вокруг них
   давала двойной скролл и двойную рамку. meta.flush убирает поля листа,
   и страница заполняет его целиком. */
.page-card--flush {
  padding: 0;
  overflow: hidden;
  display: flex;
}

.page-card--flush > :deep(*) {
  flex: 1 1 auto;
  min-height: 0;
  min-width: 0;
}

/* Страницы со своими белыми панелями (роли, ачивки, подарки и т.п.) на белом
   листе выглядели пятнами без границ. meta.bare убирает лист: панели ложатся
   на серый фон приложения и читаются как положено. */
.page-card--bare {
  background: transparent;
  box-shadow: none;
  padding: 0;
  min-height: 0;
}

@media (max-width: 767px) {
  .admin-app {
    position: relative;
    overflow-x: hidden;
  }

  .sidebar {
    position: fixed;
    top: 0;
    left: 0;
    bottom: 0;
    /* 100vh может превышать реально видимое, пока показаны панели мобильного
       браузера, и это снова вытолкнуло бы нижний блок за экран даже при
       прокручиваемой навигации. dvh следит за видимой областью. */
    height: 100vh;
    height: 100dvh;
    z-index: 1000;
    box-shadow: 0 10px 40px rgba(0, 0, 0, 0.2);
    transform: translateX(0);
    transition: transform 0.3s cubic-bezier(0.16, 1, 0.3, 1);
    width: 260px !important;
    padding: 24px 16px;
  }

  .sidebar.minimized {
    transform: translateX(-100%);
    width: 260px !important;
  }

  /* В установленном приложении домашний индикатор лежит поверх низа экрана;
     без этого выход оказывается под ним. В остальных случаях равно 0. */
  .sidebar-footer {
    padding-bottom: env(safe-area-inset-bottom, 0px);
  }

  .sidebar-backdrop {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(15, 23, 42, 0.5);
    backdrop-filter: blur(4px);
    -webkit-backdrop-filter: blur(4px);
    z-index: 999;
    animation: fadeIn 0.2s ease;
  }

  .main-wrapper {
    padding: 12px;
    gap: 16px;
    width: 100%;
    overflow-x: hidden;
  }

  .top-header {
    flex-wrap: wrap;
    gap: 10px;
  }

  .page-title {
    font-size: 20px;
    letter-spacing: -0.3px;
  }

  .header-controls {
    gap: 8px;
    margin-left: auto;
  }

  .user-pill {
    padding: 0 10px;
    height: 36px;
    font-size: 12px;
  }

  .user-phone-text {
    max-width: 105px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .btn-toggle-sidebar {
    width: 36px;
    height: 36px;
    font-size: 18px;
  }

  .btn-logout {
    width: 36px;
    height: 36px;
    font-size: 16px;
  }

  .page-card {
    padding: 14px 12px;
    border-radius: 16px;
    min-height: calc(100vh - 110px);
    overflow-x: auto;
    -webkit-overflow-scrolling: touch;
  }

  /* Те же правила слияния, но под мобильные поля листа. */
  .page-card > :deep(* > .admin-card),
  .page-card > :deep(* > .admin-table-card),
  .page-card > :deep(* > .va-card) {
    margin: -14px -12px;
    border-radius: 16px;
  }

  .page-card--flush {
    padding: 0;
  }

  /* Порядок важен: мобильный .page-card выше перезаписал бы bare. */
  .page-card--bare {
    background: transparent;
    box-shadow: none;
    padding: 0;
    min-height: 0;
  }
}

@keyframes fadeIn {
  from { opacity: 0; }
  to { opacity: 1; }
}
</style>

