<template>
  <div class="language-switcher">
    <va-select
      v-model="currentLocale"
      :options="AVAILABLE_LOCALES"
      text-by="name"
      value-by="code"
      :label="$t('language')"
      dense
      style="width: 140px"
      @update:modelValue="onChange"
    />
  </div>
</template>

<script lang="ts">
import { defineComponent, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { AVAILABLE_LOCALES, setLocale } from '../i18n'

export default defineComponent({
  name: 'LanguageSwitcher',
  setup() {
    const { locale } = useI18n()
    const currentLocale = ref(locale.value)

    const onChange = (value: any) => {
      const code = typeof value === 'object' ? value.code : value
      if (code && code !== locale.value) {
        setLocale(code)
        currentLocale.value = code
      }
    }

    return {
      currentLocale,
      AVAILABLE_LOCALES,
      onChange,
    }
  },
})
</script>

<style scoped>
.language-switcher {
  display: inline-flex;
  align-items: center;
}
</style>
