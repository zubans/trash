<template>
  <div class="passport-fields">
    <label class="pf-field">
      <span>{{ $t('passport.fields.series') }}</span>
      <input
        :value="modelValue.series"
        class="pf-input"
        :class="{ invalid: errors.series }"
        inputmode="numeric"
        maxlength="5"
        placeholder="45 10"
        :disabled="disabled"
        @input="set('series', ($event.target as HTMLInputElement).value)"
      />
      <span v-if="errors.series" class="pf-error">{{ errors.series }}</span>
    </label>
    <label class="pf-field">
      <span>{{ $t('passport.fields.number') }}</span>
      <input
        :value="modelValue.number"
        class="pf-input"
        :class="{ invalid: errors.number }"
        inputmode="numeric"
        maxlength="7"
        placeholder="123456"
        :disabled="disabled"
        @input="set('number', ($event.target as HTMLInputElement).value)"
      />
      <span v-if="errors.number" class="pf-error">{{ errors.number }}</span>
    </label>
    <label class="pf-field">
      <span>{{ $t('passport.fields.issuedAt') }}</span>
      <input
        :value="modelValue.issued_at"
        type="date"
        class="pf-input"
        :class="{ invalid: errors.issued_at }"
        :disabled="disabled"
        @input="set('issued_at', ($event.target as HTMLInputElement).value)"
      />
      <span v-if="errors.issued_at" class="pf-error">{{ errors.issued_at }}</span>
    </label>
    <label class="pf-field">
      <span>{{ $t('passport.fields.divisionCode') }}</span>
      <input
        :value="modelValue.division_code"
        class="pf-input"
        :class="{ invalid: errors.division_code }"
        placeholder="770-001"
        maxlength="7"
        :disabled="disabled"
        @input="set('division_code', ($event.target as HTMLInputElement).value)"
      />
      <span v-if="errors.division_code" class="pf-error">{{ errors.division_code }}</span>
    </label>
    <label class="pf-field wide">
      <span>{{ $t('passport.fields.issuedBy') }}</span>
      <input
        :value="modelValue.issued_by"
        class="pf-input"
        :disabled="disabled"
        @input="set('issued_by', ($event.target as HTMLInputElement).value)"
      />
    </label>
  </div>
</template>

<script lang="ts">
import { defineComponent, type PropType } from 'vue'
import type { PassportData } from '../../api/passport'

// Поля паспорта — одни на регистрацию, профиль, верификацию и админку.
// Обязательны серия, номер и дата выдачи; остальное — по желанию. Проверяет
// сервер, ошибки приходят по полям.
export default defineComponent({
  name: 'PassportFields',
  props: {
    modelValue: { type: Object as PropType<PassportData>, required: true },
    errors: { type: Object as PropType<Record<string, string>>, default: () => ({}) },
    disabled: { type: Boolean, default: false },
  },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    const set = (key: keyof PassportData, value: string) => emit('update:modelValue', { ...props.modelValue, [key]: value })
    return { set }
  },
})
</script>

<style scoped>
.passport-fields {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 10px;
}
.pf-field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 12px;
  color: #475569;
}
.pf-field.wide {
  grid-column: 1 / -1;
}
.pf-input {
  border: 1px solid #e2e8f0;
  border-radius: 10px;
  padding: 9px 10px;
  font-size: 14px;
  color: #0f172a;
  background: #fff;
}
.pf-input.invalid {
  border-color: #ef4444;
}
.pf-error {
  color: #dc2626;
  font-size: 12px;
}
</style>
