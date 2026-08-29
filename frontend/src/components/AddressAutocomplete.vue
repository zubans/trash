<template>
  <div class="address-field" ref="root">
    <label v-if="label" class="address-label">{{ label }}</label>

    <div class="address-input-wrap">
      <input
        ref="input"
        type="text"
        class="address-input"
        :class="{ 'is-chosen': isChosen }"
        :placeholder="placeholder"
        :disabled="disabled"
        :value="query"
        autocomplete="off"
        autocapitalize="off"
        spellcheck="false"
        @input="onInput"
        @focus="onFocus"
        @keydown.down.prevent="move(1)"
        @keydown.up.prevent="move(-1)"
        @keydown.enter.prevent="onEnter"
        @keydown.esc="close"
      />
      <i class="ph ph-map-pin address-icon"></i>
      <span v-if="loading" class="address-spinner" />
      <button
        v-else-if="query"
        type="button"
        class="address-clear"
        :aria-label="'Очистить адрес'"
        @click="clear"
      >
        <i class="ph-bold ph-x"></i>
      </button>
    </div>

    <!-- The apartment lives in the same field: picking a building leaves the
         address one step short, and this finishes it in place rather than
         sending the person to a second box somewhere else on the form. -->
    <div v-if="isChosen && !hasFlat && needsFlat" class="flat-row">
      <span class="flat-prefix">кв./офис</span>
      <input
        type="text"
        class="flat-input"
        :placeholder="flatPlaceholder"
        :disabled="disabled"
        v-model="flat"
        maxlength="12"
        @input="onFlatInput"
      />
    </div>

    <div v-if="open && suggestions.length > 0" class="address-dropdown">
      <button
        v-for="(item, index) in suggestions"
        :key="item.fias_id || item.value || index"
        type="button"
        class="address-option"
        :class="{ 'is-active': index === activeIndex }"
        @mousedown.prevent="choose(item)"
        @mouseenter="activeIndex = index"
      >
        <i class="ph ph-map-pin option-icon"></i>
        <span class="option-text">{{ item.value }}</span>
      </button>
    </div>

    <p v-if="errorText" class="address-error">{{ errorText }}</p>
    <p v-else-if="hint" class="address-hint">{{ hint }}</p>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, computed, watch, onMounted, onUnmounted, PropType } from 'vue'
import api from '../services/api'

/** A postal address kept as its parts, exactly as the backend stores it. */
export interface StructuredAddress {
  value: string
  region?: string
  city?: string
  street?: string
  house?: string
  flat?: string
  fias_id?: string
  lat?: number
  lon?: number
  source?: string
}

/**
 * A single address field backed by the address register.
 *
 * It replaces the previous arrangement, where the address was typed into a
 * plain box, matched against a fixed spelling, and the flat was a separate
 * input somewhere else on the form. The parts come back from the provider and
 * are passed on as parts, so a building like "12 к. 1" is data rather than a
 * format error, and the coordinates arrive with the pick instead of needing a
 * second lookup.
 */
