<template>
  <div class="admin-app">
    <!-- Mobile Sidebar Backdrop Overlay -->
    <div
      v-if="!sidebarMinimized && isMobile"
      class="sidebar-backdrop"
      @click="sidebarMinimized = true"
    ></div>

    <!-- Premium Sidebar -->
    <aside :class="['sidebar', { 'minimized': sidebarMinimized }]">
      <div class="logo">
        <AppLogo :hide-text="sidebarMinimized && !isMobile" />
        <!-- Icon only: there is no room for the label beside the logo, so the
             accessible name comes from aria-label rather than visible text. -->
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

      <!-- Only the nav scrolls: the logo stays put and the footer below stays
           reachable, which is the whole point on a phone. -->
      <div class="sidebar-scroll">
        <div v-if="!sidebarMinimized || isMobile" class="nav-section">Управление</div>
        <div class="nav-list">
          <router-link to="/admin/users" class="nav-item" :class="{ active: currentRouteName === 'admin-users' }" @click="closeSidebarOnMobile">
            <i class="ph ph-users"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.users') }}</span>
          </router-link>

          <router-link to="/admin/support-chats" class="nav-item" :class="{ active: currentRouteName === 'admin-support-chats' }" @click="closeSidebarOnMobile">
            <div class="nav-icon-wrap">
              <i class="ph ph-chats-teardrop"></i>
              <span v-if="unreadSupportCount > 0 && sidebarMinimized && !isMobile" class="nav-dot-badge"></span>
            </div>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.supportChats') }}</span>
            <span v-if="unreadSupportCount > 0 && (!sidebarMinimized || isMobile)" class="nav-badge">{{ unreadSupportCount }}</span>
          </router-link>

          <router-link to="/admin/topups" class="nav-item" :class="{ active: currentRouteName === 'admin-topups' }" @click="closeSidebarOnMobile">
            <i class="ph-fill ph-wallet"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.topups') }}</span>
          </router-link>

          <router-link to="/admin/withdrawals" class="nav-item" :class="{ active: currentRouteName === 'admin-withdrawals' }" @click="closeSidebarOnMobile">
            <i class="ph ph-bank"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.withdrawals') }}</span>
          </router-link>

          <router-link to="/admin/commission" class="nav-item" :class="{ active: currentRouteName === 'admin-commission' }" @click="closeSidebarOnMobile">
            <i class="ph ph-percent"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.commission') }}</span>
          </router-link>

          <router-link to="/admin/transactions" class="nav-item" :class="{ active: currentRouteName === 'admin-transactions' }" @click="closeSidebarOnMobile">
            <i class="ph ph-arrows-left-right"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.transactions') }}</span>
          </router-link>

          <router-link to="/admin/reconciliation" class="nav-item" :class="{ active: currentRouteName === 'admin-reconciliation' }" @click="closeSidebarOnMobile">
            <i class="ph ph-scales"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.reconciliation') }}</span>
          </router-link>

          <router-link to="/admin/broadcasts" class="nav-item" :class="{ active: currentRouteName === 'admin-broadcasts' }" @click="closeSidebarOnMobile">
            <i class="ph ph-megaphone"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.broadcasts') }}</span>
          </router-link>
        </div>

        <div v-if="!sidebarMinimized || isMobile" class="nav-section">Система</div>
        <div class="nav-list">
          <router-link to="/admin/shifts" class="nav-item" :class="{ active: currentRouteName === 'admin-shifts' }" @click="closeSidebarOnMobile">
            <i class="ph ph-clock-user"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.activeShifts') }}</span>
          </router-link>

          <router-link to="/admin/orders/active" class="nav-item" :class="{ active: currentRouteName === 'admin-active-orders' }" @click="closeSidebarOnMobile">
            <i class="ph ph-package"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.activeOrders') }}</span>
          </router-link>

          <router-link to="/admin/orders/completed" class="nav-item" :class="{ active: currentRouteName === 'admin-completed-orders' }" @click="closeSidebarOnMobile">
            <i class="ph ph-check-circle"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.completedOrders') }}</span>
          </router-link>

          <router-link to="/admin/service-catalog" class="nav-item" :class="{ active: currentRouteName === 'admin-service-catalog' }" @click="closeSidebarOnMobile">
            <i class="ph ph-list-dashes"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.serviceCatalog') }}</span>
          </router-link>

          <router-link to="/admin/settings" class="nav-item" :class="{ active: currentRouteName === 'admin-settings' }" @click="closeSidebarOnMobile">
            <i class="ph ph-gear"></i>
            <span v-if="!sidebarMinimized || isMobile">{{ $t('app.settings') }}</span>
          </router-link>
        </div>
      </div>

      <!-- Language lives here; logout sits at the top next to the logo. The
           whole footer is dropped when minimized, so an empty bordered strip is
           not left behind. -->
      <div v-if="!sidebarMinimized || isMobile" class="sidebar-footer">
        <div class="sidebar-lang">
          <span>Язык</span>
          <LanguageSwitcher />
        </div>
      </div>
    </aside>

    <!-- Main Content Area -->
    <main class="main-wrapper">
      <!-- Top Header Controls -->
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

      <!-- View Slot Container -->
      <div class="page-card">
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
import LanguageSwitcher from './components/LanguageSwitcher.vue'
import AppLogo from './components/AppLogo.vue'

