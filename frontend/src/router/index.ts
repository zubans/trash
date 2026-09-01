import { createRouter, createWebHistory, createWebHashHistory, RouteRecordRaw } from 'vue-router'
import { Capacitor } from '@capacitor/core'
import { useAuthStore } from '../stores/auth-store'

import LoginView from '../pages/auth/Login.vue'
import CustomerDashboardV2View from '../pages/customer/CustomerDashboardV2.vue'
import CustomerProfilePageView from '../pages/customer/CustomerProfilePage.vue'
import ExecutorDashboardView from '../pages/executor/ExecutorDashboard.vue'
import ExecutorProfilePageView from '../pages/executor/ExecutorProfilePage.vue'

const routes: Array<RouteRecordRaw> = [
  {
    path: '/',
    redirect: '/login',
  },
  {
    path: '/login',
    name: 'login',
    component: LoginView,
    meta: { requiresGuest: true },
  },
  {
    path: '/admin',
    component: () => import('../AppLayout.vue'),
    meta: { requiresAuth: true, role: 'ADMIN' },
    children: [
      {
        path: '',
        redirect: '/admin/users',
      },
      {
        path: 'users',
        name: 'admin-users',
        component: () => import('../pages/admin/UserList.vue'),
      },
      {
        path: 'support-chats',
        name: 'admin-support-chats',
        component: () => import('../pages/admin/AdminSupportChats.vue'),
      },
      {
        path: 'topups',
        name: 'admin-topups',
        component: () => import('../pages/admin/TopUpRequests.vue'),
      },
      {
        path: 'withdrawals',
        name: 'admin-withdrawals',
        component: () => import('../pages/admin/WithdrawalRequests.vue'),
      },
      {
        path: 'commission',
        name: 'admin-commission',
        component: () => import('../pages/admin/PlatformCommission.vue'),
      },
      {
        path: 'transactions',
        name: 'admin-transactions',
        component: () => import('../pages/admin/TransactionHistory.vue'),
      },
      {
        path: 'reconciliation',
        name: 'admin-reconciliation',
        component: () => import('../pages/admin/FinancialReconciliation.vue'),
      },
      {
        path: 'settings',
        name: 'admin-settings',
        component: () => import('../pages/admin/SystemSettings.vue'),
      },
      {
        path: 'shifts',
        name: 'admin-shifts',
        component: () => import('../pages/admin/ActiveShifts.vue'),
      },
      {
        path: 'orders/active',
        name: 'admin-active-orders',
        component: () => import('../pages/admin/ActiveOrders.vue'),
      },
      {
        path: 'orders/completed',
        name: 'admin-completed-orders',
        component: () => import('../pages/admin/CompletedOrders.vue'),
      },
      {
        path: 'service-catalog',
        name: 'admin-service-catalog',
        component: () => import('../pages/admin/ServiceCatalog.vue'),
      },
      {
        path: 'broadcasts',
        name: 'admin-broadcasts',
        component: () => import('../pages/admin/EmailBroadcasts.vue'),
      },
      {
        path: 'escalations',
        name: 'admin-escalations',
        component: () => import('../pages/admin/Escalations.vue'),
      },
      {
        // Reference for the script editor in the service constructor. Not in the
        // menu: it is reached from the "как писать скрипты" link next to the
        // editor, where the question actually comes up.
        path: 'service-scripts',
        name: 'admin-service-scripts-help',
        component: () => import('../pages/admin/ServiceScriptHelp.vue'),
      },
    ],
  },
  {
    path: '/customer',
    name: 'customer-dashboard',
    component: CustomerDashboardV2View,
    meta: { requiresAuth: true, role: 'CUSTOMER' },
  },
  {
    path: '/customer/profile',
    name: 'customer-profile',
    component: CustomerProfilePageView,
    meta: { requiresAuth: true, role: 'CUSTOMER' },
  },
  {
    path: '/customer-v2',
    name: 'customer-dashboard-v2',
    component: CustomerDashboardV2View,
    meta: { requiresAuth: true, role: 'CUSTOMER' },
  },
  {
    path: '/executor',
    name: 'executor-dashboard',
    component: ExecutorDashboardView,
    meta: { requiresAuth: true, role: 'EXECUTOR' },
  },
  {
    path: '/executor/profile',
    name: 'executor-profile',
    component: ExecutorProfilePageView,
    meta: { requiresAuth: true, role: 'EXECUTOR' },
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: () => {
      const authStore = useAuthStore()
      if (authStore.isAuthenticated) {
        return dashboardHome(authStore)
      }
      return '/login'
    },
  },
]

// dashboardHome picks the landing route for a signed-in user, honouring the
// role they last switched to (activeRole) and falling back to any role they
// hold. MODERATOR shares the executor dashboard.
function dashboardHome(authStore: ReturnType<typeof useAuthStore>): string {
  const active = authStore.activeRole
  if (active === 'ADMIN' && authStore.isAdmin) return '/admin'
  if (active === 'CUSTOMER' && authStore.isCustomer) return '/customer'
  if (active === 'EXECUTOR' && (authStore.isExecutor || authStore.isModerator)) return '/executor'
  if (authStore.isAdmin) return '/admin'
  if (authStore.isExecutor || authStore.isModerator) return '/executor'
  if (authStore.isCustomer) return '/customer'
  return '/login'
}

// canAccessRole reports whether the user may open a route gated on requiredRole.
// A MODERATOR may open the EXECUTOR dashboard (moderator orders live there).
function canAccessRole(authStore: ReturnType<typeof useAuthStore>, requiredRole: string): boolean {
  if (!requiredRole) return true
  if (authStore.hasRole(requiredRole)) return true
  if (requiredRole === 'EXECUTOR' && authStore.isModerator) return true
  return false
}

const router = createRouter({
  history: Capacitor.isNativePlatform() ? createWebHashHistory() : createWebHistory(),
  routes,
})

router.beforeEach((to, _from, next) => {
  const authStore = useAuthStore()

  if (to.matched.some((record) => record.meta.requiresAuth)) {
    if (!authStore.isAuthenticated) {
      next('/login')
    } else {
      const requiredRole = to.meta.role as string
      if (!canAccessRole(authStore, requiredRole)) {
        // Not authorized for this role — send to a dashboard they can use.
        next(dashboardHome(authStore))
      } else {
        next()
      }
    }
  } else if (to.matched.some((record) => record.meta.requiresGuest)) {
    if (authStore.isAuthenticated) {
      next(dashboardHome(authStore))
    } else {
      next()
    }
  } else {
    next()
  }
})

router.onError((error, to) => {
  if (
    error.message.includes('Failed to fetch dynamically imported module') ||
    error.message.includes('Importing a module script failed')
  ) {
    console.warn('Chunk load failed due to new build, reloading page to fetch latest version...', error)
    if (Capacitor.isNativePlatform()) {
      if (to?.fullPath) {
        window.location.hash = `#${to.fullPath}`
      } else {
        window.location.reload()
      }
    } else {
      if (to?.fullPath) {
        window.location.href = to.fullPath
      } else {
        window.location.reload()
      }
    }
  }
})

export default router
