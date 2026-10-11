import { createContext, lazy, Suspense, useContext, useEffect, useRef, useState, type CSSProperties } from 'react'
import { flushSync } from 'react-dom'
import type { PageKind } from './api'
import PageIcon from './PageIcon'
import { downloadUrl } from './download'
import { DEFAULT_COLLECTION_ICON, DEFAULT_DATABASE_ICON, DEFAULT_ICON, iconColorAttr } from './pageIcon'
import Popover from './Popover'
import { atLeast, findPage, type PageNode, type SidebarGroup, type TeamSpace } from './sidebarData'
import SidebarSearch from './SidebarSearch'
import Collapse from './Collapse'
import { usePresence } from './usePresence'
import { startPointerDrag } from './pointerDrag'
import { endExternalPageDrag, externalPageDrag } from './pageDrag'
import './Sidebar.css'

// Touch screens (no hover): the grips are hidden there and too small to
// hit, so the whole row is the drag source instead, started by a
// long-press (main.tsx's touch drag-and-drop polyfill). With a mouse,
// only the grip drags, so clicks and double-click-rename on the rest of
// the row never fight a drag.
const TOUCH_DRAG = typeof window !== 'undefined' && window.matchMedia('(hover: none)').matches

// Sidebar show/hide slide; keep in sync with Sidebar.css and mobile.css.
const SIDEBAR_SLIDE_MS = 300

// Lazy: both are popovers only ever mounted on demand (icon/emoji data
// + the full lucide icon list for IconPicker, the members API round
// trip for GroupMembersPopover) -- deferring them keeps that weight out
// of the sidebar's own (always-mounted) chunk.
const IconPicker = lazy(() => import('./IconPicker'))
const GroupMembersPopover = lazy(() => import('./GroupMembers'))

const WIDTH_KEY = 'nook-sidebar-width'
const MIN_WIDTH = 200
const MAX_WIDTH = 440
const DEFAULT_WIDTH = 272
// A space/group section is its own whole-header-plus-page-list div (a
// big, easy-to-hit drop target, see Teamspace's own comment on why),
// but before/after during a reorder is decided against a small fixed
// band near its header specifically -- see handleSpaceDragOver's own
// comment for the full reasoning.
const ROW_DROP_THRESHOLD_PX = 36

function readStoredWidth(): number {
  try {
    const stored = Number(localStorage.getItem(WIDTH_KEY))
    if (stored >= MIN_WIDTH && stored <= MAX_WIDTH) return stored
  } catch {
    // localStorage unavailable -- fall through to the default.
  }
  return DEFAULT_WIDTH
}

interface SidebarProps {
  /** Opens the archive (restore archived pages, spaces, groups). */
  onOpenArchive: () => void
  teamspaces: TeamSpace[]
  groups: SidebarGroup[]
  activeId: string | null
  onSelect: (id: string) => void
  onRenamePage: (id: string, title: string) => void
  onChangeIcon: (id: string, icon: string) => void
  // parentId is null for a new top-level page in spaceId.
  onCreatePage: (spaceId: string, parentId: string | null, kind: PageKind) => void
  onArchivePage: (id: string) => void
  // Moves a page: dropped mid-row ("into") reparents it under that row,
  // landing last -- dropped in the top/bottom band ("before"/"after")
  // lands it at that exact sibling position, in the same parent or a
  // different one alike. See App.tsx's movePageTo/movePageToPosition for
  // the full reasoning (PageTreeItem's own drag-over cursor-ratio
  // computation is what picks which of the three this is).
  onMovePageTo: (draggedId: string, spaceId: string, parentId: string | null) => void
  onMovePageToPosition: (
    draggedId: string,
    spaceId: string,
    parentId: string | null,
    targetId: string,
    position: 'before' | 'after',
  ) => void
  // Creates a new private space inside groupId -- previously the only
  // way to get more than one top-level area was the one bootstrapAdmin
  // creates at account creation; now every group can have its own.
  onCreateSpace: (name: string, groupId: string) => void
  onRenameSpace: (spaceId: string, name: string) => void
  onChangeSpaceIcon: (spaceId: string, icon: string) => void
  onArchiveSpace: (spaceId: string) => void
  onReorderSpaces: (draggedId: string, targetId: string, position: 'before' | 'after') => void
  // Creates a new group (migrations/0009_groups.sql) -- the real
  // invite/tenant boundary; every space lives in exactly one.
  onCreateGroup: (name: string) => void
  onRenameGroup: (groupId: string, name: string) => void
  onChangeGroupIcon: (groupId: string, icon: string) => void
  onReorderGroups: (draggedId: string, targetId: string, position: 'before' | 'after') => void
  onArchiveGroup: (groupId: string) => void
  onMoveSpaceToGroup: (spaceId: string, groupId: string) => void
  // Collapse state/toggle now lives in App.tsx (TabBar owns the toggle
  // control, Notion-style) -- Sidebar only needs to know whether to
  // render at all.
  collapsed: boolean
}

// Small popover form for onCreateSpace/onCreateGroup, mirroring
// IconPicker's click-outside-to-commit pattern rather than a plain
// onBlur (onBlur would also fire while, say, selecting the input's own
// text). Shared by both -- creating a space and creating a group are
// both just "type a name, commit or cancel".
function NewSpaceForm({
  placeholder,
  onCreate,
  onClose,
}: {
  placeholder: string
  onCreate: (name: string) => void
  onClose: () => void
}) {
  const [value, setValue] = useState('')
  const ref = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    inputRef.current?.focus()
  }, [])

  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        commit()
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value])

  function commit() {
    const trimmed = value.trim()
    if (trimmed) onCreate(trimmed)
    onClose()
  }

  return (
    <div className="new-space-form" ref={ref}>
      <input
        ref={inputRef}
        className="new-space-input"
        placeholder={placeholder}
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') commit()
          if (e.key === 'Escape') onClose()
        }}
      />
    </div>
  )
}

// The "+" button next to a space or a page row -- opens a tiny menu to
// pick what kind of page to create (a plain document, or a database
// page, see ADR 0004) instead of always creating a document directly.
function CreatePageButton({
  spaceId,
  parentId,
  onCreatePage,
  label,
  allowCollection = true,
}: {
  spaceId: string
  parentId: string | null
  onCreatePage: (spaceId: string, parentId: string | null, kind: PageKind) => void
  label: string
  // False inside a collection or a database: a collection holds only
  // documents and databases (backend pages.CanHold).
  allowCollection?: boolean
}) {
  const [menuAnchor, setMenuAnchor] = useState<HTMLButtonElement | null>(null)
  const btnRef = useRef<HTMLButtonElement>(null)

  function choose(kind: PageKind) {
    onCreatePage(spaceId, parentId, kind)
    setMenuAnchor(null)
  }

  return (
    <>
      <button
        ref={btnRef}
        className={menuAnchor ? 'page-add-child open' : 'page-add-child'}
        onClick={(e) => {
          e.stopPropagation()
          setMenuAnchor(btnRef.current)
        }}
        title={label}
        aria-label={label}
      >
        +
      </button>
      {menuAnchor && (
        <Popover
          anchorEl={menuAnchor}
          width={190}
          maxHeight={allowCollection ? 136 : 96}
          onClose={() => setMenuAnchor(null)}
          className="create-menu"
        >
          <button type="button" className="create-menu-item" onClick={() => choose('document')}>
            <PageIcon icon={DEFAULT_ICON} className="page-icon" /> New page
          </button>
          <button type="button" className="create-menu-item" onClick={() => choose('database')}>
            <PageIcon icon={DEFAULT_DATABASE_ICON} className="page-icon" /> New database
          </button>
          {allowCollection && (
            <button type="button" className="create-menu-item" onClick={() => choose('collection')}>
              <PageIcon icon={DEFAULT_COLLECTION_ICON} className="page-icon" /> New collection
            </button>
          )}
        </Popover>
      )}
    </>
  )
}

