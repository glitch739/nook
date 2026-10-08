import type { ReactNode } from 'react'
import { usePresence } from './usePresence'
import './Collapse.css'

const DURATION_MS = 200

// Animated open/close for a block of sidebar content. The height
// animates via grid-template-rows 0fr <-> 1fr (no measuring), with a
// light fade. Closed content is unmounted once the animation ends (see
// usePresence), same as the plain {open && ...} it replaces. Clipping
// (overflow: hidden) is applied only while closed or animating, so
// menus and drop indicators inside are never cut off when settled open.
export default function Collapse({ open, children }: { open: boolean; children: ReactNode }) {
  const { mounted, shown, animating, finish } = usePresence(open, DURATION_MS)

  function handleTransitionEnd(e: React.TransitionEvent<HTMLDivElement>) {
    if (e.target !== e.currentTarget || e.propertyName !== 'grid-template-rows') return
    finish()
  }

  if (!mounted) return null
  const className = ['collapse', shown && 'collapse-open', (animating || !shown) && 'collapse-clip']
    .filter(Boolean)
    .join(' ')
  return (
    <div className={className} onTransitionEnd={handleTransitionEnd}>
      <div className="collapse-inner">{children}</div>
    </div>
  )
}
