<template>
  <div v-if="show && product" class="modal-overlay" @click.self="close">
    <div class="modal-card">
      <button type="button" class="btn-close" :aria-label="$t('common.close')" @click="close">
        <i class="ph ph-x"></i>
      </button>

      <div>
        <h2 class="modal-title">{{ $t('shop.checkout.title') }}</h2>
        <div class="modal-sub">{{ title }}</div>
      </div>

      <template v-if="product.kind === 'PHYSICAL'">
        <div v-if="product.max_qty_per_order > 1" class="field">
          <label class="field-label">{{ $t('shop.checkout.quantity') }}</label>
          <input v-model.number="quantity" type="number" min="1" :max="product.max_qty_per_order" />
          <span class="note">{{ $t('shop.card.limit', { count: product.max_qty_per_order }) }}</span>
        </div>

        <div v-if="product.variants.length" class="field">
          <label class="field-label">{{ $t('shop.card.variant') }}</label>
          <select v-model="variant">
            <option value="" disabled>—</option>
            <option v-for="v in product.variants" :key="v.code" :value="v.code">{{ variantTitle(v) }}</option>
          </select>
          <span v-if="fieldErrors.variant" class="field-error">{{ fieldErrors.variant }}</span>
        </div>

        <div class="field">
          <label class="field-label">{{ $t('shop.card.fulfillment') }}</label>
          <select v-model="method">
            <option v-for="m in product.fulfillment_methods" :key="m" :value="m">{{ $t('shop.methods.' + m) }}</option>
          </select>
          <span v-if="fieldErrors.method" class="field-error">{{ fieldErrors.method }}</span>
        </div>

        <div v-if="method === 'PICKUP'" class="field">
          <label class="field-label">{{ $t('shop.checkout.pickupPoint') }}</label>
          <select v-model="pickupPointId">
            <option value="" disabled>—</option>
            <option v-for="p in pickupPoints" :key="p.id" :value="p.id">
              {{ localized(p.title) }} — {{ p.address }}
            </option>
          </select>
          <span v-if="!pickupPoints.length" class="note">{{ $t('shop.checkout.noPickupPoints') }}</span>
          <span v-if="selectedPoint?.hours" class="note">{{ selectedPoint.hours }}</span>
          <span v-if="fieldErrors.pickup_point_id" class="field-error">{{ fieldErrors.pickup_point_id }}</span>
        </div>

        <template v-if="method === 'DELIVERY'">
          <div class="field">
            <AddressAutocomplete v-model="address" :label="$t('shop.checkout.address')" :needs-flat="false" />
            <span v-if="fieldErrors.address" class="field-error">{{ fieldErrors.address }}</span>
          </div>
          <div class="field">
            <label class="field-label">{{ $t('shop.checkout.recipient') }}</label>
            <input v-model="recipient" type="text" autocomplete="name" />
            <span v-if="fieldErrors.recipient" class="field-error">{{ fieldErrors.recipient }}</span>
          </div>
          <div class="field">
            <label class="field-label">{{ $t('shop.checkout.phone') }}</label>
            <input v-model="phone" type="tel" autocomplete="tel" />
            <span v-if="fieldErrors.phone" class="field-error">{{ fieldErrors.phone }}</span>
          </div>
        </template>
      </template>

      <div v-if="quote" class="note">{{ startLine }}</div>

      <div class="rows">
        <div class="row">
          <span class="muted">{{ $t('shop.checkout.total') }}</span>
          <strong>{{ money(total) }}</strong>
        </div>
        <div class="row">
          <span class="muted">{{ $t('shop.checkout.balanceBefore') }}</span>
          <span>{{ balance === null ? '…' : money(balance) }}</span>
        </div>
        <div class="row">
          <span class="muted">{{ $t('shop.checkout.balanceAfter') }}</span>
          <span :class="{ negative: after < 0 }">{{ balance === null ? '…' : money(after) }}</span>
        </div>
      </div>

      <!-- Купить в долг нельзя: не хватает — заявка на пополнение прямо
           отсюда, как на главном экране. -->
      <div v-if="balance !== null && after < 0" class="topup">
        <div class="alert info">{{ $t('shop.checkout.notEnough', { amount: money(-after) }) }}</div>
        <div v-if="topUpSent" class="alert success">{{ topUpSent }}</div>
        <div v-else class="topup-row">
          <input v-model.number="topUpAmount" type="number" min="1" step="1" />
          <button type="button" class="btn-secondary" :disabled="topUpSending || topUpAmount <= 0" @click="requestTopUp">
            {{ $t('shop.checkout.topUp') }}
          </button>
        </div>
      </div>

      <label class="agree">
        <input v-model="agree" type="checkbox" />
        <span>
          {{ $t('shop.checkout.agree') }} —
          <button type="button" class="link" @click="showOffer = true">{{ $t('shop.offerLink') }}</button>
        </span>
      </label>

      <div v-if="error" class="alert error">{{ error }}</div>

      <button type="button" class="btn-primary" :disabled="!canPay" @click="pay">
        <template v-if="submitting">{{ $t('shop.checkout.paying') }}</template>
        <template v-else>{{ $t('shop.checkout.pay', { amount: money(total) }) }}</template>
      </button>
    </div>

    <div v-if="showOffer" class="modal-overlay offer-overlay" @click.self="showOffer = false">
      <div class="modal-card offer-card">
        <button type="button" class="btn-close" :aria-label="$t('common.close')" @click="showOffer = false">
          <i class="ph ph-x"></i>
        </button>
        <h2 class="modal-title">{{ $t('shop.offer.title') }}</h2>
        <div class="modal-sub">{{ $t('shop.offer.edition', { version: offerVersion }) }}</div>
        <OfferText />
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, ref, watch, type PropType } from 'vue'
import { useI18n } from 'vue-i18n'
import api from '../../services/api'
import { useAuthStore } from '../../stores/auth-store'
import AddressAutocomplete, { type StructuredAddress } from '../AddressAutocomplete.vue'
import OfferText from './OfferText.vue'
import {
  getPickupPoints,
  localized as pick,
  newRequestId,
  purchase,
  shopError,
  shopErrorText,
  type Localized,
  type PerkQuote,
  type PickupPoint,
  type ShopProduct,
  type ShopVariant,
} from '../../api/shop'
import { perkStartLine } from '../../utils/perk'

