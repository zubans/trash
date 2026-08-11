<template>
  <ServerStatusIndicator />
  <UpdateBanner />
  <router-view />
  <AppVersionFooter />
</template>

<script lang="ts">
import { defineComponent, onMounted } from 'vue'
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

    onMounted(() => {
      loadCurrency()
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
