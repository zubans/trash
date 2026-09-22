<template>
  <div class="quote">
    <div class="columns">
      <!-- Пока действует другая привилегия, «сейчас» и «по уровню» — разные
           числа. Новая начнётся после неё и применится к ставке по уровню. -->
      <div class="column">
        <div class="label">{{ hasActivePerk ? $t('shop.perk.byLevel') : $t('shop.perk.now') }}</div>
        <div class="value">{{ formatPercent(quote.level_percent) }} %</div>
        <div v-if="hasActivePerk" class="hint muted-hint" data-test="current">
          {{ $t('shop.perk.currentWithPerk', { percent: formatPercent(quote.current_percent) }) }}
        </div>
      </div>
      <i class="ph-bold ph-arrow-right arrow"></i>
      <div class="column accent">
        <div class="label">{{ $t('shop.perk.withPerk') }}</div>
        <div class="value" data-test="with-perk">{{ formatPercent(quote.percent_with_perk) }} %</div>
        <div class="hint">{{ $t('shop.perk.forDays', { days: product.perk_days }) }}</div>
      </div>
    </div>

    <div class="formula" data-test="formula">
      {{ perkFormula(quote.level_percent, quote.percent_with_perk) }}
    </div>

    <div class="payback">
      <div class="label">{{ $t('shop.perk.payback') }}</div>
      <p>{{ $t('shop.perk.paid30', { paid: money(quote.commission_paid) }) }}</p>
      <p v-if="quote.savings > 0">{{ $t('shop.perk.savings', { savings: money(quote.savings), days: product.perk_days }) }}</p>
      <p v-if="quote.breakeven_turnover" data-test="breakeven">
        {{ $t('shop.perk.breakeven', { amount: money(quote.breakeven_turnover) }) }}
      </p>
    </div>

    <div class="start" data-test="start">{{ perkStartLine(quote.starts_at, quote.queued) }}</div>
    <div class="note">{{ $t('shop.perk.atConfirmation') }}</div>
    <div class="note">{{ $t('shop.perk.notApplied') }}</div>
  </div>
</template>

<script lang="ts">
import { computed, defineComponent, type PropType } from 'vue'
import type { PerkQuote, ShopProduct } from '../../api/shop'
import { formatPercent, perkFormula, perkStartLine } from '../../utils/perk'

// «Сейчас / с привилегией / окупаемость» (implementation_plan_shop.md §3.6).
// Все числа — из ответа сервера: ставку по уровню, ставку с привилегией и
// экономию считает та же формула, что закрывает заказы.
export default defineComponent({
  name: 'PerkQuoteBlock',
  props: {
    quote: { type: Object as PropType<PerkQuote>, required: true },
    product: { type: Object as PropType<ShopProduct>, required: true },
    currencySymbol: { type: String, default: '₽' },
  },
  setup(props) {
    const money = (value: number) =>
      `${Number(value || 0).toLocaleString('ru-RU', { maximumFractionDigits: 0 })} ${props.currencySymbol}`
    const hasActivePerk = computed(() => props.quote.current_percent !== props.quote.level_percent)
    return { money, formatPercent, perkFormula, perkStartLine, hasActivePerk }
  },
})
</script>

<style scoped>
.quote {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.columns {
  display: flex;
  align-items: center;
  gap: 12px;
}
.column {
  flex: 1;
  background: #f8fafc;
  border-radius: 12px;
  padding: 10px 12px;
}
.column.accent {
  background: #ecfdf5;
}
.arrow {
  color: #94a3b8;
}
.label {
  font-size: 12px;
  font-weight: 600;
  color: #64748b;
}
.value {
  font-size: 22px;
  font-weight: 700;
  color: #0f172a;
}
.hint {
  font-size: 12px;
  color: #047857;
}
.muted-hint {
  color: #64748b;
}
.formula {
  font-size: 14px;
  font-weight: 600;
  color: #334155;
}
.payback p {
  margin: 4px 0 0;
  font-size: 14px;
  line-height: 1.45;
  color: #334155;
}
.start {
  font-size: 14px;
  font-weight: 600;
  color: #4338ca;
}
.note {
  font-size: 12px;
  line-height: 1.4;
  color: #64748b;
}
</style>
