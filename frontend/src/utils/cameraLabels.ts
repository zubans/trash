import { i18n } from '../i18n'

// Локализованные подписи для нативного диалога камеры Capacitor (CameraSource.Prompt).
// Без них панель действий показывает английские умолчания Capacitor
// («From Photos» / «Take Picture»). Разверните результат в Camera.getPhoto().
export function cameraPromptLabels() {
  const t = i18n.global.t as (key: string) => string
  return {
    promptLabelHeader: t('camera.header'),
    promptLabelCancel: t('camera.cancel'),
    promptLabelPhoto: t('camera.fromGallery'),
    promptLabelPicture: t('camera.takePicture'),
  }
}
