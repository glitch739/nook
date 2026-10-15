import { Fragment, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import type { Property, Row } from './api'
import { boardColumns, boardKey, type BoardColumn } from './dbView'
import PageIcon from './PageIcon'
import { SelectPill } from './PropertyCell'

// The board (kanban) view of a database (DatabaseView.tsx): one column
// per option of a select property or per person of a person property,
// a card per row. A card is moved by its grip: into another column (that
// sets the property) and, in manual order, between cards (the board's
// own order, kept apart from the table's row order). A click elsewhere
// on the card opens the row panel.
//
// Columns are moved the same way, by a grip in their header: the
// board's own column order for the property it groups by (layout only;
// the property's options and the rows' values are untouched).
//
// The drag follows pointer events, not HTML5 drag-and-drop: the native
// drag of the cards never started for the user in Safari, Arc or the
// Dock web app (the tab bar went the same way, see TabBar.tsx). Pointer
// events also cover touch, so the grip works under a finger as well.

type Target = { key: string | null; before: string | null }

function GripIcon() {
  return (
    <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="9" cy="5" r="1" />
      <circle cx="9" cy="12" r="1" />
      <circle cx="9" cy="19" r="1" />
      <circle cx="15" cy="5" r="1" />
      <circle cx="15" cy="12" r="1" />
      <circle cx="15" cy="19" r="1" />
    </svg>
  )
}

export default function DatabaseBoard({
  rows,
  property,
  order,
  display,
  readOnly,
  onOpen,
  onDrop,
  onAdd,
  columnOrder,
  onMoveColumns,
}: {
  rows: Row[]
  property: Property
  /** The board's manual card order; undefined while the view is sorted,
   * and cards then only move between columns. */
  order: string[] | undefined
  display: (row: Row) => { icon: string; title: string }
  readOnly: boolean
  onOpen: (rowId: string) => void
  /** key: the column's option or person id (null: no value). beforeId:
   * the card it was dropped above (null: the end), undefined while sorted. */
  onDrop: (rowId: string, key: string | null, beforeId: string | null | undefined) => void
  onAdd: (key: string | null) => void
  /** The saved column order for this property ("" for "no value"). */
  columnOrder: string[] | undefined
  /** A column was dropped: every column's key in the new order. */
  onMoveColumns: (keys: string[]) => void
}) {
  // The card being dragged: the pointer, where on the card it was
  // grabbed, and the card's width (for its copy under the pointer).
  const [drag, setDrag] = useState<{ id: string; x: number; y: number; dx: number; dy: number; width: number } | null>(
    null,
  )
  const [target, setTarget] = useState<Target | null>(null)
  const stopRef = useRef<(() => void) | null>(null)
  useEffect(() => () => stopRef.current?.(), [])

  // A column being dragged by its header grip, and the slot it would go
  // to: an index into the other columns (their count: the end).
  const [colDrag, setColDrag] = useState<{ key: string | null; x: number; y: number; dx: number; dy: number; width: number } | null>(
    null,
  )
  const [colSlot, setColSlot] = useState<number | null>(null)

  const columns = boardColumns(rows, property, order, columnOrder)
  const manual = order !== undefined

  // The column and slot under a point: above the card there (its upper
  // half) or the next one, or the column's end.
  function targetAt(x: number, y: number, draggedId: string): Target | null {
    const el = document.elementFromPoint(x, y)
    const colEl = el?.closest<HTMLElement>('[data-board-col]')
    if (!colEl) return null
    const key = colEl.dataset.boardCol || null
    const col = columns.find((c) => c.key === key)
    if (!col) return null
    const cards = col.rows.filter((r) => r.id !== draggedId)
    const cardEl = el?.closest<HTMLElement>('[data-row-id]')
    const i = cardEl ? cards.findIndex((r) => r.id === cardEl.dataset.rowId) : -1
    if (!cardEl || i === -1) return { key, before: null }
    const r = cardEl.getBoundingClientRect()
    return { key, before: y < r.top + r.height / 2 ? cards[i].id : (cards[i + 1]?.id ?? null) }
  }

  // Follows one pointer from a press on a grip until it is let go: move
  // gets every position, drop the release point, end always runs last.
  // The tab bar's guards (TabBar.tsx) apply: a move with no button down
  // (released outside the window, an app switch) or a lost window focus
  // cancels instead of leaving the item stuck to the pointer, and moves
  // outside the window are ignored (Safari full screen on a second
  // display sends a stray one right after the press). Escape cancels.
  function followPointer(
    e: React.PointerEvent,
    h: { move: (x: number, y: number) => void; drop: (x: number, y: number) => void; end: () => void },
  ) {
    e.preventDefault()
    e.stopPropagation()
    const pointerId = e.pointerId
    const mouse = e.pointerType === 'mouse'
    const move = (ev: PointerEvent) => {
      if (ev.pointerId !== pointerId) return
      if (mouse && (ev.buttons & 1) === 0) return stop()
      if (ev.clientX < 0 || ev.clientY < 0 || ev.clientX > window.innerWidth || ev.clientY > window.innerHeight) return
      h.move(ev.clientX, ev.clientY)
    }
    const up = (ev: PointerEvent) => {
      if (ev.pointerId !== pointerId) return
      stop()
      if (ev.type === 'pointerup') h.drop(ev.clientX, ev.clientY)
    }
    const key = (ev: KeyboardEvent) => {
      if (ev.key === 'Escape') stop()
    }
    const stop = () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
      window.removeEventListener('pointercancel', up)
      window.removeEventListener('keydown', key)
      window.removeEventListener('blur', stop)
      document.body.classList.remove('board-dragging')
      stopRef.current = null
      h.end()
    }
    stopRef.current?.()
    stopRef.current = stop
    document.body.classList.add('board-dragging')
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up)
    window.addEventListener('pointercancel', up)
    window.addEventListener('keydown', key)
    window.addEventListener('blur', stop)
  }

  function startDrag(e: React.PointerEvent, row: Row) {
    if (readOnly || (e.pointerType === 'mouse' && e.button !== 0)) return
    const card = (e.currentTarget as HTMLElement).closest<HTMLElement>('[data-row-id]')
    if (!card) return
    const rect = card.getBoundingClientRect()
    let current: Target | null = null
    setDrag({ id: row.id, x: e.clientX, y: e.clientY, dx: e.clientX - rect.left, dy: e.clientY - rect.top, width: rect.width })
    followPointer(e, {
      move: (x, y) => {
        setDrag((d) => (d ? { ...d, x, y } : d))
        current = targetAt(x, y, row.id)
        setTarget((t) => (t?.key === current?.key && t?.before === current?.before ? t : current))
      },
      drop: (x, y) => {
        const t = targetAt(x, y, row.id) ?? current
        if (!t) return
        const moved = boardKey(row, property) !== t.key
        if (moved || manual) onDrop(row.id, t.key, manual ? t.before : undefined)
      },
      end: () => {
        setDrag(null)
        setTarget(null)
      },
    })
  }

  function startColumnDrag(e: React.PointerEvent, col: BoardColumn) {
    if (readOnly || (e.pointerType === 'mouse' && e.button !== 0)) return
    const section = (e.currentTarget as HTMLElement).closest<HTMLElement>('[data-board-col]')
    if (!section) return
    const rect = section.getBoundingClientRect()
    const others = columns.filter((c) => c.key !== col.key)
    // The other columns' centres, measured once: they don't move while
    // the marker is shown.
    const centres = others.map((c) => {
      const el = section.parentElement?.querySelector<HTMLElement>(`[data-board-col="${CSS.escape(c.key ?? '')}"]`)
      const r = el?.getBoundingClientRect()
      return r ? r.left + r.width / 2 : 0
    })
    let slot = others.length
    setColDrag({ key: col.key, x: e.clientX, y: e.clientY, dx: e.clientX - rect.left, dy: e.clientY - rect.top, width: rect.width })
    followPointer(e, {
      move: (x, y) => {
        setColDrag((d) => (d ? { ...d, x, y } : d))
        slot = centres.filter((c) => c < x).length
        setColSlot(slot)
      },
      drop: () => {
        const keys = others.map((c) => c.key ?? '')
        keys.splice(slot, 0, col.key ?? '')
        if (keys.join('\n') !== columns.map((c) => c.key ?? '').join('\n')) onMoveColumns(keys)
      },
      end: () => {
        setColDrag(null)
        setColSlot(null)
      },
    })
  }

  const dragged = drag ? rows.find((r) => r.id === drag.id) : undefined
  const draggedCol = colDrag ? columns.find((c) => c.key === colDrag.key) : undefined
  // Where the column marker goes: before this column's key, or the end.
  const others = colDrag ? columns.filter((c) => c.key !== colDrag.key) : []
  const markerBefore = colDrag && colSlot !== null ? (colSlot < others.length ? others[colSlot].key : undefined) : null

  const head = (col: BoardColumn) =>
    col.key === null ? (
      <span className="db-board-none">{col.label}</span>
    ) : (
      <SelectPill option={{ id: col.key, label: col.label, color: col.color }} />
    )

  return (
    <div className="db-board">
      {columns.map((col: BoardColumn) => {
        const isOver = drag !== null && target?.key === col.key
        const colMarker = colDrag && markerBefore === col.key && <div className="db-board-col-marker" aria-hidden="true" />
        const marker = (before: string | null) =>
          manual && isOver && target?.before === before && <div className="db-board-marker" aria-hidden="true" />
        return (
          <Fragment key={col.key ?? 'none'}>
          {colMarker}
          <section
            data-board-col={col.key ?? ''}
            className={['db-board-col', isOver && 'db-board-col-over', colDrag?.key === col.key && 'db-board-col-dragging']
              .filter(Boolean)
              .join(' ')}
          >
            <header className="db-board-col-head">
              {!readOnly && (
                <span className="db-board-grip db-board-col-grip" title="Drag to move the column" onPointerDown={(e) => startColumnDrag(e, col)}>
                  <GripIcon />
                </span>
              )}
              {head(col)}
              <span className="hint">{col.rows.length}</span>
            </header>
            <div className="db-board-cards">
              {col.rows.map((row) => {
                const d = display(row)
                return (
                  <div key={row.id} className="db-board-slot">
                    {marker(row.id)}
                    <div
                      role="button"
                      tabIndex={0}
                      data-row-id={row.id}
                      className={drag?.id === row.id ? 'db-board-card db-board-card-dragging' : 'db-board-card'}
                      onClick={() => onOpen(row.id)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter' || e.key === ' ') {
                          e.preventDefault()
                          onOpen(row.id)
                        }
                      }}
                    >
                      {!readOnly && (
                        <span
                          className="db-board-grip"
                          title="Drag to move"
                          onPointerDown={(e) => startDrag(e, row)}
                          onClick={(e) => e.stopPropagation()}
                        >
                          <GripIcon />
                        </span>
                      )}
                      <PageIcon icon={d.icon} className="page-icon" />
                      <span className="db-board-card-title">{d.title || 'Untitled'}</span>
                    </div>
                  </div>
                )
              })}
              {marker(null)}
              {!readOnly && (
                <button type="button" className="db-board-add" onClick={() => onAdd(col.key)}>
                  + New
                </button>
              )}
            </div>
          </section>
          </Fragment>
        )
      })}
      {colDrag && markerBefore === undefined && <div className="db-board-col-marker" aria-hidden="true" />}
      {colDrag &&
        draggedCol &&
        createPortal(
          <div
            className="db-board-col db-board-col-ghost"
            style={{ left: colDrag.x - colDrag.dx, top: colDrag.y - colDrag.dy, width: colDrag.width }}
            aria-hidden="true"
          >
            <header className="db-board-col-head">
              <span className="db-board-grip db-board-col-grip">
                <GripIcon />
              </span>
              {head(draggedCol)}
              <span className="hint">{draggedCol.rows.length}</span>
            </header>
          </div>,
          document.body,
        )}
      {drag &&
        dragged &&
        createPortal(
          <div
            className="db-board-card db-board-ghost"
            style={{ left: drag.x - drag.dx, top: drag.y - drag.dy, width: drag.width }}
            aria-hidden="true"
          >
            <span className="db-board-grip">
              <GripIcon />
            </span>
            <PageIcon icon={display(dragged).icon} className="page-icon" />
            <span className="db-board-card-title">{display(dragged).title || 'Untitled'}</span>
          </div>,
          document.body,
        )}
    </div>
  )
}
