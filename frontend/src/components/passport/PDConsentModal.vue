<template>
  <div v-if="visible" class="consent-backdrop">
    <div class="consent-modal" role="dialog" aria-modal="true">
      <h2>{{ $t('passport.consent.title') }}</h2>
      <p class="lead">{{ $t('passport.consent.lead') }}</p>
      <div class="consent-scroll">
        <OfferText :text="consent" />
      </div>
      <p v-if="error" class="error">{{ error }}</p>
      <div class="actions">
        <button type="button" class="btn-secondary" :disabled="busy" @click="later">{{ $t('passport.consent.later') }}</button>
        <button type="button" class="btn-primary" :disabled="busy" @click="accept">{{ $t('passport.consent.accept') }}</button>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../../stores/auth-store'
import { acceptPDConsent } from '../../api/passport'
import OfferText from '../shop/OfferText.vue'
import consent from '../../content/personal-data-consent.md?raw'

// Окно согласия на обработку персональных данных — для тех, кто
// зарегистрировался до галочки, и после смены редакции
// (implementation_plan_delivery_passport.md §4). «Позже» закрывает окно до
// конца сеанса: без согласия работает всё, кроме паспорта.
export default defineComponent({
  name: 'PDConsentModal',
  components: { OfferText },
  setup() {
    const { t } = useI18n()
    const authStore = useAuthStore()
    const dismissed = ref(false)
    const busy = ref(false)
    const error = ref('')

    const visible = computed(() => !!authStore.user?.pd_consent_required && !dismissed.value)

    const accept = async () => {
      busy.value = true
      error.value = ''
      try {
        await acceptPDConsent()
        await authStore.fetchMe()
      } catch {
        error.value = t('passport.consent.failed')
      } finally {
        busy.value = false
      }
    }
    const later = () => {
      dismissed.value = true
    }
    return { visible, consent, busy, error, accept, later }
  },
})
</script>

<style scoped>
.consent-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(15, 23, 42, 0.55);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
  z-index: 3000;
}
.consent-modal {
  background: #fff;
  border-radius: 18px;
  padding: 20px;
  max-width: 640px;
  width: 100%;
  max-height: 90vh;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
h2 {
  margin: 0;
  font-size: 18px;
}
.lead {
  margin: 0;
  color: #475569;
  font-size: 14px;
}
.consent-scroll {
  overflow: auto;
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  padding: 12px;
  flex: 1;
}
.error {
  color: #dc2626;
  margin: 0;
  font-size: 13px;
}
.actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  flex-wrap: wrap;
}
.btn-primary,
.btn-secondary {
  border: none;
  border-radius: 12px;
  padding: 10px 16px;
  font-weight: 600;
  cursor: pointer;
}
.btn-primary {
  background: #10b981;
  color: #fff;
}
.btn-secondary {
  background: #f1f5f9;
  color: #334155;
}
</style>
