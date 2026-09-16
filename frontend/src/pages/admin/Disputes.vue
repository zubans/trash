<template>
  <div class="disputes-page">
    <header class="page-head">
      <p class="page-sub">
        Заказчик заявил, что заказ, отмеченный исполнителем, не выполнен. Сверьте доказательства и решите:
        кто прав — тому деньги, второй стороне штрафной балл; неизвестно — обе стороны получают своё и по баллу.
      </p>
      <div class="head-actions">
        <select v-model="status" class="status-select" @change="load">
          <option value="OPEN">Открытые</option>
          <option value="CLOSED">Закрытые</option>
          <option value="">Все</option>
        </select>
        <button type="button" class="btn-refresh" :disabled="loading" title="Обновить" @click="load">
          <i class="ph-bold ph-arrows-clockwise"></i>
        </button>
      </div>
    </header>

    <p v-if="errorMsg" class="alert error">{{ errorMsg }}</p>
    <p v-if="successMsg" class="alert success">{{ successMsg }}</p>
    <p v-if="!loading && !disputes.length" class="empty">Споров нет.</p>

    <div v-for="item in disputes" :key="item.id" :class="['case-card', { open: item.status === 'OPEN' }]">
      <div class="case-head">
        <div>
          <div class="case-title">
            {{ item.service_name }}
            <span class="case-amount">{{ money(item.hold_amount || item.final_amount) }}</span>
            <span :class="['case-badge', item.status === 'OPEN' ? 'open' : 'closed']">
              {{ item.status === 'OPEN' ? 'Открыт' : closedText(item) }}
            </span>
          </div>
          <div class="case-meta">
            Заказ {{ item.order_id.slice(0, 8) }} · спор от {{ formatDate(item.created_at) }}
            <template v-if="item.order_address"> · {{ item.order_address }}</template>
          </div>
        </div>
        <button type="button" class="btn-evidence" @click="toggleEvidence(item)">
          <i class="ph-bold ph-images"></i>
          {{ evidenceFor === item.id ? 'Скрыть доказательства' : 'Доказательства' }}
        </button>
      </div>

      <div class="parties">
        <div class="party">
          <div class="party-role">Заказчик</div>
          <div class="party-name">{{ item.customer_name || 'Без имени' }}</div>
          <div class="party-meta">{{ item.customer_phone }} · баллов: {{ item.customer_active_points }}</div>
        </div>
        <div class="party">
          <div class="party-role">Исполнитель</div>
          <div class="party-name">{{ item.executor_name || 'Без имени' }}</div>
          <div class="party-meta">
            {{ item.executor_phone }} · баллов: {{ item.executor_active_points }} · споров всего: {{ item.executor_disputes_total }}
          </div>
        </div>
      </div>

      <blockquote class="claim">«{{ item.claim }}»</blockquote>
      <p v-if="item.resolution_note" class="note">Комментарий арбитра: {{ item.resolution_note }}</p>

      <div v-if="evidenceFor === item.id" class="evidence">
        <p v-if="evidenceLoading" class="muted">Загружаем доказательства…</p>
        <template v-else-if="evidence">
          <div class="evidence-order">
            <div>
              <span class="label">Отметка «Исполнил»:</span>
              {{ evidence.order.executed_at ? formatDate(evidence.order.executed_at) : '—' }}
              <template v-if="evidence.order.executed_at_device">
                (на телефоне {{ formatDate(evidence.order.executed_at_device) }})
              </template>
            </div>
            <div v-if="evidence.gesture">
              <span class="label">Жест заказа:</span> {{ evidence.gesture.title }} — {{ evidence.gesture.description }}
            </div>
            <div v-else class="muted">Фото-подтверждение по заказу не требовалось.</div>
          </div>

          <p v-if="evidence.order.photo_required && !evidence.proofs.length" class="alert error">
            Снимков нет.
          </p>

          <div v-for="proof in evidence.proofs" :key="proof.id" class="proof">
            <a :href="fileSrc(proof.file_url)" target="_blank" rel="noopener" class="proof-image-link">
              <img :src="fileSrc(proof.file_url)" :alt="proof.kind" class="proof-image" loading="lazy" />
            </a>
            <div class="proof-info">
              <div class="proof-title">
                {{ proof.kind === 'AREA' ? 'Место заказа' : 'Селфи с заказчиком' }}
                <span class="muted">· камера {{ proof.camera === 'FRONT' ? 'фронтальная' : 'основная' }}</span>
              </div>
              <div :class="['verdict', integrityVerdict(proof).tone]">{{ integrityVerdict(proof).text }}</div>

              <ul class="facts">
                <li>
                  Снято {{ formatDate(proof.device_taken_at) }} —
                  <strong>{{ minutesText(proof.taken_vs_executed_min) }}</strong> отметки «Исполнил»
                </li>
                <li v-if="proof.exif_taken_at">
                  В файле: {{ formatDate(proof.exif_taken_at) }} (расхождение {{ Math.abs(Math.round(proof.exif_vs_device_min || 0)) }} мин)
                </li>
                <li v-else>В файле времени съёмки нет</li>
                <li>
                  До адреса заказа: <strong>{{ metersText(proof.distance_to_order_m) }}</strong>
                  <template v-if="proof.exif_distance_to_order_m !== undefined">
                    (по файлу {{ metersText(proof.exif_distance_to_order_m) }})
                  </template>
                </li>
                <li v-if="proof.track">
                  Трек: точка {{ minutesText(-proof.track.age_min) }} снимка, до места снимка
                  {{ metersText(proof.track.distance_to_photo_m) }}, до адреса {{ metersText(proof.track.distance_to_order_m) }}
                </li>
                <li v-else>Трека рядом со временем съёмки нет (окно ±{{ evidence.limits.max_track_gap_min }} мин)</li>
                <li v-for="alert in proof.geo_alerts" :key="alert.id" class="bad-text">
                  Аномалия скорости {{ Math.round(alert.calculated_speed_kmh) }} км/ч в {{ formatDate(alert.created_at) }}
                </li>
              </ul>

              <div v-if="proof.flags.length" class="flags">
                <span v-for="flag in proof.flags" :key="flag" class="flag">{{ flagLabel(flag) }}</span>
              </div>
            </div>
          </div>
        </template>
      </div>

      <div v-if="item.status === 'OPEN' && canResolve" class="decide">
        <textarea
          v-model="notes[item.id]"
          class="note-input"
          rows="2"
          maxlength="2000"
          placeholder="Комментарий к решению — его увидят обе стороны"
        ></textarea>
        <div class="decide-actions">
          <button type="button" class="btn-decide executor" :disabled="busyId === item.id" @click="decide(item, 'executor')">
            Прав исполнитель
          </button>
          <button type="button" class="btn-decide customer" :disabled="busyId === item.id" @click="decide(item, 'customer')">
            Прав заказчик
          </button>
          <button type="button" class="btn-decide unknown" :disabled="busyId === item.id" @click="decide(item, 'unknown')">
            Неизвестно
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, onMounted, reactive, ref } from 'vue'
import {
  getDisputeEvidence,
  listDisputes,
  resolveDispute,
  type AdminDispute,
  type DisputeDecision,
  type DisputeEvidence,
} from '../../api/disputes'
import { resolveFileUrl } from '../../services/api'
import { useAuthStore } from '../../stores/auth-store'
import {
  CLOSURE_LABELS,
  DECISION_LABELS,
  FLAG_LABELS,
  integrityVerdict,
  metersText,
  minutesText,
} from './disputeEvidence'

