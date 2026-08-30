<template>
  <div v-if="enabled" class="dbg" :class="{ collapsed }">
    <div class="dbg-bar" @click="collapsed = !collapsed">
      <span class="dbg-title">
        <i class="ph-fill ph-terminal-window"></i>
        API
        <span class="dbg-count">{{ logs.length }}</span>
        <span v-if="errorCount > 0" class="dbg-errs">{{ errorCount }} ERR</span>
      </span>
      <span class="dbg-actions" @click.stop>
        <button type="button" title="Очистить" @click="clear">🗑</button>
        <button type="button" title="Выключить консоль" @click="disable">⏻</button>
        <button type="button" :title="collapsed ? 'Развернуть' : 'Свернуть'" @click="collapsed = !collapsed">
          {{ collapsed ? '▲' : '▼' }}
        </button>
      </span>
    </div>

    <div v-show="!collapsed" class="dbg-body">
      <div ref="logEl" class="dbg-log">
        <div v-if="logs.length === 0" class="dbg-empty">Пока нет запросов…</div>
        <template v-for="log in logs" :key="log.id">
          <!-- Tap a request to expand its full details. -->
          <div class="dbg-line" :class="[lineClass(log), { open: isOpen(log.id) }]" @click="toggle(log.id)">
            <span class="dbg-caret">{{ isOpen(log.id) ? '▾' : '▸' }}</span>
            <span class="dbg-time">{{ fmtTime(log.ts) }}</span>
            <span class="dbg-method">{{ log.method }}</span>
            <span class="dbg-code">{{ log.status ?? (log.error ? 'ERR' : '…') }}</span>
            <span class="dbg-url">{{ shortUrl(log.url) }}</span>
            <span v-if="log.durationMs != null" class="dbg-dur">{{ log.durationMs }}ms</span>
          </div>

          <div v-if="isOpen(log.id)" class="dbg-detail">
            <div class="dbg-drow"><b>URL</b><span>{{ log.url }}</span></div>
            <div class="dbg-drow"><b>Метод</b><span>{{ log.method }}</span></div>
            <div class="dbg-drow">
              <b>Статус</b><span>{{ log.status ?? '—' }}{{ log.ok === false ? ' · ошибка' : '' }}</span>
            </div>
            <div class="dbg-drow"><b>Время</b><span>{{ fmtTime(log.ts) }}</span></div>
            <div v-if="log.durationMs != null" class="dbg-drow"><b>Длит.</b><span>{{ log.durationMs }} ms</span></div>
            <div v-if="log.error" class="dbg-drow err"><b>Ошибка</b><span>{{ log.error }}</span></div>
            <div v-if="log.responseSnippet" class="dbg-dbody">
              <div class="dbg-dbody-head">
                <b>Ответ</b>
                <button type="button" class="dbg-copy" @click.stop="copy(log)">
                  {{ copiedId === log.id ? 'скопировано' : 'копировать' }}
                </button>
              </div>
              <pre class="dbg-pre">{{ log.responseSnippet }}</pre>
            </div>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, computed, watch, nextTick } from 'vue'
import {
  debugConsoleEnabled,
  debugLogs,
  clearDebugLogs,
  setDebugConsole,
  type DebugLogEntry,
} from '../services/debugLog'

export default defineComponent({
  name: 'DebugConsole',
  setup() {
    const collapsed = ref(true)
    const logEl = ref<HTMLElement | null>(null)
    // Which request rows are expanded to show their details. A Set (reassigned on
    // change) lets several be open at once.
    const openIds = ref<Set<number>>(new Set())
    const copiedId = ref<number | null>(null)

    const logs = debugLogs
    const errorCount = computed(
      () => debugLogs.value.filter((l) => l.error || (l.status != null && l.status >= 400)).length,
    )

    const lineClass = (log: DebugLogEntry) => {
      if (log.error || (log.status != null && log.status >= 400)) return 'err'
      if (log.status != null && log.status < 400) return 'ok'
      return ''
    }

    // Strip origin and the /api prefix so the path reads clearly on a phone.
    const shortUrl = (url: string) => url.replace(/^https?:\/\/[^/]+/, '').replace(/^\/api/, '') || '/'
    const fmtTime = (ts: number) => new Date(ts).toLocaleTimeString()

    const isOpen = (id: number) => openIds.value.has(id)
    const toggle = (id: number) => {
      const next = new Set(openIds.value)
      next.has(id) ? next.delete(id) : next.add(id)
      openIds.value = next
    }

    const copy = async (log: DebugLogEntry) => {
      const text = `${log.method} ${log.url}\nСтатус: ${log.status ?? '—'}\n${log.error ? 'Ошибка: ' + log.error + '\n' : ''}${log.responseSnippet || ''}`
      try {
        await navigator.clipboard.writeText(text)
        copiedId.value = log.id
        setTimeout(() => {
          if (copiedId.value === log.id) copiedId.value = null
        }, 1200)
      } catch {
        // clipboard unavailable — ignore
      }
    }

    const clear = () => {
      clearDebugLogs()
      openIds.value = new Set()
    }
    const disable = () => {
      setDebugConsole(false)
      collapsed.value = true
    }

    // Follow the tail like a terminal, but only when the user is already at the
    // bottom, so scrolling up to read history is not yanked back down.
    watch(
      () => debugLogs.value.length,
      async () => {
        const el = logEl.value
        if (collapsed.value || !el) return
        const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 60
        if (atBottom) {
          await nextTick()
          el.scrollTop = el.scrollHeight
        }
      },
    )

    return {
      enabled: debugConsoleEnabled,
      collapsed,
      logEl,
      logs,
      errorCount,
      copiedId,
      lineClass,
      shortUrl,
      fmtTime,
      isOpen,
      toggle,
      copy,
      clear,
      disable,
    }
  },
})
</script>

