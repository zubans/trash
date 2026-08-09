import { registerPlugin } from '@capacitor/core'

export interface AppUpdatePlugin {
  getCurrentVersion(): Promise<{ versionCode: number; versionName: string }>
  downloadAndInstall(options: { url: string }): Promise<void>
}

export const AppUpdate = registerPlugin<AppUpdatePlugin>('AppUpdate', {
  web: async () => {
    return {
      getCurrentVersion: async () => ({ versionCode: 0, versionName: '0.0' }),
      downloadAndInstall: async () => {
        throw new Error('In-app updates are not supported in the browser')
      },
    } as AppUpdatePlugin
  },
})
