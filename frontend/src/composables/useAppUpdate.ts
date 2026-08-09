import { ref, onMounted, onUnmounted } from 'vue'
import { AppUpdate } from '../plugins/app-update'
import { Capacitor } from '@capacitor/core'
import api from '../services/api'

const CHECK_INTERVAL_MS = 60 * 1000

export function useAppUpdate() {
  const updateAvailable = ref(false)
  const downloadUrl = ref<string | null>(null)
  const versionName = ref<string | null>(null)
  const errorMsg = ref<string | null>(null)

  let intervalId: number | undefined

  const checkVersion = async () => {
    if (Capacitor.getPlatform() !== 'android') {
      return
    }

    try {
      const current = await AppUpdate.getCurrentVersion()
      const response = await api.get('/app/version', { params: { platform: 'android' } })
      const remote = response.data

      if (remote.version_code > current.versionCode) {
        updateAvailable.value = true
        downloadUrl.value = remote.download_url
        versionName.value = remote.version_name
      } else {
        updateAvailable.value = false
        downloadUrl.value = null
        versionName.value = null
      }
      errorMsg.value = null
    } catch (err: any) {
      errorMsg.value = err.message || 'Failed to check for updates'
      console.error('App update check failed:', err)
    }
  }

  const installUpdate = async () => {
    if (!downloadUrl.value) {
      return
    }
    try {
      await AppUpdate.downloadAndInstall({ url: downloadUrl.value })
      updateAvailable.value = false
      errorMsg.value = null
    } catch (err: any) {
      errorMsg.value = err.message || 'Failed to install update'
      console.error('App update install failed:', err)
    }
  }

  onMounted(() => {
    if (Capacitor.getPlatform() === 'android') {
      checkVersion()
      intervalId = window.setInterval(checkVersion, CHECK_INTERVAL_MS)
    }
  })

  onUnmounted(() => {
    if (intervalId !== undefined) {
      clearInterval(intervalId)
    }
  })

  return {
    updateAvailable,
    downloadUrl,
    versionName,
    errorMsg,
    checkVersion,
    installUpdate,
  }
}
