import { ref } from 'vue'
import { Capacitor } from '@capacitor/core'

export function useImagePreviewModal() {
  const showImagePreviewModal = ref(false)
  const previewImageUrl = ref('')

  const openImagePreview = async (url: string) => {
    if (!url) return
    previewImageUrl.value = url
    showImagePreviewModal.value = true

    if (!url.startsWith('blob:')) {
      onPreviewModalImgError()
    }
  }

  const onPreviewModalImgError = async () => {
    if (!previewImageUrl.value || previewImageUrl.value.startsWith('blob:')) return
    try {
      const res = await fetch(previewImageUrl.value)
      if (res.ok) {
        const blob = await res.blob()
        if (blob.size > 0) {
          previewImageUrl.value = URL.createObjectURL(blob)
        }
      }
    } catch (e) {
      console.warn('[useImagePreviewModal] modal preview fetch fallback failed:', e)
    }
  }

  return {
    showImagePreviewModal,
    previewImageUrl,
    openImagePreview,
    onPreviewModalImgError,
  }
}
