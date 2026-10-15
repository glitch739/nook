import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { getPage, type Page, type Property, type SelectOption } from './api'
import EditableIcon from './EditableIcon'
import Editor from './Editor'
import PropertyCell from './PropertyCell'
import { startPointerDrag } from './pointerDrag'

const DEFAULT_PANEL_WIDTH = 384 // 24rem, matches the panel's old fixed CSS width
const MIN_PANEL_WIDTH = 320
const MAX_PANEL_WIDTH_RATIO = 0.5 // never past the middle of the screen

// The side panel a table row opens into (ADR 0004) -- title, the same
// property cells the table itself uses, and the row's own rich-text
// body underneath, reusing Editor.tsx unchanged. Fully self-contained:
// fetches/saves the row's content_json itself (with its own small
// debounce, mirroring App.tsx's pattern for the main editor) rather
// than routing through App.tsx's tab/activeId machinery, since a row
// isn't a tab -- opening one keeps the table visible, it doesn't
// navigate away from it.
export default function RowDetailPanel({
  rowId,
  title,
  icon,
  properties,
  values,
  onClose,
  onOpenFullPage,
  onOpenBeside,
  container,
  closing,
  onExited,
  onRenameRow,
  onChangeRowIcon,
  onPropertyChange,
  onOptionsChanged,
  readOnly = false,
}: {
  rowId: string
  title: string
  icon: string
  properties: Property[]
  values: Record<string, unknown>
  onClose: () => void
  // True while sliding out after a close; onExited fires when that
  // animation ends so the parent can unmount it.
  closing: boolean
  onExited: () => void
  // Opens this row as a full page in its own tab (the same as clicking
  // it in the sidebar), so the pointer doesn't have to travel there.
  onOpenFullPage: () => void
  /** Opens the row in a new pane beside the table (split workspace). */
  onOpenBeside?: () => void
  /** The pane the table is in: rendered into it, so in a split it slides
   * in over that pane only (App.css); otherwise it's fixed to the window. */
  container?: HTMLElement | null
  onRenameRow: (title: string) => void
  onChangeRowIcon: (icon: string) => void
  onPropertyChange: (propertyId: string, value: unknown) => void
  onOptionsChanged: (propertyId: string, options: SelectOption[]) => void
  /** A viewer: everything shown, nothing editable. */
  readOnly?: boolean
}) {
  const [page, setPage] = useState<Page | null>(null)
  const [draftTitle, setDraftTitle] = useState(title)
  // Not reset per row -- the panel component instance persists across
  // switching between rows (DatabaseView only ever unmounts it by
  // closing it entirely), so a resize sticks for as long as it stays
  // open, same as Notion's own side peek.
  const [panelWidth, setPanelWidth] = useState(DEFAULT_PANEL_WIDTH)

  useEffect(() => {
    setDraftTitle(title)
  }, [title])

  useEffect(() => {
    let cancelled = false
    setPage(null)
    getPage(rowId)
      .then((p) => {
        if (!cancelled) setPage(p)
      })
      .catch((e) => console.error('failed to load row', e))
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rowId])

  function commitTitle() {
    const trimmed = draftTitle.trim()
    onRenameRow(trimmed || title)
  }

  // Same window-level Pointer Events pattern as PageCover.tsx's cover
  // reposition and DatabaseView.tsx's column resize -- handleMove/
  // handleUp defined fresh inside the handler, closing over this drag's
  // own startX/startWidth. The panel is anchored to the right edge, so
  // dragging the handle *left* (negative clientX delta) should *widen*
  // it, the opposite sign from a plain "moved right = bigger" resize.
  //
  // setPanelWidth is throttled to one call per animation frame rather
  // than once per pointermove, same reasoning as DatabaseView.tsx's
  // column resize -- forcing a React re-render on every single
  // pointermove was outpacing the browser's own repaint of the date
  // input's native calendar icon, which visibly lagged behind the
  // input box mid-drag as a result.
  function handlePanelResizeStart(e: React.PointerEvent) {
    const startWidth = panelWidth
    startPointerDrag(e, {
      cursor: 'ew-resize',
      // Anchored to the right edge: dragging left (negative dx) widens it.
      onMove: (dx) => {
        const split = container?.closest('.workspace-grid-split')
        const maxWidth = (split && container ? container.clientWidth : window.innerWidth) * MAX_PANEL_WIDTH_RATIO
        setPanelWidth(Math.min(maxWidth, Math.max(MIN_PANEL_WIDTH, startWidth - dx)))
      },
    })
  }

  // Portaled to document.body, same reasoning as Popover.tsx: a side
  // peek that's supposed to float over the whole app (table, tab bar,
  // sidebar) can't be a DOM descendant of .database-view without an
  // ancestor's overflow/stacking context fighting it. In a split it
  // goes into its own pane instead, so it never covers the others.
  return createPortal(
    <div
      className={closing ? 'row-detail-panel row-detail-panel-closing' : 'row-detail-panel'}
      style={{ width: panelWidth }}
      onAnimationEnd={(e) => {
        if (e.target === e.currentTarget && e.animationName === 'row-detail-slide-out') onExited()
      }}
    >
      <div className="row-detail-resize-handle" onPointerDown={handlePanelResizeStart} />
      <div className="row-detail-toolbar">
        <button
          type="button"
          className="row-detail-open-full"
          onClick={onOpenFullPage}
          title="Open as full page"
          aria-label="Open as full page"
        >
          <svg
            viewBox="0 0 24 24"
            width="16"
            height="16"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <path d="M15 3h6v6" />
            <path d="m21 3-7 7" />
            <path d="m3 21 7-7" />
            <path d="M9 21H3v-6" />
          </svg>
        </button>
        {onOpenBeside && (
          <button
            type="button"
            className="row-detail-open-full"
            onClick={onOpenBeside}
            title="Open beside the table"
            aria-label="Open beside the table"
          >
            <svg
              viewBox="0 0 24 24"
              width="16"
              height="16"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
            >
              <rect x="3" y="3" width="18" height="18" rx="2" />
              <path d="M12 3v18" />
            </svg>
          </button>
        )}
        <button type="button" className="row-detail-close" onClick={onClose} aria-label="Close">
          ×
        </button>
      </div>

      <div className="row-detail-title-row">
        <EditableIcon icon={icon} onChange={onChangeRowIcon} readOnly={readOnly} />
        <input
          className="row-detail-title-input"
          value={draftTitle}
          placeholder="Untitled"
          readOnly={readOnly}
          onChange={(e) => setDraftTitle(e.target.value)}
          onBlur={commitTitle}
          onKeyDown={(e) => {
            if (e.key === 'Enter') (e.target as HTMLInputElement).blur()
          }}
        />
      </div>

      <div className="row-detail-properties">
        {properties.map((prop) => (
          <div className="row-detail-property" key={prop.id}>
            <span className="row-detail-property-name">{prop.name}</span>
            <PropertyCell
              property={prop}
              value={values[prop.id]}
              onChange={(v) => onPropertyChange(prop.id, v)}
              onOptionsChanged={(options) => onOptionsChanged(prop.id, options)}
              readOnly={readOnly}
            />
          </div>
        ))}
        {properties.length === 0 && <p className="row-detail-no-properties">No columns yet.</p>}
      </div>

      <div className="row-detail-body">
        {page && page.id === rowId ? (
          <Editor key={rowId} pageId={rowId} initialContent={page.content_json} />
        ) : (
          <p className="hint">Loading…</p>
        )}
      </div>
    </div>,
    container ?? document.body,
  )
}