<style scoped>
.dbg {
  position: fixed;
  left: 0;
  right: 0;
  bottom: 0;
  z-index: 99999;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 11px;
  background: #0b0f14;
  color: #cfd8e3;
  border-top: 1px solid #223;
  box-shadow: 0 -2px 12px rgba(0, 0, 0, 0.5);
}

.dbg-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 8px;
  background: #111823;
  cursor: pointer;
  user-select: none;
}
.dbg-title {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
}
.dbg-count {
  padding: 1px 6px;
  border-radius: 4px;
  font-size: 10px;
  background: #345;
  color: #bcd;
}
.dbg-errs {
  padding: 1px 6px;
  border-radius: 4px;
  font-size: 10px;
  background: #a33;
  color: #fdd;
  font-weight: 700;
}
.dbg-actions button {
  background: transparent;
  border: none;
  color: #9ab;
  font-size: 13px;
  padding: 0 6px;
  cursor: pointer;
}

.dbg-body {
  max-height: 45vh;
  display: flex;
  flex-direction: column;
}
.dbg-log {
  overflow-y: auto;
  padding: 4px 8px;
  flex: 1;
}
.dbg-empty {
  padding: 12px;
  text-align: center;
  color: #64748b;
}

.dbg-line {
  display: flex;
  gap: 8px;
  align-items: baseline;
  line-height: 1.5;
  white-space: nowrap;
  cursor: pointer;
  padding: 1px 4px;
  border-radius: 4px;
}
.dbg-line:hover {
  background: rgba(255, 255, 255, 0.05);
}
.dbg-line.open {
  background: rgba(255, 255, 255, 0.07);
}
.dbg-caret {
  color: #64748b;
  width: 10px;
  flex-shrink: 0;
}
.dbg-time {
  color: #64748b;
}
.dbg-method {
  color: #93c5fd;
  font-weight: 700;
  min-width: 40px;
}
.dbg-code {
  font-weight: 700;
  min-width: 30px;
}
.dbg-url {
  color: #cfd8e3;
  overflow: hidden;
  text-overflow: ellipsis;
}
.dbg-dur {
  color: #64748b;
  margin-left: auto;
  padding-left: 8px;
}

.dbg-line.err .dbg-code {
  color: #ff6b6b;
}
.dbg-line.ok .dbg-code {
  color: #7fe08a;
}

/* Expanded detail block */
.dbg-detail {
  margin: 2px 0 6px 14px;
  padding: 8px 10px;
  background: rgba(255, 255, 255, 0.04);
  border-left: 2px solid #334;
  border-radius: 6px;
}
.dbg-drow {
  display: flex;
  gap: 8px;
  line-height: 1.5;
  word-break: break-all;
}
.dbg-drow b {
  color: #7f93ad;
  min-width: 56px;
  flex-shrink: 0;
  font-weight: 600;
}
.dbg-drow span {
  color: #dbe4ee;
}
.dbg-drow.err b,
.dbg-drow.err span {
  color: #ff6b6b;
}
.dbg-dbody {
  margin-top: 6px;
}
.dbg-dbody-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 3px;
}
.dbg-dbody-head b {
  color: #7f93ad;
  font-weight: 600;
}
.dbg-copy {
  background: rgba(255, 255, 255, 0.1);
  border: none;
  color: #bcd;
  font-size: 10px;
  padding: 2px 8px;
  border-radius: 4px;
  cursor: pointer;
}
.dbg-copy:hover {
  background: rgba(255, 255, 255, 0.18);
}
.dbg-pre {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-all;
  color: #a5d6b0;
  line-height: 1.4;
  max-height: 200px;
  overflow: auto;
  background: rgba(0, 0, 0, 0.25);
  padding: 6px 8px;
  border-radius: 4px;
}
</style>
