<template>
  <div v-if="visible" class="vpn-debug" :class="{ collapsed }">
    <div class="vpn-debug-bar" @click="collapsed = !collapsed">
      <span class="vpn-debug-title">
        <i class="ph-fill ph-terminal-window"></i>
        VPN
        <span class="vpn-debug-mode" :class="modeClass">{{ state.mode || '—' }}</span>
        <span v-if="state.activeRemark" class="vpn-debug-remark">{{ state.activeRemark }}</span>
      </span>
      <span class="vpn-debug-actions" @click.stop>
        <button type="button" title="Проверить сейчас" @click="reevaluate">↻</button>
        <button type="button" title="Очистить" @click="clear">🗑</button>
        <button type="button" :title="collapsed ? 'Развернуть' : 'Свернуть'" @click="collapsed = !collapsed">
          {{ collapsed ? '▲' : '▼' }}
        </button>
      </span>
    </div>

    <div v-show="!collapsed" class="vpn-debug-body">
      <div class="vpn-debug-state">
        <div>socks: 127.0.0.1:{{ state.socksPort }} · libXray: {{ state.libXray }}</div>
        <div>health: {{ state.healthUrl }}</div>
        <div class="vpn-debug-stored">stored: {{ state.stored }}</div>
      </div>
      <div ref="logEl" class="vpn-debug-log">
        <div
          v-for="(line, i) in lines"
          :key="i"
          class="vpn-debug-line"
          :class="lineClass(line)"
        >{{ line }}</div>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, reactive, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { Capacitor, registerPlugin } from '@capacitor/core'
import { isDebug } from '../services/api'

interface VpnDebugPlugin {
  getLogs(): Promise<{ lines: string[] }>
  getState(): Promise<Record<string, any>>
  setVerbose(options: { enabled: boolean }): Promise<void>
  clear(): Promise<void>
  reevaluate(): Promise<void>
}

const VpnDebug = registerPlugin<VpnDebugPlugin>('VpnDebug')

export default defineComponent({
  name: 'VpnDebugConsole',
  setup() {
    // The console only exists in debug builds on the native app, where the
    // plugin is registered. On the web there is no proxy layer to inspect.
    const visible = isDebug && Capacitor.isNativePlatform()
    const collapsed = ref(true)
    const lines = ref<string[]>([])
    const state = reactive<Record<string, any>>({})
    const logEl = ref<HTMLElement | null>(null)
    let timer: number | undefined

    const poll = async () => {
      try {
        const [logs, st] = await Promise.all([VpnDebug.getLogs(), VpnDebug.getState()])
        const wasAtBottom =
          logEl.value != null &&
          logEl.value.scrollHeight - logEl.value.scrollTop - logEl.value.clientHeight < 40
        lines.value = logs.lines || []
        Object.assign(state, st)
        if (!collapsed.value && wasAtBottom) {
          await nextTick()
          if (logEl.value) logEl.value.scrollTop = logEl.value.scrollHeight
        }
      } catch {
        // Plugin not present or transient failure — leave the last snapshot.
      }
    }

    const reevaluate = async () => {
      try { await VpnDebug.reevaluate() } catch { /* ignore */ }
      poll()
    }
    const clear = async () => {
      try { await VpnDebug.clear() } catch { /* ignore */ }
      lines.value = []
    }

    const lineClass = (line: string) => {
      if (/ERROR|failed|MISSING|missing|did not become ready|no channel/.test(line)) return 'err'
      if (/SELECTED PROXY|ready|updated|-> HTTP 2/.test(line)) return 'ok'
      return ''
    }

    const modeClass = () => (state.mode === 'PROXY' ? 'proxy' : 'direct')

    onMounted(async () => {
      if (!visible) return
      // Raise libXray to debug level and re-run once so the trace is captured.
      try {
        await VpnDebug.setVerbose({ enabled: true })
        await VpnDebug.reevaluate()
      } catch {
        /* plugin not present — ignore */
      }
      poll()
      timer = window.setInterval(poll, 2000)
    })
    onBeforeUnmount(() => {
      if (timer) window.clearInterval(timer)
    })

    return { visible, collapsed, lines, state, logEl, reevaluate, clear, lineClass, modeClass }
  },
})
</script>

<style scoped>
.vpn-debug {
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
.vpn-debug-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 8px;
  background: #111823;
  cursor: pointer;
  user-select: none;
}
.vpn-debug-title {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
}
.vpn-debug-mode {
  padding: 1px 6px;
  border-radius: 4px;
  font-size: 10px;
}
.vpn-debug-mode.proxy { background: #7a3; color: #041; }
.vpn-debug-mode.direct { background: #345; color: #bcd; }
.vpn-debug-remark { color: #9ab; }
.vpn-debug-actions button {
  background: transparent;
  border: none;
  color: #9ab;
  font-size: 13px;
  padding: 0 6px;
  cursor: pointer;
}
.vpn-debug-body { max-height: 45vh; display: flex; flex-direction: column; }
.vpn-debug-state {
  padding: 4px 8px;
  color: #8fa;
  border-bottom: 1px solid #1b2530;
  line-height: 1.5;
  word-break: break-all;
}
.vpn-debug-stored { color: #9ab; }
.vpn-debug-log {
  overflow-y: auto;
  padding: 4px 8px;
  flex: 1;
}
.vpn-debug-line { white-space: pre-wrap; word-break: break-word; line-height: 1.45; }
.vpn-debug-line.err { color: #ff6b6b; }
.vpn-debug-line.ok { color: #7fe08a; }
</style>
