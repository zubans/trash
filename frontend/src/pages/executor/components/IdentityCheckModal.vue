<template>
  <div class="identity-overlay" @click.self="$emit('close')">
    <div class="identity-modal">
      <header class="identity-header">
        <div>
          <h3>Проверка данных</h3>
          <p class="identity-subtitle">
            Введите данные так, как они указаны в документе заказчика.
          </p>
        </div>
        <button type="button" class="identity-close" @click="$emit('close')">
          <i class="ph-bold ph-x"></i>
        </button>
      </header>

      <div class="identity-body">
        <p class="identity-note">
          Данные аккаунта вам не показываются: система сверит введённое сама.
        </p>

        <div v-for="field in fields" :key="field" class="identity-field">
          <label class="identity-label">{{ labelFor(field) }}</label>
          <input
            v-model="values[field]"
            :type="field === 'birth_date' ? 'date' : 'text'"
            class="identity-input"
            :placeholder="placeholderFor(field)"
          />
        </div>

        <!-- Паспорт заказчика с фото: без него сервер сверку не примет. Без
             сети он встаёт в очередь зашифрованным и уходит, когда сеть
             появится; сверка — после этого. -->
        <section v-if="requirePassport" class="identity-passport">
          <div class="identity-passport-head">
            <span class="identity-label">{{ $t('passport.verification.title') }}</span>
            <!-- Подсказка тому, кто верифицирует: зачем паспорт и что получит
                 заказчик. Заявку создаёт сервер сам, просить её не надо. -->
            <button type="button" class="identity-info" :aria-expanded="showHint" @click="showHint = !showHint">
              <i class="ph-bold ph-info"></i>
            </button>
            <div v-if="showHint" class="identity-hint">
              <strong>{{ $t('passport.verification.whyTitle') }}</strong>
              <p>{{ $t('passport.verification.why') }}</p>
              <button type="button" class="identity-hint-close" @click="showHint = false">{{ $t('common.close') }}</button>
            </div>
          </div>
          <p v-if="passportState === 'sent'" class="identity-ok">{{ $t('passport.verification.sent') }}</p>
          <p v-else-if="passportState === 'queued'" class="identity-warning">{{ $t('passport.verification.queued') }}</p>
          <template v-if="passportState !== 'sent'">
            <PassportFields v-model="passport" :errors="passportErrors" />
            <label class="identity-photo">
              <i class="ph-bold ph-camera"></i>
              {{ photo ? $t('passport.verification.photoTaken') : $t('passport.verification.takePhoto') }}
              <input type="file" accept="image/jpeg,image/png,image/webp" capture="environment" @change="pickPhoto" />
            </label>
            <p class="identity-auto">{{ $t('passport.verification.autoRequest') }}</p>
          </template>
        </section>

        <p v-if="warning" class="identity-warning">{{ warning }}</p>
        <p v-if="errorText" class="identity-error">{{ errorText }}</p>
      </div>

      <footer class="identity-footer">
        <button type="button" class="identity-btn-secondary" @click="$emit('close')">
          Отмена
        </button>
        <button type="button" class="identity-btn-primary" :disabled="busy" @click="submit">
          <i class="ph-bold ph-check me-1"></i>
          {{ busy ? 'Проверяем…' : 'Проверить' }}
        </button>
      </footer>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, onMounted, reactive, ref, type PropType } from 'vue'
import { useI18n } from 'vue-i18n'
import api from '../../../services/api'
import PassportFields from '../../../components/passport/PassportFields.vue'
import { emptyPassport, passportError, passportPayload, saveOrderPassport, uploadOrderPassportPhoto, type PassportData } from '../../../api/passport'
import { proofQueue } from '../../../modules/photo-proof/queue'
import { online } from '../../../modules/photo-proof/network'

// Подписи полей. Имена приходят из скрипта услуги, поэтому неизвестное
// показывается как есть, а не отбрасывается: на поведение, спрашивающее что-то
// новое, всё равно должно быть можно ответить.
const LABELS: Record<string, string> = {
  last_name: 'Фамилия',
  first_name: 'Имя',
  patronymic: 'Отчество',
  birth_date: 'Дата рождения',
}

