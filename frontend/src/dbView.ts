// Sorting and filtering a database's rows in the browser (DatabaseView).
// Pure functions over the loaded table, so they're easy to test; the
// chosen view itself is remembered per database in localStorage.
import { TITLE_COLUMN_ID, type Property, type Row } from './api'

export interface DbView {
  sortBy: string | null // a property id, TITLE_COLUMN_ID, or none
  desc: boolean
  filter: string
}

export const EMPTY_VIEW: DbView = { sortBy: null, desc: false, filter: '' }

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
