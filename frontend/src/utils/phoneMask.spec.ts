import { describe, it, expect } from 'vitest'
import { formatPhoneMask, cleanPhoneDigits } from './phoneMask'

describe('phoneMask utilities', () => {
  it('formats raw numbers into Russian +7 (XXX) XXX-XX-XX mask', () => {
    expect(formatPhoneMask('79207050707')).toBe('+7 (920) 705-07-07')
    expect(formatPhoneMask('89207050707')).toBe('+7 (920) 705-07-07')
    expect(formatPhoneMask('9207050707')).toBe('+7 (920) 705-07-07')
    expect(formatPhoneMask('+7 (920) 705-07-07')).toBe('+7 (920) 705-07-07')
    expect(formatPhoneMask('7900')).toBe('+7 (900) ')
    expect(formatPhoneMask('')).toBe('')
  })

  it('cleans phone digits for DB storage and API payloads', () => {
    expect(cleanPhoneDigits('+7 (920) 705-07-07')).toBe('79207050707')
    expect(cleanPhoneDigits('8 (920) 705-07-07')).toBe('79207050707')
    expect(cleanPhoneDigits('79207050707')).toBe('79207050707')
  })
})
