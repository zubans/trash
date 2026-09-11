import { afterEach, describe, expect, it, vi } from 'vitest'

import { useSwipeToClose } from './useSwipeToClose'

// Касание в духе TouchEvent: ровно те поля, что читает жест.
const touch = (x: number, y: number) =>
  ({
    touches: [{ clientX: x, clientY: y }],
    changedTouches: [{ clientX: x, clientY: y }],
    stopPropagation: vi.fn(),
  }) as unknown as TouchEvent & { stopPropagation: ReturnType<typeof vi.fn> }

// Проводит палец по точкам за время duration и отпускает.
const swipe = (points: [number, number][], duration: number) => {
  const onClose = vi.fn()
  const { style, handlers } = useSwipeToClose(onClose)
  const now = vi.spyOn(Date, 'now')
  now.mockReturnValue(1_000)
  handlers.onTouchStart(touch(...points[0]))
  const styles: unknown[] = []
  for (const point of points.slice(1)) {
    handlers.onTouchMove(touch(...point))
    styles.push(style.value)
  }
  now.mockReturnValue(1_000 + duration)
  const end = touch(...points[points.length - 1])
  handlers.onTouchEnd(end)
  return { onClose, end, styles, styleAfter: style.value }
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useSwipeToClose', () => {
  it('закрывает меню свайпом вправо', () => {
    const { onClose } = swipe([[100, 300], [140, 302], [200, 305]], 500)
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('панель едет за пальцем, а после отпускания стиль снимается', () => {
    const { styles, styleAfter } = swipe([[100, 300], [140, 300], [180, 300]], 500)
    expect(styles).toEqual([
      { transform: 'translateX(40px)', transition: 'none' },
      { transform: 'translateX(80px)', transition: 'none' },
    ])
    expect(styleAfter).toBeUndefined()
  })

  it('короткий медленный сдвиг не закрывает — панель возвращается', () => {
    const { onClose } = swipe([[100, 300], [130, 300], [140, 300]], 1_000)
    expect(onClose).not.toHaveBeenCalled()
  })

  it('короткий быстрый взмах закрывает', () => {
    const { onClose } = swipe([[100, 300], [130, 300], [150, 300]], 60)
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('вертикальная прокрутка меню не закрывает его, даже с уходом вправо', () => {
    const { onClose, styles } = swipe([[100, 300], [104, 340], [170, 420]], 300)
    expect(onClose).not.toHaveBeenCalled()
    expect(styles.every((value) => value === undefined)).toBe(true)
  })

  it('свайп влево не закрывает и панель не тянет', () => {
    const { onClose, styles } = swipe([[200, 300], [150, 300], [60, 300]], 200)
    expect(onClose).not.toHaveBeenCalled()
    expect(styles.every((value) => value === undefined)).toBe(true)
  })

  it('жест в меню не доходит до глобального «назад» из App.vue', () => {
    const closing = swipe([[40, 300], [90, 300], [160, 300]], 300)
    expect(closing.end.stopPropagation).toHaveBeenCalled()
    // И тап без движения тоже: иначе касание у левой кромки меню
    // считалось бы началом жеста «назад».
    const tap = swipe([[40, 300]], 50)
    expect(tap.end.stopPropagation).toHaveBeenCalled()
    expect(tap.onClose).not.toHaveBeenCalled()
  })
})
