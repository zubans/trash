<template>
  <div class="product-page">
    <div class="product-container">
      <div class="top-nav">
        <button type="button" class="btn-back" @click="goBack">
          <i class="ph-bold ph-arrow-left"></i>
          {{ $t('shop.title') }}
        </button>
      </div>

      <SkeletonList v-if="loading" :rows="3" />
      <div v-else-if="!card" class="state-note">{{ notFound }}</div>

      <template v-else>
        <div v-if="product.images.length" class="gallery">
          <img
            v-for="(img, i) in product.images"
            :key="img"
            :src="imageUrl(img)"
            :alt="`${title} ${i + 1}`"
            class="gallery-image"
          />
        </div>

        <div class="panel">
          <div class="kind">{{ $t('shop.kinds.' + product.kind) }}</div>
          <h1 class="title">{{ title }}</h1>
          <div class="price">
            <span>{{ money(product.price) }}</span>
            <s v-if="product.compare_at_price" class="old-price">{{ money(product.compare_at_price) }}</s>
          </div>
          <p v-if="description" class="description">{{ description }}</p>
          <div v-if="product.kind === 'PERK'" class="perk-name">
            {{ $t('shop.perk.days', { days: product.perk_days }) }}
          </div>
          <div v-if="product.requires_verified" class="note">{{ $t('shop.card.verified') }}</div>
          <div v-if="product.per_user_limit" class="note">
            {{ $t('shop.card.perUser', { count: product.per_user_limit, bought: card.purchased }) }}
          </div>
          <div v-if="product.kind === 'PHYSICAL'" class="note">
            {{ product.fulfillment_methods.map((m) => $t('shop.methods.' + m)).join(' · ') }}
          </div>
        </div>

        <!-- Честная витрина (§3.6): ставку считает сервер по формуле
             подтверждения заказа, а не текст на карточке. -->
        <PerkQuoteBlock v-if="quote" class="panel" :quote="quote" :product="product" :currency-symbol="currencySymbol" />

        <div v-if="blockedReason" class="alert">{{ blockedReason }}</div>

        <button type="button" class="btn-buy" :disabled="!!blockedReason" @click="showCheckout = true">
          <template v-if="quote && quote.queued">{{ $t('shop.card.buyFrom', { date: formatPerkDate(quote.starts_at) }) }}</template>
          <template v-else>{{ $t('shop.card.buy', { price: money(product.price) }) }}</template>
        </button>

        <div class="footer">
          <router-link to="/shop/offer">{{ $t('shop.offerLink') }}</router-link>
        </div>
      </template>
    </div>

    <CheckoutModal
      :show="showCheckout"
      :product="card?.product || null"
      :quote="quote"
      :offer-version="offerVersion"
      :currency-symbol="currencySymbol"
      @close="showCheckout = false"
      @purchased="onPurchased"
      @price-changed="onPriceChanged"
      @offer-changed="(v) => (offerVersion = v)"
    />
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, onMounted, ref, type PropType } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../../stores/auth-store'
import { resolveFileUrl } from '../../services/api'
import SkeletonList from '../../components/SkeletonList.vue'
import CheckoutModal from '../../components/shop/CheckoutModal.vue'
import PerkQuoteBlock from '../../components/shop/PerkQuoteBlock.vue'
import { forgetShopPurchaseCaches } from '../../composables/useShop'
import { getProductCard, localized as pick, shopErrorText, type ProductCard, type ShopOrder } from '../../api/shop'
import { formatPerkDate } from '../../utils/perk'

type Role = 'EXECUTOR' | 'CUSTOMER'
const HOME: Record<Role, string> = { EXECUTOR: '/executor', CUSTOMER: '/customer' }

