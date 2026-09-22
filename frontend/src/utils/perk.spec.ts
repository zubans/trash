import { describe, it, expect, beforeEach } from 'vitest'
import { i18n } from '../i18n'
import { formatPercent, perkBadge, perkFormula, perkQueueLines, perkStartLine, perkTitle } from './perk'
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

  it('writes the formula for each of the three kinds', () => {
    expect(perkFormula(7, 3.5, 'COMMISSION_MULTIPLIER', 0.5)).toBe('7 % × 0.5 = 3.5 %')
    expect(perkFormula(7, 2, 'COMMISSION_DISCOUNT_PP', 5)).toBe('7 % − 5 = 2 %')
    expect(perkFormula(7, 0, 'COMMISSION_FREE')).toBe('0 %')
  })

  it('names each kind', () => {
    expect(perkTitle('COMMISSION_MULTIPLIER', 0.5)).toBe('Комиссия ×0.5')
    expect(perkTitle('COMMISSION_DISCOUNT_PP', 5)).toBe('Комиссия минус 5 п.п.')
    expect(perkTitle('COMMISSION_FREE')).toBe('Без комиссии')
  })

  it('shows the badge only while a perk applies', () => {
    const base = { level_percent: 7, percent: 3.5 }
    expect(perkBadge(base)).toBe('')
    expect(
      perkBadge({ ...base, perk_kind: 'COMMISSION_MULTIPLIER', perk_value: 0.5, perk_expires_at: '2026-10-17T12:00:00Z' }),
    ).toBe('Комиссия 7 % × 0.5 = 3.5 % до 17 октября')
    expect(perkBadge({ level_percent: 7, percent: 0, perk_kind: 'COMMISSION_FREE', perk_expires_at: '2026-09-20T12:00:00Z' })).toBe(
      'Комиссия 0 % до 20 сентября',
    )
  })

  it('lists only the queued perks, of any kind', () => {
    const now = new Date('2026-09-10T12:00:00Z')
    const perk = (kind: UserPerk['kind'], starts: string, value?: number): UserPerk => ({
      id: starts, user_id: 'u', kind, value, starts_at: starts, expires_at: starts, created_at: starts,
    })
    const lines = perkQueueLines(
      [
        perk('COMMISSION_MULTIPLIER', '2026-09-01T12:00:00Z', 0.5), // действует — на плашке, не в очереди
        perk('COMMISSION_DISCOUNT_PP', '2026-09-20T12:00:00Z', 5),
        perk('COMMISSION_FREE', '2026-10-20T12:00:00Z'),
      ],
      now,
    )
    expect(lines).toEqual(['дальше: Комиссия минус 5 п.п. с 20 сентября', 'дальше: Без комиссии с 20 октября'])
  })

  it('says when a queued perk starts', () => {
    expect(perkStartLine('2026-10-03T09:00:00Z', true)).toBe('Начнёт действовать 3 октября')
    expect(perkStartLine('2026-10-03T09:00:00Z', false)).toBe('Начнёт действовать сразу после оплаты')
  })
})
