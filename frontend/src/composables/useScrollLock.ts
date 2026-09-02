import { onUnmounted, watch, type Ref } from 'vue'

// Прокрутка страницы — единственный глобал, и заморозить её одновременно хотят
// несколько мест: дашборд следит за тем, «открыта ли хоть одна модалка», а сами
// модалки тоже блокируют. Когда каждый писал document.body.style.overflow
// напрямую, выигрывал отпустивший последним: закрытие потомка размораживало
// страницу под ещё открытым родителем, а хуже того — модалка, снесённая без
// закрытия (ошибка при отрисовке размонтирует её), оставляла страницу замороженной.
//
// Поэтому у стиля здесь ровно один владелец, а вызывающие лишь добавляют и снимают заявки.
let claims = 0
let restoreTo: string | null = null

function acquire() {
  if (claims === 0) {
    restoreTo = document.body.style.overflow
    document.body.style.overflow = 'hidden'
  }
  claims += 1
}

function release() {
  if (claims === 0) return
  claims -= 1
  if (claims === 0) {
    document.body.style.overflow = restoreTo ?? ''
    restoreTo = null
  }
}

/**
 * Замораживает прокрутку страницы, пока `active` истинно.
 *
 * Заявка снимается при размонтировании компонента, поэтому модалка, исчезнувшая
 * без аккуратного закрытия, не может оставить страницу застрявшей.
 */
export function useScrollLock(active: Ref<boolean> | (() => boolean)) {
  let held = false

  const apply = (wanted: boolean) => {
    if (wanted === held) return
    if (wanted) {
      acquire()
    } else {
      release()
    }
    held = wanted
  }

  watch(active, (value) => apply(!!value), { immediate: true })
  onUnmounted(() => apply(false))
}
