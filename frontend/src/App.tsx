import { Suspense, useEffect, useRef, useState } from 'react'
import {
  archiveGroup as archiveGroupApi,
  archivePage as archivePageApi,
  archiveSpace as archiveSpaceApi,
  createGroup as createGroupApi,
  createPage as createPageApi,
  createSpace as createSpaceApi,
  getPage,
  logout,
  me,
  verifyEmail,
  movePage as movePageApi,
  moveSpaceToGroup as moveSpaceToGroupApi,
  reorderGroups as reorderGroupsApi,
  reorderPageSiblings as reorderPageSiblingsApi,
  reorderSpaces as reorderSpacesApi,
  updateGroup as updateGroupApi,
  updatePage as updatePageApi,
  updateSpace as updateSpaceApi,
  type ArchivedPage,
  type Page,
  type PageKind,
  type User,
} from './api'
import { urlTokens } from './urlTokens'
import { setCollabUser } from './collabUser'
import PageCover from './PageCover'
import PageMeta from './PageMeta'
import CommandPalette, { type PaletteAction } from './CommandPalette'
import { downloadUrl } from './download'
import RowPropertiesBar from './RowPropertiesBar'
import Sidebar from './Sidebar'
import TabBar, { type OpenTab } from './TabBar'
import {
  collectPageIds,
  extractPage,
  findPage,
  findPageLocation,
  insertGroup,
  insertPage,
  insertSpace,
  loadSidebar,
  removeGroup,
  removeSpace,
  reorderPagesInTree,
  reorderSidebarGroups,
  reorderTeamspaces,
  siblingsOf,
  updateGroupIcon,
  updateGroupName,
  updatePage as updatePageInTree,
  updateSpaceIcon,
  updateSpaceName,
  type PageNode,
  type SidebarGroup,
  type TeamSpace,
} from './sidebarData'
import { THEMES, useTheme } from './theme'
import { useIsMobile } from './useIsMobile'
import { preloadable } from './preloadable'
import { usePendingApprovals } from './usePendingApprovals'
import { emitLiveEvent } from './liveEvents'
import PageTitle from './PageTitle'
import './App.css'

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

// Rarely opened screens, also kept out of the initial chunk: Settings
// (the biggest), the archive, the forced password change, and the
// sign-in screen (which a signed-in visitor never sees). Settings and
// the archive are preloaded with the page chunks below so they still
// open instantly.
const settings = preloadable(() => import('./Settings'))
const archive = preloadable(() => import('./Archive'))
const passwordForm = preloadable(
  (): Promise<{ default: typeof import('./Settings').PasswordForm }> =>
    import('./Settings').then((m) => ({ default: m.PasswordForm })),
)
const authScreen = preloadable(() => import('./AuthScreen'))
const SettingsModal = settings.Component
const ArchiveModal = archive.Component
const PasswordForm = passwordForm.Component
const AuthScreen = authScreen.Component

// Fetch the editor and database chunks in the background once the user
// is in, while the browser is idle, so opening the first page neither
// downloads them nor waits on a Suspense fallback (see preloadable.tsx).
function preloadPageChunks() {
  const run = () => {
    editor.preload()
    databaseView.preload()
    collectionView.preload()
    settings.preload()
    archive.preload()
  }
  if ('requestIdleCallback' in window) window.requestIdleCallback(run, { timeout: 3000 })
  else setTimeout(run, 1500)
}

type Status = 'checking' | 'anonymous' | 'authenticated'

const SIDEBAR_COLLAPSED_KEY = 'nook-sidebar-collapsed'

function readStoredSidebarCollapsed(): boolean {
  try {
    return localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === 'true'
  } catch {
    return false
  }
}