export default defineComponent({
  name: 'IdentityCheckModal',
  components: { PassportFields },
  props: {
    orderId: { type: String, required: true },
    fields: { type: Array as PropType<string[]>, required: true },
    // Услуга требует паспорт заказчика с фото (require_passport).
    requirePassport: { type: Boolean, default: false },
  },
  emits: ['close', 'verified'],
  setup(props, { emit }) {
    const { t } = useI18n()
    const queue = proofQueue()
    const passport = ref<PassportData>(emptyPassport())
    const passportErrors = ref<Record<string, string>>({})
    const photo = ref<File | null>(null)
    const showHint = ref(false)
    // none — ещё не отправлен; queued — ждёт сети в очереди; sent — на сервере.
    const passportState = ref<'none' | 'queued' | 'sent'>('none')

    onMounted(() => {
      if (props.requirePassport && queue.pendingPassportFor(props.orderId)) passportState.value = 'queued'
    })

    const pickPhoto = (event: Event) => {
      const input = event.target as HTMLInputElement
      photo.value = input.files?.[0] || null
      input.value = ''
    }

    // Паспорт уходит до сверки. С сетью — сразу; без сети или при сбое — в
    // очередь, и сверка ждёт его отправки. Возвращает, можно ли сверять.
    const sendPassport = async (): Promise<boolean> => {
      if (passportState.value === 'sent') return true
      if (passportState.value === 'queued') {
        await queue.flush()
        if (!queue.pendingPassportFor(props.orderId)) {
          passportState.value = 'sent'
          return true
        }
        warning.value = t('passport.verification.queued')
        return false
      }
      const p = passport.value
      // Ничего не внесено — паспорт мог уже дойти раньше (из очереди или с
      // другого устройства): решает сервер, и при отказе форма попросит паспорт.
      if (!p.series.trim() && !p.number.trim() && !p.issued_at && !photo.value) return true
      if (!p.series.trim() || !p.number.trim() || !p.issued_at || !photo.value) {
        errorText.value = t('passport.verification.fillFirst')
        return false
      }
      const data = passportPayload(p)
      if (online.value) {
        try {
          await saveOrderPassport(props.orderId, data)
          await uploadOrderPassportPhoto(props.orderId, photo.value)
          passportState.value = 'sent'
          return true
        } catch (err: any) {
          const e = passportError(err)
          if (err?.response) {
            // Сервер ответил отказом — очередь его не исправит.
            passportErrors.value = e?.fields || {}
            errorText.value = e?.message || t('passport.verification.failed')
            return false
          }
          // Нет ответа — дальше как без сети.
        }
      }
      if (!queue.canQueuePassport()) {
        errorText.value = t('passport.verification.noOffline')
        return false
      }
      await queue.enqueuePassport(props.orderId, data, new Uint8Array(await photo.value.arrayBuffer()))
      passportState.value = 'queued'
      warning.value = t('passport.verification.queued')
      return false
    }

    const values = reactive<Record<string, string>>({})
    props.fields.forEach((field) => {
      values[field] = ''
    })

    const busy = ref(false)
    const warning = ref('')
    const errorText = ref('')

    const labelFor = (field: string) => LABELS[field] || field
    const placeholderFor = (field: string) =>
      field === 'patronymic' ? 'если есть в документе' : ''

    const submit = async () => {
      busy.value = true
      warning.value = ''
      errorText.value = ''
      try {
        if (props.requirePassport && !(await sendPassport())) return
        const { data } = await api.post(`/executor/orders/${props.orderId}/submission`, values)
        if (data.matched) {
          emit('verified', data)
          return
        }
        // Какое поле было неверным, не показывается: остальные тогда подобрали бы
        // перебором. Что делать дальше, говорит собственное сообщение сервера.
        warning.value = (data.messages && data.messages[0]) || 'Данные не совпали. Сверьте их с документом.'
        if (data.escalated) {
          emit('verified', data)
        }
      } catch (err: any) {
        const data = err.response?.data
        if (data?.error === 'passport_required') {
          // Паспорт на сервер не дошёл — вносим снова.
          passportState.value = 'none'
          errorText.value = data.message
          return
        }
        errorText.value = typeof data === 'string' && data ? data : 'Не удалось отправить данные'
      } finally {
        busy.value = false
      }
    }

    return {
      values, busy, warning, errorText, labelFor, placeholderFor, submit,
      passport, passportErrors, photo, passportState, pickPhoto, showHint,
    }
  },
})
</script>

