import { onCacheCleared } from '../../services/cache'
import { blobStore } from './blobStore'
import { clearOfflineData } from './offlineOrders'
import { startNetworkWatch } from './network'

/**
 * Подключает модуль фото-подтверждения к приложению: следит за сетью и стирает
 * всё, что лежит на устройстве, в конце сессии — на общем телефоне следующий
 * вошедший не должен получить чужие заказы, снимки и трек.
 */
export function installPhotoProof(): void {
  onCacheCleared(() => {
    clearOfflineData()
    void blobStore()
      .keys()
      .then((keys) => Promise.all(keys.map((k) => blobStore().remove(k))))
      .catch(() => undefined)
  })
  void startNetworkWatch()
}
