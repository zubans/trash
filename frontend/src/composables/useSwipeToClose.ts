import { computed, ref } from 'vue'

// Свайп вправо закрывает выдвижное меню. Меню выезжает справа, поэтому жест
// «смахнуть обратно» — вправо; панель едет за пальцем, а не ждёт, пока его
// отпустят, иначе жест не читается как перетаскивание.
//
// Два соседа, с которыми жест обязан уживаться:
// - вертикальная прокрутка меню: направление фиксируется по первым
//   пикселям движения, и вертикальный жест сюда не относится вовсе;
// - глобальный «назад» из App.vue — свайп вправо от левого края. Меню занимает
//   85% ширины, так что его левая кромка на телефоне попадает в ту же зону, и
//   без остановки всплытия один жест и закрывал бы меню, и уводил с экрана.

// Сколько нужно протащить, чтобы меню закрылось. Меньше — панель вернётся на
// место: случайное касание не должно прятать меню.
const CLOSE_DISTANCE = 60
// Быстрый короткий взмах тоже закрывает — так смахивают на телефоне.
const CLOSE_VELOCITY = 0.4 // px/мс
// Сколько пройти, прежде чем решить, горизонтальный это жест или прокрутка.
const AXIS_LOCK = 10

export function useSwipeToClose(onClose: () => void) {
  const offset = ref(0)
  const dragging = ref(false)

  let startX = 0
  let startY = 0
  let startTime = 0
  let axis: 'x' | 'y' | null = null

  const onTouchStart = (event: TouchEvent) => {
    if (event.touches.length !== 1) return
    startX = event.touches[0].clientX
    startY = event.touches[0].clientY
    startTime = Date.now()
    axis = null
    offset.value = 0
  }

  const onTouchMove = (event: TouchEvent) => {
    if (event.touches.length !== 1) return
    const dx = event.touches[0].clientX - startX
    const dy = event.touches[0].clientY - startY
    if (axis === null) {
      if (Math.abs(dx) < AXIS_LOCK && Math.abs(dy) < AXIS_LOCK) return
      axis = Math.abs(dx) > Math.abs(dy) ? 'x' : 'y'
    }
    if (axis !== 'x') return
    dragging.value = true
    // Влево панель не тянется: дальше открытого ей ехать некуда.
    offset.value = Math.max(0, dx)
  }

  const onTouchEnd = (event: TouchEvent) => {
    // Жест внутри открытого меню — его жест. До глобального «назад» он не
    // доходит ни в каком виде, даже если оказался не свайпом.
    event.stopPropagation()
    const distance = offset.value
    const elapsed = Math.max(1, Date.now() - startTime)
    const wasHorizontal = axis === 'x'
    dragging.value = false
    offset.value = 0
    axis = null
    if (wasHorizontal && (distance > CLOSE_DISTANCE || distance / elapsed > CLOSE_VELOCITY)) {
      onClose()
    }
  }

  // Пока палец тащит панель, она стоит там, где палец, и без анимации; после
  // отпускания стиль снимается, и класс .open/.sidebar доводит её сам.
  const style = computed(() =>
    dragging.value && offset.value > 0
      ? { transform: `translateX(${offset.value}px)`, transition: 'none' }
      : undefined,
  )

  return {
    style,
    handlers: { onTouchStart, onTouchMove, onTouchEnd },
  }
}