const DECISION_CONFIRM: Record<DisputeDecision, string> = {
  executor: 'Прав исполнитель: заказ оплачивается исполнителю, заказчику — штрафной балл. Решение окончательное. Продолжить?',
  customer: 'Прав заказчик: деньги возвращаются заказчику, исполнителю — штрафной балл. Решение окончательное. Продолжить?',
  unknown: 'Неизвестно: заказчику полный возврат, исполнителю оплата за счёт платформы, штрафной балл обоим. Решение окончательное. Продолжить?',
}

export default defineComponent({
  name: 'AdminDisputes',
  setup() {
    const authStore = useAuthStore()
    const disputes = ref<AdminDispute[]>([])
    const status = ref<'' | 'OPEN' | 'CLOSED'>('OPEN')
    const loading = ref(false)
    const errorMsg = ref('')
    const successMsg = ref('')
    const busyId = ref('')
    const notes = reactive<Record<string, string>>({})
    const evidenceFor = ref('')
    const evidence = ref<DisputeEvidence | null>(null)
    const evidenceLoading = ref(false)

    const canResolve = computed(() => authStore.can('disputes.edit'))

    const load = async () => {
      loading.value = true
      errorMsg.value = ''
      try {
        disputes.value = await listDisputes(status.value)
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Не удалось загрузить споры'
      } finally {
        loading.value = false
      }
    }

    const toggleEvidence = async (item: AdminDispute) => {
      if (evidenceFor.value === item.id) {
        evidenceFor.value = ''
        evidence.value = null
        return
      }
      evidenceFor.value = item.id
      evidence.value = null
      evidenceLoading.value = true
      try {
        evidence.value = await getDisputeEvidence(item.id)
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Не удалось загрузить доказательства'
      } finally {
        evidenceLoading.value = false
      }
    }

    const decide = async (item: AdminDispute, decision: DisputeDecision) => {
      if (!window.confirm(DECISION_CONFIRM[decision])) return
      busyId.value = item.id
      errorMsg.value = ''
      successMsg.value = ''
      try {
        await resolveDispute(item.id, decision, notes[item.id] || '')
        successMsg.value = `Спор закрыт: ${DECISION_LABELS[decision.toUpperCase()]}.`
        await load()
      } catch (err: any) {
        errorMsg.value =
          err.response?.status === 409
            ? 'Спор уже закрыт — заказчиком, исполнителем или другим арбитром. Список обновлён.'
            : err.response?.data || 'Не удалось закрыть спор'
        if (err.response?.status === 409) await load()
      } finally {
        busyId.value = ''
      }
    }

    const closedText = (item: AdminDispute) => {
      const closure = item.closure ? CLOSURE_LABELS[item.closure] : 'закрыт'
      return item.decision ? `${closure}: ${DECISION_LABELS[item.decision]}` : closure
    }

    const formatDate = (value: string) =>
      new Date(value).toLocaleString('ru-RU', { dateStyle: 'short', timeStyle: 'short' })
    const money = (value: number) =>
      `${Number(value || 0).toLocaleString('ru-RU', { minimumFractionDigits: 2 })} ₽`
    const fileSrc = (url: string) => resolveFileUrl(url)
    const flagLabel = (flag: string) => FLAG_LABELS[flag] || flag

    onMounted(load)

    return {
      disputes,
      status,
      loading,
      errorMsg,
      successMsg,
      busyId,
      notes,
      evidenceFor,
      evidence,
      evidenceLoading,
      canResolve,
      load,
      toggleEvidence,
      decide,
      closedText,
      formatDate,
      money,
      fileSrc,
      flagLabel,
      integrityVerdict,
      minutesText,
      metersText,
    }
  },
})
</script>

