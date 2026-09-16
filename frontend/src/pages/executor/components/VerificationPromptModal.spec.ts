import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import VerificationPromptModal from './VerificationPromptModal.vue'
import { i18n, setLocale } from '../../../i18n'
import api from '../../../services/api'

vi.mock('../../../services/api', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
  },
}))

const mockedApi = api as unknown as { get: ReturnType<typeof vi.fn>; post: ReturnType<typeof vi.fn> }

const mountModal = (show = true) =>
  mount(VerificationPromptModal, {
    props: { show },
    global: { plugins: [i18n] },
  })

describe('VerificationPromptModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    setLocale('ru')
  })

  it('не рисуется, пока show = false', () => {
    const wrapper = mountModal(false)

    expect(wrapper.find('.verification-overlay').exists()).toBe(false)
  })

  it('показывает предложение верифицироваться и обе кнопки (ru)', () => {
    const wrapper = mountModal()

    expect(wrapper.text()).toContain('Пройдите верификацию')
    expect(wrapper.text()).toContain('больше интересных заказов')
    expect(wrapper.find('.verification-btn-primary').text()).toContain('Верифицироваться')
    expect(wrapper.find('.verification-btn-secondary').text()).toContain('Позже')
  })

  it('те же тексты доступны и в английской локали', () => {
    setLocale('en')
    const wrapper = mountModal()

    expect(wrapper.text()).toContain('Get verified')
    expect(wrapper.text()).toContain('more interesting orders')
    expect(wrapper.find('.verification-btn-secondary').text()).toContain('Later')
  })

  it('кнопка «Позже» закрывает окно без заявки', async () => {
    const wrapper = mountModal()

    await wrapper.find('.verification-btn-secondary').trigger('click')

    expect(wrapper.emitted('update:show')).toEqual([[false]])
    expect(wrapper.emitted('close')).toBeTruthy()
    expect(wrapper.emitted('created')).toBeFalsy()
    expect(mockedApi.post).not.toHaveBeenCalled()
  })

  it('крестик закрывает окно без заявки', async () => {
    const wrapper = mountModal()

    await wrapper.find('.verification-close').trigger('click')

    expect(wrapper.emitted('update:show')).toEqual([[false]])
    expect(mockedApi.post).not.toHaveBeenCalled()
  })

  it('при заполненном профиле «Верифицироваться» сразу создаёт заказ', async () => {
    const order = { id: 'order-1', address: 'Москва, Арбат, 10' }
    mockedApi.post.mockResolvedValue({ data: order })
    const wrapper = mountModal()

    await wrapper.find('.verification-btn-primary').trigger('click')
    await vi.waitFor(() => expect(wrapper.emitted('created')).toBeTruthy())

    expect(mockedApi.post).toHaveBeenCalledWith('/executor/verification', {})
    expect(wrapper.emitted('created')![0]).toEqual([order])
    expect(wrapper.text()).toContain('Заявка создана')
    expect(wrapper.text()).toContain('Москва, Арбат, 10')
    expect(wrapper.emitted('update:show')).toBeFalsy()
  })

  it('без адреса просит дозаполнить только его', async () => {
    mockedApi.post.mockRejectedValue({ response: { status: 422, data: { missing: ['address'] } } })
    const wrapper = mountModal()

    await wrapper.find('.verification-btn-primary').trigger('click')
    await vi.waitFor(() => expect(wrapper.text()).toContain('Заполните данные'))

    expect(wrapper.find('.address-field').exists()).toBe(true)
    expect(wrapper.find('input[name="last_name"]').exists()).toBe(false)
    expect(wrapper.find('input[name="birth_date"]').exists()).toBe(false)
    expect(wrapper.find('.verification-error').exists()).toBe(false)
    expect(wrapper.emitted('created')).toBeFalsy()
  })

  it('отправляет дозаполненные данные и создаёт заказ', async () => {
    mockedApi.post
      .mockRejectedValueOnce({ response: { status: 422, data: { missing: ['patronymic', 'birth_date'] } } })
      .mockResolvedValueOnce({ data: { id: 'order-1' } })
    const wrapper = mountModal()

    await wrapper.find('.verification-btn-primary').trigger('click')
    await vi.waitFor(() => expect(wrapper.find('input[name="patronymic"]').exists()).toBe(true))
    await wrapper.find('input[name="patronymic"]').setValue('Иванович')
    await wrapper.find('input[name="birth_date"]').setValue('1990-03-14')
    await wrapper.find('.verification-btn-primary').trigger('click')
    await vi.waitFor(() => expect(wrapper.emitted('created')).toBeTruthy())

    expect(mockedApi.post).toHaveBeenLastCalledWith('/executor/verification', {
      patronymic: 'Иванович',
      birth_date: '1990-03-14',
    })
  })

  it('при ошибке показывает сообщение и не закрывается', async () => {
    mockedApi.post.mockRejectedValue(new Error('network down'))
    const wrapper = mountModal()

    await wrapper.find('.verification-btn-primary').trigger('click')
    await vi.waitFor(() => expect(wrapper.find('.verification-error').exists()).toBe(true))

    expect(wrapper.find('.verification-error').text()).toContain('Попробуйте ещё раз')
    expect(wrapper.emitted('created')).toBeFalsy()
    expect(wrapper.emitted('update:show')).toBeFalsy()
  })

  it('с открытой заявкой показывает её и позволяет отменить', async () => {
    mockedApi.post.mockResolvedValue({})
    const wrapper = mount(VerificationPromptModal, {
      props: { show: true, order: { id: 'order-1', address: 'Москва, Арбат, 10' } },
      global: { plugins: [i18n] },
    })

    expect(wrapper.text()).toContain('Заявка создана')
    await wrapper.find('.verification-btn-secondary').trigger('click')
    await vi.waitFor(() => expect(wrapper.emitted('cancelled')).toBeTruthy())

    expect(mockedApi.post).toHaveBeenCalledWith('/executor/verification/cancel')
    expect(wrapper.emitted('update:show')).toEqual([[false]])
  })
})
