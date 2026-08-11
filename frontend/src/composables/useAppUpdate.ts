import { ref, onMounted, onUnmounted } from 'vue'
import { Capacitor } from '@capacitor/core'
import { AppUpdate } from '../plugins/app-update'
import api from '../services/api'

const CHECK_INTERVAL_MS = 60 * 1000

export interface UpdateInfo {
  versionName: string
  versionCode: number
  downloadUrl: string
  forceUpdate: boolean
  releaseNotes: string
}

// Singleton state: multiple components may consume the same update state,
// but only the first mounted component starts the periodic check.
const updateAvailable = ref(false)
const forceUpdate = ref(false)
const downloadUrl = ref<string | null>(null)
const versionName = ref<string | null>(null)
const releaseNotes = ref<string | null>(null)
const errorMsg = ref<string | null>(null)
let activeConsumers = 0
let intervalId: number | undefined

function resolveDownloadUrl(url: string): string {
  if (!url) return ''
  if (url.startsWith('http://') || url.startsWith('https://')) {
    return url
  }
  const base = (import.meta.env.VITE_API_URL as string) || ''
  const separator = base.endsWith('/') || url.startsWith('/') ? '' : '/'
  return `${base}${separator}${url}`
}

async function checkVersion() {
  if (Capacitor.getPlatform() !== 'android') {
    return
  }

  try {
    const current = await AppUpdate.getCurrentVersion()
    const response = await api.get('/app/version', { params: { platform: 'android' } })
    const remote = response.data

    if (remote.version_code > current.versionCode) {
      updateAvailable.value = true
      forceUpdate.value = remote.force_update === true
      downloadUrl.value = resolveDownloadUrl(remote.download_url)
      versionName.value = remote.version_name
      releaseNotes.value = remote.release_notes || ''
    } else {
      reset()
    }
    errorMsg.value = null
  } catch (err: any) {
    errorMsg.value = err.message || 'Failed to check for updates'
    console.error('[AppUpdate] check failed:', err)
  }
}

function reset() {
  updateAvailable.value = false
  forceUpdate.value = false
  downloadUrl.value = null
  versionName.value = null
  releaseNotes.value = null
}

function startChecking() {
  if (activeConsumers === 0) {
    checkVersion()
    intervalId = window.setInterval(checkVersion, CHECK_INTERVAL_MS)
  }
  activeConsumers++
}

function stopChecking() {
  activeConsumers = Math.max(0, activeConsumers - 1)
  if (activeConsumers === 0 && intervalId !== undefined) {
    clearInterval(intervalId)
    intervalId = undefined
  }
}

export function useAppUpdate() {
  onMounted(() => {
    startChecking()
  })

  onUnmounted(() => {
    stopChecking()
  })

  const installUpdate = async () => {
    if (!downloadUrl.value) {
      return
    }
    try {
      await AppUpdate.downloadAndInstall({ url: downloadUrl.value })
    } catch (err: any) {
      errorMsg.value = err.message || 'Failed to install update'
      console.error('[AppUpdate] install failed:', err)
    }
  }

  return {
    updateAvailable,
    forceUpdate,
    downloadUrl,
    versionName,
    releaseNotes,
    errorMsg,
    checkVersion,
    installUpdate,
  }
}
