<template>
  <div class="system-settings-page">
    <!-- Header -->
    <div class="settings-page-header mb-4">
      <h1 class="page-title">
        <i class="ph-fill ph-gear" style="color: #5c60f5;"></i>
        {{ $t('settings.title') }}
      </h1>
    </div>

    <!-- Alert Messages -->
    <div v-if="successMsg" class="settings-alert alert-success mb-4">
      <i class="ph-bold ph-check-circle alert-icon"></i>
      <span>{{ successMsg }}</span>
      <button type="button" class="btn-dismiss" @click="successMsg = ''"><i class="ph ph-x"></i></button>
    </div>

    <div v-if="errorMsg" class="settings-alert alert-danger mb-4">
      <i class="ph-bold ph-warning-circle alert-icon"></i>
      <span>{{ errorMsg }}</span>
      <button type="button" class="btn-dismiss" @click="errorMsg = ''"><i class="ph ph-x"></i></button>
    </div>

    <!-- Form Container -->
    <form @submit.prevent="saveSettings">
      <div class="settings-cards-stack">
        <!-- Card 1: Тарификация -->
        <div class="settings-card">
          <div class="section-header">
            <div class="section-icon">
              <i class="ph-fill ph-calculator"></i>
            </div>
            <div class="section-title-group">
              <div class="section-title">Тарификация и коэффициенты</div>
              <div class="section-desc">
                Настройка базовых множителей, которые применяются при расчете итоговой стоимости заказа в зависимости от выбранного тарифа.
              </div>
            </div>
          </div>

          <div class="form-grid">
            <div class="input-group">
              <div class="input-header">
                <label class="input-label">{{ $t('settings.standardTariffCoeff') }}</label>
              </div>
              <div class="input-wrapper has-suffix">
                <input
                  v-model="values.standard_tariff_coeff"
                  type="number"
                  step="0.1"
                  min="0"
                  required
                />
                <span class="input-suffix">x</span>
              </div>
            </div>

            <div class="input-group">
              <div class="input-header">
                <label class="input-label">{{ $t('settings.increasedTariffCoeff') }}</label>
              </div>
              <div class="input-wrapper has-suffix">
                <input
                  v-model="values.increased_tariff_coeff"
                  type="number"
                  step="0.1"
                  min="0"
                  required
                />
                <span class="input-suffix">x</span>
              </div>
            </div>

            <div class="input-group">
              <div class="input-header">
                <label class="input-label">{{ $t('settings.urgentTariffCoeff') }}</label>
              </div>
              <div class="input-wrapper has-suffix">
                <input
                  v-model="values.urgent_tariff_coeff"
                  type="number"
                  step="0.1"
                  min="0"
                  required
                />
                <span class="input-suffix">x</span>
              </div>
            </div>

            <div class="input-group">
              <div class="input-header">
                <label class="input-label">{{ $t('settings.asapTariffCoeff') }}</label>
              </div>
              <div class="input-wrapper has-suffix">
                <input
                  v-model="values.asap_tariff_coeff"
                  type="number"
                  step="0.1"
                  min="0"
                  required
                />
                <span class="input-suffix">x</span>
              </div>
            </div>

            <div class="input-group">
              <div class="input-header">
                <label class="input-label">{{ $t('settings.orderCommissionPercent') }}</label>
                <div class="input-hint">{{ $t('settings.orderCommissionHint') }}</div>
              </div>
              <div class="input-wrapper has-suffix">
                <input
                  v-model="values.order_commission_percent"
                  type="number"
                  step="0.1"
                  min="0"
                  max="100"
                  required
                />
                <span class="input-suffix">%</span>
              </div>
            </div>
          </div>
        </div>

        <!-- Card 2: SLA и Лимиты (Исправлено выравнивание инпутов!) -->
        <div class="settings-card">
          <div class="section-header">
            <div class="section-icon icon-warning">
              <i class="ph-fill ph-shield-warning"></i>
            </div>
            <div class="section-title-group">
              <div class="section-title">Штрафы и лимиты</div>
              <div class="section-desc">
                Финансовые ограничения пользователей и штрафные санкции за нарушение условий предоставления сервиса (SLA).
              </div>
            </div>
          </div>

          <div class="form-grid">
            <div class="input-group">
              <div class="input-header">
                <label class="input-label">{{ $t('settings.autoMatchRadiusKm') }}</label>
              </div>
              <div class="input-wrapper has-prefix">
                <span class="input-prefix">км</span>
                <input
                  v-model="values.auto_match_radius_km"
                  type="number"
                  step="0.5"
                  min="0.5"
                  required
                />
              </div>
            </div>

            <div class="input-group">
              <div class="input-header">
                <label class="input-label">{{ $t('settings.minBalanceLimit') }}</label>
                <div class="input-hint">Максимальный отрицательный баланс, при котором возможна работа</div>
              </div>
              <div class="input-wrapper has-prefix">
                <span class="input-prefix">₽</span>
                <input
                  v-model="values.min_balance_limit"
                  type="number"
                  step="10"
                  min="0"
                  required
                />
              </div>
            </div>
          </div>
        </div>

        <!-- Card 3: Технические параметры -->
        <div class="settings-card">
          <div class="section-header">
            <div class="section-icon icon-neutral">
              <i class="ph-fill ph-cpu"></i>
            </div>
            <div class="section-title-group">
              <div class="section-title">Технические параметры</div>
              <div class="section-desc">Глобальные переменные работы системы и геолокации.</div>
            </div>
          </div>

          <div class="form-grid">
            <div class="input-group">
              <div class="input-header">
                <label class="input-label">{{ $t('settings.currency') }}</label>
              </div>
              <div class="input-wrapper">
                <select v-model="values.currency" class="custom-select">
                  <option v-for="opt in currencyOptions" :key="opt.value" :value="opt.value">
                    {{ opt.label }}
                  </option>
                </select>
              </div>
            </div>

            <div class="input-group">
              <div class="input-header">
                <label class="input-label">{{ $t('settings.executorLocationInterval') }}</label>
              </div>
              <div class="input-wrapper has-suffix">
                <input
                  v-model="values.executor_location_send_interval_seconds"
                  type="number"
                  step="1"
                  min="1"
                  required
                />
                <span class="input-suffix">сек</span>
              </div>
            </div>
          </div>

        </div>
      </div>

      <!-- Sticky Action Bar -->
      <div class="action-bar">
        <div v-if="hasUnsavedChanges" class="unsaved-warning">
          <i class="ph-fill ph-info"></i> У вас есть несохраненные изменения
        </div>
        <button
          type="button"
          class="btn btn-outline"
          :disabled="loading"
          @click="loadSettings"
        >
          <i class="ph-bold ph-arrow-counter-clockwise"></i>
          {{ $t('settings.reset') }}
        </button>
        <button
          type="submit"
          class="btn btn-primary"
          :disabled="loading"
        >
          <i class="ph-bold ph-floppy-disk"></i>
          {{ $t('settings.save') }}
        </button>
      </div>
    </form>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import api from '../../services/api'
