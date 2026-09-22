import { i18n } from '../i18n'
import type { PerkKind, UserPerk } from '../api/shop'

// Как привилегия выглядит в тексте. Формулу считает сервер (Levels.For); здесь
// только её запись: «7 % × 0.5 = 3.5 %», «7 % − 5 = 2 %», «0 %».

const t = (key: string, params?: Record<string, unknown>) => i18n.global.t(key, params ?? {}) as string
const locale = () => i18n.global.locale.value as string

// Проценты без хвостовых нулей: 3.5, а не 3.50; 7, а не 7.0.
export function formatPercent(value: number): string {
  if (!Number.isFinite(value)) return '0'
  return String(Math.round(value * 100) / 100)
}

export function formatPerkDate(value: string | Date): string {
  const date = typeof value === 'string' ? new Date(value) : value
  return date.toLocaleDateString(locale() === 'en' ? 'en-GB' : 'ru-RU', { day: 'numeric', month: 'long' })
}

// Короткое имя привилегии: «Комиссия ×0.5», «Комиссия минус 5 п.п.», «Без комиссии».
export function perkTitle(kind: PerkKind | string | undefined, value?: number | null): string {
  switch (kind) {
    case 'COMMISSION_MULTIPLIER':
      return t('shop.perk.multiplier', { value: formatPercent(value ?? 0) })
    case 'COMMISSION_DISCOUNT_PP':
      return t('shop.perk.discount', { value: formatPercent(value ?? 0) })
    case 'COMMISSION_FREE':
      return t('shop.perk.free')
    default:
      return ''
  }
}

// Формула ставки с привилегией от ставки по уровню.
export function perkFormula(levelPercent: number, percent: number, kind?: PerkKind | string, value?: number | null): string {
  const result = `${formatPercent(percent)} %`
  switch (kind) {
    case 'COMMISSION_MULTIPLIER':
      return `${formatPercent(levelPercent)} % × ${formatPercent(value ?? 0)} = ${result}`
    case 'COMMISSION_DISCOUNT_PP':
      return `${formatPercent(levelPercent)} % − ${formatPercent(value ?? 0)} = ${result}`
    default:
      return result
  }
}

// Плашка действующей привилегии: «Комиссия 7 % × 0.5 = 3.5 % до 17 октября».
export function perkBadge(level: {
  level_percent: number
  percent: number
  perk_kind?: string
  perk_value?: number
  perk_expires_at?: string
}): string {
  if (!level.perk_kind || !level.perk_expires_at) return ''
  return t('shop.perk.badge', {
    formula: perkFormula(level.level_percent, level.percent, level.perk_kind, level.perk_value),
    date: formatPerkDate(level.perk_expires_at),
  })
}

// Очередь за действующей: «дальше: Комиссия минус 5 п.п. с 20 сентября».
// Действующая привилегия в строку не попадает — она уже на плашке.
export function perkQueueLines(queue: UserPerk[], now: Date = new Date()): string[] {
  return queue
    .filter((perk) => new Date(perk.starts_at) > now && !perk.revoked_at)
    .map((perk) => t('shop.perk.next', { what: perkTitle(perk.kind, perk.value), date: formatPerkDate(perk.starts_at) }))
}

// Строка о начале действия на карточке привилегии.
export function perkStartLine(startsAt: string, queued: boolean): string {
  return queued ? t('shop.perk.startsAt', { date: formatPerkDate(startsAt) }) : t('shop.perk.startsNow')
}
