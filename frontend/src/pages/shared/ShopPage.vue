<template>
  <div class="shop-page">
    <div class="shop-container">
      <div class="top-nav">
        <button type="button" class="btn-back" @click="goHome">
          <i class="ph-bold ph-arrow-left"></i>
          {{ $t('shop.back') }}
        </button>
        <h1 class="page-title">{{ $t('shop.title') }}</h1>
        <div v-if="balance !== null" class="balance-pill">{{ money(balance) }}</div>
      </div>

      <div class="segmented" role="tablist">
        <button
          v-for="t in tabs"
          :key="t"
          type="button"
          role="tab"
          class="segment"
          :class="{ active: tab === t }"
          :aria-selected="tab === t"
          @click="setTab(t)"
        >
          {{ $t('shop.tabs.' + t) }}
        </button>
      </div>

      <!-- Витрина -->
      <template v-if="tab === 'store'">
        <SkeletonList v-if="store.loading.value" :rows="3" />
        <div v-else-if="store.error.value && !storefront.products.length" class="state-note">
          {{ $t('shop.loadFailed') }}
          <button type="button" class="link-btn" @click="store.reload()">{{ $t('common.retry') }}</button>
        </div>
        <div v-else-if="!storefront.enabled" class="state-note">{{ $t('shop.closed') }}</div>
        <template v-else>
          <div v-if="categories.length > 1" class="chips">
            <button
              v-for="c in ['', ...categories]"
              :key="c || 'all'"
              type="button"
              class="chip-btn"
              :class="{ active: category === c }"
              @click="category = c"
            >
              {{ categoryTitle(c) }}
            </button>
          </div>

          <div v-if="!visibleProducts.length" class="state-note">{{ $t('shop.empty') }}</div>
          <div v-else class="grid">
            <button
              v-for="p in visibleProducts"
              :key="p.id"
              type="button"
              class="product"
              :class="{ out: !p.in_stock }"
              @click="openProduct(p.id)"
            >
              <div class="product-image">
                <img v-if="p.images.length" :src="imageUrl(p.images[0])" :alt="localized(p.title)" loading="lazy" />
                <i v-else :class="kindIcon(p.kind)"></i>
              </div>
              <div class="product-body">
                <div class="product-title">{{ localized(p.title) }}</div>
                <div v-if="p.kind === 'PERK'" class="product-sub">
                  {{ $t('shop.perk.days', { days: p.perk_days }) }}
                </div>
                <div class="product-price">
                  <span>{{ money(p.price) }}</span>
                  <s v-if="p.compare_at_price" class="old-price">{{ money(p.compare_at_price) }}</s>
                </div>
                <div v-if="!p.in_stock" class="out-label">{{ $t('shop.outOfStock') }}</div>
              </div>
            </button>
          </div>
        </template>

        <div class="footer">
          <router-link to="/shop/offer">{{ $t('shop.offerLink') }}</router-link>
        </div>
      </template>

      <!-- Мои покупки -->
      <template v-else>
        <SkeletonList v-if="ordersResource.loading.value" :rows="3" />
        <div v-else-if="ordersResource.error.value && !orders.length" class="state-note">
          {{ $t('shop.loadFailed') }}
          <button type="button" class="link-btn" @click="ordersResource.reload()">{{ $t('common.retry') }}</button>
        </div>
        <div v-else-if="!orders.length" class="state-note">{{ $t('shop.orders.empty') }}</div>
        <div v-else class="list">
          <div v-for="o in orders" :key="o.id" class="order-row">
            <button type="button" class="order-main" @click="openOrder(o)">
              <div class="row-main">
                <div class="row-title">{{ localized(o.product_snapshot.title) }}</div>
                <div class="row-sub">
                  {{ $t('shop.orders.number', { number: o.number }) }} · {{ formatDate(o.created_at) }}
                  <template v-if="o.quantity > 1"> · {{ $t('shop.orders.quantity', { count: o.quantity }) }}</template>
                </div>
              </div>
              <div class="row-side">
                <div class="row-amount">{{ money(o.total) }}</div>
                <span :class="['status', o.status]">{{ $t('shop.status.' + o.status) }}</span>
              </div>
            </button>
            <button v-if="o.status !== 'CANCELED'" type="button" class="refund-link" @click="requestRefund(o)">
              {{ $t('shop.orders.refund') }}
            </button>
          </div>
          <div class="refund-note">
            {{ $t('shop.orders.refundHint') }}
            <router-link :to="{ path: '/shop/offer', hash: '#section-8' }">{{ $t('shop.orders.refundOffer') }}</router-link>
          </div>
        </div>
      </template>
    </div>

    <ShopOrderModal
      :order="selectedOrder"
      :currency-symbol="currencySymbol"
      @close="closeOrder"
      @refund="requestRefund"
    />
    <SupportChatModal v-model:show="showSupport" :prefill="supportPrefill" />
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, onMounted, ref, watch, type PropType } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../../stores/auth-store'
import { resolveFileUrl } from '../../services/api'
import { useCachedResource } from '../../composables/useCachedResource'
import { SHOP_ORDERS_KEY, useStorefront } from '../../composables/useShop'
import SkeletonList from '../../components/SkeletonList.vue'
import SupportChatModal from '../../components/SupportChatModal.vue'
import ShopOrderModal from '../../components/shop/ShopOrderModal.vue'
import { getMyOrder, getMyOrders, localized as pick, type Localized, type ShopOrder } from '../../api/shop'

