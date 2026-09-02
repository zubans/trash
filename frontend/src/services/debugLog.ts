// Крошечный журнал запросов внутри приложения и переключатель отладочной консоли.
//
// Мобильные WebView не дают доступа к консоли браузера, поэтому, когда что-то
// вроде «список пользователей не грузится» надо диагностировать на настоящем
// устройстве, увидеть HTTP-трафик неоткуда. Это хранилище записывает каждый
// запрос и ответ API, а экранная консоль (DebugConsole.vue) их рисует — но
// только при включённом режиме отладки, поэтому обычным пользователям ничего не видно.
//
// Режим отладки включён, если ЛИБО сборка задала VITE_DEBUG=true, ЛИБО его
// включили во время работы (сохраняется в localStorage) скрытым жестом в
// футере с версией — последнее позволяет перевести уже установленную продовую
// сборку в режим отладки без пересборки.
import { ref } from 'vue'

// Читаем окружение напрямую (а не из api.ts), чтобы избежать цикла импортов:
// api.ts импортирует этот модуль ради своих перехватчиков.
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

// setDebugConsole переключает флаг времени выполнения. Сборки с VITE_DEBUG включены в любом случае.
export function setDebugConsole(on: boolean) {
  try {
    localStorage.setItem(RUNTIME_KEY, on ? '1' : '0')
  } catch {
    // localStorage может быть недоступен; флаг в памяти всё равно действует.
  }
  debugConsoleEnabled.value = buildDebug || on
}

export function toggleDebugConsole() {
  setDebugConsole(!readRuntimeFlag())
}

export function clearDebugLogs() {
  debugLogs.value = []
}

// pushLog добавляет запись об ожидающем запросе и возвращает её id, чтобы
// перехватчик ответа мог вписать исход.
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

// logWsEvent записывает строку жизненного цикла/диагностики WebSocket в ту же
// консоль, что и HTTP-запросы. Метод 'WS' заставляет консоль рисовать OK/ERR
// вместо HTTP-статуса. Используется, чтобы изучать пропажу нативных отправок в чат.
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

// snippet отдаёт короткий безопасный предпросмотр тела ответа или ошибки для консоли.
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