export default defineComponent({
  name: 'CheckoutModal',
  components: { AddressAutocomplete, OfferText },
  props: {
    show: { type: Boolean, default: false },
    product: { type: Object as PropType<ShopProduct | null>, default: null },
    quote: { type: Object as PropType<PerkQuote | undefined>, default: undefined },
    offerVersion: { type: Number, default: 1 },
    currencySymbol: { type: String, default: '₽' },
  },
  emits: ['close', 'purchased', 'price-changed', 'offer-changed'],
  setup(props, { emit }) {
    const { t, locale } = useI18n()
    const authStore = useAuthStore()

    // request_id живёт, пока открыто окно: повторное нажатие и повтор после
    // обрыва сети несут тот же id, и сервер вернёт ту же покупку, а не спишет
    // второй раз.
    const requestId = ref('')
    const quantity = ref(1)
    const variant = ref('')
    const method = ref('')
    const pickupPointId = ref('')
    const pickupPoints = ref<PickupPoint[]>([])
    const address = ref<StructuredAddress | null>(null)
    const recipient = ref('')
    const phone = ref('')
    const agree = ref(false)
    const submitting = ref(false)
    const error = ref('')
    const fieldErrors = ref<Record<string, string>>({})
    const showOffer = ref(false)
    const topUpAmount = ref(0)
    const topUpSending = ref(false)
    const topUpSent = ref('')

    const localized = (value?: Localized) => pick(value, locale.value)
    const title = computed(() => localized(props.product?.title))
    const variantTitle = (v: ShopVariant) => localized(v.title) || v.code
    const money = (value: number) =>
      `${Number(value || 0).toLocaleString('ru-RU', { maximumFractionDigits: 2 })} ${props.currencySymbol}`

    const qty = computed(() => (props.product?.kind === 'PHYSICAL' ? Math.max(1, Math.floor(quantity.value || 1)) : 1))
    const total = computed(() => Math.round((props.product?.price || 0) * qty.value * 100) / 100)
    const balance = computed(() => authStore.balance)
    const after = computed(() => Math.round(((balance.value ?? 0) - total.value) * 100) / 100)
    const selectedPoint = computed(() => pickupPoints.value.find((p) => p.id === pickupPointId.value))
    const startLine = computed(() => (props.quote ? perkStartLine(props.quote.starts_at, props.quote.queued) : ''))

    const canPay = computed(
      () => agree.value && !submitting.value && balance.value !== null && after.value >= 0 && qty.value <= (props.product?.max_qty_per_order || 1),
    )

    const reset = () => {
      requestId.value = newRequestId()
      quantity.value = 1
      variant.value = ''
      method.value = props.product?.fulfillment_methods[0] || ''
      pickupPointId.value = ''
      agree.value = false
      error.value = ''
      fieldErrors.value = {}
      topUpSent.value = ''
      recipient.value = [authStore.user?.first_name, authStore.user?.last_name].filter(Boolean).join(' ')
      phone.value = authStore.phone || ''
    }

    watch(
      () => props.show,
      async (open) => {
        if (!open) return
        reset()
        authStore.fetchMe()
        if (props.product?.kind === 'PHYSICAL' && props.product.fulfillment_methods.includes('PICKUP')) {
          try {
            pickupPoints.value = await getPickupPoints()
          } catch {
            pickupPoints.value = []
          }
        }
      },
      { immediate: true },
    )
    watch(after, (value) => {
      if (value < 0) topUpAmount.value = Math.ceil(-value)
    }, { immediate: true })

    const close = () => {
      if (submitting.value) return
      emit('close')
    }

    const requestTopUp = async () => {
      topUpSending.value = true
      try {
        await api.post('/customer/finances/topup', { amount: topUpAmount.value })
        topUpSent.value = t('shop.checkout.topUpSent')
      } catch (err: any) {
        error.value = shopErrorText(err, t, t('shop.errors.generic'))
      } finally {
        topUpSending.value = false
      }
    }

    const pay = async () => {
      if (!canPay.value || !props.product) return
      submitting.value = true
      error.value = ''
      fieldErrors.value = {}
      try {
        const order = await purchase({
          product_id: props.product.id,
          request_id: requestId.value,
          expected_price: props.product.price,
          offer_version: props.offerVersion,
          quantity: qty.value,
          variant: variant.value || undefined,
          fulfillment:
            props.product.kind === 'PHYSICAL'
              ? {
                  method: method.value as any,
                  pickup_point_id: method.value === 'PICKUP' ? pickupPointId.value || undefined : undefined,
                  address: method.value === 'DELIVERY' ? address.value?.value || '' : undefined,
                  recipient: method.value === 'DELIVERY' ? recipient.value : undefined,
                  phone: method.value === 'DELIVERY' ? phone.value : undefined,
                }
              : undefined,
        })
        emit('purchased', order)
      } catch (err: any) {
        const e = shopError(err)
        switch (e?.error) {
          case 'price_changed': {
            const price = Number(e.details?.price)
            error.value = t('shop.checkout.priceChanged', { price: money(price) })
            emit('price-changed', price)
            break
          }
          case 'offer_changed':
            // Новая редакция: согласие снимается и текст показывается заново.
            agree.value = false
            error.value = t('shop.checkout.offerChanged')
            emit('offer-changed', Number(e.details?.offer_version) || props.offerVersion + 1)
            showOffer.value = true
            break
          case 'validation':
            fieldErrors.value = e.fields || {}
            error.value = shopErrorText(err, t, t('shop.errors.generic'))
            break
          default:
            // Обрыв сети без ответа сервера: request_id остаётся прежним, и
            // повторное нажатие не создаст вторую покупку.
            error.value = shopErrorText(err, t, t('shop.errors.generic'))
        }
      } finally {
        submitting.value = false
      }
    }

    return {
      quantity, variant, method, pickupPointId, pickupPoints, address, recipient, phone, agree,
      submitting, error, fieldErrors, showOffer, topUpAmount, topUpSending, topUpSent,
      title, variantTitle, localized, money, total, balance, after, selectedPoint, startLine, canPay,
      close, pay, requestTopUp,
    }
  },
})
</script>

<style scoped src="./shop-modal.css"></style>
<style scoped>
.negative {
  color: #dc2626;
  font-weight: 600;
}
.topup {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.topup-row {
  display: flex;
  gap: 8px;
}
.topup-row input {
  flex: 1;
  min-width: 0;
  padding: 11px 12px;
  border-radius: 12px;
  border: 1px solid rgba(0, 0, 0, 0.12);
  background: #f8fafc;
  font: inherit;
}
.agree {
  display: flex;
  gap: 10px;
  align-items: flex-start;
  font-size: 14px;
  line-height: 1.4;
  cursor: pointer;
}
.agree input {
  margin-top: 3px;
  width: 18px;
  height: 18px;
  flex-shrink: 0;
}
.offer-overlay {
  z-index: 1060;
}
.offer-card {
  max-width: 640px;
}
</style>
