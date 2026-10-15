import { Suspense, useEffect, useRef, useState } from 'react'
import {
  archiveGroup as archiveGroupApi,
  archivePage as archivePageApi,
  archiveSpace as archiveSpaceApi,
  createGroup as createGroupApi,
  createPage as createPageApi,
  createSpace as createSpaceApi,
  logout,
  me,
  verifyEmail,
  movePage as movePageApi,
  moveSpaceToGroup as moveSpaceToGroupApi,
  reorderGroups as reorderGroupsApi,
  reorderPageSiblings as reorderPageSiblingsApi,
  reorderSpaces as reorderSpacesApi,
  restoreGroup as restoreGroupApi,
  restorePage as restorePageApi,
  restoreSpace as restoreSpaceApi,
  updateGroup as updateGroupApi,
  updatePage as updatePageApi,
  updateSpace as updateSpaceApi,
  type ArchivedPage,
  type PageKind,
  type User,
} from './api'
import { urlTokens } from './urlTokens'
import { notifyError, showToast } from './toast'
import Toaster from './Toaster'
import { setCollabUser } from './collabUser'
import CommandPalette, { type PaletteAction } from './CommandPalette'
import { downloadUrl } from './download'
import Sidebar from './Sidebar'
import Workspace from './Workspace'
import { preloadPageViews } from './PageView'
import { setPageDirectory } from './pageDirectory'
import {
  activePageId,
  closeTabs as closeLayoutTabs,
  emptyLayout,
  joinAll,
  openTab,
  panes,
  parseLayout,
  prune,
  splitPane,
  type Layout,
} from './layout'
import {
  atLeast,
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
import './App.css'

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
    preloadPageViews()
    settings.preload()
    archive.preload()
  }
  if ('requestIdleCallback' in window) window.requestIdleCallback(run, { timeout: 3000 })
  else setTimeout(run, 1500)
}

