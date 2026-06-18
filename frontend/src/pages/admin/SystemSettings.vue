<template>
  <div class="system-settings">
    <h1 class="va-h3 mb-4">System Settings</h1>

    <!-- Success/Error Messages -->
    <va-alert v-if="successMsg" color="success" class="mb-4" closeable @dismissed="successMsg = ''">
      {{ successMsg }}
    </va-alert>
    <va-alert v-if="errorMsg" color="danger" class="mb-4" closeable @dismissed="errorMsg = ''">
      {{ errorMsg }}
    </va-alert>

    <va-card class="p-4" style="max-width: 600px;">
      <va-form @submit.prevent="saveSettings">
        <!-- Standard Tariff Coeff -->
        <va-input
          v-model.number="settings.standard_tariff_coeff"
          type="number"
          label="Standard Tariff Coefficient (x)"
          step="0.01"
          min="0"
          class="mb-4"
          required
        />

        <!-- Increased Tariff Coeff -->
        <va-input
          v-model.number="settings.increased_tariff_coeff"
          type="number"
          label="Increased Tariff Coefficient (x)"
          step="0.01"
          min="0"
          class="mb-4"
          required
        />

        <!-- Urgent Tariff Coeff -->
        <va-input
          v-model.number="settings.urgent_tariff_coeff"
          type="number"
          label="Urgent Tariff Coefficient (x)"
          step="0.01"
          min="0"
          class="mb-4"
          required
        />

        <!-- ASAP Tariff Coeff -->
        <va-input
          v-model.number="settings.asap_tariff_coeff"
          type="number"
          label="ASAP Tariff Coefficient (x)"
          step="0.01"
          min="0"
          class="mb-4"
          required
        />

        <!-- Fine Amount -->
        <va-input
          v-model.number="settings.fine_amount"
          type="number"
          label="SLA Violation Fine Amount ($)"
          step="1"
          min="0"
          class="mb-4"
          required
        />

        <!-- Actions -->
        <div class="d-flex gap-3">
          <va-button type="submit" color="primary">Save Changes</va-button>
          <va-button color="secondary" outline @click="loadSettings">Reset</va-button>
        </div>
      </va-form>
    </va-card>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, onMounted } from 'vue'
import api from '../../services/api'

export default defineComponent({
  name: 'SystemSettings',
  setup() {
    const settings = ref<Record<string, number>>({
      standard_tariff_coeff: 1.0,
      increased_tariff_coeff: 2.0,
      urgent_tariff_coeff: 3.0,
      asap_tariff_coeff: 8.0,
      fine_amount: 500,
    })

    const loading = ref(false)
    const successMsg = ref('')
    const errorMsg = ref('')

    const loadSettings = async () => {
      loading.value = true
      errorMsg.value = ''
      try {
        const response = await api.get('/admin/settings')
        if (response.data) {
          settings.value = response.data
        }
      } catch (err) {
        errorMsg.value = 'Failed to load system settings.'
        console.error(err)
      } finally {
        loading.value = false
      }
    }

    const saveSettings = async () => {
      successMsg.value = ''
      errorMsg.value = ''
      try {
        await api.post('/admin/settings', settings.value)
        successMsg.value = 'Settings saved successfully.'
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Failed to save settings.'
        console.error(err)
      }
    }

    onMounted(() => {
      loadSettings()
    })

    return {
      settings,
      loading,
      successMsg,
      errorMsg,
      loadSettings,
      saveSettings,
    }
  },
})
</script>

<style scoped>
.system-settings {
  padding: 10px;
}
.d-flex {
  display: flex;
}
.gap-3 {
  gap: 15px;
}
</style>
