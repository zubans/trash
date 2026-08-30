<template>
  <div v-if="enabled" class="debug-console-root">
    <!-- Floating toggle -->
    <button class="dbg-fab" :class="{ 'has-error': errorCount > 0 }" @click="open = !open">
      <i class="ph-bold ph-bug"></i>
      <span v-if="errorCount > 0" class="dbg-fab-badge">{{ errorCount }}</span>
    </button>

    <!-- Panel -->
    <div v-if="open" class="dbg-panel">
      <div class="dbg-header">
        <span class="dbg-title">Запросы · {{ logs.length }}</span>
        <div class="dbg-actions">
          <button class="dbg-btn" title="Очистить" @click="clear">
            <i class="ph-bold ph-trash"></i>
          </button>
          <button class="dbg-btn" title="Выключить консоль" @click="disable">
            <i class="ph-bold ph-power"></i>
          </button>
          <button class="dbg-btn" title="Свернуть" @click="open = false">
            <i class="ph-bold ph-x"></i>
          </button>
        </div>
      </div>

      <div class="dbg-list">
        <div v-if="logs.length === 0" class="dbg-empty">Пока нет запросов…</div>
        <div
          v-for="log in reversed"
          :key="log.id"
          class="dbg-row"
          :class="statusClass(log)"
          @click="expanded === log.id ? (expanded = null) : (expanded = log.id)"
        >
          <div class="dbg-row-main">
            <span class="dbg-method">{{ log.method }}</span>
            <span class="dbg-status">{{ log.status ?? (log.error ? 'ERR' : '…') }}</span>
            <span class="dbg-url">{{ shortUrl(log.url) }}</span>
            <span v-if="log.durationMs != null" class="dbg-dur">{{ log.durationMs }}ms</span>
          </div>
          <div v-if="expanded === log.id" class="dbg-detail">
            <div class="dbg-detail-line"><b>URL:</b> {{ log.url }}</div>
            <div class="dbg-detail-line"><b>Время:</b> {{ formatTime(log.ts) }}</div>
            <div v-if="log.error" class="dbg-detail-line err"><b>Ошибка:</b> {{ log.error }}</div>
            <div v-if="log.responseSnippet" class="dbg-detail-line">
              <b>Ответ:</b>
              <pre class="dbg-body">{{ log.responseSnippet }}</pre>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, computed, ref } from 'vue'
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
    const open = ref(false)
    const expanded = ref<number | null>(null)

    const logs = debugLogs
    const reversed = computed(() => [...debugLogs.value].reverse())
    const errorCount = computed(
      () => debugLogs.value.filter((l) => l.error || (l.status != null && l.status >= 400)).length,
    )

    const statusClass = (log: DebugLogEntry) => {
      if (log.error || (log.status != null && log.status >= 400)) return 'is-error'
      if (log.status != null && log.status < 400) return 'is-ok'
      return 'is-pending'
    }

    const shortUrl = (url: string) => url.replace(/^https?:\/\/[^/]+/, '').replace(/^\/api/, '')
    const formatTime = (ts: number) => new Date(ts).toLocaleTimeString()

    const clear = () => {
      clearDebugLogs()
      expanded.value = null
    }
    const disable = () => {
      setDebugConsole(false)
      open.value = false
    }

    return {
      enabled: debugConsoleEnabled,
      open,
      expanded,
      logs,
      reversed,
      errorCount,
      statusClass,
      shortUrl,
      formatTime,
      clear,
      disable,
    }
  },
})
</script>

<style scoped>
.debug-console-root {
  position: fixed;
  z-index: 100000;
}

.dbg-fab {
  position: fixed;
  right: 12px;
  bottom: 76px;
  width: 44px;
  height: 44px;
  border-radius: 50%;
  border: none;
  background: #0f172a;
  color: #fff;
  font-size: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 6px 16px rgba(0, 0, 0, 0.35);
  cursor: pointer;
  opacity: 0.85;
}
.dbg-fab.has-error {
  background: #b91c1c;
}
.dbg-fab-badge {
  position: absolute;
  top: -4px;
  right: -4px;
  background: #ef4444;
  color: #fff;
  font-size: 11px;
  font-weight: 700;
  min-width: 18px;
  height: 18px;
  border-radius: 9px;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0 4px;
}

.dbg-panel {
  position: fixed;
  right: 8px;
  left: 8px;
  bottom: 128px;
  max-height: 55vh;
  background: #0b1220;
  color: #e2e8f0;
  border-radius: 14px;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.5);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  font-family: 'JetBrains Mono', ui-monospace, monospace;
}

.dbg-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 12px;
  background: #111a2e;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
}
.dbg-title {
  font-size: 13px;
  font-weight: 700;
}
.dbg-actions {
  display: flex;
  gap: 6px;
}
.dbg-btn {
  background: rgba(255, 255, 255, 0.08);
  border: none;
  color: #cbd5e1;
  width: 30px;
  height: 30px;
  border-radius: 8px;
  font-size: 15px;
  cursor: pointer;
}
.dbg-btn:hover {
  background: rgba(255, 255, 255, 0.16);
}

.dbg-list {
  overflow-y: auto;
  padding: 4px 0;
}
.dbg-empty {
  padding: 16px;
  text-align: center;
  color: #64748b;
  font-size: 12px;
}

.dbg-row {
  padding: 7px 12px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
  cursor: pointer;
  font-size: 12px;
}
.dbg-row-main {
  display: flex;
  align-items: center;
  gap: 8px;
}
.dbg-method {
  font-weight: 700;
  color: #93c5fd;
  min-width: 42px;
}
.dbg-status {
  font-weight: 700;
  min-width: 34px;
}
.dbg-row.is-ok .dbg-status {
  color: #4ade80;
}
.dbg-row.is-error .dbg-status {
  color: #f87171;
}
.dbg-row.is-pending .dbg-status {
  color: #fbbf24;
}
.dbg-url {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: #e2e8f0;
}
.dbg-dur {
  color: #64748b;
  font-size: 11px;
}

.dbg-detail {
  margin-top: 8px;
  padding: 8px;
  background: rgba(255, 255, 255, 0.04);
  border-radius: 8px;
}
.dbg-detail-line {
  font-size: 11px;
  margin-bottom: 4px;
  word-break: break-all;
  color: #cbd5e1;
}
.dbg-detail-line.err {
  color: #f87171;
}
.dbg-body {
  margin: 4px 0 0;
  white-space: pre-wrap;
  word-break: break-all;
  color: #a5b4fc;
  font-size: 11px;
  max-height: 160px;
  overflow: auto;
}
</style>
