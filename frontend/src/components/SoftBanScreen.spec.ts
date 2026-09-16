import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from '../stores/auth-store'
import { isSoftBanError, SOFT_BANNED_CODE } from '../services/api'

const route = { path: '/executor' }
vi.mock('vue-router', () => ({
  useRoute: () => route,
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
}))
vi.mock('../services/api', async (orig) => {
  const actual: any = await orig()
  return { ...actual, default: { get: vi.fn().mockResolvedValue({ data: { soft_ban_reason: 'рецидив' } }) } }
})

import SoftBanScreen from './SoftBanScreen.vue'

const stubs = { SupportChatModal: true }

beforeEach(() => {
  localStorage.clear()
  localStorage.setItem('token', 'x.eyJzdWIiOiJ1MSJ9.y')
  setActivePinia(createPinia())
  route.path = '/executor'
})

describe('SoftBanScreen', () => {
  it('скрыт у активного пользователя', () => {
    const store = useAuthStore()
    store.user = { status: 'ACTIVE' } as any
    const wrapper = mount(SoftBanScreen, { global: { stubs } })
    expect(wrapper.text()).toBe('')
  })

  it('показывается по статусу SOFT_BANNED и с причиной', async () => {
    const store = useAuthStore()
    store.user = { status: 'SOFT_BANNED', balance: 150 } as any
    const wrapper = mount(SoftBanScreen, { global: { stubs } })
    await flushPromises()
    expect(wrapper.text()).toContain('Аккаунт заблокирован')
    expect(wrapper.text()).toContain('рецидив')
  })

  it('показывается по отказу сервера до загрузки профиля и не мешает почте', async () => {
    const store = useAuthStore()
    store.reportSoftBan()
    const wrapper = mount(SoftBanScreen, { global: { stubs } })
    expect(wrapper.text()).toContain('Аккаунт заблокирован')

    route.path = '/mail'
    const onMail = mount(SoftBanScreen, { global: { stubs } })
    expect(onMail.text()).toBe('')
  })

  it('сворачивается ради текущих заказов', async () => {
    const store = useAuthStore()
    store.user = { status: 'SOFT_BANNED' } as any
    const wrapper = mount(SoftBanScreen, { global: { stubs } })
    const button = wrapper.findAll('button').find((b) => b.text().includes('Текущие заказы'))
    await button!.trigger('click')
    expect(wrapper.text()).toBe('')
  })
})

describe('isSoftBanError', () => {
  it('узнаёт отказ мягкого бана', () => {
    expect(isSoftBanError({ response: { status: 403, data: { error: SOFT_BANNED_CODE } } })).toBe(true)
    expect(isSoftBanError({ response: { status: 403, data: 'Forbidden' } })).toBe(false)
  })
})
