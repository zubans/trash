<template>
  <div class="reconciliation">
    <div class="d-flex align-center justify-space-between mb-4 flex-wrap gap-2">
      <h1 class="va-h3 mb-0">{{ $t('reconciliation.title') }}</h1>
      <va-button preset="secondary" icon="refresh" :loading="loading" @click="fetchReport">
        {{ $t('reconciliation.refresh') }}
      </va-button>
    </div>

    <va-alert v-if="error" color="danger" class="mb-4">{{ error }}</va-alert>

    <!-- The headline: do the books close? Everything else is detail. -->
    <va-card v-if="report" class="mb-4" :stripe="true" :stripe-color="report.ok ? 'success' : 'danger'">
      <va-card-content>
        <div class="d-flex align-center gap-3 mb-2">
          <va-icon :name="report.ok ? 'check_circle' : 'error'" :color="report.ok ? 'success' : 'danger'" size="large" />
          <div>
            <div class="va-h5 mb-0">
              {{ report.ok ? $t('reconciliation.balanced') : $t('reconciliation.drifted') }}
            </div>
            <div class="text-secondary">{{ report.summary }}</div>
          </div>
        </div>
        <p class="text-secondary mb-0 mt-3">{{ $t('reconciliation.explainer') }}</p>
      </va-card-content>
    </va-card>

    <!-- The closing position: users on one side, platform accounts on the
         other. The two must cancel out; the difference is the whole point. -->
    <div v-if="books" class="totals mb-4">
      <va-card>
        <va-card-content>
          <div class="text-secondary mb-1">{{ $t('reconciliation.userTotal') }}</div>
          <div class="va-h4 mb-0">{{ formatMoney(books.user_total) }}</div>
        </va-card-content>
      </va-card>
      <va-card>
        <va-card-content>
          <div class="text-secondary mb-1">{{ $t('reconciliation.accountTotal') }}</div>
          <div class="va-h4 mb-0">{{ formatMoney(books.account_total) }}</div>
        </va-card-content>
      </va-card>
      <va-card :stripe="true" :stripe-color="report?.books_open ? 'danger' : 'success'">
        <va-card-content>
          <div class="text-secondary mb-1">{{ $t('reconciliation.difference') }}</div>
          <div class="va-h4 mb-0" :class="report?.books_open ? 'text-danger' : ''">
            {{ formatMoney(books.difference) }}
          </div>
        </va-card-content>
      </va-card>
    </div>

    <!-- Per-account breakdown. -->
    <va-card v-if="books" class="mb-4">
      <va-card-title>{{ $t('reconciliation.accounts') }}</va-card-title>
      <va-card-content>
        <va-data-table :items="books.accounts || []" :columns="accountColumns">
          <template #cell(code)="{ value }">
            <va-badge :text="value" color="secondary" />
          </template>
          <template #cell(balance)="{ value }">
            <strong>{{ formatMoney(value) }}</strong>
          </template>
        </va-data-table>
      </va-card-content>
    </va-card>

    <!-- Escrow against the orders that actually claim it. -->
    <va-card v-if="books" class="mb-4" :stripe="true" :stripe-color="report?.escrow_mismatch ? 'danger' : undefined">
      <va-card-title>{{ $t('reconciliation.escrow') }}</va-card-title>
      <va-card-content>
        <div class="totals">
          <div>
            <div class="text-secondary mb-1">{{ $t('reconciliation.escrowHeld') }}</div>
            <div class="va-h5 mb-0">{{ formatMoney(books.escrow_held) }}</div>
          </div>
          <div>
            <div class="text-secondary mb-1">{{ $t('reconciliation.liveOrderSum') }}</div>
            <div class="va-h5 mb-0">{{ formatMoney(books.live_order_sum) }}</div>
          </div>
          <div>
            <div class="text-secondary mb-1">{{ $t('reconciliation.escrowDrift') }}</div>
            <div class="va-h5 mb-0" :class="report?.escrow_mismatch ? 'text-danger' : ''">
              {{ formatMoney(books.escrow_drift) }}
            </div>
          </div>
        </div>
      </va-card-content>
    </va-card>

    <!-- Findings. Shown only when there are any: an empty table reads as
         "nothing checked" rather than "nothing wrong". -->
    <va-card v-if="report && report.discrepancies && report.discrepancies.length" class="mb-4" stripe stripe-color="danger">
      <va-card-title>{{ $t('reconciliation.discrepancies') }}</va-card-title>
      <va-card-content>
        <va-data-table :items="report.discrepancies" :columns="discrepancyColumns">
          <template #cell(balance)="{ value }"><strong>{{ formatMoney(value) }}</strong></template>
          <template #cell(ledger)="{ value }"><strong>{{ formatMoney(value) }}</strong></template>
          <template #cell(difference)="{ value }">
            <strong class="text-danger">{{ formatMoney(value) }}</strong>
          </template>
        </va-data-table>
      </va-card-content>
    </va-card>

    <va-card v-if="report && report.hold_anomalies && report.hold_anomalies.length" class="mb-4" stripe stripe-color="warning">
      <va-card-title>
        {{ $t('reconciliation.holdAnomalies') }} ({{ report.hold_anomalies.length }})
      </va-card-title>
      <va-card-content>
        <va-data-table :items="report.hold_anomalies" :columns="anomalyColumns" :per-page="10">
          <template #cell(order_id)="{ value }">
            <span class="text-secondary text-truncate d-inline-block" style="max-width: 220px;">{{ value }}</span>
          </template>
          <template #cell(hold_amount)="{ value }"><strong>{{ formatMoney(value) }}</strong></template>
        </va-data-table>
      </va-card-content>
    </va-card>

    <va-alert v-if="report && report.unknown_transaction_types && report.unknown_transaction_types.length"
              color="danger" class="mb-4">
      {{ $t('reconciliation.unknownTypes') }}: {{ report.unknown_transaction_types.join(', ') }}
    </va-alert>

    <va-inner-loading v-if="loading && !report" :loading="true" style="min-height: 200px;" />
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../../stores/auth-store'
import api, { formatApiError } from '../../services/api'

