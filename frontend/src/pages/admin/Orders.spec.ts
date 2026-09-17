import { describe, it, expect, beforeEach, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import Orders from './Orders.vue'
import api from '../../services/api'

vi.mock('../../services/api', () => ({
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
})
