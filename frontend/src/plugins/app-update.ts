import { registerPlugin, PluginListenerHandle } from '@capacitor/core'

export interface DownloadProgressPayload {
  progress: number
  bytesDownloaded: number
  totalBytes: number
}

// apk — скачивание с нашего сервера (debug), rustore — через RuStore,
// browser — RuStore недоступен, APK открывается в браузере
export type UpdateSource = 'apk' | 'rustore' | 'browser'

export interface AppUpdatePlugin {
  getCurrentVersion(): Promise<{ versionCode: number; versionName: string }>
  checkStoreUpdate(): Promise<{ source: UpdateSource; available?: boolean }>
  downloadAndInstall(options: { url: string; force?: boolean }): Promise<{ cancelled?: boolean } | void>
  addListener(
    eventName: 'downloadProgress',
    listenerFunc: (progress: DownloadProgressPayload) => void
  ): Promise<PluginListenerHandle>
}

export const AppUpdate = registerPlugin<AppUpdatePlugin>('AppUpdate', {
  web: async () => {
    return {
      getCurrentVersion: async () => ({ versionCode: 0, versionName: '0.0' }),
      checkStoreUpdate: async () => ({ source: 'apk' }),
      downloadAndInstall: async () => {
        throw new Error('In-app updates are not supported in the browser')
      },
      addListener: async () => {
        return { remove: async () => {} }
      },
    } as AppUpdatePlugin
  },
})
