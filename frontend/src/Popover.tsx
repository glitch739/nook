import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import './Popover.css'

const VIEWPORT_MARGIN = 8

function computePosition(
  anchorEl: HTMLElement,
  width: number,
  maxHeight: number,
): { top: number; left: number } {
  const rect = anchorEl.getBoundingClientRect()
  let top = rect.bottom + 4
  let left = rect.left
  if (top + maxHeight > window.innerHeight - VIEWPORT_MARGIN) {
    // Not enough room below -- open upward from the anchor instead.
    top = Math.max(VIEWPORT_MARGIN, rect.top - 4 - maxHeight)
  }
  if (left + width > window.innerWidth - VIEWPORT_MARGIN) {
    left = Math.max(VIEWPORT_MARGIN, window.innerWidth - width - VIEWPORT_MARGIN)
  }
  return { top, left }
}

// A small floating panel anchored to a trigger element, portaled into
// document.body and positioned in fixed coordinates off the trigger's
// own bounding rect -- escapes any scrollable/overflow-hidden ancestor
// (the sidebar, a table cell) the way a plain position: absolute child
// can't. Originally written once, inline, for IconPicker.tsx (see
// DEVLOG 2026-09-08); pulled out here once the database-page work
// needed the identical positioning + outside-click-to-close behavior
// for several more popovers (the create-page menu, cell editors, the
// column-options editor) rather than copying it each time.
export default function Popover({
  anchorEl,
  width,
  maxHeight = 400,
  onClose,
  children,
  className,
}: {
  anchorEl: HTMLElement
  width: number
  maxHeight?: number
  onClose: () => void
  children: ReactNode
  className?: string
}) {
  // On a phone a popover's designed width can exceed the screen --
  // clamp both dimensions to the viewport (minus the edge margin).
  const [viewport, setViewport] = useState(() => ({ w: window.innerWidth, h: window.innerHeight }))
  width = Math.min(width, viewport.w - 2 * VIEWPORT_MARGIN)
  maxHeight = Math.min(maxHeight, viewport.h - 2 * VIEWPORT_MARGIN)
  const [pos, setPos] = useState(() => computePosition(anchorEl, width, maxHeight))
  const rootRef = useRef<HTMLDivElement>(null)

  // The sidebar/table scrolling (or a window resize) while the popover
  // is open would otherwise leave it visually detached from its trigger
  // -- capture:true on scroll so this also fires for a scrollable
  // ancestor's own internal scroll, not just window-level scrolling.
  useLayoutEffect(() => {
    function reposition() {
      setPos(computePosition(anchorEl, width, maxHeight))
    }
    function handleResize() {
      setViewport({ w: window.innerWidth, h: window.innerHeight })
      reposition()
    }
    reposition()
    window.addEventListener('scroll', reposition, true)
    window.addEventListener('resize', handleResize)
    return () => {
      window.removeEventListener('scroll', reposition, true)
      window.removeEventListener('resize', handleResize)
    }
  }, [anchorEl, width, maxHeight])

  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (
        rootRef.current &&
        !rootRef.current.contains(e.target as Node) &&
        !anchorEl.contains(e.target as Node)
      ) {
        onClose()
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [onClose, anchorEl])

  // Keyboard use: focus moves in on open (unless the content already
  // focused its own field) and back to the trigger on close.
  useEffect(() => {
    const root = rootRef.current
    if (root && !root.contains(document.activeElement)) menuItems(root)[0]?.focus()
    return () => {
      if (root?.contains(document.activeElement)) anchorEl.focus()
    }
  }, [anchorEl])

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Escape') {
      e.stopPropagation()
      onClose()
      return
    }
    const target = e.target as HTMLElement
    // Arrows keep their own meaning inside selects, textareas and number fields.
    if (target.matches('select, textarea, input[type="number"], input[type="date"]')) return
    const items = menuItems(rootRef.current!)
    if (items.length === 0) return
    const i = items.indexOf(target as HTMLButtonElement)
    const next =
      e.key === 'ArrowDown' ? items[(i + 1) % items.length]
      : e.key === 'ArrowUp' ? items[(i - 1 + items.length) % items.length]
      : e.key === 'Home' ? items[0]
      : e.key === 'End' ? items[items.length - 1]
      : null
    if (!next) return
    e.preventDefault()
    next.focus()
  }

  return createPortal(
    <div
      ref={rootRef}
      className={className ? `popover-panel ${className}` : 'popover-panel'}
      style={{ top: pos.top, left: pos.left, width, maxHeight }}
      onClick={(e) => e.stopPropagation()}
      onKeyDown={handleKeyDown}
    >
      {children}
    </div>,
    document.body,
  )
}

// The panel's usable buttons, in order: what the arrow keys move between.
function menuItems(root: HTMLElement): HTMLButtonElement[] {
  return Array.from(root.querySelectorAll<HTMLButtonElement>('button:not(:disabled)')).filter((b) => !b.closest('[hidden]'))
}
