<template>
  <div class="shop-admin">
    <p v-if="errorMsg" class="alert error">{{ errorMsg }}</p>
    <p v-if="successMsg" class="alert success">{{ successMsg }}</p>

    <section class="panel">
      <div class="row filters">
        <select v-model="filters.status" class="input" @change="applyFilters">
          <option value="">{{ $t('shop.admin.all') }}</option>
          <option v-for="s in statuses" :key="s" :value="s">{{ $t('shop.status.' + s) }}</option>
        </select>
        <input v-model="filters.q" class="input" :placeholder="$t('shop.admin.search')" @keyup.enter="applyFilters" />
        <label class="field inline">
          <span>{{ $t('shop.admin.from') }}</span>
          <input v-model="filters.from" type="date" class="input" @change="applyFilters" />
        </label>
        <label class="field inline">
          <span>{{ $t('shop.admin.to') }}</span>
          <input v-model="filters.to" type="date" class="input" @change="applyFilters" />
        </label>
        <label class="field checkbox">
          <input v-model="filters.refund" type="checkbox" @change="applyFilters" /> {{ $t('shop.admin.refundRequested') }}
        </label>
        <button type="button" class="btn-refresh" :disabled="loading" @click="load">
          <i class="ph-bold ph-arrows-clockwise"></i>
        </button>
      </div>

      <div class="table-wrap">
        <table class="table">
          <thead>
            <tr>
              <th>№</th>
              <th>{{ $t('shop.admin.fields.titleRu') }}</th>
              <th>{{ $t('shop.admin.buyer') }}</th>
              <th>{{ $t('shop.checkout.total') }}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="o in orders"
              :key="o.id"
              class="clickable"
              :class="{ selected: selected?.id === o.id }"
              @click="open(o.id)"
            >
              <td class="mono">{{ o.number }}</td>
              <td>
                {{ o.product_snapshot.title?.ru }}
                <div class="muted">{{ formatDate(o.created_at) }}<template v-if="o.quantity > 1"> · {{ o.quantity }} шт.</template></div>
              </td>
              <td>
                {{ o.user_name || '—' }}
                <div class="muted">{{ o.user_phone }}</div>
              </td>
              <td>{{ money(o.total) }}</td>
              <td>
                <span class="badge" :class="statusClass(o.status)">{{ $t('shop.status.' + o.status) }}</span>
                <span v-if="o.refund_request_at && o.status !== 'CANCELED'" class="badge" :class="requestOld(o) ? 'danger' : 'warn'">
                  {{ requestOld(o) ? $t('shop.admin.refundOld') : $t('shop.admin.refundRequested') }}
                </span>
              </td>
            </tr>
            <tr v-if="!orders.length">
              <td colspan="5" class="empty">{{ $t('shop.admin.ordersEmpty') }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="total > pageSize" class="row pager">
        <button type="button" class="btn-secondary" :disabled="offset === 0" @click="page(-1)">←</button>
        <span class="muted">{{ offset + 1 }}–{{ Math.min(offset + pageSize, total) }} / {{ total }}</span>
        <button type="button" class="btn-secondary" :disabled="offset + pageSize >= total" @click="page(1)">→</button>
      </div>
    </section>

    <section v-if="selected" ref="cardRef" class="panel">
      <div class="card-head">
        <h2>{{ $t('shop.orders.number', { number: selected.number }) }}</h2>
        <span class="badge" :class="statusClass(selected.status)">{{ $t('shop.status.' + selected.status) }}</span>
        <button type="button" class="btn-link close" @click="close">{{ $t('common.close') }}</button>
      </div>

      <dl class="kv">
        <dt>{{ $t('shop.admin.snapshot') }}</dt>
        <dd>
          {{ selected.product_snapshot.title?.ru }} · {{ $t('shop.kinds.' + (selected.product_snapshot.kind || 'PHYSICAL')) }}
          <template v-if="selected.product_snapshot.perk_rule">
            · {{ ruleSummary(selected.product_snapshot.perk_rule, selected.product_snapshot.perk_config) }},
            {{ selected.product_snapshot.perk_days }} дн.
          </template>
        </dd>
        <dt>{{ $t('shop.admin.buyer') }}</dt>
        <dd>
          {{ selected.user_name || '—' }} · {{ selected.user_phone }}
          <router-link
            v-if="selected.support_chat_id && can('support_chats.view')"
            :to="{ path: '/admin/support-chats', query: { chat: selected.support_chat_id } }"
            class="btn-link"
          >
            {{ $t('shop.admin.toSupport') }}
          </router-link>
        </dd>
        <dt>{{ $t('shop.checkout.total') }}</dt>
        <dd>
          {{ money(selected.total) }} ({{ selected.quantity }} × {{ money(selected.unit_price) }})
          <template v-if="selected.refunded_amount > 0"> · {{ $t('shop.orders.refunded', { amount: money(selected.refunded_amount) }) }}</template>
        </dd>
        <template v-if="selected.variant">
          <dt>{{ $t('shop.card.variant') }}</dt>
          <dd>{{ selected.variant }}</dd>
        </template>
        <template v-if="selected.fulfillment.method">
          <dt>{{ $t('shop.card.fulfillment') }}</dt>
          <dd>
            {{ $t('shop.methods.' + selected.fulfillment.method) }}:
            <template v-if="selected.fulfillment.method === 'PICKUP'">
              {{ selected.fulfillment.pickup_point?.title?.ru }}, {{ selected.fulfillment.pickup_point?.address }}
            </template>
            <template v-else>
              {{ selected.fulfillment.address }} · {{ selected.fulfillment.recipient }} · {{ selected.fulfillment.phone }}
            </template>
          </dd>
        </template>
        <template v-if="selected.fulfillment.track">
          <dt>{{ $t('shop.orders.track') }}</dt>
          <dd class="mono">{{ selected.fulfillment.track }}</dd>
        </template>
        <dt></dt>
        <dd class="muted">{{ $t('shop.admin.offerVersion', { version: selected.offer_version }) }}</dd>
        <template v-if="selected.refund_request_at && selected.status !== 'CANCELED'">
          <dt>{{ $t('shop.admin.refundRequested') }}</dt>
          <dd :class="requestOld(selected) ? 'danger-text' : ''">{{ formatDate(selected.refund_request_at) }}</dd>
        </template>
        <template v-if="selected.cancel_reason">
          <dt>{{ $t('shop.admin.reason') }}</dt>
          <dd>{{ selected.cancel_reason }}</dd>
        </template>
      </dl>

      <template v-if="selected.coupons?.length">
        <h3>{{ $t('shop.orders.coupons') }}</h3>
        <div class="row">
          <span v-for="c in selected.coupons" :key="c.id" class="badge" :class="c.status === 'REDEEMED' ? 'ok' : ''">
            <span class="mono">{{ c.coupon_code }}</span> · {{ $t('shop.coupon.' + c.status) }}
          </span>
        </div>
      </template>

      <template v-if="selected.perks?.length">
        <h3>{{ $t('shop.admin.userPerks') }}</h3>
        <div v-for="p in selected.perks" :key="p.id" class="muted">
          {{ ruleText(p.rule_title, p.config) }} —
          {{ $t('shop.orders.perkPeriod', { from: formatDate(p.starts_at), to: formatDate(p.expires_at) }) }}
          <span v-if="p.revoked_at">({{ $t('shop.orders.revoked') }})</span>
        </div>
      </template>

      <template v-if="selected.transactions?.length">
        <h3>{{ $t('shop.admin.transactions') }}</h3>
        <table class="table">
          <tbody>
            <tr v-for="tx in selected.transactions" :key="tx.id">
              <td class="mono">{{ tx.type }}</td>
              <td>{{ tx.direction > 0 ? '+' : tx.direction < 0 ? '−' : '' }}{{ money(tx.amount) }}</td>
              <td class="muted">{{ tx.counterparty }}</td>
              <td class="muted">{{ formatDate(tx.created_at) }}</td>
            </tr>
          </tbody>
        </table>
      </template>

      <!-- Действия: переходы статуса вещи и отмена с возвратом. -->
      <template v-if="can('shop_orders.edit') && selected.status !== 'CANCELED'">
        <h3>&nbsp;</h3>
        <div v-if="nextStatus" class="row">
          <input
            v-if="nextStatus === 'SHIPPED'"
            v-model="track"
            class="input mono"
            :placeholder="$t('shop.admin.trackNumber')"
          />
          <button type="button" class="btn-primary" :disabled="busy" @click="advance">
            {{ $t('shop.admin.moveTo', { status: $t('shop.status.' + nextStatus) }) }}
          </button>
        </div>
        <span v-if="fieldErrors.track" class="field-error">{{ fieldErrors.track }}</span>

        <div class="cancel-box">
          <button v-if="!cancelOpen" type="button" class="btn-danger" @click="openCancel">{{ $t('shop.admin.cancel') }}</button>
          <template v-else>
            <h3>{{ $t('shop.admin.cancelTitle', { number: selected.number }) }}</h3>
            <p v-if="quote" class="panel-sub">
              {{ $t('shop.admin.suggested', { amount: money(quote.suggested), max: money(quote.max) }) }}
              <template v-if="quote.redeemed"> · {{ $t('shop.admin.redeemedCount', { count: quote.redeemed }) }}</template>
            </p>
            <p v-if="quote?.certificate_revealed" class="alert warn">{{ $t('shop.admin.certRevealed') }}</p>
            <div class="form-grid">
              <label class="field wide">
                <span>{{ $t('shop.admin.reason') }}</span>
                <input v-model="cancel.reason" class="input" :class="{ invalid: fieldErrors.reason }" />
                <span v-if="fieldErrors.reason" class="field-error">{{ fieldErrors.reason }}</span>
              </label>
              <label class="field">
                <span>{{ $t('shop.admin.refundAmount') }}, ₽</span>
                <input v-model.number="cancel.amount" type="number" min="0" step="0.01" :max="quote?.max" class="input" :class="{ invalid: fieldErrors.amount }" />
                <span v-if="fieldErrors.amount" class="field-error">{{ fieldErrors.amount }}</span>
              </label>
              <label v-if="selected.product_snapshot.kind === 'PHYSICAL'" class="field checkbox">
                <input v-model="cancel.restock" type="checkbox" /> {{ $t('shop.admin.restock') }}
              </label>
              <label v-if="quote?.certificate_revealed" class="field checkbox">
                <input v-model="cancel.partnerConfirmed" type="checkbox" /> {{ $t('shop.admin.partnerConfirmed') }}
              </label>
            </div>
            <div class="row">
              <button type="button" class="btn-danger" :disabled="busy || !cancel.reason.trim()" @click="doCancel">
                {{ $t('shop.admin.cancel') }}
              </button>
              <button type="button" class="btn-secondary" @click="cancelOpen = false">{{ $t('common.cancel') }}</button>
            </div>
          </template>
        </div>
      </template>
    </section>
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../../stores/auth-store'
import {
  adminCancelOrder,
  adminGetOrder,
  adminGetOrders,
  adminRefundQuote,
  adminSetOrderStatus,
  shopError,
  shopErrorText,
  type RefundQuote,
  type ShopOrder,
  type ShopOrderStatus,
} from '../../api/shop'
import { ruleSummary, ruleText } from '../../utils/perk'

const STATUSES: ShopOrderStatus[] = ['PAID', 'PROCESSING', 'SHIPPED', 'COMPLETED', 'CANCELED']
// Переход вперёд для вещи; отмена — отдельным путём с возвратом.
const NEXT: Partial<Record<ShopOrderStatus, ShopOrderStatus>> = {
  PAID: 'PROCESSING',
  PROCESSING: 'SHIPPED',
  SHIPPED: 'COMPLETED',
}
// Срок ответа по оферте — 10 дней; обращение старше пяти выделяется красным.
const REQUEST_OLD_MS = 5 * 24 * 3600 * 1000

export default defineComponent({
  name: 'ShopOrders',
  setup() {
    const route = useRoute()
    const router = useRouter()
    const { t } = useI18n()
    const authStore = useAuthStore()
    const can = (permission: string) => authStore.can(permission)

    const q = (key: string) => (typeof route.query[key] === 'string' ? (route.query[key] as string) : '')
    // Фильтр живёт в адресе, как у заказов: ссылку на «оплаченные» можно
    // переслать, и «назад» возвращает к тому же списку.
    const filters = reactive({
      status: q('status'),
      q: q('q'),
      from: q('from'),
      to: q('to'),
      refund: q('refund') === '1',
    })
    const pageSize = 50
    const offset = ref(Number(q('offset')) || 0)

    const orders = ref<ShopOrder[]>([])
    const total = ref(0)
    const loading = ref(false)
    const busy = ref(false)
    const errorMsg = ref('')
    const successMsg = ref('')
    const fieldErrors = ref<Record<string, string>>({})
    const selected = ref<ShopOrder | null>(null)
    const track = ref('')
    const cancelOpen = ref(false)
    const quote = ref<RefundQuote | null>(null)
    const cancel = reactive({ reason: '', amount: 0, restock: true, partnerConfirmed: false })
    const cardRef = ref<HTMLElement | null>(null)

    const money = (value: number) => `${Number(value || 0).toLocaleString('ru-RU', { maximumFractionDigits: 2 })} ₽`
    const formatDate = (value: string) =>
      value ? new Date(value).toLocaleString('ru-RU', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }) : ''
    const statusClass = (s: string) =>
      ({ PAID: 'info', PROCESSING: 'warn', SHIPPED: 'info', COMPLETED: 'ok', CANCELED: '' })[s] ?? ''
    const requestOld = (o: ShopOrder) => !!o.refund_request_at && Date.now() - new Date(o.refund_request_at).getTime() > REQUEST_OLD_MS
    const nextStatus = computed(() =>
      selected.value?.product_snapshot.kind === 'PHYSICAL' ? NEXT[selected.value.status] : undefined,
    )

    const load = async () => {
      loading.value = true
      errorMsg.value = ''
      try {
        const res = await adminGetOrders({ ...filters, limit: pageSize, offset: offset.value })
        orders.value = res.orders
        total.value = res.total
      } catch (err) {
        errorMsg.value = shopErrorText(err, t, t('shop.loadFailed'))
      } finally {
        loading.value = false
      }
    }

    const applyFilters = () => {
      offset.value = 0
      router.replace({
        query: {
          status: filters.status || undefined,
          q: filters.q || undefined,
          from: filters.from || undefined,
          to: filters.to || undefined,
          refund: filters.refund ? '1' : undefined,
          order: route.query.order,
        },
      })
      load()
    }

    const page = (delta: number) => {
      offset.value = Math.max(0, offset.value + delta * pageSize)
      load()
    }

    const open = (id: string) => {
      if (route.query.order !== id) router.replace({ query: { ...route.query, order: id } })
      else loadSelected(id)
    }

    const loadSelected = async (id: string) => {
      successMsg.value = ''
      fieldErrors.value = {}
      cancelOpen.value = false
      track.value = ''
      try {
        selected.value = await adminGetOrder(id)
        track.value = selected.value.fulfillment.track || ''
        await nextTick()
        cardRef.value?.scrollIntoView({ behavior: 'smooth', block: 'start' })
      } catch (err) {
        selected.value = null
        errorMsg.value = shopErrorText(err, t, t('shop.errors.generic'))
      }
    }

    const close = () => {
      selected.value = null
      router.replace({ query: { ...route.query, order: undefined } })
    }

    const afterAction = async (order: ShopOrder, message: string) => {
      selected.value = order
      successMsg.value = message
      await load()
    }

    const advance = async () => {
      if (!selected.value || !nextStatus.value) return
      busy.value = true
      fieldErrors.value = {}
      errorMsg.value = ''
      try {
        await afterAction(await adminSetOrderStatus(selected.value.id, nextStatus.value, track.value), t('shop.admin.statusChanged'))
      } catch (err) {
        fieldErrors.value = shopError(err)?.fields || {}
        errorMsg.value = shopErrorText(err, t, t('shop.errors.generic'))
      } finally {
        busy.value = false
      }
    }

    const openCancel = async () => {
      if (!selected.value) return
      cancelOpen.value = true
      cancel.reason = ''
      cancel.restock = true
      cancel.partnerConfirmed = false
      try {
        quote.value = await adminRefundQuote(selected.value.id)
        cancel.amount = quote.value.suggested
      } catch (err) {
        errorMsg.value = shopErrorText(err, t, t('shop.errors.generic'))
      }
    }

    const doCancel = async () => {
      if (!selected.value) return
      busy.value = true
      fieldErrors.value = {}
      errorMsg.value = ''
      try {
        const order = await adminCancelOrder(selected.value.id, {
          reason: cancel.reason.trim(),
          amount: cancel.amount,
          restock: cancel.restock,
          partner_confirmed: cancel.partnerConfirmed,
        })
        cancelOpen.value = false
        await afterAction(order, t('shop.admin.canceled'))
      } catch (err) {
        fieldErrors.value = shopError(err)?.fields || {}
        errorMsg.value = shopErrorText(err, t, t('shop.errors.generic'))
      } finally {
        busy.value = false
      }
    }

    watch(
      () => route.query.order,
      (id) => {
        if (typeof id === 'string' && id) loadSelected(id)
        else selected.value = null
      },
    )

    onMounted(() => {
      load()
      const id = q('order')
      if (id) loadSelected(id)
    })

    return {
      can, statuses: STATUSES, filters, pageSize, offset, orders, total, loading, busy, errorMsg, successMsg,
      fieldErrors, selected, track, cancelOpen, quote, cancel, cardRef, money, formatDate, statusClass,
      requestOld, nextStatus, load, applyFilters, page, open, close, advance, openCancel, doCancel, ruleSummary, ruleText,
    }
  },
})
</script>

<style scoped src="./shop-admin.css"></style>
<style scoped>
.filters {
  margin-bottom: 10px;
}
.field.inline {
  flex-direction: row;
  align-items: center;
  gap: 6px;
}
.pager {
  justify-content: flex-end;
  margin-top: 10px;
}
.card-head {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 10px;
}
.card-head h2 {
  margin: 0;
}
.close {
  margin-left: auto;
}
.cancel-box {
  margin-top: 16px;
  padding-top: 12px;
  border-top: 1px solid #f3f4f6;
}
.danger-text {
  color: #b91c1c;
  font-weight: 600;
}
</style>
