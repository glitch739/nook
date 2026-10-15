// What the editor's page links need from the app, without threading it
// through every component: the sidebar tree (live names and icons, and
// the pages to pick from) and how to open a page. App.tsx keeps it
// current; PageMention.tsx reads it.
import { useSyncExternalStore } from 'react'
import type { PageNode, TeamSpace } from './sidebarData'

let spaces: TeamSpace[] = []
// Built once per tree change, not per link per render: every link in
// every open editor reads its page from here.
let entries: PageEntry[] = []
let byId = new Map<string, PageNode>()
let opener: (id: string) => void = () => {}
const listeners = new Set<() => void>()

export function setPageDirectory(teamspaces: TeamSpace[], open: (id: string) => void) {
  opener = open
  if (teamspaces === spaces) return
  spaces = teamspaces
  entries = all()
  byId = new Map(entries.map((e) => [e.node.id, e.node]))
  for (const fn of listeners) fn()
}

export function openLinkedPage(id: string) {
  opener(id)
}

function subscribe(fn: () => void) {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

export interface PageEntry {
  node: PageNode
  space: string
}

function all(): PageEntry[] {
  const out: PageEntry[] = []
  const walk = (nodes: PageNode[] | undefined, space: string) => {
    for (const n of nodes ?? []) {
      out.push({ node: n, space })
      walk(n.children, space)
    }
  }
  for (const t of spaces) walk(t.pages, t.name)
  return out
}

/** Pages whose name contains query, best matches (name starts with it) first. */
export function searchPages(query: string, limit = 8): PageEntry[] {
  const q = query.trim().toLowerCase()
  const hits = entries.filter((e) => (e.node.title || 'Untitled').toLowerCase().includes(q))
  const starts = (e: PageEntry) => ((e.node.title || 'Untitled').toLowerCase().startsWith(q) ? 0 : 1)
  return hits.sort((a, b) => starts(a) - starts(b)).slice(0, limit)
}

/** A page's live name and icon, or null when it's gone or out of reach. */
export function usePageNode(id: string): PageNode | null {
  return useSyncExternalStore(subscribe, () => byId.get(id) ?? null)
}