import { toSettingsPayload } from '../../utils/settingsPayload'

export default defineComponent({
  name: 'SystemSettings',
  setup() {
    const { t } = useI18n()

    // Not Record<string, string>: Vue casts `<input type="number">` back to a
    // number, so an edited field genuinely holds one until it is normalised on
    // the way out.
    const values = ref<Record<string, string | number>>({
      standard_tariff_coeff: '1.0',
      increased_tariff_coeff: '2.0',
      urgent_tariff_coeff: '3.0',
      asap_tariff_coeff: '8.0',
      order_commission_percent: '0',
      auto_match_radius_km: '10',
      min_balance_limit: '0',
      currency: 'RUB',
      executor_location_send_interval_seconds: '5',
    })

    const initialValues = ref<Record<string, string | number>>({ ...values.value })

    const currencyOptions = [
      { label: 'Российский рубль (₽ RUB)', value: 'RUB' },
      { label: 'Доллар США ($ USD)', value: 'USD' },
      { label: 'Евро (€ EUR)', value: 'EUR' },
    ]

    const loading = ref(false)
    const successMsg = ref('')
    const errorMsg = ref('')

    const hasUnsavedChanges = computed(() => {
      for (const key of Object.keys(values.value)) {
        if (values.value[key] !== initialValues.value[key]) {
          return true
        }
      }
      return false
    })

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
          initialValues.value = { ...values.value }
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
      loading.value = true
      try {
        const payload = toSettingsPayload(values.value)
        await api.post('/admin/settings', payload)
        // Adopt the normalised values, so the unsaved-changes indicator compares
        // like with like instead of 15 against "15".
        values.value = payload
        initialValues.value = { ...payload }
        successMsg.value = t('settings.saveSuccess')
      } catch (err: any) {
        errorMsg.value = err.response?.data || t('settings.saveError')
        console.error(err)
      } finally {
        loading.value = false
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
      hasUnsavedChanges,
      loadSettings,
      saveSettings,
    }
  },
})
</script>

