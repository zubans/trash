import { i18n } from '../i18n'

// Localized labels for the native Capacitor camera prompt (CameraSource.Prompt).
// Without these, the action sheet shows Capacitor's English defaults
// ("From Photos" / "Take Picture"). Spread the result into Camera.getPhoto().
export function cameraPromptLabels() {
  const t = i18n.global.t as (key: string) => string
  return {
    promptLabelHeader: t('camera.header'),
    promptLabelCancel: t('camera.cancel'),
    promptLabelPhoto: t('camera.fromGallery'),
    promptLabelPicture: t('camera.takePicture'),
  }
}
