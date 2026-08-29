<template>
  <div class="sidebar-utilities">
    <button class="util-item" title="Поддержка" @click="$emit('openSupport')">
      <i class="ph ph-headset"></i>
      <span v-if="!minimized">{{ $t('app.support') }}</span>
    </button>

    <div class="util-item language-wrap">
      <button title="Язык" @click="$emit('toggleLanguage')">
        <i class="ph ph-globe"></i>
        <span v-if="!minimized">{{ currentLang }}</span>
      </button>
    </div>

    <button class="util-item logout" title="Выход" @click="$emit('logout')">
      <i class="ph ph-sign-out"></i>
      <span v-if="!minimized">{{ $t('app.logout') }}</span>
    </button>
  </div>
</template>

<script lang="ts">
import { defineComponent, computed } from 'vue'
import { useI18n } from '../i18n'

export default defineComponent({
  name: 'SidebarUtilities',
  props: { minimized: { type: Boolean, default: false } },
  setup() {
    const { locale } = useI18n()
    const currentLang = computed(() => locale.value.toUpperCase())
    return { currentLang }
  }
})
</script>

<style scoped>
.sidebar-utilities { display:flex; flex-direction:column; gap:8px; margin-top:auto; }
.util-item { display:flex; align-items:center; gap:10px; padding:10px; border-radius:10px; background:transparent; border:none; color:var(--text-muted); cursor:pointer; }
.util-item i { font-size:18px }
.util-item.logout { color: var(--danger-main); }
.language-wrap { display:flex }
</style>