// Single "⋮" hover-revealed menu button, replacing what used to be a
// separate always-hover-revealed icon per action (rename was already a
// double-click, but move/delete each had their own icon) -- Notion
// collapses a page row's actions the same way, one compact trigger
// instead of a row of icons that reads as an admin panel. Shared by
// page rows, teamspace headers and group headers alike.
// Move up / Move down for every row (page, space, group): the touch-
// friendly way to reorder, since HTML drag-and-drop is unreliable on
// phones and tablets. Provided once by Sidebar; rows only add their
// neighbors' ids.
interface SidebarMoves {
  movePage: (pageId: string, spaceId: string, parentId: string | null, targetId: string, position: 'before' | 'after') => void
  moveSpace: (spaceId: string, targetId: string, position: 'before' | 'after') => void
  moveGroup: (groupId: string, targetId: string, position: 'before' | 'after') => void
}
const SidebarMovesContext = createContext<SidebarMoves | null>(null)

function moveItems(
  prevId: string | undefined,
  nextId: string | undefined,
  move: (targetId: string, position: 'before' | 'after') => void,
) {
  const items: { label: string; onClick: () => void }[] = []
  if (prevId) items.push({ label: 'Move up', onClick: () => move(prevId, 'before') })
  if (nextId) items.push({ label: 'Move down', onClick: () => move(nextId, 'after') })
  return items
}

function RowMenu({ items }: { items: { label: string; onClick: (anchorEl: HTMLElement) => void; danger?: boolean }[] }) {
  const [anchor, setAnchor] = useState<HTMLButtonElement | null>(null)
  const btnRef = useRef<HTMLButtonElement>(null)

  return (
    <>
      <button
        ref={btnRef}
        className={anchor ? 'row-menu-btn open' : 'row-menu-btn'}
        onClick={(e) => {
          e.stopPropagation()
          setAnchor(btnRef.current)
        }}
        title="More actions"
        aria-label="More actions"
      >
        ⋮
      </button>
      {anchor && (
        <Popover anchorEl={anchor} width={190} maxHeight={300} onClose={() => setAnchor(null)} className="create-menu">
          {items.map((item) => (
            <button
              key={item.label}
              type="button"
              className={'create-menu-item' + (item.danger ? ' create-menu-item-danger' : '')}
              onClick={(e) => {
                e.stopPropagation()
                const el = btnRef.current!
                setAnchor(null)
                item.onClick(el)
              }}
            >
              {item.label}
            </button>
          ))}
        </Popover>
      )}
    </>
  )
}

// The lucide "grip-vertical" icon (six dots, two columns), inlined
// exactly like PageIcon.tsx does for a lucide page icon -- this is
// static UI chrome, not a user-selectable icon, so it skips that
// component's whole async-load/cache machinery and is just drawn once
// here directly.
function GripIcon() {
  return (
    <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="9" cy="5" r="1" />
      <circle cx="9" cy="12" r="1" />
      <circle cx="9" cy="19" r="1" />
      <circle cx="15" cy="5" r="1" />
      <circle cx="15" cy="12" r="1" />
      <circle cx="15" cy="19" r="1" />
    </svg>
  )
}

// The expand/collapse arrow of groups, spaces and page rows: a
// lucide chevron-right that turns to point down when open. Replaced
// the ▸/▾ text glyphs, which were too small to see.
function ChevronIcon({ open }: { open: boolean }) {
  return (
    <svg
      className={open ? 'chevron-icon chevron-icon-open' : 'chevron-icon'}
      viewBox="0 0 24 24"
      width="14"
      height="14"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="m9 18 6-6-6-6" />
    </svg>
  )
}

// A whole teamspace section: header (icon, name, collapse chevron,
// per-space "+"/delete/drag-handle) plus its own page tree, collapsible
// exactly like PageTreeItem's own children -- same reasoning: a real
// component (not inlined into the teamspaces.map() below) because it
// needs its own hooks (expanded/renaming state, the icon-picker anchor
// ref), which only works from a proper component, not a bare .map()
// callback.
//
// The whole section (header *and* its page tree), not just the header
// strip, is the drop target now -- dragover/drop live here, one level
// up from where they used to sit on just the header div. A teamspace
// with several pages underneath it used to have almost no valid drop
// target along most of its own height, since only that thin header
// strip counted; dragging past it required threading the cursor
// through a narrow band, which is what made reordering feel like it
// only worked in some directions/positions. Collapsing a section (the
// new chevron) also just shrinks that whole target down to the header
// alone, which is the easiest way to make a long section as easy to
// drag past as a short one.
function Teamspace({
  space,
  activeId,
  onSelect,
  onRenamePage,
  onChangeIcon,
  onCreatePage,
  onArchivePage,
  onRenameSpace,
  onChangeSpaceIcon,
  onArchiveSpace,
  isDragging,
  dropPosition,
  onDragStart,
  onDragOver,
  onDragLeave,
  onDrop,
  onDragEnd,
  onHeaderDragOver,
  onHeaderDrop,
  pageDragProps,
  onOpenMoveTo,
  prevId,
  nextId,
}: {
  prevId?: string
  nextId?: string
  space: TeamSpace
  activeId: string | null
  onSelect: (id: string) => void
  onRenamePage: (id: string, title: string) => void
  onChangeIcon: (id: string, icon: string) => void
  onCreatePage: (spaceId: string, parentId: string | null, kind: PageKind) => void
  onArchivePage: (id: string) => void
  onRenameSpace: (spaceId: string, name: string) => void
  onChangeSpaceIcon: (spaceId: string, icon: string) => void
  onArchiveSpace: (spaceId: string) => void
  isDragging: boolean
  dropPosition: 'before' | 'after' | null
  onDragStart: (e: React.DragEvent) => void
  onDragOver: (e: React.DragEvent) => void
  onDragLeave: () => void
  onDrop: (e: React.DragEvent) => void
  onDragEnd: () => void
  // A page dropped directly on this header becomes a top-level page of
  // this space -- a separate drop zone from the .teamspace div's own
  // onDragOver/onDrop above (those reorder *spaces*; this reparents a
  // *page*). The handlers themselves no-op unless a page is actually
  // being dragged, so both can sit on overlapping elements safely.
  onHeaderDragOver: (e: React.DragEvent) => void
  onHeaderDrop: (e: React.DragEvent) => void
  pageDragProps: (pageId: string, spaceId: string, parentId: string | null) => PageDragProps
  onOpenMoveTo: (pageId: string, anchorEl: HTMLElement) => void
}) {
  const moves = useContext(SidebarMovesContext)
  // Viewers get no edit controls: the server would refuse them anyway.
  const canEdit = atLeast(space.role, 'editor')
  const [expanded, setExpanded] = useState(true)
  // See PageTreeItem's identical pattern for why this is a ref-holding
  // piece of state rather than reading iconRef.current during render.
  const [pickerAnchor, setPickerAnchor] = useState<HTMLSpanElement | null>(null)
  const iconRef = useRef<HTMLSpanElement>(null)
  // Same double-click-to-rename pattern as a page's title -- previously
  // the only way to fix a space's name was deleting and recreating it,
  // losing every page inside.
  const [renaming, setRenaming] = useState(false)
  const [draftName, setDraftName] = useState(space.name)
  const nameInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (renaming) {
      nameInputRef.current?.focus()
      nameInputRef.current?.select()
    }
  }, [renaming])

  function commitRename() {
    const trimmed = draftName.trim()
    onRenameSpace(space.id, trimmed || space.name)
    setRenaming(false)
  }

  const className = [
    'teamspace',
    // A collapsed section is just its one-line header -- the roomier
    // gap below an *expanded* one exists to separate its page list from
    // the next section, and looks like stray dead space once there's
    // no page list there to separate from (several collapsed sections
    // in a row read as oddly far apart otherwise).
    expanded ? 'teamspace-expanded' : 'teamspace-collapsed',
    isDragging && 'teamspace-dragging',
    dropPosition && `teamspace-drop-${dropPosition}`,
  ]
    .filter(Boolean)
    .join(' ')

  return (
    <div
      className={className}
      data-section-id={space.id}
      onDragOver={onDragOver}
      onDragLeave={onDragLeave}
      onDrop={onDrop}
    >
      <div
        className="teamspace-header"
        onDragOver={onHeaderDragOver}
        onDrop={onHeaderDrop}
        draggable={TOUCH_DRAG || undefined}
        onDragStart={TOUCH_DRAG ? onDragStart : undefined}
        onDragEnd={TOUCH_DRAG ? onDragEnd : undefined}
      >
        <button
          className="chevron"
          onClick={(e) => {
            e.stopPropagation()
            setExpanded((v) => !v)
          }}
          aria-label={expanded ? 'Collapse' : 'Expand'}
        >
          <ChevronIcon open={expanded} />
        </button>
        {/* Hover-revealed drag handle, same reasoning as
            DatabaseView.tsx's column header grip -- only this small
            span is the actual HTML5 drag source, so double-click-to-
            rename and the icon/menu clicks right next to it keep
            working normally instead of fighting a drag-start on the
            whole row. */}
        <span
          className="teamspace-grip"
          draggable={!TOUCH_DRAG}
          title="Drag to reorder"
          onDragStart={TOUCH_DRAG ? undefined : onDragStart}
          onDragEnd={TOUCH_DRAG ? undefined : onDragEnd}
        >
          <GripIcon />
        </span>
        <span
          ref={iconRef}
          className={canEdit ? 'page-icon-editable' : undefined}
          onClick={(e) => {
            e.stopPropagation()
            if (canEdit) setPickerAnchor(iconRef.current)
          }}
        >
          <PageIcon icon={space.icon} className="page-icon" />
        </span>
        {pickerAnchor && (
          <Suspense fallback={null}>
            <IconPicker
              icon={space.icon}
              anchorEl={pickerAnchor}
              onChange={(icon) => onChangeSpaceIcon(space.id, icon)}
              onClose={() => setPickerAnchor(null)}
            />
          </Suspense>
        )}
        {renaming ? (
          <input
            ref={nameInputRef}
            className="teamspace-name-input"
            value={draftName}
            onChange={(e) => setDraftName(e.target.value)}
            onBlur={commitRename}
            onKeyDown={(e) => {
              if (e.key === 'Enter') commitRename()
              if (e.key === 'Escape') {
                setDraftName(space.name)
                setRenaming(false)
              }
            }}
          />
        ) : (
          <span
            className="teamspace-name"
            onDoubleClick={() => {
              if (!canEdit) return
              setDraftName(space.name)
              setRenaming(true)
            }}
            title={canEdit ? 'Double-click to rename' : undefined}
          >
            {space.name}
          </span>
        )}
        {canEdit && (
          <CreatePageButton
            spaceId={space.id}
            parentId={null}
            onCreatePage={onCreatePage}
            label="Create a new page/database"
          />
        )}
        <RowMenu
          items={[
            ...(canEdit
              ? [
                  {
                    label: 'Rename',
                    onClick: () => {
                      setDraftName(space.name)
                      setRenaming(true)
                    },
                  },
                ]
              : []),
            // Space order is the user's own, so even a viewer can move it.
            ...(moves ? moveItems(prevId, nextId, (target, pos) => moves.moveSpace(space.id, target, pos)) : []),
            ...(atLeast(space.role, 'owner')
              ? [
                  {
                    label: 'Archive',
                    danger: true,
                    onClick: () => {
                      if (window.confirm(`Archive "${space.name}"? Its pages go with it. You can restore it from the Archive.`)) {
                        onArchiveSpace(space.id)
                      }
                    },
                  },
                ]
              : []),
          ]}
        />
      </div>
      <Collapse open={expanded}>
        <PageTree
          pages={space.pages}
          depth={0}
          spaceId={space.id}
          parentId={null}
          canEdit={canEdit}
          activeId={activeId}
          onSelect={onSelect}
          onRenamePage={onRenamePage}
          onChangeIcon={onChangeIcon}
          onCreatePage={onCreatePage}
          onArchivePage={onArchivePage}
          pageDragProps={pageDragProps}
          onOpenMoveTo={onOpenMoveTo}
        />
      </Collapse>
    </div>
  )
}

