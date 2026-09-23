/**
 * Шифрование того, что нельзя держать на устройстве открытым: паспорт
 * заказчика, внесённый модератором без сети
 * (doc/implementation_plan_delivery_passport.md §3.3).
 *
 * Ключ AES-GCM создаётся на устройстве неизвлекаемым и хранится в IndexedDB
 * как объект CryptoKey: скопировав файлы очереди или данные браузера, ключ не
 * получить, а сама очередь переживает перезапуск приложения. Каждый шифротекст
 * начинается со случайного вектора инициализации.
 */
export interface Sealer {
  seal(plain: Uint8Array): Promise<Uint8Array>
  open(sealed: Uint8Array): Promise<Uint8Array>
}

const DB = 'passport-sealer'
const STORE = 'keys'
const KEY_ID = 'queue'
const IV_BYTES = 12

// WebCrypto принимает буфер, а не срез чужого: подрезанный Uint8Array
// копируется в собственный ArrayBuffer.
function bytes(data: Uint8Array): ArrayBuffer {
  const copy = new ArrayBuffer(data.byteLength)
  new Uint8Array(copy).set(data)
  return copy
}

function openDb(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DB, 1)
    request.onupgradeneeded = () => request.result.createObjectStore(STORE)
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
  })
}

function idb<T>(db: IDBDatabase, mode: IDBTransactionMode, fn: (s: IDBObjectStore) => IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    const request = fn(db.transaction(STORE, mode).objectStore(STORE))
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
  })
}

/** Шифр на WebCrypto с неизвлекаемым ключом в IndexedDB. */
export class WebCryptoSealer implements Sealer {
  private key: Promise<CryptoKey> | null = null

  /** Доступен ли шифр: без него паспорт без сети не принимается. */
  static available(): boolean {
    return typeof indexedDB !== 'undefined' && !!globalThis.crypto?.subtle
  }

  private getKey(): Promise<CryptoKey> {
    if (!this.key) {
      this.key = (async () => {
        const db = await openDb()
        const stored = await idb<CryptoKey | undefined>(db, 'readonly', (s) => s.get(KEY_ID))
        if (stored) return stored
        const key = await crypto.subtle.generateKey({ name: 'AES-GCM', length: 256 }, false, ['encrypt', 'decrypt'])
        await idb(db, 'readwrite', (s) => s.put(key, KEY_ID))
        return key
      })()
    }
    return this.key
  }

  async seal(plain: Uint8Array): Promise<Uint8Array> {
    const iv = crypto.getRandomValues(new Uint8Array(IV_BYTES))
    const cipher = new Uint8Array(await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, await this.getKey(), bytes(plain)))
    const out = new Uint8Array(IV_BYTES + cipher.length)
    out.set(iv)
    out.set(cipher, IV_BYTES)
    return out
  }

  async open(sealed: Uint8Array): Promise<Uint8Array> {
    const iv = bytes(sealed.subarray(0, IV_BYTES))
    const data = bytes(sealed.subarray(IV_BYTES))
    return new Uint8Array(await crypto.subtle.decrypt({ name: 'AES-GCM', iv }, await this.getKey(), data))
  }
}