// unreachable: the server didn't answer (offline, or a proxy error).
type Status = 'checking' | 'anonymous' | 'authenticated' | 'unreachable'

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
  // The open pages: panes with their own tabs, side by side or stacked
  // (layout.ts, drawn by Workspace.tsx). activeId is the page in the
  // focused pane, the one the sidebar highlights and commands act on.
  const [layout, setLayout] = useState<Layout>(emptyLayout)
  const activeId = activePageId(layout)
  // A page created a moment ago opens with its (empty) name focused.
  const [focusTitleId, setFocusTitleId] = useState<string | null>(null)
  const [sidebarCollapsed, setSidebarCollapsed] = useState(readStoredSidebarCollapsed)
  // Phone layout (mobile.css): the sidebar is an overlay drawer with
  // its own open/closed state -- closed by default, never persisted,
  // and independent of the desktop collapse preference above.
  const isMobile = useIsMobile()

  // Open panes and tabs survive a refresh: kept per user in this browser
  // and restored once the sidebar tree has loaded, keeping only pages
  // that still exist. The older single tab strip (nook-tabs-*) becomes
  // one pane.
  const layoutKey = user ? `nook-layout-${user.id}` : null
  const layoutRestored = useRef(false)
  useEffect(() => {
    if (!layoutKey || !user || !teamspacesLoaded || layoutRestored.current) return
    layoutRestored.current = true
    try {
      const raw = localStorage.getItem(layoutKey) ?? localStorage.getItem(`nook-tabs-${user.id}`)
      const saved = parseLayout(JSON.parse(raw ?? 'null'))
      if (saved) setLayout(prune(saved, (id) => findPage(teamspaces, id) !== null))
    } catch {
      // Unreadable or blocked storage: start with no tabs, as before.
    }
  }, [layoutKey, user, teamspacesLoaded, teamspaces])
  useEffect(() => {
    if (!layoutKey || !layoutRestored.current) return
    try {
      localStorage.setItem(layoutKey, JSON.stringify(layout))
    } catch {
      // Not persisted; tabs still work for this session.
    }
  }, [layoutKey, layout])
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
      // Cmd/Ctrl+\ splits the focused pane's page to the right (VS
      // Code's "Split Editor"); with Shift, downwards.
      if ((e.metaKey || e.ctrlKey) && !e.altKey && e.code === 'Backslash') {
        e.preventDefault()
        setLayout((l) => {
          const id = activePageId(l)
          return id ? splitPane(l, l.focused, e.shiftKey ? 'bottom' : 'right', id) : l
        })
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

  const [bootAttempt, setBootAttempt] = useState(0)
  // Unreachable at start: try again by itself as soon as the device is back online.
  useEffect(() => {
    if (status !== 'unreachable') return
    const retry = () => setBootAttempt((n) => n + 1)
    window.addEventListener('online', retry)
    return () => window.removeEventListener('online', retry)
  }, [status])

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
    Promise.all([me(), verified]).then(
      ([u, notice]) => {
        setUser(u)
        setStatus(u ? 'authenticated' : 'anonymous')
        if (notice) {
          if (u) showToast(notice)
          else setAuthNotice(notice)
        }
      },
      (e) => {
        // Before this, a failed check left "Loading…" on screen forever.
        console.error('session check failed', e)
        setStatus('unreachable')
      },
    )
  }, [bootAttempt])

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
      .catch((e) => notifyError('Could not load the sidebar', e))
      .finally(() => {
        if (!cancelled) setTeamspacesLoaded(true)
      })
    return () => {
      cancelled = true
    }
  }, [status])

  async function handleLogout() {
    await logout()
    setSettingsOpen(false)
    setUser(null)
    setStatus('anonymous')
    setTeamspaces([])
    setTeamspacesLoaded(false)
    setGroups([])
    layoutRestored.current = false
    setLayout(emptyLayout())
  }

  // Page links in the editor (PageMention.tsx) read names from the tree
  // and open pages through here.
  useEffect(() => setPageDirectory(teamspaces, openPage))

  function openPage(id: string) {
    // Picking a page is what the drawer was opened for -- get out of
    // the way so the page is visible.
    setMobileSidebarOpen(false)
    if (!findPage(teamspaces, id)) return
    setLayout((l) => openTab(l, id))
  }

  function closeTab(id: string) {
    closeTabs(new Set([id]))
  }

  // Closes these pages in every pane; a pane left empty goes away.
  function closeTabs(ids: Set<string>) {
    setLayout((l) => closeLayoutTabs(l, ids))
  }

  async function handleCreatePage(spaceId: string, parentId: string | null, kind: PageKind) {
    try {
      const page = await createPageApi(spaceId, { parentPageId: parentId ?? undefined, title: '', kind })
      const node: PageNode = { id: page.id, title: page.title, icon: page.icon, kind: page.kind }
      setTeamspaces((ts) => insertPage(ts, spaceId, parentId, node))
      setLayout((l) => openTab(l, page.id))
      setMobileSidebarOpen(false)
      setFocusTitleId(page.id)
    } catch (e) {
      notifyError('Could not create page', e)
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
      notifyError('Could not create row', e)
      return null
    }
  }

  async function handleCreateSpace(name: string, groupId: string) {
    try {
      const space = await createSpaceApi(name, 'private', groupId)
      setTeamspaces((ts) => insertSpace(ts, space))
    } catch (e) {
      notifyError('Could not create space', e)
    }
  }

  async function handleCreateGroup(name: string) {
    try {
      const group = await createGroupApi(name)
      setGroups((gs) => insertGroup(gs, group))
    } catch (e) {
      notifyError('Could not create group', e)
    }
  }

  // Rename and icon handlers skip unchanged values: every blur of a rename field
  // lands here, and each save is an API write plus a live refresh for everyone.
  async function renameGroup(id: string, name: string) {
    if (groups.find((g) => g.id === id)?.name === name) return
    setGroups((gs) => updateGroupName(gs, id, name))
    try {
      await updateGroupApi(id, { name })
    } catch (e) {
      notifyError('Could not save group name', e)
    }
  }

  async function changeGroupIcon(id: string, icon: string) {
    if (groups.find((g) => g.id === id)?.icon === icon) return
    setGroups((gs) => updateGroupIcon(gs, id, icon))
    try {
      await updateGroupApi(id, { icon })
    } catch (e) {
      notifyError('Could not save group icon', e)
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
    reorderGroupsApi(ids).catch((e) => notifyError('Could not save group order', e))
  }

  // Can fail for a reason the user must act on (the group still has
  // spaces in it, groups.Store.Archive's refusal); the toast says why.
  async function handleArchiveGroup(id: string) {
    try {
      await archiveGroupApi(id)
    } catch (e) {
      notifyError('Could not archive the group', e)
      return
    }
    const group = groups.find((g) => g.id === id)
    setGroups((gs) => removeGroup(gs, id))
    offerUndo(`Archived "${group?.name ?? 'group'}"`, () => restoreGroupApi(id))
  }

  async function renameSpace(id: string, name: string) {
    if (teamspaces.find((s) => s.id === id)?.name === name) return
    setTeamspaces((ts) => updateSpaceName(ts, id, name))
    try {
      await updateSpaceApi(id, { name })
    } catch (e) {
      notifyError('Could not save space name', e)
    }
  }

  async function changeSpaceIcon(id: string, icon: string) {
    if (teamspaces.find((s) => s.id === id)?.icon === icon) return
    setTeamspaces((ts) => updateSpaceIcon(ts, id, icon))
    try {
      await updateSpaceApi(id, { icon })
    } catch (e) {
      notifyError('Could not save space icon', e)
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
    reorderSpacesApi(ids).catch((e) => notifyError('Could not save space order', e))
  }

  async function archiveSpace(id: string) {
    const space = teamspaces.find((s) => s.id === id)
    try {
      await archiveSpaceApi(id)
    } catch (e) {
      notifyError('Could not archive space', e)
      return
    }
    setTeamspaces((ts) => removeSpace(ts, id))
    offerUndo(`Archived "${space?.name ?? 'space'}"`, () => restoreSpaceApi(id))
    if (!space) return
    // Every open tab that belonged to a page inside the archived space
    // is now pointing at something the sidebar can no longer reach --
    // close them the same way closeTab would, in one batch.
    closeTabs(new Set(collectPageIds(space.pages)))
  }

  async function renamePage(id: string, title: string) {
    if (findPage(teamspaces, id)?.title === title) return
    setTeamspaces((ts) => updatePageInTree(ts, id, (p) => ({ ...p, title })))
    try {
      await updatePageApi(id, { title })
    } catch (e) {
      notifyError('Could not save title', e)
    }
  }

  async function changeIcon(id: string, icon: string) {
    if (findPage(teamspaces, id)?.icon === icon) return
    setTeamspaces((ts) => updatePageInTree(ts, id, (p) => ({ ...p, icon })))
    try {
      await updatePageApi(id, { icon })
    } catch (e) {
      notifyError('Could not save icon', e)
    }
  }

  // Re-reads the whole sidebar (spaces, pages, groups) -- after a restore
  // from the archive, which can bring back anything anywhere.
  async function reloadSidebar() {
    try {
      const { teamspaces: ts, groups: gs } = await loadSidebar()
      setTeamspaces(ts)
      setGroups(gs)
      // Tabs follow the tree (they take titles and icons from it), and
      // pages that are gone (archived or moved out of reach by someone
      // else) close.
      setLayout((l) => prune(l, (id) => findPage(ts, id) !== null))
    } catch (e) {
      console.error('failed to reload the sidebar', e)
    }
  }

  async function openRestoredPage(p: ArchivedPage) {
    setArchiveOpen(false)
    await reloadSidebar()
    setMobileSidebarOpen(false)
    setLayout((l) => openTab(l, p.id))
  }

  // The command palette's actions, built from the current state.
  function paletteActions(): PaletteAction[] {
    const activeSpace = activeId ? teamspaces.find((t) => findPage([t], activeId)) : undefined
    // Only a space the user can add pages to.
    const canAdd = (t: TeamSpace | undefined) => atLeast(t?.role, 'editor')
    const targetSpace = canAdd(activeSpace) ? activeSpace : teamspaces.find(canAdd)
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
        {
          id: 'split-right',
          label: 'Split right',
          hint: isMacPlatform() ? '⌘\\' : 'Ctrl \\',
          icon: 'lucide:columns-2',
          run: () => setLayout((l) => splitPane(l, l.focused, 'right', activeId)),
        },
        {
          id: 'split-down',
          label: 'Split down',
          hint: isMacPlatform() ? '⇧⌘\\' : 'Ctrl ⇧ \\',
          icon: 'lucide:rows-2',
          run: () => setLayout((l) => splitPane(l, l.focused, 'bottom', activeId)),
        },
      )
    }
    const openPanes = panes(layout.root)
    if (openPanes.length > 1) {
      list.push(
        {
          id: 'next-pane',
          label: 'Focus next pane',
          icon: 'lucide:arrow-right-left',
          run: () =>
            setLayout((l) => {
              const all = panes(l.root)
              const next = all[(all.findIndex((p) => p.id === l.focused) + 1) % all.length]
              return { ...l, focused: next.id }
            }),
        },
        { id: 'join-panes', label: 'Join all panes', icon: 'lucide:square', run: () => setLayout(joinAll) },
      )
    }
    list.push(
      { id: 'export-all', label: 'Export everything (ZIP)', icon: 'lucide:package', run: () => downloadUrl('/api/export.zip') },
      { id: 'logout', label: 'Log out', icon: 'lucide:log-out', run: () => void handleLogout() },
    )
    return list
  }

  async function archivePage(id: string) {
    // Sub-pages are archived with it, so their tabs close too.
    const node = findPage(teamspaces, id)
    const ids = new Set(node ? collectPageIds([node]) : [id])
    try {
      await archivePageApi(id)
    } catch (e) {
      notifyError('Could not archive page', e)
      return
    }
    setTeamspaces((ts) =>
      ts.map((space) => ({ ...space, pages: removePageFromTree(space.pages, id) })),
    )
    closeTabs(ids)
    offerUndo(`Archived "${node?.title || 'Untitled'}"`, () => restorePageApi(id))
  }

  // "Archived · Undo": restores through the archive API, then reloads the
  // tree (a restore can bring back a whole subtree).
  function offerUndo(text: string, restore: () => Promise<unknown>) {
    showToast(text, {
      action: {
        label: 'Undo',
        run: () =>
          void restore().then(
            () => reloadSidebar(),
            (e) => notifyError('Could not undo', e),
          ),
      },
    })
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
  async function movePageTo(pageId: string, targetSpaceId: string, targetParentId: string | null): Promise<boolean> {
    const from = findPageLocation(teamspaces, pageId)
    if (!from) return false
    const { teamspaces: withoutPage, removed } = extractPage(teamspaces, pageId)
    if (!removed) return false

    setTeamspaces(insertPage(withoutPage, targetSpaceId, targetParentId, removed))
    try {
      await movePageApi(pageId, targetSpaceId, targetParentId)
    } catch (e) {
      setTeamspaces((ts) => {
        const { teamspaces: reverted } = extractPage(ts, pageId)
        return insertPage(reverted, from.spaceId, from.parentId, removed)
      })
      notifyError('Could not move the page', e)
      return false
    }
    return true
  }

  // Places draggedId at the exact dropped position among
  // (targetSpaceId, targetParentId)'s children, either immediately
  // before or after targetId -- moving it there first (movePageTo,
  // landing at the end) if it isn't already one of those siblings, then
  // always following up with a precise reorderPageSiblings call so
  // "before/after a specific sibling" is exact regardless of whether a
  // move happened first.
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
    if (!sameParent && !(await movePageTo(draggedId, targetSpaceId, targetParentId))) return

    // A move only appends draggedId, so the other siblings are the same
    // before and after it.
    const ids = siblingsOf(teamspaces, targetSpaceId, targetParentId)
      .map((p) => p.id)
      .filter((id) => id !== draggedId)
    const targetIdx = ids.indexOf(targetId)
    ids.splice(targetIdx === -1 ? ids.length : position === 'before' ? targetIdx : targetIdx + 1, 0, draggedId)
    setTeamspaces((ts) => reorderPagesInTree(ts, targetSpaceId, targetParentId, ids))
    reorderPageSiblingsApi(targetSpaceId, targetParentId, ids).catch((e) => notifyError('Could not save page order', e))
  }

  async function moveSpaceToGroup(spaceId: string, groupId: string) {
    const space = teamspaces.find((t) => t.id === spaceId)
    if (!space || space.groupId === groupId) return
    setTeamspaces((ts) => ts.map((t) => (t.id === spaceId ? { ...t, groupId } : t)))
    try {
      await moveSpaceToGroupApi(spaceId, groupId)
    } catch (e) {
      setTeamspaces((ts) => ts.map((t) => (t.id === spaceId ? { ...t, groupId: space.groupId } : t)))
      notifyError('Could not move the space', e)
    }
  }

  if (status === 'unreachable') {
    return (
      <div className="centered">
        <div className="card auth-card">
          <h1>Nook</h1>
          <p className="auth-subtitle">
            {navigator.onLine ? "Can't reach the server right now." : "You're offline."} Page text you edited but
            that wasn't saved yet stays on this device and is sent once Nook is reachable again.
          </p>
          {/* A full reload, not just a re-check: behind Cloudflare Access an
              expired login also lands here, and only a page load re-runs it. */}
          <button type="button" className="settings-primary" onClick={() => window.location.reload()}>
            Try again
          </button>
        </div>
      </div>
    )
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
        <Toaster />
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
          <Workspace
            layout={layout}
            setLayout={setLayout}
            teamspaces={teamspaces}
            teamspacesLoaded={teamspacesLoaded}
            meId={user.id}
            isMobile={isMobile}
            focusTitleId={focusTitleId}
            onTitleFocused={() => setFocusTitleId(null)}
            actions={{
              openPage,
              renamePage,
              changeIcon,
              createPage: handleCreatePage,
              archivePage,
              createRow: handleCreateRow,
            }}
            bar={{
              sidebarCollapsed: sidebarHidden,
              onToggleSidebar: () => (isMobile ? setMobileSidebarOpen((o) => !o) : setSidebarCollapsed((c) => !c)),
              userLabel: user.display_name,
              onOpenSettings: () => {
                setSettingsTab('profile')
                setSettingsOpen(true)
              },
              pendingApprovals: pendingCount,
              onOpenApprovals: () => {
                setSettingsTab('users')
                setSettingsOpen(true)
              },
              onLogout: handleLogout,
            }}
          />
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

