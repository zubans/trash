// A tiny in-app request log + debug console gate.
//
// Mobile WebViews give no access to the browser console, so when something like
// "the user list never loads" has to be diagnosed on a real device, there is no
// way to see the HTTP traffic. This store records every API request/response and
// an on-screen console (DebugConsole.vue) renders it — but only when debug mode
// is on, so nothing shows for ordinary users.
//
// Debug mode is on when EITHER the build sets VITE_DEBUG=true, OR it is switched
// on at runtime (persisted in localStorage) via the hidden gesture in the
// version footer — the latter lets an already-installed production build be put
// into debug mode without a rebuild.
import { ref } from 'vue'

// Read the env directly (not from api.ts) to avoid an import cycle: api.ts
// imports this module for its interceptors.
const buildDebug = import.meta.env.VITE_DEBUG === 'true'

const RUNTIME_KEY = 'debug_console_enabled'

function readRuntimeFlag(): boolean {
  try {
    return localStorage.getItem(RUNTIME_KEY) === '1'
  } catch {
    return false
  }
}

export interface DebugLogEntry {
  id: number
  ts: number
  method: string
  url: string
  status?: number
  ok?: boolean
  durationMs?: number
  error?: string
  responseSnippet?: string
}

const MAX_ENTRIES = 300

export const debugConsoleEnabled = ref<boolean>(buildDebug || readRuntimeFlag())
export const debugLogs = ref<DebugLogEntry[]>([])

let seq = 0

// setDebugConsole toggles the runtime flag. VITE_DEBUG builds stay on regardless.
export function setDebugConsole(on: boolean) {
  try {
    localStorage.setItem(RUNTIME_KEY, on ? '1' : '0')
  } catch {
    // localStorage may be unavailable; the in-memory flag still applies.
  }
  debugConsoleEnabled.value = buildDebug || on
}

export function toggleDebugConsole() {
  setDebugConsole(!readRuntimeFlag())
}

export function clearDebugLogs() {
  debugLogs.value = []
}

// pushLog appends a pending request entry and returns its id so the response
// interceptor can fill in the outcome.
export function pushLog(entry: Omit<DebugLogEntry, 'id'>): number {
  const id = ++seq
  debugLogs.value.push({ id, ...entry })
  if (debugLogs.value.length > MAX_ENTRIES) {
    debugLogs.value.splice(0, debugLogs.value.length - MAX_ENTRIES)
  }
  return id
}

export function updateLog(id: number, patch: Partial<DebugLogEntry>) {
  const entry = debugLogs.value.find((l) => l.id === id)
  if (entry) Object.assign(entry, patch)
}

// logWsEvent records a WebSocket lifecycle/diagnostic line in the same console
// as HTTP requests. method 'WS' makes the console render OK/ERR instead of an
// HTTP status. Used to study why native chat sends go missing.
export function logWsEvent(label: string, opts?: { ok?: boolean; error?: string; detail?: string }) {
  if (!debugConsoleEnabled.value) return
  pushLog({
    ts: Date.now(),
    method: 'WS',
    url: label,
    ok: opts?.ok,
    error: opts?.error,
    responseSnippet: opts?.detail,
  })
}

// snippet renders a short, safe preview of a response/error body for the console.
export function snippet(data: unknown, max = 400): string {
  if (data == null) return ''
  let text: string
  try {
    text = typeof data === 'string' ? data : JSON.stringify(data)
  } catch {
    text = String(data)
  }
  return text.length > max ? text.slice(0, max) + '…' : text
}
