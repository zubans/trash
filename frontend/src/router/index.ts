import { createRouter, createWebHistory, createWebHashHistory, RouteRecordRaw } from 'vue-router'
import { Capacitor } from '@capacitor/core'
import { useAuthStore } from '../stores/auth-store'

const routes: Array<RouteRecordRaw> = [
  {
    path: '/',
    redirect: '/login',
  },
  {
    path: '/login',
    name: 'login',
    component: () => import('../pages/auth/Login.vue'),
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
        path: 'topups',
        name: 'admin-topups',
        component: () => import('../pages/admin/TopUpRequests.vue'),
      },
      {
        path: 'transactions',
        name: 'admin-transactions',
        component: () => import('../pages/admin/TransactionHistory.vue'),
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
    ],
  },
  {
    path: '/customer',
    name: 'customer-dashboard',
    component: () => import('../pages/customer/CustomerDashboardV2.vue'),
    meta: { requiresAuth: true, role: 'CUSTOMER' },
  },
  {
    path: '/customer-v2',
    name: 'customer-dashboard-v2',
    component: () => import('../pages/customer/CustomerDashboardV2.vue'),
    meta: { requiresAuth: true, role: 'CUSTOMER' },
  },
  {
    path: '/executor',
    name: 'executor-dashboard',
    component: () => import('../pages/executor/ExecutorDashboard.vue'),
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

router.beforeEach((to, from, next) => {
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
    if (to?.fullPath) {
      window.location.href = to.fullPath
    } else {
      window.location.reload()
    }
  }
})

export default router
