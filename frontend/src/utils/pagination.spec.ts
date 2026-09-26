import { describe, it, expect } from 'vitest'
import { appendPage, hasMorePages } from './pagination'

describe('постраничные списки без счётчика', () => {
  it('следующая страница есть, пока пришла полная', () => {
    expect(hasMorePages(100, 100)).toBe(true)
    expect(hasMorePages(37, 100)).toBe(false)
    expect(hasMorePages(0, 100)).toBe(false)
  })

  it('дописывает страницу, пропуская строки, сдвинутые свежей записью', () => {
    const shown = [{ id: 'c' }, { id: 'b' }]
    // Пока читали, пришло новое письмо: offset сдвинулся, и «b» пришла снова.
    const page = [{ id: 'b' }, { id: 'a' }]
    expect(appendPage(shown, page, (m) => m.id).map((m) => m.id)).toEqual(['c', 'b', 'a'])
  })
})
