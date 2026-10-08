import { useEffect, useState } from 'react'

// Mount/animate/unmount bookkeeping for something that slides or fades
// in and out (Collapse.tsx, the sidebar, the row peek). While closed,
// the content is unmounted, so hidden things cost no DOM or memory.
//
//   mounted:   render the content at all
//   shown:     apply the "open" styles (the CSS transition runs on change)
//   animating: a transition is in progress (e.g. clip overflow meanwhile)
//   finish:    call from transitionend/animationend; a timer also calls
//              it, for when that event never fires (hidden tab, no
//              transition)
export function usePresence(open: boolean, durationMs: number) {
  const [mounted, setMounted] = useState(open)
  const [shown, setShown] = useState(open)
  const [animating, setAnimating] = useState(false)
  const [settledOpen, setSettledOpen] = useState(open)

  // Mount during render when opening, so the first paint is the closed
  // state and the effect below can animate from it.
  if (open && !mounted) setMounted(true)
  // Opened and closed again before the open animation even started:
  // nothing is showing, so just unmount.
  if (!open && open === settledOpen && mounted && !shown && !animating) setMounted(false)

  useEffect(() => {
    if (open === settledOpen) return
    const reduced = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
    let timer: number | undefined
    // Two frames: the first lets the just-mounted closed state reach the
    // screen, so flipping to "shown" in the second is a real transition
    // and not a jump.
    let raf2 = 0
    const raf1 = requestAnimationFrame(() => {
      raf2 = requestAnimationFrame(() => {
        setSettledOpen(open)
        setAnimating(!reduced)
        setShown(open)
        if (reduced) {
          if (!open) setMounted(false)
          return
        }
        timer = window.setTimeout(() => {
          setAnimating(false)
          if (!open) setMounted(false)
        }, durationMs + 80)
      })
    })
    return () => {
      cancelAnimationFrame(raf1)
      cancelAnimationFrame(raf2)
      window.clearTimeout(timer)
    }
  }, [open, settledOpen, durationMs])

  function finish() {
    setAnimating(false)
    if (!open) setMounted(false)
  }

  return { mounted, shown, animating, finish }
}
