<template>
  <div class="app-layout">
    <!-- Navbar -->
    <va-navbar color="primary" class="app-layout__navbar">
      <template #left>
        <va-button
          color="primary"
          icon="menu"
          class="mr-2 d-md-none-btn"
          @click="sidebarMinimized = !sidebarMinimized"
        />
        <div class="logo">
          <strong>{{ $t('app.adminTitle') }}</strong>
        </div>
      </template>
      <template #right>
        <div class="d-flex align-items-center flex-wrap">
          <LanguageSwitcher class="mr-2" />
          <span class="user-info mr-2 d-none d-sm-inline">{{ phone }}</span>
          <va-button color="danger" size="small" @click="doLogout">{{ $t('app.logout') }}</va-button>
        </div>
      </template>
    </va-navbar>

    <!-- Sidebar and Main Panel -->
    <div class="app-layout__container">
      <va-sidebar
        :minimized="sidebarMinimized"
        :class="['app-layout__sidebar', { 'mobile-hidden': sidebarMinimized }]"
      >
        <va-sidebar-item :active="currentRouteName === 'admin-users'" to="/admin/users" @click="closeSidebarOnMobile">
          <va-sidebar-item-content>
            <va-icon name="people" />
            <va-sidebar-item-title>{{ $t('app.users') }}</va-sidebar-item-title>
          </va-sidebar-item-content>
        </va-sidebar-item>

        <va-sidebar-item :active="currentRouteName === 'admin-topups'" to="/admin/topups" @click="closeSidebarOnMobile">
          <va-sidebar-item-content>
            <va-icon name="account_balance_wallet" />
            <va-sidebar-item-title>{{ $t('app.topups') }}</va-sidebar-item-title>
          </va-sidebar-item-content>
        </va-sidebar-item>

        <va-sidebar-item :active="currentRouteName === 'admin-transactions'" to="/admin/transactions" @click="closeSidebarOnMobile">
          <va-sidebar-item-content>
            <va-icon name="history" />
            <va-sidebar-item-title>{{ $t('app.transactions') }}</va-sidebar-item-title>
          </va-sidebar-item-content>
        </va-sidebar-item>

        <va-sidebar-item :active="currentRouteName === 'admin-settings'" to="/admin/settings" @click="closeSidebarOnMobile">
          <va-sidebar-item-content>
            <va-icon name="settings" />
            <va-sidebar-item-title>{{ $t('app.settings') }}</va-sidebar-item-title>
          </va-sidebar-item-content>
        </va-sidebar-item>

        <va-sidebar-item :active="currentRouteName === 'admin-shifts'" to="/admin/shifts" @click="closeSidebarOnMobile">
          <va-sidebar-item-content>
            <va-icon name="schedule" />
            <va-sidebar-item-title>{{ $t('app.activeShifts') }}</va-sidebar-item-title>
          </va-sidebar-item-content>
        </va-sidebar-item>

        <va-sidebar-item :active="currentRouteName === 'admin-active-orders'" to="/admin/orders/active" @click="closeSidebarOnMobile">
          <va-sidebar-item-content>
            <va-icon name="pending_actions" />
            <va-sidebar-item-title>{{ $t('app.activeOrders') }}</va-sidebar-item-title>
          </va-sidebar-item-content>
        </va-sidebar-item>

        <va-sidebar-item :active="currentRouteName === 'admin-completed-orders'" to="/admin/orders/completed" @click="closeSidebarOnMobile">
          <va-sidebar-item-content>
            <va-icon name="check_circle" />
            <va-sidebar-item-title>{{ $t('app.completedOrders') }}</va-sidebar-item-title>
          </va-sidebar-item-content>
        </va-sidebar-item>

        <va-sidebar-item :active="currentRouteName === 'admin-service-catalog'" to="/admin/service-catalog" @click="closeSidebarOnMobile">
          <va-sidebar-item-content>
            <va-icon name="category" />
            <va-sidebar-item-title>{{ $t('app.serviceCatalog') }}</va-sidebar-item-title>
          </va-sidebar-item-content>
        </va-sidebar-item>
      </va-sidebar>

      <main class="app-layout__main">
        <div class="main-content">
          <router-view />
        </div>
      </main>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, computed, ref, onMounted } from 'vue'
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

    const sidebarMinimized = ref(window.innerWidth < 768)

    onMounted(() => {
      window.addEventListener('resize', () => {
        if (window.innerWidth < 768) {
          sidebarMinimized.value = true
        }
      })
    })

    const closeSidebarOnMobile = () => {
      if (window.innerWidth < 768) {
        sidebarMinimized.value = true
      }
    }

    const phone = computed(() => authStore.phone)
    const currentRouteName = computed(() => route.name)

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
      currentRouteName,
      sidebarMinimized,
      closeSidebarOnMobile,
      doLogout,
    }
  },
})
</script>

<style scoped>
.app-layout {
  display: flex;
  flex-direction: column;
  height: 100vh;
  overflow: hidden;
}

.app-layout__navbar {
  min-height: 56px;
  box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
  z-index: 1001;
  padding: 0 8px;
}

.app-layout__container {
  display: flex;
  flex: 1;
  overflow: hidden;
  position: relative;
}

.app-layout__sidebar {
  width: 240px !important;
  flex-shrink: 0;
  box-shadow: 2px 0 4px rgba(0, 0, 0, 0.05);
  transition: all 0.3s ease;
  z-index: 1000;
}

.app-layout__main {
  flex: 1;
  padding: 12px;
  background-color: #f6f8fa;
  overflow-y: auto;
  overflow-x: auto;
}

.main-content {
  padding: 16px;
  background: white;
  border-radius: 8px;
  box-shadow: 0 4px 6px -1px rgba(0,0,0,0.1);
  min-height: 80vh;
  overflow-x: auto;
}

.logo {
  font-size: 1.1rem;
  color: white;
  white-space: nowrap;
}

.user-info {
  color: white;
  font-size: 0.85rem;
}

@media (max-width: 767px) {
  .app-layout__sidebar.mobile-hidden {
    display: none !important;
  }
  .app-layout__sidebar {
    position: absolute;
    top: 0;
    left: 0;
    height: 100%;
    background: white;
    width: 240px !important;
  }
  .app-layout__main {
    padding: 8px;
  }
  .main-content {
    padding: 12px;
  }
}
</style>
