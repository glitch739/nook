// A page dragged into the sidebar from outside it: an item of a
// collection's list (CollectionView.tsx). The sidebar's own page drags
// keep their id in Sidebar's state; this module carries the id of one
// that started elsewhere, so the same drop targets and the same move
// (App.tsx's movePageTo / movePageToPosition) handle both. The id can't
// travel in dataTransfer alone: browsers hide its data until the drop,
// and the sidebar needs it during dragover to pick a drop position.
let externalPageId: string | null = null

export function startExternalPageDrag(pageId: string) {
  externalPageId = pageId
}

export function endExternalPageDrag() {
  externalPageId = null
}

export function externalPageDrag(): string | null {
  return externalPageId
}

// A page dragged from the sidebar, for the workspace's panes
// (Workspace.tsx): dropped on a pane it opens there, on a pane's edge in
// a new pane. Same reason as above: dataTransfer is unreadable until the
// drop, and the pane shows where it would land while hovering.
let sidebarPageId: string | null = null

export function startSidebarPageDrag(pageId: string) {
  sidebarPageId = pageId
}

export function endSidebarPageDrag() {
  sidebarPageId = null
}

/** The page being dragged from the sidebar or a collection, if any. */
export function draggedPage(): string | null {
  return sidebarPageId ?? externalPageId
}
