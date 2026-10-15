import { useEffect, useRef, useState, type CSSProperties } from 'react'
import PageView, { type PageActions } from './PageView'
import Popover from './Popover'
import TabBar, { type OpenTab } from './TabBar'
import { draggedPage } from './pageDrag'
import { startPointerDrag } from './pointerDrag'
import { findPage, type TeamSpace } from './sidebarData'
import {
  closeOthers,
  closeTabs,
  cornerPane,
  focus,
  focusedPane,
  moveTab,
  openBeside,
  openTab,
  panes,
  resize,
  splitPane,
  type Edge,
  type Layout,
  type LayoutNode,
  type Pane,
} from './layout'

// The open pages as a grid of panes (layout.ts), VS Code / Arc style.
// Every pane is drawn absolutely positioned inside one container, keyed
// by its id, never nested in its split's DOM: splitting, moving or
// closing a neighbour then only moves a pane, it never remounts it, so
// its editor, scroll position and caret survive. Dividers sit on the
// borders between panes and resize the split they belong to.

interface Rect {
  x: number
  y: number
  w: number
  h: number
}

interface Divider {
  splitId: string
  index: number // between child index and index + 1
  dir: 'row' | 'col'
  /** Where the border is, as a fraction of the whole grid. */
  at: number
  /** The split's own rect (its length is what sizes are fractions of). */
  split: Rect
  sizes: number[]
}

function place(node: LayoutNode, rect: Rect, out: { panes: Map<string, Rect>; dividers: Divider[] }) {
  if (node.kind === 'pane') {
    out.panes.set(node.id, rect)
    return
  }
  let offset = 0
  node.children.forEach((child, i) => {
    const size = node.sizes[i]
    const r =
      node.dir === 'row'
        ? { x: rect.x + offset * rect.w, y: rect.y, w: size * rect.w, h: rect.h }
        : { x: rect.x, y: rect.y + offset * rect.h, w: rect.w, h: size * rect.h }
    offset += size
    if (i < node.children.length - 1) {
      out.dividers.push({
        splitId: node.id,
        index: i,
        dir: node.dir,
        at: node.dir === 'row' ? rect.x + offset * rect.w : rect.y + offset * rect.h,
        split: rect,
        sizes: node.sizes,
      })
    }
    place(child, r, out)
  })
}

const pct = (v: number) => `${v * 100}%`

// Where a drag over a pane's body lands: one of its edges (a new pane
// there) when within this share of the pane from that edge, else the
// pane itself.
const EDGE_ZONE = 0.22

type DropTarget =
  | { paneId: string; zone: 'center' | Edge }
  | { paneId: string; zone: 'tabs'; index: number; left: number; top: number; height: number }

// The dragged thing: a tab of a pane (pointer drag, TabBar.tsx) or a
// page from the sidebar or a collection (HTML5 drag, pageDrag.ts).
type DragSource = { tabId: string; paneId: string } | { pageId: string }

function targetAt(x: number, y: number, source: DragSource, layout: Layout): DropTarget | null {
  const el = document.elementFromPoint(x, y)
  const paneEl = el?.closest<HTMLElement>('[data-pane-id]')
  const paneId = paneEl?.dataset.paneId
  const pane = paneId ? panes(layout.root).find((p) => p.id === paneId) : undefined
  if (!paneEl || !pane) return null
  const fromPane = 'tabId' in source ? source.paneId : null
  const dragged = 'tabId' in source ? source.tabId : source.pageId

  const bar = el?.closest<HTMLElement>('[data-pane-tabs]')
  if (bar) {
    // Into this pane's tab strip, between the tabs the pointer is between.
    const tabs = Array.from(bar.querySelectorAll<HTMLElement>('[data-tab-id]')).filter(
      (t) => t.dataset.tabId !== dragged,
    )
    const index = tabs.filter((t) => {
      const r = t.getBoundingClientRect()
      return r.left + r.width / 2 < x
    }).length
    const paneRect = paneEl.getBoundingClientRect()
    const barRect = bar.getBoundingClientRect()
    const edge =
      index < tabs.length
        ? tabs[index].getBoundingClientRect().left - 2
        : tabs.length
          ? tabs[tabs.length - 1].getBoundingClientRect().right + 2
          : barRect.left + 4
    return {
      paneId: pane.id,
      zone: 'tabs',
      index,
      left: edge - paneRect.left,
      top: barRect.top - paneRect.top + 4,
      height: barRect.height - 8,
    }
  }

  const body = el?.closest<HTMLElement>('[data-pane-body]')
  if (!body) return null
  const r = body.getBoundingClientRect()
  const rx = (x - r.left) / r.width
  const ry = (y - r.top) / r.height
  const edges: [Edge, number][] = [
    ['left', rx],
    ['right', 1 - rx],
    ['top', ry],
    ['bottom', 1 - ry],
  ]
  const [edge, d] = edges.reduce((a, b) => (b[1] < a[1] ? b : a))
  if (d < EDGE_ZONE) {
    // Splitting a pane by its only tab would leave it empty.
    if (fromPane === pane.id && pane.tabs.length === 1) return null
    return { paneId: pane.id, zone: edge }
  }
  // Dropping a tab back onto its own pane changes nothing.
  if (fromPane === pane.id) return null
  return { paneId: pane.id, zone: 'center' }
}

