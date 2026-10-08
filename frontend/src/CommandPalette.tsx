import { useEffect, useMemo, useRef, useState } from 'react'
import PageIcon from './PageIcon'
import { fold, score } from './paletteSearch'
import type { PageNode, SidebarGroup, TeamSpace } from './sidebarData'
import './CommandPalette.css'

// Cmd/Ctrl+K: jump to any page by title, or run an action, from the
// keyboard. (Full-text search inside pages stays in the sidebar, Cmd/
// Ctrl+Shift+F, and is also one of the actions here.)

export interface PaletteAction {
  id: string
  label: string
  hint?: string
  icon?: string
  run: () => void
}

interface Item {
  key: string
  label: string
  detail: string
  icon: string
  run: () => void
}

export default function CommandPalette({
  teamspaces,
  groups,
  actions,
  onOpenPage,
  onClose,
}: {
  teamspaces: TeamSpace[]
  groups: SidebarGroup[]
  actions: PaletteAction[]
  onOpenPage: (id: string) => void
  onClose: () => void
}) {
  const [query, setQuery] = useState('')
  const [index, setIndex] = useState(0)
  const listRef = useRef<HTMLUListElement>(null)

  const pages = useMemo(() => {
    const groupName = new Map(groups.map((g) => [g.id, g.name]))
    const out: { page: PageNode; path: string }[] = []
    for (const space of teamspaces) {
      const base = [groupName.get(space.groupId ?? ''), space.name].filter(Boolean).join(' / ')
      const walk = (list: PageNode[], trail: string) => {
        for (const p of list) {
          out.push({ page: p, path: trail })
          if (p.children?.length) walk(p.children, `${trail} / ${p.title || 'Untitled'}`)
        }
      }
      walk(space.pages, base)
    }
    return out
  }, [teamspaces, groups])

  const items = useMemo<Item[]>(() => {
    const q = fold(query.trim())
    const actionItems = actions
      .map((a) => ({ a, s: score(a.label, q) }))
      .filter((x) => x.s >= 0)
      .sort((x, y) => x.s - y.s)
      .map(({ a }) => ({ key: 'a:' + a.id, label: a.label, detail: a.hint ?? '', icon: a.icon ?? '›', run: a.run }))
    const pageItems = pages
      .map((x) => ({ x, s: score(x.page.title || 'Untitled', q) }))
      .filter((y) => y.s >= 0)
      .sort((a, b) => a.s - b.s)
      .slice(0, 40)
      .map(({ x }) => ({
        key: 'p:' + x.page.id,
        label: x.page.title || 'Untitled',
        detail: x.path,
        icon: x.page.icon,
        run: () => onOpenPage(x.page.id),
      }))
    // With no query: actions first; while typing, pages first (that's
    // what people mostly search for).
    return q ? [...pageItems, ...actionItems] : [...actionItems, ...pageItems]
  }, [query, pages, actions, onOpenPage])

  const selected = Math.min(index, Math.max(0, items.length - 1))

  useEffect(() => {
    listRef.current?.querySelector('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest' })
  }, [selected])

  function run(item: Item | undefined) {
    if (!item) return
    onClose()
    item.run()
  }

  return (
    <div className="modal-backdrop palette-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="palette" role="dialog" aria-modal="true" aria-label="Command palette">
        <input
          className="palette-input"
          autoFocus
          placeholder="Go to a page or run a command…"
          value={query}
          onChange={(e) => {
            setQuery(e.target.value)
            setIndex(0)
          }}
          onKeyDown={(e) => {
            if (e.key === 'ArrowDown') {
              e.preventDefault()
              setIndex((i) => Math.min(i + 1, items.length - 1))
            } else if (e.key === 'ArrowUp') {
              e.preventDefault()
              setIndex((i) => Math.max(i - 1, 0))
            } else if (e.key === 'Enter') {
              e.preventDefault()
              run(items[selected])
            } else if (e.key === 'Escape') {
              e.preventDefault()
              onClose()
            }
          }}
        />
        <ul className="palette-list" ref={listRef} role="listbox">
          {items.length === 0 && <li className="palette-empty">Nothing matches.</li>}
          {items.map((item, i) => (
            <li
              key={item.key}
              role="option"
              aria-selected={i === selected}
              className={i === selected ? 'palette-item active' : 'palette-item'}
              onMouseMove={() => setIndex(i)}
              onClick={() => run(item)}
            >
              <PageIcon icon={item.icon} className="page-icon palette-icon" />
              <span className="palette-label">{item.label}</span>
              {item.detail && <span className="palette-detail">{item.detail}</span>}
            </li>
          ))}
        </ul>
      </div>
    </div>
  )
}
