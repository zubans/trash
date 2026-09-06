<template>
  <div class="escalations-page">
    <header class="page-head">
      <div>
        <h1>Модерация проверок</h1>
        <p class="page-sub">
          Заказы, которые скрипт услуги передал администратору: данные, введённые
          исполнителем, не совпали с данными аккаунта.
        </p>
      </div>
      <div class="head-actions">
        <select v-model="status" class="status-select" @change="load">
          <option value="OPEN">Открытые</option>
          <option value="RESOLVED">Закрытые</option>
        </select>
        <button type="button" class="btn-refresh" :disabled="loading" @click="load">
          <i class="ph-bold ph-arrows-clockwise"></i>
        </button>
      </div>
    </header>

    <p v-if="errorMsg" class="alert error">{{ errorMsg }}</p>
    <p v-if="successMsg" class="alert success">{{ successMsg }}</p>

    <p v-if="!loading && !escalations.length" class="empty">
      Ничего не ждёт решения.
    </p>

    <div v-for="item in escalations" :key="item.id" class="case-card">
      <div class="case-head">
        <div>
          <div class="case-title">
            {{ item.customer_name || 'Без имени' }}
            <span class="case-badge">{{ item.service_code }}</span>
          </div>
          <div class="case-meta">
            Заказ {{ item.order_id.slice(0, 8) }} · статус {{ item.order_status }} ·
            {{ formatDate(item.created_at) }}
          </div>
        </div>
        <!-- Два разных решения, поэтому две кнопки. «Верифицировать» — это
             «документ в порядке»: заказчик подтверждается, заказ закрывается и
             исполнитель получает вознаграждение. «Снять с модерации» ничего не
             подтверждает — оно возвращает заказ исполнителю с новым кругом
             попыток. -->
        <div v-if="item.status === 'OPEN'" class="case-actions">
          <button
            v-if="canVerify"
            type="button"
            class="btn-verify"
            :disabled="busyId === item.id"
            @click="verify(item)"
          >
            <i class="ph-bold ph-seal-check"></i>
            Верифицировать
          </button>
          <button
            type="button"
            class="btn-resolve"
            :disabled="busyId === item.id"
            @click="resolve(item)"
          >
            Снять с модерации
          </button>
        </div>
      </div>

      <p class="case-reason">{{ item.reason }}</p>

      <table v-if="item.submissions && item.submissions.length" class="attempts">
        <thead>
          <tr>
            <th>Попытка</th>
            <th v-for="field in fieldsOf(item)" :key="field">{{ labelFor(field) }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="submission in item.submissions" :key="submission.id">
            <td>{{ submission.attempt }}</td>
            <td
              v-for="field in fieldsOf(item)"
              :key="field"
              :class="{ mismatch: submission.mismatches.includes(field) }"
            >
              {{ submission.fields[field] || '—' }}
            </td>
          </tr>
        </tbody>
      </table>

      <p class="case-hint">
        Красным — поля, не совпавшие с аккаунтом. <strong>Верифицировать</strong> —
        документ в порядке: заказчик подтверждается, заказ закрывается сам,
        исполнитель получает вознаграждение. <strong>Снять с модерации</strong>
        ничего не подтверждает: заказ возвращается исполнителю, и попытки ввода
        начинаются заново. Если проверка не прошла совсем — отмените заказ.
      </p>
    </div>
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, onMounted, ref } from 'vue'
import {
  getEscalations,
  resolveEscalation,
  verifyEscalationCustomer,
  type BehaviorEscalation,
} from '../../api/escalations'
import { useAuthStore } from '../../stores/auth-store'

const LABELS: Record<string, string> = {
  last_name: 'Фамилия',
  first_name: 'Имя',
  patronymic: 'Отчество',
  birth_date: 'Дата рождения',
}

