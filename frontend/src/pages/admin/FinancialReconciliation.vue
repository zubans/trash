<template>
  <div class="reconciliation-page">
    <!-- Top Action Toolbar -->
    <div class="page-toolbar mb-4">
      <div class="toolbar-info">
        <h2 class="toolbar-title">{{ $t('reconciliation.title') }}</h2>
        <span v-if="lastUpdatedText" class="last-updated">
          <i class="ph ph-clock"></i> {{ lastUpdatedText }}
        </span>
      </div>
      <div class="toolbar-actions">
        <button
          type="button"
          class="btn-refresh"
          :disabled="loading"
          @click="fetchReport"
        >
          <i class="ph-bold ph-arrows-clockwise" :class="{ 'spin': loading }"></i>
          <span>{{ $t('reconciliation.run') }}</span>
        </button>
      </div>
    </div>

    <!-- API Error Alert -->
    <div v-if="error" class="alert-card danger mb-4">
      <div class="alert-icon"><i class="ph-bold ph-warning-circle"></i></div>
      <div class="alert-content">
        <div class="alert-title">{{ $t('reconciliation.loadFailed') }}</div>
        <div class="alert-text">{{ error }}</div>
      </div>
    </div>

    <!-- Main Alert Card (Status & Detailed Technical Log) -->
    <div
      v-if="report"
      class="alert-card mb-4"
      :class="isReportBalanced ? 'success' : 'danger'"
    >
      <div class="alert-icon">
        <i :class="isReportBalanced ? 'ph-bold ph-check-circle' : 'ph-bold ph-warning-circle'"></i>
      </div>
      <div class="alert-content">
        <div class="alert-title">
          {{ isReportBalanced ? $t('reconciliation.balanced') : $t('reconciliation.drifted') }}
        </div>
        
        <!-- Technical Log in JetBrains Mono -->
        <div
          v-if="report.summary"
          class="alert-tech-log"
          :class="{ 'success-log': isReportBalanced }"
        >
          {{ report.summary }}
        </div>
        
        <!-- Human Explainer Text -->
        <div class="alert-text">
          {{ $t('reconciliation.explainer') }}
          <div class="alert-note">{{ $t('reconciliation.runNote') }}</div>
        </div>
      </div>
    </div>

    <!-- Bento Grid Metrics -->
    <div v-if="books" class="metrics-grid mb-4">
      <div class="metric-card">
        <div class="metric-label">{{ $t('reconciliation.userTotal') }}</div>
        <div class="metric-value">{{ formatMoney(books.user_total) }}</div>
      </div>

      <div class="metric-card">
        <div class="metric-label">{{ $t('reconciliation.accountTotal') }}</div>
        <div class="metric-value">{{ formatMoney(books.account_total) }}</div>
      </div>

      <div
        class="metric-card"
        :class="isBooksOpen ? 'highlight-danger' : 'highlight-success'"
      >
        <div class="metric-label">{{ $t('reconciliation.difference') }}</div>
        <div class="metric-value">{{ formatMoney(books.difference) }}</div>
      </div>
    </div>

    <!-- Section 1: System Accounts -->
    <div v-if="books && books.accounts && books.accounts.length" class="section-wrap mb-4">
      <div class="section-header">
        <i class="ph-fill ph-hard-drives"></i>
        <span>{{ $t('reconciliation.accounts') }}</span>
      </div>

      <div class="table-responsive">
        <div class="grid-table accounts-table">
          <div class="grid-row grid-header">
            <div>{{ $t('reconciliation.accountCode') }}</div>
            <div>{{ $t('reconciliation.accountName') }}</div>
            <div class="text-end">{{ $t('reconciliation.accountBalance') }}</div>
          </div>

          <div
            v-for="acc in books.accounts"
            :key="acc.code"
            class="grid-row"
          >
            <div>
              <span
                class="pill-tag"
                :class="isAccountZero(acc.balance) ? 'neutral' : 'dark'"
              >
                {{ acc.code }}
              </span>
            </div>
            <div class="cell-text">{{ acc.name }}</div>
            <div
              class="cell-mono text-end"
              :class="{ 'text-muted-balance': isAccountZero(acc.balance) }"
            >
              {{ formatMoney(acc.balance) }}
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Section 2: Escrow & Active Orders -->
    <div v-if="books" class="section-wrap mb-4">
      <div class="section-header">
        <i class="ph-fill ph-lock-key"></i>
        <span>{{ $t('reconciliation.escrow') }}</span>
      </div>

      <div class="table-responsive">
        <div class="grid-table escrow-table">
          <div class="grid-row grid-header">
            <div>{{ $t('reconciliation.escrowHeld') }}</div>
            <div>{{ $t('reconciliation.liveOrderSum') }}</div>
            <div>{{ $t('reconciliation.escrowDrift') }}</div>
          </div>

          <div class="grid-row">
            <div class="cell-mono">{{ formatMoney(books.escrow_held) }}</div>
            <div class="cell-mono">{{ formatMoney(books.live_order_sum) }}</div>
            <div
              class="cell-mono"
              :class="isEscrowMismatch ? 'text-danger-bold' : 'text-success-bold'"
            >
              {{ formatMoney(books.escrow_drift) }}
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Section 3: Discrepancies (if any) -->
    <div
      v-if="report && report.discrepancies && report.discrepancies.length"
      class="section-wrap section-wrap-danger mb-4"
    >
      <div class="section-header text-danger">
        <i class="ph-fill ph-user-focus"></i>
        <span>{{ $t('reconciliation.discrepancies') }}</span>
        <span class="section-count text-danger">({{ report.discrepancies.length }})</span>
      </div>

      <div class="table-responsive">
        <div class="grid-table discrepancies-table">
          <div class="grid-row grid-header">
            <div>{{ $t('reconciliation.userPhone') }}</div>
            <div>{{ $t('reconciliation.storedBalance') }}</div>
            <div>{{ $t('reconciliation.ledgerSum') }}</div>
            <div>{{ $t('reconciliation.difference') }}</div>
          </div>

          <div
            v-for="d in report.discrepancies"
            :key="d.user_id"
            class="grid-row"
          >
            <div class="cell-mono cell-clickable" @click="copyText(d.phone || d.user_id, 'Номер')">
              {{ d.phone || d.user_id }}
            </div>
            <div class="cell-mono">{{ formatMoney(d.balance) }}</div>
            <div class="cell-mono">{{ formatMoney(d.ledger) }}</div>
            <div class="cell-mono text-danger-bold">{{ formatMoney(d.difference) }}</div>
          </div>
        </div>
      </div>
    </div>

    <!-- Section 4: Hold Anomalies (if any) -->
    <div
      v-if="report && report.hold_anomalies && report.hold_anomalies.length"
      class="section-wrap section-wrap-warning mb-4"
    >
      <div class="section-header">
        <i class="ph-fill ph-warning"></i>
        <span>{{ $t('reconciliation.holdAnomalies') }}</span>
        <span class="section-count">({{ report.hold_anomalies.length }})</span>
      </div>

      <div class="table-responsive">
        <div class="grid-table orders-table">
          <div class="grid-row grid-header">
            <div>{{ $t('reconciliation.orderId') }}</div>
            <div>{{ $t('reconciliation.orderStatus') }}</div>
            <div>{{ $t('reconciliation.holdAmount') }}</div>
            <div>{{ $t('reconciliation.reason') }}</div>
          </div>

          <div
            v-for="anomaly in paginatedAnomalies"
            :key="anomaly.order_id"
            class="grid-row"
          >
            <div>
              <span
                class="cell-id"
                :title="anomaly.order_id"
                @click="copyText(anomaly.order_id, 'ID заказа')"
              >
                {{ truncateId(anomaly.order_id) }}
                <i class="ph ph-copy copy-icon"></i>
              </span>
            </div>
            <div>
              <span class="pill-status" :class="getStatusClass(anomaly.status)">
                {{ anomaly.status }}
              </span>
            </div>
            <div class="cell-mono">{{ formatMoney(anomaly.hold_amount) }}</div>
            <div class="cell-text text-danger-reason">{{ anomaly.reason }}</div>
          </div>
        </div>
      </div>

      <!-- Pagination for anomalies -->
      <div v-if="report.hold_anomalies.length > perPage" class="pagination-bar mt-2">
        <button
          type="button"
          class="btn-page"
          :disabled="currentPage === 1"
          @click="currentPage--"
        >
          <i class="ph ph-caret-left"></i>
        </button>
        <span class="page-info">{{ currentPage }} / {{ totalPages }}</span>
        <button
          type="button"
          class="btn-page"
          :disabled="currentPage === totalPages"
          @click="currentPage++"
        >
          <i class="ph ph-caret-right"></i>
        </button>
      </div>
    </div>

    <!-- Section 5: Unknown Transaction Types Alert -->
    <div
      v-if="report && report.unknown_transaction_types && report.unknown_transaction_types.length"
      class="alert-card danger mb-4"
    >
      <div class="alert-icon"><i class="ph-bold ph-warning"></i></div>
      <div class="alert-content">
        <div class="alert-title">{{ $t('reconciliation.unknownTypes') }}</div>
        <div class="alert-tech-log">
          {{ report.unknown_transaction_types.join(', ') }}
        </div>
      </div>
    </div>

    <!-- Loading Skeleton -->
    <div v-if="loading && !report" class="skeleton-container py-5">
      <div class="skeleton-bar title-bar mb-4"></div>
      <div class="skeleton-grid mb-4">
        <div class="skeleton-card"></div>
        <div class="skeleton-card"></div>
        <div class="skeleton-card"></div>
      </div>
      <div class="skeleton-box mb-4"></div>
    </div>

    <!-- Copy Toast Notification -->
    <transition name="toast-fade">
      <div v-if="toastMessage" class="toast-notification">
        <i class="ph-fill ph-check-circle"></i>
        <span>{{ toastMessage }}</span>
      </div>
    </transition>
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
    const lastUpdated = ref<Date | null>(null)
    const toastMessage = ref('')
    let toastTimer: any = null

    const currentPage = ref(1)
    const perPage = ref(10)

    const books = computed(() => report.value?.books ?? null)
    const currencySymbol = computed(() => (authStore.currency === 'RUB' ? '₽' : '$'))

    const isReportBalanced = computed(() => {
      if (!report.value) return true
      if (report.value.ok !== undefined) return report.value.ok
      return (
        !report.value.books_open &&
        !report.value.escrow_mismatch &&
        (!report.value.discrepancies || report.value.discrepancies.length === 0) &&
        (!report.value.hold_anomalies || report.value.hold_anomalies.length === 0) &&
        (!report.value.unknown_transaction_types || report.value.unknown_transaction_types.length === 0)
      )
    })

    const isBooksOpen = computed(() => {
      if (report.value?.books_open) return true
      if (books.value?.difference !== undefined && books.value?.difference !== null) {
        return Math.abs(parseFloat(String(books.value.difference))) > 0.001
      }
      return false
    })

    const isEscrowMismatch = computed(() => {
      if (report.value?.escrow_mismatch) return true
      if (books.value?.escrow_drift !== undefined && books.value?.escrow_drift !== null) {
        return Math.abs(parseFloat(String(books.value.escrow_drift))) > 0.001
      }
      return false
    })

    const lastUpdatedText = computed(() => {
      if (!lastUpdated.value) return ''
      return lastUpdated.value.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
    })

    const totalPages = computed(() => {
      if (!report.value?.hold_anomalies) return 1
      return Math.ceil(report.value.hold_anomalies.length / perPage.value) || 1
    })

    const paginatedAnomalies = computed(() => {
      if (!report.value?.hold_anomalies) return []
      const start = (currentPage.value - 1) * perPage.value
      return report.value.hold_anomalies.slice(start, start + perPage.value)
    })

    const formatMoney = (value: string | number | null | undefined) => {
      if (value === null || value === undefined || value === '') return '—'
      const num = typeof value === 'number' ? value : parseFloat(value)
      if (isNaN(num)) return `${currencySymbol.value} ${value}`
      const formatted = Math.abs(num).toLocaleString('ru-RU', {
        minimumFractionDigits: 0,
        maximumFractionDigits: 2,
      })
      const sign = num < 0 ? '-' : ''
      return `${currencySymbol.value} ${sign}${formatted}`
    }

    const isAccountZero = (val: string | number | null | undefined) => {
      if (val === null || val === undefined || val === '') return true
      const num = typeof val === 'number' ? val : parseFloat(val)
      return isNaN(num) || Math.abs(num) < 0.001
    }

    const truncateId = (id: string) => {
      if (!id) return ''
      if (id.length <= 22) return id
      return `${id.slice(0, 8)}...${id.slice(-6)}`
    }

    const getStatusClass = (status: string) => {
      const upper = (status || '').toUpperCase()
      if (['COMPLETED', 'EXECUTED'].includes(upper)) return 'success'
      if (['CANCELED', 'CANCELLED', 'REJECTED'].includes(upper)) return 'danger'
      return 'warning'
    }

    const copyText = (text: string, label: string) => {
      if (!text) return
      navigator.clipboard.writeText(text).then(() => {
        showToast(`${label} скопирован`)
      }).catch(() => {})
    }

    const showToast = (msg: string) => {
      toastMessage.value = msg
      if (toastTimer) clearTimeout(toastTimer)
      toastTimer = setTimeout(() => {
        toastMessage.value = ''
      }, 2500)
    }

    const fetchReport = async () => {
      loading.value = true
      error.value = ''
      try {
        const response = await api.get('/admin/finances/reconciliation')
        report.value = response.data
        lastUpdated.value = new Date()
        currentPage.value = 1
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
      lastUpdatedText,
      toastMessage,
      currentPage,
      perPage,
      totalPages,
      paginatedAnomalies,
      isReportBalanced,
      isBooksOpen,
      isEscrowMismatch,
      formatMoney,
      isAccountZero,
      truncateId,
      getStatusClass,
      copyText,
      fetchReport,
    }
  },
})
</script>

