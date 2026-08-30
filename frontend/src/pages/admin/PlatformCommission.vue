<template>
  <div class="commission-page">
    <div class="page-toolbar mb-4">
      <div class="toolbar-info">
        <h2 class="toolbar-title">
          <i class="ph-fill ph-percent"></i>
          {{ $t('commission.title') }}
        </h2>
      </div>
      <div class="toolbar-actions">
        <button type="button" class="btn-refresh" :disabled="loading" @click="fetchCommission">
          <i class="ph-bold ph-arrows-clockwise" :class="{ spin: loading }"></i>
          <span>{{ $t('commission.refresh') }}</span>
        </button>
      </div>
    </div>

    <div v-if="successMsg" class="alert-card success mb-4">
      <div class="alert-icon"><i class="ph-bold ph-check-circle"></i></div>
      <div class="alert-content">
        <div class="alert-title">{{ successMsg }}</div>
      </div>
    </div>

    <div v-if="errorMsg" class="alert-card danger mb-4">
      <div class="alert-icon"><i class="ph-bold ph-warning-circle"></i></div>
      <div class="alert-content">
        <div class="alert-title">{{ errorMsg }}</div>
      </div>
    </div>

    <!-- What the account holds and what is being charged -->
    <div class="metrics-grid mb-4">
      <div class="metric-card highlight">
        <div class="metric-label">{{ $t('commission.balance') }}</div>
        <div class="metric-value">{{ formatMoney(balance) }}</div>
      </div>

      <div class="metric-card">
        <div class="metric-label">{{ $t('commission.rate') }}</div>
        <div class="metric-value">{{ percent }} %</div>
        <div class="metric-note">
          {{ $t('commission.rateHint') }}
          <router-link to="/admin/settings">{{ $t('commission.goToSettings') }}</router-link>
        </div>
      </div>
    </div>

    <div class="explainer-card mb-4">
      <i class="ph-fill ph-info"></i>
      <span>{{ $t('commission.explainer') }}</span>
    </div>

    <!-- Payout -->
    <div class="payout-card">
      <div class="section-header">
        <div class="section-icon"><i class="ph-fill ph-bank"></i></div>
        <div class="section-title-group">
          <div class="section-title">{{ $t('commission.payoutTitle') }}</div>
          <div class="section-desc">{{ $t('commission.payoutHint') }}</div>
        </div>
      </div>

      <form class="payout-form" @submit.prevent="confirmPayout">
        <div class="input-group">
          <label class="input-label">{{ $t('commission.amount') }}</label>
          <div class="input-wrapper has-prefix">
            <span class="input-prefix">{{ currencySymbol }}</span>
            <input
              v-model.number="amount"
              type="number"
              step="0.01"
              min="0"
              :max="balance"
              required
            />
          </div>
        </div>

        <button
          type="button"
          class="btn btn-outline"
          :disabled="loading || balance <= 0"
          @click="amount = balance"
        >
          {{ $t('commission.withdrawAll') }}
        </button>

        <button
          type="submit"
          class="btn btn-primary"
          :disabled="submitting || !canWithdraw"
        >
          <i class="ph-bold ph-arrow-line-up-right"></i>
          {{ $t('commission.withdraw') }}
        </button>
      </form>
    </div>

    <va-modal
      v-model="showConfirm"
      :title="$t('commission.confirmTitle')"
      :message="$t('commission.confirmMessage', { amount: formatMoney(amount) })"
      :ok-text="$t('common.confirm')"
      :cancel-text="$t('common.cancel')"
      @ok="payout"
    />
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../../stores/auth-store'
import api from '../../services/api'

export default defineComponent({
  name: 'PlatformCommission',
  setup() {
    const { t } = useI18n()
    const authStore = useAuthStore()

    const balance = ref(0)
    const percent = ref(0)
    const amount = ref(0)
    const loading = ref(false)
    const submitting = ref(false)
    const showConfirm = ref(false)
    const successMsg = ref('')
    const errorMsg = ref('')

    const currencySymbol = computed(() => (authStore.currency === 'RUB' ? '₽' : '$'))

    // The server refuses an overdraw anyway; this only keeps the button from
    // offering an amount that is already known to be impossible.
    const canWithdraw = computed(() => amount.value > 0 && amount.value <= balance.value)

    const formatMoney = (value: number) => {
      const num = Number(value) || 0
      return `${currencySymbol.value} ${num.toLocaleString('ru-RU', {
        minimumFractionDigits: 0,
        maximumFractionDigits: 2,
      })}`
    }

    const apply = (data: any) => {
      balance.value = Number(data?.balance ?? 0)
      percent.value = Number(data?.percent ?? 0)
      if (amount.value > balance.value) {
        amount.value = balance.value
      }
    }

    const fetchCommission = async () => {
      loading.value = true
      errorMsg.value = ''
      try {
        const response = await api.get('/admin/finances/commission')
        apply(response.data)
      } catch (err: any) {
        errorMsg.value = err.response?.data || t('commission.loadFailed')
      } finally {
        loading.value = false
      }
    }

    const confirmPayout = () => {
      if (!canWithdraw.value) return
      showConfirm.value = true
    }

    const payout = async () => {
      if (!canWithdraw.value) return
      submitting.value = true
      successMsg.value = ''
      errorMsg.value = ''
      try {
        const response = await api.post('/admin/finances/commission/payout', { amount: amount.value })
        apply(response.data)
        amount.value = 0
        successMsg.value = t('commission.success')
      } catch (err: any) {
        errorMsg.value = err.response?.data || t('commission.payoutFailed')
      } finally {
        submitting.value = false
        showConfirm.value = false
      }
    }

    onMounted(fetchCommission)

    return {
      balance,
      percent,
      amount,
      loading,
      submitting,
      showConfirm,
      successMsg,
      errorMsg,
      currencySymbol,
      canWithdraw,
      formatMoney,
      fetchCommission,
      confirmPayout,
      payout,
    }
  },
})
</script>

