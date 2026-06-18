<template>
  <div class="customer-dashboard">
    <div class="dashboard-header mb-5">
      <div class="d-flex justify-content-between align-items-center">
        <div>
          <h1 class="va-h3 m-0">Customer Dashboard</h1>
          <span class="text-secondary text-sm">Manage your profile and wallet</span>
        </div>
        <va-button color="danger" outline size="small" @click="handleLogout">
          <va-icon name="logout" class="mr-2" /> Logout
        </va-button>
      </div>
    </div>

    <!-- Alert messages -->
    <va-alert v-if="successMsg" color="success" class="mb-4" closeable @dismissed="successMsg = ''">
      {{ successMsg }}
    </va-alert>
    <va-alert v-if="errorMsg" color="danger" class="mb-4" closeable @dismissed="errorMsg = ''">
      {{ errorMsg }}
    </va-alert>

    <div class="row g-4">
      <!-- Profile Card -->
      <div class="col-md-6">
        <va-card class="p-4 h-100 flex-column d-flex justify-content-between shadow-card">
          <div>
            <h3 class="va-h5 mb-4 text-primary d-flex align-items-center">
              <va-icon name="account_circle" class="mr-2" /> Account Details
            </h3>
            <div class="info-list">
              <div class="info-item mb-3">
                <span class="info-label">Phone</span>
                <span class="info-val">{{ phone }}</span>
              </div>
              <div class="info-item mb-3">
                <span class="info-label">Account Status</span>
                <span class="info-val">
                  <va-badge color="success">{{ status }}</va-badge>
                </span>
              </div>
            </div>
          </div>

          <div class="balance-box mt-4 p-3 text-center">
            <span class="balance-label d-block text-secondary text-sm mb-1">Available Balance</span>
            <span class="balance-amount">${{ Number(balance).toFixed(2) }}</span>
          </div>
        </va-card>
      </div>

      <!-- Top-up Card -->
      <div class="col-md-6">
        <va-card class="p-4 h-100 shadow-card">
          <h3 class="va-h5 mb-4 text-primary d-flex align-items-center">
            <va-icon name="payment" class="mr-2" /> Request Wallet Top-Up
          </h3>
          <p class="text-secondary text-sm mb-4">
            Enter the amount you wish to add. An administrator will verify and approve your request.
          </p>

          <va-form @submit.prevent="submitTopUp">
            <va-input
              v-model.number="topUpAmount"
              type="number"
              label="Amount ($)"
              placeholder="100"
              min="1"
              step="any"
              class="mb-4"
              required
            >
              <template #prependInner>
                <va-icon name="attach_money" />
              </template>
            </va-input>
            
            <va-button type="submit" block :loading="submitting">
              Submit Request
            </va-button>
          </va-form>
        </va-card>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../../stores/auth-store'
import api from '../../services/api'

export default defineComponent({
  name: 'CustomerDashboard',
  setup() {
    const router = useRouter()
    const authStore = useAuthStore()

    const phone = ref('')
    const balance = ref(0)
    const status = ref('ACTIVE')

    const topUpAmount = ref(100)
    const submitting = ref(false)
    const successMsg = ref('')
    const errorMsg = ref('')

    const fetchProfile = async () => {
      errorMsg.value = ''
      try {
        const response = await api.get('/customer/profile')
        if (response.data) {
          phone.value = response.data.Phone
          balance.value = response.data.Balance
          status.value = response.data.Status
        }
      } catch (err) {
        errorMsg.value = 'Failed to load profile details.'
        console.error(err)
      }
    }

    const submitTopUp = async () => {
      successMsg.value = ''
      errorMsg.value = ''
      submitting.value = true
      try {
        await api.post('/customer/finances/topup', { amount: topUpAmount.value })
        successMsg.value = `Wallet top-up request for $${topUpAmount.value.toFixed(2)} submitted successfully!`
        topUpAmount.value = 100
        await fetchProfile() // reload balance
      } catch (err: any) {
        errorMsg.value = err.response?.data || 'Failed to submit top-up request.'
        console.error(err)
      } finally {
        submitting.value = false
      }
    }

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

    onMounted(() => {
      fetchProfile()
    })

    return {
      phone,
      balance,
      status,
      topUpAmount,
      submitting,
      successMsg,
      errorMsg,
      submitTopUp,
      handleLogout,
    }
  },
})
</script>

<style scoped>
.customer-dashboard {
  max-width: 900px;
  margin: 40px auto;
  padding: 0 20px;
}

.shadow-card {
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.05);
  border-radius: 12px !important;
  padding: 30px !important;
}

.info-list {
  border-top: 1px solid #edf2f7;
  padding-top: 15px;
}

.info-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.info-label {
  color: #718096;
  font-weight: 500;
}

.info-val {
  font-weight: 600;
  color: #2d3748;
}

.balance-box {
  background: #ebf8ff;
  border: 1px solid #bee3f8;
  border-radius: 8px;
}

.balance-amount {
  font-size: 2.2rem;
  font-weight: 800;
  color: #2b6cb0;
}

.row {
  display: flex;
  flex-wrap: wrap;
  margin-right: -15px;
  margin-left: -15px;
}

.col-md-6 {
  flex: 0 0 50%;
  max-width: 50%;
  padding: 0 15px;
  box-sizing: border-box;
}

@media (max-width: 768px) {
  .col-md-6 {
    flex: 0 0 100%;
    max-width: 100%;
    margin-bottom: 20px;
  }
}

.d-flex {
  display: flex;
}
.flex-column {
  flex-direction: column;
}
.justify-content-between {
  justify-content: space-between;
}
.align-items-center {
  align-items: center;
}
.h-100 {
  height: 100%;
}
.mr-2 {
  margin-right: 8px;
}
.m-0 {
  margin: 0;
}
</style>