function applyDrop(layout: Layout, source: DragSource, t: DropTarget): Layout {
  if ('tabId' in source) {
    if (t.zone === 'tabs') return moveTab(layout, source.tabId, source.paneId, t.paneId, t.index)
    if (t.zone === 'center') {
      const target = panes(layout.root).find((p) => p.id === t.paneId)
      return moveTab(layout, source.tabId, source.paneId, t.paneId, target?.tabs.length ?? 0)
    }
    return splitPane(layout, t.paneId, t.zone, source.tabId, source.paneId)
  }
  if (t.zone === 'tabs') return openTab(layout, source.pageId, t.paneId, t.index)
  if (t.zone === 'center') return openTab(layout, source.pageId, t.paneId)
  return splitPane(layout, t.paneId, t.zone, source.pageId)
}

// The right-click menu of a tab: splits duplicate the page (VS Code's
// "Split Editor"); the "move" entries take the tab out of its pane.
function menuItems(
  pane: Pane,
  tabId: string,
  others: Pane[],
  titleOf: (pageId: string | null) => string,
): [string, (l: Layout) => Layout][] {
  const items: [string, (l: Layout) => Layout][] = [
    ['Split right', (l) => splitPane(l, pane.id, 'right', tabId)],
    ['Split down', (l) => splitPane(l, pane.id, 'bottom', tabId)],
  ]
  if (pane.tabs.length > 1) {
    items.push(['Move to a new pane on the right', (l) => splitPane(l, pane.id, 'right', tabId, pane.id)])
  }
  // Panes are named by the page they show.
  // Two panes showing the same page get a number to tell them apart.
  const names = others.map((p) => titleOf(p.active))
  others.forEach((p, i) => {
    const n = names.filter((x) => x === names[i]).length > 1 ? ` (${names.slice(0, i + 1).filter((x) => x === names[i]).length})` : ''
    items.push([`Move next to “${names[i]}”${n}`, (l) => moveTab(l, tabId, pane.id, p.id, p.tabs.length)])
  })
  if (pane.tabs.length > 1) items.push(['Close other tabs', (l) => closeOthers(l, pane.id, tabId)])
  items.push(['Close tab', (l) => closeTabs(l, new Set([tabId]), pane.id)])
  return items
}

export interface WorkspaceProps {
  layout: Layout
  setLayout: (update: (l: Layout) => Layout) => void
  teamspaces: TeamSpace[]
  teamspacesLoaded: boolean
  meId: string
  isMobile: boolean
  focusTitleId: string | null
  onTitleFocused: () => void
  actions: PageActions
  /** Shared by every pane's tab bar (only shown on the corner panes). */
  bar: {
    sidebarCollapsed: boolean
    onToggleSidebar: () => void
    userLabel: string
    onOpenSettings: () => void
    onLogout: () => void
    pendingApprovals: number
    onOpenApprovals: () => void
  }
}