// A whole group section: header (icon, name, collapse chevron,
// drag-handle, member-management button, add-space, "..." menu) plus
// every teamspace filed under it (TeamSpace.groupId) -- mirrors
// Teamspace's own shape closely (same collapse/drag/rename
// patterns), one tier up. Delete requires the group to be empty first
// (groups.Store.Archive refuses otherwise) -- unlike archiving a space,
// archiving a whole group could strand every other member's access to
// everything inside it, so it can't just cascade the way a space's own
// pages ride along with it; the confirm() below explains that instead.
function GroupSection({
  group,
  expanded,
  onToggleExpanded,
  teamspaces,
  activeId,
  onSelect,
  onRenamePage,
  onChangeIcon,
  onCreatePage,
  onArchivePage,
  onCreateSpace,
  onRenameSpace,
  onChangeSpaceIcon,
  onArchiveSpace,
  onRenameGroup,
  onChangeGroupIcon,
  onArchiveGroup,
  spaceDragProps,
  isDragging,
  dropPosition,
  isSpaceDropTarget,
  onDragStart,
  onDragOver,
  onDragLeave,
  onDrop,
  onDragEnd,
  teamspaceHeaderDragOver,
  teamspaceHeaderDrop,
  pageDragProps,
  onOpenMoveTo,
  prevId,
  nextId,
}: {
  prevId?: string
  nextId?: string
  group: SidebarGroup
  teamspaces: TeamSpace[]
  activeId: string | null
  onSelect: (id: string) => void
  onRenamePage: (id: string, title: string) => void
  onChangeIcon: (id: string, icon: string) => void
  onCreatePage: (spaceId: string, parentId: string | null, kind: PageKind) => void
  onArchivePage: (id: string) => void
  onCreateSpace: (name: string, groupId: string) => void
  onRenameSpace: (spaceId: string, name: string) => void
  onChangeSpaceIcon: (spaceId: string, icon: string) => void
  onArchiveSpace: (spaceId: string) => void
  onRenameGroup: (groupId: string, name: string) => void
  onChangeGroupIcon: (groupId: string, icon: string) => void
  onArchiveGroup: (groupId: string) => void
  spaceDragProps: (spaceId: string) => {
    isDragging: boolean
    dropPosition: 'before' | 'after' | null
    onDragStart: (e: React.DragEvent) => void
    onDragOver: (e: React.DragEvent) => void
    onDragLeave: () => void
    onDrop: (e: React.DragEvent) => void
    onDragEnd: () => void
  }
  isDragging: boolean
  dropPosition: 'before' | 'after' | null
  // True while a *space* (not a group) being dragged is hovering this
  // group as a valid cross-group destination -- separate from
  // dropPosition above, which is only ever set for group-vs-group
  // reordering.
  isSpaceDropTarget: boolean
  // Open/closed state lives in Sidebar so the collapse/expand-all
  // button next to the Groups label can see and set every group.
  expanded: boolean
  onToggleExpanded: () => void
  onDragStart: (e: React.DragEvent) => void
  onDragOver: (e: React.DragEvent) => void
  onDragLeave: () => void
  onDrop: (e: React.DragEvent) => void
  onDragEnd: () => void
  // Forwarded straight through to every Teamspace/PageTree below --
  // GroupSection itself has no page-drag state of its own, that all
  // lives up in Sidebar (see its own handlePageDrag*/pageDragProps).
  teamspaceHeaderDragOver: (e: React.DragEvent) => void
  teamspaceHeaderDrop: (spaceId: string) => (e: React.DragEvent) => void
  pageDragProps: (pageId: string, spaceId: string, parentId: string | null) => PageDragProps
  onOpenMoveTo: (pageId: string, anchorEl: HTMLElement) => void
}) {
  const moves = useContext(SidebarMovesContext)
  const [pickerAnchor, setPickerAnchor] = useState<HTMLSpanElement | null>(null)
  const iconRef = useRef<HTMLSpanElement>(null)
  const [renaming, setRenaming] = useState(false)
  const [draftName, setDraftName] = useState(group.name)
  const nameInputRef = useRef<HTMLInputElement>(null)
  const [membersAnchor, setMembersAnchor] = useState<HTMLElement | null>(null)
  const [creatingSpace, setCreatingSpace] = useState(false)
  // Same roles the server checks: admin to rename, editor to add a space.
  const canAdmin = atLeast(group.role, 'admin')

  useEffect(() => {
    if (renaming) {
      nameInputRef.current?.focus()
      nameInputRef.current?.select()
    }
  }, [renaming])

  function commitRename() {
    const trimmed = draftName.trim()
    onRenameGroup(group.id, trimmed || group.name)
    setRenaming(false)
  }

  const className = [
    'sidebar-group',
    expanded ? 'sidebar-group-expanded' : 'sidebar-group-collapsed',
    isDragging && 'sidebar-group-dragging',
    dropPosition && `sidebar-group-drop-${dropPosition}`,
    isSpaceDropTarget && 'sidebar-group-drop-space',
  ]
    .filter(Boolean)
    .join(' ')

  return (
    <div
      className={className}
      data-section-id={group.id}
      onDragOver={onDragOver}
      onDragLeave={onDragLeave}
      onDrop={onDrop}
    >
      <div
        className="sidebar-group-header"
        draggable={TOUCH_DRAG || undefined}
        onDragStart={TOUCH_DRAG ? onDragStart : undefined}
        onDragEnd={TOUCH_DRAG ? onDragEnd : undefined}
      >
        <button
          className="chevron"
          onClick={(e) => {
            e.stopPropagation()
            onToggleExpanded()
          }}
          aria-label={expanded ? 'Collapse' : 'Expand'}
        >
          <ChevronIcon open={expanded} />
        </button>
        <span
          className="teamspace-grip"
          draggable={!TOUCH_DRAG}
          title="Drag to reorder"
          onDragStart={TOUCH_DRAG ? undefined : onDragStart}
          onDragEnd={TOUCH_DRAG ? undefined : onDragEnd}
        >
          <GripIcon />
        </span>
        <span
          ref={iconRef}
          className={canAdmin ? 'page-icon-editable group-icon-badge' : 'group-icon-badge'}
          data-icon-color={iconColorAttr(group.icon)}
          onClick={(e) => {
            e.stopPropagation()
            if (canAdmin) setPickerAnchor(iconRef.current)
          }}
        >
          <PageIcon icon={group.icon} className="page-icon" />
        </span>
        {pickerAnchor && (
          <Suspense fallback={null}>
            <IconPicker
              icon={group.icon}
              anchorEl={pickerAnchor}
              onChange={(icon) => onChangeGroupIcon(group.id, icon)}
              onClose={() => setPickerAnchor(null)}
            />
          </Suspense>
        )}
        {renaming ? (
          <input
            ref={nameInputRef}
            className="teamspace-name-input"
            value={draftName}
            onChange={(e) => setDraftName(e.target.value)}
            onBlur={commitRename}
            onKeyDown={(e) => {
              if (e.key === 'Enter') commitRename()
              if (e.key === 'Escape') {
                setDraftName(group.name)
                setRenaming(false)
              }
            }}
          />
        ) : (
          <span
            className="sidebar-group-name"
            onDoubleClick={() => {
              if (!canAdmin) return
              setDraftName(group.name)
              setRenaming(true)
            }}
            title={canAdmin ? 'Double-click to rename' : undefined}
          >
            {group.name}
          </span>
        )}
        {membersAnchor && (
          <Suspense fallback={null}>
            <GroupMembersPopover groupId={group.id} anchorEl={membersAnchor} onClose={() => setMembersAnchor(null)} />
          </Suspense>
        )}
        {atLeast(group.role, 'editor') && (
          <button
            className="page-add-child"
            onClick={(e) => {
              e.stopPropagation()
              setCreatingSpace(true)
            }}
            title="Create a new space"
            aria-label="Create a new space"
          >
            +
          </button>
        )}
        <RowMenu
          items={[
            {
              label: 'Group members',
              onClick: (anchorEl) => setMembersAnchor(anchorEl),
            },
            ...(canAdmin
              ? [
                  {
                    label: 'Rename',
                    onClick: () => {
                      setDraftName(group.name)
                      setRenaming(true)
                    },
                  },
                ]
              : []),
            ...(moves ? moveItems(prevId, nextId, (target, pos) => moves.moveGroup(group.id, target, pos)) : []),
            ...(atLeast(group.role, 'owner')
              ? [
                  {
                    label: 'Archive',
                    danger: true,
                    onClick: () => {
                      if (
                        window.confirm(
                          `Archive "${group.name}"? This only works when the group has no spaces left in it -- archive or move them first. You can restore it from the Archive.`,
                        )
                      ) {
                        onArchiveGroup(group.id)
                      }
                    },
                  },
                ]
              : []),
          ]}
        />
      </div>
      <Collapse open={expanded}>
        <div className="sidebar-group-body">
          {teamspaces.map((space, i) => (
            <Teamspace
              key={space.id}
              prevId={teamspaces[i - 1]?.id}
              nextId={teamspaces[i + 1]?.id}
              space={space}
              activeId={activeId}
              onSelect={onSelect}
              onRenamePage={onRenamePage}
              onChangeIcon={onChangeIcon}
              onCreatePage={onCreatePage}
              onArchivePage={onArchivePage}
              onRenameSpace={onRenameSpace}
              onChangeSpaceIcon={onChangeSpaceIcon}
              onArchiveSpace={onArchiveSpace}
              {...spaceDragProps(space.id)}
              onHeaderDragOver={teamspaceHeaderDragOver}
              onHeaderDrop={teamspaceHeaderDrop(space.id)}
              pageDragProps={pageDragProps}
              onOpenMoveTo={onOpenMoveTo}
            />
          ))}
          {creatingSpace && (
            <NewSpaceForm
              placeholder="Space name…"
              onCreate={(name) => onCreateSpace(name, group.id)}
              onClose={() => setCreatingSpace(false)}
            />
          )}
        </div>
      </Collapse>
    </div>
  )
}

