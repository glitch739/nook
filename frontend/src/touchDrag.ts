// Extras for drags started by touch (main.tsx's drag-and-drop polyfill
// turns a long-press into HTML5 drag events). Mouse drags are native and
// trusted events, so everything here checks isTrusted and leaves them
// alone.
//
//  - A "lifted" drag image: a copy of the item on a solid surface,
//    slightly larger and with a shadow (.touch-drag-ghost in mobile.css),
//    instead of the polyfill's faint half-transparent clone.
//  - A short vibration when the drag starts, where the browser allows it
//    (Android; iOS Safari gives web pages no vibration).
//  - Auto-scroll: holding the item near the top or bottom edge of a
//    scrolling list (the sidebar, the page) scrolls it, even while the
//    finger stays still. The polyfill can only scroll the whole window.

const EDGE = 56 // px from the list's edge where scrolling starts
const MAX_SPEED = 14 // px per frame at the very edge

let ghost: HTMLElement | null = null
let lastX = 0
let lastY = 0
let scrollRaf: number | null = null

function scrollableAt(x: number, y: number): HTMLElement | null {
  for (let el = document.elementFromPoint(x, y) as HTMLElement | null; el; el = el.parentElement) {
    if (el.scrollHeight <= el.clientHeight) continue
    const overflow = getComputedStyle(el).overflowY
    if (overflow === 'auto' || overflow === 'scroll') return el
  }
  return null
}

function scrollStep() {
  scrollRaf = requestAnimationFrame(scrollStep)
  const list = scrollableAt(lastX, lastY)
  if (!list) return
  const rect = list.getBoundingClientRect()
  const fromTop = lastY - rect.top
  const fromBottom = rect.bottom - lastY
  if (fromTop < EDGE) list.scrollTop -= Math.ceil(((EDGE - fromTop) / EDGE) * MAX_SPEED)
  else if (fromBottom < EDGE) list.scrollTop += Math.ceil(((EDGE - fromBottom) / EDGE) * MAX_SPEED)
}

function cleanup() {
  if (scrollRaf !== null) cancelAnimationFrame(scrollRaf)
  scrollRaf = null
  ghost?.remove()
  ghost = null
  document.body.classList.remove('touch-dragging')
}

function onDragStart(e: DragEvent) {
  if (e.isTrusted) return
  const src = e.target as HTMLElement | null
  if (!src || !e.dataTransfer) return
  const rect = src.getBoundingClientRect()
  lastX = e.clientX
  lastY = e.clientY

  // The polyfill copies the drag image's computed style, so the ghost has
  // to be in the document (off screen) when setDragImage is called.
  ghost = src.cloneNode(true) as HTMLElement
  ghost.classList.add('touch-drag-ghost')
  ghost.style.width = `${rect.width}px`
  ghost.style.left = '-9999px'
  ghost.style.top = '0'
  document.body.appendChild(ghost)
  e.dataTransfer.setDragImage(ghost, e.clientX - rect.left, e.clientY - rect.top)

  document.body.classList.add('touch-dragging')
  navigator.vibrate?.(12)
  if (scrollRaf === null) scrollRaf = requestAnimationFrame(scrollStep)
}

function onDragOver(e: DragEvent) {
  if (e.isTrusted) return
  lastX = e.clientX
  lastY = e.clientY
}

function onDragEnd(e: DragEvent) {
  if (!e.isTrusted) cleanup()
}

export function enableTouchDragExtras() {
  document.addEventListener('dragstart', onDragStart, true)
  document.addEventListener('dragover', onDragOver, true)
  document.addEventListener('dragend', onDragEnd, true)
  document.addEventListener('drop', onDragEnd, true)
}
