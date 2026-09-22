import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { i18n } from '../../i18n'
import PerkQuoteBlock from './PerkQuoteBlock.vue'
import type { PerkQuote, ShopProduct } from '../../api/shop'

const product = (rule: string, days = 30): ShopProduct => ({
  id: 'p', kind: 'PERK', category: 'perks', title: { ru: 'x' }, description: {}, images: [], price: 1000,
  roles: [], requires_verified: false, max_qty_per_order: 1, variants: [], fulfillment_methods: [],
  perk_rule: rule, perk_config: {}, perk_days: days, sort_order: 0, is_active: true, in_stock: true,
})

const quote = (withPerk: number, extra: Partial<PerkQuote> = {}): PerkQuote => ({
  base_percent: 10, level_percent: 7, current_percent: 7, percent_with_perk: withPerk,
  commission_paid: 2400, savings: 1200, breakeven_turnover: 28571, starts_at: '2026-10-03T09:00:00Z',
  expires_at: '2026-11-02T09:00:00Z', queued: false, queue_length: 0, max_queued: 3, useless: false, ...extra,
})

const render = (p: ShopProduct, q: PerkQuote) =>
  mount(PerkQuoteBlock, { props: { product: p, quote: q }, global: { plugins: [i18n] } })

describe('PerkQuoteBlock', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'ru'
  })

  // Ставку считает правило на сервере; карточка показывает результат, не
  // зная, какое это правило.
  it('shows the rate from the level rate to the one with the perk', () => {
    const w = render(product('commission_multiplier'), quote(3.5))
    expect(w.find('[data-test=formula]').text()).toBe('7 % → 3.5 %')
    expect(w.find('[data-test=with-perk]').text()).toBe('3.5 %')
  })

  it('counts savings over the term of the perk', () => {
    const w = render(product('commission_free', 1), quote(0, { savings: 80 }))
    expect(w.find('[data-test=formula]').text()).toBe('7 % → 0 %')
    expect(w.text()).toContain('80')
    expect(w.text()).toContain('1 дн.')
  })

  it('says the perk starts later when the queue is not empty', () => {
    const w = render(product('commission_multiplier'), quote(3.5, { queued: true }))
    expect(w.find('[data-test=start]').text()).toBe('Начнёт действовать 3 октября')
  })

  it('separates the level rate from the current one while another perk applies', () => {
    const w = render(product('commission_free'), quote(0, { current_percent: 5, queued: true }))
    expect(w.text()).toContain('По уровню')
    expect(w.find('[data-test=current]').text()).toBe('Сейчас действует привилегия: 5 %')
  })

  it('says the perk starts right away when nothing is queued', () => {
    const w = render(product('commission_multiplier'), quote(3.5))
    expect(w.find('[data-test=start]').text()).toBe('Начнёт действовать сразу после оплаты')
  })
})
