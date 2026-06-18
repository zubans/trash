<template>
  <div class="executor-dashboard">
    <div class="dashboard-header mb-5">
      <div class="d-flex justify-content-between align-items-center">
        <div>
          <h1 class="va-h3 m-0">Executor Dashboard</h1>
          <span class="text-secondary text-sm">Welcome back to your shift panel</span>
        </div>
        <va-button color="danger" outline size="small" @click="handleLogout">
          <va-icon name="logout" class="mr-2" /> Logout
        </va-button>
      </div>
    </div>

    <va-card class="p-4 shadow-card text-center" style="max-width: 500px; margin: 0 auto;">
      <va-icon name="pedal_bike" size="large" color="primary" class="mb-4" />
      <h3 class="va-h4 mb-2">Shift Controls</h3>
      <p class="text-secondary mb-4">
        Shift logs, active order tracking, and GPS telemetry will be launched in Stage 3.
      </p>
      
      <div class="profile-summary p-3 bg-light rounded">
        <p class="mb-2"><strong>Phone:</strong> {{ phone }}</p>
        <p class="m-0"><strong>Role:</strong> {{ role }}</p>
      </div>
    </va-card>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../../stores/auth-store'
import api from '../../services/api'

export default defineComponent({
  name: 'ExecutorDashboard',
  setup() {
    const router = useRouter()
    const authStore = useAuthStore()

    const phone = ref(authStore.phone)
    const role = ref(authStore.role)

    const handleLogout = async () => {
      try {
        await api.post('/logout')
      } catch (e) {
        console.error('Logout error blacklisting token', e)
      } finally {
        authStore.logout()
        router.push('/login')
      }
    }

    return {
      phone,
      role,
      handleLogout,
    }
  },
})
</script>

<style scoped>
.executor-dashboard {
  max-width: 900px;
  margin: 40px auto;
  padding: 0 20px;
}
.shadow-card {
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.05);
  border-radius: 12px !important;
}
.bg-light {
  background-color: #f8fafc;
}
.rounded {
  border-radius: 8px;
}
.d-flex {
  display: flex;
}
.justify-content-between {
  justify-content: space-between;
}
.align-items-center {
  align-items: center;
}
.mr-2 {
  margin-right: 8px;
}
.mb-4 {
  margin-bottom: 16px;
}
.mb-2 {
  margin-bottom: 8px;
}
</style>
