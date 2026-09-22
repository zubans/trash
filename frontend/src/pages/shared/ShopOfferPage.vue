<template>
  <div class="offer-page">
    <div class="offer-container">
      <div class="top-nav">
        <button type="button" class="btn-back" @click="goBack">
          <i class="ph-bold ph-arrow-left"></i>
          {{ $t('shop.title') }}
        </button>
      </div>
      <div class="panel">
        <h1 class="title">{{ $t('shop.offer.title') }}</h1>
        <div class="edition">{{ $t('shop.offer.edition', { version: store.data.value.offer_version }) }}</div>
        <OfferText />
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, nextTick, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '../../stores/auth-store'
import OfferText from '../../components/shop/OfferText.vue'
import { useStorefront } from '../../composables/useShop'

// Оферта магазина (implementation_plan_shop.md §4.5). Ссылка «п. 8 оферты»
// из «Моих покупок» ведёт сюда с якорем раздела.
export default defineComponent({
  name: 'ShopOfferPage',
  components: { OfferText },
  setup() {
    const route = useRoute()
    const router = useRouter()
    const authStore = useAuthStore()
    const store = useStorefront()

    const goBack = () => {
      if (window.history.length > 1) router.back()
      else router.push(authStore.activeRole === 'EXECUTOR' ? '/executor/shop' : '/customer/shop')
    }

    onMounted(async () => {
      store.load()
      if (route.hash) {
        await nextTick()
        document.querySelector(route.hash)?.scrollIntoView()
      }
    })

    return { store, goBack }
  },
})
</script>

<style scoped>
.offer-page {
  min-height: 100vh;
  background: #f6f7fb;
  padding: 16px;
  padding-bottom: calc(24px + env(safe-area-inset-bottom));
}
.offer-container {
  max-width: 720px;
  margin: 0 auto;
}
.top-nav {
  margin-bottom: 12px;
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
.panel {
  background: #fff;
  border-radius: 16px;
  padding: 20px 18px;
}
.title {
  margin: 0;
  font-size: 20px;
  font-weight: 700;
  color: #0f172a;
}
.edition {
  margin: 4px 0 8px;
  font-size: 13px;
  color: #64748b;
}
</style>
