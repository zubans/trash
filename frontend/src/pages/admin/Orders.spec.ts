import { describe, it, expect, beforeEach, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import Orders from './Orders.vue'
import api from '../../services/api'

// Подменяется только транспорт: разбор ошибок (formatApiError) — настоящий.
vi.mock('../../services/api', async (orig) => ({
  ...(await orig<typeof import('../../services/api')>()),
  default: { get: vi.fn(), post: vi.fn() },
}))

const permissions = new Set<string>()
vi.mock('../../stores/auth-store', () => ({
  useAuthStore: () => ({ currency: 'RUB', can: (p: string) => permissions.has(p) }),
}))

const replace = vi.fn()
const route = { query: {} as Record<string, string> }
vi.mock('vue-router', () => ({
  useRoute: () => route,
  useRouter: () => ({ replace }),
}))

const mockedApi = api as unknown as { get: ReturnType<typeof vi.fn>; post: ReturnType<typeof vi.fn> }

const row = (id: string, status: string) => ({
  id,
  status,
  customer_phone: '+79990000000',
  service_variant_name: 'Вынос мусора',
  final_amount: 100,
  created_at: '2026-09-01T10:00:00Z',
})

const mountPage = async (rows: any[]) => {
  mockedApi.get.mockResolvedValue({ data: { orders: rows, total: rows.length, services: [], periods: [] } })
  const wrapper = mount(Orders, { global: { mocks: { $t: (k: string) => k } } })
  await flushPromises()
  return wrapper
}

describe('Orders (admin)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    permissions.clear()
    route.query = {}
  })

  it('запрашивает группу статусов из адреса, по умолчанию — активные', async () => {
    await mountPage([])
    expect(mockedApi.get).toHaveBeenCalledWith('/admin/orders', expect.objectContaining({
      params: expect.objectContaining({ status: 'active' }),
    }))

    vi.clearAllMocks()
    route.query = { status: 'review' }
    await mountPage([])
    expect(mockedApi.get).toHaveBeenCalledWith('/admin/orders', expect.objectContaining({
      params: expect.objectContaining({ status: 'review' }),
    }))
  })

  it('кнопка «Вернуть в работу» — только у заказа на проверке и только с правом orders.edit', async () => {
    permissions.add('orders.edit')
    const wrapper = await mountPage([row('a', 'EXECUTED'), row('b', 'ASSIGNED'), row('c', 'COMPLETED')])

    const buttons = wrapper.findAll('.btn-return')
    expect(buttons).toHaveLength(1)
    expect(wrapper.text()).toContain('на проверке')

    permissions.clear()
    const readOnly = await mountPage([row('a', 'EXECUTED')])
    expect(readOnly.findAll('.btn-return')).toHaveLength(0)
  })

  it('возвращает заказ в работу после подтверждения и перечитывает список', async () => {
    permissions.add('orders.edit')
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    mockedApi.post.mockResolvedValue({})
    const wrapper = await mountPage([row('a', 'EXECUTED')])
    mockedApi.get.mockClear()

    await wrapper.find('.btn-return').trigger('click')
    await flushPromises()

    expect(mockedApi.post).toHaveBeenCalledWith('/admin/orders/a/return-to-work')
    expect(mockedApi.get).toHaveBeenCalledWith('/admin/orders', expect.anything())
    expect(wrapper.text()).toContain('Заказ возвращён в работу.')
  })

  // Счётчик и фасеты сервер отдаёт только с первой страницей (offset = 0):
  // раньше страница обнуляла их на каждом листании, и со второй страницы
  // пропадали и «51–100 из N», и списки фильтров.
  it('при листании сохраняет счётчик и фасеты первой страницы', async () => {
    mockedApi.get.mockResolvedValueOnce({
      data: {
        orders: Array.from({ length: 50 }, (_, i) => row(`p1-${i}`, 'ASSIGNED')),
        total: 120,
        services: ['Вынос мусора', 'Уборка'],
        periods: ['2026-09'],
      },
    })
    const wrapper = mount(Orders, { global: { mocks: { $t: (k: string) => k } } })
    await flushPromises()
    expect(wrapper.text()).toContain('1–50 из 120')

    // Вторая страница: только строки, без total/services/periods.
    mockedApi.get.mockResolvedValueOnce({
      data: { orders: Array.from({ length: 50 }, (_, i) => row(`p2-${i}`, 'ASSIGNED')) },
    })
    await wrapper.findAll('.page-btn')[1].trigger('click')
    await flushPromises()

    const params = mockedApi.get.mock.calls[1][1].params
    expect(params.offset).toBe(50)
    // Свежий счётчик на простом листании не просим: он есть с первой страницы.
    expect(params.total).toBeUndefined()
    expect(wrapper.text()).toContain('51–100 из 120')
    const serviceOptions = wrapper.findAll('select')[1].findAll('option').map((o) => o.text())
    expect(serviceOptions).toEqual(['Все услуги', 'Вынос мусора', 'Уборка'])
    expect(wrapper.findAll('select')[2].findAll('option')).toHaveLength(2)
  })

  it('после возврата в работу на дальней странице просит свежий счётчик', async () => {
    permissions.add('orders.edit')
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    mockedApi.post.mockResolvedValue({})
    mockedApi.get.mockResolvedValueOnce({
      data: { orders: [row('a', 'EXECUTED')], total: 60, services: [], periods: [] },
    })
    const wrapper = mount(Orders, { global: { mocks: { $t: (k: string) => k } } })
    await flushPromises()
    mockedApi.get.mockResolvedValueOnce({ data: { orders: [row('b', 'EXECUTED')] } })
    await wrapper.findAll('.page-btn')[1].trigger('click')
    await flushPromises()

    mockedApi.get.mockResolvedValueOnce({ data: { orders: [], total: 50 } })
    await wrapper.find('.btn-return').trigger('click')
    await flushPromises()

    const calls = mockedApi.get.mock.calls
    const params = calls[calls.length - 1][1].params
    expect(params).toEqual(expect.objectContaining({ offset: 50, total: 1 }))
    expect(params.facets).toBeUndefined()
  })

  it('отказ возврата в работу показывает текст сервера, а 500 — понятное сообщение', async () => {
    permissions.add('orders.edit')
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const wrapper = await mountPage([row('a', 'EXECUTED')])

    mockedApi.post.mockRejectedValueOnce({
      response: { status: 409, data: 'вернуть в работу можно только заказ на проверке' },
    })
    await wrapper.find('.btn-return').trigger('click')
    await flushPromises()
    expect(wrapper.find('.action-msg').text()).toBe('вернуть в работу можно только заказ на проверке')

    mockedApi.post.mockRejectedValueOnce({ response: { status: 500, data: 'internal error' } })
    await wrapper.find('.btn-return').trigger('click')
    await flushPromises()
    const text = wrapper.find('.action-msg').text()
    expect(text).not.toContain('internal error')
    expect(text).toBe('Не удалось вернуть заказ в работу: сбой на сервере, попробуйте позже')
  })
})
