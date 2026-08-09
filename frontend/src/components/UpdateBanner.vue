<template>
  <div v-if="updateAvailable" class="update-banner mb-3">
    <va-alert color="info" class="m-0" closeable @dismissed="dismissed = true">
      <div class="d-flex align-items-center justify-content-between flex-wrap gap-2">
        <span>
          {{ $t('app.updateAvailable', { version: versionName }) }}
        </span>
        <va-button color="primary" size="small" :loading="installing" @click="install">
          {{ $t('app.installUpdate') }}
        </va-button>
      </div>
    </va-alert>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, computed } from 'vue'
import { useAppUpdate } from '../composables/useAppUpdate'

export default defineComponent({
  name: 'UpdateBanner',
  setup() {
    const { updateAvailable, versionName, installUpdate } = useAppUpdate()
    const dismissed = ref(false)
    const installing = ref(false)

    const visible = computed(() => updateAvailable.value && !dismissed.value)

    const install = async () => {
      installing.value = true
      try {
        await installUpdate()
      } finally {
        installing.value = false
      }
    }

    return {
      updateAvailable: visible,
      versionName,
      installing,
      install,
      dismissed,
    }
  },
})
</script>

<style scoped>
.update-banner {
  width: 100%;
}
</style>