<style scoped>
.system-settings-page {
  max-width: 1000px;
  margin: 0 auto;
  font-family: 'Outfit', sans-serif;
  color: #0f172a;
  position: relative;
  padding-bottom: 24px;
}

.settings-page-header {
  margin-bottom: 24px;
}

.page-title {
  font-size: 28px;
  font-weight: 700;
  color: #0f172a;
  letter-spacing: -0.5px;
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 0;
}

.settings-cards-stack {
  display: flex;
  flex-direction: column;
  gap: 24px;
  margin-bottom: 32px;
}

/* Settings Card */
.settings-card {
  background: #ffffff;
  border-radius: 24px;
  box-shadow: 0 4px 24px rgba(15, 23, 42, 0.04);
  padding: 32px;
  display: flex;
  flex-direction: column;
  gap: 24px;
  border: 1px solid rgba(0, 0, 0, 0.04);
}

.section-header {
  display: flex;
  align-items: flex-start;
  gap: 16px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.05);
  padding-bottom: 20px;
}

.section-icon {
  width: 48px;
  height: 48px;
  border-radius: 14px;
  flex-shrink: 0;
  background: #eef2ff;
  color: #5c60f5;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 24px;
}

.section-icon.icon-warning {
  background: #fffbeb;
  color: #f59e0b;
}

.section-icon.icon-neutral {
  background: #f1f5f9;
  color: #64748b;
}

.section-title-group {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.section-title {
  font-size: 18px;
  font-weight: 700;
  color: #0f172a;
}

.section-desc {
  font-size: 14px;
  color: #64748b;
  line-height: 1.4;
  max-width: 650px;
}

/* Form Grid & Alignment Fix */
.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 24px;
}

/* Toggle row (boolean setting) */
.toggle-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  margin-top: 24px;
  padding-top: 20px;
  border-top: 1px solid #f1f5f9;
}
.toggle-text {
  max-width: 640px;
}

.switch {
  position: relative;
  display: inline-block;
  width: 48px;
  height: 28px;
  flex-shrink: 0;
}
.switch input {
  opacity: 0;
  width: 0;
  height: 0;
}
.switch-slider {
  position: absolute;
  cursor: pointer;
  inset: 0;
  background: #cbd5e1;
  border-radius: 999px;
  transition: background 0.2s ease;
}
.switch-slider::before {
  content: '';
  position: absolute;
  height: 22px;
  width: 22px;
  left: 3px;
  top: 3px;
  background: #ffffff;
  border-radius: 50%;
  transition: transform 0.2s ease;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.2);
}
.switch input:checked + .switch-slider {
  background: #10b981;
}
.switch input:checked + .switch-slider::before {
  transform: translateX(20px);
}

.input-group {
  display: flex;
  flex-direction: column;
}

/* Fix for Card 2 alignment: min-height on input-header ensures inputs align at the bottom */
.input-header {
  min-height: 38px;
  display: flex;
  flex-direction: column;
  justify-content: flex-end;
  margin-bottom: 8px;
}

.input-label {
  font-size: 13px;
  font-weight: 600;
  color: #0f172a;
}

.input-hint {
  font-size: 12px;
  color: #64748b;
  margin-top: 2px;
}

/* Modern Input Wrapper */
.input-wrapper {
  position: relative;
  display: flex;
  align-items: center;
}

