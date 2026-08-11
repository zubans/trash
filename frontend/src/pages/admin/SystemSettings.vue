<template>
  <div class="system-settings">
    <h1 class="va-h3 mb-4">{{ $t('settings.title') }}</h1>

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
          v-model="values.standard_tariff_coeff"
          type="number"
          :label="$t('settings.standardTariffCoeff')"
          step="0.01"
          min="0"
          class="mb-4"
          required
        />

        <!-- Increased Tariff Coeff -->
        <va-input
          v-model="values.increased_tariff_coeff"
          type="number"
          :label="$t('settings.increasedTariffCoeff')"
          step="0.01"
          min="0"
          class="mb-4"
          required
        />

        <!-- Urgent Tariff Coeff -->
        <va-input
          v-model="values.urgent_tariff_coeff"
          type="number"
          :label="$t('settings.urgentTariffCoeff')"
          step="0.01"
          min="0"
          class="mb-4"
          required
        />

        <!-- ASAP Tariff Coeff -->
        <va-input
          v-model="values.asap_tariff_coeff"
          type="number"
          :label="$t('settings.asapTariffCoeff')"
          step="0.01"
          min="0"
          class="mb-4"
          required
        />

        <!-- Fine Amount -->
        <va-input
          v-model="values.geofence_fine_amount"
          type="number"
          :label="$t('settings.fineAmount')"
          step="1"
          min="0"
          class="mb-4"
          required
        />

        <!-- Currency -->
        <va-select
          v-model="values.currency"
          :options="currencyOptions"
          :label="$t('settings.currency')"
          text-by="label"
          value-by="value"
          class="mb-4"
        />

        <!-- Executor location send interval -->
        <va-input
          v-model="values.executor_location_send_interval_seconds"
          type="number"
          :label="$t('settings.executorLocationInterval')"
          step="1"
          min="1"
          class="mb-4"
          required
        />

        <!-- Actions -->
        <div class="d-flex gap-3">
          <va-button type="submit" color="primary">{{ $t('settings.save') }}</va-button>
          <va-button color="secondary" outline @click="loadSettings">{{ $t('settings.reset') }}</va-button>
        </div>
      </va-form>
    </va-card>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import api from '../../services/api'

export default defineComponent({
  name: 'SystemSettings',
  setup() {
    const { t } = useI18n()

    const values = ref<Record<string, string>>({
      standard_tariff_coeff: '1.0',
      increased_tariff_coeff: '2.0',
      urgent_tariff_coeff: '3.0',
      asap_tariff_coeff: '8.0',
      geofence_fine_amount: '500',
      currency: 'RUB',
      executor_location_send_interval_seconds: '5',
    })

    const currencyOptions = [
      { label: '₽ RUB', value: 'RUB' },
      { label: '$ USD', value: 'USD' },
      { label: '€ EUR', value: 'EUR' },
    ]

    const loading = ref(false)
    const successMsg = ref('')
    const errorMsg = ref('')

    const loadSettings = async () => {
      loading.value = true
      errorMsg.value = ''
      try {
        const response = await api.get('/admin/settings')
        if (response.data) {
          for (const key of Object.keys(values.value)) {
            if (response.data[key] !== undefined) {
              values.value[key] = String(response.data[key])
            }
          }
        }
      } catch (err) {
        errorMsg.value = t('settings.loadError')
        console.error(err)
      } finally {
        loading.value = false
      }
    }

    const saveSettings = async () => {
      successMsg.value = ''
      errorMsg.value = ''
      try {
        await api.post('/admin/settings', values.value)
        successMsg.value = t('settings.saveSuccess')
      } catch (err: any) {
        errorMsg.value = err.response?.data || t('settings.saveError')
        console.error(err)
      }
    }

    onMounted(() => {
      loadSettings()
    })

    return {
      values,
      currencyOptions,
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
