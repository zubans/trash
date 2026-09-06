<template>
  <div class="incidents-page">
    <header class="page-head">
      <div>
        <h1>Денежные инциденты</h1>
        <p class="page-sub">
          Здесь то, что код заметил и обошёл зажимом: расчёт запросил больше, чем
          заплатил заказчик, или подарок, которого нет на складе. Движение
          записано по зажатым числам и книги сведены — но кто-то получил не то,
          что предписывают правила, и причина в расчёте, давшем исходное число.
        </p>
      </div>
      <div class="head-actions">
        <label class="toggle">
          <input v-model="showAll" type="checkbox" @change="load" />
          <span>показывать разобранные</span>
        </label>
        <button type="button" class="btn-refresh" :disabled="loading" @click="load">
          <i class="ph-bold ph-arrows-clockwise"></i>
        </button>
      </div>
    </header>

    <p v-if="errorMsg" class="alert error">{{ errorMsg }}</p>

    <p v-if="!loading && !incidents.length" class="empty">
      Инцидентов нет. Это и есть нормальное состояние.
    </p>

    <div v-for="incident in incidents" :key="incident.id" class="incident-card" :class="{ resolved: incident.resolved_at }">
      <div class="incident-head">
        <div>
          <div class="incident-title">
            <span class="kind">{{ kindLabel(incident.kind) }}</span>
            <span class="badge" :class="incident.severity.toLowerCase()">{{ incident.severity }}</span>
            <span v-if="incident.resolved_at" class="badge ok">разобран</span>
          </div>
          <div class="incident-meta">
            {{ formatDate(incident.created_at) }}
            <template v-if="incident.order_id"> · заказ {{ incident.order_id.slice(0, 8) }}</template>
            <template v-if="incident.user_id"> · пользователь {{ incident.user_id.slice(0, 8) }}</template>
          </div>
        </div>
      </div>

      <!-- Три числа и есть весь инцидент: сколько должно было быть, сколько
           посчитал код и сколько в итоге записано. -->
      <div class="amounts">
        <div class="amount">
          <span class="label">ожидалось</span>
          <span class="value">{{ formatAmount(incident.expected) }}</span>
        </div>
        <div class="amount">
          <span class="label">посчитано</span>
          <span class="value bad">{{ formatAmount(incident.actual) }}</span>
        </div>
        <div class="amount">
          <span class="label">записано</span>
          <span class="value">{{ formatAmount(incident.applied) }}</span>
        </div>
      </div>

      <pre v-if="incident.details" class="details">{{ formatDetails(incident.details) }}</pre>

      <div v-if="incident.resolved_at" class="resolution">
        <i class="ph-fill ph-check-circle"></i> {{ incident.resolution }}
      </div>
      <div v-else class="resolve-row">
        <input
          v-model="resolutions[incident.id]"
          class="input"
          placeholder="Что выяснилось и что сделано"
        />
        <button
          type="button"
          class="btn-primary"
          :disabled="!resolutions[incident.id] || resolving === incident.id"
          @click="resolve(incident)"
        >
          Закрыть
        </button>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, onMounted, reactive, ref } from 'vue'

import { adminGetIncidents, adminResolveIncident, type MoneyIncident } from '../../api/achievements'

const KIND_LABELS: Record<string, string> = {
  reward_exceeds_payment: 'Вознаграждение больше уплаченного',
  commission_out_of_range: 'Комиссия вне допустимого',
  settlement_mismatch: 'Распределение не сошлось с удержанием',
  gift_out_of_stock: 'Подарок не выдан: склад пуст',
  points_cap_hit: 'Упёрся суточный потолок баллов',
}