export default defineComponent({
  name: 'AddressAutocomplete',
  props: {
    modelValue: { type: Object as PropType<StructuredAddress | null>, default: null },
    label: { type: String, default: '' },
    placeholder: { type: String, default: 'Начните вводить адрес' },
    hint: { type: String, default: '' },
    flatPlaceholder: { type: String, default: '101' },
    disabled: { type: Boolean, default: false },
    /** Ask for the apartment once a building has been chosen. */
    needsFlat: { type: Boolean, default: true },
  },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    const root = ref<HTMLElement | null>(null)
    const input = ref<HTMLInputElement | null>(null)

    const query = ref(props.modelValue?.value || '')
    const flat = ref(props.modelValue?.flat || '')
    const suggestions = ref<StructuredAddress[]>([])
    const chosen = ref<StructuredAddress | null>(props.modelValue)
    const loading = ref(false)
    const open = ref(false)
    const activeIndex = ref(-1)
    const errorText = ref('')

    let debounce: ReturnType<typeof setTimeout> | undefined
    let sequence = 0

    const isChosen = computed(() => chosen.value !== null)
    const hasFlat = computed(() => !!chosen.value?.flat)

    /** Rebuilds the one-line form so the field shows what will be submitted. */
    const compose = (addr: StructuredAddress, withFlat: string): string => {
      const parts = [addr.city, addr.street].filter((p) => p && p.trim())
      if (addr.house && addr.house.trim()) parts.push(`д. ${addr.house.trim()}`)
      if (withFlat && withFlat.trim()) parts.push(`кв. ${withFlat.trim()}`)
      return parts.length > 0 ? parts.join(', ') : addr.value
    }

    const publish = () => {
      if (!chosen.value) {
        emit('update:modelValue', null)
        return
      }
      const withFlat: StructuredAddress = {
        ...chosen.value,
        flat: chosen.value.flat || flat.value.trim() || undefined,
      }
      withFlat.value = compose(withFlat, withFlat.flat || '')
      emit('update:modelValue', withFlat)
    }

    const search = async (text: string) => {
      const mine = ++sequence
      loading.value = true
      errorText.value = ''
      try {
        const res = await api.get('/geo/suggest', { params: { q: text, count: 7 } })
        // A slower earlier request must not overwrite a newer answer.
        if (mine !== sequence) return
        suggestions.value = res.data || []
        open.value = suggestions.value.length > 0
        activeIndex.value = -1
      } catch (err: any) {
        if (mine !== sequence) return
        suggestions.value = []
        open.value = false
        // Say what happened. The previous version logged this to the console
        // and left an empty dropdown, which reads as "your street is unknown".
        const status = err?.response?.status
        if (status === 503) {
          errorText.value = 'Подсказки адресов сейчас недоступны. Сообщите в поддержку.'
        } else if (status === 429) {
          errorText.value = 'Слишком много запросов, повторите через несколько секунд.'
        } else {
          errorText.value = 'Не удалось загрузить подсказки. Проверьте соединение.'
        }
      } finally {
        if (mine === sequence) loading.value = false
      }
    }

    const onInput = (event: Event) => {
      query.value = (event.target as HTMLInputElement).value
      // Editing the text invalidates the pick: what is in the box is no longer
      // the address that was chosen from the register.
      chosen.value = null
      flat.value = ''
      emit('update:modelValue', null)

      clearTimeout(debounce)
      const text = query.value.trim()
      if (text.length < 3) {
        suggestions.value = []
        open.value = false
        loading.value = false
        errorText.value = ''
        return
      }
      loading.value = true
      debounce = setTimeout(() => search(text), 350)
    }

    const choose = (item: StructuredAddress) => {
      chosen.value = item
      flat.value = item.flat || ''
      query.value = compose(item, flat.value)
      suggestions.value = []
      open.value = false
      activeIndex.value = -1
      errorText.value = ''
      publish()
    }

    const onFlatInput = () => {
      if (!chosen.value) return
      query.value = compose(chosen.value, flat.value)
      publish()
    }

    const move = (delta: number) => {
      if (!open.value || suggestions.value.length === 0) return
      const next = activeIndex.value + delta
      activeIndex.value = (next + suggestions.value.length) % suggestions.value.length
    }

    const onEnter = () => {
      if (open.value && activeIndex.value >= 0) {
        choose(suggestions.value[activeIndex.value])
      }
    }

    const onFocus = () => {
      if (suggestions.value.length > 0) open.value = true
    }

    const close = () => {
      open.value = false
      activeIndex.value = -1
    }

    const clear = () => {
      query.value = ''
      flat.value = ''
      chosen.value = null
      suggestions.value = []
      open.value = false
      errorText.value = ''
      emit('update:modelValue', null)
      input.value?.focus()
    }

    const onDocumentClick = (event: MouseEvent) => {
      if (root.value && !root.value.contains(event.target as Node)) close()
    }

    onMounted(() => document.addEventListener('click', onDocumentClick))
    onUnmounted(() => {
      document.removeEventListener('click', onDocumentClick)
      clearTimeout(debounce)
    })

    // Keep the field in step with the bound value when the parent drives it:
    // clearing it resets the box, and seeding a saved address (e.g. when editing
    // an existing one) fills the box so it can be adjusted in place. Composed
    // values echoed back by our own publish() match the current query and are
    // ignored, so this never loops.
    watch(
      () => props.modelValue,
      (next) => {
        if (next === null) {
          if (chosen.value !== null) {
            query.value = ''
            flat.value = ''
            chosen.value = null
          }
          return
        }
        if (next.value && next.value !== query.value) {
          chosen.value = next
          flat.value = next.flat || ''
          query.value = next.value
        }
      },
    )

    return {
      root,
      input,
      query,
      flat,
      suggestions,
      loading,
      open,
      activeIndex,
      errorText,
      isChosen,
      hasFlat,
      onInput,
      onFocus,
      onFlatInput,
      choose,
      move,
      onEnter,
      close,
      clear,
    }
  },
})
</script>

