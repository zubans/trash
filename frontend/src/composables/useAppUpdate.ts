import { ref, onMounted, onUnmounted } from 'vue'
import { Capacitor } from '@capacitor/core'
import { AppUpdate } from '../plugins/app-update'
import api, { apiUrl } from '../services/api'

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
const versionCode = ref<number | null>(null)
const releaseNotes = ref<string | null>(null)
const errorMsg = ref<string | null>(null)
let activeConsumers = 0
let intervalId: number | undefined = undefined
const downloadProgress = ref(0)
const bytesDownloaded = ref(0)
const totalBytes = ref(0)
const isDownloading = ref(false)
let listenerHandle: any = null

function resolveDownloadUrl(url: string): string {
  if (!url) return ''
  if (url.startsWith('http://') || url.startsWith('https://')) {
    return url
  }
  const base = apiUrl || ''
  const separator = base.endsWith('/') || url.startsWith('/') ? '' : '/'
  return `${base}${separator}${url}`
}

async function setupProgressListener() {
  if (Capacitor.getPlatform() === 'android' && !listenerHandle) {
    listenerHandle = await AppUpdate.addListener('downloadProgress', (data) => {
      downloadProgress.value = data.progress || 0
      bytesDownloaded.value = data.bytesDownloaded || 0
      totalBytes.value = data.totalBytes || 0
    })
  }
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
      versionCode.value = remote.version_code
      const dismissedCode = localStorage.getItem('dismissed_app_version_code')
      const isDismissed = dismissedCode && Number(dismissedCode) === remote.version_code

      updateAvailable.value = !isDismissed
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
  versionCode.value = null
  releaseNotes.value = null
  downloadProgress.value = 0
  bytesDownloaded.value = 0
  totalBytes.value = 0
  isDownloading.value = false
}

function startChecking() {
  if (activeConsumers === 0) {
    checkVersion()
    setupProgressListener()
    intervalId = window.setInterval(checkVersion, CHECK_INTERVAL_MS)
  }
  activeConsumers++
}

function stopChecking() {
  activeConsumers = Math.max(0, activeConsumers - 1)
  if (activeConsumers === 0) {
    if (intervalId !== undefined) {
      clearInterval(intervalId)
      intervalId = undefined
    }
    if (listenerHandle) {
      listenerHandle.remove()
      listenerHandle = null
    }
  }
}

export function useAppUpdate() {
  onMounted(() => {
    startChecking()
  })

  onUnmounted(() => {
    stopChecking()
  })

  const dismissUpdate = () => {
    if (versionCode.value) {
      localStorage.setItem('dismissed_app_version_code', String(versionCode.value))
    }
    updateAvailable.value = false
  }

  const installUpdate = async () => {
    if (!downloadUrl.value) {
      return
    }
    isDownloading.value = true
    downloadProgress.value = 0
    bytesDownloaded.value = 0
    totalBytes.value = 0
    try {
      await AppUpdate.downloadAndInstall({ url: downloadUrl.value })
    } catch (err: any) {
      errorMsg.value = err.message || 'Failed to install update'
      console.error('[AppUpdate] install failed:', err)
    } finally {
      isDownloading.value = false
    }
  }

  return {
    updateAvailable,
    forceUpdate,
    downloadUrl,
    versionName,
    versionCode,
    releaseNotes,
    errorMsg,
    downloadProgress,
    bytesDownloaded,
    totalBytes,
    isDownloading,
    checkVersion,
    dismissUpdate,
    installUpdate,
  }
}
