// The sidebar's search box (ADR 0007). Two sources, one list:
//
//  - pages: full-text search on the server (GET /api/search), which
//    matches titles, document text and database cells, accent- and
//    case-insensitively, as prefixes ("hazk" finds "Házkörüli") and,
//    ranked after those, inside words ("db01" finds "srvdb01"), plus
//    the names and text of attached files, OCR included (ADR 0009). A
//    matching file is its own row under its page: opening it opens the
//    page and the file's preview (a PDF at the matching page).
//  - groups and spaces: filtered right here from what the sidebar has
//    already loaded, with the same folding rules -- there are only ever
//    a handful of them, so a round trip would buy nothing.
//
// While the query is non-empty Sidebar hides its tree and this list
// takes its place. Arrow keys move the selection, Enter opens, Escape
// clears; Cmd/Ctrl+K focuses the box from anywhere.

import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { attachmentPreviewUrl, searchPages, type SearchFileHit, type SearchResult } from './api'
import PageIcon from './PageIcon'
import { findPageAncestors, type SidebarGroup, type TeamSpace } from './sidebarData'

const DEBOUNCE_MS = 150
const MARK_START = '\u0002'
const MARK_END = '\u0003'

const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform)

/** Splits a server string carrying \u0002...\u0003 markers into plain
 * text and <mark> pieces -- React escapes both, so no markup from the
 * server ever reaches the DOM. */
function splitMarked(text: string): ReactNode[] {
  const out: ReactNode[] = []
  let rest = text
  let key = 0
  while (rest) {
    const start = rest.indexOf(MARK_START)
    if (start === -1) {
      out.push(rest)
      break
    }
    if (start > 0) out.push(rest.slice(0, start))
    const end = rest.indexOf(MARK_END, start + 1)
    const stop = end === -1 ? rest.length : end
    out.push(<mark key={key++}>{rest.slice(start + 1, stop)}</mark>)
    rest = end === -1 ? '' : rest.slice(end + 1)
  }
  return out
}

/** Lowercases and strips diacritics one character at a time, keeping a
 * map from each folded index back to the original string so matches
 * can be highlighted in the original text. */
function fold(text: string): { folded: string; origin: number[] } {
  let folded = ''
  const origin: number[] = []
  for (let i = 0; i < text.length; i++) {
    const f = text[i].normalize('NFD').replace(/\p{M}/gu, '').toLowerCase()
    for (let j = 0; j < f.length; j++) origin.push(i)
    folded += f
  }
  return { folded, origin }
}

function queryTerms(query: string): string[] {
  return fold(query).folded.split(/[^\p{L}\p{N}]+/u).filter(Boolean)
}

/** Highlights every term in name; null when some term doesn't occur. */
function matchName(name: string, terms: string[]): ReactNode[] | null {
  if (terms.length === 0) return null
  const { folded, origin } = fold(name)
  const hit = new Array<boolean>(name.length).fill(false)
  for (const term of terms) {
    let at = folded.indexOf(term)
    if (at === -1) return null
    while (at !== -1) {
      for (let k = at; k < at + term.length; k++) hit[origin[k]] = true
      at = folded.indexOf(term, at + term.length)
    }
  }
  const out: ReactNode[] = []
  let i = 0
  while (i < name.length) {
    let j = i
    while (j < name.length && hit[j] === hit[i]) j++
    out.push(hit[i] ? <mark key={i}>{name.slice(i, j)}</mark> : name.slice(i, j))
    i = j
  }
  return out
}

type Item =
  | { kind: 'page'; key: string; result: SearchResult }
  // A matching attachment, a row of its own under its page (ADR 0009).
  | { kind: 'file'; key: string; result: SearchResult; hit: SearchFileHit }
  | { kind: 'group'; key: string; group: SidebarGroup; label: ReactNode[] }
  | { kind: 'space'; key: string; space: TeamSpace; label: ReactNode[] }

interface SidebarSearchProps {
  query: string
  onQueryChange: (query: string) => void
  teamspaces: TeamSpace[]
  groups: SidebarGroup[]
  activeId: string | null
  onSelectPage: (id: string) => void
  // Clears the search and scrolls the tree to that group/space.
  onRevealSection: (kind: 'group' | 'space', id: string) => void
}