<style scoped>
.identity-overlay {
  position: fixed;
  inset: 0;
  background: rgba(15, 23, 42, 0.55);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
  z-index: 1200;
}

.identity-modal {
  background: #ffffff;
  border-radius: 20px;
  width: 100%;
  max-width: 420px;
  max-height: 92vh;
  overflow: auto;
  box-shadow: 0 24px 48px -16px rgba(15, 23, 42, 0.35);
}

.identity-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 20px 20px 12px;
}

.identity-header h3 {
  margin: 0;
  font-size: 18px;
  font-weight: 700;
  color: #0f172a;
}

.identity-subtitle {
  margin: 6px 0 0;
  font-size: 13px;
  color: #64748b;
  line-height: 1.4;
}

.identity-close {
  border: none;
  background: #f1f5f9;
  color: #475569;
  width: 32px;
  height: 32px;
  border-radius: 10px;
  cursor: pointer;
  flex-shrink: 0;
}

.identity-body {
  padding: 4px 20px 8px;
}

.identity-note {
  background: #f8fafc;
  border-left: 3px solid #6366f1;
  padding: 8px 12px;
  border-radius: 0 8px 8px 0;
  font-size: 12px;
  color: #475569;
  margin: 0 0 14px;
  line-height: 1.4;
}

.identity-field {
  margin-bottom: 12px;
}

.identity-label {
  display: block;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.4px;
  color: #64748b;
  text-transform: uppercase;
  margin-bottom: 6px;
}

.identity-input {
  width: 100%;
  height: 44px;
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  padding: 0 14px;
  font-size: 15px;
  color: #0f172a;
  font-family: inherit;
}

.identity-input:focus {
  outline: none;
  border-color: #6366f1;
  box-shadow: 0 0 0 3px rgba(99, 102, 241, 0.15);
}

.identity-warning {
  margin: 4px 0 0;
  padding: 10px 12px;
  background: #fffbeb;
  border-radius: 10px;
  color: #b45309;
  font-size: 13px;
  line-height: 1.45;
}

.identity-error {
  margin: 8px 0 0;
  color: #b91c1c;
  font-size: 13px;
}

.identity-footer {
  display: flex;
  gap: 10px;
  padding: 14px 20px 20px;
}

.identity-btn-secondary,
.identity-btn-primary {
  flex: 1;
  height: 46px;
  border-radius: 12px;
  font-size: 15px;
  font-weight: 600;
  cursor: pointer;
  border: none;
  font-family: inherit;
}

.identity-btn-secondary {
  background: #f1f5f9;
  color: #475569;
}

.identity-btn-primary {
  background: #6366f1;
  color: #ffffff;
}

.identity-btn-primary:disabled {
  opacity: 0.6;
  cursor: default;
}
.identity-passport {
  border-top: 1px solid #e2e8f0;
  padding-top: 12px;
  margin-top: 4px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.identity-passport-head {
  position: relative;
  display: flex;
  align-items: center;
  gap: 6px;
}
.identity-passport-head .identity-label {
  margin-bottom: 0;
}
.identity-info {
  border: none;
  background: none;
  color: #6366f1;
  cursor: pointer;
  padding: 0;
  line-height: 1;
}
.identity-hint {
  position: absolute;
  top: 24px;
  left: 0;
  right: 0;
  z-index: 1;
  background: #ffffff;
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  box-shadow: 0 12px 24px -12px rgba(15, 23, 42, 0.35);
  padding: 12px;
  font-size: 13px;
  color: #334155;
  line-height: 1.45;
}
.identity-hint p {
  margin: 6px 0 8px;
}
.identity-hint-close {
  border: none;
  background: #f1f5f9;
  color: #475569;
  border-radius: 8px;
  padding: 6px 10px;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
}
.identity-auto {
  margin: 0;
  font-size: 12px;
  color: #64748b;
  line-height: 1.4;
}
.identity-photo {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  align-self: flex-start;
  background: #f1f5f9;
  border-radius: 12px;
  padding: 9px 14px;
  font-size: 13px;
  font-weight: 600;
  color: #334155;
  cursor: pointer;
}
.identity-photo input {
  display: none;
}
.identity-ok {
  color: #15803d;
  font-size: 13px;
  margin: 0;
}
</style>
