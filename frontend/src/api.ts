// API client. Types here match the Go backend's JSON responses field
// for field (snake_case, straight off the wire) -- deliberately not
// remapped to camelCase, so there's no translation layer to keep in
// sync as the backend evolves.

export interface User {
  id: string
  username: string
  display_name: string
  email: string
  email_verified: boolean
  is_admin: boolean
  must_change_password: boolean
}

export type RegistrationMode = 'open' | 'approval' | 'invite' | 'closed'

export interface AuthConfig {
  registration: RegistrationMode
  email_enabled: boolean
}

export interface SessionInfo {
  id: string
  created_at: string
  last_seen_at: string
  user_agent: string
  ip: string
  current: boolean
}

export interface AdminUser {
  id: string
  username: string
  display_name: string
  email: string
  email_verified: boolean
  status: 'pending' | 'active' | 'disabled'
  is_admin: boolean
  created_at: string
}

export interface Invite {
  id: string
  email: string
  group_id: string
  group_role: string
  expires_at: string
  used_at: string
  user_id: string
  created_at: string
  path?: string
}

export interface ActivityEntry {
  id: string
  actor_id: string
  actor_name: string
  space_id: string
  page_id: string
  page_title: string
  action: string
  meta: Record<string, unknown>
  created_at: string
}

export interface Space {
  id: string
  name: string
  type: 'private' | 'shared'
  icon: string
  group_id: string
  // Only ever populated by listSpaces -- the caller's own ordering of
  // this space (see spaceResponse's own comment on the backend).
  position?: string
}

// Group: the tier above spaces (migrations/0009_groups.sql on the
// backend) -- the real invite/tenant boundary for multi-user. Inviting
// someone to a group grants them access to every space inside it.
export interface Group {
  id: string
  name: string
  icon: string
  position?: string
}

export interface GroupMember {
  user_id: string
  username: string
  display_name: string
  role: 'viewer' | 'editor' | 'admin' | 'owner'
}

// PageKind: 'database' pages hold structured rows/columns instead of a
// Tiptap document -- see docs/adr/0004-database-pages.md. A database's
// rows are themselves ordinary 'document' pages (children of the
// database page), so Kind only ever distinguishes the database page
// itself from every other page, rows included.
// 'collection' (migration 0018): gathers documents and databases and
// lists them on its own page (CollectionView); the sidebar shows it as a
// single row.
export type PageKind = 'document' | 'database' | 'collection'

// TreeNode is the sidebar-shaped view: no content_json (the sidebar
// never needs it -- see backend internal/httpapi/pages.go).
export interface TreeNode {
  id: string
  parent_page_id: string | null
  title: string
  icon: string
  position: string
  kind: PageKind
  children: TreeNode[]
}

// Page is the full single-page view, content_json included.
export interface Page {
  id: string
  space_id: string
  parent_page_id: string | null
  title: string
  icon: string
  content_json: string
  position: string
  created_by: string
  kind: PageKind
  created_at: string
  updated_at: string
  updated_by: string
  updated_by_name: string
}

// PropertyType: a database page's column types. A 'person' value is
// {user_id} of someone who can reach the space (see listPeople).
export type PropertyType = 'text' | 'select' | 'date' | 'person' | 'number' | 'checkbox'

export interface SelectOption {
  id: string
  label: string
  color: string
}

export interface Property {
  id: string
  /** The database page this column belongs to. */
  page_id: string
  name: string
  type: PropertyType
  options: SelectOption[]
  position: string
}

// Row is one row of a database page (really just a lightweight view of
// one of its child pages) plus its cell values, keyed by property id.
// A value's shape depends on the owning property's type: a string for
// text/date, a number for number, a bool for checkbox, {option_id} for
// select, or {user_id} for person (older cells may still hold a name
// string) -- see the backend migration's comment.
export interface Row {
  id: string
  title: string
  icon: string
  position: string
  values: Record<string, unknown>
}

