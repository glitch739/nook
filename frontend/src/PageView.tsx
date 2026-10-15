import { Suspense, useEffect, useState } from 'react'
import { getPage, type Page, type PageKind } from './api'
import { notifyError } from './toast'
import PageCover from './PageCover'
import PageMeta from './PageMeta'
import PageTitle from './PageTitle'
import RowPropertiesBar from './RowPropertiesBar'
import Backlinks from './Backlinks'
import { atLeast, findPage, type PageNode, type TeamSpace } from './sidebarData'
import { preloadable } from './preloadable'

// Lazy: both are only needed once a page is actually open (never on the
// login/auth screen, and DatabaseView specifically only for database-kind
// pages) -- the editor in particular pulls in Tiptap + all its extensions
// + Yjs, one of the heaviest parts of the whole bundle, so keeping it out
// of the initial chunk means an unauthenticated visitor's first paint
// doesn't pay for it at all.
const databaseView = preloadable(() => import('./DatabaseView'))
const editor = preloadable(() => import('./Editor'))
const collectionView = preloadable(() => import('./CollectionView'))
const DatabaseView = databaseView.Component
const Editor = editor.Component
const CollectionView = collectionView.Component

export function preloadPageViews() {
  editor.preload()
  databaseView.preload()
  collectionView.preload()
}

/** What a page view can do to the tree, the tabs and the server (App.tsx). */
export interface PageActions {
  openPage: (id: string) => void
  renamePage: (id: string, title: string) => Promise<void> | void
  changeIcon: (id: string, icon: string) => Promise<void> | void
  createPage: (spaceId: string, parentId: string | null, kind: PageKind) => Promise<void> | void
  archivePage: (id: string) => Promise<void> | void
  createRow: (spaceId: string, databasePageId: string, title: string) => Promise<PageNode | null>
  /** Opens a page in a new pane beside this one (not on phones). */
  openBeside?: (id: string) => void
}

// One open page: cover, meta, title and its body (document, database or
// collection). Each pane of the workspace (Workspace.tsx) has its own, so
// the same page can be open side by side; realtime editing already shares
// one connection per page (collabProvider.ts).
export default function PageView({
  pageId,
  teamspaces,
  meId,
  autoFocusTitle,
  onAutoFocused,
  actions,
}: {
  pageId: string
  teamspaces: TeamSpace[]
  meId: string
  autoFocusTitle: boolean
  onAutoFocused: () => void
  actions: PageActions
}) {
  // The full content (incl. content_json), loaded separately from the
  // tree/tabs, since those only carry the slim treeNodeResponse shape
  // (no content_json, see internal/httpapi/pages.go).
  const [page, setPage] = useState<Page | null>(null)
  useEffect(() => {
    let cancelled = false
    getPage(pageId)
      .then((p) => {
        if (!cancelled) setPage(p)
      })
      .catch((e) => notifyError('Could not load page', e))
    return () => {
      cancelled = true
    }
  }, [pageId])

  const node = findPage(teamspaces, pageId)
  // Viewers see pages read-only: the same editor+ check the server makes.
  const readOnly = !atLeast(teamspaces.find((t) => t.id === page?.space_id)?.role, 'editor')

  // page.id === pageId matters, not just page being non-null: getPage is
  // async, so right after switching tabs, page still holds the *previous*
  // page's content for one or more renders while the fetch for the new
  // pageId is in flight. Without this check, Editor would mount under the
  // new page's key with the old page's content_json as its initial value
  // -- and since Tiptap only reads `content` once at creation, any edit
  // made in that window would debounce-save the *old* page's content onto
  // the *new* page's id, permanently swapping their content. Showing the
  // loading hint until both actually match closes that window instead of
  // racing it.
  if (!page || page.id !== pageId) return <p className="hint">Loading…</p>

  return (
    // A database spans the page's full width, so its name and "Last
    // edited" line do too: on a wide screen they'd otherwise sit in the
    // centred text column, away from the table's toolbar.
    <div className={page.kind === 'database' ? 'page-view page-view-wide' : 'page-view'}>
      <PageCover key={pageId} pageId={page.id} readOnly={readOnly} />
      <PageMeta key={`meta-${pageId}`} page={page} meId={meId} />
      {/* The tree has the freshest name and icon (a rename in the
          sidebar, or one by someone else arriving live); a page
          the tree doesn't hold falls back to the loaded copy. */}
      <PageTitle
        key={`title-${pageId}`}
        title={node?.title ?? page.title}
        icon={node?.icon ?? page.icon}
        autoFocus={autoFocusTitle}
        readOnly={readOnly}
        onAutoFocused={onAutoFocused}
        onRename={(title) => void actions.renamePage(page.id, title)}
        onChangeIcon={(icon) => void actions.changeIcon(page.id, icon)}
      />
      <Suspense fallback={<p className="hint">Loading…</p>}>
        {page.kind === 'collection' ? (
          <CollectionView
            key={pageId}
            page={page}
            readOnly={readOnly}
            childrenKey={(node?.children ?? []).map((c) => `${c.id}:${c.title}:${c.icon}`).join('|')}
            onOpenPage={actions.openPage}
            onCreatePage={actions.createPage}
            onRenamePage={actions.renamePage}
            onArchivePage={actions.archivePage}
          />
        ) : page.kind === 'database' ? (
          <DatabaseView
            key={pageId}
            pageId={page.id}
            spaceId={page.space_id}
            readOnly={readOnly}
            teamspaces={teamspaces}
            onCreateRow={actions.createRow}
            onArchiveRow={actions.archivePage}
            onRenameRow={actions.renamePage}
            onChangeRowIcon={actions.changeIcon}
            onOpenPage={actions.openPage}
            onOpenBeside={actions.openBeside}
          />
        ) : (
          <>
            {/* A row (ADR 0004) is a plain 'document'-kind page
                nested under a 'database' one -- opened as its own
                full-page tab here rather than through the row
                detail peek, it otherwise showed no trace of the
                database's own columns, just the rich-text body. */}
            {page.parent_page_id && findPage(teamspaces, page.parent_page_id)?.kind === 'database' && (
              <RowPropertiesBar
                key={pageId}
                rowId={page.id}
                databasePageId={page.parent_page_id}
                readOnly={readOnly}
              />
            )}
            <Editor key={pageId} pageId={page.id} initialContent={page.content_json} />
          </>
        )}
      </Suspense>
      <Backlinks pageId={page.id} onOpenPage={actions.openPage} />
    </div>
  )
}