<style scoped>
.address-field {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.address-label {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-secondary, #64748b);
}

.address-input-wrap {
  position: relative;
  display: flex;
  align-items: center;
}

.address-input {
  width: 100%;
  padding: 12px 40px 12px 40px;
  border: 1px solid var(--border-color, #e2e8f0);
  border-radius: 12px;
  font-size: 15px;
  background: var(--surface, #ffffff);
  color: var(--text-primary, #0f172a);
}

.address-input:focus {
  outline: none;
  border-color: #5c60f5;
  box-shadow: 0 0 0 3px rgba(92, 96, 245, 0.12);
}

/* A chosen address is a different state from typed text: it is the one that
   will actually be submitted. */
.address-input.is-chosen {
  border-color: #16a34a;
}

.address-icon {
  position: absolute;
  left: 14px;
  font-size: 18px;
  color: #94a3b8;
  pointer-events: none;
}

.address-spinner {
  position: absolute;
  right: 14px;
  width: 16px;
  height: 16px;
  border: 2px solid #cbd5e1;
  border-top-color: #5c60f5;
  border-radius: 50%;
  animation: address-spin 0.7s linear infinite;
}

@keyframes address-spin {
  to { transform: rotate(360deg); }
}

@media (prefers-reduced-motion: reduce) {
  .address-spinner { animation-duration: 2s; }
}

.address-clear {
  position: absolute;
  right: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border: none;
  border-radius: 50%;
  background: #f1f5f9;
  color: #64748b;
  cursor: pointer;
}

.address-clear:hover { background: #e2e8f0; }

.flat-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border: 1px dashed var(--border-color, #cbd5e1);
  border-radius: 12px;
  background: #f8fafc;
}

.flat-prefix {
  font-size: 13px;
  font-weight: 600;
  color: #64748b;
  white-space: nowrap;
}

.flat-input {
  flex: 1;
  min-width: 0;
  border: none;
  background: transparent;
  font-size: 15px;
  color: var(--text-primary, #0f172a);
}

.flat-input:focus { outline: none; }

.address-dropdown {
  position: absolute;
  top: 100%;
  left: 0;
  right: 0;
  z-index: 40;
  margin-top: 4px;
  max-height: 280px;
  overflow-y: auto;
  border: 1px solid var(--border-color, #e2e8f0);
  border-radius: 12px;
  background: var(--surface, #ffffff);
  box-shadow: 0 12px 24px -8px rgba(15, 23, 42, 0.18);
}

.address-option {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 11px 14px;
  border: none;
  background: transparent;
  text-align: left;
  font-size: 14px;
  color: var(--text-primary, #0f172a);
  cursor: pointer;
}

.address-option.is-active,
.address-option:hover { background: #eef2ff; }

.option-icon { font-size: 16px; color: #94a3b8; flex-shrink: 0; }
.option-text { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.address-error { margin: 0; font-size: 12px; color: #dc2626; }
.address-hint { margin: 0; font-size: 12px; color: var(--text-secondary, #64748b); }
</style>