<style scoped>
.disputes-page { max-width: 1040px; margin: 0 auto; padding: 16px; color: #0f172a; }
.page-head { display: flex; justify-content: space-between; gap: 16px; margin-bottom: 20px; }
.page-sub { margin: 6px 0 0; color: #64748b; font-size: 14px; line-height: 1.5; max-width: 660px; }
.head-actions { display: flex; gap: 8px; flex-shrink: 0; }
.status-select { height: 40px; border-radius: 10px; border: 1px solid #e2e8f0; padding: 0 12px; font-family: inherit; font-size: 14px; background: #fff; }
.btn-refresh { width: 40px; height: 40px; border-radius: 10px; border: 1px solid #e2e8f0; background: #fff; cursor: pointer; color: #475569; }
.alert { padding: 10px 14px; border-radius: 10px; font-size: 14px; margin: 0 0 16px; }
.alert.error { background: #fef2f2; color: #b91c1c; }
.alert.success { background: #f0fdf4; color: #15803d; }
.empty, .muted { color: #64748b; font-size: 14px; }
.case-card { border: 1px solid #e2e8f0; border-radius: 16px; padding: 18px 20px; margin-bottom: 16px; background: #fff; }
.case-card.open { border-color: #fecaca; }
.case-head { display: flex; justify-content: space-between; align-items: flex-start; gap: 16px; }
.case-title { font-size: 16px; font-weight: 700; display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.case-amount { color: #334155; font-weight: 600; }
.case-badge { padding: 2px 8px; border-radius: 999px; font-size: 11px; font-weight: 600; }
.case-badge.open { background: #fee2e2; color: #b91c1c; }
.case-badge.closed { background: #f1f5f9; color: #475569; }
.case-meta { margin-top: 4px; font-size: 12px; color: #64748b; }
.btn-evidence { height: 38px; border-radius: 10px; border: 1px solid #c7d2fe; background: #eef2ff; color: #4338ca; font-weight: 600; cursor: pointer; padding: 0 12px; font-family: inherit; white-space: nowrap; }
.parties { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 10px; margin: 14px 0 10px; }
.party { background: #f8fafc; border-radius: 12px; padding: 10px 12px; }
.party-role { font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; color: #64748b; font-weight: 700; }
.party-name { font-weight: 600; }
.party-meta { font-size: 12px; color: #64748b; }
.claim { margin: 0 0 8px; padding: 10px 14px; border-left: 3px solid #dc2626; background: #fff7f7; border-radius: 0 10px 10px 0; font-size: 14px; }
.note { font-size: 13px; color: #334155; }
.evidence { border-top: 1px dashed #e2e8f0; margin-top: 12px; padding-top: 12px; }
.evidence-order { font-size: 13px; line-height: 1.6; margin-bottom: 10px; }
.label { color: #64748b; }
.proof { display: flex; gap: 14px; flex-wrap: wrap; border: 1px solid #f1f5f9; border-radius: 12px; padding: 12px; margin-bottom: 10px; }
.proof-image { width: 220px; max-width: 100%; border-radius: 10px; object-fit: cover; background: #f1f5f9; }
.proof-info { flex: 1; min-width: 240px; }
.proof-title { font-weight: 700; margin-bottom: 6px; }
.verdict { font-size: 13px; font-weight: 600; padding: 6px 10px; border-radius: 8px; margin-bottom: 8px; }
.verdict.ok { background: #f0fdf4; color: #15803d; }
.verdict.warn { background: #fffbeb; color: #b45309; }
.verdict.bad { background: #fef2f2; color: #b91c1c; }
.facts { margin: 0; padding-left: 18px; font-size: 13px; line-height: 1.6; color: #334155; }
.bad-text { color: #b91c1c; }
.flags { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
.flag { background: #fef2f2; color: #b91c1c; border-radius: 999px; padding: 2px 10px; font-size: 12px; font-weight: 600; }
.decide { border-top: 1px solid #f1f5f9; margin-top: 12px; padding-top: 12px; }
.note-input { width: 100%; border: 1px solid #e2e8f0; border-radius: 10px; padding: 8px 10px; font-family: inherit; font-size: 14px; resize: vertical; }
.decide-actions { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 8px; }
.btn-decide { height: 40px; border-radius: 10px; border: none; padding: 0 14px; font-weight: 600; cursor: pointer; font-family: inherit; color: #fff; }
.btn-decide:disabled { opacity: 0.55; cursor: default; }
.btn-decide.executor { background: #059669; }
.btn-decide.customer { background: #dc2626; }
.btn-decide.unknown { background: #64748b; }
@media (max-width: 640px) {
  .page-head, .case-head { flex-direction: column; }
}
</style>