type Role = 'EXECUTOR' | 'CUSTOMER'
type Tab = 'store' | 'orders'

const HOME: Record<Role, string> = { EXECUTOR: '/executor', CUSTOMER: '/customer' }
const KIND_ICONS: Record<string, string> = {
  PERK: 'ph-fill ph-lightning',
  PHYSICAL: 'ph-fill ph-t-shirt',
  CERTIFICATE: 'ph-fill ph-ticket',
}

// Магазин один на обе роли: какие товары кому видны, решает сервер по ролям на
// товаре; отличается только то, куда вернуться (implementation_plan_shop.md §8).
export default defineComponent({
  name: 'ShopPage',
  components: { SkeletonList, SupportChatModal, ShopOrderModal },
  props: {
    role: { type: String as PropType<Role>, required: true },
  },
  setup(props) {
    const router = useRouter()
    const route = useRoute()
    const { t, locale } = useI18n()
    const authStore = useAuthStore()
    const home = HOME[props.role] || HOME.CUSTOMER

    const tabs: Tab[] = ['store', 'orders']
    const tab = ref<Tab>(route.query.tab === 'orders' ? 'orders' : 'store')
    const category = ref('')

    const store = useStorefront()
    const storefront = store.data
    const ordersResource = useCachedResource<ShopOrder[]>({
      key: SHOP_ORDERS_KEY,
      initial: [],
      fetcher: getMyOrders,
    })
    const orders = ordersResource.data

    const currencySymbol = computed(() => (authStore.currency === 'RUB' ? '₽' : '$'))
    const balance = computed(() => authStore.balance)
    const money = (value: number) =>
      `${Number(value || 0).toLocaleString('ru-RU', { maximumFractionDigits: 2 })} ${currencySymbol.value}`
    const localized = (value?: Localized) => pick(value, locale.value)
    const formatDate = (value: string) =>
      new Date(value).toLocaleDateString(locale.value === 'en' ? 'en-GB' : 'ru-RU', { day: 'numeric', month: 'short' })
    const imageUrl = (path: string) => resolveFileUrl(path)
    const kindIcon = (kind: string) => KIND_ICONS[kind] || 'ph-fill ph-bag'

    const categories = computed(() => {
      const seen: string[] = []
      for (const p of storefront.value.products) if (!seen.includes(p.category)) seen.push(p.category)
      return seen
    })
    const categoryTitle = (c: string) => {
      if (!c) return t('shop.categories.all')
      const key = `shop.categories.${c}`
      const translated = t(key)
      return translated === key ? c : translated
    }
    const visibleProducts = computed(() =>
      storefront.value.products.filter((p) => !category.value || p.category === category.value),
    )

    const setTab = (next: Tab) => {
      tab.value = next
      router.replace({ query: { ...route.query, tab: next === 'orders' ? 'orders' : undefined, order: undefined } })
      if (next === 'orders') ordersResource.load()
    }

    const openProduct = (id: string) => router.push(`${home}/shop/${id}`)

    // Карточка покупки открывается и по ссылке из письма (?order=id): тогда
    // её ещё может не быть в списке, и она читается отдельно.
    const selectedOrder = ref<ShopOrder | null>(null)
    const openOrder = async (order: ShopOrder) => {
      selectedOrder.value = order
      try {
        selectedOrder.value = await getMyOrder(order.id)
      } catch {
        /* в списке уже есть всё, кроме свежего статуса */
      }
    }
    const closeOrder = () => {
      selectedOrder.value = null
      if (route.query.order) router.replace({ query: { ...route.query, order: undefined } })
    }
    const openOrderFromQuery = async () => {
      const id = route.query.order
      if (typeof id !== 'string' || !id) return
      tab.value = 'orders'
      try {
        selectedOrder.value = await getMyOrder(id)
      } catch {
        selectedOrder.value = null
      }
    }

    // «Оформить возврат» — только чат поддержки с подставленным номером;
    // отправляет сообщение сам покупатель (оферта, п. 8.2).
    const showSupport = ref(false)
    const supportPrefill = ref('')
    const requestRefund = (order: ShopOrder) => {
      supportPrefill.value = t('shop.orders.refundPrefill', { number: order.number })
      selectedOrder.value = null
      showSupport.value = true
    }

    const goHome = () => router.push(home)

    watch(() => route.query.order, openOrderFromQuery)

    onMounted(() => {
      store.load()
      authStore.fetchMe()
      if (tab.value === 'orders' || route.query.order) ordersResource.load()
      openOrderFromQuery()
    })

    return {
      tabs, tab, category, store, storefront, ordersResource, orders, currencySymbol, balance,
      money, localized, formatDate, imageUrl, kindIcon, categories, categoryTitle, visibleProducts,
      setTab, openProduct, selectedOrder, openOrder, closeOrder, showSupport, supportPrefill,
      requestRefund, goHome,
    }
  },
})
</script>

