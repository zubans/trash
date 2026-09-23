import { createRouter, createWebHistory, createWebHashHistory, RouteRecordRaw } from 'vue-router'
import { Capacitor } from '@capacitor/core'
import { useAuthStore } from '../stores/auth-store'

import LoginView from '../pages/auth/Login.vue'
import CustomerDashboardV2View from '../pages/customer/CustomerDashboardV2.vue'
import CustomerProfilePageView from '../pages/customer/CustomerProfilePage.vue'
import ExecutorDashboardView from '../pages/executor/ExecutorDashboard.vue'
import ExecutorProfilePageView from '../pages/executor/ExecutorProfilePage.vue'
import AchievementsPageView from '../pages/executor/AchievementsPage.vue'
import GiftsPageView from '../pages/executor/GiftsPage.vue'
import MailPageView from '../pages/MailPage.vue'
import OrderHistoryPageView from '../pages/shared/OrderHistoryPage.vue'

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
    // Панель открывает не роль ADMIN, а наличие хоть одного права в её
    // разделах: роль «финансист» с одной галочкой «сверка» обязана дойти до
    // своей страницы. Что именно доступно внутри, решает meta.permission
    // каждого маршрута — те же коды, что охраняют маршруты на бэкенде.
    path: '/admin',
    component: () => import('../AppLayout.vue'),
    meta: { requiresAuth: true, adminPanel: true },
    children: [
      {
        path: '',
        redirect: () => adminHome(),
      },
      {
        path: 'users',
        name: 'admin-users',
        component: () => import('../pages/admin/UserList.vue'),
        meta: { permission: 'users.view', bare: true },
      },
      {
        path: 'roles',
        name: 'admin-roles',
        component: () => import('../pages/admin/Roles.vue'),
        meta: { permission: 'roles.view', bare: true },
      },
      {
        path: 'support-chats',
        name: 'admin-support-chats',
        component: () => import('../pages/admin/AdminSupportChats.vue'),
        meta: { permission: 'support_chats.view', flush: true },
      },
      {
        path: 'topups',
        name: 'admin-topups',
        component: () => import('../pages/admin/TopUpRequests.vue'),
        meta: { permission: 'topups.view' },
      },
      {
        path: 'withdrawals',
        name: 'admin-withdrawals',
        component: () => import('../pages/admin/WithdrawalRequests.vue'),
        meta: { permission: 'withdrawals.view' },
      },
      {
        path: 'commission',
        name: 'admin-commission',
        component: () => import('../pages/admin/PlatformCommission.vue'),
        meta: { permission: 'commission.view' },
      },
      {
        path: 'transactions',
        name: 'admin-transactions',
        component: () => import('../pages/admin/TransactionHistory.vue'),
        meta: { permission: 'transactions.view' },
      },
      {
        path: 'reconciliation',
        name: 'admin-reconciliation',
        component: () => import('../pages/admin/FinancialReconciliation.vue'),
        meta: { permission: 'reconciliation.view' },
      },
      {
        path: 'settings',
        name: 'admin-settings',
        component: () => import('../pages/admin/SystemSettings.vue'),
        meta: { permission: 'settings.view', bare: true },
      },
      {
        path: 'shifts',
        name: 'admin-shifts',
        component: () => import('../pages/admin/ActiveShifts.vue'),
        meta: { permission: 'shifts.view' },
      },
      {
        path: 'orders',
        name: 'admin-orders',
        component: () => import('../pages/admin/Orders.vue'),
        meta: { permission: 'orders.view' },
      },
      // Старые адреса разделов «Активные» и «Выполненные» ведут на общий список
      // с тем же фильтром: на них могли остаться закладки.
      { path: 'orders/active', redirect: { path: '/admin/orders', query: { status: 'active' } } },
      { path: 'orders/completed', redirect: { path: '/admin/orders', query: { status: 'completed' } } },
      {
        path: 'service-catalog',
        name: 'admin-service-catalog',
        component: () => import('../pages/admin/ServiceCatalog.vue'),
        meta: { permission: 'service_catalog.view', bare: true },
      },
      {
        path: 'broadcasts',
        name: 'admin-broadcasts',
        component: () => import('../pages/admin/EmailBroadcasts.vue'),
        meta: { permission: 'broadcasts.view' },
      },
      {
        path: 'mail',
        name: 'admin-mail',
        component: () => import('../pages/admin/Mail.vue'),
        meta: { permission: 'mail.view', flush: true },
      },
      {
        // Очередь заявок на статус «проверенный»: её ведёт поддержка, а не
        // карточка отдельного пользователя.
        path: 'check-requests',
        name: 'admin-check-requests',
        component: () => import('../pages/admin/CheckRequests.vue'),
        meta: { permission: 'checks.view', bare: true },
      },
      {
        path: 'escalations',
        name: 'admin-escalations',
        component: () => import('../pages/admin/Escalations.vue'),
        meta: { permission: 'escalations.view', bare: true },
      },
      {
        path: 'disputes',
        name: 'admin-disputes',
        component: () => import('../pages/admin/Disputes.vue'),
        meta: { permission: 'disputes.view', bare: true },
      },
      {
        path: 'watermark-symbols',
        name: 'admin-watermark-symbols',
        component: () => import('../pages/admin/WatermarkSymbols.vue'),
        meta: { permission: 'watermarks.view', bare: true },
      },
      {
        path: 'achievements',
        name: 'admin-achievements',
        component: () => import('../pages/admin/Achievements.vue'),
        meta: { permission: 'achievements.view', bare: true },
      },
      {
        path: 'gifts',
        name: 'admin-gifts',
        component: () => import('../pages/admin/Gifts.vue'),
        meta: { permission: 'gifts.view', bare: true },
      },
      {
        path: 'incidents',
        name: 'admin-incidents',
        component: () => import('../pages/admin/MoneyIncidents.vue'),
        meta: { permission: 'incidents.view', bare: true },
      },
      {
        // Справочник для редактора скриптов в конструкторе услуг. Не в меню: до него
        // добираются по ссылке «как писать скрипты» рядом с редактором, там, где
        // вопрос и возникает.
        path: 'service-scripts',
        name: 'admin-service-scripts-help',
        component: () => import('../pages/admin/ServiceScriptHelp.vue'),
        meta: { permission: 'service_catalog.view', bare: true },
      },
      // Магазин: три раздела прав — товары, заказы, выручка (service/permission.go).
      {
        path: 'shop/products',
        name: 'admin-shop-products',
        component: () => import('../pages/admin/ShopProducts.vue'),
        meta: { permission: 'shop.view', bare: true },
      },
      {
        path: 'shop/pickup-points',
        name: 'admin-shop-pickup-points',
        component: () => import('../pages/admin/ShopPickupPoints.vue'),
        meta: { permission: 'shop.view', bare: true },
      },
      {
        path: 'shop/perk-rules',
        name: 'admin-shop-perk-rules',
        component: () => import('../pages/admin/ShopPerkRules.vue'),
        meta: { permission: 'perk_rules.view', bare: true },
      },
      {
        path: 'shop/orders',
        name: 'admin-shop-orders',
        component: () => import('../pages/admin/ShopOrders.vue'),
        meta: { permission: 'shop_orders.view', bare: true },
      },
      {
        path: 'shop/revenue',
        name: 'admin-shop-revenue',
        component: () => import('../pages/admin/ShopRevenue.vue'),
        meta: { permission: 'shop_revenue.view' },
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
    path: '/customer/history',
    name: 'customer-history',
    component: OrderHistoryPageView,
    props: { role: 'CUSTOMER' },
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
    path: '/executor/history',
    name: 'executor-history',
    component: OrderHistoryPageView,
    props: { role: 'EXECUTOR' },
    meta: { requiresAuth: true, role: 'EXECUTOR' },
  },
  {
    path: '/executor/achievements',
    name: 'executor-achievements',
    component: AchievementsPageView,
    meta: { requiresAuth: true, role: 'EXECUTOR' },
  },
  {
    path: '/executor/gifts',
    name: 'executor-gifts',
    component: GiftsPageView,
    props: { role: 'EXECUTOR' },
    meta: { requiresAuth: true, role: 'EXECUTOR' },
  },
  // Магазин один на обе роли: что кому видно, решает сервер по ролям на
  // товаре; маршруты разные, чтобы «назад» вело на свой дашборд.
  {
    path: '/customer/shop',
    name: 'customer-shop',
    component: () => import('../pages/shared/ShopPage.vue'),
    props: { role: 'CUSTOMER' },
    meta: { requiresAuth: true, role: 'CUSTOMER' },
  },
  {
    path: '/customer/shop/:id',
    name: 'customer-shop-product',
    component: () => import('../pages/shared/ShopProductPage.vue'),
    props: { role: 'CUSTOMER' },
    meta: { requiresAuth: true, role: 'CUSTOMER' },
  },
  {
    // Купоны на купленные вещи. У заказчика ачивок нет, поэтому страница
    // подарков у него — это купоны магазина.
    path: '/customer/gifts',
    name: 'customer-gifts',
    component: GiftsPageView,
    props: { role: 'CUSTOMER' },
    meta: { requiresAuth: true, role: 'CUSTOMER' },
  },
  {
    path: '/executor/shop',
    name: 'executor-shop',
    component: () => import('../pages/shared/ShopPage.vue'),
    props: { role: 'EXECUTOR' },
    meta: { requiresAuth: true, role: 'EXECUTOR' },
  },
  {
    path: '/executor/shop/:id',
    name: 'executor-shop-product',
    component: () => import('../pages/shared/ShopProductPage.vue'),
    props: { role: 'EXECUTOR' },
    meta: { requiresAuth: true, role: 'EXECUTOR' },
  },
  {
    // Согласие на обработку персональных данных открыто без входа: на него
    // ведёт ссылка из формы регистрации.
    path: '/legal/personal-data',
    name: 'personal-data-consent',
    component: () => import('../pages/shared/PersonalDataConsentPage.vue'),
  },
  {
    // Оферта без требования роли: её читают из окна оформления любой роли.
    path: '/shop/offer',
    name: 'shop-offer',
    component: () => import('../pages/shared/ShopOfferPage.vue'),
    meta: { requiresAuth: true },
  },
  {
    // Почта без требования роли: новость адресуется человеку, а не его роли в
    // заказе, и ящик один на все роли, между которыми он переключается.
    path: '/mail',
    name: 'mail',
    component: MailPageView,
    meta: { requiresAuth: true },
  },
  {
    // Пароль — не часть профиля: это про вход, и страница одна для всех ролей.
    path: '/password',
    name: 'change-password',
    component: () => import('../pages/shared/ChangePasswordPage.vue'),
    meta: { requiresAuth: true },
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

// dashboardHome выбирает начальный маршрут для вошедшего пользователя, учитывая
// роль, на которую он переключался последней (activeRole), и откатываясь к любой
// его роли. MODERATOR делит дашборд с исполнителем.
function dashboardHome(authStore: ReturnType<typeof useAuthStore>): string {
  const active = authStore.activeRole
  if (active === 'ADMIN' && authStore.isAdmin) return '/admin'
  if (active === 'CUSTOMER' && authStore.isCustomer) return '/customer'
  if (active === 'EXECUTOR' && (authStore.isExecutor || authStore.isModerator)) return '/executor'
  if (authStore.isAdmin) return '/admin'
  if (authStore.isExecutor || authStore.isModerator) return '/executor'
  if (authStore.isCustomer) return '/customer'
  // Роль, заведённая администратором, может не быть ни заказчиком, ни
  // исполнителем — только набором разделов панели. Её дом там.
  if (authStore.hasAdminAccess) return '/admin'
  return '/login'
}

// canAccessRole сообщает, может ли пользователь открыть маршрут с требованием requiredRole.
// MODERATOR может открыть дашборд EXECUTOR (заказы модератора живут там).
function canAccessRole(authStore: ReturnType<typeof useAuthStore>, requiredRole: string): boolean {
  if (!requiredRole) return true
  if (authStore.hasRole(requiredRole)) return true
  if (requiredRole === 'EXECUTOR' && authStore.isModerator) return true
  return false
}

// adminHome выбирает первую страницу панели, до которой у пользователя есть
// право. Жёсткий редирект на /admin/users отправлял бы роль без права на
// пользователей прямиком в отказ на её собственной стартовой странице.
function adminHome(): string {
  const authStore = useAuthStore()
  const first = adminSections.find((section) => authStore.can(section.permission))
  return first ? first.path : '/mail'
}

// adminSections перечисляет разделы панели в порядке меню: он же — порядок
// поиска стартовой страницы.
const adminSections: { path: string; permission: string }[] = [
  { path: '/admin/users', permission: 'users.view' },
  { path: '/admin/roles', permission: 'roles.view' },
  { path: '/admin/support-chats', permission: 'support_chats.view' },
  { path: '/admin/topups', permission: 'topups.view' },
  { path: '/admin/withdrawals', permission: 'withdrawals.view' },
  { path: '/admin/commission', permission: 'commission.view' },
  { path: '/admin/transactions', permission: 'transactions.view' },
  { path: '/admin/reconciliation', permission: 'reconciliation.view' },
  { path: '/admin/incidents', permission: 'incidents.view' },
  { path: '/admin/broadcasts', permission: 'broadcasts.view' },
  { path: '/admin/mail', permission: 'mail.view' },
  { path: '/admin/shifts', permission: 'shifts.view' },
  { path: '/admin/orders', permission: 'orders.view' },
  { path: '/admin/service-catalog', permission: 'service_catalog.view' },
  { path: '/admin/achievements', permission: 'achievements.view' },
  { path: '/admin/gifts', permission: 'gifts.view' },
  { path: '/admin/check-requests', permission: 'checks.view' },
  { path: '/admin/escalations', permission: 'escalations.view' },
  { path: '/admin/disputes', permission: 'disputes.view' },
  { path: '/admin/watermark-symbols', permission: 'watermarks.view' },
  { path: '/admin/shop/products', permission: 'shop.view' },
  { path: '/admin/shop/perk-rules', permission: 'perk_rules.view' },
  { path: '/admin/shop/orders', permission: 'shop_orders.view' },
  { path: '/admin/shop/revenue', permission: 'shop_revenue.view' },
  { path: '/admin/settings', permission: 'settings.view' },
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
    } else if (to.matched.some((record) => record.meta.adminPanel) && !authStore.hasAdminAccess) {
      // В панель нет доступа вовсе — отправляем на дашборд, которым он может пользоваться.
      next(dashboardHome(authStore))
    } else {
      const requiredRole = to.meta.role as string
      const requiredPermission = to.meta.permission as string
      if (!canAccessRole(authStore, requiredRole)) {
        // Нет прав на эту роль — отправляем на дашборд, которым он может пользоваться.
        next(dashboardHome(authStore))
      } else if (requiredPermission && !authStore.can(requiredPermission)) {
        // Доступ в панель есть, а до этого раздела права нет: ведём на первый
        // раздел, который ему открыт, а не в отказ.
        next(adminHome())
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
