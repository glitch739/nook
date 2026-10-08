import { useEffect, useState } from 'react'
import {
  deleteArchivedGroup,
  deleteArchivedPage,
  deleteArchivedSpace,
  listArchive,
  restoreGroup,
  restorePage,
  restoreSpace,
  type ArchiveList,
  type ArchivedPage,
} from './api'
import PageIcon from './PageIcon'
import './Archive.css'

type Tab = 'pages' | 'spaces' | 'groups'

function when(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleString()
}

// The archive: everything the user can reach that was archived, with
// Restore. Nothing in Nook is ever hard-deleted by archiving, so this is
// where an accidental "Archive" is undone. Restoring a page closes the
// archive and opens the page (onRestoredPage); restoring a space or
// group keeps the archive open, the sidebar refreshes behind it.
export default function ArchiveModal({
  onClose,
  onRestoredPage,
  onRestored,
}: {
  onClose: () => void
  onRestoredPage: (page: ArchivedPage) => void
  onRestored: () => void
}) {
  const [list, setList] = useState<ArchiveList | null>(null)
  const [tab, setTab] = useState<Tab>('pages')
  const [filter, setFilter] = useState('')
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [reloadKey, setReloadKey] = useState(0)

  useEffect(() => {
    let cancelled = false
    listArchive().then(
      (l) => !cancelled && setList(l),
      (e) => !cancelled && setError(e instanceof Error ? e.message : 'Failed to load the archive'),
    )
    return () => {
      cancelled = true
    }
  }, [reloadKey])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  async function run(id: string, action: () => Promise<unknown>, after: () => void) {
    setBusy(id)
    setError(null)
    try {
      await action()
      after()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'The action failed')
    } finally {
      setBusy(null)
    }
  }

  // Permanent delete, after a confirm; the list reloads so counts and
  // anything that moved (a detached sub-page) are current.
  function remove(id: string, label: string, warning: string, action: () => Promise<unknown>) {
    if (!window.confirm(`Delete "${label}" permanently?\n\n${warning}This cannot be undone.`)) return
    run(id, action, () => setReloadKey((k) => k + 1))
  }

  const deleteButton = (id: string, onClick: () => void) => (
    <button type="button" className="settings-secondary danger" disabled={busy === id} onClick={onClick}>
      Delete
    </button>
  )

  const q = filter.trim().toLowerCase()
  const match = (...parts: string[]) => !q || parts.join(' ').toLowerCase().includes(q)
  const pages = (list?.pages ?? []).filter((p) => match(p.title || 'Untitled', p.space_name, p.parent_title, p.archived_by))
  const spaces = (list?.spaces ?? []).filter((s) => match(s.name, s.group_name))
  const groups = (list?.groups ?? []).filter((g) => match(g.name))
  const counts: Record<Tab, number> = {
    pages: list?.pages.length ?? 0,
    spaces: list?.spaces.length ?? 0,
    groups: list?.groups.length ?? 0,
  }
  const tabs: [Tab, string][] = [
    ['pages', 'Pages'],
    ['spaces', 'Spaces'],
    ['groups', 'Groups'],
  ]

  const empty = (what: string, total: number) => (
    <p className="archive-empty">{total === 0 ? `No archived ${what}.` : `No archived ${what} match the filter.`}</p>
  )

  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="archive-modal" role="dialog" aria-modal="true" aria-label="Archive">
        <header className="archive-header">
          <div>
            <h2>Archive</h2>
            <p className="hint">Archived items are hidden, not deleted. Restore brings them back where they were; Delete removes them for good.</p>
          </div>
          <button type="button" className="settings-close" onClick={onClose} aria-label="Close">
            ×
          </button>
        </header>

        <div className="archive-toolbar">
          <div className="archive-tabs" role="tablist">
            {tabs.map(([k, label]) => (
              <button
                key={k}
                type="button"
                role="tab"
                aria-selected={tab === k}
                className={tab === k ? 'active' : ''}
                onClick={() => setTab(k)}
              >
                {label} <span className="archive-count">{counts[k]}</span>
              </button>
            ))}
          </div>
          <input
            type="search"
            className="archive-filter"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter…"
            aria-label="Filter the archive"
          />
        </div>

        {error && <p className="error archive-error">{error}</p>}

        <div className="archive-body">
          {!list && !error && <p className="archive-empty">Loading…</p>}

          {list && tab === 'pages' && (
            <ul className="archive-list">
              {pages.length === 0 && empty('pages', counts.pages)}
              {pages.map((p) => (
                <li key={p.id}>
                  <PageIcon icon={p.icon} className="page-icon archive-icon" />
                  <div className="archive-main">
                    <div className="archive-title">{p.title || 'Untitled'}</div>
                    <div className="archive-meta">
                      {[
                        p.space_name,
                        p.parent_title && `was under “${p.parent_title}”`,
                        p.sub_pages > 0 && `+${p.sub_pages} sub-page${p.sub_pages === 1 ? '' : 's'}`,
                        `archived ${when(p.archived_at)}${p.archived_by ? ` by ${p.archived_by}` : ''}`,
                      ]
                        .filter(Boolean)
                        .join(' · ')}
                    </div>
                  </div>
                  <button
                    type="button"
                    className="settings-primary"
                    disabled={busy === p.id}
                    onClick={() => run(p.id, () => restorePage(p.id), () => onRestoredPage(p))}
                  >
                    Restore
                  </button>
                  {deleteButton(p.id, () =>
                    remove(
                      p.id,
                      p.title || 'Untitled',
                      p.sub_pages > 0 ? `Its ${p.sub_pages} sub-page${p.sub_pages === 1 ? '' : 's'} archived with it go too. ` : '',
                      () => deleteArchivedPage(p.id),
                    ),
                  )}
                </li>
              ))}
            </ul>
          )}

          {list && tab === 'spaces' && (
            <ul className="archive-list">
              {spaces.length === 0 && empty('spaces', counts.spaces)}
              {spaces.map((s) => (
                <li key={s.id}>
                  <PageIcon icon={s.icon || '📁'} className="page-icon archive-icon" />
                  <div className="archive-main">
                    <div className="archive-title">{s.name}</div>
                    <div className="archive-meta">
                      {[
                        s.group_name && `in ${s.group_name}${s.group_archived ? ' (archived too, comes back with it)' : ''}`,
                        `${s.pages} page${s.pages === 1 ? '' : 's'}`,
                        `archived ${when(s.archived_at)}`,
                      ]
                        .filter(Boolean)
                        .join(' · ')}
                    </div>
                  </div>
                  <button
                    type="button"
                    className="settings-primary"
                    disabled={busy === s.id}
                    onClick={() =>
                      run(s.id, () => restoreSpace(s.id), () => {
                        setReloadKey((k) => k + 1)
                        onRestored()
                      })
                    }
                  >
                    Restore
                  </button>
                  {deleteButton(s.id, () =>
                    remove(s.id, s.name, `All ${s.pages} page${s.pages === 1 ? '' : 's'} in it are deleted too. `, () =>
                      deleteArchivedSpace(s.id),
                    ),
                  )}
                </li>
              ))}
            </ul>
          )}

          {list && tab === 'groups' && (
            <ul className="archive-list">
              {groups.length === 0 && empty('groups', counts.groups)}
              {groups.map((g) => (
                <li key={g.id}>
                  <PageIcon icon={g.icon} className="page-icon archive-icon" />
                  <div className="archive-main">
                    <div className="archive-title">{g.name}</div>
                    <div className="archive-meta">archived {when(g.archived_at)}</div>
                  </div>
                  <button
                    type="button"
                    className="settings-primary"
                    disabled={busy === g.id}
                    onClick={() =>
                      run(g.id, () => restoreGroup(g.id), () => {
                        setReloadKey((k) => k + 1)
                        onRestored()
                      })
                    }
                  >
                    Restore
                  </button>
                  {deleteButton(g.id, () => remove(g.id, g.name, '', () => deleteArchivedGroup(g.id)))}
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  )
}
