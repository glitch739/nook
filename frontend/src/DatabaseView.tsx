import { useEffect, useRef, useState } from 'react'
import {
  createProperty as createPropertyApi,
  deleteProperty as deletePropertyApi,
  listRows,
  reorderColumns as reorderColumnsApi,
  setRowProperty as setRowPropertyApi,
  TITLE_COLUMN_ID,
  updateProperty as updatePropertyApi,
  type DatabaseTable,
  type Property,
  type PropertyType,
  type Row,
  type SelectOption,
} from './api'
import { useLiveRefresh, useLocalWrites } from './liveEvents'
import { usePresence } from './usePresence'
import { startPointerDrag } from './pointerDrag'
import './DatabaseView.css'
import PageIcon from './PageIcon'
import Popover from './Popover'
import PropertyCell from './PropertyCell'
import RowDetailPanel from './RowDetailPanel'
import { findPage, type PageNode, type TeamSpace } from './sidebarData'

// Row peek slide-out; keep in sync with .row-detail-panel-closing.
const ROW_PANEL_EXIT_MS = 220

// Column widths (px). Only the title's default differs -- it usually
// carries an icon plus a delete button alongside the text, so it needs
// more room by default than a plain property column does. Actual
// widths only diverge from these once the user drags a resize handle;
// nothing is persisted server-side yet, so they reset on reload.
const DEFAULT_COLUMN_WIDTH = 180
const DEFAULT_TITLE_WIDTH = 260
const ADD_COLUMN_WIDTH = 40
const MIN_COLUMN_WIDTH = 80

const PROPERTY_TYPE_LABELS: Record<PropertyType, string> = {
  text: 'Text',
  select: 'Select (e.g. Status)',
  date: 'Date',
  person: 'Person',
  number: 'Number',
  checkbox: 'Checkbox',
}

// The "+" at the end of the header row -- name + type, then
// createProperty. A column's type is fixed after creation (see
// databases.Store.UpdateProperty's own comment on why), so this is the
// only place it's ever chosen.
function AddPropertyMenu({
  anchorEl,
  onCreate,
  onClose,
}: {
  anchorEl: HTMLElement
  onCreate: (name: string, type: PropertyType) => void
  onClose: () => void
}) {
  const [type, setType] = useState<PropertyType>('text')
  // Starts pre-filled with the default type's own label ("Text") --
  // picking a different type keeps auto-filling the name to match it,
  // right up until the user actually types something themselves
  // (nameTouched), at which point their own name wins and stops being
  // overwritten by further type clicks.
  const [name, setName] = useState(PROPERTY_TYPE_LABELS.text)
  const [nameTouched, setNameTouched] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    inputRef.current?.focus()
    inputRef.current?.select()
  }, [])

  function chooseType(t: PropertyType) {
    setType(t)
    if (!nameTouched) setName(PROPERTY_TYPE_LABELS[t])
  }

  function commit() {
    const trimmed = name.trim()
    if (trimmed) onCreate(trimmed, type)
    onClose()
  }

  return (
    <Popover anchorEl={anchorEl} width={220} maxHeight={280} onClose={onClose} className="add-property-menu">
      <input
        ref={inputRef}
        className="add-property-input"
        placeholder="Column name…"
        value={name}
        onChange={(e) => {
          setName(e.target.value)
          setNameTouched(true)
        }}
        onKeyDown={(e) => {
          if (e.key === 'Enter') commit()
          if (e.key === 'Escape') onClose()
        }}
      />
      <div className="add-property-types">
        {(Object.keys(PROPERTY_TYPE_LABELS) as PropertyType[]).map((t) => (
          <button
            key={t}
            type="button"
            className={t === type ? 'add-property-type active' : 'add-property-type'}
            onClick={() => chooseType(t)}
          >
            {PROPERTY_TYPE_LABELS[t]}
          </button>
        ))}
      </div>
      <button type="button" className="add-property-submit" onClick={commit}>
        Create
      </button>
    </Popover>
  )
}