<style scoped>
.shop-page {
  min-height: 100vh;
  background: #f6f7fb;
  padding: 16px;
  padding-bottom: calc(24px + env(safe-area-inset-bottom));
}
.shop-container {
  max-width: 720px;
  margin: 0 auto;
}
.top-nav {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
}
.btn-back {
  border: none;
  background: #fff;
  border-radius: 10px;
  padding: 8px 12px;
  font-size: 14px;
  color: #475569;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.page-title {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
  color: #0f172a;
  flex: 1;
}
.balance-pill {
  background: #fff;
  border-radius: 999px;
  padding: 6px 12px;
  font-size: 14px;
  font-weight: 600;
  color: #0f172a;
  white-space: nowrap;
}
.segmented {
  display: flex;
  background: #eceef3;
  border-radius: 10px;
  padding: 3px;
  margin-bottom: 16px;
}
.segment {
  flex: 1;
  border: none;
  background: transparent;
  border-radius: 8px;
  padding: 7px 0;
  font-size: 13px;
  font-weight: 500;
  color: #64748b;
  cursor: pointer;
  font-family: inherit;
}
.segment.active {
  background: #fff;
  color: #0f172a;
  box-shadow: 0 1px 2px rgba(15, 23, 42, 0.08);
}
.chips {
  display: flex;
  gap: 8px;
  overflow-x: auto;
  margin-bottom: 12px;
  padding-bottom: 2px;
}
.chip-btn {
  border: 1px solid #e2e8f0;
  background: #fff;
  border-radius: 999px;
  padding: 6px 14px;
  font: inherit;
  font-size: 13px;
  color: #475569;
  white-space: nowrap;
  cursor: pointer;
}
.chip-btn.active {
  background: #0f172a;
  border-color: #0f172a;
  color: #fff;
}
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
  gap: 12px;
}
.product {
  display: flex;
  flex-direction: column;
  background: #fff;
  border: none;
  border-radius: 16px;
  overflow: hidden;
  padding: 0;
  text-align: left;
  font: inherit;
  cursor: pointer;
}
.product.out {
  opacity: 0.6;
}
.product-image {
  aspect-ratio: 4 / 3;
  background: #eef2ff;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #6366f1;
  font-size: 40px;
}
.product-image img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.product-body {
  padding: 10px 12px 12px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.product-title {
  font-size: 14px;
  font-weight: 600;
  color: #0f172a;
}
.product-sub {
  font-size: 12px;
  color: #64748b;
}
.product-price {
  display: flex;
  align-items: baseline;
  gap: 6px;
  font-size: 15px;
  font-weight: 700;
  color: #0f172a;
}
.old-price {
  font-size: 12px;
  font-weight: 500;
  color: #94a3b8;
}
.out-label {
  font-size: 12px;
  font-weight: 600;
  color: #b91c1c;
}
.footer {
  margin-top: 20px;
  text-align: center;
  font-size: 13px;
}
.footer a,
.refund-note a {
  color: #4f46e5;
}
.list {
  background: #fff;
  border-radius: 14px;
  overflow: hidden;
}
.order-row + .order-row {
  border-top: 1px solid #f1f5f9;
}
.order-main {
  width: 100%;
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 14px 4px;
  border: none;
  background: transparent;
  text-align: left;
  font: inherit;
  cursor: pointer;
}
.row-main {
  min-width: 0;
}
.row-title {
  font-size: 15px;
  font-weight: 500;
  color: #0f172a;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.row-sub {
  margin-top: 2px;
  font-size: 13px;
  color: #94a3b8;
}
.row-side {
  flex-shrink: 0;
  text-align: right;
}
.row-amount {
  font-size: 15px;
  font-weight: 600;
  color: #0f172a;
}
.status {
  font-size: 12px;
}
.status.COMPLETED {
  color: #16a34a;
}
.status.CANCELED {
  color: #94a3b8;
}
.status.PAID,
.status.PROCESSING,
.status.SHIPPED {
  color: #4f46e5;
}
.refund-link {
  border: none;
  background: none;
  padding: 0 14px 12px;
  font: inherit;
  font-size: 13px;
  color: #4f46e5;
  cursor: pointer;
}
.refund-note {
  padding: 12px 14px;
  border-top: 1px solid #f1f5f9;
  font-size: 12px;
  color: #64748b;
}
.state-note {
  background: #fff;
  border-radius: 14px;
  padding: 28px 16px;
  text-align: center;
  font-size: 14px;
  color: #64748b;
}
.link-btn {
  border: none;
  background: none;
  color: #4f46e5;
  font: inherit;
  cursor: pointer;
  padding: 0 0 0 4px;
}
</style>
