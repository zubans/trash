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
          <div class="dbg-line" :class="lineClass(log)">
            <span class="dbg-time">{{ fmtTime(log.ts) }}</span>
            <span class="dbg-method">{{ log.method }}</span>
            <span class="dbg-code">{{ log.status ?? (log.error ? 'ERR' : '…') }}</span>
            <span class="dbg-url">{{ shortUrl(log.url) }}</span>
            <span v-if="log.durationMs != null" class="dbg-dur">{{ log.durationMs }}ms</span>
          </div>
          <div v-if="detail(log)" class="dbg-sub" :class="lineClass(log)">↳ {{ detail(log) }}</div>
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

    // A dim second line carrying the error message or the response body, so the
    // reason a call failed (or what an "empty" list actually returned) is visible
    // without leaving the console.
    const detail = (log: DebugLogEntry) => {
      if (log.error) return log.error
      if (log.responseSnippet) return log.responseSnippet
      return ''
    }

    const clear = () => clearDebugLogs()
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
      lineClass,
      shortUrl,
      fmtTime,
      detail,
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

.dbg-line.err .dbg-code,
.dbg-sub.err {
  color: #ff6b6b;
}
.dbg-line.ok .dbg-code {
  color: #7fe08a;
}

.dbg-sub {
  color: #8ea; /* dim response/error body */
  white-space: pre-wrap;
  word-break: break-all;
  line-height: 1.4;
  padding: 0 0 3px 8px;
  opacity: 0.85;
}
</style>
