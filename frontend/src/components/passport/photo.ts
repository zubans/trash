/**
 * Съёмка паспорта: только живое фото, и снимок подписывается приложением
 * (doc/implementation_plan_delivery_passport.md §3.4).
 *
 * В приложении открывается камера, а не выбор файла: паспорт, принесённый
 * картинкой, ничего не подтверждает. В браузере выбор файла запретить нельзя,
 * поэтому у снимка есть вторая, скрытая сторона — подпись ключом, который
 * сервер выдаёт перед съёмкой, и незаметная метка в изображении. Считает их тот
 * же код, что и для снимков заказов (modules/photo-proof/imageSync), поэтому
 * второй реализации подписи в проекте нет.
 *
 * Проверку человеку не показывают: её итог видит модератор, когда решает,
 * ставить ли статус «проверенный».
 */
import { Capacitor } from '@capacitor/core'
import { Camera, CameraDirection, CameraResultType, CameraSource } from '@capacitor/camera'
import { base64ToBytes, prepareProofPhoto, toRfc3339 } from '../../modules/photo-proof/imageSync'
import type { CaptureKey } from '../../api/passport'

/** Вид снимка в подписи; серверу известна та же строка. */
const PASSPORT_KIND = 'passport'

/** Есть ли своя камера: в браузере остаётся выбор файла. */
export function cameraAvailable(): boolean {
  return Capacitor.isNativePlatform()
}

/** Снимок камерой. null — съёмку закрыли, не сделав кадр. */
export async function shootPassport(): Promise<Uint8Array | null> {
  const photo = await Camera.getPhoto({
    quality: 92,
    resultType: CameraResultType.Uri,
    // Только камера: галерея тут ничего не доказывает.
    source: CameraSource.Camera,
    direction: CameraDirection.Rear,
    correctOrientation: false,
    saveToGallery: false,
  })
  if (!photo.webPath) return null
  return new Uint8Array(await (await fetch(photo.webPath)).arrayBuffer())
}

/** Момент съёмки — до обработки снимка, с точностью до секунды. */
export function shotAt(): Date {
  const at = new Date()
  at.setMilliseconds(0)
  return at
}

export { toRfc3339 }

/**
 * Подписывает снимок. Ключ выдан для области — своего профиля или заказа
 * верификации, — поэтому подпись одной области в другой не годится.
 */
export async function signPassportPhoto(original: Uint8Array, capture: CaptureKey, takenAt: Date): Promise<Uint8Array> {
  return prepareProofPhoto(original, {
    orderId: capture.id,
    symbolCode: PASSPORT_KIND,
    symbolNumber: 0,
    key: base64ToBytes(capture.key),
    takenAt,
    lat: null,
    lon: null,
  })
}
