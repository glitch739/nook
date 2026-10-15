// The open pages, VS Code / Arc style: a tree of panes. A pane has its
// own tabs (page ids) and its own active tab; a split lays its children
// side by side ('row') or stacked ('col') with resizable sizes. Pure
// functions over immutable data, so the whole model is unit-tested
// (layout.test.ts) and App.tsx only ever swaps in a new value.

export interface Pane {
  kind: 'pane'
  id: string
  tabs: string[]
  active: string | null
}

export interface Split {
  kind: 'split'
  id: string
  dir: 'row' | 'col'
  children: LayoutNode[]
  /** Fractions of the split's length, one per child, summing to 1. */
  sizes: number[]
}

export type LayoutNode = Pane | Split

export interface Layout {
  root: LayoutNode
  /** The pane that the sidebar, the palette and shortcuts act on. */
  focused: string
}

export type Edge = 'left' | 'right' | 'top' | 'bottom'

let seq = 0
export const newId = (prefix: string) => `${prefix}${Date.now().toString(36)}${(seq++).toString(36)}`

export function emptyLayout(): Layout {
  const id = newId('p')
  return { root: { kind: 'pane', id, tabs: [], active: null }, focused: id }
}

/** Every pane, in reading order (left to right, top to bottom). */
export function panes(node: LayoutNode): Pane[] {
  return node.kind === 'pane' ? [node] : node.children.flatMap(panes)
}

export function findPane(layout: Layout, id: string): Pane | undefined {
  return panes(layout.root).find((p) => p.id === id)
}

export function focusedPane(layout: Layout): Pane {
  return findPane(layout, layout.focused) ?? panes(layout.root)[0]
}

/** The page shown in the focused pane. */
export function activePageId(layout: Layout): string | null {
  return focusedPane(layout).active
}

// Rebuilds the tree with one pane replaced by whatever fn returns (a
// pane, or a split to put in its place).
function mapPane(node: LayoutNode, id: string, fn: (p: Pane) => LayoutNode): LayoutNode {
  if (node.kind === 'pane') return node.id === id ? fn(node) : node
  return { ...node, children: node.children.map((c) => mapPane(c, id, fn)) }
}

function mapPanes(node: LayoutNode, fn: (p: Pane) => Pane): LayoutNode {
  if (node.kind === 'pane') return fn(node)
  return { ...node, children: node.children.map((c) => mapPanes(c, fn)) }
}

// The tab that takes over when `removed` leaves `tabs`: its right
// neighbour, else its left one.
function neighbour(tabs: string[], removed: Set<string>, active: string | null): string | null {
  if (!active || !removed.has(active)) return active
  const kept = tabs.slice(0, tabs.indexOf(active)).filter((t) => !removed.has(t)).length
  const next = tabs.filter((t) => !removed.has(t))
  return next[kept] ?? next[kept - 1] ?? null
}

function withoutTabs(p: Pane, ids: Set<string>): Pane {
  if (!p.tabs.some((t) => ids.has(t))) return p
  return { ...p, tabs: p.tabs.filter((t) => !ids.has(t)), active: neighbour(p.tabs, ids, p.active) }
}

/**
 * Tidies the tree after any change: empty panes go (unless it's the last
 * one), a split left with one child is replaced by that child, a split
 * inside a split of the same direction is merged into it, and sizes are
 * rescaled to sum to 1. The focus moves to a pane that still exists.
 */
export function normalize(layout: Layout): Layout {
  const tidy = (node: LayoutNode): LayoutNode | null => {
    if (node.kind === 'pane') return node.tabs.length ? node : null
    const children: LayoutNode[] = []
    const sizes: number[] = []
    node.children.forEach((c, i) => {
      const t = tidy(c)
      if (!t) return
      const size = node.sizes[i] ?? 1 / node.children.length
      if (t.kind === 'split' && t.dir === node.dir) {
        t.children.forEach((gc, j) => {
          children.push(gc)
          sizes.push(size * t.sizes[j])
        })
      } else {
        children.push(t)
        sizes.push(size)
      }
    })
    if (!children.length) return null
    if (children.length === 1) return children[0]
    const total = sizes.reduce((a, b) => a + b, 0) || 1
    return { ...node, children, sizes: sizes.map((s) => s / total) }
  }
  const all = panes(layout.root)
  const root = tidy(layout.root) ?? { ...all[0], tabs: [], active: null }
  const left = panes(root)
  let focused = layout.focused
  if (!left.some((p) => p.id === focused)) {
    // The pane before the removed one in reading order, else the first.
    const before = all.slice(0, all.findIndex((p) => p.id === focused)).reverse()
    focused = before.find((p) => left.some((q) => q.id === p.id))?.id ?? left[0].id
  }
  return { root, focused }
}

