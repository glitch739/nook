// Sidebar tree types + helpers, backed by the real spaces/pages API
// (internal/pages, internal/httpapi/pages.go on the backend). This used
// to be a static mock; loadTeamspaces() below now builds the same shape
// from real data.

import { getSidebar, type Group, type PageKind, type Space, type TreeNode } from './api'

export interface PageNode {
  id: string
  title: string
  icon: string
  kind: PageKind
  children?: PageNode[]
}

export interface TeamSpace {
  id: string
  name: string
  icon: string
  type: Space['type']
  // Which group (SidebarGroup) this space is filed under -- groups.map
  // over the sidebar's own SidebarGroup[] state renders each group's
  // teamspaces by filtering this app-side rather than the tree itself
  // being nested one level deeper, so every existing helper below
  // (findPage, updatePage, insertPage, ...) keeps working over a flat
  // TeamSpace[] unchanged.
  groupId: string
  pages: PageNode[]
}

// SidebarGroup is deliberately just the group's own metadata, not a
// container of TeamSpace[] -- see TeamSpace.groupId's own comment.
export interface SidebarGroup {
  id: string
  name: string
  icon: string
}

function toPageNode(node: TreeNode): PageNode {
  return {
    id: node.id,
    title: node.title,
    icon: node.icon,
    kind: node.kind,
    children: node.children.length > 0 ? node.children.map(toPageNode) : undefined,
  }
}

/** Loads the whole sidebar in one request: every group and space the
 * current user can see, in their own order, each space with its page tree. */
export async function loadSidebar(): Promise<{ teamspaces: TeamSpace[]; groups: SidebarGroup[] }> {
  const data = await getSidebar()
  return {
    teamspaces: data.spaces.map((space) => ({
      id: space.id,
      name: space.name,
      icon: space.icon,
      type: space.type,
      groupId: space.group_id,
      pages: space.pages.map(toPageNode),
    })),
    groups: data.groups.map((g) => ({ id: g.id, name: g.name, icon: g.icon })),
  }
}

/** Immutably appends a newly created space, for an optimistic sidebar
 * update right after a successful createSpace() call. */
export function insertSpace(teamspaces: TeamSpace[], space: Space): TeamSpace[] {
  return [
    ...teamspaces,
    { id: space.id, name: space.name, icon: space.icon, type: space.type, groupId: space.group_id, pages: [] },
  ]
}

/** Immutably appends a newly created group, for an optimistic sidebar
 * update right after a successful createGroup() call. */
export function insertGroup(groups: SidebarGroup[], group: Group): SidebarGroup[] {
  return [...groups, { id: group.id, name: group.name, icon: group.icon }]
}

/** Immutably updates one group's icon, mirroring updateSpaceIcon. */
export function updateGroupIcon(groups: SidebarGroup[], id: string, icon: string): SidebarGroup[] {
  return groups.map((g) => (g.id === id ? { ...g, icon } : g))
}

/** Immutably renames one group, mirroring updateSpaceName. */
export function updateGroupName(groups: SidebarGroup[], id: string, name: string): SidebarGroup[] {
  return groups.map((g) => (g.id === id ? { ...g, name } : g))
}

/** Immutably reorders the group list to match ids, mirroring
 * reorderTeamspaces -- ids must be exactly a permutation of the
 * current groups' own ids. */
export function reorderSidebarGroups(groups: SidebarGroup[], ids: string[]): SidebarGroup[] {
  const byId = new Map(groups.map((g) => [g.id, g]))
  return ids.map((id) => byId.get(id)!)
}

/** Immutably updates one space's icon, for an optimistic sidebar update
 * right after a successful updateSpace() call. */
export function updateSpaceIcon(teamspaces: TeamSpace[], id: string, icon: string): TeamSpace[] {
  return teamspaces.map((space) => (space.id === id ? { ...space, icon } : space))
}

/** Immutably renames one space, for an optimistic sidebar update right
 * after a successful updateSpace() call. */
export function updateSpaceName(teamspaces: TeamSpace[], id: string, name: string): TeamSpace[] {
  return teamspaces.map((space) => (space.id === id ? { ...space, name } : space))
}

/** Immutably removes a whole space, for an optimistic sidebar update
 * right after a successful archiveSpace() call. */
export function removeSpace(teamspaces: TeamSpace[], id: string): TeamSpace[] {
  return teamspaces.filter((space) => space.id !== id)
}

/** Immutably removes a whole group, for an optimistic sidebar update
 * right after a successful archiveGroup() call. The backend refuses to
 * archive a non-empty group, so there's no matching "remove its
 * spaces too" here -- by the time this runs, it never had any. */
export function removeGroup(groups: SidebarGroup[], id: string): SidebarGroup[] {
  return groups.filter((g) => g.id !== id)
}

/** Immutably reorders the top-level teamspace list to match ids, for an
 * optimistic sidebar update right after a successful reorderSpaces()
 * call -- ids must be exactly a permutation of the current teamspaces'
 * own ids (App.tsx's reorderTeamspaces is the only caller, and already
 * builds ids that way). */
export function reorderTeamspaces(teamspaces: TeamSpace[], ids: string[]): TeamSpace[] {
  const byId = new Map(teamspaces.map((t) => [t.id, t]))
  return ids.map((id) => byId.get(id)!)
}

/** Every page id in a tree (and its subtrees), depth-first -- used to
 * figure out which open tabs to close when a whole space is archived. */
export function collectPageIds(pages: PageNode[]): string[] {
  return pages.flatMap((p) => [p.id, ...(p.children ? collectPageIds(p.children) : [])])
}

