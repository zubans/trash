import { describe, it, expect, beforeEach } from 'vitest'
import { i18n, setLocale, AVAILABLE_LOCALES } from './index'

describe('i18n module', () => {
  beforeEach(() => {
    localStorage.clear()
    setLocale('ru')
  })

  it('exports available locales list', () => {
    expect(AVAILABLE_LOCALES).toEqual([
      { code: 'en', name: 'English' },
      { code: 'ru', name: 'Русский' },
    ])
  })

  it('changes locale and persists to localStorage on setLocale()', () => {
    setLocale('en')
    expect(i18n.global.locale.value).toBe('en')
    expect(localStorage.getItem('locale')).toBe('en')

    setLocale('ru')
    expect(i18n.global.locale.value).toBe('ru')
    expect(localStorage.getItem('locale')).toBe('ru')
  })
})
