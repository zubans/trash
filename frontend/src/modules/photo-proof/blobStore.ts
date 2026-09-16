import { Capacitor } from '@capacitor/core'
import { Directory, Filesystem } from '@capacitor/filesystem'

/**
 * Хранилище байтов снимков, пока они не ушли на сервер.
 *
 * Снимок, сделанный без сети, обязан пережить закрытие приложения и перезапуск
 * телефона: иначе исполнитель теряет доказательство ровно тогда, когда оно
 * нужнее всего. На Android байты лежат файлами в каталоге данных приложения, в
 * вебе — в IndexedDB. localStorage для этого не годится: несколько мегабайт
 * одного снимка упираются в его квоту.
 */
export interface BlobStore {
  put(key: string, data: Uint8Array): Promise<void>
  get(key: string): Promise<Uint8Array | null>
  remove(key: string): Promise<void>
  /** Все ключи — чтобы при выходе из аккаунта стереть всё. */
  keys(): Promise<string[]>
}

const DIR = 'photo-proof'

function toBase64(bytes: Uint8Array): string {
  let binary = ''
  const chunk = 0x8000
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunk))
  }
  return btoa(binary)
}

function fromBase64(data: string): Uint8Array {
  const binary = atob(data)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
  return bytes
}

/** Файлы в каталоге данных приложения (Android). */
export class FilesystemBlobStore implements BlobStore {
  async put(key: string, data: Uint8Array): Promise<void> {
    await Filesystem.writeFile({
      path: `${DIR}/${key}`,
      data: toBase64(data),
      directory: Directory.Data,
      recursive: true,
    })
  }

  async get(key: string): Promise<Uint8Array | null> {
    try {
      const file = await Filesystem.readFile({ path: `${DIR}/${key}`, directory: Directory.Data })
      return typeof file.data === 'string'
        ? fromBase64(file.data)
        : new Uint8Array(await (file.data as Blob).arrayBuffer())
    } catch {
      return null
    }
  }

  async remove(key: string): Promise<void> {
    try {
      await Filesystem.deleteFile({ path: `${DIR}/${key}`, directory: Directory.Data })
    } catch {
      // Файла уже нет — это и было целью.
    }
  }

  async keys(): Promise<string[]> {
    try {
      const listing = await Filesystem.readdir({ path: DIR, directory: Directory.Data })
      return listing.files.map((f: any) => (typeof f === 'string' ? f : f.name))
    } catch {
      return []
    }
  }
}

/** IndexedDB (веб). */
export class IndexedDbBlobStore implements BlobStore {
  private db: Promise<IDBDatabase> | null = null

  private open(): Promise<IDBDatabase> {
    if (!this.db) {
      this.db = new Promise((resolve, reject) => {
        const request = indexedDB.open('photo-proof', 1)
        request.onupgradeneeded = () => request.result.createObjectStore('blobs')
        request.onsuccess = () => resolve(request.result)
        request.onerror = () => reject(request.error)
      })
    }
    return this.db
  }

  private async run<T>(mode: IDBTransactionMode, fn: (store: IDBObjectStore) => IDBRequest<T>): Promise<T> {
    const db = await this.open()
    return new Promise((resolve, reject) => {
      const request = fn(db.transaction('blobs', mode).objectStore('blobs'))
      request.onsuccess = () => resolve(request.result)
      request.onerror = () => reject(request.error)
    })
  }

  async put(key: string, data: Uint8Array): Promise<void> {
    await this.run('readwrite', (s) => s.put(data, key))
  }

  async get(key: string): Promise<Uint8Array | null> {
    const value = await this.run<any>('readonly', (s) => s.get(key))
    return value ? new Uint8Array(value) : null
  }

  async remove(key: string): Promise<void> {
    await this.run('readwrite', (s) => s.delete(key))
  }

  async keys(): Promise<string[]> {
    const keys = await this.run<IDBValidKey[]>('readonly', (s) => s.getAllKeys())
    return keys.map(String)
  }
}

/** В памяти — для тестов и как запасной вариант без хранилища. */
export class MemoryBlobStore implements BlobStore {
  private items = new Map<string, Uint8Array>()
  async put(key: string, data: Uint8Array) {
    this.items.set(key, data)
  }
  async get(key: string) {
    return this.items.get(key) ?? null
  }
  async remove(key: string) {
    this.items.delete(key)
  }
  async keys() {
    return [...this.items.keys()]
  }
}

let defaultStore: BlobStore | null = null

export function blobStore(): BlobStore {
  if (!defaultStore) {
    if (Capacitor.isNativePlatform()) defaultStore = new FilesystemBlobStore()
    else if (typeof indexedDB !== 'undefined') defaultStore = new IndexedDbBlobStore()
    else defaultStore = new MemoryBlobStore()
  }
  return defaultStore
}
