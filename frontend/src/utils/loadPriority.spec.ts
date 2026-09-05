import { describe, it, expect, vi } from 'vitest'
import { loadByPriority } from './loadPriority'

/** Задача, которую тест завершает вручную, — чтобы проверять именно порядок. */
function deferred(log: string[], name: string) {
  let release: () => void = () => {}
  const task = () =>
    new Promise<void>((resolve) => {
      log.push(`start:${name}`)
      release = () => {
        log.push(`done:${name}`)
        resolve()
      }
    })
  return { task, release: () => release() }
}

describe('loadByPriority', () => {
  it('запускает ступень целиком параллельно', async () => {
    const log: string[] = []
    const a = deferred(log, 'a')
    const b = deferred(log, 'b')

    const done = loadByPriority([[a.task, b.task]])
    expect(log).toEqual(['start:a', 'start:b'])

    a.release()
    b.release()
    await done
  })

  it('следующая ступень ждёт предыдущую', async () => {
    const log: string[] = []
    const first = deferred(log, 'first')
    const second = deferred(log, 'second')

    const done = loadByPriority([[first.task], [second.task]])
    expect(log).toEqual(['start:first'])

    first.release()
    await Promise.resolve()
    await Promise.resolve()
    expect(log).toContain('start:second')

    second.release()
    await done
    expect(log).toEqual(['start:first', 'done:first', 'start:second', 'done:second'])
  })

  // Провалившийся второстепенный запрос не должен уносить с собой историю,
  // которая идёт следом, — иначе одна ошибка обрывала бы загрузку экрана.
  it('ошибка задачи не обрывает очередь', async () => {
    const later = vi.fn(async () => {})

    await loadByPriority([
      [async () => { throw new Error('boom') }],
      [later],
    ])

    expect(later).toHaveBeenCalledTimes(1)
  })

  it('пустые ступени пропускаются', async () => {
    const task = vi.fn(async () => {})
    await loadByPriority([[], [task], []])
    expect(task).toHaveBeenCalledTimes(1)
  })
})
