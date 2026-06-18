import { createRouter, createWebHistory, RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '../stores/auth-store'

const routes: Array<RouteRecordRaw> = [
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
    ],
  },
  {
    path: '/customer',
    name: 'customer-dashboard',
    component: () => import('../pages/customer/CustomerDashboard.vue'),
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
  history: createWebHistory(),
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

export default router