export default function SidebarSearch({
  query,
  onQueryChange,
  teamspaces,
  groups,
  activeId,
  onSelectPage,
  onRevealSection,
}: SidebarSearchProps) {
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const [pages, setPages] = useState<SearchResult[]>([])
  // The query `pages` belongs to -- while it differs from the current
  // one, a request is in flight and the old list is shown dimmed.
  const [pagesFor, setPagesFor] = useState('')
  const [failed, setFailed] = useState(false)
  // Keyboard selection, tied to the list it was made in: a new result
  // set starts back at the top without an extra reset render.
  const [selection, setSelection] = useState<{ items: unknown; index: number }>({ items: null, index: 0 })

  const trimmed = query.trim()
  const terms = useMemo(() => queryTerms(trimmed), [trimmed])

  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      // Cmd/Ctrl+K is the command palette now; full-text search lives on
      // Cmd/Ctrl+Shift+F (and the palette's "Search in page contents").
      if ((e.metaKey || e.ctrlKey) && !e.altKey && e.shiftKey && e.key.toLowerCase() === 'f') {
        e.preventDefault()
        inputRef.current?.focus()
        inputRef.current?.select()
      }
    }
    function focusSearch() {
      inputRef.current?.focus()
      inputRef.current?.select()
    }
    document.addEventListener('keydown', handleKey)
    window.addEventListener('nook:focus-search', focusSearch)
    return () => {
      document.removeEventListener('keydown', handleKey)
      window.removeEventListener('nook:focus-search', focusSearch)
    }
  }, [])

  useEffect(() => {
    if (!trimmed) return
    const controller = new AbortController()
    const timer = window.setTimeout(() => {
      searchPages(trimmed, controller.signal)
        .then((results) => {
          setPages(results)
          setPagesFor(trimmed)
          setFailed(false)
        })
        .catch((err: unknown) => {
          if (controller.signal.aborted) return
          console.error('search failed', err)
          setPages([])
          setPagesFor(trimmed)
          setFailed(true)
        })
    }, DEBOUNCE_MS)
    return () => {
      window.clearTimeout(timer)
      controller.abort()
    }
  }, [trimmed])

  const sections = useMemo(() => {
    const groupItems: Item[] = []
    for (const group of groups) {
      const label = matchName(group.name, terms)
      if (label) groupItems.push({ kind: 'group', key: `g:${group.id}`, group, label })
    }
    const spaceItems: Item[] = []
    for (const space of teamspaces) {
      const label = matchName(space.name, terms)
      if (label) spaceItems.push({ kind: 'space', key: `s:${space.id}`, space, label })
    }
    const pageItems: Item[] = pages.flatMap((result) => [
      { kind: 'page' as const, key: `p:${result.page_id}`, result },
      ...(result.files ?? []).map((hit) => ({ kind: 'file' as const, key: `f:${hit.file_id}`, result, hit })),
    ])
    return { pageItems, placeItems: [...groupItems, ...spaceItems] }
  }, [groups, teamspaces, pages, terms])

  const items = useMemo(() => [...sections.pageItems, ...sections.placeItems], [sections])
  const selected = selection.items === items ? selection.index : 0
  const setSelected = (update: (i: number) => number) => setSelection({ items, index: update(selected) })

  useEffect(() => {
    listRef.current
      ?.querySelector(`[data-search-index="${selected}"]`)
      ?.scrollIntoView({ block: 'nearest' })
  }, [selected])

  function activate(item: Item) {
    if (item.kind === 'page') onSelectPage(item.result.page_id)
    else if (item.kind === 'file') {
      // The page, and the file itself where it can be read (a PDF at the
      // matching page) in a tab of its own.
      onSelectPage(item.result.page_id)
      const { url, filename, previewable, page_no } = item.hit
      const preview = attachmentPreviewUrl(url, filename, previewable, page_no)
      if (preview) window.open(preview, '_blank', 'noopener')
    } else if (item.kind === 'group') onRevealSection('group', item.group.id)
    else onRevealSection('space', item.space.id)
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Escape') {
      e.preventDefault()
      onQueryChange('')
      inputRef.current?.blur()
    } else if (e.key === 'ArrowDown' && items.length > 0) {
      e.preventDefault()
      setSelected((i) => (i + 1) % items.length)
    } else if (e.key === 'ArrowUp' && items.length > 0) {
      e.preventDefault()
      setSelected((i) => (i - 1 + items.length) % items.length)
    } else if (e.key === 'Enter' && items[selected]) {
      e.preventDefault()
      activate(items[selected])
    }
  }

  const groupName = (id: string) => groups.find((g) => g.id === id)?.name
  const pending = trimmed !== '' && pagesFor !== trimmed

  function renderItem(item: Item, index: number) {
    const className = [
      'search-result',
      index === selected && 'search-result-selected',
      item.kind === 'page' && item.result.page_id === activeId && 'active',
    ]
      .filter(Boolean)
      .join(' ')
    const common = {
      className,
      'data-search-index': index,
      onMouseMove: () => index !== selected && setSelected(() => index),
      // mousedown, not click: keeps focus in the input, so arrow keys
      // keep working right after a click.
      onMouseDown: (e: React.MouseEvent) => e.preventDefault(),
      onClick: () => activate(item),
      role: 'option',
      'aria-selected': index === selected,
    }

    if (item.kind === 'page') {
      const r = item.result
      const location = findPageAncestors(teamspaces, r.page_id)
      const crumbs = location
        ? [groupName(location.space.groupId), location.space.name, ...location.ancestors.map((a) => a.title || 'Untitled')]
        : []
      return (
        <div key={item.key} {...common}>
          <PageIcon icon={r.icon} className="page-icon search-result-icon" />
          <div className="search-result-body">
            <div className="search-result-title">{r.title ? splitMarked(r.title_marked) : 'Untitled'}</div>
            {crumbs.length > 0 && (
              <div className="search-result-path">{crumbs.filter(Boolean).join(' › ')}</div>
            )}
            {r.snippet && <div className="search-result-snippet">{splitMarked(r.snippet)}</div>}
          </div>
        </div>
      )
    }

    if (item.kind === 'file') {
      const h = item.hit
      const where = [h.part.replace(/^\//, ''), h.page_no > 0 && `p. ${h.page_no}`, h.used_ocr && 'OCR'].filter(Boolean).join(' · ')
      return (
        <div key={item.key} {...common} className={`${className} search-result-file`} title="Open the page and the file">
          <span className="search-result-icon search-result-file-icon" aria-hidden="true">
            📎
          </span>
          <div className="search-result-body">
            <div className="search-result-title">{splitMarked(h.name_marked)}</div>
            {where && <div className="search-result-path">{where}</div>}
            {h.snippet && <div className="search-result-snippet">{splitMarked(h.snippet)}</div>}
          </div>
        </div>
      )
    }

    const icon = item.kind === 'group' ? item.group.icon : item.space.icon
    const hint = item.kind === 'group' ? 'Group' : `Space · ${groupName(item.space.groupId) ?? ''}`
    return (
      <div key={item.key} {...common}>
        <PageIcon icon={icon} className="page-icon search-result-icon" />
        <div className="search-result-body">
          <div className="search-result-title">{item.label}</div>
          <div className="search-result-path">{hint}</div>
        </div>
      </div>
    )
  }

  return (
    <>
      <div className="sidebar-search-wrap">
        <svg className="sidebar-search-glyph" viewBox="0 0 24 24" aria-hidden="true">
          <circle cx="11" cy="11" r="7" />
          <path d="m20 20-3.5-3.5" />
        </svg>
        <input
          ref={inputRef}
          className="sidebar-search"
          placeholder="Search…"
          value={query}
          onChange={(e) => onQueryChange(e.target.value)}
          onKeyDown={handleKeyDown}
          role="combobox"
          aria-expanded={trimmed !== ''}
          aria-controls="sidebar-search-results"
          aria-label="Search pages, spaces, and groups"
          spellCheck={false}
        />
        {query ? (
          <button
            className="sidebar-search-clear"
            onClick={() => {
              onQueryChange('')
              inputRef.current?.focus()
            }}
            aria-label="Clear search"
            title="Clear search (Esc)"
          >
            ×
          </button>
        ) : (
          <kbd className="sidebar-search-kbd">{isMac ? '⇧⌘F' : 'Ctrl ⇧ F'}</kbd>
        )}
      </div>

      {trimmed !== '' && (
        <div
          id="sidebar-search-results"
          ref={listRef}
          className={pending ? 'search-results search-results-pending' : 'search-results'}
          role="listbox"
        >
          {sections.pageItems.length > 0 && (
            <>
              <div className="sidebar-section-label">
                <span>Pages</span>
              </div>
              {sections.pageItems.map((item, i) => renderItem(item, i))}
            </>
          )}
          {sections.placeItems.length > 0 && (
            <>
              <div className="sidebar-section-label">
                <span>Groups and spaces</span>
              </div>
              {sections.placeItems.map((item, i) => renderItem(item, sections.pageItems.length + i))}
            </>
          )}
          {items.length === 0 && !pending && (
            <div className="search-empty">
              {failed ? 'Search failed. Try again.' : `No results for "${trimmed}"`}
            </div>
          )}
          {items.length === 0 && pending && <div className="search-empty">Searching…</div>}
        </div>
      )}
    </>
  )
}
