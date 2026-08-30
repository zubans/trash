import { describe, it, expect } from 'vitest'
import { toSettingsPayload } from './settingsPayload'

describe('toSettingsPayload', () => {
  // The regression: Vue hands back a number for any `<input type="number">`
  // the admin edited, and the settings API only accepts strings.
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