export default defineComponent({
  name: 'FinancialReconciliation',
  setup() {
    const { t } = useI18n()
    const authStore = useAuthStore()
    const report = ref<any>(null)
    const loading = ref(false)
    const error = ref('')

    const books = computed(() => report.value?.books ?? null)
    const currencySymbol = computed(() => (authStore.currency === 'RUB' ? '₽' : '$'))

    // Amounts arrive as decimal strings so they never pass through a float on
    // the way here; they must not start doing so on the way to the screen.
    const formatMoney = (value: string | number | null | undefined) => {
      if (value === null || value === undefined || value === '') return '—'
      return `${currencySymbol.value}${value}`
    }

    const accountColumns = [
      { key: 'code', label: t('reconciliation.accountCode') },
      { key: 'name', label: t('reconciliation.accountName') },
      { key: 'balance', label: t('reconciliation.accountBalance') },
    ]

    const discrepancyColumns = [
      { key: 'phone', label: t('reconciliation.userPhone') },
      { key: 'balance', label: t('reconciliation.storedBalance') },
      { key: 'ledger', label: t('reconciliation.ledgerSum') },
      { key: 'difference', label: t('reconciliation.difference') },
    ]

    const anomalyColumns = [
      { key: 'order_id', label: t('reconciliation.orderId') },
      { key: 'status', label: t('reconciliation.orderStatus') },
      { key: 'hold_amount', label: t('reconciliation.holdAmount') },
      { key: 'reason', label: t('reconciliation.reason') },
    ]

    const fetchReport = async () => {
      loading.value = true
      error.value = ''
      try {
        const response = await api.get('/admin/finances/reconciliation')
        report.value = response.data
      } catch (err: any) {
        error.value = formatApiError(err, t('reconciliation.loadFailed'))
      } finally {
        loading.value = false
      }
    }

    onMounted(fetchReport)

    return {
      report,
      books,
      loading,
      error,
      accountColumns,
      discrepancyColumns,
      anomalyColumns,
      formatMoney,
      fetchReport,
    }
  },
})
</script>

<style scoped>
.totals {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 1rem;
}

.gap-2 {
  gap: 0.5rem;
}

.gap-3 {
  gap: 0.75rem;
}
</style>