// One page's own drag props, mirroring spaceDragProps/columnDragProps
// elsewhere -- computed once per row by Sidebar's pageDragProps(id,
// spaceId, parentId) and spread onto the row div.
interface PageDragProps {
  isDragging: boolean
  dropPosition: 'before' | 'after' | 'into' | null
  onDragStart: (e: React.DragEvent) => void
  onDragOver: (e: React.DragEvent) => void
  onDragLeave: () => void
  onDrop: (e: React.DragEvent) => void
  onDragEnd: () => void
}

interface PageTreeProps {
  pages: PageNode[]
  depth: number
  spaceId: string
  // This level's own parent -- null for a space's top-level pages, a
  // page id one level in. Needed (not derivable from pages alone) so a
  // reorder/move call knows which sibling group it's actually scoped to.
  parentId: string | null
  // Editor or better in this space: viewers get no edit controls.
  canEdit: boolean
  activeId: string | null
  onSelect: (id: string) => void
  onRenamePage: (id: string, title: string) => void
  onChangeIcon: (id: string, icon: string) => void
  onCreatePage: (spaceId: string, parentId: string | null, kind: PageKind) => void
  onArchivePage: (id: string) => void
  pageDragProps: (pageId: string, spaceId: string, parentId: string | null) => PageDragProps
  // Anchors the "Move to..." popover to the button that opened it
  // (Popover.tsx needs a live element to position off of) -- the
  // non-drag fallback for moving a page onto a different space
  // entirely, without needing to actually drag it there.
  onOpenMoveTo: (pageId: string, anchorEl: HTMLElement) => void
}

function PageTree(props: PageTreeProps) {
  const { pages, depth } = props
  return (
    <ul className="page-tree">
      {pages.map((page, i) => (
        <PageTreeItem
          key={page.id}
          {...props}
          page={page}
          depth={depth}
          prevId={pages[i - 1]?.id}
          nextId={pages[i + 1]?.id}
        />
      ))}
    </ul>
  )
}

