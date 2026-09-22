<template>
  <div v-if="order" class="modal-overlay" @click.self="$emit('close')">
    <div class="modal-card">
      <button type="button" class="btn-close" :aria-label="$t('common.close')" @click="$emit('close')">
        <i class="ph ph-x"></i>
      </button>

      <div>
        <h2 class="modal-title">{{ $t('shop.orders.number', { number: order.number }) }}</h2>
        <div class="modal-sub">{{ title }} · {{ formatDate(order.created_at) }}</div>
      </div>

      <div class="rows">
        <div class="row">
          <span class="muted">{{ $t('shop.checkout.total') }}</span>
          <strong>{{ money(order.total) }}</strong>
        </div>
        <div class="row">
          <span class="muted">{{ $t('shop.orders.status') }}</span>
          <span :class="['chip', order.status]">{{ $t('shop.status.' + order.status) }}</span>
        </div>
        <div v-if="order.quantity > 1" class="row">
          <span class="muted">{{ $t('shop.checkout.quantity') }}</span>
          <span>{{ order.quantity }}</span>
        </div>
        <div v-if="order.variant" class="row">
          <span class="muted">{{ $t('shop.card.variant') }}</span>
          <span>{{ order.variant }}</span>
        </div>
        <div v-if="order.fulfillment.method === 'PICKUP'" class="row">
          <span class="muted">{{ $t('shop.orders.pickupAt') }}</span>
          <span class="right">
            {{ localized(order.fulfillment.pickup_point?.title) }}<br />
            <small>{{ order.fulfillment.pickup_point?.address }}</small>
          </span>
        </div>
        <div v-if="order.fulfillment.method === 'DELIVERY'" class="row">
          <span class="muted">{{ $t('shop.orders.deliveryTo') }}</span>
          <span class="right">{{ order.fulfillment.address }}</span>
        </div>
        <div v-if="order.fulfillment.track" class="row">
          <span class="muted">{{ $t('shop.orders.track') }}</span>
          <code>{{ order.fulfillment.track }}</code>
        </div>
        <div v-if="order.refunded_amount > 0" class="row">
          <span class="muted">{{ $t('shop.orders.refunded', { amount: money(order.refunded_amount) }) }}</span>
        </div>
      </div>

      <div v-if="order.cancel_reason" class="note">
        {{ $t('shop.orders.canceledReason', { reason: order.cancel_reason }) }}
      </div>

      <div v-for="perk in order.perks || []" :key="perk.id" class="note">
        <strong>{{ perkTitle(perk.kind, perk.value) }}</strong> —
        {{ $t('shop.orders.perkPeriod', { from: formatDate(perk.starts_at), to: formatDate(perk.expires_at) }) }}
        <span v-if="perk.revoked_at">({{ $t('shop.orders.revoked') }})</span>
      </div>

      <div v-if="order.coupons?.length" class="coupons">
        <div class="field-label">{{ $t('shop.orders.coupons') }}</div>
        <div v-for="coupon in order.coupons" :key="coupon.id" class="coupon">
          <div class="coupon-main">
            <code class="coupon-code">{{ coupon.coupon_code }}</code>
            <span class="chip">{{ $t('shop.coupon.' + coupon.status) }}</span>
          </div>
          <div v-if="secrets[coupon.id]" class="secret">
            {{ $t('shop.orders.code') }}: <code>{{ secrets[coupon.id] }}</code>
          </div>
          <button
            v-else-if="canReveal(coupon)"
            type="button"
            class="btn-secondary small"
            :disabled="revealing === coupon.id"
            @click="reveal(coupon)"
          >
            {{ $t('shop.orders.showCode') }}
          </button>
        </div>
      </div>

      <div v-if="error" class="alert error">{{ error }}</div>

      <!-- Своей отмены у покупателя нет: возврат — диалог в чате поддержки
           (оферта, п. 8). Кнопка лишь открывает чат с номером покупки. -->
      <template v-if="order.status !== 'CANCELED'">
        <button type="button" class="btn-secondary" @click="$emit('refund', order)">
          <i class="ph-bold ph-arrow-u-up-left"></i>
          {{ $t('shop.orders.refund') }}
        </button>
        <div class="note">
          {{ $t('shop.orders.refundHint') }}
          <router-link :to="{ path: '/shop/offer', hash: '#section-8' }">{{ $t('shop.orders.refundOffer') }}</router-link>
        </div>
      </template>
    </div>
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, reactive, ref, type PropType } from 'vue'
import { useI18n } from 'vue-i18n'
import { revealGift, type UserGift } from '../../api/achievements'
import { localized as pick, type Localized, type ShopOrder } from '../../api/shop'
import { perkTitle } from '../../utils/perk'
import { formatApiError } from '../../services/api'

export default defineComponent({
  name: 'ShopOrderModal',
  props: {
    order: { type: Object as PropType<ShopOrder | null>, default: null },
    currencySymbol: { type: String, default: '₽' },
  },
  emits: ['close', 'refund'],
  setup(props) {
    const { t, locale } = useI18n()
    const secrets = reactive<Record<string, string>>({})
    const revealing = ref('')
    const error = ref('')

    const localized = (value?: Localized) => pick(value, locale.value)
    const title = computed(() => localized(props.order?.product_snapshot.title))
    const money = (value: number) =>
      `${Number(value || 0).toLocaleString('ru-RU', { maximumFractionDigits: 2 })} ${props.currencySymbol}`
    const formatDate = (value: string) =>
      value ? new Date(value).toLocaleDateString(locale.value === 'en' ? 'en-GB' : 'ru-RU', { day: 'numeric', month: 'long', year: 'numeric' }) : ''

    // Код сертификата показывается только по явному запросу владельца: показ
    // пишется в аудит, и после него возврат возможен лишь с подтверждением
    // партнёра (оферта, п. 8.7).
    const canReveal = (coupon: UserGift) =>
      props.order?.product_snapshot.kind === 'CERTIFICATE' && (coupon.status === 'ISSUED' || coupon.status === 'REVEALED')

    const reveal = async (coupon: UserGift) => {
      revealing.value = coupon.id
      error.value = ''
      try {
        const opened = await revealGift(coupon.id)
        if (opened.secret) secrets[coupon.id] = opened.secret
      } catch (err: any) {
        error.value = formatApiError(err, t('shop.errors.generic'))
      } finally {
        revealing.value = ''
      }
    }

    return { secrets, revealing, error, title, money, formatDate, localized, canReveal, reveal, perkTitle }
  },
})
</script>

<style scoped src="./shop-modal.css"></style>
<style scoped>
.right {
  text-align: right;
}
.coupons {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.coupon {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 10px 12px;
  border: 1px dashed #cbd5e1;
  border-radius: 12px;
}
.coupon-main {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.coupon-code {
  font-size: 15px;
  font-weight: 700;
  letter-spacing: 0.05em;
}
.secret code {
  font-weight: 700;
}
.small {
  padding: 8px 12px;
  font-size: 13px;
}
</style>
