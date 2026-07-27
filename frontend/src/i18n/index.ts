import { createI18n } from 'vue-i18n'
import en from './locales/en.json'
import ru from './locales/ru.json'

export const AVAILABLE_LOCALES = [
  { code: 'en', name: 'English' },
  { code: 'ru', name: 'Русский' },
]

const savedLocale = localStorage.getItem('locale') || 'ru'

export const i18n = createI18n({
  legacy: false,
  locale: savedLocale,
  fallbackLocale: 'en',
  messages: {
    en,
    ru,
  },
})

export function setLocale(locale: string) {
  i18n.global.locale.value = locale as 'en' | 'ru'
  localStorage.setItem('locale', locale)
}
