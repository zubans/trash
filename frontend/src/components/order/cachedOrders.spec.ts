import { describe, expect, it } from 'vitest'
import { acceptPresentedOrders } from './cachedOrders'

describe('acceptPresentedOrders', () => {
  it('принимает список, собранный сервером под смотрящего', () => {
    expect(acceptPresentedOrders([{ id: '1', actions: { cancel: false, reject: false, review: false } }])).toBe(true)
  })

  it('принимает пустой список', () => {
    expect(acceptPresentedOrders([])).toBe(true)
  })

  it('отбрасывает список, сохранённый до появления actions', () => {
    expect(acceptPresentedOrders([{ id: '1', actions: {} }, { id: '2' }])).toBe(false)
  })

  it('отбрасывает запись, которая не является списком', () => {
    expect(acceptPresentedOrders({ orders: [] })).toBe(false)
    expect(acceptPresentedOrders(null)).toBe(false)
  })
})
