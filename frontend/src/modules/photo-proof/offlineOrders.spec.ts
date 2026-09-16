import { describe, it, expect, beforeEach } from 'vitest'
import {
  saveOfflineOrders,
  loadOfflineOrders,
  offlineOrder,
  patchOfflineOrder,
  clearOfflineData,
} from './offlineOrders'
import { MemoryBlobStore } from './blobStore'

beforeEach(() => {
  localStorage.clear()
  localStorage.setItem('userID', 'executor-1')
})

describe('offline orders', () => {
  it('хранит только открытые заказы и без срока годности', () => {
    saveOfflineOrders([
      { id: 'a', status: 'ASSIGNED', photo_proof: { required: true, nonce: 'x' } },
      { id: 'b', status: 'COMPLETED' },
      { id: 'c', status: 'DISPUTED' },
    ])
    const snapshot = loadOfflineOrders()
    expect(snapshot?.orders.map((o) => o.id)).toEqual(['a', 'c'])
    expect(offlineOrder('a')?.photo_proof?.nonce).toBe('x')
  })

  it('не показывает заказы другому пользователю', () => {
    saveOfflineOrders([{ id: 'a', status: 'ASSIGNED' }])
    localStorage.setItem('userID', 'executor-2')
    expect(loadOfflineOrders()).toBeNull()
  })

  it('обновляет заказ на устройстве', () => {
    saveOfflineOrders([{ id: 'a', status: 'ASSIGNED' }])
    patchOfflineOrder('a', { status: 'EXECUTED' })
    expect(offlineOrder('a')?.status).toBe('EXECUTED')
  })

  it('стирается в конце сессии', () => {
    saveOfflineOrders([{ id: 'a', status: 'ASSIGNED' }])
    localStorage.setItem('unrelated', '1')
    clearOfflineData()
    expect(loadOfflineOrders()).toBeNull()
    expect(localStorage.getItem('unrelated')).toBe('1')
  })
})

describe('blob store', () => {
  it('хранит и удаляет байты снимка', async () => {
    const store = new MemoryBlobStore()
    await store.put('p1', new Uint8Array([1, 2, 3]))
    expect(Array.from((await store.get('p1')) || [])).toEqual([1, 2, 3])
    expect(await store.keys()).toEqual(['p1'])
    await store.remove('p1')
    expect(await store.get('p1')).toBeNull()
  })
})
