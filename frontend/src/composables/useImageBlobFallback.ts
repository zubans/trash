import { ref } from 'vue'
import { resolveFileUrl } from '../services/api'

export function useImageBlobFallback() {
  const blobImageCache = ref<Record<string, string>>({})

  const isImageAttachment = (msg: any) => {
    if (!msg || !msg.file_url) return false
    if (msg.file_type === 'image') return true
    const url = msg.file_url.toLowerCase()
    return (
      url.endsWith('.jpg') ||
      url.endsWith('.jpeg') ||
      url.endsWith('.png') ||
      url.endsWith('.webp') ||
      url.endsWith('.gif')
    )
  }

  const getImageSrc = (path?: string) => {
    if (!path) return ''
    if (blobImageCache.value[path]) {
      return blobImageCache.value[path]
    }
    return resolveFileUrl(path)
  }

  const onChatImgError = async (path?: string) => {
    if (!path || blobImageCache.value[path]) return
    const fullUrl = resolveFileUrl(path)
    try {
      const res = await fetch(fullUrl)
      if (res.ok) {
        const blob = await res.blob()
        if (blob.size > 0) {
          blobImageCache.value[path] = URL.createObjectURL(blob)
        }
      }
    } catch (err) {
      console.warn('[useImageBlobFallback] fetch blob fallback failed for:', fullUrl, err)
    }
  }

  return {
    blobImageCache,
    isImageAttachment,
    getImageSrc,
    onChatImgError,
  }
}