export default defineComponent({
  name: 'ShopProductPage',
  components: { SkeletonList, CheckoutModal, PerkQuoteBlock },
  props: {
    role: { type: String as PropType<Role>, required: true },
  },
  setup(props) {
    const route = useRoute()
    const router = useRouter()
    const { t, locale } = useI18n()
    const authStore = useAuthStore()
    const shopHome = `${HOME[props.role] || HOME.CUSTOMER}/shop`

    const card = ref<ProductCard | null>(null)
    const loading = ref(true)
    const notFound = ref('')
    const showCheckout = ref(false)
    const offerVersion = ref(1)

    const product = computed(() => card.value!.product)
    const quote = computed(() => card.value?.perk_quote)
    const currencySymbol = computed(() => (authStore.currency === 'RUB' ? '₽' : '$'))
    const money = (value: number) =>
      `${Number(value || 0).toLocaleString('ru-RU', { maximumFractionDigits: 2 })} ${currencySymbol.value}`
    const title = computed(() => pick(card.value?.product.title, locale.value))
    const description = computed(() => pick(card.value?.product.description, locale.value))
    const imageUrl = (path: string) => resolveFileUrl(path)

    // Почему купить нельзя — показывается до нажатия, а не отказом после.
    const blockedReason = computed(() => {
      const c = card.value
      if (!c) return ''
      if (!c.product.in_stock) return t('shop.outOfStock')
      if (c.perk_quote?.useless) return t('shop.perk.useless')
      if (c.perk_quote && c.perk_quote.queue_length >= c.perk_quote.max_queued) {
        return t('shop.perk.queueFull', { max: c.perk_quote.max_queued })
      }
      if (c.product.per_user_limit && c.purchased >= c.product.per_user_limit) return t('shop.errors.limit_reached')
      return ''
    })

    const load = async () => {
      loading.value = true
      try {
        card.value = await getProductCard(String(route.params.id))
        offerVersion.value = card.value.offer_version
      } catch (err) {
        card.value = null
        notFound.value = shopErrorText(err, t, t('shop.card.notFound'))
      } finally {
        loading.value = false
      }
    }

    // Цена изменилась между показом и оплатой: карточка перерисовывается с
    // новой ценой, окно оформления остаётся открытым с объяснением.
    const onPriceChanged = (price: number) => {
      if (card.value && Number.isFinite(price)) card.value.product.price = price
    }

    const onPurchased = (order: ShopOrder) => {
      showCheckout.value = false
      forgetShopPurchaseCaches()
      authStore.fetchMe()
      router.push({ path: shopHome, query: { tab: 'orders', order: order.id } })
    }

    const goBack = () => router.push(shopHome)

    onMounted(load)

    return {
      card, loading, notFound, showCheckout, offerVersion, product, quote, currencySymbol, money,
      title, description, imageUrl, blockedReason, onPriceChanged, onPurchased, goBack,
      formatPerkDate,
    }
  },
})
</script>

<style scoped>
.product-page {
  min-height: 100vh;
  background: #f6f7fb;
  padding: 16px;
  padding-bottom: calc(24px + env(safe-area-inset-bottom));
}
.product-container {
  max-width: 640px;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: 12px;
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
.gallery {
  display: flex;
  gap: 8px;
  overflow-x: auto;
  scroll-snap-type: x mandatory;
  border-radius: 16px;
}
.gallery-image {
  flex: 0 0 100%;
  aspect-ratio: 4 / 3;
  object-fit: cover;
  border-radius: 16px;
  scroll-snap-align: start;
  background: #eef2ff;
}
.panel {
  background: #fff;
  border-radius: 16px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.kind {
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: #6366f1;
}
.title {
  margin: 0;
  font-size: 20px;
  font-weight: 700;
  color: #0f172a;
}
.price {
  display: flex;
  align-items: baseline;
  gap: 8px;
  font-size: 20px;
  font-weight: 700;
}
.old-price {
  font-size: 14px;
  font-weight: 500;
  color: #94a3b8;
}
.description {
  margin: 0;
  font-size: 14px;
  line-height: 1.5;
  color: #334155;
  white-space: pre-line;
}
.perk-name {
  font-size: 14px;
  font-weight: 600;
  color: #0f172a;
}
.note {
  font-size: 13px;
  color: #64748b;
}
.alert {
  background: #fff7ed;
  color: #9a3412;
  border-radius: 12px;
  padding: 10px 12px;
  font-size: 14px;
}
.btn-buy {
  border: none;
  border-radius: 14px;
  padding: 15px 16px;
  background: #4f46e5;
  color: #fff;
  font: inherit;
  font-size: 16px;
  font-weight: 600;
  cursor: pointer;
}
.btn-buy:disabled {
  background: #c7d2fe;
  cursor: not-allowed;
}
.footer {
  text-align: center;
  font-size: 13px;
}
.footer a {
  color: #4f46e5;
}
.state-note {
  background: #fff;
  border-radius: 14px;
  padding: 28px 16px;
  text-align: center;
  font-size: 14px;
  color: #64748b;
}
</style>
