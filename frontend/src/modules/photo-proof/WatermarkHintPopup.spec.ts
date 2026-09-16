import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import WatermarkHintPopup from './WatermarkHintPopup.vue'

const bunny = { code: 'bunny', number: 3, title: 'Зайчик', description: 'Два пальца', fits_in_selfie: true }
const foot = { code: 'left_foot', number: 5, title: 'Левая нога', description: 'Носок ноги', fits_in_selfie: false }

describe('WatermarkHintPopup', () => {
  it('показывает жест для снимка места заказа', () => {
    const wrapper = mount(WatermarkHintPopup, { props: { gesture: bunny } })
    expect(wrapper.text()).toContain('Зайчик')
    expect(wrapper.text()).toContain('место заказа')
  })

  it('не требует жест ногой в селфи', () => {
    const wrapper = mount(WatermarkHintPopup, { props: { gesture: foot, selfie: true } })
    expect(wrapper.text()).toContain('не помещается')
  })

  it('сообщает о подтверждении и отмене', async () => {
    const wrapper = mount(WatermarkHintPopup, { props: { gesture: bunny } })
    const buttons = wrapper.findAll('button')
    await buttons[1].trigger('click')
    await buttons[0].trigger('click')
    expect(wrapper.emitted('confirm')).toHaveLength(1)
    expect(wrapper.emitted('cancel')).toHaveLength(1)
  })
})
