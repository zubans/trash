<template>
  <div class="server-status" title="Server status">
    <span class="status-dot" :class="{ online: isOnline, offline: !isOnline }"></span>
    <button
      v-if="updateAvailable"
      class="update-badge"
      :title="$t('app.updateAvailable', { version: versionName })"
      @click="installUpdate"
    >
      <span class="update-icon">⬆</span>
    </button>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, onMounted, onUnmounted } from 'vue'
import axios from 'axios'
import { apiUrl } from '../services/api'
import { useAppUpdate } from '../composables/useAppUpdate'

export default defineComponent({
  name: 'ServerStatusIndicator',
  setup() {
    const isOnline = ref(false)
    let intervalId: number | null = null

    const {
      updateAvailable,
      versionName,
      installUpdate,
    } = useAppUpdate()

    const checkHealth = async () => {
      try {
        await axios.get(`${apiUrl}/health`, { timeout: 5000 })
        isOnline.value = true
      } catch (e) {
        isOnline.value = false
      }
    }

    onMounted(() => {
      checkHealth()
      intervalId = window.setInterval(checkHealth, 10000)
    })

    onUnmounted(() => {
      if (intervalId !== null) {
        clearInterval(intervalId)
      }
    })

    return {
      isOnline,
      updateAvailable,
      versionName,
      installUpdate,
    }
  },
})
</script>

<style scoped>
.server-status {
  position: fixed;
  top: 16px;
  left: 16px;
  z-index: 9999;
  display: flex;
  align-items: center;
  gap: 8px;
}

.status-dot {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  display: inline-block;
  box-shadow: 0 0 4px rgba(0, 0, 0, 0.3);
}

.status-dot.online {
  background-color: #22c55e;
}

.status-dot.offline {
  background-color: #ef4444;
}

.update-badge {
  width: 22px;
  height: 22px;
  border-radius: 50%;
  border: none;
  background-color: #f59e0b;
  color: white;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  box-shadow: 0 0 4px rgba(0, 0, 0, 0.3);
  animation: pulse 1.5s infinite;
}

.update-badge:hover {
  background-color: #d97706;
}

.update-icon {
  font-size: 12px;
  line-height: 1;
}

@keyframes pulse {
  0% {
    box-shadow: 0 0 0 0 rgba(245, 158, 11, 0.7);
  }
  70% {
    box-shadow: 0 0 0 8px rgba(245, 158, 11, 0);
  }
  100% {
    box-shadow: 0 0 0 0 rgba(245, 158, 11, 0);
  }
}
</style>
