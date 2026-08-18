/**
 * Formats a raw digit string or arbitrary input into a Russian phone mask:
 * +7 (XXX) XXX-XX-XX
 */
export function formatPhoneMask(input: string): string {
  let digits = input.replace(/\D/g, '')

  // If user typed 8 at the start (e.g. 8999...), convert leading 8 to 7
  if (digits.startsWith('8')) {
    digits = '7' + digits.slice(1)
  }

  // If user didn't start with 7, prepend 7 if there are digits
  if (digits.length > 0 && !digits.startsWith('7')) {
    digits = '7' + digits
  }

  // Cap max length to 11 digits (7 + 10 digits)
  digits = digits.slice(0, 11)

  if (digits.length === 0) {
    return ''
  }

  let formatted = '+7'
  if (digits.length > 1) {
    formatted += ' (' + digits.slice(1, 4)
  }
  if (digits.length >= 4) {
    formatted += ') ' + digits.slice(4, 7)
  }
  if (digits.length >= 7) {
    formatted += '-' + digits.slice(7, 9)
  }
  if (digits.length >= 9) {
    formatted += '-' + digits.slice(9, 11)
  }

  return formatted
}

/**
 * Extracts raw digits for database storage & API requests (e.g., 79999999999).
 */
export function cleanPhoneDigits(input: string): string {
  let digits = input.replace(/\D/g, '')
  if (digits.startsWith('8')) {
    digits = '7' + digits.slice(1)
  }
  return digits
}
