import { describe, it, expect } from 'vitest'
import { toSettingsPayload } from './settingsPayload'

describe('toSettingsPayload', () => {
  // Регрессия: Vue возвращает число для любого `<input type="number">`,
  // который правил админ, а API настроек принимает только строки.
  it('stringifies numbers produced by number inputs', () => {
    expect(toSettingsPayload({ order_commission_percent: 15 })).toEqual({
      order_commission_percent: '15',
    })
    expect(toSettingsPayload({ asap_tariff_coeff: 8.5 })).toEqual({
      asap_tariff_coeff: '8.5',
    })
  })

  it('leaves strings alone', () => {
    expect(toSettingsPayload({ currency: 'RUB', show_unverified_customer_orders: '0' })).toEqual({
      currency: 'RUB',
      show_unverified_customer_orders: '0',
    })
  })

  it('sends an empty value rather than the words null or undefined', () => {
    expect(toSettingsPayload({ a: null, b: undefined })).toEqual({ a: '', b: '' })
  })
})