function PageTreeItem({
  page,
  depth,
  spaceId,
  parentId,
  canEdit,
  activeId,
  onSelect,
  onRenamePage,
  onChangeIcon,
  onCreatePage,
  onArchivePage,
  pageDragProps,
  onOpenMoveTo,
  prevId,
  nextId,
}: PageTreeProps & { page: PageNode; prevId?: string; nextId?: string }) {
  const moves = useContext(SidebarMovesContext)
  // A collection's documents and databases are listed on its own page
  // (CollectionView), not in the tree: that's the point of it.
  const isCollection = page.kind === 'collection'
  const hasChildren = !isCollection && !!page.children?.length
  const [expanded, setExpanded] = useState(true)
  const [renaming, setRenaming] = useState(false)
  // Holds the icon span's own element once clicked, rather than reading
  // iconRef.current directly from the render expression below (which
  // React's rules of hooks flag: refs are only meant to be read from
  // event handlers/effects, not render) -- this doubles as both "is the
  // picker open" and "where to anchor it".
  const [pickerAnchor, setPickerAnchor] = useState<HTMLSpanElement | null>(null)
  const [draftTitle, setDraftTitle] = useState(page.title)
  const titleInputRef = useRef<HTMLInputElement>(null)
  const iconRef = useRef<HTMLSpanElement>(null)

  useEffect(() => {
    if (renaming) {
      titleInputRef.current?.focus()
      titleInputRef.current?.select()
    }
  }, [renaming])

  function commitRename() {
    const trimmed = draftTitle.trim()
    onRenamePage(page.id, trimmed || page.title)
    setRenaming(false)
  }

  const drag = pageDragProps(page.id, spaceId, parentId)
  const className = [
    'page-row',
    // An open page inside a collection isn't in the tree, so the
    // collection row itself shows where you are.
    (activeId === page.id || (isCollection && page.children?.some((c) => c.id === activeId))) && 'active',
    drag.isDragging && 'page-row-dragging',
    drag.dropPosition && `page-row-drop-${drag.dropPosition}`,
  ]
    .filter(Boolean)
    .join(' ')

  return (
    <li>
      <div
        className={className}
        // --row-indent is where the row's content (chevron, icon, title)
        // starts; each nesting level adds 1.1rem. The drag grip no longer
        // takes a slot in the row's flex flow -- it's absolutely
        // positioned just inside this indent (see .page-grip), so the
        // base is 2.3rem instead of the old 1.6rem-plus-a-reserved-grip:
        // pages still sit clearly under their teamspace, but ~10px
        // further left, leaving that much more room for the title
        // before it truncates.
        style={{ '--row-indent': `${2.3 + depth * 1.1}rem` } as CSSProperties}
        onClick={() => onSelect(page.id)}
        onDragOver={drag.onDragOver}
        onDragLeave={drag.onDragLeave}
        onDrop={drag.onDrop}
        draggable={(canEdit && TOUCH_DRAG) || undefined}
        onDragStart={TOUCH_DRAG ? drag.onDragStart : undefined}
        onDragEnd={TOUCH_DRAG ? drag.onDragEnd : undefined}
      >
        <button
          className={'chevron' + (hasChildren ? '' : ' chevron-hidden')}
          onClick={(e) => {
            e.stopPropagation()
            setExpanded((v) => !v)
          }}
          aria-label={expanded ? 'Collapse' : 'Expand'}
        >
          {hasChildren && <ChevronIcon open={expanded} />}
        </button>

        {/* Hover-revealed drag handle, same reasoning as every other
            grip in this file -- the sole HTML5 drag source, so
            double-click-to-rename/icon-picker/menu clicks on the rest
            of the row keep working normally. */}
        <span
          className="page-grip"
          hidden={!canEdit}
          draggable={!TOUCH_DRAG}
          title="Drag to reorder or move"
          onDragStart={TOUCH_DRAG ? undefined : drag.onDragStart}
          onDragEnd={TOUCH_DRAG ? undefined : drag.onDragEnd}
        >
          <GripIcon />
        </span>

        <span
          ref={iconRef}
          className={canEdit ? 'page-icon-editable' : undefined}
          onClick={(e) => {
            e.stopPropagation()
            if (canEdit) setPickerAnchor(iconRef.current)
          }}
        >
          <PageIcon icon={page.icon} className="page-icon" />
        </span>
        {pickerAnchor && (
          <Suspense fallback={null}>
            <IconPicker
              icon={page.icon}
              anchorEl={pickerAnchor}
              onChange={(icon) => onChangeIcon(page.id, icon)}
              onClose={() => setPickerAnchor(null)}
            />
          </Suspense>
        )}

        {renaming ? (
          <input
            ref={titleInputRef}
            className="page-title-input"
            value={draftTitle}
            onChange={(e) => setDraftTitle(e.target.value)}
            onClick={(e) => e.stopPropagation()}
            onBlur={commitRename}
            onKeyDown={(e) => {
              if (e.key === 'Enter') commitRename()
              if (e.key === 'Escape') {
                setDraftTitle(page.title)
                setRenaming(false)
              }
            }}
          />
        ) : (
          // Click/Enter bubbles to the row's onClick, which opens the page.
          <button
            type="button"
            className={'page-title' + (page.title ? '' : ' page-title-empty')}
            aria-current={activeId === page.id ? 'page' : undefined}
            onDoubleClick={(e) => {
              e.stopPropagation()
              if (!canEdit) return
              setDraftTitle(page.title)
              setRenaming(true)
            }}
            title={canEdit ? 'Double-click to rename' : undefined}
          >
            {page.title || 'Untitled'}
          </button>
        )}

        <span onClick={(e) => e.stopPropagation()} hidden={!canEdit}>
          <CreatePageButton
            spaceId={spaceId}
            parentId={page.id}
            onCreatePage={onCreatePage}
            label={isCollection ? 'Add a page/database to this collection' : 'Create a sub-page/database'}
            allowCollection={page.kind === 'document'}
          />
        </span>
        <span onClick={(e) => e.stopPropagation()}>
          <RowMenu
            items={[
              ...(canEdit
                ? [
                    {
                      label: 'Rename',
                      onClick: () => {
                        setDraftTitle(page.title)
                        setRenaming(true)
                      },
                    },
                    ...(moves
                      ? moveItems(prevId, nextId, (target, pos) => moves.movePage(page.id, spaceId, parentId, target, pos))
                      : []),
                    { label: 'Move elsewhere...', onClick: (anchorEl: HTMLElement) => onOpenMoveTo(page.id, anchorEl) },
                  ]
                : []),
              { label: 'Export as Markdown', onClick: () => downloadUrl(`/api/pages/${page.id}/export.md`) },
              ...(canEdit
                ? [
                    {
                      label: 'Archive',
                      danger: true,
                      onClick: () => {
                        if (window.confirm(`Archive "${page.title || 'Untitled'}"?`)) onArchivePage(page.id)
                      },
                    },
                  ]
                : []),
            ]}
          />
        </span>
      </div>
      {hasChildren && (
        <Collapse open={expanded}>
          <PageTree
            pages={page.children!}
            depth={depth + 1}
            spaceId={spaceId}
            parentId={page.id}
            canEdit={canEdit}
            activeId={activeId}
            onSelect={onSelect}
            onRenamePage={onRenamePage}
            onChangeIcon={onChangeIcon}
            onCreatePage={onCreatePage}
            onArchivePage={onArchivePage}
            pageDragProps={pageDragProps}
            onOpenMoveTo={onOpenMoveTo}
          />
        </Collapse>
      )}
    </li>
  )
}