export default defineComponent({
  name: 'AdminEscalations',
  setup() {
    const authStore = useAuthStore()

    const escalations = ref<BehaviorEscalation[]>([])
    const status = ref('OPEN')
    const loading = ref(false)
    const errorMsg = ref('')
    const successMsg = ref('')
    // Какой случай сейчас в работе: обе кнопки на карточке блокируются вместе,
    // чтобы двойной клик не отправил и верификацию, и снятие.
    const busyId = ref('')

    // Верификация — это правка пользователя, и охраняется она правом на
    // пользователей, а не правом на модерацию. У модератора, которому выдали
    // только разбор проверок, кнопки не будет: сервер такой запрос всё равно
    // отклонит, и показывать её значило бы обещать несуществующее.
    const canVerify = computed(() => authStore.can('users.edit'))

    const load = async () => {
      loading.value = true
      errorMsg.value = ''
      try {
        escalations.value = await getEscalations(status.value)
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Не удалось загрузить список'
      } finally {
        loading.value = false
      }
    }

    const resolve = async (item: BehaviorEscalation) => {
      successMsg.value = ''
      errorMsg.value = ''
      busyId.value = item.id
      try {
        await resolveEscalation(item.id)
        successMsg.value = 'Случай снят с модерации: заказ вернулся исполнителю, попытки начинаются заново'
        await load()
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Не удалось закрыть случай'
      } finally {
        busyId.value = ''
      }
    }

    // Верифицировать — решение по существу: документ в порядке. Дальше всё
    // делает скрипт услуги по событию user.verified: закрывает заказ, платит
    // исполнителю и снимает случай с модерации, поэтому отдельно закрывать его
    // здесь не нужно — список просто перезагружается.
    const verify = async (item: BehaviorEscalation) => {
      successMsg.value = ''
      errorMsg.value = ''
      const name = item.customer_name || 'заказчика'
      if (!window.confirm(`Верифицировать ${name}? Заказ закроется, исполнитель получит вознаграждение.`)) {
        return
      }
      busyId.value = item.id
      try {
        await verifyEscalationCustomer(item.customer_id)
        successMsg.value = 'Заказчик верифицирован, заказ закрыт'
        await load()
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Не удалось верифицировать заказчика'
      } finally {
        busyId.value = ''
      }
    }

    // Колонки — это то, что содержат попытки, поэтому поведение, проверяющее другие
    // поля, отрисовывается без правки этой страницы.
    const fieldsOf = (item: BehaviorEscalation) => {
      const fields = new Set<string>()
      ;(item.submissions || []).forEach((submission) => {
        Object.keys(submission.fields || {}).forEach((field) => fields.add(field))
      })
      return Array.from(fields)
    }

    const labelFor = (field: string) => LABELS[field] || field

    const formatDate = (value: string) =>
      new Date(value).toLocaleString('ru-RU', { dateStyle: 'short', timeStyle: 'short' })

    onMounted(load)

    return {
      escalations,
      status,
      loading,
      errorMsg,
      successMsg,
      busyId,
      canVerify,
      load,
      resolve,
      verify,
      fieldsOf,
      labelFor,
      formatDate,
    }
  },
})
</script>

<style scoped>
.escalations-page {
  max-width: 1000px;
  margin: 0 auto;
  padding: 16px;
  color: #0f172a;
}

.page-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 20px;
}

.page-head h1 {
  margin: 0;
  font-size: 22px;
  font-weight: 700;
}

.page-sub {
  margin: 6px 0 0;
  color: #64748b;
  font-size: 14px;
  line-height: 1.5;
  max-width: 620px;
}

.head-actions {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
}

.status-select {
  height: 40px;
  border-radius: 10px;
  border: 1px solid #e2e8f0;
  padding: 0 12px;
  font-family: inherit;
  font-size: 14px;
  background: #fff;
}

.btn-refresh {
  width: 40px;
  height: 40px;
  border-radius: 10px;
  border: 1px solid #e2e8f0;
  background: #fff;
  cursor: pointer;
  color: #475569;
}

.alert {
  padding: 10px 14px;
  border-radius: 10px;
  font-size: 14px;
  margin: 0 0 16px;
}

.alert.error {
  background: #fef2f2;
  color: #b91c1c;
}

.alert.success {
  background: #f0fdf4;
  color: #15803d;
}

.empty {
  color: #64748b;
  font-size: 14px;
}

.case-card {
  border: 1px solid #e2e8f0;
  border-radius: 16px;
  padding: 18px 20px;
  margin-bottom: 16px;
  background: #fff;
}

.case-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}

.case-title {
  font-size: 16px;
  font-weight: 700;
}

.case-badge {
  display: inline-block;
  margin-left: 8px;
  padding: 2px 8px;
  border-radius: 999px;
  background: #eef2ff;
  color: #4338ca;
  font-size: 11px;
  font-weight: 600;
  vertical-align: middle;
}

.case-meta {
  margin-top: 4px;
  font-size: 12px;
  color: #64748b;
}

.case-actions {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
  flex-wrap: wrap;
}

.btn-resolve {
  border: 1px solid #e2e8f0;
  background: #fff;
  border-radius: 10px;
  padding: 8px 14px;
  font-size: 13px;
  font-weight: 600;
  color: #475569;
  cursor: pointer;
  flex-shrink: 0;
}

/* Заливкой выделена именно верификация: из двух решений это то, ради которого
   случай попал к администратору. */
.btn-verify {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border: none;
  background: #047857;
  color: #fff;
  border-radius: 10px;
  padding: 8px 14px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  flex-shrink: 0;
}

.btn-verify:disabled,
.btn-resolve:disabled {
  opacity: 0.5;
  cursor: default;
}

.case-reason {
  margin: 12px 0;
  font-size: 14px;
  color: #334155;
}

.attempts {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.attempts th,
.attempts td {
  text-align: left;
  padding: 8px 10px;
  border-bottom: 1px solid #f1f5f9;
}

.attempts th {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.4px;
  color: #64748b;
}

.attempts td.mismatch {
  color: #b91c1c;
  font-weight: 600;
}

.case-hint {
  margin: 12px 0 0;
  font-size: 12px;
  color: #64748b;
  line-height: 1.5;
}
</style>
