<template>
  <div class="language-switcher">
    <button
      v-for="loc in AVAILABLE_LOCALES"
      :key="loc.code"
      type="button"
      :class="['lang-btn', { active: currentLocale === loc.code }]"
      @click="selectLocale(loc.code)"
    >
      {{ loc.label }}
    </button>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { setLocale } from '../i18n'

export default defineComponent({
  name: 'LanguageSwitcher',
  setup() {
    const { locale } = useI18n()
    const currentLocale = ref(locale.value)

    const AVAILABLE_LOCALES = [
      { code: 'ru', label: 'RU' },
      { code: 'en', label: 'EN' },
    ]

    const selectLocale = (code: string) => {
      if (code && code !== locale.value) {
        setLocale(code)
        currentLocale.value = code
      }
    }

    return {
      currentLocale,
      AVAILABLE_LOCALES,
      selectLocale,
    }
  },
})
</script>

<style scoped>
.language-switcher {
  display: inline-flex;
  align-items: center;
  background: rgba(255, 255, 255, 0.8);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  border: 1px solid rgba(0, 0, 0, 0.08);
  border-radius: 20px;
  padding: 2px;
  gap: 2px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.04);
}

.lang-btn {
  border: none;
  background: transparent;
  color: #64748b;
  font-weight: 600;
  font-size: 12px;
  padding: 4px 10px;
  border-radius: 16px;
  cursor: pointer;
  transition: all 0.2s cubic-bezier(0.16, 1, 0.3, 1);
  line-height: 1;
}

.lang-btn:hover {
  color: #0f172a;
}

.lang-btn.active {
  background: #6366f1;
  color: #ffffff;
  box-shadow: 0 2px 6px rgba(99, 102, 241, 0.3);
}
</style>
