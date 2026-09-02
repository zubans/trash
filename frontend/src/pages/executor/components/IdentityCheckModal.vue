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
import { defineComponent, reactive, ref, type PropType } from 'vue'
import api from '../../../services/api'

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
  props: {
    orderId: { type: String, required: true },
    fields: { type: Array as PropType<string[]>, required: true },
  },
  emits: ['close', 'verified'],
  setup(props, { emit }) {
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
        errorText.value = err.response?.data || 'Не удалось отправить данные'
      } finally {
        busy.value = false
      }
    }

    return { values, busy, warning, errorText, labelFor, placeholderFor, submit }
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
  overflow: hidden;
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
</style>
