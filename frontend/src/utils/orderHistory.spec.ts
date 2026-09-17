import { describe, it, expect } from 'vitest'
import { filterHistory, groupHistoryByMonth, summarizeHistory } from './orderHistory'

const orders = [
  { id: 'active', status: 'ASSIGNED', created_at: '2026-09-10T10:00:00Z', final_amount: 999 },
  { id: 'review', status: 'EXECUTED', created_at: '2026-09-10T10:00:00Z', final_amount: 999 },
  { id: 'sep-done', status: 'COMPLETED', completed_at: '2026-09-12T10:00:00Z', final_amount: 300 },
  { id: 'sep-cancel', status: 'CANCELED', canceled_at: '2026-09-14T10:00:00Z', hold_amount: 500 },
  { id: 'aug-done', status: 'COMPLETED', completed_at: '2026-08-20T10:00:00Z', final_amount: 200 },
]

describe('orderHistory', () => {
  it('берёт только закрытые заказы и фильтрует по статусу', () => {
    expect(filterHistory(orders, 'all').map((o) => o.id)).toEqual(['sep-done', 'sep-cancel', 'aug-done'])
    expect(filterHistory(orders, 'completed').map((o) => o.id)).toEqual(['sep-done', 'aug-done'])
    expect(filterHistory(orders, 'canceled').map((o) => o.id)).toEqual(['sep-cancel'])
  })

  it('группирует по месяцу закрытия, новые сверху', () => {
    const groups = groupHistoryByMonth(filterHistory(orders, 'all'))
    expect(groups.map((g) => g.label)).toEqual(['Сентябрь 2026', 'Август 2026'])
    expect(groups[0].orders.map((o) => o.id)).toEqual(['sep-cancel', 'sep-done'])
  })

  it('считает сумму только по выполненным', () => {
    expect(summarizeHistory(orders)).toEqual({ completed: 2, canceled: 1, completedAmount: 500 })
  })
})
