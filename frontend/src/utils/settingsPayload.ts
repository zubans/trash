/**
 * The settings form binds every numeric field with `v-model` on an
 * `<input type="number">`, and Vue casts those to real numbers — a field the
 * admin actually typed into leaves the model as `15`, not `"15"`. The API
 * decodes the body into a string map, so a single edited number made the whole
 * save fail with `400 invalid request body`, while untouched fields (still
 * strings from the load) went through fine.
 *
 * Normalising here, at the boundary, keeps the form free to hold whatever type
 * the input gives it.
 */
export function toSettingsPayload(values: Record<string, unknown>): Record<string, string> {
  const payload: Record<string, string> = {}
  for (const [key, value] of Object.entries(values)) {
    // null/undefined would stringify to "null"/"undefined" and be stored as
    // that literal text; an empty value is the honest translation.
    payload[key] = value === null || value === undefined ? '' : String(value)
  }
  return payload
}