// The strip at a header's right edge that resizes the column via
// Pointer Events -- a separate drag source from the grip (HTML5 DnD,
// reorders the column instead). Shared by ColumnHeader and
// TitleColumnHeader rather than inlined twice.
function ColumnResizeHandle({
  onPointerDown,
  active,
}: {
  onPointerDown: (e: React.PointerEvent) => void
  active: boolean
}) {
  return (
    <div
      className={active ? 'db-th-resize-handle db-th-resize-handle-active' : 'db-th-resize-handle'}
      onPointerDown={onPointerDown}
    />
  )
}

// A column header: a drag handle (hover-revealed, like Notion's own --
// only that small grip is the actual HTML5 drag source, not the whole
// cell, so clicking the name/menu still works normally instead of
// fighting with drag-start), its name, and a small menu to rename or
// delete it.
function ColumnHeader({
  property,
  onRename,
  onDelete,
  isDragging,
  isDropTarget,
  onDragStart,
  onDragOver,
  onDragLeave,
  onDrop,
  onDragEnd,
  onResizeStart,
  isResizing,
}: {
  property: Property
  onRename: (name: string) => void
  onDelete: () => void
  isDragging: boolean
  isDropTarget: boolean
  onDragStart: (e: React.DragEvent) => void
  onDragOver: (e: React.DragEvent) => void
  onDragLeave: () => void
  onDrop: (e: React.DragEvent) => void
  onDragEnd: () => void
  onResizeStart: (e: React.PointerEvent) => void
  isResizing: boolean
}) {
  const [menuAnchor, setMenuAnchor] = useState<HTMLButtonElement | null>(null)
  const [renaming, setRenaming] = useState(false)
  const [draft, setDraft] = useState(property.name)
  const btnRef = useRef<HTMLButtonElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (renaming) {
      inputRef.current?.focus()
      inputRef.current?.select()
    }
  }, [renaming])

  function commitRename() {
    const trimmed = draft.trim()
    if (trimmed) onRename(trimmed)
    setRenaming(false)
  }

  const className = [
    'db-th',
    isDragging && 'db-th-dragging',
    isDropTarget && 'db-th-drop-target',
  ]
    .filter(Boolean)
    .join(' ')

  return (
    <th className={className} onDragOver={onDragOver} onDragLeave={onDragLeave} onDrop={onDrop}>
      {/* Flex lives on this inner div, not the <th> itself -- a <th>/
          <td> given display: flex directly stops participating in the
          table's own row-height/cell-box algorithm the way its plain
          table-cell siblings do, which is exactly what caused the row
          misalignment fixed earlier this session (.db-td-title-inner
          in DatabaseView.css carries the same note). */}
      <div className="db-th-inner">
        <span
          className="db-th-grip"
          draggable
          title="Drag to reorder"
          onDragStart={onDragStart}
          onDragEnd={onDragEnd}
        >
          ⠿
        </span>
        {renaming ? (
          <input
            ref={inputRef}
            className="db-th-input"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onBlur={commitRename}
            onKeyDown={(e) => {
              if (e.key === 'Enter') commitRename()
              if (e.key === 'Escape') {
                setDraft(property.name)
                setRenaming(false)
              }
            }}
          />
        ) : (
          <button ref={btnRef} type="button" className="db-th-btn" onClick={() => setMenuAnchor(btnRef.current)}>
            {property.name}
          </button>
        )}
      </div>
      {menuAnchor && (
        <Popover
          anchorEl={menuAnchor}
          width={160}
          maxHeight={100}
          onClose={() => setMenuAnchor(null)}
          className="column-menu"
        >
          <button
            type="button"
            className="column-menu-item"
            onClick={() => {
              setDraft(property.name)
              setRenaming(true)
              setMenuAnchor(null)
            }}
          >
            Rename
          </button>
          <button
            type="button"
            className="column-menu-item column-menu-danger"
            onClick={() => {
              onDelete()
              setMenuAnchor(null)
            }}
          >
            Delete column
          </button>
        </Popover>
      )}
      <ColumnResizeHandle onPointerDown={onResizeStart} active={isResizing} />
    </th>
  )
}