function mapPages(pages: PageNode[], id: string, fn: (page: PageNode) => PageNode): PageNode[] {
  return pages.map((page) => {
    if (page.id === id) return fn(page)
    if (page.children) return { ...page, children: mapPages(page.children, id, fn) }
    return page
  })
}

/** Immutably updates one page (by id) anywhere in the tree, across all teamspaces. */
export function updatePage(
  teamspaces: TeamSpace[],
  id: string,
  fn: (page: PageNode) => PageNode,
): TeamSpace[] {
  return teamspaces.map((space) => ({ ...space, pages: mapPages(space.pages, id, fn) }))
}

/** Finds one page (by id) anywhere in the tree, across all teamspaces. */
export function findPage(teamspaces: TeamSpace[], id: string): PageNode | undefined {
  function search(pages: PageNode[]): PageNode | undefined {
    for (const page of pages) {
      if (page.id === id) return page
      if (page.children) {
        const found = search(page.children)
        if (found) return found
      }
    }
    return undefined
  }
  for (const space of teamspaces) {
    const found = search(space.pages)
    if (found) return found
  }
  return undefined
}

/** Immutably inserts a new page under parentId (or as a top-level page
 * of spaceId if parentId is null), for optimistic sidebar updates right
 * after a successful createPage() call. */
export function insertPage(
  teamspaces: TeamSpace[],
  spaceId: string,
  parentId: string | null,
  page: PageNode,
): TeamSpace[] {
  return teamspaces.map((space) => {
    if (space.id !== spaceId) return space
    if (parentId === null) {
      return { ...space, pages: [...space.pages, page] }
    }
    return {
      ...space,
      pages: mapPages(space.pages, parentId, (parent) => ({
        ...parent,
        children: [...(parent.children ?? []), page],
      })),
    }
  })
}

/** Where one page currently lives -- which space, and which page (or
 * none, for a top-level page) is its parent. Needed before a drag can
 * tell "reorder among the same siblings" (api.reorderPageSiblings)
 * apart from "actually moving somewhere else" (api.movePage): the drag
 * handler has a page id and a drop target, not this on its own. */
export function findPageLocation(
  teamspaces: TeamSpace[],
  id: string,
): { spaceId: string; parentId: string | null } | undefined {
  function search(pages: PageNode[], parentId: string | null): { parentId: string | null } | undefined {
    for (const page of pages) {
      if (page.id === id) return { parentId }
      if (page.children) {
        const found = search(page.children, page.id)
        if (found) return found
      }
    }
    return undefined
  }
  for (const space of teamspaces) {
    const found = search(space.pages, null)
    if (found) return { spaceId: space.id, ...found }
  }
  return undefined
}

/** The current sibling list at (spaceId, parentId) -- a page's own
 * top-level pages if parentId is null, otherwise a specific page's
 * children. Used to validate/build the id order a drag-driven
 * reorderPageSiblings call sends. */
export function siblingsOf(teamspaces: TeamSpace[], spaceId: string, parentId: string | null): PageNode[] {
  const space = teamspaces.find((t) => t.id === spaceId)
  if (!space) return []
  if (parentId === null) return space.pages
  const parent = findPage(teamspaces, parentId)
  return parent?.children ?? []
}

/** Immutably re-stamps the sibling order at (spaceId, parentId) to
 * match ids, mirroring reorderTeamspaces/reorderSidebarGroups one tree
 * level in. ids must be exactly that sibling list's current member set
 * (App.tsx's drag handlers are the only callers, and already build ids
 * that way). */
export function reorderPagesInTree(
  teamspaces: TeamSpace[],
  spaceId: string,
  parentId: string | null,
  ids: string[],
): TeamSpace[] {
  function reorderList(pages: PageNode[]): PageNode[] {
    const byId = new Map(pages.map((p) => [p.id, p]))
    return ids.map((id) => byId.get(id)!)
  }
  return teamspaces.map((space) => {
    if (space.id !== spaceId) return space
    if (parentId === null) {
      return { ...space, pages: reorderList(space.pages) }
    }
    return { ...space, pages: mapPages(space.pages, parentId, (p) => ({ ...p, children: reorderList(p.children ?? []) })) }
  })
}

/** Immutably removes one page (and its whole subtree, intact) from
 * wherever it currently is, returning both the updated tree and the
 * removed node -- the pairing a move needs (pull it out of its old
 * spot, then insertPage it under the new one) that plain removeSpace-
 * style filtering doesn't give back. */
export function extractPage(
  teamspaces: TeamSpace[],
  id: string,
): { teamspaces: TeamSpace[]; removed: PageNode | null } {
  let removed: PageNode | null = null
  function removeFromList(pages: PageNode[]): PageNode[] {
    const result: PageNode[] = []
    for (const page of pages) {
      if (page.id === id) {
        removed = page
        continue
      }
      result.push(page.children ? { ...page, children: removeFromList(page.children) } : page)
    }
    return result
  }
  const next = teamspaces.map((space) => ({ ...space, pages: removeFromList(space.pages) }))
  return { teamspaces: next, removed }
}

/** The chain of pages from a space's top level down to (not including)
 * pageId, for a search result's breadcrumb. undefined when the page
 * isn't in the loaded tree at all. */
export function findPageAncestors(
  teamspaces: TeamSpace[],
  pageId: string,
): { space: TeamSpace; ancestors: PageNode[] } | undefined {
  function walk(pages: PageNode[], trail: PageNode[]): PageNode[] | undefined {
    for (const page of pages) {
      if (page.id === pageId) return trail
      if (page.children) {
        const found = walk(page.children, [...trail, page])
        if (found) return found
      }
    }
    return undefined
  }
  for (const space of teamspaces) {
    const ancestors = walk(space.pages, [])
    if (ancestors) return { space, ancestors }
  }
  return undefined
}
