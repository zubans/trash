<template>
  <div class="server-status" title="Server status">
    <span class="status-dot" :class="{ online: isOnline, offline: !isOnline }"></span>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, onMounted, onUnmounted } from 'vue'
import axios from 'axios'
import { apiUrl } from '../services/api'

export default defineComponent({
  name: 'ServerStatusIndicator',
  setup() {
    const isOnline = ref(false)
    let intervalId: number | null = null

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
    }
  },
})
</script>

<style scoped>
.server-status {
  position: fixed;
  top: 16px;
  right: 16px;
  z-index: 9999;
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
</style>
