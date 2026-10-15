// Sorting and filtering a database's rows in the browser (DatabaseView).
// Pure functions over the loaded table, so they're easy to test; the
// chosen view itself is remembered per database in localStorage.
import { TITLE_COLUMN_ID, type Property, type Row } from './api'

export interface DbView {
  sortBy: string | null // a property id, TITLE_COLUMN_ID, or none
  desc: boolean
  filter: string
  /** Board (kanban) view grouped by this select or person property; null: table. */
  board: string | null
}

export const EMPTY_VIEW: DbView = { sortBy: null, desc: false, filter: '', board: null }

// What a cell shows, as text: used for filtering and for text sorting.
export function cellText(property: Property, value: unknown): string {
  if (value === null || value === undefined) return ''
  switch (property.type) {
    case 'select': {
      const id = (value as { option_id?: string }).option_id
      return property.options.find((o) => o.id === id)?.label ?? ''
    }
    case 'person': {
      if (typeof value === 'string') return value // a pre-2026-10-03 name
      const id = (value as { user_id?: string }).user_id
      return property.options.find((o) => o.id === id)?.label ?? ''
    }
    case 'checkbox':
      return value === true ? 'yes' : ''
    default:
      return String(value)
  }
}

// A comparable key, or null for an empty cell (always sorted last).
function sortKey(property: Property | undefined, row: Row, title: string): string | number | null {
  if (!property) return title.trim() ? title.toLowerCase() : null
  const value = row.values[property.id]
  switch (property.type) {
    case 'number':
      return typeof value === 'number' ? value : null
    case 'checkbox':
      return value === true ? 1 : 0
    case 'select': {
      // Status-style columns sort in the order their options are listed.
      const id = (value as { option_id?: string } | null)?.option_id
      const i = property.options.findIndex((o) => o.id === id)
      return i === -1 ? null : i
    }
    default: {
      const text = cellText(property, value).trim()
      return text ? text.toLowerCase() : null
    }
  }
}

export function applyView(
  rows: Row[],
  properties: Property[],
  view: DbView,
  titleOf: (row: Row) => string,
): Row[] {
  const q = view.filter.trim().toLowerCase()
  let out = q
    ? rows.filter((r) =>
        [titleOf(r), ...properties.map((p) => cellText(p, r.values[p.id]))].some((t) => t.toLowerCase().includes(q)),
      )
    : rows
  if (view.sortBy) {
    const prop = view.sortBy === TITLE_COLUMN_ID ? undefined : properties.find((p) => p.id === view.sortBy)
    if (view.sortBy === TITLE_COLUMN_ID || prop) {
      const keyed = out.map((r) => ({ r, k: sortKey(prop, r, titleOf(r)) }))
      keyed.sort((a, b) => {
        if (a.k === null || b.k === null) return a.k === b.k ? 0 : a.k === null ? 1 : -1
        const c = typeof a.k === 'number' && typeof b.k === 'number' ? a.k - b.k : String(a.k).localeCompare(String(b.k))
        return view.desc ? -c : c
      })
      out = keyed.map((x) => x.r)
    }
  }
  return out
}

function readJSON<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key)
    return raw ? { ...fallback, ...JSON.parse(raw) } : fallback
  } catch {
    return fallback
  }
}

function writeJSON(key: string, value: unknown) {
  try {
    localStorage.setItem(key, JSON.stringify(value))
  } catch {
    // Not remembered this time; the view still works.
  }
}

export const readDbView = (pageId: string) => readJSON<DbView>(`nook-db-view-${pageId}`, EMPTY_VIEW)
export const saveDbView = (pageId: string, v: DbView) => writeJSON(`nook-db-view-${pageId}`, v)
export const readColumnWidths = (pageId: string) => readJSON<Record<string, number>>(`nook-db-widths-${pageId}`, {})
export const saveColumnWidths = (pageId: string, w: Record<string, number>) => writeJSON(`nook-db-widths-${pageId}`, w)

/** One board column: an option or a person (or "no value", key null) and its rows. */
export interface BoardColumn {
  key: string | null
  label: string
  color: string
  rows: Row[]
}

/** The board column a row belongs in: its option id or person id, or null. */
export function boardKey(row: Row, property: Property): string | null {
  const v = row.values[property.id] as { option_id?: string; user_id?: string } | null | undefined
  return (property.type === 'person' ? v?.user_id : v?.option_id) ?? null
}

/** The cell value that puts a row in column key. */
export function boardValue(property: Property, key: string | null): unknown {
  if (key === null) return null
  return property.type === 'person' ? { user_id: key } : { option_id: key }
}

// Rows grouped for the board view by a select property (one column per
// option, in order) or a person property (one per person picked in it
// so far; its options keep their names and colors), with a "No <name>"
// column first for rows without a (still known) value. Row order inside
// a column is the order given (filtered and sorted by applyView).
//
// order, when given (the board's own manual order, DatabaseTable's
// board_order), sorts each column's cards; rows not in it keep their
// given order after the listed ones.
//
// columnOrder, when given (the board's own column order for this
// property, "" for the "no value" column), puts the listed columns first
// in that order; the rest follow as above.
export function boardColumns(rows: Row[], property: Property, order?: string[], columnOrder?: string[]): BoardColumn[] {
  const columns: BoardColumn[] = [
    { key: null, label: `No ${property.name}`, color: '', rows: [] },
    ...property.options.map((o) => ({ key: o.id, label: o.label, color: o.color, rows: [] as Row[] })),
  ]
  const byKey = new Map(columns.map((c) => [c.key, c]))
  for (const r of rows) {
    ;(byKey.get(boardKey(r, property)) ?? columns[0]).rows.push(r)
  }
  if (order?.length) {
    const rank = new Map(order.map((id, i) => [id, i]))
    const at = (r: Row) => rank.get(r.id) ?? Infinity
    // Array.prototype.sort is stable: unlisted rows keep their order.
    for (const c of columns) c.rows.sort((a, b) => at(a) - at(b))
  }
  if (columnOrder?.length) {
    const rank = new Map(columnOrder.map((k, i) => [k, i]))
    const at = (c: BoardColumn) => rank.get(c.key ?? '') ?? Infinity
    columns.sort((a, b) => at(a) - at(b))
  }
  return columns
}

/**
 * The board order after moving dragged right before beforeId (or to the
 * end, which is also the end of its column). The result lists every row,
 * the old order first, then rows it didn't have, in table order.
 */
export function moveInBoardOrder(order: string[], rowIds: string[], dragged: string, beforeId: string | null): string[] {
  const live = new Set(rowIds)
  const all = [...new Set([...order.filter((id) => live.has(id)), ...rowIds])]
  if (beforeId === dragged) return all // dropped on itself
  const full = all.filter((id) => id !== dragged)
  const at = beforeId === null ? -1 : full.indexOf(beforeId)
  full.splice(at === -1 ? full.length : at, 0, dragged)
  return full
}
