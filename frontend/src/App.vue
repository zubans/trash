<template>
  <ServerStatusIndicator />
  <UpdateBanner />
  <router-view />
  <AppVersionFooter />
</template>

<script lang="ts">
import { defineComponent, onMounted, onUnmounted } from 'vue'
import ServerStatusIndicator from './components/ServerStatusIndicator.vue'
import UpdateBanner from './components/UpdateBanner.vue'
import AppVersionFooter from './components/AppVersionFooter.vue'
import { useAuthStore } from './stores/auth-store'
import api from './services/api'

export default defineComponent({
  name: 'App',
  components: { ServerStatusIndicator, UpdateBanner, AppVersionFooter },
  setup() {
    const authStore = useAuthStore()

    const loadCurrency = async () => {
      try {
        const response = await api.get('/settings')
        if (response.data?.currency) {
          authStore.setCurrency(response.data.currency)
        }
      } catch (err) {
        console.error('Failed to load public settings:', err)
      }
    }

    // Swipe gesture: left-to-right swipe near left edge to navigate back
    let touchStartX = 0
    let touchStartY = 0

    const handleTouchStart = (e: TouchEvent) => {
      if (e.touches.length === 1) {
        touchStartX = e.touches[0].clientX
        touchStartY = e.touches[0].clientY
      }
    }

    const handleTouchEnd = (e: TouchEvent) => {
      if (e.changedTouches.length === 1) {
        const touchEndX = e.changedTouches[0].clientX
        const touchEndY = e.changedTouches[0].clientY
        const deltaX = touchEndX - touchStartX
        const deltaY = Math.abs(touchEndY - touchStartY)

        // Trigger back navigation if:
        // 1. Swipe starts near left edge (first 100px of screen)
        // 2. Horizontal swipe distance > 70px
        // 3. Vertical drift is small (< 50px)
        if (touchStartX <= 100 && deltaX > 70 && deltaY < 50) {
          if (window.history.length > 1) {
            window.history.back()
          }
        }
      }
    }

    onMounted(() => {
      loadCurrency()
      window.addEventListener('touchstart', handleTouchStart, { passive: true })
      window.addEventListener('touchend', handleTouchEnd, { passive: true })
    })

    onUnmounted(() => {
      window.removeEventListener('touchstart', handleTouchStart)
      window.removeEventListener('touchend', handleTouchEnd)
    })
  },
})
</script>

<style>
body {
  margin: 0;
  font-family: 'Source Sans Pro', sans-serif;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
}
</style>
