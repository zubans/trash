import { describe, it, expect, beforeEach } from 'vitest'
import { i18n } from '../i18n'
import { formatPercent, perkBadge, perkFormula, perkQueueLines, perkStartLine, ruleText } from './perk'
import type { UserPerk } from '../api/shop'

describe('perk text', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'ru'
  })

  it('formats percents without trailing zeros', () => {
    expect(formatPercent(3.5)).toBe('3.5')
    expect(formatPercent(7)).toBe('7')
    expect(formatPercent(2.3333)).toBe('2.33')
  })

  it('writes the rate before and after, whatever the rule', () => {
    expect(perkFormula(7, 3.5)).toBe('7 % → 3.5 %')
    expect(perkFormula(7, 0)).toBe('7 % → 0 %')
    expect(perkFormula(0, 0)).toBe('0 %')
  })

  it('puts the constants into the rule title', () => {
    expect(ruleText('Комиссия умножается на VALUE', { VALUE: 0.5 })).toBe('Комиссия умножается на 0.5')
    expect(ruleText('Комиссия минус VALUE пунктов', { VALUE: 5 })).toBe('Комиссия минус 5 пунктов')
    expect(ruleText('Без комиссии', {})).toBe('Без комиссии')
    // Константа заменяется только целым словом.
    expect(ruleText('VALUES и VALUE', { VALUE: 1 })).toBe('VALUES и 1')
  })

  it('shows the badge only while a perk applies', () => {
    const base = { level_percent: 7, percent: 3.5 }
    expect(perkBadge(base)).toBe('')
    expect(perkBadge({ ...base, perk_rule: 'commission_multiplier', perk_expires_at: '2026-10-17T12:00:00Z' })).toBe(
      'Комиссия 7 % → 3.5 % до 17 октября',
    )
  })

  it('lists only the queued perks, of any rule', () => {
    const now = new Date('2026-09-10T12:00:00Z')
    const perk = (title: string, starts: string, config: UserPerk['config'] = {}): UserPerk => ({
      id: starts, user_id: 'u', rule_code: 'r', rule_title: title, config,
      starts_at: starts, expires_at: starts, created_at: starts,
    })
    const lines = perkQueueLines(
      [
        perk('Комиссия умножается на VALUE', '2026-09-01T12:00:00Z', { VALUE: 0.5 }), // действует — на плашке
        perk('Комиссия минус VALUE пунктов', '2026-09-20T12:00:00Z', { VALUE: 5 }),
        perk('Без комиссии', '2026-10-20T12:00:00Z'),
      ],
      now,
    )
    expect(lines).toEqual(['дальше: Комиссия минус 5 пунктов с 20 сентября', 'дальше: Без комиссии с 20 октября'])
  })

  it('says when a queued perk starts', () => {
    expect(perkStartLine('2026-10-03T09:00:00Z', true)).toBe('Начнёт действовать 3 октября')
    expect(perkStartLine('2026-10-03T09:00:00Z', false)).toBe('Начнёт действовать сразу после оплаты')
  })
})
