import { describe, expect, it } from 'vitest'
import {
  activePageId,
  closeOthers,
  closeTabs,
  cornerPane,
  emptyLayout,
  joinAll,
  moveTab,
  openBeside,
  openTab,
  panes,
  parseLayout,
  prune,
  resize,
  splitPane,
  type Layout,
  type Split,
} from './layout'

const tabsOf = (l: Layout) => panes(l.root).map((p) => p.tabs.join(','))

function threeTabs(): Layout {
  let l = emptyLayout()
  for (const id of ['a', 'b', 'c']) l = openTab(l, id)
  return l
}

describe('layout', () => {
  it('opens tabs in the focused pane without duplicates', () => {
    let l = threeTabs()
    l = openTab(l, 'b')
    expect(tabsOf(l)).toEqual(['a,b,c'])
    expect(activePageId(l)).toBe('b')
  })

  it('closing the active tab activates its right neighbour, else the left', () => {
    let l = openTab(threeTabs(), 'b')
    l = closeTabs(l, new Set(['b']))
    expect(activePageId(l)).toBe('c')
    l = closeTabs(l, new Set(['c']))
    expect(activePageId(l)).toBe('a')
  })

  it('reorders within a pane', () => {
    const l = threeTabs()
    const p = l.root.kind === 'pane' ? l.root.id : ''
    expect(tabsOf(moveTab(l, 'a', p, p, 2))).toEqual(['b,c,a'])
  })

  it('splits right by dragging a tab, and the source keeps the rest', () => {
    const l = threeTabs()
    const p = l.focused
    const s = splitPane(l, p, 'right', 'c', p)
    expect(tabsOf(s)).toEqual(['a,b', 'c'])
    expect((s.root as Split).dir).toBe('row')
    expect(activePageId(s)).toBe('c')
  })

  it('a split command duplicates the page', () => {
    const l = threeTabs()
    expect(tabsOf(splitPane(l, l.focused, 'bottom', 'c'))).toEqual(['a,b,c', 'c'])
  })

  it("won't split a pane by dragging its only tab to its own edge", () => {
    const l = openTab(emptyLayout(), 'a')
    expect(splitPane(l, l.focused, 'right', 'a', l.focused)).toBe(l)
  })

  it('moving the last tab out of a pane removes the pane', () => {
    const l = threeTabs()
    const left = l.focused
    const s = splitPane(l, left, 'right', 'c', left)
    const right = s.focused
    const m = moveTab(s, 'c', right, left, 0)
    expect(tabsOf(m)).toEqual(['c,a,b'])
    expect(m.root.kind).toBe('pane')
    expect(m.focused).toBe(left)
  })

  it('a same-direction split adds a sibling and halves the target', () => {
    let l = threeTabs()
    l = splitPane(l, l.focused, 'right', 'c', l.focused)
    l = splitPane(l, l.focused, 'right', 'b')
    const root = l.root as Split
    expect(root.children).toHaveLength(3)
    expect(root.sizes.map((s) => +s.toFixed(2))).toEqual([0.5, 0.25, 0.25])
  })

  it('nests a perpendicular split and collapses it when emptied', () => {
    let l = threeTabs()
    const left = l.focused
    l = splitPane(l, left, 'right', 'c', left)
    l = splitPane(l, l.focused, 'bottom', 'a', left)
    expect(tabsOf(l)).toEqual(['b', 'c', 'a'])
    expect(((l.root as Split).children[1] as Split).dir).toBe('col')
    l = closeTabs(l, new Set(['a']))
    expect(tabsOf(l)).toEqual(['b', 'c'])
    expect((l.root as Split).children.every((c) => c.kind === 'pane')).toBe(true)
  })

  it('closing everything leaves one empty pane', () => {
    let l = threeTabs()
    l = splitPane(l, l.focused, 'right', 'c', l.focused)
    l = closeTabs(l, new Set(['a', 'b', 'c']))
    expect(l.root.kind).toBe('pane')
    expect(activePageId(l)).toBeNull()
  })

  it('clamps and rescales sizes', () => {
    let l = threeTabs()
    l = splitPane(l, l.focused, 'right', 'c', l.focused)
    const id = (l.root as Split).id
    const r = resize(l, id, [0.99, 0.01]).root as Split
    expect(r.sizes[1]).toBeGreaterThan(0.07)
    expect(r.sizes[0] + r.sizes[1]).toBeCloseTo(1)
  })

  it('closeOthers, joinAll and prune', () => {
    let l = threeTabs()
    expect(tabsOf(closeOthers(l, l.focused, 'b'))).toEqual(['b'])
    l = splitPane(l, l.focused, 'right', 'c', l.focused)
    expect(tabsOf(joinAll(l))).toEqual(['a,b,c'])
    expect(tabsOf(prune(l, (id) => id !== 'c'))).toEqual(['a,b'])
  })

  it('open beside reuses the pane on the right, else splits', () => {
    let l = openTab(emptyLayout(), 'table')
    const left = l.focused
    l = openBeside(l, left, 'row1')
    expect(tabsOf(l)).toEqual(['table', 'row1'])
    l = openBeside(l, left, 'row2')
    expect(tabsOf(l)).toEqual(['table', 'row1,row2'])
    expect(activePageId(l)).toBe('row2')
  })

  it('corner panes', () => {
    let l = threeTabs()
    const left = l.focused
    l = splitPane(l, left, 'right', 'c', left)
    l = splitPane(l, l.focused, 'bottom', 'a', left)
    expect(cornerPane(l.root, 'left').tabs).toEqual(['b'])
    expect(cornerPane(l.root, 'right').tabs).toEqual(['c'])
  })

  it('parses stored layouts, old tab strips and junk', () => {
    const old = parseLayout({ tabs: [{ id: 'x', title: 'X', icon: '' }, { id: 'y' }], active: 'y' })
    expect(old && tabsOf(old)).toEqual(['x,y'])
    expect(old && activePageId(old)).toBe('y')
    let l = threeTabs()
    l = splitPane(l, l.focused, 'right', 'c', l.focused)
    const back = parseLayout(JSON.parse(JSON.stringify(l)))
    expect(back && tabsOf(back)).toEqual(['a,b', 'c'])
    expect(back?.focused).toBe(l.focused)
    expect(parseLayout(null)).toBeNull()
    expect(parseLayout({ root: { kind: 'split', id: 's', dir: 'diag', children: [] } })).toBeNull()
    const bad = parseLayout({ root: { kind: 'split', id: 's', dir: 'row', children: [{ kind: 'pane', id: 'p', tabs: ['a', 5] }], sizes: [NaN] } })
    expect(bad && tabsOf(bad)).toEqual(['a'])
  })
})