<style scoped>
.commission-page {
  font-family: 'Outfit', sans-serif;
  color: #0f172a;
  max-width: 1000px;
}

.page-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

.toolbar-title {
  font-size: 22px;
  font-weight: 700;
  margin: 0;
  display: flex;
  align-items: center;
  gap: 10px;
}

.btn-refresh {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 10px 18px;
  border-radius: 12px;
  border: 1px solid rgba(0, 0, 0, 0.1);
  background: #ffffff;
  font-weight: 600;
  font-size: 14px;
  color: #0f172a;
  cursor: pointer;
}

.btn-refresh:disabled { opacity: 0.6; cursor: not-allowed; }

.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }

.metrics-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 16px;
}

.metric-card {
  background: #ffffff;
  border: 1px solid rgba(0, 0, 0, 0.05);
  border-radius: 20px;
  padding: 24px;
  box-shadow: 0 4px 24px rgba(15, 23, 42, 0.04);
}

.metric-card.highlight {
  border-color: rgba(16, 185, 129, 0.35);
  background: linear-gradient(180deg, #ecfdf5 0%, #ffffff 60%);
}

.metric-label {
  font-size: 13px;
  font-weight: 600;
  color: #64748b;
  margin-bottom: 8px;
}

.metric-value {
  font-family: 'JetBrains Mono', monospace;
  font-size: 28px;
  font-weight: 700;
}

.metric-note {
  margin-top: 8px;
  font-size: 12px;
  color: #64748b;
}

.metric-note a {
  color: #5c60f5;
  font-weight: 600;
  text-decoration: none;
  margin-left: 4px;
}

.explainer-card {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  background: #f8fafc;
  border: 1px solid rgba(0, 0, 0, 0.05);
  border-radius: 16px;
  padding: 16px 18px;
  font-size: 14px;
  line-height: 1.5;
  color: #475569;
}

.explainer-card i { color: #5c60f5; font-size: 18px; }

.payout-card {
  background: #ffffff;
  border: 1px solid rgba(0, 0, 0, 0.05);
  border-radius: 24px;
  padding: 28px;
  box-shadow: 0 4px 24px rgba(15, 23, 42, 0.04);
}

.section-header {
  display: flex;
  gap: 16px;
  align-items: flex-start;
  border-bottom: 1px solid rgba(0, 0, 0, 0.05);
  padding-bottom: 20px;
  margin-bottom: 20px;
}

.section-icon {
  width: 48px;
  height: 48px;
  border-radius: 14px;
  flex-shrink: 0;
  background: #eef2ff;
  color: #5c60f5;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 24px;
}

.section-title { font-size: 18px; font-weight: 700; }

.section-desc {
  font-size: 14px;
  color: #64748b;
  line-height: 1.4;
  max-width: 620px;
  margin-top: 4px;
}

.payout-form {
  display: flex;
  align-items: flex-end;
  gap: 12px;
  flex-wrap: wrap;
}

.input-group { display: flex; flex-direction: column; flex: 1 1 220px; }

.input-label {
  font-size: 13px;
  font-weight: 600;
  margin-bottom: 8px;
}

.input-wrapper { position: relative; display: flex; align-items: center; }

.input-wrapper input {
  width: 100%;
  padding: 12px 16px 12px 36px;
  border-radius: 12px;
  border: 1px solid rgba(0, 0, 0, 0.1);
  background: #f8fafc;
  font-family: 'JetBrains Mono', monospace;
  font-size: 15px;
  font-weight: 500;
  color: #0f172a;
  outline: none;
}

.input-wrapper input:focus {
  background: #ffffff;
  border-color: #5c60f5;
  box-shadow: 0 0 0 4px rgba(92, 96, 245, 0.1);
}

.input-prefix {
  position: absolute;
  left: 16px;
  color: #64748b;
  font-weight: 600;
  pointer-events: none;
}

.btn {
  padding: 12px 24px;
  border-radius: 12px;
  font-size: 15px;
  font-weight: 600;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  border: none;
}

.btn-outline {
  background: transparent;
  border: 1px solid rgba(0, 0, 0, 0.1);
  color: #0f172a;
}

.btn-primary {
  background: #5c60f5;
  color: #ffffff;
  box-shadow: 0 4px 12px rgba(92, 96, 245, 0.2);
}

.btn:disabled { opacity: 0.6; cursor: not-allowed; }

.alert-card {
  display: flex;
  gap: 12px;
  align-items: center;
  border-radius: 16px;
  padding: 14px 18px;
  font-size: 14px;
  font-weight: 500;
}

.alert-card.success { background: #ecfdf5; color: #065f46; border: 1px solid #a7f3d0; }
.alert-card.danger { background: #fef2f2; color: #991b1b; border: 1px solid #fecaca; }

.alert-icon { font-size: 18px; }

@media (max-width: 640px) {
  .payout-card { padding: 20px; border-radius: 16px; }
  .payout-form .btn { flex: 1 1 100%; justify-content: center; }
}
</style>