// TITLE_COLUMN_ID is the sentinel id representing the title ("Name")
// column in a reorder -- matches databases.TitleColumnID on the
// backend exactly (see reorderColumns' own comment). It isn't a real
// Property (the title lives on each row's page directly, not as a
// column definition), but it can be dragged and reordered among the
// real ones, so it needs an id to appear in that ordering alongside
// them.
export const TITLE_COLUMN_ID = 'title'

export interface DatabaseTable {
  properties: Property[]
  rows: Row[]
  // Where the title column sorts among properties -- same zero-padded
  // scheme as Property.position, so the frontend can sort them
  // together (plain string comparison) to render both interleaved in
  // the right order.
  title_column_position: string
  /** The board view's own card order (row ids); rows not in it follow. */
  board_order: string[]
  /** The board view's column order per grouping property (option or
   * user ids, "" for "no value"). Layout only. */
  board_columns: Record<string, string[]>
}

// The server's 429 body is terse ("too many requests"); people get a
// sentence that says what to do.
const RATE_LIMITED = 'Too many requests in a short time. Wait a few seconds and try again.'

async function parseJSONOrThrow<T>(res: Response): Promise<T> {
  if (!res.ok) {
    if (res.status === 429) throw new Error(RATE_LIMITED)
    const text = await res.text()
    throw new Error(text || `request failed: ${res.status}`)
  }
  return res.json() as Promise<T>
}

// send is a small JSON request helper for the account endpoints: throws
// the server's error text (the UI translates the known ones).
async function send<T = void>(method: string, url: string, body?: unknown): Promise<T> {
  const res = await fetch(url, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!res.ok) {
    if (res.status === 429) throw new Error(RATE_LIMITED)
    const text = (await res.text()).trim()
    throw new Error(text || `request failed: ${res.status}`)
  }
  if (res.status === 204 || res.status === 202) return undefined as T
  const text = await res.text()
  return (text ? JSON.parse(text) : undefined) as T
}

// login accepts a username or an email address.
export function login(loginName: string, password: string): Promise<User> {
  return send<User>('POST', '/api/auth/login', { login: loginName, password })
}

export const authConfig = () => send<AuthConfig>('GET', '/api/auth/config')

// register returns the logged-in user, or {pending: true} when the
// account awaits admin approval.
export async function register(input: {
  email: string
  username: string
  display_name: string
  password: string
  invite?: string
}): Promise<User | { pending: true }> {
  return send<User | { pending: true }>('POST', '/api/auth/register', input)
}

export const checkInvite = (token: string) =>
  send<{ email: string }>('POST', '/api/auth/invite/check', { token })
export const verifyEmail = (token: string) => send('POST', '/api/auth/verify-email', { token })
export const forgotPassword = (email: string) => send('POST', '/api/auth/forgot', { email })
export const resetPassword = (token: string, password: string) =>
  send('POST', '/api/auth/reset', { token, password })

export const updateMe = (changes: { display_name?: string; email?: string; current_password?: string }) =>
  send<User>('PUT', '/api/me', changes)
export const changePassword = (current_password: string, new_password: string) =>
  send('PUT', '/api/me/password', { current_password, new_password })
export const resendVerification = () => send('POST', '/api/me/verify-email')
// Per-account UI preferences (theme etc.), merged by top-level key.
export const getPreferences = () => send<Record<string, unknown>>('GET', '/api/me/preferences')
export const updatePreferences = (patch: Record<string, unknown>) =>
  send<Record<string, unknown>>('PUT', '/api/me/preferences', patch)
export const listSessions = () => send<SessionInfo[]>('GET', '/api/me/sessions')
export const revokeSession = (id: string) => send('DELETE', `/api/me/sessions/${encodeURIComponent(id)}`)
export const revokeOtherSessions = () => send('DELETE', '/api/me/sessions')