export default function Workspace(props: WorkspaceProps) {
  const { layout, setLayout, teamspaces, isMobile } = props
  const gridRef = useRef<HTMLDivElement>(null)
  const [drop, setDrop] = useState<DropTarget | null>(null)
  // Read by the drop handlers, which run outside React's render.
  const dropRef = useRef<DropTarget | null>(null)
  const showDrop = (t: DropTarget | null) => {
    const prev = dropRef.current
    dropRef.current = t
    if (JSON.stringify(prev) !== JSON.stringify(t)) setDrop(t)
  }
  const [resizing, setResizing] = useState(false)
  // A page drag that ends anywhere (dropped elsewhere, Escape) clears the
  // marker; the grid only hears about drags that end over it.
  useEffect(() => {
    const clear = () => {
      dropRef.current = null
      setDrop(null)
    }
    document.addEventListener('dragend', clear)
    return () => document.removeEventListener('dragend', clear)
  }, [])
  const [menu, setMenu] = useState<{ paneId: string; tabId: string; anchor: HTMLElement } | null>(null)

  // Phones show the focused pane only, full size; the others are kept.
  const all = isMobile ? [focusedPane(layout)] : panes(layout.root)
  const geometry = { panes: new Map<string, Rect>(), dividers: [] as Divider[] }
  if (isMobile) geometry.panes.set(all[0].id, { x: 0, y: 0, w: 1, h: 1 })
  else place(layout.root, { x: 0, y: 0, w: 1, h: 1 }, geometry)
  const split = all.length > 1
  const topLeft = isMobile ? all[0].id : cornerPane(layout.root, 'left').id
  const topRight = isMobile ? all[0].id : cornerPane(layout.root, 'right').id

  const tabsOf = (pane: Pane): OpenTab[] =>
    pane.tabs.flatMap((id) => {
      const node = findPage(teamspaces, id)
      return node ? [{ id, title: node.title || 'Untitled', icon: node.icon }] : []
    })

  function startResize(d: Divider, e: React.PointerEvent) {
    const grid = gridRef.current?.getBoundingClientRect()
    if (!grid) return
    const length = d.dir === 'row' ? grid.width * d.split.w : grid.height * d.split.h
    const start = d.sizes
    setResizing(true)
    startPointerDrag(e, {
      cursor: d.dir === 'row' ? 'col-resize' : 'row-resize',
      onMove: (dx, dy) => {
        const delta = (d.dir === 'row' ? dx : dy) / length
        const pair = start[d.index] + start[d.index + 1]
        // At least 200px each side, or 8% on a small screen.
        const min = Math.min(200 / length, pair / 2)
        const a = Math.min(Math.max(start[d.index] + delta, min), pair - min)
        const sizes = [...start]
        sizes[d.index] = a
        sizes[d.index + 1] = pair - a
        setLayout((l) => resize(l, d.splitId, sizes, 0))
      },
      onEnd: () => setResizing(false),
    })
  }

  function equalize(d: Divider) {
    const sizes = [...d.sizes]
    const pair = sizes[d.index] + sizes[d.index + 1]
    sizes[d.index] = sizes[d.index + 1] = pair / 2
    setLayout((l) => resize(l, d.splitId, sizes))
  }

  function nudge(d: Divider, by: number) {
    const sizes = [...d.sizes]
    sizes[d.index] += by
    sizes[d.index + 1] -= by
    if (sizes[d.index] > 0.08 && sizes[d.index + 1] > 0.08) setLayout((l) => resize(l, d.splitId, sizes))
  }

  // HTML5 drags of a page from the sidebar or a collection, over a pane.
  // Capture phase, so the editor inside never sees a page drop (it would
  // insert the page id as text).
  function pageDragOver(e: React.DragEvent) {
    const pageId = draggedPage()
    if (!pageId) return
    const t = targetAt(e.clientX, e.clientY, { pageId }, layout)
    if (!t) return
    e.preventDefault()
    e.stopPropagation()
    // The sidebar and collections start their drags as 'move'.
    e.dataTransfer.dropEffect = 'move'
    showDrop(t)
  }
  function pageDrop(e: React.DragEvent) {
    const pageId = draggedPage()
    // Recomputed here: Safari fires dragleave with no relatedTarget while
    // moving between a pane's children, which can clear the stored one.
    const t = pageId ? (targetAt(e.clientX, e.clientY, { pageId }, layout) ?? dropRef.current) : null
    if (!pageId || !t) return
    e.preventDefault()
    e.stopPropagation()
    showDrop(null)
    setLayout((l) => applyDrop(l, { pageId }, t))
  }
  function pageDragLeave(e: React.DragEvent) {
    // Only when leaving the grid itself, not moving between its children.
    if (!gridRef.current?.contains(e.relatedTarget as Node | null)) showDrop(null)
  }

  const menuPane = menu ? panes(layout.root).find((p) => p.id === menu.paneId) : undefined
  const otherPanes = menu ? panes(layout.root).filter((p) => p.id !== menu.paneId) : []

  return (
    <div
      ref={gridRef}
      className={['workspace-grid', resizing && 'workspace-grid-resizing', split && 'workspace-grid-split']
        .filter(Boolean)
        .join(' ')}
      onDragOverCapture={pageDragOver}
      onDropCapture={pageDrop}
      onDragLeave={pageDragLeave}
    >
      {all.map((pane) => {
        const r = geometry.panes.get(pane.id)!
        const style: CSSProperties = { left: pct(r.x), top: pct(r.y), width: pct(r.w), height: pct(r.h) }
        const target = drop?.paneId === pane.id ? drop : null
        return (
          <section
            key={pane.id}
            className={['pane', split && pane.id === layout.focused && 'pane-focused'].filter(Boolean).join(' ')}
            style={style}
            data-pane-id={pane.id}
            onPointerDownCapture={() => setLayout((l) => focus(l, pane.id))}
            onFocusCapture={() => setLayout((l) => focus(l, pane.id))}
          >
            <TabBar
              paneId={pane.id}
              tabs={tabsOf(pane)}
              activeId={pane.active}
              focused={!split || pane.id === layout.focused}
              showToggle={pane.id === topLeft}
              showAccount={pane.id === topRight}
              onSelectTab={(id) => setLayout((l) => openTab(l, id, pane.id))}
              onCloseTab={(id) => setLayout((l) => closeTabs(l, new Set([id]), pane.id))}
              onReorderTabs={(dragged, target) =>
                setLayout((l) => moveTab(l, dragged, pane.id, pane.id, pane.tabs.indexOf(target)))
              }
              onMoveTab={(dragged, index) => setLayout((l) => moveTab(l, dragged, pane.id, pane.id, index))}
              onDragOut={(tabId, x, y) => showDrop(targetAt(x, y, { tabId, paneId: pane.id }, layout))}
              onDropOut={(tabId) => {
                const t = dropRef.current
                if (t) setLayout((l) => applyDrop(l, { tabId, paneId: pane.id }, t))
              }}
              onDragOutEnd={() => showDrop(null)}
              onTabMenu={(tabId, anchor) => setMenu({ paneId: pane.id, tabId, anchor })}
              sidebarCollapsed={props.bar.sidebarCollapsed}
              onToggleSidebar={props.bar.onToggleSidebar}
              compact={isMobile}
              userLabel={props.bar.userLabel}
              onOpenSettings={props.bar.onOpenSettings}
              pendingApprovals={props.bar.pendingApprovals}
              onOpenApprovals={props.bar.onOpenApprovals}
              onLogout={props.bar.onLogout}
            />
            <div className="pane-body">
              <div className="workspace-body" data-pane-body={pane.id}>
                {pane.active ? (
                  <PageView
                    key={pane.active}
                    pageId={pane.active}
                    teamspaces={teamspaces}
                    meId={props.meId}
                    autoFocusTitle={props.focusTitleId === pane.active && pane.id === layout.focused}
                    onAutoFocused={props.onTitleFocused}
                    actions={
                      isMobile
                        ? props.actions
                        : { ...props.actions, openBeside: (id) => setLayout((l) => openBeside(l, pane.id, id)) }
                    }
                  />
                ) : (
                  <p className="hint">
                    {props.teamspacesLoaded
                      ? 'Pick a page from the sidebar, or create a new one with the "+" button.'
                      : 'Loading…'}
                  </p>
                )}
              </div>
              {target && target.zone !== 'tabs' && <div className={`pane-drop pane-drop-${target.zone}`} />}
            </div>
            {target?.zone === 'tabs' && (
              <div className="pane-tab-marker" style={{ left: target.left, top: target.top, height: target.height }} />
            )}
          </section>
        )
      })}

      {geometry.dividers.map((d) => {
        const row = d.dir === 'row'
        const style: CSSProperties = row
          ? { left: pct(d.at), top: pct(d.split.y), height: pct(d.split.h) }
          : { top: pct(d.at), left: pct(d.split.x), width: pct(d.split.w) }
        return (
          <div
            key={`${d.splitId}-${d.index}`}
            className={`pane-divider pane-divider-${d.dir}`}
            style={style}
            role="separator"
            aria-orientation={row ? 'vertical' : 'horizontal'}
            aria-label="Resize panes"
            tabIndex={0}
            onPointerDown={(e) => startResize(d, e)}
            onDoubleClick={() => equalize(d)}
            onKeyDown={(e) => {
              const back = row ? 'ArrowLeft' : 'ArrowUp'
              const fwd = row ? 'ArrowRight' : 'ArrowDown'
              if (e.key === back || e.key === fwd) {
                e.preventDefault()
                nudge(d, e.key === fwd ? 0.02 : -0.02)
              }
            }}
          />
        )
      })}

      {menu && menuPane && (
        <Popover anchorEl={menu.anchor} width={220} onClose={() => setMenu(null)} className="create-menu">
          {menuItems(menuPane, menu.tabId, otherPanes, (id) => (id && findPage(teamspaces, id)?.title) || 'Untitled').map(([label, next]) => (
            <button
              key={label}
              type="button"
              className="create-menu-item"
              onClick={() => {
                setMenu(null)
                setLayout(next)
              }}
            >
              {label}
            </button>
          ))}
        </Popover>
      )}
    </div>
  )
}
