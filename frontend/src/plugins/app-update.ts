import { registerPlugin, PluginListenerHandle } from '@capacitor/core'

export interface DownloadProgressPayload {
  progress: number
  bytesDownloaded: number
  totalBytes: number
}

export interface AppUpdatePlugin {
  getCurrentVersion(): Promise<{ versionCode: number; versionName: string }>
  downloadAndInstall(options: { url: string }): Promise<void>
  addListener(
    eventName: 'downloadProgress',
    listenerFunc: (progress: DownloadProgressPayload) => void
  ): Promise<PluginListenerHandle>
}

export const AppUpdate = registerPlugin<AppUpdatePlugin>('AppUpdate', {
  web: async () => {
    return {
      getCurrentVersion: async () => ({ versionCode: 0, versionName: '0.0' }),
      downloadAndInstall: async () => {
        throw new Error('In-app updates are not supported in the browser')
      },
      addListener: async () => {
        return { remove: async () => {} }
      },
    } as AppUpdatePlugin
  },
})