export function focus(layout: Layout, paneId: string): Layout {
  return findPane(layout, paneId) && layout.focused !== paneId ? { ...layout, focused: paneId } : layout
}

/** Opens pageId in a pane (the focused one by default) and shows it. */
export function openTab(layout: Layout, pageId: string, paneId = layout.focused, index?: number): Layout {
  const target = findPane(layout, paneId) ?? focusedPane(layout)
  const root = mapPane(layout.root, target.id, (p) => {
    if (p.kind !== 'pane') return p
    if (p.tabs.includes(pageId) && index === undefined) return { ...p, active: pageId }
    const tabs = p.tabs.filter((t) => t !== pageId)
    const at = index === undefined ? tabs.length : Math.max(0, Math.min(index, tabs.length))
    tabs.splice(at, 0, pageId)
    return { ...p, tabs, active: pageId }
  })
  return { root, focused: target.id }
}

/** Closes pages everywhere (or only in one pane); empty panes go away. */
export function closeTabs(layout: Layout, ids: Set<string>, paneId?: string): Layout {
  const root = paneId
    ? mapPane(layout.root, paneId, (p) => withoutTabs(p as Pane, ids))
    : mapPanes(layout.root, (p) => withoutTabs(p, ids))
  return normalize({ ...layout, root })
}

/** Closes every tab of a pane except keep. */
export function closeOthers(layout: Layout, paneId: string, keep: string): Layout {
  const p = findPane(layout, paneId)
  if (!p) return layout
  return closeTabs(layout, new Set(p.tabs.filter((t) => t !== keep)), paneId)
}

/**
 * Moves a tab to index in another pane, or to another index in its own
 * pane. The target pane gets focus and shows it.
 */
export function moveTab(layout: Layout, pageId: string, fromPaneId: string, toPaneId: string, index: number): Layout {
  if (!findPane(layout, fromPaneId) || !findPane(layout, toPaneId)) return layout
  if (fromPaneId === toPaneId) {
    return openTab(layout, pageId, toPaneId, index)
  }
  const removed = { ...layout, root: mapPane(layout.root, fromPaneId, (p) => withoutTabs(p as Pane, new Set([pageId]))) }
  // Not normalized in between: the source pane may be the target's only
  // sibling and would otherwise take the target's place (and id) away.
  return normalize(openTab(removed, pageId, toPaneId, index))
}

/**
 * Opens pageId in a new pane at one edge of targetPaneId. With
 * fromPaneId the tab moves there (a drag); without, the page is only
 * added (a "split right" command duplicates the view, as in VS Code).
 */
export function splitPane(
  layout: Layout,
  targetPaneId: string,
  edge: Edge,
  pageId: string,
  fromPaneId?: string,
): Layout {
  const target = findPane(layout, targetPaneId)
  if (!target) return layout
  // Dragging a pane's only tab to its own edge would leave it empty.
  if (fromPaneId === targetPaneId && target.tabs.length === 1 && target.tabs[0] === pageId) return layout
  const dir: Split['dir'] = edge === 'left' || edge === 'right' ? 'row' : 'col'
  const before = edge === 'left' || edge === 'top'
  const fresh: Pane = { kind: 'pane', id: newId('p'), tabs: [pageId], active: pageId }

  let root = layout.root
  if (fromPaneId) root = mapPane(root, fromPaneId, (p) => withoutTabs(p as Pane, new Set([pageId])))

  // Next to the target inside a parent split of the same direction (the
  // target's share is halved), or the target becomes a new split.
  const insert = (node: LayoutNode): LayoutNode => {
    if (node.kind === 'pane') {
      if (node.id !== targetPaneId) return node
      return { kind: 'split', id: newId('s'), dir, children: before ? [fresh, node] : [node, fresh], sizes: [0.5, 0.5] }
    }
    const i = node.children.findIndex((c) => c.kind === 'pane' && c.id === targetPaneId)
    if (i !== -1 && node.dir === dir) {
      const children = [...node.children]
      const sizes = [...node.sizes]
      const half = sizes[i] / 2
      sizes[i] = half
      const at = before ? i : i + 1
      children.splice(at, 0, fresh)
      sizes.splice(at, 0, half)
      return { ...node, children, sizes }
    }
    return { ...node, children: node.children.map(insert) }
  }
  return normalize({ root: insert(root), focused: fresh.id })
}

