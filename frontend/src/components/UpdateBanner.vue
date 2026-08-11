<template>
  <div>
    <!-- Force update: modal overlay, cannot be dismissed -->
    <div
      v-if="showForceUpdate"
      class="force-update-overlay"
    >
      <div class="force-update-dialog">
        <h5 class="mb-2">
          {{ $t('app.updateRequired') }}
        </h5>
        <p class="version mb-2">
          {{ $t('app.updateAvailable', { version: versionName }) }}
        </p>
        <p
          v-if="releaseNotes"
          class="release-notes"
        >
          {{ releaseNotes }}
        </p>
        <va-button
          color="primary"
          :loading="installing"
          block
          @click="install"
        >
          {{ $t('app.installUpdate') }}
        </va-button>
      </div>
    </div>

    <!-- Regular update: dismissible banner -->
    <div
      v-else-if="showBanner"
      class="update-banner mb-3"
    >
      <va-alert
        color="info"
        class="m-0"
        closeable
        @dismissed="dismissed = true"
      >
        <div class="d-flex align-items-center justify-content-between flex-wrap gap-2">
          <div>
            <div>
              {{ $t('app.updateAvailable', { version: versionName }) }}
            </div>
            <div
              v-if="releaseNotes"
              class="release-notes-small"
            >
              {{ releaseNotes }}
            </div>
          </div>
          <va-button
            color="primary"
            size="small"
            :loading="installing"
            @click="install"
          >
            {{ $t('app.installUpdate') }}
          </va-button>
        </div>
      </va-alert>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, computed } from 'vue'
import { useAppUpdate } from '../composables/useAppUpdate'

export default defineComponent({
  name: 'UpdateBanner',
  setup() {
    const {
      updateAvailable,
      forceUpdate,
      versionName,
      releaseNotes,
      installUpdate,
    } = useAppUpdate()

    const dismissed = ref(false)
    const installing = ref(false)

    const showForceUpdate = computed(() => updateAvailable.value && forceUpdate.value)
    const showBanner = computed(() => updateAvailable.value && !forceUpdate.value && !dismissed.value)

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
      installing,
      install,
    }
  },
})
</script>

<style scoped>
.update-banner {
  width: 100%;
}

.force-update-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.7);
  z-index: 9999;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 1rem;
}

.force-update-dialog {
  background: white;
  border-radius: 8px;
  padding: 1.5rem;
  max-width: 400px;
  width: 100%;
  text-align: center;
}

.release-notes {
  font-size: 0.875rem;
  color: #666;
  margin-bottom: 1rem;
  white-space: pre-line;
}

.release-notes-small {
  font-size: 0.75rem;
  color: #666;
  margin-top: 0.25rem;
  white-space: pre-line;
}

.version {
  font-size: 0.9rem;
  color: #444;
}
</style>
