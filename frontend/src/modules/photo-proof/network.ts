import { ref } from 'vue'
import { Capacitor } from '@capacitor/core'
import { Network } from '@capacitor/network'

/**
 * Есть ли сеть. На Android — по плагину (браузерное navigator.onLine в WebView
 * врёт при переключении между Wi-Fi и мобильной сетью), в вебе — по событиям
 * online/offline.
 *
 * «Сеть есть» не значит «сервер ответит»: очередь всё равно разбирает ошибки
 * каждого запроса. Это лишь сигнал, когда пробовать снова.
 */
export const online = ref(typeof navigator === 'undefined' ? true : navigator.onLine !== false)

type Listener = () => void
const listeners = new Set<Listener>()
let started = false

/** Подписка на момент, когда сеть появилась. */
export function onOnline(listener: Listener): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

function setOnline(value: boolean) {
  const cameBack = value && !online.value
  online.value = value
  if (cameBack) listeners.forEach((l) => l())
}

export async function startNetworkWatch(): Promise<void> {
  if (started) return
  started = true
  if (Capacitor.isNativePlatform()) {
    try {
      const status = await Network.getStatus()
      online.value = status.connected
      await Network.addListener('networkStatusChange', (s) => setOnline(s.connected))
      return
    } catch {
      // Плагин недоступен — падаем на события браузера.
    }
  }
  if (typeof window !== 'undefined') {
    window.addEventListener('online', () => setOnline(true))
    window.addEventListener('offline', () => setOnline(false))
  }
}
