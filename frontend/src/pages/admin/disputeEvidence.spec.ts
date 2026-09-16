import { describe, it, expect } from 'vitest'
import { integrityVerdict, minutesText, metersText } from './disputeEvidence'

describe('карточка доказательств', () => {
  it('переводит проверку файла в понятный вывод', () => {
    expect(integrityVerdict({ seal_status: 'VALID', mark_status: 'FOUND' }).tone).toBe('ok')
    expect(integrityVerdict({ seal_status: 'MISSING', mark_status: 'FOUND' }).tone).toBe('warn')
    expect(integrityVerdict({ seal_status: 'MISSING', mark_status: 'NOT_FOUND' }).tone).toBe('bad')
    expect(integrityVerdict({ seal_status: 'VALID', mark_status: 'MISMATCH' }).text).toContain('подмены')
    expect(integrityVerdict({ seal_status: 'INVALID', mark_status: 'FOUND' }).text).toContain('подмены')
  })

  it('пишет время и расстояние', () => {
    expect(minutesText(5)).toBe('5 мин до')
    expect(minutesText(-90)).toBe('1.5 ч после')
    expect(minutesText(null)).toBe('—')
    expect(metersText(42.4)).toBe('42 м')
    expect(metersText(1530)).toBe('1.5 км')
  })
})