export const adminListUsers = () => send<AdminUser[]>('GET', '/api/admin/users')
// Only works on a disabled account (409 otherwise); see adminDeleteUserHandler.
export const adminDeleteUser = (id: string) => send('DELETE', `/api/admin/users/${id}`)
export const adminCreateUser = (u: { username: string; email: string; display_name: string; password: string }) =>
  send<AdminUser>('POST', '/api/admin/users', u)
export const adminUpdateUser = (id: string, changes: { status?: 'active' | 'disabled'; is_admin?: boolean }) =>
  send<AdminUser>('PUT', `/api/admin/users/${id}`, changes)
export const adminResetLink = (id: string) =>
  send<{ path: string; expires_at: string }>('POST', `/api/admin/users/${id}/reset-link`)
export const adminListInvites = () => send<Invite[]>('GET', '/api/admin/invites')
export const adminCreateInvite = (i: { email?: string; group_id?: string; role?: string; expires_in_hours?: number }) =>
  send<Invite>('POST', '/api/admin/invites', i)
export const adminDeleteInvite = (id: string) => send('DELETE', `/api/admin/invites/${id}`)

export const pageActivity = (pageId: string) => send<ActivityEntry[]>('GET', `/api/pages/${pageId}/activity`)

// Page history (migration 0019): versions newest first, then one
// version's document. Restoring is an ordinary edit in the editor.
export interface PageVersion {
  id: string
  created_at: string
  updated_at: string
  user_name: string
}
export const listVersions = (pageId: string) => send<PageVersion[]>('GET', `/api/pages/${pageId}/versions`)
export const getVersion = (pageId: string, versionId: string) =>
  send<{ content_json: string }>('GET', `/api/pages/${pageId}/versions/${versionId}`)
/** Pages that link to this one (only those the caller can see). */
export const listBacklinks = (pageId: string) =>
  send<{ id: string; title: string; icon: string }[]>('GET', `/api/pages/${pageId}/backlinks`)

export async function logout(): Promise<void> {
  await fetch('/api/auth/logout', { method: 'POST' })
}

// me returns the current session's user, or null if there isn't one
// (a 401 here just means "not logged in", not an error).
export async function me(): Promise<User | null> {
  const res = await fetch('/api/me')
  if (res.status === 401) return null
  return parseJSONOrThrow<User>(res)
}

// The whole sidebar in one request (GET /api/sidebar): the caller's
// groups and spaces, in their own order, each space with its page tree.
export interface SidebarPayload {
  // role: the caller's own role, so the UI offers only what the server allows.
  groups: (Group & { role: GroupMember['role'] })[]
  spaces: (Space & { role: GroupMember['role']; pages: TreeNode[] })[]
}
export const getSidebar = () => send<SidebarPayload>('GET', '/api/sidebar')

// Creates a new space inside groupId, owned by the caller. type is
// always 'private' today -- a *shared* space's sharing now happens at
// the group level (inviting someone there grants them every space in
// it), not per-space.
export function createSpace(name: string, type: Space['type'], groupId: string): Promise<Space> {
  return send<Space>('POST', '/api/spaces', { name, type, group_id: groupId })
}

export function updateSpace(
  spaceId: string,
  changes: { name?: string; icon?: string },
): Promise<Space> {
  const body: Record<string, string> = {}
  if (changes.name !== undefined) body.name = changes.name
  if (changes.icon !== undefined) body.icon = changes.icon
  return send<Space>('PUT', `/api/spaces/${spaceId}`, body)
}

export async function archiveSpace(spaceId: string): Promise<void> {
  await send('DELETE', `/api/spaces/${spaceId}`)
}

// spaceIds must be exactly the caller's own current spaces in their new
// order -- see reorderSpacesHandler's own comment on the backend.
export async function reorderSpaces(spaceIds: string[]): Promise<void> {
  await send('PUT', '/api/spaces/order', { space_ids: spaceIds })
}

// Moves spaceId to a different group -- see moveSpaceHandler's own
// comment on the backend for the "editor+ on both the source and
// destination group" requirement.
export function moveSpaceToGroup(spaceId: string, groupId: string): Promise<Space> {
  return send<Space>('PUT', `/api/spaces/${spaceId}/move`, { group_id: groupId })
}

