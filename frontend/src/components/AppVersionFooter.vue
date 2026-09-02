<template>
  <div class="app-version-footer">
    <!-- Семь нажатий на версию переключают отладочную консоль (см. registerTap). -->
    <span class="version-tap" @click="registerTap" v-if="version">v{{ version }}</span>
    <span class="version-tap" @click="registerTap" v-if="build">(build {{ build }})</span>
    <span v-if="tapHint" class="tap-hint">{{ tapHint }}</span>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, onMounted } from 'vue'
import { Capacitor } from '@capacitor/core'
import { AppUpdate } from '../plugins/app-update'
import { toggleDebugConsole, debugConsoleEnabled } from '../services/debugLog'

export default defineComponent({
  name: 'AppVersionFooter',
  setup() {
    const version = ref<string | null>(null)
    const build = ref<number | null>(null)

    // Скрытый жест: 7 быстрых нажатий на версию переключают отладочную консоль. Это
    // единственный способ включить логирование запросов на уже установленной
    // продовой сборке (которая собрана без VITE_DEBUG).
    const tapHint = ref('')
    let taps = 0
    let tapTimer: ReturnType<typeof setTimeout> | undefined

    const registerTap = () => {
      taps += 1
      clearTimeout(tapTimer)
      tapTimer = setTimeout(() => {
        taps = 0
      }, 800)
      if (taps >= 7) {
        taps = 0
        toggleDebugConsole()
        tapHint.value = debugConsoleEnabled.value ? 'Debug ON' : 'Debug OFF'
        setTimeout(() => {
          tapHint.value = ''
        }, 1500)
      }
    }

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
      tapHint,
      registerTap,
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

/* Номера версий нажимаемы (скрытый отладочный жест), хотя сам футер игнорирует
   события указателя, чтобы никогда не перекрывать интерфейс под собой. */
.version-tap {
  pointer-events: auto;
  cursor: default;
  -webkit-user-select: none;
  user-select: none;
}

.tap-hint {
  margin-left: 6px;
  color: #6366f1;
  font-weight: 700;
}
</style>