// The title ("Name") column's header -- draggable/reorderable among the
// real property columns exactly like ColumnHeader, just without a
// rename/delete menu (the title column is neither renamed nor
// deletable; it's the row's own title, internal/pages already owns
// that). Kept as a separate component rather than a special case
// inside ColumnHeader since there's no Property to hand it and no
// rename/delete affordance to wire up -- forcing both into one
// component would mean threading a bunch of "only if this isn't the
// title column" conditionals through it instead.
function TitleColumnHeader({
  isDragging,
  isDropTarget,
  onDragStart,
  onDragOver,
  onDragLeave,
  onDrop,
  onDragEnd,
  onResizeStart,
  isResizing,
}: {
  isDragging: boolean
  isDropTarget: boolean
  onDragStart: (e: React.DragEvent) => void
  onDragOver: (e: React.DragEvent) => void
  onDragLeave: () => void
  onDrop: (e: React.DragEvent) => void
  onDragEnd: () => void
  onResizeStart: (e: React.PointerEvent) => void
  isResizing: boolean
}) {
  const className = [
    'db-th',
    'db-th-title',
    isDragging && 'db-th-dragging',
    isDropTarget && 'db-th-drop-target',
  ]
    .filter(Boolean)
    .join(' ')

  return (
    <th className={className} onDragOver={onDragOver} onDragLeave={onDragLeave} onDrop={onDrop}>
      <div className="db-th-inner">
        <span
          className="db-th-grip"
          draggable
          title="Drag to reorder"
          onDragStart={onDragStart}
          onDragEnd={onDragEnd}
        >
          ⠿
        </span>
        <span className="db-th-btn db-th-label">Name</span>
      </div>
      <ColumnResizeHandle onPointerDown={onResizeStart} active={isResizing} />
    </th>
  )
}

// A row's own title cell -- single click opens the row detail panel
// (RowDetailPanel), same as before; double-click on the title text
// itself switches to inline editing instead, same
// double-click-to-rename convention as every other name in the app
// (Sidebar.tsx's page/space/group rows) and consistent with every
// *other* column in this same table already being directly editable
// in place. Previously the title was the one cell that couldn't be --
// any click at all, including a plain single click meant to just fix a
// typo, forced a trip through the side panel.
//
// A plain onClick+onDoubleClick pair isn't enough here: a native
// double-click still fires a real click first (click, click, dblclick,
// in that order), so onOpen would fire once -- briefly sliding the
// whole side panel open -- before the second click's dblclick handler
// ever gets a chance to switch to renaming. That's a harmless "flash
// select" for Sidebar.tsx's own double-click-to-rename (reselecting an
// already-open tab is a no-op), but opening/closing this whole panel is
// a real, visible glitch, not a no-op. Debouncing the click instead --
// wait to see if a second one arrives before actually opening -- is the
// standard fix.
function TitleCell({
  icon,
  title,
  onOpen,
  onRename,
  onArchive,
}: {
  icon: string
  title: string
  onOpen: () => void
  onRename: (title: string) => void
  onArchive: () => void
}) {
  const [renaming, setRenaming] = useState(false)
  const [draft, setDraft] = useState(title)
  const inputRef = useRef<HTMLInputElement>(null)
  const clickTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    if (renaming) {
      inputRef.current?.focus()
      inputRef.current?.select()
    }
  }, [renaming])

  // Cancel a pending debounced open if this cell unmounts mid-wait (the
  // row gets archived, or the table re-renders it away) before the
  // timer fires.
  useEffect(() => () => {
    if (clickTimer.current) clearTimeout(clickTimer.current)
  }, [])

  function handleTitleClick() {
    if (clickTimer.current) return
    clickTimer.current = setTimeout(() => {
      clickTimer.current = null
      onOpen()
    }, 220)
  }

  function handleTitleDoubleClick() {
    if (clickTimer.current) {
      clearTimeout(clickTimer.current)
      clickTimer.current = null
    }
    setDraft(title)
    setRenaming(true)
  }

  function commitRename() {
    const trimmed = draft.trim()
    onRename(trimmed)
    setRenaming(false)
  }

  return (
    <div className="db-td-title-inner">
      <button type="button" className="db-row-title-btn" onClick={onOpen}>
        <PageIcon icon={icon} className="page-icon" />
      </button>
      {renaming ? (
        <input
          ref={inputRef}
          className="db-row-title-input"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onClick={(e) => e.stopPropagation()}
          onBlur={commitRename}
          onKeyDown={(e) => {
            if (e.key === 'Enter') commitRename()
            if (e.key === 'Escape') {
              setDraft(title)
              setRenaming(false)
            }
          }}
        />
      ) : (
        <span
          className={title ? 'db-row-title' : 'db-row-title cell-empty'}
          onClick={handleTitleClick}
          onDoubleClick={handleTitleDoubleClick}
          title="Double-click to rename"
        >
          {title || 'Untitled'}
        </span>
      )}
      <button
        type="button"
        className="db-row-delete"
        title="Archive row"
        aria-label="Archive row"
        onClick={onArchive}
      >
        🗑
      </button>
    </div>
  )
}