// -- Groups (migrations/0009_groups.sql) --------------------------------

export function listGroups(): Promise<Group[]> {
  return send<Group[]>('GET', '/api/groups')
}

export function createGroup(name: string): Promise<Group> {
  return send<Group>('POST', '/api/groups', { name })
}

export function updateGroup(groupId: string, changes: { name?: string; icon?: string }): Promise<Group> {
  const body: Record<string, string> = {}
  if (changes.name !== undefined) body.name = changes.name
  if (changes.icon !== undefined) body.icon = changes.icon
  return send<Group>('PUT', `/api/groups/${groupId}`, body)
}

// Throws (message surfaced as-is) with a 409 specifically when the
// group still has spaces in it -- see groups.Store.Archive's own
// comment on the backend for why that's refused rather than orphaning
// them.
export async function archiveGroup(groupId: string): Promise<void> {
  await send('DELETE', `/api/groups/${groupId}`)
}

export async function reorderGroups(groupIds: string[]): Promise<void> {
  await send('PUT', '/api/groups/order', { group_ids: groupIds })
}

export function listGroupMembers(groupId: string): Promise<GroupMember[]> {
  return send<GroupMember[]>('GET', `/api/groups/${groupId}/members`)
}

// Adds an existing account (by username) to groupId with role. role can
// never rank higher than the caller's own role in the group (enforced
// on the backend); server errors ("no such user", "already a member",
// ...) are surfaced as-is.
export function inviteGroupMember(
  groupId: string,
  invite: { username: string; role: GroupMember['role'] },
): Promise<GroupMember> {
  return send<GroupMember>('POST', `/api/groups/${groupId}/members`, invite)
}

export async function updateGroupMemberRole(
  groupId: string,
  userId: string,
  role: GroupMember['role'],
): Promise<void> {
  await send('PUT', `/api/groups/${groupId}/members/${userId}`, { role })
}

export async function removeGroupMember(groupId: string, userId: string): Promise<void> {
  await send('DELETE', `/api/groups/${groupId}/members/${userId}`)
}

// SearchResult: one page hit from GET /api/search (backend
// internal/search, ADR 0007). title_marked and snippet wrap matched
// terms in \u0002...\u0003 markers -- split them with splitMarked
// (SidebarSearch.tsx), never render them as HTML.
export interface SearchResult {
  page_id: string
  space_id: string
  parent_page_id: string | null
  kind: PageKind
  icon: string
  title: string
  title_marked: string
  snippet: string
}

export function searchPages(query: string, signal?: AbortSignal): Promise<SearchResult[]> {
  return fetch(`/api/search?q=${encodeURIComponent(query)}`, { signal }).then(parseJSONOrThrow<SearchResult[]>)
}

// One entry of a collection's list (GET /api/pages/{id}/items).
export interface CollectionItem {
  id: string
  title: string
  icon: string
  kind: PageKind
  created_at: string
  updated_at: string
  updated_by_name: string
}
export const listCollectionItems = (collectionId: string) =>
  send<CollectionItem[]>('GET', `/api/pages/${collectionId}/items`)

export function getPage(pageId: string): Promise<Page> {
  return send<Page>('GET', `/api/pages/${pageId}`)
}

export function createPage(
  spaceId: string,
  opts: { parentPageId?: string; title?: string; kind?: PageKind },
): Promise<Page> {
  return send<Page>('POST', `/api/spaces/${spaceId}/pages`, {
    parent_page_id: opts.parentPageId ?? null,
    title: opts.title ?? '',
    kind: opts.kind ?? 'document',
  })
}

export function updatePage(
  pageId: string,
  changes: { title?: string; icon?: string; contentJson?: string },
): Promise<Page> {
  const body: Record<string, string> = {}
  if (changes.title !== undefined) body.title = changes.title
  if (changes.icon !== undefined) body.icon = changes.icon
  if (changes.contentJson !== undefined) body.content_json = changes.contentJson

  return send<Page>('PUT', `/api/pages/${pageId}`, body)
}

