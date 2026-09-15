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

  it('кнопка «Позже» закрывает окно без заявки в поддержку', async () => {
    const wrapper = mountModal()

    await wrapper.find('.verification-btn-secondary').trigger('click')

    expect(wrapper.emitted('update:show')).toEqual([[false]])
    expect(wrapper.emitted('close')).toBeTruthy()
    expect(wrapper.emitted('sent')).toBeFalsy()
    expect(mockedApi.post).not.toHaveBeenCalled()
  })

  it('крестик закрывает окно без заявки в поддержку', async () => {
    const wrapper = mountModal()

    await wrapper.find('.verification-close').trigger('click')

    expect(wrapper.emitted('update:show')).toEqual([[false]])
    expect(mockedApi.post).not.toHaveBeenCalled()
  })

  it('кнопка «Верифицироваться» шлёт просьбу в чат поддержки и сообщает об отправке', async () => {
    mockedApi.get.mockResolvedValue({ data: { id: 'chat-1' } })
    mockedApi.post.mockResolvedValue({ data: { id: 'msg-1' } })
    const wrapper = mountModal()

    await wrapper.find('.verification-btn-primary').trigger('click')
    await vi.waitFor(() => expect(wrapper.emitted('sent')).toBeTruthy())

    expect(mockedApi.get).toHaveBeenCalledWith('/support/chat')
    expect(mockedApi.post).toHaveBeenCalledWith('/support/chats/chat-1/messages', {
      text: 'Здравствуйте! Прошу верифицировать мой аккаунт.',
    })
    expect(wrapper.emitted('update:show')).toEqual([[false]])
  })

  it('текст заявки в поддержку следует выбранной локали', async () => {
    setLocale('en')
    mockedApi.get.mockResolvedValue({ data: { id: 'chat-1' } })
    mockedApi.post.mockResolvedValue({ data: { id: 'msg-1' } })
    const wrapper = mountModal()

    await wrapper.find('.verification-btn-primary').trigger('click')
    await vi.waitFor(() => expect(wrapper.emitted('sent')).toBeTruthy())

    expect(mockedApi.post).toHaveBeenCalledWith('/support/chats/chat-1/messages', {
      text: 'Hello! Please verify my account.',
    })
  })

  it('при ошибке отправки показывает сообщение и не закрывается', async () => {
    mockedApi.get.mockResolvedValue({ data: { id: 'chat-1' } })
    mockedApi.post.mockRejectedValue(new Error('network down'))
    const wrapper = mountModal()

    await wrapper.find('.verification-btn-primary').trigger('click')
    await vi.waitFor(() => expect(wrapper.find('.verification-error').exists()).toBe(true))

    expect(wrapper.find('.verification-error').text()).toContain('Попробуйте ещё раз')
    expect(wrapper.emitted('sent')).toBeFalsy()
    expect(wrapper.emitted('update:show')).toBeFalsy()
  })
})
