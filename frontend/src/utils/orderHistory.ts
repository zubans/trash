// История заказов — закрытые заказы, сгруппированные по месяцам. Логика
// отделена от страницы, чтобы исполнитель и заказчик видели одно и то же и
// чтобы её можно было проверить без монтирования экрана.

export type HistoryFilter = 'all' | 'completed' | 'canceled'

export interface HistoryOrder {
  id: string
  status: string
  created_at?: string
  completed_at?: string
  canceled_at?: string
  final_amount?: number | string | null
  hold_amount?: number | string | null
  [key: string]: any
}

export interface HistoryGroup<T extends HistoryOrder = HistoryOrder> {
  /** YYYY-MM, ключ группы. */
  key: string
  /** «Сентябрь 2026». */
  label: string
  orders: T[]
}

export interface HistorySummary {
  completed: number
  canceled: number
  /** Сумма выполненных заказов. Отменённые не считаются: денег по ним не было. */
  completedAmount: number
}

const CLOSED = ['COMPLETED', 'CANCELED']

const MONTHS = [
  'Январь', 'Февраль', 'Март', 'Апрель', 'Май', 'Июнь',
  'Июль', 'Август', 'Сентябрь', 'Октябрь', 'Ноябрь', 'Декабрь',
]

/** Когда заказ закрылся: завершён, отменён или, если дат нет, создан. */
export const closedAt = (order: HistoryOrder): string =>
  order.completed_at || order.canceled_at || order.created_at || ''

export const orderAmount = (order: HistoryOrder): number =>
  Number(order.final_amount ?? order.hold_amount ?? 0) || 0

/** В историю попадают только закрытые заказы: остальные живут на главном экране. */
export const closedOrders = <T extends HistoryOrder>(orders: T[]): T[] =>
  orders.filter((o) => CLOSED.includes(o.status))

export const filterHistory = <T extends HistoryOrder>(orders: T[], filter: HistoryFilter): T[] => {
  const closed = closedOrders(orders)
  if (filter === 'completed') return closed.filter((o) => o.status === 'COMPLETED')
  if (filter === 'canceled') return closed.filter((o) => o.status === 'CANCELED')
  return closed
}

export const summarizeHistory = (orders: HistoryOrder[]): HistorySummary => {
  const summary: HistorySummary = { completed: 0, canceled: 0, completedAmount: 0 }
  for (const o of closedOrders(orders)) {
    if (o.status === 'COMPLETED') {
      summary.completed++
      summary.completedAmount += orderAmount(o)
    } else {
      summary.canceled++
    }
  }
  return summary
}

/** Группы по месяцу закрытия, новые сверху; внутри месяца — тоже новые сверху. */
export const groupHistoryByMonth = <T extends HistoryOrder>(orders: T[]): HistoryGroup<T>[] => {
  const sorted = orders
    .slice()
    .sort((a, b) => new Date(closedAt(b)).getTime() - new Date(closedAt(a)).getTime())
  const groups: HistoryGroup<T>[] = []
  for (const order of sorted) {
    const date = new Date(closedAt(order))
    const valid = !isNaN(date.getTime())
    const key = valid ? `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}` : 'unknown'
    let group = groups[groups.length - 1]
    if (!group || group.key !== key) {
      group = { key, label: valid ? `${MONTHS[date.getMonth()]} ${date.getFullYear()}` : 'Без даты', orders: [] }
      groups.push(group)
    }
    group.orders.push(order)
  }
  return groups
}