export default function Sidebar({
  teamspaces,
  groups,
  activeId,
  onSelect,
  onRenamePage,
  onChangeIcon,
  onCreatePage,
  onArchivePage,
  onMovePageTo,
  onMovePageToPosition,
  onCreateSpace,
  onRenameSpace,
  onChangeSpaceIcon,
  onArchiveSpace,
  onReorderSpaces,
  onCreateGroup,
  onRenameGroup,
  onChangeGroupIcon,
  onReorderGroups,
  onArchiveGroup,
  onMoveSpaceToGroup,
  collapsed,
  onOpenArchive,
}: SidebarProps) {
  const [width, setWidth] = useState(readStoredWidth)
  const [resizing, setResizing] = useState(false)
  const [creatingGroup, setCreatingGroup] = useState(false)
  // Ids of collapsed groups (everything else is open, so a new group
  // starts open). Not persisted, same as before.
  const [collapsedGroups, setCollapsedGroups] = useState<Set<string>>(() => new Set())
  function toggleGroup(id: string) {
    setCollapsedGroups((prev) => {
      const next = new Set(prev)
      if (!next.delete(id)) next.add(id)
      return next
    })
  }
  const allGroupsCollapsed = groups.length > 0 && groups.every((g) => collapsedGroups.has(g.id))
  // Non-empty while the search box has text: the tree is swapped for
  // SidebarSearch's result list (see its own header comment).
  const [searchQuery, setSearchQuery] = useState('')
  const searching = searchQuery.trim() !== ''
  // Drag state stays local to Sidebar (purely a UI concern); the actual
  // reorder computation lives in App.tsx's reorderTeamspaces/
  // reorderGroups, same split as TabBar.tsx's own tab reorder. Two
  // independent pairs -- a space drag and a group drag are different
  // gestures on different elements, never active at the same time, but
  // sharing one pair of state variables would make that an assumption
  // instead of something the types themselves guarantee.
  const [draggedSpaceId, setDraggedSpaceId] = useState<string | null>(null)
  const [spaceDropTarget, setSpaceDropTarget] = useState<{ id: string; position: 'before' | 'after' } | null>(null)
  const [draggedGroupId, setDraggedGroupId] = useState<string | null>(null)
  const [groupDropTarget, setGroupDropTarget] = useState<{ id: string; position: 'before' | 'after' } | null>(null)
  // Which group is the current cross-group drop target for a *space*
  // drag (as opposed to groupDropTarget, which is for reordering
  // groups against each other) -- previously there was no visual
  // feedback at all for this, so hovering a valid destination looked
  // identical to hovering nothing, and the drop landing correctly felt
  // like luck rather than something the user could see coming.
  const [spaceDropGroupId, setSpaceDropGroupId] = useState<string | null>(null)

  // stopPropagation everywhere below -- a space now drags inside a
  // group section that has its own dragover/drop handlers for
  // reordering *groups* (handleGroupDragOver/handleGroupDrop further
  // down). Without it, every one of these events also bubbles up to
  // the enclosing .sidebar-group div; that outer handler checks its
  // own draggedGroupId and no-ops harmlessly during a space drag, but
  // letting a nested drop zone's events reach an ancestor drop zone at
  // all is exactly the kind of thing that fights browsers' own
  // drag-and-drop state in practice (which of two overlapping valid
  // targets a drop resolves to isn't reliably "the innermost one" once
  // both received the event) -- cutting it off at the source is the
  // standard fix for nested drop zones, not just a guess.
  //
  // Drop position is read straight off the cursor's own vertical
  // position within the hovered row (top half = before, bottom half =
  // after), same as PageDragProps below -- replaces an earlier version
  // that inferred direction by comparing the dragged item's and
  // target's original list indices, which left "make this the last
  // space in the group" with no reliable way to trigger (see
  // reorderSpaces' own comment in App.tsx).
  function handleSpaceDragStart(spaceId: string) {
    return (e: React.DragEvent) => {
      e.stopPropagation()
      e.dataTransfer.effectAllowed = 'move'
      e.dataTransfer.setData('text/plain', spaceId)
      setDraggedSpaceId(spaceId)
    }
  }

  function handleSpaceDragOver(spaceId: string) {
    return (e: React.DragEvent) => {
      if (!draggedSpaceId || draggedSpaceId === spaceId) return
      const draggedSpace = teamspaces.find((t) => t.id === draggedSpaceId)
      const targetSpace = teamspaces.find((t) => t.id === spaceId)
      if (!draggedSpace || !targetSpace) return
      e.preventDefault()
      e.stopPropagation()
      // Hovering a space that's actually in a *different* group --
      // this is a cross-group move, not a reorder, so there's no
      // "before/after this specific row" position to show (a move
      // just lands wherever the destination group's own order puts
      // it). Highlighting the *whole* destination group instead --
      // same indicator a drop on its header already used -- was
      // previously only shown there specifically; showing a
      // before/after row indicator here too, on top of nothing
      // happening with it, is what read as "buggy."
      if (draggedSpace.groupId !== targetSpace.groupId) {
        setSpaceDropTarget(null)
        setSpaceDropGroupId((cur) => (cur === targetSpace.groupId ? cur : targetSpace.groupId))
        return
      }
      setSpaceDropGroupId(null)
      const rect = e.currentTarget.getBoundingClientRect()
      // Split against a small fixed band near the row's own header, not
      // 50% of the whole section -- the section (this whole .teamspace
      // div, header plus every page under it if expanded) stays the big,
      // easy-to-hit target it was built to be, but a space with several
      // pages under it makes that div tall, and splitting *that* in half
      // put the before/after boundary somewhere in the middle of its
      // page list -- nowhere near the header a user is actually aiming
      // at, which is what read as imprecise.
      const position: 'before' | 'after' =
        e.clientY - rect.top < Math.min(rect.height / 2, ROW_DROP_THRESHOLD_PX) ? 'before' : 'after'
      setSpaceDropTarget((cur) => (cur?.id === spaceId && cur.position === position ? cur : { id: spaceId, position }))
    }
  }

  function handleSpaceDrop(spaceId: string) {
    return (e: React.DragEvent) => {
      // Bail before touching the event when no *space* drag is
      // actually in progress -- same reasoning as handlePageDrop's own
      // fix: a page or group being dragged can still land here via
      // bubbling (this row's own dragover already correctly ignores
      // those), and unconditionally stopping propagation would swallow
      // the drop before an ancestor that actually wants it sees it.
      if (!draggedSpaceId) return
      e.preventDefault()
      e.stopPropagation()
      const draggedId = draggedSpaceId
      const target = spaceDropTarget
      setDraggedSpaceId(null)
      setSpaceDropTarget(null)
      setSpaceDropGroupId(null)
      if (draggedId === spaceId) return
      // Dropped onto a space that's actually in a *different* group --
      // moves it there (onMoveSpaceToGroup), the same action a drop on
      // that group's own header triggers. Previously this only worked
      // via the header specifically; dropping onto any of the group's
      // existing spaces (the much bigger, more natural target) silently
      // did nothing, which read as "moving a space between groups
      // doesn't work" even though the narrow header strip did.
      const draggedSpace = teamspaces.find((t) => t.id === draggedId)
      const targetSpace = teamspaces.find((t) => t.id === spaceId)
      if (!draggedSpace || !targetSpace) return
      if (draggedSpace.groupId !== targetSpace.groupId) {
        onMoveSpaceToGroup(draggedId, targetSpace.groupId)
        return
      }
      if (!target) return
      onReorderSpaces(draggedId, spaceId, target.position)
    }
  }

  function handleSpaceDragEnd() {
    setDraggedSpaceId(null)
    setSpaceDropTarget(null)
    setSpaceDropGroupId(null)
  }

  // Bundles one space's drag props for GroupSection -- it only knows
  // about its own filtered teamspaces, not the drag state that lives up
  // here.
  function spaceDragProps(spaceId: string) {
    return {
      isDragging: draggedSpaceId === spaceId,
      dropPosition: spaceDropTarget?.id === spaceId ? spaceDropTarget.position : null,
      onDragStart: handleSpaceDragStart(spaceId),
      onDragOver: handleSpaceDragOver(spaceId),
      onDragLeave: () => {
        setSpaceDropTarget((cur) => (cur?.id === spaceId ? null : cur))
        const space = teamspaces.find((t) => t.id === spaceId)
        if (space) setSpaceDropGroupId((cur) => (cur === space.groupId ? null : cur))
      },
      onDrop: handleSpaceDrop(spaceId),
      onDragEnd: handleSpaceDragEnd,
    }
  }

  function handleGroupDragStart(groupId: string) {
    return (e: React.DragEvent) => {
      e.dataTransfer.effectAllowed = 'move'
      e.dataTransfer.setData('text/plain', groupId)
      setDraggedGroupId(groupId)
    }
  }

  // Handles two different drags on the *same* whole-section drop zone:
  // a group being reordered against its siblings, or a space being
  // dropped in to move it into this group. Previously the space case
  // only worked on the header's own thin strip -- dropping a space
  // anywhere else on a group (its body, or an empty group with no
  // header-adjacent space rows to land on instead) silently did
  // nothing, which is exactly what made moving a space into a group
  // that has no spaces in it yet impossible: there's no existing space
  // row to drop onto (see handleSpaceDrop's own cross-group handling),
  // and the header alone is too thin a target for reasons identical to
  // Teamspace's own "whole section is the drop zone" comment above.
  function handleGroupDragOver(groupId: string) {
    return (e: React.DragEvent) => {
      if (draggedGroupId) {
        if (draggedGroupId === groupId) return
        e.preventDefault()
        const rect = e.currentTarget.getBoundingClientRect()
        // Same fixed-band reasoning as handleSpaceDragOver -- a group's
        // whole section (every space and page filed under it, if
        // expanded) can be considerably taller than just its header.
        const position: 'before' | 'after' =
          e.clientY - rect.top < Math.min(rect.height / 2, ROW_DROP_THRESHOLD_PX) ? 'before' : 'after'
        setGroupDropTarget((cur) =>
          cur?.id === groupId && cur.position === position ? cur : { id: groupId, position },
        )
        return
      }
      if (!draggedSpaceId) return
      const space = teamspaces.find((t) => t.id === draggedSpaceId)
      if (!space) return
      // Its own group: nothing to do here, and the sidebar-wide
      // "drop at the end" fallback mustn't light up the last group either.
      e.stopPropagation()
      if (space.groupId === groupId) return
      e.preventDefault()
      setSpaceDropGroupId((cur) => (cur === groupId ? cur : groupId))
    }
  }

  function handleGroupDrop(groupId: string) {
    return (e: React.DragEvent) => {
      if (draggedGroupId) {
        e.preventDefault()
        const draggedId = draggedGroupId
        const target = groupDropTarget
        setDraggedGroupId(null)
        setGroupDropTarget(null)
        if (draggedId !== groupId && target) onReorderGroups(draggedId, groupId, target.position)
        return
      }
      if (!draggedSpaceId) return
      e.preventDefault()
      e.stopPropagation()
      const spaceId = draggedSpaceId
      const space = teamspaces.find((t) => t.id === spaceId)
      setDraggedSpaceId(null)
      setSpaceDropTarget(null)
      setSpaceDropGroupId(null)
      if (space && space.groupId !== groupId) onMoveSpaceToGroup(spaceId, groupId)
    }
  }

  function handleGroupDragEnd() {
    setDraggedGroupId(null)
    setGroupDropTarget(null)
  }

  // Fallback for the blank area below the last group -- a group with
  // few or no spaces (or simply the last one in the list) can leave a
  // lot of the sidebar's own remaining height with no group section
  // underneath the cursor at all, and none of the per-group handlers
  // above ever fire there. Dropping a space anywhere in that dead zone
  // means "put it at the end", the same as dropping past the last item
  // in any reorderable list.
  function handleSidebarDragOver(e: React.DragEvent) {
    if (!draggedSpaceId || groups.length === 0) return
    const lastGroup = groups[groups.length - 1]
    const space = teamspaces.find((t) => t.id === draggedSpaceId)
    if (!space || space.groupId === lastGroup.id) return
    e.preventDefault()
    setSpaceDropGroupId((cur) => (cur === lastGroup.id ? cur : lastGroup.id))
  }

  function handleSidebarDrop(e: React.DragEvent) {
    if (!draggedSpaceId || groups.length === 0) return
    e.preventDefault()
    const lastGroup = groups[groups.length - 1]
    const spaceId = draggedSpaceId
    const space = teamspaces.find((t) => t.id === spaceId)
    setDraggedSpaceId(null)
    setSpaceDropTarget(null)
    setSpaceDropGroupId(null)
    if (space && space.groupId !== lastGroup.id) onMoveSpaceToGroup(spaceId, lastGroup.id)
  }

  // Page drag state, mirroring the space/group pairs above one tree
  // level in. dropTarget also carries which third of the hovered row
  // the cursor is over -- before/after (an exact sibling position) or
  // into (reparent as that row's own child) -- computed once per
  // dragover from the row's own bounding rect, not tracked separately.
  const [draggedPageId, setDraggedPageId] = useState<string | null>(null)
  const [pageDropTarget, setPageDropTarget] = useState<{ id: string; position: 'before' | 'after' | 'into' } | null>(
    null,
  )

  function handlePageDragStart(pageId: string) {
    return (e: React.DragEvent) => {
      e.stopPropagation()
      e.dataTransfer.effectAllowed = 'move'
      e.dataTransfer.setData('text/plain', pageId)
      setDraggedPageId(pageId)
    }
  }

  // The page being dragged: one of the sidebar's own rows, or an item
  // dragged in from a collection's list (pageDrag.ts).
  const currentDraggedPageId = () => draggedPageId ?? externalPageDrag()

  // A drag from a collection's list that ends without a drop here (let
  // go elsewhere, or cancelled with Escape) must not leave a drop marker
  // behind on the last row it passed over.
  useEffect(() => {
    const clear = () => setPageDropTarget(null)
    document.addEventListener('dragend', clear)
    return () => document.removeEventListener('dragend', clear)
  }, [])

  function handlePageDragOver(pageId: string) {
    return (e: React.DragEvent) => {
      const draggedId = currentDraggedPageId()
      if (!draggedId || draggedId === pageId) return
      e.preventDefault()
      e.stopPropagation()
      const rect = e.currentTarget.getBoundingClientRect()
      const ratio = (e.clientY - rect.top) / rect.height
      let position: 'before' | 'after' | 'into' = ratio < 0.25 ? 'before' : ratio > 0.75 ? 'after' : 'into'
      // A collection can't go into a collection or a database
      // (pages.CanHold on the backend): only beside it.
      if (position === 'into' && findPage(teamspaces, draggedId)?.kind === 'collection') {
        const targetKind = findPage(teamspaces, pageId)?.kind
        if (targetKind === 'collection' || targetKind === 'database') position = ratio < 0.5 ? 'before' : 'after'
      }
      setPageDropTarget((cur) => (cur?.id === pageId && cur.position === position ? cur : { id: pageId, position }))
    }
  }

  function handlePageDrop(pageId: string, spaceId: string, parentId: string | null) {
    return (e: React.DragEvent) => {
      // Bail before touching the event at all when no *page* drag is
      // actually in progress -- a space being dragged onto a target
      // space's own expanded page list lands on one of these page rows
      // first, and this handler was unconditionally calling
      // stopPropagation() regardless, silently swallowing the drop
      // before it could ever reach the space's own container-level
      // handler. handlePageDragOver already had this same guard first;
      // this one didn't, which is what made a within-group space
      // reorder fail specifically whenever the drop landed over a
      // page row instead of empty space in the target section.
      const draggedId = currentDraggedPageId()
      if (!draggedId) return
      e.preventDefault()
      e.stopPropagation()
      const target = pageDropTarget
      setDraggedPageId(null)
      setPageDropTarget(null)
      endExternalPageDrag()
      if (draggedId === pageId || !target) return
      if (target.position === 'into') {
        onMovePageTo(draggedId, spaceId, pageId)
      } else {
        onMovePageToPosition(draggedId, spaceId, parentId, pageId, target.position)
      }
    }
  }

  function handlePageDragEnd() {
    setDraggedPageId(null)
    setPageDropTarget(null)
  }

  function pageDragProps(pageId: string, spaceId: string, parentId: string | null): PageDragProps {
    return {
      isDragging: draggedPageId === pageId,
      dropPosition: pageDropTarget?.id === pageId ? pageDropTarget.position : null,
      onDragStart: handlePageDragStart(pageId),
      onDragOver: handlePageDragOver(pageId),
      onDragLeave: () => setPageDropTarget((cur) => (cur?.id === pageId ? null : cur)),
      onDrop: handlePageDrop(pageId, spaceId, parentId),
      onDragEnd: handlePageDragEnd,
    }
  }

  // A page dropped on a teamspace's own header becomes a top-level page
  // of that space (parentId null) -- the header sits above the whole
  // page-tree drop zone, so this is the one spot in a space that isn't
  // already "before/after/into" some specific existing page row.
  function handleTeamspaceHeaderDragOver(e: React.DragEvent) {
    if (!currentDraggedPageId()) return
    e.preventDefault()
    e.stopPropagation()
  }

  function handleTeamspaceHeaderDrop(spaceId: string) {
    return (e: React.DragEvent) => {
      const pageId = currentDraggedPageId()
      if (!pageId) return
      e.preventDefault()
      e.stopPropagation()
      setDraggedPageId(null)
      setPageDropTarget(null)
      endExternalPageDrag()
      onMovePageTo(pageId, spaceId, null)
    }
  }

  // "Move to..." fallback picker -- lets a page move to a different
  // space without dragging at all (point 13 of the sidebar-refactor
  // spec: every drag-only interaction needs a non-drag equivalent).
  // Always lands the page as a top-level page of the chosen space, same
  // as a drop on that space's own header above.
  const [moveTo, setMoveTo] = useState<{ pageId: string; anchorEl: HTMLElement } | null>(null)

  useEffect(() => {
    try {
      localStorage.setItem(WIDTH_KEY, String(width))
    } catch {
      // Not persisted this session; the sidebar still works at this width.
    }
  }, [width])

  // Double-click on the resize handle: the narrowest width at which every
  // visible row's full name fits. The natural (max-content) width counts
  // each row's whole name, ignoring the ellipsis. The search box and the
  // footer are left out of the measurement (.sidebar-measuring): a text
  // input asks for ~20 characters by default and would set a floor of
  // its own. Hover-only buttons get no room, so a long name may
  // ellipsize while hovered. Collapsed content isn't rendered, so it
  // doesn't count.
  const asideRef = useRef<HTMLElement>(null)
  const AUTOFIT_SLACK = 4
  function autoFitWidth() {
    const el = asideRef.current
    if (!el) return
    const prevWidth = el.style.width
    el.style.transition = 'none'
    el.classList.add('sidebar-measuring')
    el.style.width = 'max-content'
    const natural = Math.ceil(el.getBoundingClientRect().width)
    el.classList.remove('sidebar-measuring')
    el.style.width = prevWidth
    void el.offsetWidth
    el.style.transition = ''
    setWidth(Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, natural + AUTOFIT_SLACK)))
  }

  function startResizing(e: React.PointerEvent) {
    const startWidth = width
    setResizing(true)
    startPointerDrag(e, {
      onMove: (dx) => setWidth(Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, startWidth + dx))),
      onEnd: () => setResizing(false),
    })
  }

  // Collapsing hides the sidebar entirely -- the toggle to bring it back
  // lives in TabBar (top-left, Notion-style), not a floating affordance
  // duplicated here. It slides out and in: on desktop the shell's width
  // animates (the sidebar keeps its own width inside, so its content
  // moves as one piece instead of squeezing); on mobile the drawer
  // slides (mobile.css). Unmounted once closed, as before.
  const presence = usePresence(!collapsed, SIDEBAR_SLIDE_MS)
  if (!presence.mounted) return null
  const shellClassName = [
    'sidebar-shell',
    presence.shown ? 'sidebar-shell-open' : 'sidebar-shell-closing',
    (presence.animating || !presence.shown) && 'sidebar-shell-clip',
    resizing && 'sidebar-shell-resizing',
  ]
    .filter(Boolean)
    .join(' ')

  return (
    <SidebarMovesContext.Provider
      value={{
        movePage: (pageId, spaceId, parentId, targetId, position) =>
          onMovePageToPosition(pageId, spaceId, parentId, targetId, position),
        moveSpace: onReorderSpaces,
        moveGroup: onReorderGroups,
      }}
    >
    <div
      className={shellClassName}
      style={{ width: presence.shown ? `${width}px` : 0 }}
      onTransitionEnd={(e) => {
        if (e.target === e.currentTarget && e.propertyName === 'width') presence.finish()
      }}
    >
    <aside
      ref={asideRef}
      className={resizing ? 'sidebar resizing' : 'sidebar'}
      style={{ width: `${width}px` }}
      onDragOver={handleSidebarDragOver}
      onDrop={handleSidebarDrop}
    >
      <div className="sidebar-workspace">
        <img className="workspace-icon" src="/favicon.svg" alt="" />
        <span className="workspace-name">Nook</span>
      </div>

      <SidebarSearch
        query={searchQuery}
        onQueryChange={setSearchQuery}
        teamspaces={teamspaces}
        groups={groups}
        activeId={activeId}
        onSelectPage={onSelect}
        onRevealSection={(_kind, id) => {
          // A group/space picked from the results: bring the tree back
          // synchronously so its section exists, then scroll to it and
          // flash it briefly.
          flushSync(() => setSearchQuery(''))
          const el = document.querySelector<HTMLElement>(`[data-section-id="${CSS.escape(id)}"]`)
          if (!el) return
          el.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
          el.classList.remove('sidebar-flash')
          void el.offsetWidth // restart the animation if it is still running
          el.classList.add('sidebar-flash')
          window.setTimeout(() => el.classList.remove('sidebar-flash'), 1200)
        }}
      />

      {!searching && (
        <>
          {/* Notion's own "Teamspaces" label above its teamspace list --
              the "+" here is this sidebar's only way to create a brand new
              group (an existing group's own "..." only makes sense for
              actions *on that group*, not for creating an unrelated
              sibling); hover-revealed so it doesn't sit as permanent
              floating text the way the old bottom button did. */}
          <div className="sidebar-section-label">
            <span>Groups</span>
            <span className="sidebar-section-actions">
              <button
                className="page-add-child sidebar-collapse-all"
                onClick={() =>
                  setCollapsedGroups(allGroupsCollapsed ? new Set() : new Set(groups.map((g) => g.id)))
                }
                title={allGroupsCollapsed ? 'Expand all groups' : 'Collapse all groups'}
                aria-label={allGroupsCollapsed ? 'Expand all groups' : 'Collapse all groups'}
              >
                <svg
                  viewBox="0 0 24 24"
                  width="18"
                  height="18"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  aria-hidden="true"
                >
                  {allGroupsCollapsed ? (
                    <>
                      <path d="m7 15 5 5 5-5" />
                      <path d="m7 9 5-5 5 5" />
                    </>
                  ) : (
                    <>
                      <path d="m7 20 5-5 5 5" />
                      <path d="m7 4 5 5 5-5" />
                    </>
                  )}
                </svg>
              </button>
              <button
                className="page-add-child"
                onClick={() => setCreatingGroup(true)}
                title="Create a new group"
                aria-label="Create a new group"
              >
                +
              </button>
            </span>
          </div>
          {creatingGroup && (
            <NewSpaceForm placeholder="Group name…" onCreate={onCreateGroup} onClose={() => setCreatingGroup(false)} />
          )}

          {groups.map((group, i) => (
            <GroupSection
              key={group.id}
              prevId={groups[i - 1]?.id}
              nextId={groups[i + 1]?.id}
              group={group}
              expanded={!collapsedGroups.has(group.id)}
              onToggleExpanded={() => toggleGroup(group.id)}
              teamspaces={teamspaces.filter((t) => t.groupId === group.id)}
              activeId={activeId}
              onSelect={onSelect}
              onRenamePage={onRenamePage}
              onChangeIcon={onChangeIcon}
              onCreatePage={onCreatePage}
              onArchivePage={onArchivePage}
              onCreateSpace={onCreateSpace}
              onRenameSpace={onRenameSpace}
              onChangeSpaceIcon={onChangeSpaceIcon}
              onArchiveSpace={onArchiveSpace}
              onRenameGroup={onRenameGroup}
              onChangeGroupIcon={onChangeGroupIcon}
              onArchiveGroup={onArchiveGroup}
              spaceDragProps={spaceDragProps}
              isDragging={draggedGroupId === group.id}
              dropPosition={groupDropTarget?.id === group.id ? groupDropTarget.position : null}
              isSpaceDropTarget={spaceDropGroupId === group.id}
              onDragStart={handleGroupDragStart(group.id)}
              onDragOver={handleGroupDragOver(group.id)}
              onDragLeave={() => {
                setGroupDropTarget((cur) => (cur?.id === group.id ? null : cur))
                setSpaceDropGroupId((cur) => (cur === group.id ? null : cur))
              }}
              onDrop={handleGroupDrop(group.id)}
              onDragEnd={handleGroupDragEnd}
              teamspaceHeaderDragOver={handleTeamspaceHeaderDragOver}
              teamspaceHeaderDrop={handleTeamspaceHeaderDrop}
              pageDragProps={pageDragProps}
              onOpenMoveTo={(pageId, anchorEl) => setMoveTo({ pageId, anchorEl })}
            />
          ))}
        </>
      )}

      {moveTo && (
        <Popover
          anchorEl={moveTo.anchorEl}
          width={220}
          maxHeight={280}
          onClose={() => setMoveTo(null)}
          className="move-to-menu"
        >
          {groups.map((group) => {
            const groupSpaces = teamspaces.filter((t) => t.groupId === group.id)
            if (groupSpaces.length === 0) return null
            return (
              <div key={group.id} className="move-to-group">
                <div className="move-to-group-name">{group.name}</div>
                {groupSpaces.map((space) => (
                  <button
                    key={space.id}
                    type="button"
                    className="move-to-item"
                    onClick={() => {
                      onMovePageTo(moveTo.pageId, space.id, null)
                      setMoveTo(null)
                    }}
                  >
                    <PageIcon icon={space.icon} className="page-icon" />
                    {space.name}
                  </button>
                ))}
              </div>
            )
          })}
        </Popover>
      )}

      <div className="sidebar-footer">
        <button type="button" className="sidebar-footer-btn" onClick={onOpenArchive}>
          <PageIcon icon="lucide:archive" className="page-icon" />
          Archive
        </button>
      </div>

      <div
        className="sidebar-resize-handle"
        onPointerDown={startResizing}
        onDoubleClick={autoFitWidth}
        title="Drag to resize, double-click to fit"
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize sidebar width"
      />
    </aside>
    </div>
    </SidebarMovesContext.Provider>
  )
}