export default function App() {
  const [status, setStatus] = useState<Status>('checking')
  const [user, setUser] = useState<User | null>(null)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [archiveOpen, setArchiveOpen] = useState(false)
  const [paletteOpen, setPaletteOpen] = useState(false)
  // Which Settings tab to open on; the pending-approvals badge opens
  // straight onto Users.
  const [settingsTab, setSettingsTab] = useState<'profile' | 'users' | 'appearance'>('profile')
  // One-shot message for the logged-out screen (e.g. email verified).
  const [authNotice, setAuthNotice] = useState<string | null>(null)
  const { appearance, setAppearance } = useTheme(user?.id ?? null)

  // Sidebar/page-tree state lives here (not in Sidebar itself) because
  // both Sidebar and TabBar need to read and change it. teamspaces is
  // loaded from the real API (see sidebarData.loadTeamspaces) once
  // logged in -- there is no mock data anymore.
  const [teamspaces, setTeamspaces] = useState<TeamSpace[]>([])
  const [teamspacesLoaded, setTeamspacesLoaded] = useState(false)
  // Groups (migrations/0009_groups.sql) -- loaded alongside teamspaces
  // below; each TeamSpace's own groupId is what associates it with one
  // of these for rendering (see TeamSpace.groupId's own comment).
  const [groups, setGroups] = useState<SidebarGroup[]>([])
  const [openTabs, setOpenTabs] = useState<OpenTab[]>([])
  const [activeId, setActiveId] = useState<string | null>(null)
  // The full content (incl. content_json) of whichever page activeId
  // points to -- loaded separately from the tree/tabs, since those only
  // carry the slim treeNodeResponse shape (no content_json, see
  // internal/httpapi/pages.go).
  const [activePage, setActivePage] = useState<Page | null>(null)
  // A page created a moment ago opens with its (empty) name focused.
  const [focusTitleId, setFocusTitleId] = useState<string | null>(null)
  const [sidebarCollapsed, setSidebarCollapsed] = useState(readStoredSidebarCollapsed)
  // Phone layout (mobile.css): the sidebar is an overlay drawer with
  // its own open/closed state -- closed by default, never persisted,
  // and independent of the desktop collapse preference above.
  const isMobile = useIsMobile()
  const activeNode = activeId ? findPage(teamspaces, activeId) : null

  // Open tabs survive a refresh: kept per user in this browser and
  // restored once the sidebar tree has loaded, keeping only pages that
  // still exist (and taking their current titles/icons from the tree).
  const tabsKey = user ? `nook-tabs-${user.id}` : null
  const tabsRestored = useRef(false)
  useEffect(() => {
    if (!tabsKey || !teamspacesLoaded || tabsRestored.current) return
    tabsRestored.current = true
    try {
      const saved = JSON.parse(localStorage.getItem(tabsKey) ?? 'null') as { tabs: OpenTab[]; active: string | null } | null
      if (!saved || !Array.isArray(saved.tabs)) return
      const tabs = saved.tabs
        .map((t) => {
          const page = findPage(teamspaces, t.id)
          return page ? { id: t.id, title: page.title || 'Untitled', icon: page.icon } : null
        })
        .filter((t): t is OpenTab => t !== null)
      if (!tabs.length) return
      setOpenTabs(tabs)
      setActiveId(tabs.some((t) => t.id === saved.active) ? saved.active : tabs[0].id)
    } catch {
      // Unreadable or blocked storage: start with no tabs, as before.
    }
  }, [tabsKey, teamspacesLoaded, teamspaces])
  useEffect(() => {
    if (!tabsKey || !tabsRestored.current) return
    try {
      localStorage.setItem(tabsKey, JSON.stringify({ tabs: openTabs, active: activeId }))
    } catch {
      // Not persisted; tabs still work for this session.
    }
  }, [tabsKey, openTabs, activeId])
  const { count: pendingCount, refresh: refreshPending } = usePendingApprovals(user?.is_admin === true)

  useEffect(() => {
    if (status === 'authenticated') preloadPageChunks()
  }, [status])

  // Cmd/Ctrl+K toggles the command palette.
  useEffect(() => {
    if (status !== 'authenticated') return
    function onKey(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setPaletteOpen((o) => !o)
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [status])

  // Live sidebar: the server signals "tree changed" (any page, space or
  // group change by anyone) over Server-Sent Events, and the sidebar
  // re-fetches. Bursts are coalesced; EventSource reconnects by itself.
  const reloadRef = useRef<() => void>(() => {})
  reloadRef.current = () => void reloadSidebar()
  useEffect(() => {
    if (status !== 'authenticated' || typeof EventSource === 'undefined') return
    const es = new EventSource('/api/events')
    let timer: number | undefined
    es.addEventListener('tree', () => {
      window.clearTimeout(timer)
      timer = window.setTimeout(() => reloadRef.current(), 500)
      emitLiveEvent('tree')
    })
    es.addEventListener('data', () => emitLiveEvent('data'))
    // After a reconnect (laptop sleep, network blip) signals may have
    // been missed while the stream was down, so treat it as "both
    // changed". The first open is the initial load, nothing to catch up.
    let opened = false
    es.addEventListener('open', () => {
      if (!opened) {
        opened = true
        return
      }
      reloadRef.current()
      emitLiveEvent('tree')
      emitLiveEvent('data')
    })
    return () => {
      window.clearTimeout(timer)
      es.close()
    }
  }, [status])
  const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false)
  const sidebarHidden = isMobile ? !mobileSidebarOpen : sidebarCollapsed


  useEffect(() => {
    try {
      localStorage.setItem(SIDEBAR_COLLAPSED_KEY, String(sidebarCollapsed))
    } catch {
      // Not persisted this session; collapsing still works.
    }
  }, [sidebarCollapsed])

  // On load, check whether a session cookie from a previous visit is
  // still valid -- this is what proves sessions actually persist across
  // page reloads, not just within one login.
  useEffect(() => {
    if (user) setCollabUser(user.id, user.display_name)
  }, [user])

  useEffect(() => {
    // An email-verification link (?verify=token) works whether or not
    // the person is logged in on this browser.
    const verifyToken = urlTokens.verify
    const verified = verifyToken
      ? verifyEmail(verifyToken).then(
          () => 'Email address verified. ✓',
          () => 'This verification link is invalid or has expired.',
        )
      : Promise.resolve(null)
    Promise.all([me(), verified]).then(([u, notice]) => {
      setUser(u)
      setStatus(u ? 'authenticated' : 'anonymous')
      if (notice) {
        if (u) window.setTimeout(() => window.alert(notice), 0)
        else setAuthNotice(notice)
      }
    })
  }, [])

  // Load the groups, spaces and page trees once authenticated (one
  // request, GET /api/sidebar).
  useEffect(() => {
    if (status !== 'authenticated') return
    let cancelled = false
    loadSidebar()
      .then(({ teamspaces: ts, groups: gs }) => {
        if (cancelled) return
        setTeamspaces(ts)
        setGroups(gs)
      })
      .catch((e) => console.error('failed to load the sidebar', e))
      .finally(() => {
        if (!cancelled) setTeamspacesLoaded(true)
      })
    return () => {
      cancelled = true
    }
  }, [status])

  // Load the active page's full content whenever the selected page
  // changes. Flushes any debounced save for the *previous* page first
  // (the cleanup function runs before this effect re-runs, and on
  // unmount) so switching pages quickly never drops the last edit.
  useEffect(() => {
    if (!activeId) {
      setActivePage(null)
      return
    }
    let cancelled = false
    getPage(activeId)
      .then((p) => {
        if (!cancelled) setActivePage(p)
      })
      .catch((e) => console.error('failed to load page', e))
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeId])

  async function handleLogout() {
    await logout()
    setSettingsOpen(false)
    setUser(null)
    setStatus('anonymous')
    setTeamspaces([])
    setTeamspacesLoaded(false)
    setGroups([])
    tabsRestored.current = false
    setOpenTabs([])
    setActiveId(null)
    setActivePage(null)
  }

  function openPage(id: string) {
    // Picking a page is what the drawer was opened for -- get out of
    // the way so the page is visible.
    setMobileSidebarOpen(false)
    const page = findPage(teamspaces, id)
    if (!page) return
    setOpenTabs((tabs) =>
      tabs.some((t) => t.id === id) ? tabs : [...tabs, { id, title: page.title, icon: page.icon }],
    )
    setActiveId(id)
  }

  // Purely a local reorder of in-memory UI state -- open tabs aren't
  // persisted anywhere (see openTabs' own declaration above), so unlike
  // DatabaseView's column reorder this never needs a backend call.
  function reorderTabs(draggedId: string, targetId: string) {
    setOpenTabs((tabs) => {
      if (draggedId === targetId) return tabs
      const from = tabs.findIndex((t) => t.id === draggedId)
      const targetIdx = tabs.findIndex((t) => t.id === targetId)
      if (from === -1 || targetIdx === -1) return tabs
      const next = [...tabs]
      const [moved] = next.splice(from, 1)
      // Always inserting *before* the target (the previous version of
      // this) silently no-ops whenever the target is the dragged tab's
      // immediate right neighbor -- "insert before the tab right after
      // you" is exactly where you already are, so the single most
      // natural "nudge it one step right" gesture did nothing. Moving
      // left never had this problem (inserting before a target to your
      // left is always a real move). Insert *after* the target instead
      // whenever the drag came from its left, so dropping on the very
      // next tab actually swaps them, and dropping on the last tab can
      // finally land you in the last position too (previously
      // unreachable -- "before the last tab" tops out one short of it).
      let insertAt = next.findIndex((t) => t.id === targetId)
      insertAt = insertAt === -1 ? next.length : from < targetIdx ? insertAt + 1 : insertAt
      next.splice(insertAt, 0, moved)
      return next
    })
  }

  // Mouse drags in the tab bar (TabBar.tsx) know the exact slot the tab
  // was dropped into, including after the last tab.
  function moveTab(draggedId: string, toIndex: number) {
    setOpenTabs((tabs) => {
      const from = tabs.findIndex((t) => t.id === draggedId)
      if (from === -1) return tabs
      const next = [...tabs]
      const [moved] = next.splice(from, 1)
      next.splice(Math.max(0, Math.min(toIndex, next.length)), 0, moved)
      return next
    })
  }

  function closeTab(id: string) {
    setOpenTabs((tabs) => {
      const idx = tabs.findIndex((t) => t.id === id)
      const next = tabs.filter((t) => t.id !== id)
      if (id === activeId) {
        const fallback = next[idx] ?? next[idx - 1] ?? null
        setActiveId(fallback ? fallback.id : null)
      }
      return next
    })
  }

  async function handleCreatePage(spaceId: string, parentId: string | null, kind: PageKind) {
    try {
      const page = await createPageApi(spaceId, { parentPageId: parentId ?? undefined, title: '', kind })
      const node: PageNode = { id: page.id, title: page.title, icon: page.icon, kind: page.kind }
      setTeamspaces((ts) => insertPage(ts, spaceId, parentId, node))
      setOpenTabs((tabs) => [...tabs, { id: page.id, title: page.title || 'Untitled', icon: page.icon }])
      setMobileSidebarOpen(false)
      setFocusTitleId(page.id)
      setActiveId(page.id)
    } catch (e) {
      console.error('failed to create page', e)
    }
  }

  // Creates a row inside a database page (ADR 0004) -- deliberately not
  // handleCreatePage, which also opens a tab and navigates activeId
  // there: adding a row should keep the user looking at the table, not
  // jump them away to a blank document editor. Still needs to update
  // the sidebar tree, though -- a row is a real page and shows up
  // nested under its database there like any other child page.
  async function handleCreateRow(
    spaceId: string,
    databasePageId: string,
    title: string,
  ): Promise<PageNode | null> {
    try {
      const page = await createPageApi(spaceId, { parentPageId: databasePageId, title })
      const node: PageNode = { id: page.id, title: page.title, icon: page.icon, kind: page.kind }
      setTeamspaces((ts) => insertPage(ts, spaceId, databasePageId, node))
      return node
    } catch (e) {
      console.error('failed to create row', e)
      return null
    }
  }

  async function handleCreateSpace(name: string, groupId: string) {
    try {
      const space = await createSpaceApi(name, 'private', groupId)
      setTeamspaces((ts) => insertSpace(ts, space))
    } catch (e) {
      console.error('failed to create space', e)
    }
  }

  async function handleCreateGroup(name: string) {
    try {
      const group = await createGroupApi(name)
      setGroups((gs) => insertGroup(gs, group))
    } catch (e) {
      console.error('failed to create group', e)
    }
  }

  async function renameGroup(id: string, name: string) {
    setGroups((gs) => updateGroupName(gs, id, name))
    try {
      await updateGroupApi(id, { name })
    } catch (e) {
      console.error('failed to save group name', e)
    }
  }

  async function changeGroupIcon(id: string, icon: string) {
    setGroups((gs) => updateGroupIcon(gs, id, icon))
    try {
      await updateGroupApi(id, { icon })
    } catch (e) {
      console.error('failed to save group icon', e)
    }
  }

  // Same directional-insert reorder logic as reorderSpaces/reorderTabs.
  // Explicit before/after, same reasoning as reorderSpaces above.
  function reorderGroups(draggedId: string, targetId: string, position: 'before' | 'after') {
    if (draggedId === targetId) return
    const from = groups.findIndex((g) => g.id === draggedId)
    const targetIdx = groups.findIndex((g) => g.id === targetId)
    if (from === -1 || targetIdx === -1) return

    const ids = groups.map((g) => g.id)
    ids.splice(from, 1)
    const targetPos = ids.indexOf(targetId)
    ids.splice(position === 'before' ? targetPos : targetPos + 1, 0, draggedId)

    setGroups((gs) => reorderSidebarGroups(gs, ids))
    reorderGroupsApi(ids).catch((e) => console.error('failed to save group order', e))
  }

  // Unlike archiveSpace, this can fail for a reason the user actually
  // needs to see and act on (the group still has spaces in it,
  // groups.Store.Archive's own refusal on the backend) rather than a
  // background save that's fine to just log -- surfaced with alert()
  // since there's no toast/notification system in this app to route it
  // through instead.
  async function handleArchiveGroup(id: string) {
    try {
      await archiveGroupApi(id)
    } catch (e) {
      alert(e instanceof Error ? e.message : 'Failed to delete the group')
      return
    }
    setGroups((gs) => removeGroup(gs, id))
  }

  async function renameSpace(id: string, name: string) {
    setTeamspaces((ts) => updateSpaceName(ts, id, name))
    try {
      await updateSpaceApi(id, { name })
    } catch (e) {
      console.error('failed to save space name', e)
    }
  }

  async function changeSpaceIcon(id: string, icon: string) {
    setTeamspaces((ts) => updateSpaceIcon(ts, id, icon))
    try {
      await updateSpaceApi(id, { icon })
    } catch (e) {
      console.error('failed to save space icon', e)
    }
  }

  // Same directional-insert reorder logic as reorderTabs above (and
  // DatabaseView.tsx's handleColumnDrop) -- always inserting before the
  // drop target silently no-ops when the target is the dragged item's
  // immediate right neighbor, so a rightward drag inserts after it
  // instead. See reorderTabs' own comment for the full reasoning.
  // position ('before'/'after' the row the cursor was actually hovering,
  // read straight off which half of that row it was over) replaces an
  // earlier version that guessed the direction by comparing the dragged
  // item's and target's original indices -- that heuristic was right in
  // principle but left "drop onto the very last row" with no way to
  // mean "put me after it" when the target itself was already past the
  // dragged item, since both dropping and dragging past always reduced
  // to the same "before" outcome. Explicit position removes that
  // ambiguity entirely: wherever the cursor is, that's the answer.
  function reorderSpaces(draggedId: string, targetId: string, position: 'before' | 'after') {
    if (draggedId === targetId) return
    const from = teamspaces.findIndex((t) => t.id === draggedId)
    const targetIdx = teamspaces.findIndex((t) => t.id === targetId)
    if (from === -1 || targetIdx === -1) return
    // Reordering only ever applies within the dragged space's *own*
    // group -- Sidebar.tsx's handleSpaceDrop already routes a
    // cross-group drop to moveSpaceToGroup instead of calling this at
    // all, so this is a defense-in-depth guard, not the thing that
    // decides which action a drop means.
    if (teamspaces[from].groupId !== teamspaces[targetIdx].groupId) return

    const ids = teamspaces.map((t) => t.id)
    ids.splice(from, 1)
    const targetPos = ids.indexOf(targetId)
    ids.splice(position === 'before' ? targetPos : targetPos + 1, 0, draggedId)

    setTeamspaces((ts) => reorderTeamspaces(ts, ids))
    reorderSpacesApi(ids).catch((e) => console.error('failed to save space order', e))
  }

  async function archiveSpace(id: string) {
    const space = teamspaces.find((s) => s.id === id)
    try {
      await archiveSpaceApi(id)
    } catch (e) {
      console.error('failed to archive space', e)
      return
    }
    setTeamspaces((ts) => removeSpace(ts, id))
    if (!space) return
    // Every open tab that belonged to a page inside the archived space
    // is now pointing at something the sidebar can no longer reach --
    // close them the same way closeTab would, in one batch.
    const idsToClose = new Set(collectPageIds(space.pages))
    setOpenTabs((tabs) => {
      const next = tabs.filter((t) => !idsToClose.has(t.id))
      if (activeId && idsToClose.has(activeId)) {
        setActiveId(next[0]?.id ?? null)
      }
      return next
    })
  }

  async function renamePage(id: string, title: string) {
    setTeamspaces((ts) => updatePageInTree(ts, id, (p) => ({ ...p, title })))
    setOpenTabs((tabs) => tabs.map((t) => (t.id === id ? { ...t, title } : t)))
    if (activePage?.id === id) setActivePage({ ...activePage, title })
    try {
      await updatePageApi(id, { title })
    } catch (e) {
      console.error('failed to save title', e)
    }
  }

  async function changeIcon(id: string, icon: string) {
    setTeamspaces((ts) => updatePageInTree(ts, id, (p) => ({ ...p, icon })))
    setOpenTabs((tabs) => tabs.map((t) => (t.id === id ? { ...t, icon } : t)))
    if (activePage?.id === id) setActivePage({ ...activePage, icon })
    try {
      await updatePageApi(id, { icon })
    } catch (e) {
      console.error('failed to save icon', e)
    }
  }

  // Re-reads the whole sidebar (spaces, pages, groups) -- after a restore
  // from the archive, which can bring back anything anywhere.
  async function reloadSidebar() {
    try {
      const { teamspaces: ts, groups: gs } = await loadSidebar()
      setTeamspaces(ts)
      setGroups(gs)
      // Tabs follow the tree: current titles/icons, and pages that are
      // gone (archived or moved out of reach by someone else) close.
      setOpenTabs((tabs) =>
        tabs
          .map((t) => {
            const page = findPage(ts, t.id)
            return page ? { ...t, title: page.title || 'Untitled', icon: page.icon } : null
          })
          .filter((t): t is OpenTab => t !== null),
      )
      setActiveId((cur) => (cur && findPage(ts, cur) ? cur : null))
    } catch (e) {
      console.error('failed to reload the sidebar', e)
    }
  }

  async function openRestoredPage(p: ArchivedPage) {
    setArchiveOpen(false)
    await reloadSidebar()
    setMobileSidebarOpen(false)
    setOpenTabs((tabs) =>
      tabs.some((t) => t.id === p.id) ? tabs : [...tabs, { id: p.id, title: p.title || 'Untitled', icon: p.icon }],
    )
    setActiveId(p.id)
  }

  // The command palette's actions, built from the current state.
  function paletteActions(): PaletteAction[] {
    const activeSpace = activeId ? teamspaces.find((t) => findPage([t], activeId)) : undefined
    const targetSpace = activeSpace ?? teamspaces[0]
    const showSidebar = () => (isMobile ? setMobileSidebarOpen(true) : setSidebarCollapsed(false))
    const list: PaletteAction[] = []
    if (targetSpace) {
      list.push(
        {
          id: 'new-page',
          label: 'New page',
          hint: `in ${targetSpace.name}`,
          icon: 'lucide:file-plus',
          run: () => void handleCreatePage(targetSpace.id, null, 'document'),
        },
        {
          id: 'new-db',
          label: 'New database',
          hint: `in ${targetSpace.name}`,
          icon: 'lucide:table',
          run: () => void handleCreatePage(targetSpace.id, null, 'database'),
        },
      )
    }
    list.push(
      {
        id: 'search',
        label: 'Search in page contents',
        hint: isMacPlatform() ? '⇧⌘F' : 'Ctrl ⇧ F',
        icon: 'lucide:search',
        run: () => {
          showSidebar()
          window.setTimeout(() => window.dispatchEvent(new Event('nook:focus-search')), 60)
        },
      },
      // One entry per theme, like VS Code's "Color Theme" picker.
      ...[{ id: 'system', label: 'System', base: 'light' as const }, ...THEMES]
        .filter((t) => t.id !== appearance.theme)
        .map((t) => ({
          id: `theme-${t.id}`,
          label: `Theme: ${t.label}`,
          icon: t.id === 'system' ? 'lucide:monitor' : t.base === 'dark' ? 'lucide:moon' : 'lucide:sun',
          run: () => setAppearance({ theme: t.id }, { x: window.innerWidth / 2, y: window.innerHeight / 2 }),
        })),
      {
        id: 'sidebar',
        label: 'Toggle sidebar',
        icon: 'lucide:panel-left',
        run: () => (isMobile ? setMobileSidebarOpen((o) => !o) : setSidebarCollapsed((c) => !c)),
      },
      { id: 'archive', label: 'Open archive', icon: 'lucide:archive', run: () => setArchiveOpen(true) },
      {
        id: 'settings',
        label: 'Open settings',
        icon: 'lucide:settings',
        run: () => {
          setSettingsTab('profile')
          setSettingsOpen(true)
        },
      },
    )
    if (activeId) {
      list.push(
        {
          id: 'export-page',
          label: 'Export this page as Markdown',
          icon: 'lucide:file-down',
          run: () => downloadUrl(`/api/pages/${activeId}/export.md`),
        },
        { id: 'close-tab', label: 'Close tab', icon: 'lucide:x', run: () => closeTab(activeId) },
      )
    }
    list.push(
      { id: 'export-all', label: 'Export everything (ZIP)', icon: 'lucide:package', run: () => downloadUrl('/api/export.zip') },
      { id: 'logout', label: 'Log out', icon: 'lucide:log-out', run: () => void handleLogout() },
    )
    return list
  }

  async function archivePage(id: string) {
    try {
      await archivePageApi(id)
    } catch (e) {
      console.error('failed to archive page', e)
      return
    }
    setTeamspaces((ts) =>
      ts.map((space) => ({ ...space, pages: removePageFromTree(space.pages, id) })),
    )
    closeTab(id)
  }

  // Moves pageId to be the last child of (targetSpaceId,
  // targetParentId) -- reparenting, cross-space, and cross-group moves
  // are all this same call (see movePage's own comment on the backend
  // for why). Optimistic with a real rollback on failure (not just a
  // console.error) since a move is exactly the kind of drag gesture
  // that's easy to fumble into an invalid target (a circular parent, a
  // permission boundary) -- reusing extractPage/insertPage for both the
  // optimistic apply and, if the API call rejects it, putting the page
  // back exactly where findPageLocation said it started.
  async function movePageTo(pageId: string, targetSpaceId: string, targetParentId: string | null) {
    const from = findPageLocation(teamspaces, pageId)
    if (!from) return
    const { teamspaces: withoutPage, removed } = extractPage(teamspaces, pageId)
    if (!removed) return

    setTeamspaces(insertPage(withoutPage, targetSpaceId, targetParentId, removed))
    try {
      await movePageApi(pageId, targetSpaceId, targetParentId)
    } catch (e) {
      setTeamspaces((ts) => {
        const { teamspaces: reverted } = extractPage(ts, pageId)
        return insertPage(reverted, from.spaceId, from.parentId, removed)
      })
      alert(e instanceof Error ? e.message : 'Failed to move the page')
    }
  }

  // Places draggedId at the exact dropped position among
  // (targetSpaceId, targetParentId)'s children, either immediately
  // before or after targetId -- moving it there first (movePageTo,
  // landing at the end) if it isn't already one of those siblings, then
  // always following up with a precise reorderPageSiblings call so
  // "before/after a specific sibling" is exact regardless of whether a
  // move happened first. Functional setTeamspaces throughout so the
  // reorder step always computes from state *after* movePageTo's own
  // update actually landed, not a stale snapshot from before it.
  async function movePageToPosition(
    draggedId: string,
    targetSpaceId: string,
    targetParentId: string | null,
    targetId: string,
    position: 'before' | 'after',
  ) {
    if (draggedId === targetId) return
    const from = findPageLocation(teamspaces, draggedId)
    const sameParent = from && from.spaceId === targetSpaceId && from.parentId === targetParentId
    if (!sameParent) {
      await movePageTo(draggedId, targetSpaceId, targetParentId)
    }

    setTeamspaces((ts) => {
      const siblings = siblingsOf(ts, targetSpaceId, targetParentId)
      const ids = siblings.map((p) => p.id).filter((id) => id !== draggedId)
      const targetIdx = ids.indexOf(targetId)
      const insertAt = targetIdx === -1 ? ids.length : position === 'before' ? targetIdx : targetIdx + 1
      ids.splice(insertAt, 0, draggedId)
      reorderPageSiblingsApi(targetSpaceId, targetParentId, ids).catch((e) =>
        console.error('failed to save page order', e),
      )
      return reorderPagesInTree(ts, targetSpaceId, targetParentId, ids)
    })
  }

  async function moveSpaceToGroup(spaceId: string, groupId: string) {
    const space = teamspaces.find((t) => t.id === spaceId)
    if (!space || space.groupId === groupId) return
    setTeamspaces((ts) => ts.map((t) => (t.id === spaceId ? { ...t, groupId } : t)))
    try {
      await moveSpaceToGroupApi(spaceId, groupId)
    } catch (e) {
      setTeamspaces((ts) => ts.map((t) => (t.id === spaceId ? { ...t, groupId: space.groupId } : t)))
      alert(e instanceof Error ? e.message : 'Failed to move the space')
    }
  }

  if (status === 'checking') {
    return (
      <>
        <div className="centered">Loading…</div>
      </>
    )
  }

  if (status === 'authenticated' && user && user.must_change_password) {
    return (
      <>
        <div className="centered">
          <div className="card auth-card">
            <h1>Nook</h1>
            <p className="auth-subtitle">Choose your own password before continuing.</p>
            <Suspense fallback={null}>
              <PasswordForm forced onDone={() => setUser({ ...user, must_change_password: false })} />
            </Suspense>
            <button type="button" className="link-button" onClick={handleLogout}>
              Log out
            </button>
          </div>
        </div>
      </>
    )
  }

  if (status === 'authenticated' && user) {
    return (
      <div className="app-shell">
        {paletteOpen && (
          <CommandPalette
            teamspaces={teamspaces}
            groups={groups}
            actions={paletteActions()}
            onOpenPage={openPage}
            onClose={() => setPaletteOpen(false)}
          />
        )}
        <Suspense fallback={null}>
        {archiveOpen && (
          <ArchiveModal
            onClose={() => setArchiveOpen(false)}
            onRestoredPage={openRestoredPage}
            onRestored={reloadSidebar}
          />
        )}
        {settingsOpen && (
          <SettingsModal
            user={user}
            initialTab={settingsTab}
            appearance={appearance}
            onAppearanceChange={setAppearance}
            onUserChange={setUser}
            onClose={() => {
              setSettingsOpen(false)
              refreshPending()
            }}
          />
        )}
        </Suspense>
        <Sidebar
          teamspaces={teamspaces}
          groups={groups}
          activeId={activeId}
          onSelect={openPage}
          onRenamePage={renamePage}
          onChangeIcon={changeIcon}
          onCreatePage={handleCreatePage}
          onArchivePage={archivePage}
          onMovePageTo={movePageTo}
          onMovePageToPosition={movePageToPosition}
          onCreateSpace={handleCreateSpace}
          onRenameSpace={renameSpace}
          onChangeSpaceIcon={changeSpaceIcon}
          onArchiveSpace={archiveSpace}
          onReorderSpaces={reorderSpaces}
          onCreateGroup={handleCreateGroup}
          onRenameGroup={renameGroup}
          onChangeGroupIcon={changeGroupIcon}
          onReorderGroups={reorderGroups}
          onArchiveGroup={handleArchiveGroup}
          onMoveSpaceToGroup={moveSpaceToGroup}
          collapsed={sidebarHidden}
          onOpenArchive={() => {
            setMobileSidebarOpen(false)
            setArchiveOpen(true)
          }}
        />
        {isMobile && mobileSidebarOpen && (
          <button
            type="button"
            className="sidebar-backdrop"
            aria-label="Close sidebar"
            onClick={() => setMobileSidebarOpen(false)}
          />
        )}
        <div className="workspace">
          <TabBar
            tabs={openTabs}
            activeId={activeId}
            onSelectTab={setActiveId}
            onCloseTab={closeTab}
            onReorderTabs={reorderTabs}
            onMoveTab={moveTab}
            sidebarCollapsed={sidebarHidden}
            onToggleSidebar={() =>
              isMobile ? setMobileSidebarOpen((o) => !o) : setSidebarCollapsed((c) => !c)
            }
            compact={isMobile}
            userLabel={user.display_name}
            onOpenSettings={() => {
              setSettingsTab('profile')
              setSettingsOpen(true)
            }}
            pendingApprovals={pendingCount}
            onOpenApprovals={() => {
              setSettingsTab('users')
              setSettingsOpen(true)
            }}
            onLogout={handleLogout}
          />
          <div className="workspace-body">
            {/* activePage.id === activeId matters, not just activePage
                being non-null: getPage(activeId) above is async, so right
                after switching tabs, activePage still holds the *previous*
                page's content for one or more renders while the fetch for
                the new activeId is in flight. Without this check, Editor
                would mount under the new page's key with the old page's
                content_json as its initial value -- and since Tiptap only
                reads `content` once at creation, any edit made in that
                window would debounce-save the *old* page's content onto
                the *new* page's id, permanently swapping their content.
                Showing the loading hint until both actually match closes
                that window instead of racing it. */}
            {activeId && activePage && activePage.id === activeId ? (
              <>
                <PageCover key={activeId} pageId={activePage.id} />
                <PageMeta key={`meta-${activeId}`} page={activePage} meId={user.id} />
                {/* The tree has the freshest name and icon (a rename in the
                    sidebar, or one by someone else arriving live); a page
                    the tree doesn't hold falls back to the loaded copy. */}
                <PageTitle
                  key={`title-${activeId}`}
                  title={activeNode?.title ?? activePage.title}
                  icon={activeNode?.icon ?? activePage.icon}
                  autoFocus={focusTitleId === activeId}
                  onAutoFocused={() => setFocusTitleId(null)}
                  onRename={(title) => void renamePage(activePage.id, title)}
                  onChangeIcon={(icon) => void changeIcon(activePage.id, icon)}
                />
                <Suspense fallback={<p className="hint">Loading…</p>}>
                  {activePage.kind === 'collection' ? (
                    <CollectionView
                      key={activeId}
                      page={activePage}
                      childrenKey={(activeNode?.children ?? []).map((c) => `${c.id}:${c.title}:${c.icon}`).join('|')}
                      onOpenPage={openPage}
                      onCreatePage={handleCreatePage}
                      onRenamePage={renamePage}
                      onArchivePage={archivePage}
                    />
                  ) : activePage.kind === 'database' ? (
                    <DatabaseView
                      key={activeId}
                      pageId={activePage.id}
                      spaceId={activePage.space_id}
                      teamspaces={teamspaces}
                      onCreateRow={handleCreateRow}
                      onArchiveRow={archivePage}
                      onRenameRow={renamePage}
                      onChangeRowIcon={changeIcon}
                      onOpenPage={openPage}
                    />
                  ) : (
                    <>
                      {/* A row (ADR 0004) is a plain 'document'-kind page
                          nested under a 'database' one -- opened as its own
                          full-page tab here rather than through the row
                          detail peek, it otherwise showed no trace of the
                          database's own columns, just the rich-text body. */}
                      {activePage.parent_page_id &&
                        findPage(teamspaces, activePage.parent_page_id)?.kind === 'database' && (
                          <RowPropertiesBar
                            key={activeId}
                            rowId={activePage.id}
                            databasePageId={activePage.parent_page_id}
                          />
                        )}
                      <Editor key={activeId} pageId={activePage.id} initialContent={activePage.content_json} />
                    </>
                  )}
                </Suspense>
              </>
            ) : (
              <p className="hint">
                {teamspacesLoaded
                  ? 'Pick a page from the sidebar, or create a new one with the "+" button.'
                  : 'Loading…'}
              </p>
            )}
          </div>
        </div>
      </div>
    )
  }

  return (
    <>
      <Suspense fallback={<div className="centered">Loading…</div>}>
        <AuthScreen
          initialNotice={authNotice}
          onAuthenticated={(u) => {
            setUser(u)
            setStatus('authenticated')
          }}
        />
      </Suspense>
    </>
  )
}

function removePageFromTree(pages: PageNode[], id: string): PageNode[] {
  return pages
    .filter((p) => p.id !== id)
    .map((p) => (p.children ? { ...p, children: removePageFromTree(p.children, id) } : p))
}

function isMacPlatform(): boolean {
  return typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform)
}

