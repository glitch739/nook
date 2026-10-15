import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import PageIcon from './PageIcon'
import Popover from './Popover'
import './TabBar.css'

// Touch screens (no hover): tabs keep using HTML5 drag-and-drop, which
// main.tsx's polyfill starts with a long-press, as the sidebar does.
const TOUCH_DRAG = typeof window !== 'undefined' && window.matchMedia('(hover: none)').matches

// How far (px) a mouse has to move with the button down before a press
// on a tab becomes a drag instead of a click.
const DRAG_THRESHOLD = 5

export interface OpenTab {
  id: string
  title: string
  icon: string
}

export default function TabBar({
  tabs,
  activeId,
  onSelectTab,
  onCloseTab,
  onReorderTabs,
  onMoveTab,
  sidebarCollapsed,
  onToggleSidebar,
  userLabel,
  onOpenSettings,
  onLogout,
  compact,
  pendingApprovals,
  onOpenApprovals,
  paneId,
  focused = true,
  showToggle = true,
  showAccount = true,
  onDragOut,
  onDropOut,
  onDragOutEnd,
  onTabMenu,
}: {
  tabs: OpenTab[]
  activeId: string | null
  onSelectTab: (id: string) => void
  onCloseTab: (id: string) => void
  onReorderTabs: (draggedId: string, targetId: string) => void
  /** Mouse drag: put draggedId at index toIndex of the new order. */
  onMoveTab: (draggedId: string, toIndex: number) => void
  sidebarCollapsed: boolean
  onToggleSidebar: () => void
  userLabel: string
  onOpenSettings: () => void
  onLogout: () => void
  /** Phone layout: the name / log out cluster collapses into
   * one account button with a menu, leaving the width to the tabs. */
  compact: boolean
  /** Admins only: sign-ups waiting for approval (0 hides the badge). */
  pendingApprovals: number
  onOpenApprovals: () => void
  /** The workspace pane this bar belongs to (Workspace.tsx). */
  paneId?: string
  /** False for a pane other than the focused one: its active tab dims. */
  focused?: boolean
  /** In a split only the top-left bar has the sidebar toggle and only
   * the top-right one the account controls. */
  showToggle?: boolean
  showAccount?: boolean
  /** A mouse drag left this bar (pulled off it, or over another pane):
   * the workspace shows where the tab would land at (x, y). */
  onDragOut?: (tabId: string, x: number, y: number) => void
  /** Released while out: the workspace places the tab. */
  onDropOut?: (tabId: string) => void
  /** The drag came back to this bar or ended: clear the drop marker. */
  onDragOutEnd?: () => void
  /** Right-click on a tab. */
  onTabMenu?: (tabId: string, anchor: HTMLElement) => void
}) {
  const pendingLabel = `${pendingApprovals} sign-up${pendingApprovals === 1 ? '' : 's'} awaiting approval`
  const [accountAnchor, setAccountAnchor] = useState<HTMLButtonElement | null>(null)
  // The whole tab is the drag source. With a mouse the drag is tracked
  // with pointer events, not HTML5 drag-and-drop, so it behaves the same
  // in a browser tab and in the macOS Dock web app (where the native
  // drag of a tab didn't work). The tab is picked up and follows the
  // pointer along the bar, and the other tabs slide aside to open the
  // slot it will land in, including the one after the last tab. Escape
  // or a cancelled pointer puts it back. Touch keeps the HTML5 path
  // (TOUCH_DRAG above), started by a long-press.
  const [draggedTabId, setDraggedTabId] = useState<string | null>(null)
  const [dropTargetTabId, setDropTargetTabId] = useState<string | null>(null)
  // The mouse drag in progress: which tab, how far it has moved (dx),
  // where it started (from) and which slot it would land in (slot), and
  // how far its neighbors move aside (shift: its width plus the gap).
  const [lift, setLift] = useState<{
    id: string
    dx: number
    from: number
    slot: number
    shift: number
    /** Pulled off the bar: the tab stays in place, dimmed, and a copy of
     * it (ghost) follows the pointer over the workspace. */
    out: boolean
  } | null>(null)
  const [ghost, setGhost] = useState<{ x: number; y: number } | null>(null)
  // One render with no transitions, right after a drop: the tabs then
  // swap their slide-aside offsets for their real new positions, which
  // must not animate.
  const [settling, setSettling] = useState(false)
  // The dropped tab's on-screen left edge just before the drop; after
  // the reorder renders it glides from there into its slot.
  const settleFrom = useRef<{ id: string; left: number } | null>(null)
  const tabsRef = useRef<HTMLDivElement>(null)
  // Set when a mouse drag just ended, so the click that follows the
  // release doesn't also select the tab under the pointer.
  const suppressClick = useRef(false)
  const stopPointerDrag = useRef<(() => void) | null>(null)

  useEffect(() => () => stopPointerDrag.current?.(), [])

  useEffect(() => {
    if (!settling) return
    const raf = requestAnimationFrame(() => setSettling(false))
    return () => cancelAnimationFrame(raf)
  }, [settling])

  useLayoutEffect(() => {
    const from = settleFrom.current
    if (!from) return
    settleFrom.current = null
    const el = tabsRef.current?.querySelector<HTMLElement>(`[data-tab-id="${CSS.escape(from.id)}"]`)
    if (!el) return
    const delta = from.left - el.getBoundingClientRect().left
    if (Math.abs(delta) < 1 || window.matchMedia('(prefers-reduced-motion: reduce)').matches) return
    el.animate([{ transform: `translateX(${delta}px)` }, { transform: 'translateX(0)' }], {
      duration: 180,
      easing: 'cubic-bezier(0.2, 0.8, 0.2, 1)',
    })
  })

  function handlePointerDown(tabId: string) {
    return (e: React.PointerEvent<HTMLDivElement>) => {
      if (TOUCH_DRAG || e.pointerType === 'touch' || e.button !== 0) return
      const container = tabsRef.current
      if (!container) return
      const startX = e.clientX
      const startY = e.clientY
      const pointerId = e.pointerId
      const source = e.currentTarget
      // Keeps the pointer's events coming to us even when it leaves the
      // bar or the window mid-drag.
      try {
        source.setPointerCapture(pointerId)
      } catch {
        // Not supported: the window listeners below still work.
      }
      let dragging = false
      let slots: { left: number; width: number }[] = []
      let from = -1
      let shift = 0
      let barRect = container.getBoundingClientRect()
      let startContentX = 0
      let current: { dx: number; slot: number } = { dx: 0, slot: -1 }
      // Off the bar: over the rest of the workspace (another pane's tabs,
      // or a pane's body), where Workspace.tsx picks the drop target.
      let out = false
      let pos = { x: startX, y: startY }
      let raf: number | null = null
      const body = document.body.style
      const prevSelect = body.userSelect
      const prevCursor = body.cursor

      const begin = () => {
        barRect = container.getBoundingClientRect()
        const els = Array.from(container.querySelectorAll<HTMLElement>('[data-tab-id]'))
        slots = els.map((el) => {
          const r = el.getBoundingClientRect()
          return { left: r.left - barRect.left + container.scrollLeft, width: r.width }
        })
        from = els.findIndex((el) => el.dataset.tabId === tabId)
        if (from === -1) return false
        const gap = slots.length > 1 ? slots[1].left - (slots[0].left + slots[0].width) : 0
        shift = slots[from].width + gap
        startContentX = startX - barRect.left + container.scrollLeft
        current = { dx: 0, slot: from }
        body.userSelect = 'none'
        body.cursor = 'grabbing'
        setDraggedTabId(tabId)
        return true
      }

      const update = (clientX: number) => {
        // Holding the tab near either end of an overflowing bar scrolls it.
        if (clientX < barRect.left + 24) container.scrollLeft -= 10
        else if (clientX > barRect.right - 24) container.scrollLeft += 10
        const contentX = clientX - barRect.left + container.scrollLeft
        const me = slots[from]
        const last = slots[slots.length - 1]
        const rawDx = contentX - startContentX
        // Drawn within the row of tabs, but the slot follows the pointer
        // itself: pulled past either end, it is the first or last slot.
        const dx = Math.min(Math.max(rawDx, -me.left), last.left + last.width - (me.left + me.width))
        const center = me.left + me.width / 2 + rawDx
        let slot = 0
        slots.forEach((s, i) => {
          if (i !== from && s.left + s.width / 2 < center) slot++
        })
        current = { dx, slot }
        schedule()
      }

      // One state update per frame, whatever the event rate.
      const schedule = () => {
        if (raf !== null) return
        raf = requestAnimationFrame(() => {
          raf = null
          setLift({ id: tabId, dx: out ? 0 : current.dx, from, slot: out ? from : current.slot, shift, out })
          setGhost(out ? { ...pos } : null)
        })
      }

      // Off the bar: more than a little above or below it, or over
      // another pane (a pane beside this one has its tabs at the same
      // height, so the band alone can't tell).
      const isOut = (x: number, y: number) => {
        if (!onDragOut) return false
        if (y < barRect.top - 16 || y > barRect.bottom + 16) return true
        const pane = document.elementFromPoint(x, y)?.closest<HTMLElement>('[data-pane-id]')
        return !!pane && pane.dataset.paneId !== paneId
      }

      const move = (ev: PointerEvent) => {
        if (ev.pointerId !== pointerId) return
        // Button already up but its pointerup never reached us (released
        // outside the window, focus switch): end the drag instead of
        // letting the tab stick to the cursor until the next click drops it.
        if ((ev.buttons & 1) === 0) {
          finish(false)
          return
        }
        // Safari in full screen on a second display sends a stray move at
        // a fixed offset (seen: -776, +2390) right after the press; taken
        // as real, it turned clicks into drags. Outside the window is never
        // a useful slot anyway.
        if (ev.clientX < 0 || ev.clientY < 0 || ev.clientX > window.innerWidth || ev.clientY > window.innerHeight) return
        if (!dragging) {
          if (Math.hypot(ev.clientX - startX, ev.clientY - startY) < DRAG_THRESHOLD) return
          if (!begin()) {
            stop()
            return
          }
          dragging = true
        }
        pos = { x: ev.clientX, y: ev.clientY }
        if (isOut(ev.clientX, ev.clientY)) {
          out = true
          onDragOut?.(tabId, ev.clientX, ev.clientY)
          schedule()
          return
        }
        if (out) {
          out = false
          onDragOutEnd?.()
        }
        update(ev.clientX)
      }

      const finish = (drop: boolean) => {
        // Before stop(): it clears the workspace's drop target.
        if (dragging && out && drop) onDropOut?.(tabId)
        stop()
        if (!dragging) return
        suppressClick.current = true
        // A click only follows a release over the same element; clear the
        // flag in case none comes.
        window.setTimeout(() => (suppressClick.current = false), 0)
        if (out) return
        if (drop && current.slot !== from) {
          const el = container.querySelector<HTMLElement>(`[data-tab-id="${CSS.escape(tabId)}"]`)
          if (el) settleFrom.current = { id: tabId, left: el.getBoundingClientRect().left }
          setSettling(true)
          onMoveTab(tabId, current.slot)
        }
      }
      const up = (ev: PointerEvent) => {
        if (ev.pointerId === pointerId) finish(ev.type === 'pointerup')
      }
      const key = (ev: KeyboardEvent) => {
        if (ev.key === 'Escape') finish(false)
      }
      const cancel = () => finish(false)
      const stop = () => {
        if (raf !== null) cancelAnimationFrame(raf)
        raf = null
        window.removeEventListener('pointermove', move)
        window.removeEventListener('pointerup', up)
        window.removeEventListener('pointercancel', up)
        window.removeEventListener('keydown', key)
        window.removeEventListener('blur', cancel)
        try {
          source.releasePointerCapture(pointerId)
        } catch {
          // Already released.
        }
        if (out) onDragOutEnd?.()
        body.userSelect = prevSelect
        body.cursor = prevCursor
        setLift(null)
        setGhost(null)
        setDraggedTabId(null)
        setDropTargetTabId(null)
        stopPointerDrag.current = null
      }
      stopPointerDrag.current?.()
      stopPointerDrag.current = stop
      window.addEventListener('pointermove', move)
      window.addEventListener('pointerup', up)
      window.addEventListener('pointercancel', up)
      window.addEventListener('keydown', key)
      // Switching window/app mid-press swallows the pointerup.
      window.addEventListener('blur', cancel)
    }
  }

  // Where a tab is drawn during a mouse drag: the lifted one follows the
  // pointer, the ones between its old and new slot move aside by its width.
  function liftOffset(index: number, tabId: string): number {
    if (!lift) return 0
    if (tabId === lift.id) return lift.dx
    if (index > lift.from && index <= lift.slot) return -lift.shift
    if (index < lift.from && index >= lift.slot) return lift.shift
    return 0
  }

  // HTML5 drag-and-drop, touch screens only (started by the polyfill).
  function handleDragStart(tabId: string) {
    return (e: React.DragEvent) => {
      e.dataTransfer.effectAllowed = 'move'
      e.dataTransfer.setData('text/plain', tabId)
      setDraggedTabId(tabId)
    }
  }

  function handleDragOver(tabId: string) {
    return (e: React.DragEvent) => {
      if (!draggedTabId || draggedTabId === tabId) return
      e.preventDefault()
      setDropTargetTabId(tabId)
    }
  }

  function handleDrop(tabId: string) {
    return (e: React.DragEvent) => {
      e.preventDefault()
      const draggedId = draggedTabId
      setDraggedTabId(null)
      setDropTargetTabId(null)
      if (!draggedId || draggedId === tabId) return
      onReorderTabs(draggedId, tabId)
    }
  }

  function handleDragEnd() {
    setDraggedTabId(null)
    setDropTargetTabId(null)
  }

  return (
    <div className={focused ? 'tab-bar' : 'tab-bar tab-bar-unfocused'}>
      {showToggle && (
      <button
        className="tab-bar-toggle"
        onClick={onToggleSidebar}
        title={sidebarCollapsed ? 'Show sidebar' : 'Hide sidebar'}
        aria-label={sidebarCollapsed ? 'Show sidebar' : 'Hide sidebar'}
      >
        {/* A panel outline whose divider slides with the sidebar:
            further right while it's shown, near the left edge while
            hidden. Same 300ms as the sidebar slide (Sidebar.css). */}
        <svg
          viewBox="0 0 24 24"
          width="18"
          height="18"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <rect x="3" y="3" width="18" height="18" rx="4" />
          <path
            className={sidebarCollapsed ? 'tab-bar-toggle-divider' : 'tab-bar-toggle-divider tab-bar-toggle-divider-open'}
            d="M8 8v8"
          />
        </svg>
      </button>
      )}

      <div
        ref={tabsRef}
        data-pane-tabs={paneId}
        className={['tab-bar-tabs', lift && 'tab-bar-tabs-dragging', settling && 'tab-bar-tabs-settling']
          .filter(Boolean)
          .join(' ')}
      >
        {tabs.map((tab, index) => (
          <div
            key={tab.id}
            className={[
              'tab',
              tab.id === activeId && 'active',
              lift?.id === tab.id && (lift.out ? 'tab-detached' : 'tab-lifted'),
              !lift && tab.id === draggedTabId && 'tab-dragging',
              tab.id === dropTargetTabId && 'tab-drop-target',
            ]
              .filter(Boolean)
              .join(' ')}
            style={
              lift
                ? {
                    transform: `translateX(${liftOffset(index, tab.id)}px)${lift.id === tab.id ? ' scale(1.04)' : ''}`,
                  }
                : undefined
            }
            data-tab-id={tab.id}
            onPointerDown={handlePointerDown(tab.id)}
            // Text selection and the native drag of the icon would start
            // on a mouse press and take the gesture over (Safari).
            onMouseDown={(e) => {
              // Button 1 too: a middle press would start autoscroll.
              if ((!TOUCH_DRAG && e.button === 0) || e.button === 1) e.preventDefault()
            }}
            // Middle click closes, as in browsers and VS Code.
            onAuxClick={(e) => {
              if (e.button === 1) onCloseTab(tab.id)
            }}
            draggable={TOUCH_DRAG || undefined}
            onDragStart={TOUCH_DRAG ? handleDragStart(tab.id) : (e) => e.preventDefault()}
            onDragOver={TOUCH_DRAG ? handleDragOver(tab.id) : undefined}
            onDragLeave={TOUCH_DRAG ? () => setDropTargetTabId((cur) => (cur === tab.id ? null : cur)) : undefined}
            onDrop={TOUCH_DRAG ? handleDrop(tab.id) : undefined}
            onDragEnd={TOUCH_DRAG ? handleDragEnd : undefined}
            onContextMenu={
              onTabMenu
                ? (e) => {
                    e.preventDefault()
                    onTabMenu(tab.id, e.currentTarget)
                  }
                : undefined
            }
            onClick={() => {
              if (suppressClick.current) {
                suppressClick.current = false
                return
              }
              onSelectTab(tab.id)
            }}
          >
            <PageIcon icon={tab.icon} className="tab-icon" />
            <span className="tab-title">{tab.title}</span>
            <button
              className="tab-close"
              draggable={false}
              // Never the start of a tab drag: a press here is a close.
              onPointerDown={(e) => e.stopPropagation()}
              onMouseDown={(e) => e.stopPropagation()}
              onClick={(e) => {
                e.stopPropagation()
                onCloseTab(tab.id)
              }}
              aria-label={`Close ${tab.title}`}
            >
              ×
            </button>
          </div>
        ))}
      </div>

      {ghost &&
        lift &&
        createPortal(
          <div className="tab tab-ghost" style={{ left: ghost.x, top: ghost.y }} aria-hidden="true">
            <PageIcon icon={tabs.find((t) => t.id === lift.id)?.icon ?? ''} className="tab-icon" />
            <span className="tab-title">{tabs.find((t) => t.id === lift.id)?.title}</span>
          </div>,
          document.body,
        )}

      {!showAccount ? null : compact ? (
        <>
          <button
            className="tab-bar-account"
            onClick={(e) => setAccountAnchor(accountAnchor ? null : e.currentTarget)}
            title="Account"
            aria-label="Account menu"
            aria-expanded={accountAnchor !== null}
          >
            {userLabel.trim().charAt(0) || '?'}
            {pendingApprovals > 0 && <span className="pending-dot" aria-label={pendingLabel} />}
          </button>
          {accountAnchor && (
            <Popover
              anchorEl={accountAnchor}
              width={200}
              onClose={() => setAccountAnchor(null)}
              className="tab-bar-account-menu"
            >
              <div className="tab-bar-account-name">{userLabel}</div>
              {pendingApprovals > 0 && (
                <button
                  className="create-menu-item pending-menu-item"
                  onClick={() => {
                    setAccountAnchor(null)
                    onOpenApprovals()
                  }}
                >
                  {pendingLabel}
                </button>
              )}
              <button
                className="create-menu-item"
                onClick={() => {
                  setAccountAnchor(null)
                  onOpenSettings()
                }}
              >
                Settings
              </button>
              <button
                className="create-menu-item create-menu-item-danger"
                onClick={() => {
                  setAccountAnchor(null)
                  onLogout()
                }}
              >
                Log out
              </button>
            </Popover>
          )}
        </>
      ) : (
        <div className="tab-bar-right">
          <div className="tab-bar-user">
            {pendingApprovals > 0 && (
              <button className="pending-badge" onClick={onOpenApprovals} title={pendingLabel}>
                {pendingApprovals} waiting
              </button>
            )}
            <button className="tab-bar-user-name" onClick={onOpenSettings} title="Settings">
              {userLabel}
            </button>
            <button onClick={onLogout}>Log out</button>
          </div>
        </div>
      )}
    </div>
  )
}
