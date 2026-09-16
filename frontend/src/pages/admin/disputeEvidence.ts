import type { ProofEvidence } from '../../api/disputes'

// Подписи для карточки доказательств — отдельно от страницы, чтобы их можно было
// проверить тестом: арбитр читает именно эти слова.

export const FLAG_LABELS: Record<string, string> = {
  SEAL: 'файл не подтверждён',
  MARK: 'метка снимка не найдена',
  TIME: 'далеко по времени от отметки «Исполнил»',
  EXIF_TIME: 'время в файле расходится со временем телефона',
  DISTANCE: 'далеко от адреса заказа',
  NO_TRACK: 'рядом по времени нет трека',
  TRACK_DISTANCE: 'трек был далеко от места снимка',
  GEO_ALERT: 'аномалия скорости рядом со съёмкой',
}

/** Что проверка файла говорит арбитру простыми словами. */
export function integrityVerdict(proof: Pick<ProofEvidence, 'seal_status' | 'mark_status'>): { text: string; tone: 'ok' | 'warn' | 'bad' } {
  const { seal_status: seal, mark_status: mark } = proof
  if (seal === 'INVALID' || mark === 'MISMATCH') {
    return { text: 'Признаки подмены: данные снимка не сходятся с заказом', tone: 'bad' }
  }
  if (seal === 'VALID' && mark === 'FOUND') {
    return { text: 'Снимок сделан приложением для этого заказа', tone: 'ok' }
  }
  if (mark === 'FOUND') {
    return { text: 'Снят приложением для этого заказа, но файл пересохранён', tone: 'warn' }
  }
  return { text: 'Не подтверждено, что снимок сделан приложением', tone: 'bad' }
}

export const CLOSURE_LABELS: Record<string, string> = {
  CUSTOMER_CONFIRMED: 'заказчик подтвердил выполнение',
  EXECUTOR_CONCEDED: 'исполнитель признал, что не выполнил',
  ARBITRATION: 'решение арбитра',
}

export const DECISION_LABELS: Record<string, string> = {
  EXECUTOR: 'прав исполнитель',
  CUSTOMER: 'прав заказчик',
  UNKNOWN: 'неизвестно',
}

export function minutesText(value?: number | null): string {
  if (value === undefined || value === null) return '—'
  const abs = Math.abs(value)
  const text = abs < 60 ? `${Math.round(abs)} мин` : `${(abs / 60).toFixed(1)} ч`
  return value < 0 ? `${text} после` : `${text} до`
}

export function metersText(value?: number | null): string {
  if (value === undefined || value === null) return '—'
  return value < 1000 ? `${Math.round(value)} м` : `${(value / 1000).toFixed(1)} км`
}
