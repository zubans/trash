<template>
  <div class="app-layout">
    <!-- Navbar -->
    <va-navbar color="primary" class="app-layout__navbar">
      <template #left>
        <div class="logo">
          <strong>{{ $t('app.adminTitle') }}</strong>
        </div>
      </template>
      <template #right>
        <LanguageSwitcher class="mr-3" />
        <span class="user-info mr-3">{{ $t('app.admin') }}: {{ phone }}</span>
        <va-button color="danger" size="small" @click="doLogout">{{ $t('app.logout') }}</va-button>
      </template>
    </va-navbar>

    <!-- Sidebar and Main Panel -->
    <div class="app-layout__container">
      <va-sidebar v-slot="{ sidebarVisible }" class="app-layout__sidebar">
        <va-sidebar-item :active="currentRouteName === 'admin-users'" to="/admin/users">
          <va-sidebar-item-content>
            <va-icon name="people" />
            <va-sidebar-item-title>{{ $t('app.users') }}</va-sidebar-item-title>
          </va-sidebar-item-content>
        </va-sidebar-item>

        <va-sidebar-item :active="currentRouteName === 'admin-topups'" to="/admin/topups">
          <va-sidebar-item-content>
            <va-icon name="account_balance_wallet" />
            <va-sidebar-item-title>{{ $t('app.topups') }}</va-sidebar-item-title>
          </va-sidebar-item-content>
        </va-sidebar-item>

        <va-sidebar-item :active="currentRouteName === 'admin-transactions'" to="/admin/transactions">
          <va-sidebar-item-content>
            <va-icon name="history" />
            <va-sidebar-item-title>{{ $t('app.transactions') }}</va-sidebar-item-title>
          </va-sidebar-item-content>
        </va-sidebar-item>

        <va-sidebar-item :active="currentRouteName === 'admin-settings'" to="/admin/settings">
          <va-sidebar-item-content>
            <va-icon name="settings" />
            <va-sidebar-item-title>{{ $t('app.settings') }}</va-sidebar-item-title>
          </va-sidebar-item-content>
        </va-sidebar-item>

        <va-sidebar-item :active="currentRouteName === 'admin-shifts'" to="/admin/shifts">
          <va-sidebar-item-content>
            <va-icon name="schedule" />
            <va-sidebar-item-title>{{ $t('app.activeShifts') }}</va-sidebar-item-title>
          </va-sidebar-item-content>
        </va-sidebar-item>

        <va-sidebar-item :active="currentRouteName === 'admin-active-orders'" to="/admin/orders/active">
          <va-sidebar-item-content>
            <va-icon name="pending_actions" />
            <va-sidebar-item-title>{{ $t('app.activeOrders') }}</va-sidebar-item-title>
          </va-sidebar-item-content>
        </va-sidebar-item>

        <va-sidebar-item :active="currentRouteName === 'admin-completed-orders'" to="/admin/orders/completed">
          <va-sidebar-item-content>
            <va-icon name="check_circle" />
            <va-sidebar-item-title>{{ $t('app.completedOrders') }}</va-sidebar-item-title>
          </va-sidebar-item-content>
        </va-sidebar-item>

        <va-sidebar-item :active="currentRouteName === 'admin-service-catalog'" to="/admin/service-catalog">
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
import { defineComponent, computed } from 'vue'
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
  height: 60px;
  box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
  z-index: 10;
}

.app-layout__container {
  display: flex;
  flex: 1;
  overflow: hidden;
}

.app-layout__sidebar {
  width: 240px !important;
  flex-shrink: 0;
  box-shadow: 2px 0 4px rgba(0, 0, 0, 0.05);
}

.app-layout__main {
  flex: 1;
  padding: 20px;
  background-color: #f6f8fa;
  overflow-y: auto;
}

.main-content {
  padding: 24px;
  background: white;
  border-radius: 8px;
  box-shadow: 0 4px 6px -1px rgba(0,0,0,0.1);
  min-height: 80vh;
}

.logo {
  font-size: 1.2rem;
  color: white;
}

.user-info {
  color: white;
  font-size: 0.9rem;
  margin-right: 15px;
}
</style>
