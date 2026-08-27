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
        <div class="logo-container opt-1">
          <svg class="logo-svg" viewBox="0 0 40 40" fill="none">
            <path d="M 6 35 V 17 C 6 9, 15 9, 20 18 C 25 9, 32 9, 32 17 V 23" stroke="#5c60f5" stroke-width="7" stroke-linecap="round" stroke-linejoin="round"></path>
            <circle cx="19" cy="27" r="4.5" fill="#10b981"></circle>
          </svg>
          <div v-if="!sidebarMinimized || isMobile" class="text-block">
            <div class="text-top">оя</div>
            <div class="text-bottom">услуга</div>
          </div>
        </div>
      </div>

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
          <LanguageSwitcher />

          <div class="control-pill user-pill">
            <i class="ph-fill ph-user-circle"></i>
            <span class="user-phone-text">{{ phone || '7 999 999 99 99' }}</span>
          </div>

          <button class="btn-logout" :title="$t('app.logout')" @click="doLogout">
            <i class="ph-bold ph-sign-out"></i>
          </button>
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

export default defineComponent({
  name: 'AppLayout',
  components: { LanguageSwitcher },
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
  margin-bottom: 32px;
  padding: 0 4px;
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
    height: 100vh;
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

