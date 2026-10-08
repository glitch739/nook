import { useEffect, useMemo, useRef, useState } from 'react'
import { listCollectionItems, type CollectionItem, type PageKind } from './api'
import { useLiveRefresh } from './liveEvents'
import PageIcon from './PageIcon'
import { DEFAULT_DATABASE_ICON, DEFAULT_ICON } from './pageIcon'
import Popover from './Popover'
import './CollectionView.css'

// A collection page (kind 'collection'): its documents and databases as
// a list (most recently edited first by default) with a filter and a
// sort, so they don't each take a row in the sidebar. Items open as
// pages; "New" adds a document or a database inside the collection.

type Sort = 'edited' | 'title' | 'created'

const SORT_KEY = 'nook-collection-sort'

function readSort(): Sort {
  try {
    const v = localStorage.getItem(SORT_KEY)
    if (v === 'edited' || v === 'title' || v === 'created') return v
  } catch {
    // no storage: default
  }
  return 'edited'
}

const dateFormat = new Intl.DateTimeFormat('en-US', {
  year: 'numeric',
  month: 'long',
  day: 'numeric',
  hour: 'numeric',
  minute: '2-digit',
})

function formatDate(iso: string): string {
  const d = new Date(iso)
  return isNaN(d.getTime()) ? '' : dateFormat.format(d)
}

function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return ''
  return (parts[0][0] + (parts.length > 1 ? parts[parts.length - 1][0] : '')).toUpperCase()
}

export default function CollectionView({
  page,
  onOpenPage,
  onCreatePage,
}: {
  page: { id: string; space_id: string; title: string; icon: string }
  onOpenPage: (id: string) => void
  onCreatePage: (spaceId: string, parentId: string, kind: PageKind) => void
}) {
  const [items, setItems] = useState<CollectionItem[] | null>(null)
  const [filter, setFilter] = useState('')
  const [sort, setSort] = useState<Sort>(readSort)
  const [newAnchor, setNewAnchor] = useState<HTMLButtonElement | null>(null)
  const newBtnRef = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    let cancelled = false
    listCollectionItems(page.id)
      .then((list) => {
        if (!cancelled) setItems(list)
      })
      .catch((e) => console.error('failed to load collection', e))
    return () => {
      cancelled = true
    }
  }, [page.id])

  // Pages added, moved, renamed or archived anywhere arrive as a live
  // "tree" signal: re-fetch the list.
  useLiveRefresh(['tree'], () => {
    listCollectionItems(page.id)
      .then(setItems)
      .catch((e) => console.error('failed to refresh collection', e))
  })

  useEffect(() => {
    try {
      localStorage.setItem(SORT_KEY, sort)
    } catch {
      // not remembered this time
    }
  }, [sort])

  const shown = useMemo(() => {
    if (!items) return []
    const q = filter.trim().toLowerCase()
    const list = q ? items.filter((i) => (i.title || 'Untitled').toLowerCase().includes(q)) : [...items]
    if (sort === 'title') list.sort((a, b) => (a.title || 'Untitled').localeCompare(b.title || 'Untitled'))
    else if (sort === 'created') list.sort((a, b) => b.created_at.localeCompare(a.created_at))
    else list.sort((a, b) => b.updated_at.localeCompare(a.updated_at))
    return list
  }, [items, filter, sort])

  function create(kind: PageKind) {
    setNewAnchor(null)
    onCreatePage(page.space_id, page.id, kind)
  }

  return (
    <div className="collection-view">
      <h1 className="collection-title">
        <PageIcon icon={page.icon} className="collection-title-icon" />
        <span>{page.title || 'Untitled'}</span>
      </h1>

      <div className="collection-toolbar">
        <div className="collection-filter">
          <svg viewBox="0 0 24 24" width="15" height="15" aria-hidden="true">
            <circle cx="11" cy="11" r="7" fill="none" stroke="currentColor" strokeWidth="2" />
            <path d="m20 20-3.5-3.5" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
          </svg>
          <input
            type="search"
            placeholder="Filter by title…"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            aria-label="Filter by title"
          />
        </div>
        <select
          className="collection-sort"
          value={sort}
          onChange={(e) => setSort(e.target.value as Sort)}
          aria-label="Sort"
        >
          <option value="edited">Recently edited</option>
          <option value="title">Title A–Z</option>
          <option value="created">Recently created</option>
        </select>
        <button
          ref={newBtnRef}
          type="button"
          className="collection-new"
          onClick={() => setNewAnchor(newAnchor ? null : newBtnRef.current)}
        >
          New
        </button>
        {newAnchor && (
          <Popover anchorEl={newAnchor} width={190} maxHeight={96} onClose={() => setNewAnchor(null)} className="create-menu">
            <button type="button" className="create-menu-item" onClick={() => create('document')}>
              <PageIcon icon={DEFAULT_ICON} className="page-icon" /> New page
            </button>
            <button type="button" className="create-menu-item" onClick={() => create('database')}>
              <PageIcon icon={DEFAULT_DATABASE_ICON} className="page-icon" /> New database
            </button>
          </Popover>
        )}
      </div>

      {items === null ? (
        <p className="hint">Loading…</p>
      ) : shown.length === 0 ? (
        <p className="collection-empty">
          {items.length === 0
            ? 'Nothing here yet. Add a page or a database with "New", or drag pages onto this collection in the sidebar.'
            : 'No titles match the filter.'}
        </p>
      ) : (
        <ul className="collection-list">
          {shown.map((item) => (
            <li key={item.id}>
              <button type="button" className="collection-item" onClick={() => onOpenPage(item.id)}>
                <PageIcon icon={item.icon} className="collection-item-icon" />
                <span className="collection-item-title">{item.title || 'Untitled'}</span>
                <span className="collection-item-date">
                  {formatDate(sort === 'created' ? item.created_at : item.updated_at)}
                </span>
                <span className="collection-item-who" title={item.updated_by_name || undefined}>
                  {initials(item.updated_by_name)}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