.input-wrapper input,
.input-wrapper select {
  width: 100%;
  padding: 12px 16px;
  border-radius: 12px;
  border: 1px solid rgba(0, 0, 0, 0.1);
  background: #f8fafc;
  font-family: 'JetBrains Mono', monospace;
  font-size: 15px;
  font-weight: 500;
  color: #0f172a;
  transition: all 0.2s ease-in-out;
  outline: none;
}

.input-wrapper input:focus,
.input-wrapper select:focus {
  background: #ffffff;
  border-color: #5c60f5;
  box-shadow: 0 0 0 4px rgba(92, 96, 245, 0.1);
}

.input-prefix {
  position: absolute;
  left: 16px;
  color: #64748b;
  font-weight: 600;
  font-size: 15px;
  pointer-events: none;
}

.input-wrapper.has-prefix input {
  padding-left: 36px;
}

.input-suffix {
  position: absolute;
  right: 16px;
  color: #64748b;
  font-weight: 600;
  font-size: 14px;
  pointer-events: none;
}

.input-wrapper.has-suffix input {
  padding-right: 48px;
}

/* Custom Select */
.custom-select {
  appearance: none;
  -webkit-appearance: none;
  font-family: 'Outfit', sans-serif !important;
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='16' height='16' fill='%2364748b' viewBox='0 0 256 256'%3E%3Cpath d='M213.66,101.66l-80,80a8,8,0,0,1-11.32,0l-80-80A8,8,0,0,1,53.66,90.34L128,164.69l74.34-74.35a8,8,0,0,1,11.32,11.32Z'%3E%3C/path%3E%3C/svg%3E");
  background-repeat: no-repeat;
  background-position: right 16px center;
  padding-right: 40px !important;
  cursor: pointer;
}

/* Sticky Action Bar */
.action-bar {
  position: sticky;
  bottom: -28px;
  background: rgba(255, 255, 255, 0.95);
  backdrop-filter: blur(16px);
  -webkit-backdrop-filter: blur(16px);
  border-top: 1px solid rgba(0, 0, 0, 0.05);
  padding: 16px 24px;
  margin-top: 24px;
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 16px;
  border-radius: 16px;
  box-shadow: 0 -10px 30px rgba(15, 23, 42, 0.05);
  z-index: 50;
}

.btn {
  padding: 12px 24px;
  border-radius: 12px;
  font-size: 15px;
  font-weight: 600;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  transition: all 0.2s ease-in-out;
  border: none;
}

.btn-outline {
  background: transparent;
  border: 1px solid rgba(0, 0, 0, 0.1);
  color: #0f172a;
}

.btn-outline:hover:not(:disabled) {
  background: #f8fafc;
  border-color: rgba(0, 0, 0, 0.15);
}

.btn-primary {
  background: #5c60f5;
  color: #ffffff;
  box-shadow: 0 4px 12px rgba(92, 96, 245, 0.2);
}

.btn-primary:hover:not(:disabled) {
  transform: translateY(-2px);
  box-shadow: 0 8px 16px rgba(92, 96, 245, 0.3);
}

.btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.unsaved-warning {
  margin-right: auto;
  display: flex;
  align-items: center;
  gap: 8px;
  color: #f59e0b;
  font-size: 14px;
  font-weight: 600;
}

/* Alert Messages */
.settings-alert {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 18px;
  border-radius: 12px;
  font-size: 14px;
  font-weight: 500;
}

.alert-success {
  background: #ecfdf5;
  color: #065f46;
  border: 1px solid #a7f3d0;
}

.alert-danger {
  background: #fef2f2;
  color: #991b1b;
  border: 1px solid #fecaca;
}

.alert-icon {
  font-size: 18px;
}

.btn-dismiss {
  margin-left: auto;
  background: transparent;
  border: none;
  color: inherit;
  font-size: 16px;
  cursor: pointer;
  opacity: 0.7;
}

.btn-dismiss:hover { opacity: 1; }

@media (max-width: 900px) {
  .form-grid {
    grid-template-columns: 1fr;
    gap: 16px;
  }
  .settings-card {
    padding: 20px;
    border-radius: 16px;
  }
  .action-bar {
    padding: 12px 16px;
    flex-wrap: wrap;
  }
}
</style>