export async function archivePage(pageId: string): Promise<void> {
  await send('DELETE', `/api/pages/${pageId}`)
}

// pageIds must be exactly parentPageId's current children within
// spaceId (its top-level pages if parentPageId is null) -- see
// reorderPageSiblingsHandler's own comment on the backend.
export async function reorderPageSiblings(
  spaceId: string,
  parentPageId: string | null,
  pageIds: string[],
): Promise<void> {
  await send('PUT', `/api/spaces/${spaceId}/pages/order`, { parent_page_id: parentPageId, page_ids: pageIds })
}

// Moves pageId to be the last child of (spaceId, parentPageId) --
// reparenting within the same space, or to a different space entirely
// (with or without a parent there; a page moving to a space in a
// different group is the identical call, a page is never parented to
// a group directly). Throws with the server's own message on refusal
// (self-parent, circular hierarchy, parent in a different space) --
// see movePageHandler's own comment on the backend.
export function movePage(
  pageId: string,
  spaceId: string,
  parentPageId: string | null,
): Promise<Page> {
  return send<Page>('PUT', `/api/pages/${pageId}/move`, { space_id: spaceId, parent_page_id: parentPageId })
}

// -- Database pages (ADR 0004) --------------------------------------

export function createProperty(
  pageId: string,
  opts: { name: string; type: PropertyType },
): Promise<Property> {
  return send<Property>('POST', `/api/pages/${pageId}/properties`, { name: opts.name, type: opts.type })
}

// columnIds must be exactly the database's current columns in their
// new order: every property id, plus TITLE_COLUMN_ID exactly once --
// see reorderColumnsHandler's own comment on the backend for why (no
// partial moves, the whole order is sent each drag-and-drop).
export const setBoardColumns = (pageId: string, propertyId: string, keys: string[]) =>
  send('PUT', `/api/pages/${pageId}/board-columns`, { property_id: propertyId, keys })
export const setBoardOrder = (pageId: string, rowIds: string[]) =>
  send('PUT', `/api/pages/${pageId}/board-order`, { row_ids: rowIds })

export async function reorderColumns(pageId: string, columnIds: string[]): Promise<void> {
  await send('PUT', `/api/pages/${pageId}/columns/order`, { column_ids: columnIds })
}

export function updateProperty(
  propertyId: string,
  changes: { name?: string; options?: SelectOption[] },
): Promise<Property> {
  const body: { name?: string; options?: SelectOption[] } = {}
  if (changes.name !== undefined) body.name = changes.name
  if (changes.options !== undefined) body.options = changes.options
  return send<Property>('PUT', `/api/properties/${propertyId}`, body)
}

export async function deleteProperty(propertyId: string): Promise<void> {
  await send('DELETE', `/api/properties/${propertyId}`)
}

export function listRows(pageId: string): Promise<DatabaseTable> {
  return send<DatabaseTable>('GET', `/api/pages/${pageId}/rows`)
}

// value is sent verbatim as JSON -- its shape must match the owning
// property's type (see the Row.values doc comment above).
export async function setRowProperty(rowId: string, propertyId: string, value: unknown): Promise<void> {
  await send('PUT', `/api/pages/${rowId}/properties/${propertyId}`, { value })
}

// -- Page cover images -------------------------------------------------
// Deliberately the last piece of ADR 0004, riding on the minimal
// internal/assets file storage that only exists because this needed
// it.

export interface CoverMeta {
  id: string
  filename: string
  mime_type: string
  size_bytes: number
  // Which vertical slice of the image shows through its fixed-height
  // crop window, 0-100 -- rendered as object-position: center <v>%.
  // 50 (centered) both for a freshly uploaded cover and anything set
  // before this field existed.
  position_y: number
}

export function pageCoverUrl(pageId: string): string {
  return `/api/pages/${pageId}/cover`
}

