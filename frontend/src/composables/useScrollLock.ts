import { onUnmounted, watch, type Ref } from 'vue'

// Page scrolling is a single global, and several things want to freeze it at
// once: a dashboard tracks "is any modal open" while the modals themselves also
// lock. When each of them wrote document.body.style.overflow directly, whichever
// released last won — a child closing would unfreeze the page under a parent
// that was still open, and, worse, a modal torn down without closing (an error
// during render unmounts it) left the page frozen with no way to recover.
//
// So the style has exactly one owner here, and callers only add and drop claims.
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
 * Freezes page scrolling while `active` is true.
 *
 * The claim is released when the component unmounts, so a modal that goes away
 * without closing cleanly cannot leave the page stuck.
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
