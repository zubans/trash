<template>
  <div>
    <!-- Принудительное обновление: модальный оверлей, закрыть нельзя -->
    <div
      v-if="showForceUpdate"
      class="force-update-overlay"
    >
      <div class="force-update-dialog">
        <div class="force-update-logo-wrapper mb-3">
          <AppLogo :hide-text="true" />
        </div>
        <h5 class="mb-2">
          {{ $t('app.updateRequired') }}
        </h5>
        <p class="version mb-2">
          {{ $t('app.updateAvailable') }}
        </p>
        <p
          v-if="releaseNotes"
          class="release-notes"
        >
          {{ releaseNotes }}
        </p>

        <!-- Индикатор прогресса при загрузке -->
        <div v-if="installing || isDownloading" class="progress-section my-3">
          <va-progress-bar :model-value="downloadProgress" color="primary" class="mb-1" />
          <div class="d-flex justify-content-between text-xs text-secondary">
            <span>{{ formattedDownloaded }} / {{ formattedTotal }}</span>
            <span>{{ downloadProgress }}%</span>
          </div>
        </div>

        <va-button
          v-else
          color="primary"
          block
          @click="install"
        >
          {{ $t('app.installUpdate') }}
        </va-button>
      </div>
    </div>

    <!-- Обычное обновление: закрываемый баннер -->
    <div
      v-else-if="showBanner"
      class="update-banner-bar"
    >
      <div class="banner-content">
        <div class="banner-icon">
          <AppLogo :hide-text="true" :compact="true" />
        </div>
        <div class="banner-text-box">
          <div class="banner-title">
            {{ $t('app.updateAvailable') }}
          </div>
          <div v-if="releaseNotes" class="banner-notes">
            {{ releaseNotes }}
          </div>

          <!-- Полоса прогресса в баннере -->
          <div v-if="installing || isDownloading" class="progress-section mt-2">
            <div class="progress-bar-bg">
              <div class="progress-bar-fill" :style="{ width: downloadProgress + '%' }"></div>
            </div>
            <div class="progress-meta">
              <span>{{ formattedDownloaded }} / {{ formattedTotal }}</span>
              <span>{{ downloadProgress }}%</span>
            </div>
          </div>
        </div>

        <button
          v-if="!installing && !isDownloading"
          type="button"
          class="btn-update-action"
          @click="install"
        >
          <i class="ph-bold ph-download-simple"></i>
          <span>{{ $t('app.installUpdate') }}</span>
        </button>

        <button
          type="button"
          class="btn-dismiss"
          title="Закрыть"
          @click="dismiss"
        >
          <i class="ph ph-x"></i>
        </button>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, computed } from 'vue'
import { useAppUpdate } from '../composables/useAppUpdate'
import AppLogo from './AppLogo.vue'

export default defineComponent({
  name: 'UpdateBanner',
  components: { AppLogo },

  setup() {
    const {
      updateAvailable,
      forceUpdate,
      versionName,
      releaseNotes,
      downloadProgress,
      bytesDownloaded,
      totalBytes,
      isDownloading,
      dismissUpdate,
      installUpdate,
    } = useAppUpdate()

    const dismissed = ref(false)
    const installing = ref(false)

    const showForceUpdate = computed(() => updateAvailable.value && forceUpdate.value)
    const showBanner = computed(() => updateAvailable.value && !forceUpdate.value && !dismissed.value)

    const dismiss = () => {
      dismissed.value = true
      dismissUpdate()
    }

    const formatSize = (bytes: number) => {
      if (!bytes || bytes <= 0) return '0 MB'
      const mb = bytes / (1024 * 1024)
      return `${mb.toFixed(1)} MB`
    }

    const formattedDownloaded = computed(() => formatSize(bytesDownloaded.value))
    const formattedTotal = computed(() => formatSize(totalBytes.value))

    const install = async () => {
      installing.value = true
      try {
        await installUpdate()
      } finally {
        installing.value = false
      }
    }

    return {
      showForceUpdate,
      showBanner,
      versionName,
      releaseNotes,
      dismissed,
      installing,
      isDownloading,
      downloadProgress,
      formattedDownloaded,
      formattedTotal,
      install,
      dismiss,
    }
  },
})
</script>

<style scoped>
.update-banner-bar {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  z-index: 950;
  margin: 12px 16px;
  background: rgba(255, 255, 255, 0.85);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  border: 1px solid rgba(99, 102, 241, 0.2);
  border-radius: 16px;
  padding: 12px 16px;
  box-shadow: 0 8px 24px -4px rgba(99, 102, 241, 0.12), 0 2px 6px rgba(0,0,0,0.04);
  transition: all 0.3s ease;
}

.banner-content {
  display: flex;
  align-items: center;
  gap: 12px;
}

.banner-icon {
  width: 40px;
  height: 40px;
  border-radius: 12px;
  background: #ffffff;
  border: 1px solid rgba(92, 96, 245, 0.15);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.04);
  padding: 4px;
}

.banner-text-box {
  flex: 1;
  min-width: 0;
}

.banner-title {
  font-size: 14px;
  font-weight: 600;
  color: #0f172a;
  line-height: 1.3;
}

.banner-notes {
  font-size: 12px;
  color: #64748b;
  margin-top: 2px;
  white-space: pre-line;
}

.btn-update-action {
  background: linear-gradient(135deg, #6366f1 0%, #4f46e5 100%);
  color: #ffffff;
  border: none;
  border-radius: 10px;
  padding: 8px 14px;
  font-size: 13px;
  font-weight: 600;
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  white-space: nowrap;
  box-shadow: 0 4px 12px rgba(99, 102, 241, 0.25);
  transition: all 0.2s ease;
}

.btn-update-action:hover {
  transform: translateY(-1px);
  box-shadow: 0 6px 16px rgba(99, 102, 241, 0.35);
}

.btn-dismiss {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  border: none;
  background: rgba(0,0,0,0.04);
  color: #64748b;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
  cursor: pointer;
  transition: all 0.2s ease;
}

.btn-dismiss:hover {
  background: rgba(0,0,0,0.08);
  color: #0f172a;
}

.progress-bar-bg {
  width: 100%;
  height: 6px;
  background: rgba(99, 102, 241, 0.15);
  border-radius: 9999px;
  overflow: hidden;
}

.progress-bar-fill {
  height: 100%;
  background: linear-gradient(90deg, #6366f1, #3b82f6);
  border-radius: 9999px;
  transition: width 0.2s ease;
}

.progress-meta {
  display: flex;
  justify-content: space-between;
  font-size: 11px;
  color: #64748b;
  margin-top: 4px;
}

.force-update-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(15, 23, 42, 0.6);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  z-index: 9999;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
}

.force-update-dialog {
  background: rgba(255, 255, 255, 0.95);
  border-radius: 24px;
  padding: 28px;
  max-width: 380px;
  width: 100%;
  text-align: center;
  box-shadow: 0 20px 50px -10px rgba(15, 23, 42, 0.2);
  border: 1px solid rgba(255,255,255,0.8);
}

.release-notes {
  font-size: 13px;
  color: #64748b;
  margin-bottom: 16px;
  white-space: pre-line;
}

.version {
  font-size: 14px;
  font-weight: 500;
  color: #334155;
}

@media (max-width: 576px) {
  .banner-content {
    flex-wrap: wrap;
  }
  .btn-update-action {
    width: 100%;
    justify-content: center;
  }
}
</style>