// getPageCoverMeta is the "does this page have a cover, and if so
// what's its position" check -- null (not an error) when there isn't
// one, same as me() treating a 401 as "not logged in" rather than
// throwing.
export async function getPageCoverMeta(pageId: string): Promise<CoverMeta | null> {
  const res = await fetch(`${pageCoverUrl(pageId)}/meta`)
  // 204 = no cover (404 from servers before 2026-10-03).
  if (res.status === 204 || res.status === 404) return null
  return parseJSONOrThrow<CoverMeta>(res)
}

export function uploadPageCover(pageId: string, file: File): Promise<CoverMeta> {
  const form = new FormData()
  form.append('file', file)
  return fetch(pageCoverUrl(pageId), { method: 'PUT', body: form }).then(parseJSONOrThrow<CoverMeta>)
}

export async function deletePageCover(pageId: string): Promise<void> {
  await send('DELETE', pageCoverUrl(pageId))
}

export async function setPageCoverPosition(pageId: string, positionY: number): Promise<void> {
  await send('PUT', `${pageCoverUrl(pageId)}/position`, { position_y: positionY })
}

// -- Images inside a page's text -----------------------------------------
// Uploaded once, then referenced from the Tiptap document by url --
// the bytes never go into content_json or the Yjs history.

export interface PageImage {
  id: string
  url: string
  mime_type: string
  size_bytes: number
}

export function uploadPageImage(pageId: string, file: Blob, filename: string): Promise<PageImage> {
  const form = new FormData()
  form.append('file', file, filename)
  return fetch(`/api/pages/${pageId}/images`, { method: 'POST', body: form }).then(parseJSONOrThrow<PageImage>)
}

// -- Archive -------------------------------------------------------------

export interface ArchivedPage {
  id: string
  title: string
  icon: string
  kind: PageKind
  space_id: string
  space_name: string
  parent_title: string
  archived_at: string
  archived_by: string
  /** Sub-pages archived together with this one; they come back with it. */
  sub_pages: number
}

export interface ArchivedSpace {
  id: string
  name: string
  icon: string
  archived_at: string
  group_name: string
  /** The group is archived too; restoring the space restores it as well. */
  group_archived: boolean
  /** Pages inside, which come back with the space. */
  pages: number
}

export interface ArchivedGroup {
  id: string
  name: string
  icon: string
  archived_at: string
}

export interface ArchiveList {
  pages: ArchivedPage[]
  spaces: ArchivedSpace[]
  groups: ArchivedGroup[]
}

export const listArchive = () => send<ArchiveList>('GET', '/api/archive')
export const restorePage = (id: string) => send('POST', `/api/pages/${id}/restore`)
export const restoreSpace = (id: string) => send('POST', `/api/spaces/${id}/restore`)
export const restoreGroup = (id: string) => send('POST', `/api/groups/${id}/restore`)
// Permanent: only archived items (409 otherwise).
export const deleteArchivedPage = (id: string) => send('DELETE', `/api/archive/pages/${id}`)
export const deleteArchivedSpace = (id: string) => send('DELETE', `/api/archive/spaces/${id}`)
export const deleteArchivedGroup = (id: string) => send('DELETE', `/api/archive/groups/${id}`)

// -- Files attached inside a page's text ---------------------------------

export interface PageAttachment {
  id: string
  url: string
  filename: string
  mime_type: string
  size_bytes: number
}

export function uploadPageAttachment(pageId: string, file: File): Promise<PageAttachment> {
  const form = new FormData()
  form.append('file', file, file.name)
  return fetch(`/api/pages/${pageId}/attachments`, { method: 'POST', body: form }).then(
    parseJSONOrThrow<PageAttachment>,
  )
}

// -- People (who a "Person" database cell can pick) -----------------------

export interface Person {
  id: string
  username: string
  display_name: string
}

export const listPeople = (databasePageId: string) => send<Person[]>('GET', `/api/pages/${databasePageId}/people`)
