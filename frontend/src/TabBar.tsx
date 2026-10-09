import { useEffect, useRef, useState } from 'react'
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
  sidebarCollapsed,
  onToggleSidebar,
  userLabel,
  onOpenSettings,
  onLogout,
  compact,
  pendingApprovals,
  onOpenApprovals,
}: {
  tabs: OpenTab[]
  activeId: string | null
  onSelectTab: (id: string) => void
  onCloseTab: (id: string) => void
  onReorderTabs: (draggedId: string, targetId: string) => void
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
}) {
  const pendingLabel = `${pendingApprovals} sign-up${pendingApprovals === 1 ? '' : 's'} awaiting approval`
  const [accountAnchor, setAccountAnchor] = useState<HTMLButtonElement | null>(null)
  // The whole tab is the drag source. With a mouse the drag is tracked
  // with pointer events, not HTML5 drag-and-drop: in the macOS Dock web
  // app (Safari "Add to Dock") tabs could be neither moved nor closed,
  // while a normal browser tab worked. The native drag of a draggable
  // tab is the part that differs between the two, and a press on the
  // close button inside it can turn into that drag instead of a click.
  // Pointer events behave the same in a browser tab and in a web app
  // window. Touch keeps the HTML5 path (TOUCH_DRAG above).
  const [draggedTabId, setDraggedTabId] = useState<string | null>(null)
  const [dropTargetTabId, setDropTargetTabId] = useState<string | null>(null)
  // Set when a mouse drag just ended, so the click that follows the
  // release doesn't also select the tab under the pointer.
  const suppressClick = useRef(false)
  const stopPointerDrag = useRef<(() => void) | null>(null)

  useEffect(() => () => stopPointerDrag.current?.(), [])

  function tabIdAt(x: number, y: number): string | null {
    const el = document.elementFromPoint(x, y)?.closest<HTMLElement>('[data-tab-id]')
    return el?.dataset.tabId ?? null
  }

  function handlePointerDown(tabId: string) {
    return (e: React.PointerEvent) => {
      if (TOUCH_DRAG || e.pointerType === 'touch' || e.button !== 0) return
      const startX = e.clientX
      const startY = e.clientY
      let dragging = false
      let target: string | null = null
      const body = document.body.style
      const prevSelect = body.userSelect

      const move = (ev: PointerEvent) => {
        if (!dragging) {
          if (Math.hypot(ev.clientX - startX, ev.clientY - startY) < DRAG_THRESHOLD) return
          dragging = true
          body.userSelect = 'none'
          setDraggedTabId(tabId)
        }
        const over = tabIdAt(ev.clientX, ev.clientY)
        target = over && over !== tabId ? over : null
        setDropTargetTabId(target)
      }
      const end = (ev: PointerEvent) => {
        stop()
        if (!dragging) return
        suppressClick.current = true
        // A click only follows a release over the same element; clear the
        // flag in case none comes.
        window.setTimeout(() => (suppressClick.current = false), 0)
        if (ev.type === 'pointerup' && target) onReorderTabs(tabId, target)
      }
      const cancel = (ev: KeyboardEvent) => {
        if (ev.key !== 'Escape') return
        target = null
        end(new PointerEvent('pointercancel'))
      }
      const stop = () => {
        window.removeEventListener('pointermove', move)
        window.removeEventListener('pointerup', end)
        window.removeEventListener('pointercancel', end)
        window.removeEventListener('keydown', cancel)
        body.userSelect = prevSelect
        setDraggedTabId(null)
        setDropTargetTabId(null)
        stopPointerDrag.current = null
      }
      stopPointerDrag.current?.()
      stopPointerDrag.current = stop
      window.addEventListener('pointermove', move)
      window.addEventListener('pointerup', end)
      window.addEventListener('pointercancel', end)
      window.addEventListener('keydown', cancel)
    }
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
    <div className="tab-bar">
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

      <div className="tab-bar-tabs">
        {tabs.map((tab) => (
          <div
            key={tab.id}
            className={[
              'tab',
              tab.id === activeId && 'active',
              tab.id === draggedTabId && 'tab-dragging',
              tab.id === dropTargetTabId && 'tab-drop-target',
            ]
              .filter(Boolean)
              .join(' ')}
            data-tab-id={tab.id}
            onPointerDown={handlePointerDown(tab.id)}
            draggable={TOUCH_DRAG || undefined}
            onDragStart={TOUCH_DRAG ? handleDragStart(tab.id) : undefined}
            onDragOver={TOUCH_DRAG ? handleDragOver(tab.id) : undefined}
            onDragLeave={TOUCH_DRAG ? () => setDropTargetTabId((cur) => (cur === tab.id ? null : cur)) : undefined}
            onDrop={TOUCH_DRAG ? handleDrop(tab.id) : undefined}
            onDragEnd={TOUCH_DRAG ? handleDragEnd : undefined}
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

      {compact ? (
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
