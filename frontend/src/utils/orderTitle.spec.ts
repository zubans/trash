import { describe, it, expect } from 'vitest'
import { orderTitle, orderTitleLine } from './orderTitle'

const variant: any = { id: 'v1', parent_id: 'c1', code: 'BASEMENT', name: { ru: 'Стандартная цокольная' }, node_type: 'VARIANT' }
const category: any = { id: 'c1', code: 'CLEANING', name: { ru: 'Уборка' }, node_type: 'CATEGORY' }

describe('orderTitle', () => {
  it('puts the category on top and the service underneath', () => {
    const got = orderTitle({ service_variant: variant }, { categories: [category] })
    expect(got).toEqual({ title: 'Уборка', subtitle: 'Стандартная цокольная' })
  })

  it('resolves a variant carried only as an id', () => {
    const got = orderTitle(
      { service_variant_id: 'v1' },
      { variants: { v1: variant }, categories: [category] },
    )
    expect(got).toEqual({ title: 'Уборка', subtitle: 'Стандартная цокольная' })
  })

  it('promotes the service to the title when no category resolves', () => {
    const got = orderTitle({ service_variant: variant }, {})
    expect(got).toEqual({ title: 'Стандартная цокольная', subtitle: '' })
  })

  it('never leaves the title blank', () => {
    expect(orderTitle({}, {})).toEqual({ title: 'Заказ', subtitle: '' })
  })

  it('does not repeat itself when category and service share a name', () => {
    const same: any = { ...variant, name: { ru: 'Уборка' } }
    const got = orderTitle({ service_variant: same }, { categories: [category] })
    expect(got).toEqual({ title: 'Уборка', subtitle: '' })
  })

  it('joins both parts on one line for tables', () => {
    expect(orderTitleLine({ service_variant: variant }, { categories: [category] }))
      .toBe('Уборка · Стандартная цокольная')
  })
})
