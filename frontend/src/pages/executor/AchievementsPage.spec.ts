import { describe, it, expect, beforeEach, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AchievementsPage from './AchievementsPage.vue'
import PerkBadge from '../../components/shop/PerkBadge.vue'
import api from '../../services/api'

// Подменяется только транспорт: обёртки api/shop и api/achievements — настоящие.
vi.mock('../../services/api', async (orig) => ({
  ...(await orig<typeof import('../../services/api')>()),
  default: { get: vi.fn() },
}))

vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))

const mockedApi = api as unknown as { get: ReturnType<typeof vi.fn> }

const perk = {
  id: 'perk-1',
  user_id: 'u1',
  rule_code: 'half_commission',
  rule_title: 'Комиссия вдвое меньше',
  config: {},
  starts_at: '2026-10-01T00:00:00Z',
  expires_at: '2026-10-08T00:00:00Z',
  created_at: '2026-09-20T00:00:00Z',
}

describe('AchievementsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  // GET /executor/level удалён: уровень и очередь привилегий приходят одним
  // GET /me/perks, а level в нём — полный service.Level.
  it('берёт уровень и очередь привилегий из GET /me/perks', async () => {
    mockedApi.get.mockImplementation(async (url: string) => {
      if (url === '/executor/achievements') return { data: [] }
      if (url === '/me/perks') {
        return {
          data: {
            level: {
              points: 12,
              level: 3,
              next_level_points: 16,
              base_percent: 10,
              discount_pp: 3,
              percent: 7,
              max_useful_level: 10,
              level_percent: 7,
            },
            queue: [perk],
          },
        }
      }
      throw new Error(`неожиданный запрос ${url}`)
    })

    const wrapper = mount(AchievementsPage, { global: { stubs: { PerkBadge: true } } })
    await flushPromises()

    const urls = mockedApi.get.mock.calls.map((call) => call[0])
    expect(urls).toContain('/me/perks')
    expect(urls).not.toContain('/executor/level')

    expect(wrapper.find('.level-title').text()).toBe('Уровень 3')
    expect(wrapper.find('.level-sub').text()).toBe('12 баллов')
    expect(wrapper.find('.commission-value').text()).toBe('7%')
    expect(wrapper.find('.commission-was').text()).toContain('10%')

    const badge = wrapper.findComponent(PerkBadge)
    expect(badge.props('queue')).toEqual([perk])
    expect(badge.props('level')).toEqual(expect.objectContaining({ level: 3, percent: 7 }))
  })

  it('без уровня в ответе показывает нулевой, а не ломается', async () => {
    mockedApi.get.mockImplementation(async (url: string) =>
      url === '/me/perks' ? { data: {} } : { data: [] },
    )
    const wrapper = mount(AchievementsPage, { global: { stubs: { PerkBadge: true } } })
    await flushPromises()
    expect(wrapper.find('.level-title').text()).toBe('Уровень 0')
    expect(wrapper.findComponent(PerkBadge).props('queue')).toEqual([])
  })
})
