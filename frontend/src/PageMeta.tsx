import { useState } from 'react'
import { pageActivity, type ActivityEntry, type Page } from './api'
import './PageMeta.css'

const ACTION_LABELS: Record<string, string> = {
  'page.create': 'created it',
  'page.edit': 'edited it',
  'page.rename': 'renamed it',
  'page.move': 'moved it',
  'page.archive': 'archived it',
  'row.edit': 'edited a row',
}

function relativeTime(iso: string): string {
  const t = new Date(iso).getTime()
  if (isNaN(t)) return ''
  const s = Math.round((Date.now() - t) / 1000)
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  if (s < 7 * 86400) return `${Math.floor(s / 86400)}d ago`
  return new Date(iso).toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' })
}

function describe(e: ActivityEntry, pageId: string): string {
  const verb = ACTION_LABELS[e.action] ?? e.action
  const meta = e.meta ?? {}
  let extra = ''
  if (e.action === 'row.edit') {
    extra = `: ${e.page_title || 'untitled'}${meta.property ? ` · ${String(meta.property)}` : ''}`
  } else if (e.page_id !== pageId) {
    extra = `: ${e.page_title || 'untitled'}`
  } else if (e.action === 'page.rename' && meta.title) {
    extra = ` → "${String(meta.title)}"`
  }
  return `${verb}${extra}`
}

// "Last edited by X · 5m ago" under the page title area, plus an
// expandable activity feed (ADR 0005 §9).
export default function PageMeta({ page, meId }: { page: Page; meId: string }) {
  const [open, setOpen] = useState(false)
  const [entries, setEntries] = useState<ActivityEntry[] | null>(null)

  function toggle() {
    const next = !open
    setOpen(next)
    if (next) pageActivity(page.id).then(setEntries).catch(() => setEntries([]))
  }

  const who = page.updated_by === meId ? 'you' : page.updated_by_name || 'unknown'

  return (
    <div className="page-meta">
      <button type="button" className="page-meta-line" onClick={toggle} title="History">
        {page.updated_by ? `Last edited by ${who} · ${relativeTime(page.updated_at)}` : `Edited ${relativeTime(page.updated_at)}`}
        <span className="page-meta-chevron">{open ? '▾' : '▸'}</span>
      </button>
      {open && (
        <ul className="page-activity">
          {entries === null && <li className="hint">Loading…</li>}
          {entries?.length === 0 && <li className="hint">No history recorded yet.</li>}
          {entries?.map((e) => (
            <li key={e.id}>
              <strong>{e.actor_id === meId ? 'You' : e.actor_name || 'Deleted user'}</strong> {describe(e, page.id)}
              <span className="hint"> · {relativeTime(e.created_at)}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