export default defineComponent({
  name: 'AppLayout',
  components: { LanguageSwitcher, AppLogo },
  setup() {
    const router = useRouter()
    const route = useRoute()
    const authStore = useAuthStore()

    const windowWidth = ref(window.innerWidth)
    const isMobile = computed(() => windowWidth.value < 768)
    const sidebarMinimized = ref(window.innerWidth < 768)
    const unreadSupportCount = ref(0)
    let unreadTimer: any = null

    const fetchUnreadSupport = async () => {
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
      unreadTimer = setInterval(fetchUnreadSupport, 3000)
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

    const pageTitle = computed(() => {
      switch (route.name) {
        case 'admin-users': return 'Пользователи'
        case 'admin-topups': return 'Запросы на пополнение'
        case 'admin-withdrawals': return 'Запросы на вывод'
        case 'admin-commission': return 'Комиссия платформы'
        case 'admin-transactions': return 'Транзакции'
        case 'admin-broadcasts': return 'Рассылки писем'
        case 'admin-shifts': return 'Активные смены'
        case 'admin-active-orders': return 'Активные заказы'
        case 'admin-completed-orders': return 'Выполненные заказы'
        case 'admin-service-catalog': return 'Каталог услуг'
        case 'admin-settings': return 'Системные настройки'
        default: return 'Панель администратора'
      }
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
      phone,
      isMobile,
      unreadSupportCount,
      currentRouteName,
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

/* Sidebar Styles */
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

/* Logout, pinned to the top row beside the logo. */
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

/* Minimized, the 80px rail has no room for two things side by side, so the
   button drops under the logo mark instead of being squeezed out of view. */
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

/* The scrolling part of the sidebar.
   The sidebar is a fixed-height flex column, so without this the nav simply
   overflowed it and .admin-app's overflow:hidden clipped whatever did not fit.
   On a phone that put the footer — language and "Выйти из аккаунта" — below the
   fold with no way to scroll to it. */
.sidebar-scroll {
  flex: 1;
  /* A flex child refuses to shrink below its content without this, which would
     keep the overflow on the sidebar instead of moving it in here. */
  min-height: 0;
  overflow-y: auto;
  /* Scrolling to the end of the menu must not start dragging the page behind
     the open overlay. */
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

/* Sidebar footer: the language switcher, pinned to the bottom. */
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

/* Main Content Wrapper */
.main-wrapper {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow-y: auto;
  padding: 32px 40px;
  gap: 24px;
}

/* Top Header */
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

/* Page Card Box */
.page-card {
  background: #ffffff;
  border-radius: 24px;
  box-shadow: 0 4px 24px rgba(15, 23, 42, 0.04);
  padding: 28px;
  min-height: calc(100vh - 160px);
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
    /* 100vh can exceed what is actually visible while a mobile browser's
       toolbars are showing, which would push the footer off screen again even
       with the nav scrolling. dvh tracks the visible viewport. */
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

  /* In the installed app the home indicator sits over the bottom of the screen;
     without this the logout ends up underneath it. Resolves to 0 elsewhere. */
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
}

@keyframes fadeIn {
  from { opacity: 0; }
  to { opacity: 1; }
}
</style>

