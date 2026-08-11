<template>
  <div class="app-version-footer">
    <span v-if="version">v{{ version }}</span>
    <span v-if="build">(build {{ build }})</span>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, onMounted } from 'vue'
import { Capacitor } from '@capacitor/core'
import { AppUpdate } from '../plugins/app-update'

export default defineComponent({
  name: 'AppVersionFooter',
  setup() {
    const version = ref<string | null>(null)
    const build = ref<number | null>(null)

    onMounted(async () => {
      try {
        if (Capacitor.isNativePlatform()) {
          const info = await AppUpdate.getCurrentVersion()
          version.value = info.versionName
          build.value = info.versionCode
        } else {
          version.value = 'web'
          build.value = 0
        }
      } catch (err) {
        console.error('Failed to get app version:', err)
        version.value = 'unknown'
      }
    })

    return {
      version,
      build,
    }
  },
})
</script>

<style scoped>
.app-version-footer {
  position: fixed;
  bottom: 4px;
  left: 0;
  right: 0;
  text-align: center;
  font-size: 11px;
  color: rgba(0, 0, 0, 0.4);
  pointer-events: none;
  z-index: 100;
}

.app-version-footer span + span {
  margin-left: 4px;
}
</style>
