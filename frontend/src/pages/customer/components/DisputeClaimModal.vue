<template>
  <div class="claim-overlay" @click.self="$emit('close')">
    <div class="claim-modal" role="dialog" aria-modal="true">
      <header class="claim-header">
        <div>
          <h3>Заказ не выполнен</h3>
          <p class="claim-subtitle">
            Опишите, что не так. Спор уйдёт на разбор, исполнитель получит уведомление.
          </p>
        </div>
        <button type="button" class="claim-close" aria-label="Закрыть" @click="$emit('close')">
          <i class="ph-bold ph-x"></i>
        </button>
      </header>

      <div class="claim-body">
        <textarea
          v-model="claim"
          class="claim-input"
          rows="5"
          maxlength="2000"
          placeholder="Например: мешки остались у подъезда"
        ></textarea>
        <p class="claim-note">
          Если договоритесь с исполнителем, закройте спор кнопкой подтверждения выполнения — в любой момент до решения.
        </p>
        <p v-if="errorText" class="claim-error">{{ errorText }}</p>
      </div>

      <footer class="claim-footer">
        <button type="button" class="claim-btn secondary" @click="$emit('close')">Отмена</button>
        <button type="button" class="claim-btn danger" :disabled="busy || !claim.trim()" @click="submit">
          {{ busy ? 'Отправляем…' : 'Оспорить' }}
        </button>
      </footer>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref } from 'vue'
import api from '../../../services/api'

export default defineComponent({
  name: 'DisputeClaimModal',
  props: {
    orderId: { type: String, required: true },
  },
  emits: ['close', 'opened'],
  setup(props, { emit }) {
    const claim = ref('')
    const busy = ref(false)
    const errorText = ref('')

    const submit = async () => {
      busy.value = true
      errorText.value = ''
      try {
        const { data } = await api.post(`/customer/orders/${props.orderId}/dispute`, { claim: claim.value })
        emit('opened', data)
      } catch (err: any) {
        errorText.value = err.response?.data || 'Не удалось открыть спор'
      } finally {
        busy.value = false
      }
    }

    return { claim, busy, errorText, submit }
  },
})
</script>

<style scoped>
.claim-overlay {
  position: fixed;
  inset: 0;
  background: rgba(15, 23, 42, 0.55);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
  z-index: 1200;
}
.claim-modal {
  background: #fff;
  border-radius: 20px;
  width: 100%;
  max-width: 420px;
  box-shadow: 0 24px 48px -16px rgba(15, 23, 42, 0.35);
}
.claim-header {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding: 20px 20px 10px;
}
.claim-header h3 {
  margin: 0;
  font-size: 18px;
  font-weight: 700;
  color: #0f172a;
}
.claim-subtitle {
  margin: 6px 0 0;
  font-size: 13px;
  color: #64748b;
  line-height: 1.4;
}
.claim-close {
  border: none;
  background: #f1f5f9;
  color: #475569;
  width: 32px;
  height: 32px;
  border-radius: 10px;
  cursor: pointer;
  flex-shrink: 0;
}
.claim-body {
  padding: 4px 20px 8px;
}
.claim-input {
  width: 100%;
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  padding: 10px 12px;
  font-size: 15px;
  font-family: inherit;
  resize: vertical;
}
.claim-input:focus {
  outline: none;
  border-color: #dc2626;
  box-shadow: 0 0 0 3px rgba(220, 38, 38, 0.12);
}
.claim-note {
  margin: 8px 0 0;
  font-size: 12px;
  color: #64748b;
  line-height: 1.4;
}
.claim-error {
  margin: 8px 0 0;
  color: #b91c1c;
  font-size: 13px;
}
.claim-footer {
  display: flex;
  gap: 10px;
  padding: 12px 20px 20px;
}
.claim-btn {
  flex: 1;
  height: 46px;
  border: none;
  border-radius: 12px;
  font-size: 15px;
  font-weight: 600;
  cursor: pointer;
  font-family: inherit;
}
.claim-btn:disabled {
  opacity: 0.55;
  cursor: default;
}
.claim-btn.secondary {
  background: #f1f5f9;
  color: #475569;
}
.claim-btn.danger {
  background: #dc2626;
  color: #fff;
}
</style>
