import { i18n } from '../i18n'
import type { PerkConfig, UserPerk } from '../api/shop'

// Как привилегия выглядит в тексте. Ставку считает правило на сервере
// (Levels.For): клиент не знает ни одного правила и пишет только результат —
// «7 % → 3.5 %».

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

// Название правила с подставленными константами: «Комиссия умножается на
// VALUE» и {VALUE: 0.5} дают «Комиссия умножается на 0.5». Правило называет
// свои константы в названии, и клиенту не нужно знать, что они значат.
export function ruleText(title: string | undefined, config?: PerkConfig): string {
  let text = title ?? ''
  for (const [key, value] of Object.entries(config ?? {})) {
    const shown = typeof value === 'number' ? formatPercent(value) : String(value)
    text = text.replace(new RegExp(`\\b${key}\\b`, 'g'), shown)
  }
  return text
}

// Ставка до и после привилегии: «7 % → 3.5 %». Если привилегия ставку не
// меняет — одно число.
export function perkFormula(levelPercent: number, percent: number): string {
  const after = `${formatPercent(percent)} %`
  return levelPercent === percent ? after : `${formatPercent(levelPercent)} % → ${after}`
}

// Плашка действующей привилегии: «Комиссия 7 % → 3.5 % до 17 октября».
export function perkBadge(level: {
  level_percent: number
  percent: number
  perk_rule?: string
  perk_expires_at?: string
}): string {
  if (!level.perk_rule || !level.perk_expires_at) return ''
  return t('shop.perk.badge', {
    formula: perkFormula(level.level_percent, level.percent),
    date: formatPerkDate(level.perk_expires_at),
  })
}

// Очередь за действующей: «дальше: Комиссия минус 5 пунктов с 20 сентября».
// Действующая привилегия в строку не попадает — она уже на плашке.
export function perkQueueLines(queue: UserPerk[], now: Date = new Date()): string[] {
  return queue
    .filter((perk) => new Date(perk.starts_at) > now && !perk.revoked_at)
    .map((perk) => t('shop.perk.next', { what: ruleText(perk.rule_title, perk.config), date: formatPerkDate(perk.starts_at) }))
}

// Строка о начале действия на карточке привилегии.
export function perkStartLine(startsAt: string, queued: boolean): string {
  return queued ? t('shop.perk.startsAt', { date: formatPerkDate(startsAt) }) : t('shop.perk.startsNow')
}

// Правило и константы товара одной строкой — для админки, где названия правила
// под рукой нет: «commission_multiplier, VALUE 0.5».
export function ruleSummary(code: string | undefined, config?: PerkConfig): string {
  const constants = Object.entries(config ?? {}).map(([k, v]) => `${k} ${typeof v === 'number' ? formatPercent(v) : v}`)
  return [code ?? '', ...constants].filter(Boolean).join(', ')
}