/** Sets a split's sizes (fractions), keeping each at least min. */
export function resize(layout: Layout, splitId: string, sizes: number[], min = 0.08): Layout {
  const walk = (node: LayoutNode): LayoutNode => {
    if (node.kind === 'pane') return node
    if (node.id !== splitId) return { ...node, children: node.children.map(walk) }
    if (sizes.length !== node.children.length) return node
    const clamped = sizes.map((s) => Math.max(min, s))
    const total = clamped.reduce((a, b) => a + b, 0)
    return { ...node, sizes: clamped.map((s) => s / total) }
  }
  return { ...layout, root: walk(layout.root) }
}

/** Every pane's tabs gathered into one pane (keeps the focused tab). */
export function joinAll(layout: Layout): Layout {
  const all = panes(layout.root)
  if (all.length < 2) return layout
  const active = focusedPane(layout).active
  const tabs = [...new Set(all.flatMap((p) => p.tabs))]
  const id = layout.focused
  return { root: { kind: 'pane', id, tabs, active: active ?? tabs[0] ?? null }, focused: id }
}

/** Drops pages that no longer exist (archived, moved out of reach). */
export function prune(layout: Layout, exists: (pageId: string) => boolean): Layout {
  const gone = new Set(panes(layout.root).flatMap((p) => p.tabs.filter((t) => !exists(t))))
  return gone.size ? closeTabs(layout, gone) : layout
}

/**
 * Reads a stored layout. Storage is per browser and could hold anything
 * (an older version, a hand edit), so every field is checked; the old
 * single tab strip ({tabs, active}) becomes one pane.
 */
export function parseLayout(raw: unknown): Layout | null {
  if (!raw || typeof raw !== 'object') return null
  const r = raw as Record<string, unknown>
  if (Array.isArray(r.tabs)) {
    const tabs = r.tabs.map((t) => (t && typeof t === 'object' ? (t as { id?: unknown }).id : t)).filter(isId)
    const id = newId('p')
    const active = isId(r.active) && tabs.includes(r.active) ? r.active : (tabs[0] ?? null)
    return { root: { kind: 'pane', id, tabs, active }, focused: id }
  }
  const node = (n: unknown, depth: number): LayoutNode | null => {
    if (!n || typeof n !== 'object' || depth > 12) return null
    const o = n as Record<string, unknown>
    if (!isId(o.id)) return null
    if (o.kind === 'pane' && Array.isArray(o.tabs)) {
      const tabs = [...new Set(o.tabs.filter(isId))]
      return { kind: 'pane', id: o.id, tabs, active: isId(o.active) && tabs.includes(o.active) ? o.active : (tabs[0] ?? null) }
    }
    if (o.kind === 'split' && (o.dir === 'row' || o.dir === 'col') && Array.isArray(o.children)) {
      const children = o.children.map((c) => node(c, depth + 1)).filter((c): c is LayoutNode => c !== null)
      const sizes = Array.isArray(o.sizes) && o.sizes.length === o.children.length ? o.sizes : []
      const ok = sizes.every((s) => typeof s === 'number' && s > 0 && Number.isFinite(s))
      return {
        kind: 'split',
        id: o.id,
        dir: o.dir,
        children,
        sizes: ok && children.length === o.children.length ? (sizes as number[]) : children.map(() => 1 / children.length),
      }
    }
    return null
  }
  const root = node(r.root, 0)
  if (!root) return null
  return normalize({ root, focused: isId(r.focused) ? r.focused : '' })
}

function isId(v: unknown): v is string {
  return typeof v === 'string' && v.length > 0 && v.length < 100
}

/** The pane at the top-left corner (gets the sidebar toggle). */
export function cornerPane(node: LayoutNode, corner: 'left' | 'right'): Pane {
  if (node.kind === 'pane') return node
  const child = node.dir === 'row' && corner === 'right' ? node.children[node.children.length - 1] : node.children[0]
  return cornerPane(child, corner)
}

/**
 * Opens pageId beside a pane, VS Code's "open to the side": in the pane
 * right of it when there is one, else in a new pane on its right. So
 * opening several rows of a table one after another fills one pane
 * instead of piling up new ones.
 */
export function openBeside(layout: Layout, paneId: string, pageId: string): Layout {
  const find = (node: LayoutNode): Pane | null => {
    if (node.kind === 'pane') return null
    const i = node.children.findIndex((c) => c.kind === 'pane' && c.id === paneId)
    if (i !== -1 && node.dir === 'row' && node.children[i + 1]) return panes(node.children[i + 1])[0]
    for (const c of node.children) {
      const hit = find(c)
      if (hit) return hit
    }
    return null
  }
  const right = find(layout.root)
  return right ? openTab(layout, pageId, right.id) : splitPane(layout, paneId, 'right', pageId)
}
