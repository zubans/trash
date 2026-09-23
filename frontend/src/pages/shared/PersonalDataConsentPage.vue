<template>
  <div class="consent-page">
    <div class="consent-container">
      <button type="button" class="btn-back" @click="goBack">
        <i class="ph-bold ph-arrow-left"></i>
        {{ $t('common.back') }}
      </button>
      <div class="panel">
        <h1 class="title">{{ $t('passport.consent.title') }}</h1>
        <OfferText :text="consent" />
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent } from 'vue'
import { useRouter } from 'vue-router'
import OfferText from '../../components/shop/OfferText.vue'
import consent from '../../content/personal-data-consent.md?raw'

// Текст согласия на обработку персональных данных. Страница открыта без входа:
// на неё ведёт ссылка из формы регистрации.
export default defineComponent({
  name: 'PersonalDataConsentPage',
  components: { OfferText },
  setup() {
    const router = useRouter()
    const goBack = () => (window.history.length > 1 ? router.back() : router.push('/login'))
    return { consent, goBack }
  },
})
</script>

<style scoped>
.consent-page {
  min-height: 100vh;
  background: #f1f5f9;
  padding: 16px;
}
.consent-container {
  max-width: 760px;
  margin: 0 auto;
}
.btn-back {
  border: none;
  background: none;
  color: #475569;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 0;
  cursor: pointer;
}
.panel {
  background: #fff;
  border-radius: 16px;
  padding: 20px;
}
.title {
  font-size: 20px;
  margin: 0 0 12px;
}
</style>
