import { useState } from 'react'
import PageIcon from './PageIcon'
import Popover from './Popover'
import './TabBar.css'

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
  // The whole tab is the drag source, not a separate grip like
  // DatabaseView.tsx's column headers need -- there's no rename/menu
  // click target living on the same element to protect here, just
  // "select this tab," and browsers already suppress a click after an
  // actual drag happened, same as real browser tabs.
  const [draggedTabId, setDraggedTabId] = useState<string | null>(null)
  const [dropTargetTabId, setDropTargetTabId] = useState<string | null>(null)

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
            draggable
            onDragStart={handleDragStart(tab.id)}
            onDragOver={handleDragOver(tab.id)}
            onDragLeave={() => setDropTargetTabId((cur) => (cur === tab.id ? null : cur))}
            onDrop={handleDrop(tab.id)}
            onDragEnd={handleDragEnd}
            onClick={() => onSelectTab(tab.id)}
          >
            <PageIcon icon={tab.icon} className="tab-icon" />
            <span className="tab-title">{tab.title}</span>
            <button
              className="tab-close"
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
