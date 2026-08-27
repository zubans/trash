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
        if (authStore.isAdmin) {
          return '/admin'
        }
        if (authStore.isCustomer) {
          return '/customer'
        }
        if (authStore.isExecutor) {
          return '/executor'
        }
      }
      return '/login'
    },
  },
]

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
      if (requiredRole && authStore.role !== requiredRole) {
        // Unauthorized role - send to correct dashboard
        if (authStore.isAdmin) {
          next('/admin')
        } else if (authStore.isCustomer) {
          next('/customer')
        } else if (authStore.isExecutor) {
          next('/executor')
        } else {
          next('/login')
        }
      } else {
        next()
      }
    }
  } else if (to.matched.some((record) => record.meta.requiresGuest)) {
    if (authStore.isAuthenticated) {
      if (authStore.isAdmin) {
        next('/admin')
      } else if (authStore.isCustomer) {
        next('/customer')
      } else if (authStore.isExecutor) {
        next('/executor')
      } else {
        next()
      }
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