export default defineComponent({
  name: 'AdminMoneyIncidents',
  setup() {
    const incidents = ref<MoneyIncident[]>([])
    const resolutions = reactive<Record<string, string>>({})
    const loading = ref(false)
    const resolving = ref('')
    const showAll = ref(false)
    const errorMsg = ref('')

    const load = async () => {
      loading.value = true
      errorMsg.value = ''
      try {
        incidents.value = await adminGetIncidents(showAll.value)
      } catch {
        errorMsg.value = 'Не удалось загрузить инциденты.'
      } finally {
        loading.value = false
      }
    }

    const resolve = async (incident: MoneyIncident) => {
      resolving.value = incident.id
      try {
        await adminResolveIncident(incident.id, resolutions[incident.id])
        await load()
      } catch {
        errorMsg.value = 'Не удалось закрыть инцидент.'
      } finally {
        resolving.value = ''
      }
    }

    const kindLabel = (kind: string) => KIND_LABELS[kind] ?? kind

    // Суммы приходят в рублях (money.Amount сериализуется рублями), пустые —
    // это стороны, которых инцидент не касается.
    const formatAmount = (value?: number) =>
      value === undefined || value === null ? '—' : `${value} ₽`

    const formatDetails = (details: Record<string, unknown>) => JSON.stringify(details, null, 2)

    const formatDate = (value: string) => new Date(value).toLocaleString('ru-RU')

    onMounted(load)

    return {
      incidents,
      resolutions,
      loading,
      resolving,
      showAll,
      errorMsg,
      load,
      resolve,
      kindLabel,
      formatAmount,
      formatDetails,
      formatDate,
    }
  },
})
</script>

<style scoped>
.incidents-page {
  padding: 20px;
  max-width: 900px;
}

.page-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
  margin-bottom: 20px;
}

h1 {
  font-size: 22px;
  margin: 0 0 6px;
}

.page-sub {
  color: #6b7280;
  font-size: 13px;
  margin: 0;
  max-width: 620px;
}

.head-actions {
  display: flex;
  align-items: center;
  gap: 12px;
  white-space: nowrap;
}

.toggle {
  font-size: 12px;
  color: #6b7280;
  display: flex;
  align-items: center;
  gap: 6px;
}

.btn-refresh {
  border: none;
  background: #fff;
  border-radius: 10px;
  padding: 8px 12px;
  cursor: pointer;
}

.alert.error {
  background: #fef2f2;
  color: #b91c1c;
  padding: 10px 12px;
  border-radius: 10px;
  font-size: 13px;
}

.empty {
  color: #6b7280;
  font-size: 14px;
}

.incident-card {
  background: #fff;
  border-radius: 14px;
  padding: 16px;
  margin-bottom: 12px;
  border: 1px solid #fecaca;
}

.incident-card.resolved {
  border-color: #eef0f4;
  opacity: 0.75;
}

.incident-title {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.kind {
  font-weight: 600;
  font-size: 15px;
  color: #111827;
}

.badge {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 999px;
  background: #f3f4f6;
  color: #4b5563;
}

.badge.critical {
  background: #fef2f2;
  color: #b91c1c;
}

.badge.warning {
  background: #fffbeb;
  color: #b45309;
}

.badge.ok {
  background: #ecfdf5;
  color: #059669;
}

.incident-meta {
  color: #9ca3af;
  font-size: 12px;
  margin-top: 4px;
}

.amounts {
  display: flex;
  gap: 24px;
  margin-top: 12px;
  flex-wrap: wrap;
}

.amount {
  display: flex;
  flex-direction: column;
}

.label {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: #9ca3af;
}

.value {
  font-size: 16px;
  font-weight: 600;
  color: #111827;
}

.value.bad {
  color: #b91c1c;
}

.details {
  background: #f9fafb;
  border-radius: 8px;
  padding: 10px;
  font-size: 12px;
  margin-top: 12px;
  overflow-x: auto;
}

.resolution {
  margin-top: 12px;
  color: #059669;
  font-size: 13px;
  display: flex;
  align-items: center;
  gap: 6px;
}

.resolve-row {
  display: flex;
  gap: 10px;
  margin-top: 12px;
  flex-wrap: wrap;
}

.input {
  flex: 1;
  min-width: 220px;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  padding: 8px 10px;
  font-size: 13px;
}

.btn-primary {
  border: none;
  background: #111827;
  color: #fff;
  border-radius: 10px;
  padding: 8px 16px;
  font-size: 13px;
  cursor: pointer;
}

.btn-primary:disabled {
  opacity: 0.5;
  cursor: default;
}
</style>
