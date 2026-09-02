import { Capacitor } from '@capacitor/core'
import { Geolocation } from '@capacitor/geolocation'

export interface Coordinates {
  lat: number
  lon: number
}

/**
 * Почему не удалось получить позицию. Вызывающему нужно различие: отказ в
 * разрешении чинится в настройках приложения, выключенные службы геолокации — в
 * системных, а таймаут стоит просто повторить.
 */
export type GeolocationFailure =
  | 'denied'
  | 'services-disabled'
  | 'timeout'
  | 'unavailable'
  | 'unsupported'

export class GeolocationError extends Error {
  constructor(readonly reason: GeolocationFailure, message: string) {
    super(message)
    this.name = 'GeolocationError'
  }
}

const MESSAGES: Record<GeolocationFailure, string> = {
  denied: 'Нет доступа к геопозиции. Разрешите доступ к местоположению в настройках приложения.',
  'services-disabled': 'Геолокация выключена в настройках устройства. Включите её и повторите.',
  timeout: 'Не удалось определить местоположение: нет сигнала. Попробуйте ещё раз на открытом месте.',
  unavailable: 'Не удалось определить местоположение.',
  unsupported: 'Устройство не поддерживает определение местоположения.',
}

export function geolocationMessage(err: unknown): string {
  if (err instanceof GeolocationError) return MESSAGES[err.reason]
  return MESSAGES.unavailable
}

// У холодного устройства нет готовой координаты, а запрос точной в помещении
// может занять куда больше, чем человек согласен ждать. Поэтому первая попытка
// быстро принимает кэшированную или сетевую позицию, и только если она ничего
// не даёт, мы платим за настоящую координату GNSS с таймаутом, которого хватит.
// Прежняя единственная попытка — низкая точность, пять секунд — на большинстве
// Android просто истекала, оставляя вызывающего без координат и без ошибки.
const FAST_ATTEMPT = { enableHighAccuracy: false, timeout: 10000, maximumAge: 60000 }
const PRECISE_ATTEMPT = { enableHighAccuracy: true, timeout: 25000, maximumAge: 0 }

/**
 * Убеждается, что приложению можно читать позицию устройства, и спрашивает
 * пользователя, если это ещё не решено.
 *
 * Android выдаёт геолокацию во время работы, поэтому одной записи в манифесте
 * мало: без этого самый первый getCurrentPosition просто падает.
 */
export async function ensureLocationPermission(): Promise<void> {
  if (!Capacitor.isNativePlatform()) return

  let status
  try {
    status = await Geolocation.checkPermissions()
  } catch (err) {
    // Плагин бросает здесь, когда службы геолокации устройства выключены, —
    // а это другая проблема, нежели разрешение, в котором отказал пользователь.
    throw new GeolocationError('services-disabled', String(err))
  }

  if (status.location === 'granted' || status.coarseLocation === 'granted') return

  if (status.location === 'denied') {
    throw new GeolocationError('denied', 'location permission denied')
  }

  const requested = await Geolocation.requestPermissions({ permissions: ['location'] })
  if (requested.location !== 'granted' && requested.coarseLocation !== 'granted') {
    throw new GeolocationError('denied', 'location permission not granted')
  }
}

function fromWeb(position: GeolocationPosition): Coordinates {
  return { lat: position.coords.latitude, lon: position.coords.longitude }
}

function webPosition(options: PositionOptions): Promise<Coordinates> {
  return new Promise((resolve, reject) => {
    navigator.geolocation.getCurrentPosition(
      (position) => resolve(fromWeb(position)),
      (err) => {
        if (err.code === err.PERMISSION_DENIED) {
          reject(new GeolocationError('denied', err.message))
        } else if (err.code === err.TIMEOUT) {
          reject(new GeolocationError('timeout', err.message))
        } else {
          reject(new GeolocationError('unavailable', err.message))
        }
      },
      options,
    )
  })
}

async function nativePosition(options: PositionOptions): Promise<Coordinates> {
  const position = await Geolocation.getCurrentPosition(options)
  if (!position || !position.coords) {
    throw new GeolocationError('unavailable', 'plugin returned no coordinates')
  }
  return { lat: position.coords.latitude, lon: position.coords.longitude }
}

/**
 * Читает текущую позицию устройства или бросает GeolocationError с причиной,
 * по которой не смог. Никогда не возвращает устаревшие или выдуманные
 * координаты: вызывающий, получивший значение, может на него положиться.
 */
export async function getCurrentCoordinates(): Promise<Coordinates> {
  const native = Capacitor.isNativePlatform()

  if (!native && !navigator.geolocation) {
    throw new GeolocationError('unsupported', 'navigator.geolocation is unavailable')
  }

  await ensureLocationPermission()

  const read = native ? nativePosition : webPosition
  try {
    return await read(FAST_ATTEMPT)
  } catch (err) {
    // Отказ не вылечится повторным вопросом, а спросить дважды означало бы
    // показать пользователю второй запрос разрешения.
    if (err instanceof GeolocationError && (err.reason === 'denied' || err.reason === 'services-disabled')) {
      throw err
    }
    try {
      return await read(PRECISE_ATTEMPT)
    } catch (precise) {
      if (precise instanceof GeolocationError) throw precise
      throw new GeolocationError('timeout', String(precise))
    }
  }
}