// Every column id (the title column plus every property), sorted by
// position -- plain string comparison, since both use the same
// zero-padded scheme (positionWidth on the backend). This is what
// actually decides render order for both <thead> and every <tbody> row.
function sortedColumnIds(table: DatabaseTable): string[] {
  const entries = [
    { id: TITLE_COLUMN_ID, position: table.title_column_position },
    ...table.properties.map((p) => ({ id: p.id, position: p.position })),
  ]
  entries.sort((a, b) => a.position.localeCompare(b.position))
  return entries.map((e) => e.id)
}

// A database page's table view (ADR 0004): columns come from
// internal/databases' properties, rows are the page's own children
// (reusing internal/pages entirely -- see the ADR). Owns its own table
// state locally rather than living in App.tsx's teamspaces tree, since
// nothing outside this page needs to know about individual cell values;
// row creation/rename/icon/archive still go through the callbacks below
// so the sidebar tree (which does show rows, as ordinary nested pages)
// stays in sync.
export default function DatabaseView({
  pageId,
  spaceId,
  teamspaces,
  onCreateRow,
  onArchiveRow,
  onRenameRow,
  onChangeRowIcon,
  onOpenPage,
}: {
  pageId: string
  spaceId: string
  // The sidebar tree, App.tsx's own source of truth for every page's
  // icon/title -- a row is just another page in it. Needed so an edit
  // made *there* (icon picker or rename in the sidebar itself) reaches
  // this component's own row cache too, see the sync effect below.
  teamspaces: TeamSpace[]
  onCreateRow: (spaceId: string, databasePageId: string, title: string) => Promise<PageNode | null>
  onArchiveRow: (rowId: string) => void
  onRenameRow: (rowId: string, title: string) => void
  onChangeRowIcon: (rowId: string, icon: string) => void
  onOpenPage: (pageId: string) => void
}) {
  const [table, setTable] = useState<DatabaseTable | null>(null)
  const [openRowId, setOpenRowId] = useState<string | null>(null)
  // The row peek slides in and out (usePresence). While it slides out,
  // openRowId is already null, so panelRowId keeps the last row so
  // there's still something to show.
  const panelPresence = usePresence(openRowId !== null, ROW_PANEL_EXIT_MS)
  const [panelRowId, setPanelRowId] = useState<string | null>(null)
  if (openRowId !== null && openRowId !== panelRowId) setPanelRowId(openRowId)
  const [addPropertyAnchor, setAddPropertyAnchor] = useState<HTMLButtonElement | null>(null)
  const addPropertyBtnRef = useRef<HTMLButtonElement>(null)
  // Either a real property id or TITLE_COLUMN_ID -- the title column
  // drags and drops among the properties exactly like one of them.
  const [draggedColumnId, setDraggedColumnId] = useState<string | null>(null)
  const [dropTargetColumnId, setDropTargetColumnId] = useState<string | null>(null)
  // Explicit widths only for columns the user has actually resized --
  // everything else falls back to a default in columnWidth() below, so
  // adding a column never needs to seed an entry here.
  const [columnWidths, setColumnWidths] = useState<Record<string, number>>({})
  const [resizingColumnId, setResizingColumnId] = useState<string | null>(null)

  // Resets the loaded table/open-row state as soon as pageId changes,
  // during render rather than via the effect below -- same reasoning
  // as PageIcon.tsx's identical pattern: the effect is left to do only
  // what actually needs one, the async fetch.
  const [trackedPageId, setTrackedPageId] = useState(pageId)
  if (pageId !== trackedPageId) {
    setTrackedPageId(pageId)
    setTable(null)
    setOpenRowId(null)
  }

  useEffect(() => {
    let cancelled = false
    listRows(pageId)
      .then((t) => {
        if (!cancelled) setTable(t)
      })
      .catch((e) => console.error('failed to load database', e))
    return () => {
      cancelled = true
    }
  }, [pageId])

  // Another user's edit (a cell, a column, a new or renamed row) arrives
  // as a live "data" or "tree" signal: re-fetch the whole table, which
  // also refreshes an open row-detail panel since it renders from this
  // state. The ref guards against a slow response landing after the
  // user has already switched to another database.
  const writes = useLocalWrites()
  const livePageIdRef = useRef(pageId)
  useEffect(() => {
    livePageIdRef.current = pageId
  }, [pageId])
  useLiveRefresh(['data', 'tree'], () => {
    const id = pageId
    const snap = writes.snapshot()
    listRows(id)
      .then((t) => {
        if (livePageIdRef.current === id && writes.canApply(snap)) setTable(t)
      })
      .catch((e) => console.error('failed to refresh database', e))
  })

  // table.rows' own icon/title (from the initial listRows fetch) is
  // otherwise only ever updated by this component's own handlers
  // (handleChangeRowIcon, handleRenameRow, ...) -- which also forward
  // the same edit up through onChangeRowIcon/onRenameRow into
  // App.tsx's changeIcon/renamePage, so an edit made *here* (a table
  // cell or the row detail panel) reaches the sidebar fine. An edit
  // made the other way -- the icon picker or a rename directly in the
  // sidebar -- only ever touches App.tsx's teamspaces state, which
  // nothing here read from, so it never reached this table's row cache.
  // Rather than a useEffect copying teamspaces into table state (a
  // second, redundant source of truth to keep in sync, and exactly the
  // kind of derived-state effect oxlint's set-state-in-effect rule
  // warns about), rowDisplay resolves the icon/title to show for a row
  // at render time, straight from teamspaces (the real source of truth
  // for both -- they're page fields, not row/cell-value fields this
  // component owns) with the fetched row as a fallback for the brief
  // window before teamspaces has loaded.
  function rowDisplay(row: Row): { icon: string; title: string } {
    const node = findPage(teamspaces, row.id)
    return node ? { icon: node.icon, title: node.title } : { icon: row.icon, title: row.title }
  }

  async function handleAddRow() {
    const created = await writes.track(onCreateRow(spaceId, pageId, ''))
    if (!created) return
    const newRow: Row = { id: created.id, title: created.title, icon: created.icon, position: '', values: {} }
    setTable((t) => (t ? { ...t, rows: [...t.rows, newRow] } : t))
    setOpenRowId(newRow.id)
  }

  function handleArchiveRow(rowId: string) {
    if (!window.confirm('Archive this row?')) return
    onArchiveRow(rowId)
    setTable((t) => (t ? { ...t, rows: t.rows.filter((r) => r.id !== rowId) } : t))
    if (openRowId === rowId) setOpenRowId(null)
  }

  function handleRenameRow(rowId: string, title: string) {
    onRenameRow(rowId, title)
    setTable((t) => (t ? { ...t, rows: t.rows.map((r) => (r.id === rowId ? { ...r, title } : r)) } : t))
  }

  function handleChangeRowIcon(rowId: string, icon: string) {
    onChangeRowIcon(rowId, icon)
    setTable((t) => (t ? { ...t, rows: t.rows.map((r) => (r.id === rowId ? { ...r, icon } : r)) } : t))
  }

  function handleCellChange(rowId: string, propertyId: string, value: unknown) {
    setTable((t) =>
      t
        ? {
            ...t,
            rows: t.rows.map((r) =>
              r.id === rowId ? { ...r, values: { ...r.values, [propertyId]: value } } : r,
            ),
          }
        : t,
    )
    writes.track(setRowPropertyApi(rowId, propertyId, value)).catch((e) => console.error('failed to save cell', e))
  }

  function handleOptionsChanged(propertyId: string, options: SelectOption[]) {
    setTable((t) =>
      t ? { ...t, properties: t.properties.map((p) => (p.id === propertyId ? { ...p, options } : p)) } : t,
    )
    writes.track(updatePropertyApi(propertyId, { options })).catch((e) => console.error('failed to save options', e))
  }

  async function handleAddProperty(name: string, type: PropertyType) {
    try {
      const prop = await writes.track(createPropertyApi(pageId, { name, type }))
      setTable((t) => (t ? { ...t, properties: [...t.properties, prop] } : t))
    } catch (e) {
      console.error('failed to add property', e)
    }
  }

  function handleRenameProperty(propertyId: string, name: string) {
    setTable((t) =>
      t ? { ...t, properties: t.properties.map((p) => (p.id === propertyId ? { ...p, name } : p)) } : t,
    )
    writes.track(updatePropertyApi(propertyId, { name })).catch((e) => console.error('failed to rename column', e))
  }

  function handleDeleteProperty(propertyId: string) {
    if (!window.confirm('Delete this column? Every row loses its value in it.')) return
    setTable((t) => (t ? { ...t, properties: t.properties.filter((p) => p.id !== propertyId) } : t))
    writes.track(deletePropertyApi(propertyId)).catch((e) => console.error('failed to delete column', e))
  }

  function handleColumnDragStart(columnId: string) {
    return (e: React.DragEvent) => {
      // Firefox in particular won't start a drag at all unless
      // setData is actually called here -- the real "what's being
      // dragged" bookkeeping is the React state below, this is just to
      // satisfy the browser's own drag-init requirement.
      e.dataTransfer.effectAllowed = 'move'
      e.dataTransfer.setData('text/plain', columnId)
      setDraggedColumnId(columnId)
    }
  }

  function handleColumnDragOver(columnId: string) {
    return (e: React.DragEvent) => {
      if (!draggedColumnId || draggedColumnId === columnId) return
      e.preventDefault() // required for onDrop to ever fire
      setDropTargetColumnId(columnId)
    }
  }

  function handleColumnDrop(columnId: string) {
    return (e: React.DragEvent) => {
      e.preventDefault()
      const draggedId = draggedColumnId
      setDraggedColumnId(null)
      setDropTargetColumnId(null)
      if (!table || !draggedId || draggedId === columnId) return

      const ids = sortedColumnIds(table)
      const from = ids.indexOf(draggedId)
      const targetIdx = ids.indexOf(columnId)
      if (from === -1 || targetIdx === -1) return
      ids.splice(from, 1)
      // Always inserting *before* the target silently no-ops whenever
      // the target is the dragged column's immediate right neighbor --
      // "insert before the column right after you" is exactly where
      // you already are, so dragging one step right did nothing (App.
      // tsx's reorderTabs had the identical bug, fixed the same way).
      // Insert *after* the target instead whenever the drag came from
      // its left, so the immediate-neighbor case actually swaps, and
      // dropping on the last column can reach the last position too.
      let insertAt = ids.indexOf(columnId)
      insertAt = insertAt === -1 ? ids.length : from < targetIdx ? insertAt + 1 : insertAt
      ids.splice(insertAt, 0, draggedId)

      applyColumnOrder(ids)
      writes.track(reorderColumnsApi(pageId, ids)).catch((e) => console.error('failed to save column order', e))
    }
  }

  function handleColumnDragEnd() {
    setDraggedColumnId(null)
    setDropTargetColumnId(null)
  }

  // Optimistic local update matching exactly what the backend will
  // persist: every column (title included) gets a fresh sequential
  // position matching its new slot in ids -- same simple re-stamping
  // ReorderColumns does server-side, see its own comment on the
  // backend for why that's fine (columns are always few).
  function applyColumnOrder(ids: string[]) {
    const positions = new Map(ids.map((id, i) => [id, String(i + 1).padStart(6, '0')]))
    setTable((t) =>
      t
        ? {
            ...t,
            title_column_position: positions.get(TITLE_COLUMN_ID) ?? t.title_column_position,
            properties: t.properties.map((p) => ({ ...p, position: positions.get(p.id) ?? p.position })),
          }
        : t,
    )
  }

  function columnWidth(columnId: string): number {
    return columnWidths[columnId] ?? (columnId === TITLE_COLUMN_ID ? DEFAULT_TITLE_WIDTH : DEFAULT_COLUMN_WIDTH)
  }

  // Same window-level Pointer Events pattern as PageCover.tsx's cover
  // reposition drag: handleMove/handleUp are defined fresh inside the
  // handler itself, closing over this particular drag's own startX/
  // startWidth, so add/removeEventListener always pair up correctly
  // even if a second resize starts before the first one's listeners
  // would otherwise have been cleaned up.
  //
  // The actual setColumnWidths call is throttled to one per animation
  // frame (rAF), not once per pointermove -- pointermove can fire far
  // more often than the screen can paint, and forcing a React re-render
  // for every single one of those events was overwhelming native form
  // controls in the resizing column's cells (a date input's calendar
  // icon visibly lagging/desyncing from its own box mid-drag).
  function handleColumnResizeStart(columnId: string) {
    return (e: React.PointerEvent) => {
      const startWidth = columnWidth(columnId)
      setResizingColumnId(columnId)
      startPointerDrag(e, {
        cursor: 'col-resize',
        onMove: (dx) => setColumnWidths((w) => ({ ...w, [columnId]: Math.max(MIN_COLUMN_WIDTH, startWidth + dx) })),
        onEnd: () => setResizingColumnId(null),
      })
    }
  }

  if (!table) {
    return <p className="hint">Loading…</p>
  }

  const openRow = panelPresence.mounted ? table.rows.find((r) => r.id === panelRowId) : undefined
  const openRowDisplay = openRow ? rowDisplay(openRow) : null
  const columnIds = sortedColumnIds(table)
  const propertiesById = new Map(table.properties.map((p) => [p.id, p]))

  function columnDragProps(columnId: string) {
    return {
      isDragging: draggedColumnId === columnId,
      isDropTarget: dropTargetColumnId === columnId,
      onDragStart: handleColumnDragStart(columnId),
      onDragOver: handleColumnDragOver(columnId),
      onDragLeave: () => setDropTargetColumnId((cur) => (cur === columnId ? null : cur)),
      onDrop: handleColumnDrop(columnId),
      onDragEnd: handleColumnDragEnd,
    }
  }

  function columnResizeProps(columnId: string) {
    return {
      onResizeStart: handleColumnResizeStart(columnId),
      isResizing: resizingColumnId === columnId,
    }
  }

  function cellDragClass(columnId: string): string {
    return [
      'db-td',
      draggedColumnId === columnId && 'db-td-dragging',
      dropTargetColumnId === columnId && 'db-td-drop-target',
    ]
      .filter(Boolean)
      .join(' ')
  }

  return (
    <div className="database-view">
      <div className="database-table-wrap">
        <table className="database-table">
          <colgroup>
            {columnIds.map((id) => (
              <col key={id} style={{ width: columnWidth(id) }} />
            ))}
            <col style={{ width: ADD_COLUMN_WIDTH }} />
          </colgroup>
          <thead>
            <tr>
              {columnIds.map((id) =>
                id === TITLE_COLUMN_ID ? (
                  <TitleColumnHeader key={id} {...columnDragProps(id)} {...columnResizeProps(id)} />
                ) : (
                  <ColumnHeader
                    key={id}
                    property={propertiesById.get(id)!}
                    onRename={(name) => handleRenameProperty(id, name)}
                    onDelete={() => handleDeleteProperty(id)}
                    {...columnDragProps(id)}
                    {...columnResizeProps(id)}
                  />
                ),
              )}
              <th className="db-th db-th-add">
                <button
                  ref={addPropertyBtnRef}
                  type="button"
                  className="db-add-column-btn"
                  onClick={() => setAddPropertyAnchor(addPropertyBtnRef.current)}
                  title="New column"
                  aria-label="New column"
                >
                  +
                </button>
                {addPropertyAnchor && (
                  <AddPropertyMenu
                    anchorEl={addPropertyAnchor}
                    onCreate={handleAddProperty}
                    onClose={() => setAddPropertyAnchor(null)}
                  />
                )}
              </th>
            </tr>
          </thead>
          <tbody>
            {table.rows.map((row) => {
              const display = rowDisplay(row)
              return (
              <tr key={row.id} className="db-tr">
                {columnIds.map((id) =>
                  id === TITLE_COLUMN_ID ? (
                    <td key={id} className={`${cellDragClass(id)} db-td-title`}>
                      <TitleCell
                        icon={display.icon}
                        title={display.title}
                        onOpen={() => setOpenRowId(row.id)}
                        onRename={(title) => handleRenameRow(row.id, title)}
                        onArchive={() => handleArchiveRow(row.id)}
                      />
                    </td>
                  ) : (
                    <td key={id} className={cellDragClass(id)}>
                      <PropertyCell
                        property={propertiesById.get(id)!}
                        value={row.values[id]}
                        onChange={(v) => handleCellChange(row.id, id, v)}
                        onOptionsChanged={(options) => handleOptionsChanged(id, options)}
                      />
                    </td>
                  ),
                )}
                <td className="db-td db-td-add" />
              </tr>
              )
            })}
          </tbody>
        </table>
        <button type="button" className="db-add-row-btn" onClick={handleAddRow}>
          + New row
        </button>
      </div>

      {openRow && openRowDisplay && (
        <RowDetailPanel
          rowId={openRow.id}
          closing={!panelPresence.shown}
          onExited={panelPresence.finish}
          title={openRowDisplay.title}
          icon={openRowDisplay.icon}
          properties={table.properties}
          values={openRow.values}
          onClose={() => setOpenRowId(null)}
          onOpenFullPage={() => {
            setOpenRowId(null)
            onOpenPage(openRow.id)
          }}
          onRenameRow={(title) => handleRenameRow(openRow.id, title)}
          onChangeRowIcon={(icon) => handleChangeRowIcon(openRow.id, icon)}
          onPropertyChange={(propertyId, value) => handleCellChange(openRow.id, propertyId, value)}
          onOptionsChanged={handleOptionsChanged}
        />
      )}
    </div>
  )
}
