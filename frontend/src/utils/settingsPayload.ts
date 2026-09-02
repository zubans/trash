/**
 * Форма настроек привязывает каждое числовое поле через `v-model` к
 * `<input type="number">`, а Vue приводит их к настоящим числам — поле, в
 * которое админ действительно печатал, оставляет в модели `15`, а не `"15"`.
 * API декодирует тело в карту строк, поэтому одно отредактированное число
 * роняло всё сохранение с `400 invalid request body`, тогда как нетронутые поля
 * (всё ещё строки после загрузки) проходили нормально.
 *
 * Нормализация здесь, на границе, оставляет форме свободу держать тот тип,
 * который даёт ей input.
 */
export function toSettingsPayload(values: Record<string, unknown>): Record<string, string> {
  const payload: Record<string, string> = {}
  for (const [key, value] of Object.entries(values)) {
    // null/undefined превратились бы в «null»/«undefined» и сохранились бы этим
    // буквальным текстом; пустое значение — честный перевод.
    payload[key] = value === null || value === undefined ? '' : String(value)
  }
  return payload
}
