// A pointer drag tracked on the window (the pointer may leave the handle),
// for the resize and reposition handles: sidebar width, row panel width,
// database column width, cover position.
//
// onMove gets the distance from the start point and runs at most once
// per animation frame: pointermove can fire far more often than the
// screen paints, and a React re-render per event made native inputs in
// the resized area visibly lag. The last pending move is applied on
// release (not dropped), and pointercancel (a touch interrupted by the
// system) ends the drag like pointerup does, so it can't get stuck.
export function startPointerDrag(
  e: { clientX: number; clientY: number; preventDefault(): void },
  opts: {
    onMove: (dx: number, dy: number) => void
    onEnd?: () => void
    /** Body cursor for the duration of the drag. */
    cursor?: string
  },
) {
  e.preventDefault()
  const startX = e.clientX
  const startY = e.clientY
  let lastX = startX
  let lastY = startY
  let raf: number | null = null
  const body = document.body.style
  const prevCursor = body.cursor
  const prevSelect = body.userSelect
  if (opts.cursor) body.cursor = opts.cursor
  body.userSelect = 'none'

  const apply = () => {
    raf = null
    opts.onMove(lastX - startX, lastY - startY)
  }
  const move = (ev: PointerEvent) => {
    lastX = ev.clientX
    lastY = ev.clientY
    if (raf === null) raf = requestAnimationFrame(apply)
  }
  const end = () => {
    if (raf !== null) {
      cancelAnimationFrame(raf)
      apply()
    }
    window.removeEventListener('pointermove', move)
    window.removeEventListener('pointerup', end)
    window.removeEventListener('pointercancel', end)
    body.cursor = prevCursor
    body.userSelect = prevSelect
    opts.onEnd?.()
  }
  window.addEventListener('pointermove', move)
  window.addEventListener('pointerup', end)
  window.addEventListener('pointercancel', end)
}
