/**
 * Форматирует строку из цифр или произвольный ввод в российскую телефонную
 * маску: +7 (XXX) XXX-XX-XX
 */
export function formatPhoneMask(input: string): string {
  let digits = input.replace(/\D/g, '')

  // Если пользователь набрал 8 в начале (например, 8999...), меняем ведущую 8 на 7
  if (digits.startsWith('8')) {
    digits = '7' + digits.slice(1)
  }

  // Если пользователь начал не с 7, дописываем 7 в начало, когда цифры есть
  if (digits.length > 0 && !digits.startsWith('7')) {
    digits = '7' + digits
  }

  // Ограничиваем длину 11 цифрами (7 + 10 цифр)
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
 * Извлекает голые цифры для хранения в базе и запросов к API (например, 79999999999).
 */
export function cleanPhoneDigits(input: string): string {
  let digits = input.replace(/\D/g, '')
  if (digits.startsWith('8')) {
    digits = '7' + digits.slice(1)
  }
  return digits
}
