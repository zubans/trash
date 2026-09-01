import { Capacitor } from '@capacitor/core'
import { Geolocation } from '@capacitor/geolocation'

export interface Coordinates {
  lat: number
  lon: number
}

/**
 * Why a position could not be obtained. The caller needs the distinction: a
 * denied permission is fixed in the app's settings, disabled location services
 * are fixed in the system ones, and a timeout is worth simply retrying.
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

// A cold device has no fix to hand out, and asking for a precise one indoors can
// take far longer than a person will wait. So the first attempt accepts a
// cached or network-derived position quickly, and only if that yields nothing
// do we pay for a real GNSS fix with a timeout long enough to actually get one.
// The previous single attempt — low accuracy, five seconds — simply expired on
// most Android devices and left the caller with no coordinates and no error.
const FAST_ATTEMPT = { enableHighAccuracy: false, timeout: 10000, maximumAge: 60000 }
const PRECISE_ATTEMPT = { enableHighAccuracy: true, timeout: 25000, maximumAge: 0 }

/**
 * Makes sure the app may read the device position, asking the user if it has
 * not been decided yet.
 *
 * Android grants location at runtime, so a manifest entry alone is not enough:
 * without this the very first getCurrentPosition simply fails.
 */
export async function ensureLocationPermission(): Promise<void> {
  if (!Capacitor.isNativePlatform()) return

  let status
  try {
    status = await Geolocation.checkPermissions()
  } catch (err) {
    // The plugin throws here when the device's location services are off,
    // which is a different problem from a permission the user declined.
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
 * Reads the device's current position, or throws a GeolocationError saying why
 * it could not. Never resolves with stale or made-up coordinates: a caller that
 * gets a value can rely on it.
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
    // A denial will not be cured by asking again, and asking twice would put a
    // second permission prompt in front of the user.
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