<style scoped>
.reconciliation-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
  font-family: 'Outfit', sans-serif;
  color: #0f172a;
}

/* Toolbar */
.page-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
}

.toolbar-info {
  display: flex;
  align-items: baseline;
  gap: 12px;
}

.toolbar-title {
  font-size: 22px;
  font-weight: 700;
  color: #0f172a;
  margin: 0;
  letter-spacing: -0.5px;
}

.last-updated {
  font-size: 13px;
  color: #64748b;
  display: flex;
  align-items: center;
  gap: 4px;
}

.btn-refresh {
  background: #ffffff;
  border: 1px solid rgba(0, 0, 0, 0.08);
  border-radius: 12px;
  height: 40px;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 0 16px;
  font-size: 14px;
  font-weight: 600;
  color: #5c60f5;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.02);
  cursor: pointer;
  transition: all 0.2s ease;
}

.btn-refresh:hover:not(:disabled) {
  background: #f8fafc;
  border-color: rgba(92, 96, 245, 0.3);
  transform: translateY(-1px);
}

.btn-refresh:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.spin {
  animation: spinIcon 0.8s linear infinite;
}

@keyframes spinIcon {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

/* Alert Card */
.alert-card {
  border-radius: 20px;
  padding: 24px;
  display: flex;
  gap: 16px;
  align-items: flex-start;
  transition: all 0.2s ease;
}

.alert-card.danger {
  background: #fef2f2;
  border: 1px solid #fca5a5;
  box-shadow: 0 8px 24px -8px rgba(239, 68, 68, 0.15);
}

.alert-card.success {
  background: #ecfdf5;
  border: 1px solid #a7f3d0;
  box-shadow: 0 8px 24px -8px rgba(16, 185, 129, 0.15);
}

.alert-icon {
  width: 42px;
  height: 42px;
  border-radius: 12px;
  background: #ffffff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 24px;
  flex-shrink: 0;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
}

.alert-card.danger .alert-icon { color: #ef4444; }
.alert-card.success .alert-icon { color: #10b981; }

.alert-content {
  display: flex;
  flex-direction: column;
  gap: 12px;
  width: 100%;
}

.alert-card.danger .alert-title { color: #991b1b; font-size: 18px; font-weight: 700; }
.alert-card.success .alert-title { color: #065f46; font-size: 18px; font-weight: 700; }

.alert-tech-log {
  background: rgba(0, 0, 0, 0.04);
  border-radius: 8px;
  padding: 12px 16px;
  font-family: 'JetBrains Mono', monospace;
  font-size: 12px;
  color: #991b1b;
  border: 1px solid rgba(0, 0, 0, 0.05);
  overflow-x: auto;
  word-break: break-word;
  line-height: 1.5;
}

.alert-tech-log.success-log {
  color: #065f46;
  background: rgba(16, 185, 129, 0.08);
  border-color: rgba(16, 185, 129, 0.15);
}

.alert-card.danger .alert-note {
  margin-top: 0.5rem;
  opacity: 0.85;
}

.alert-text { font-size: 14px; color: #7f1d1d; line-height: 1.5; }
.alert-card.success .alert-text { font-size: 14px; color: #047857; line-height: 1.5; }

/* Bento Grid Metrics */
.metrics-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 20px;
}

.metric-card {
  background: #ffffff;
  border-radius: 20px;
  padding: 24px;
  border: 1px solid rgba(0, 0, 0, 0.04);
  box-shadow: 0 4px 20px rgba(15, 23, 42, 0.03);
  display: flex;
  flex-direction: column;
  gap: 8px;
  transition: all 0.2s ease;
}

.metric-label {
  font-size: 12px;
  font-weight: 700;
  color: #64748b;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.metric-value {
  font-family: 'JetBrains Mono', monospace;
  font-size: 32px;
  font-weight: 700;
  color: #0f172a;
  letter-spacing: -1px;
}

.metric-card.highlight-danger {
  background: linear-gradient(135deg, #fff1f2, #fee2e2);
  border: 1px solid #fecaca;
}
.metric-card.highlight-danger .metric-label { color: #991b1b; }
.metric-card.highlight-danger .metric-value { color: #7f1d1d; }

.metric-card.highlight-success {
  background: linear-gradient(135deg, #ecfdf5, #d1fae5);
  border: 1px solid #a7f3d0;
}
.metric-card.highlight-success .metric-label { color: #065f46; }
.metric-card.highlight-success .metric-value { color: #047857; }

/* Section Styling */
.section-wrap {
  background: #ffffff;
  border-radius: 20px;
  padding: 24px;
  border: 1px solid rgba(0, 0, 0, 0.04);
  box-shadow: 0 4px 20px rgba(15, 23, 42, 0.03);
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.section-header {
  font-size: 16px;
  font-weight: 700;
  color: #0f172a;
  display: flex;
  align-items: center;
  gap: 10px;
}

.section-header i {
  color: #5c60f5;
  font-size: 20px;
}

.section-count {
  color: #64748b;
  font-weight: 600;
}

/* Tables */
.table-responsive {
  width: 100%;
  overflow-x: auto;
  border-radius: 12px;
}

.grid-table {
  width: 100%;
  min-width: 550px;
  display: flex;
  flex-direction: column;
}

.grid-row {
  display: grid;
  align-items: center;
  gap: 16px;
  padding: 14px 0;
  border-bottom: 1px dashed rgba(0, 0, 0, 0.06);
}

.grid-row:last-child {
  border-bottom: none;
}

.grid-header {
  padding: 0 0 12px 0;
  border-bottom: 1px solid rgba(0, 0, 0, 0.08);
  font-size: 11px;
  font-weight: 700;
  color: #64748b;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.accounts-table .grid-row {
  grid-template-columns: 140px 1fr 160px;
}

.escrow-table .grid-row {
  grid-template-columns: repeat(3, 1fr);
}

.discrepancies-table .grid-row {
  grid-template-columns: 180px 1fr 1fr 1fr;
}

.orders-table .grid-row {
  grid-template-columns: 220px 140px 140px 1fr;
}

/* Cells & Tags */
.cell-mono {
  font-family: 'JetBrains Mono', monospace;
  font-size: 14px;
  font-weight: 600;
  color: #0f172a;
}

.cell-text {
  font-size: 14px;
  color: #0f172a;
}

.text-muted-balance {
  color: #94a3b8 !important;
}

.cell-clickable {
  cursor: pointer;
  color: #5c60f5;
  transition: opacity 0.2s;
}

.cell-clickable:hover {
  text-decoration: underline;
}

.cell-id {
  font-family: 'JetBrains Mono', monospace;
  font-size: 13px;
  color: #475569;
  background: #f8fafc;
  padding: 4px 10px;
  border-radius: 8px;
  border: 1px solid rgba(0, 0, 0, 0.04);
  display: inline-flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  transition: all 0.15s ease;
}

.cell-id:hover {
  background: #eef2ff;
  color: #5c60f5;
  border-color: rgba(92, 96, 245, 0.2);
}

.copy-icon {
  font-size: 12px;
  opacity: 0.6;
}

.pill-tag {
  display: inline-block;
  padding: 4px 10px;
  border-radius: 8px;
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  font-family: 'JetBrains Mono', monospace;
}

.pill-tag.dark {
  background: #0f172a;
  color: #ffffff;
}

.pill-tag.neutral {
  background: #f1f5f9;
  color: #64748b;
}

.pill-status {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 5px 12px;
  border-radius: 99px;
  font-size: 12px;
  font-weight: 600;
}

.pill-status::before {
  content: '';
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
}

.pill-status.success {
  background: #ecfdf5;
  color: #10b981;
}

.pill-status.danger {
  background: #fef2f2;
  color: #ef4444;
}

.pill-status.warning {
  background: #fffbeb;
  color: #f59e0b;
}

.text-danger-reason {
  color: #991b1b;
  font-weight: 500;
}

.text-danger-bold {
  color: #ef4444;
  font-weight: 700;
}

.text-success-bold {
  color: #10b981;
  font-weight: 700;
}

.text-end {
  text-align: right;
}

/* Pagination Bar */
.pagination-bar {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
}

.btn-page {
  background: #f8fafc;
  border: 1px solid rgba(0, 0, 0, 0.08);
  border-radius: 8px;
  width: 32px;
  height: 32px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 16px;
  color: #0f172a;
  cursor: pointer;
  transition: all 0.15s ease;
}

.btn-page:hover:not(:disabled) {
  background: #eef2ff;
  color: #5c60f5;
}

.btn-page:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.page-info {
  font-size: 13px;
  font-weight: 600;
  color: #64748b;
}

/* Skeleton Loading */
.skeleton-container {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.skeleton-bar {
  height: 32px;
  width: 200px;
  background: #e2e8f0;
  border-radius: 8px;
  animation: skeletonPulse 1.5s infinite;
}

.skeleton-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 16px;
}

.skeleton-card {
  height: 100px;
  background: #e2e8f0;
  border-radius: 16px;
  animation: skeletonPulse 1.5s infinite;
}

.skeleton-box {
  height: 200px;
  background: #e2e8f0;
  border-radius: 16px;
  animation: skeletonPulse 1.5s infinite;
}

@keyframes skeletonPulse {
  0%, 100% { opacity: 0.6; }
  50% { opacity: 0.9; }
}

/* Toast Notification */
.toast-notification {
  position: fixed;
  bottom: 24px;
  right: 24px;
  background: #0f172a;
  color: #ffffff;
  padding: 12px 20px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: 500;
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.2);
  z-index: 9999;
}

.toast-fade-enter-active,
.toast-fade-leave-active {
  transition: all 0.25s ease;
}

.toast-fade-enter-from,
.toast-fade-leave-to {
  opacity: 0;
  transform: translateY(10px);
}

/* Responsive Rules */
@media (max-width: 900px) {
  .metrics-grid {
    grid-template-columns: 1fr;
  }
  .alert-card {
    flex-direction: column;
    padding: 16px;
  }
  .metric-value {
    font-size: 28px;
  }
  .section-wrap {
    padding: 16px;
  }
}
</style>

