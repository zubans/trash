<template>
  <div class="shop-admin">
    <p v-if="errorMsg" class="alert error">{{ errorMsg }}</p>
    <p v-if="successMsg" class="alert success">{{ successMsg }}</p>

    <section class="panel">
      <div class="balance-label">{{ $t('shop.admin.balance') }}</div>
      <div class="balance" :class="{ negative: (revenue?.balance || 0) < 0 }">{{ money(revenue?.balance || 0) }}</div>
    </section>

    <section class="panel">
      <h2>{{ $t('shop.admin.sales') }}</h2>
      <div class="row period">
        <label class="field">
          <span>{{ $t('shop.admin.from') }}</span>
          <input v-model="from" type="date" class="input" />
        </label>
        <label class="field">
          <span>{{ $t('shop.admin.to') }}</span>
          <input v-model="to" type="date" class="input" />
        </label>
        <button type="button" class="btn-secondary" :disabled="loading" @click="load">
          <i class="ph-bold ph-arrows-clockwise"></i>
        </button>
      </div>
      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>{{ $t('shop.admin.fields.titleRu') }}</th>
              <th>{{ $t('shop.admin.salesOrders') }}</th>
              <th>{{ $t('shop.admin.salesQty') }}</th>
              <th>{{ $t('shop.admin.salesTotal') }}</th>
              <th>{{ $t('shop.admin.salesRefunded') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in revenue?.sales || []" :key="row.product_id">
              <td>{{ row.title?.ru }} <span class="muted">· {{ $t('shop.kinds.' + row.kind) }}</span></td>
              <td>{{ row.orders }}</td>
              <td>{{ row.quantity }}</td>
              <td>{{ money(row.total) }}</td>
              <td>{{ money(row.refunded) }}</td>
            </tr>
            <tr v-if="!(revenue?.sales || []).length">
              <td colspan="5" class="empty">{{ $t('shop.admin.noSales') }}</td>
            </tr>
            <tr v-else class="totals">
              <td colspan="3"></td>
              <td>{{ money(revenue?.total || 0) }}</td>
              <td>{{ money(revenue?.refunded || 0) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section v-if="can('shop_revenue.edit')" class="panel">
      <h2>{{ $t('shop.admin.payout') }}</h2>
      <p class="panel-sub">{{ $t('shop.admin.payoutHint') }}</p>
      <div class="row">
        <input v-model.number="amount" type="number" min="0" step="0.01" class="input" :placeholder="$t('shop.admin.amount')" />
        <button type="button" class="btn-secondary" :disabled="!revenue || revenue.balance <= 0" @click="amount = revenue?.balance || 0">
          {{ $t('commission.withdrawAll') }}
        </button>
        <button type="button" class="btn-primary" :disabled="!canPayout || paying" @click="showConfirm = true">
          {{ $t('shop.admin.payout') }}
        </button>
      </div>
    </section>

    <va-modal
      v-model="showConfirm"
      :message="$t('shop.admin.payoutConfirm', { amount: money(amount) })"
      :ok-text="$t('common.confirm')"
      :cancel-text="$t('common.cancel')"
      @ok="payout"
    />
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../../stores/auth-store'
import { adminGetRevenue, adminPayoutRevenue, shopErrorText, type ShopRevenue } from '../../api/shop'

const isoDay = (d: Date) => d.toISOString().slice(0, 10)

// Выручка магазина — по образцу «Комиссии платформы»: остаток счёта SHOP,
// продажи за период и вывод, охраняемый остатком (implementation_plan_shop.md §4.1).
export default defineComponent({
  name: 'ShopRevenue',
  setup() {
    const { t } = useI18n()
    const authStore = useAuthStore()
    const can = (permission: string) => authStore.can(permission)

    const revenue = ref<ShopRevenue | null>(null)
    const today = new Date()
    const from = ref(isoDay(new Date(today.getTime() - 30 * 24 * 3600 * 1000)))
    const to = ref(isoDay(today))
    const amount = ref(0)
    const loading = ref(false)
    const paying = ref(false)
    const showConfirm = ref(false)
    const errorMsg = ref('')
    const successMsg = ref('')

    const money = (value: number) => `${Number(value || 0).toLocaleString('ru-RU', { maximumFractionDigits: 2 })} ₽`
    const canPayout = computed(() => !!revenue.value && amount.value > 0 && amount.value <= revenue.value.balance)

    const load = async () => {
      loading.value = true
      errorMsg.value = ''
      try {
        revenue.value = await adminGetRevenue(from.value, to.value)
      } catch (err) {
        errorMsg.value = shopErrorText(err, t, t('shop.loadFailed'))
      } finally {
        loading.value = false
      }
    }

    const payout = async () => {
      if (!canPayout.value) return
      paying.value = true
      errorMsg.value = ''
      successMsg.value = ''
      try {
        await adminPayoutRevenue(amount.value)
        amount.value = 0
        successMsg.value = t('shop.admin.payoutDone')
        await load()
      } catch (err) {
        errorMsg.value = shopErrorText(err, t, t('shop.errors.generic'))
      } finally {
        paying.value = false
        showConfirm.value = false
      }
    }

    onMounted(load)

    return { can, revenue, from, to, amount, loading, paying, showConfirm, errorMsg, successMsg, money, canPayout, load, payout }
  },
})
</script>

<style scoped src="./shop-admin.css"></style>
<style scoped>
.balance-label {
  font-size: 13px;
  font-weight: 600;
  color: #6b7280;
}
.balance {
  font-family: ui-monospace, monospace;
  font-size: 28px;
  font-weight: 700;
}
.balance.negative {
  color: #b91c1c;
}
.period {
  align-items: flex-end;
  margin-bottom: 10px;
}
.totals td {
  font-weight: 700;
}
</style>
