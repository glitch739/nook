import { useEffect, useRef, useState } from 'react'
import {
  adminCreateInvite,
  adminCreateUser,
  adminDeleteInvite,
  adminListInvites,
  adminListUsers,
  adminResetLink,
  adminDeleteUser,
  adminUpdateUser,
  changePassword,
  listGroups,
  listSessions,
  resendVerification,
  revokeOtherSessions,
  revokeSession,
  updateMe,
  type AdminUser,
  type Group,
  type Invite,
  type SessionInfo,
  type User,
} from './api'
import { authErrorText } from './authErrors'
import Popover from './Popover'
import { ACCENTS, THEMES, type Appearance, type Base, type ThemeInfo, type ToggleOrigin } from './theme'
import './Settings.css'

type Tab = 'profile' | 'appearance' | 'security' | 'data' | 'users'

function fmtDate(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  return isNaN(d.getTime()) ? iso : d.toLocaleString('en-US', { dateStyle: 'medium', timeStyle: 'short' })
}

// Short, readable device name from a user agent string.
function deviceLabel(ua: string): string {
  if (!ua) return 'Unknown device'
  const browser = /Edg\//.test(ua)
    ? 'Edge'
    : /Firefox\//.test(ua)
      ? 'Firefox'
      : /Chrome\//.test(ua)
        ? 'Chrome'
        : /Safari\//.test(ua)
          ? 'Safari'
          : 'Browser'
  const os = /iPhone|iPad/.test(ua)
    ? 'iOS'
    : /Android/.test(ua)
      ? 'Android'
      : /Mac OS X/.test(ua)
        ? 'macOS'
        : /Windows/.test(ua)
          ? 'Windows'
          : /Linux/.test(ua)
            ? 'Linux'
            : ''
  return os ? `${browser} · ${os}` : browser
}

function useFlash() {
  const [msg, setMsg] = useState<{ text: string; error?: boolean } | null>(null)
  return {
    msg,
    ok: (text: string) => setMsg({ text }),
    fail: (e: unknown) => setMsg({ text: authErrorText(e), error: true }),
    clear: () => setMsg(null),
  }
}

function Flash({ msg }: { msg: { text: string; error?: boolean } | null }) {
  if (!msg) return null
  return <p className={msg.error ? 'error' : 'notice'}>{msg.text}</p>
}

// ---- Appearance ----

function Swatch({ colors }: { colors: ThemeInfo['swatch'] }) {
  const [bg, surface, accent, text] = colors
  return (
    <span className="theme-swatch" style={{ background: bg }} aria-hidden="true">
      <span style={{ background: surface }} />
      <span style={{ background: accent }} />
      <span style={{ background: text }} />
    </span>
  )
}

// "System": the chosen light and dark themes' backgrounds, split diagonally.
function SystemSwatch({ light, dark }: { light: string; dark: string }) {
  return (
    <span
      className="theme-swatch"
      style={{ background: `linear-gradient(135deg, ${light} 50%, ${dark} 50%)` }}
      aria-hidden="true"
    />
  )
}

const themeInfo = (id: string) => THEMES.find((t) => t.id === id)

// A dropdown like VS Code's / ChatGPT's theme picker: the current theme
// on a button, the choices in a popover, each with a color preview.
// `bases` limits the list (the System mode's light/dark pickers);
// `withSystem` adds the System entry on top.
function ThemePicker({
  value,
  bases,
  withSystem,
  appearance,
  label,
  onChange,
}: {
  value: string
  bases: Base[]
  withSystem: boolean
  appearance: Appearance
  label: string
  onChange: (id: string, origin: ToggleOrigin) => void
}) {
  const [anchor, setAnchor] = useState<HTMLButtonElement | null>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const lightBg = themeInfo(appearance.lightTheme)?.swatch[0] ?? '#f5f5f7'
  const darkBg = themeInfo(appearance.darkTheme)?.swatch[0] ?? '#191919'
  const current = themeInfo(value)

  function pick(id: string, e: React.MouseEvent<HTMLButtonElement>) {
    const r = e.currentTarget.getBoundingClientRect()
    setAnchor(null)
    onChange(id, { x: r.left + r.width / 2, y: r.top + r.height / 2 })
    buttonRef.current?.focus()
  }

  function option(id: string, text: string, swatch: React.ReactNode) {
    const selected = id === value
    return (
      <button
        key={id}
        type="button"
        role="option"
        aria-selected={selected}
        className={selected ? 'theme-option selected' : 'theme-option'}
        onClick={(e) => pick(id, e)}
      >
        {swatch}
        <span className="theme-option-label">{text}</span>
        {selected && <span className="theme-option-check">✓</span>}
      </button>
    )
  }

  return (
    <>
      <button
        ref={buttonRef}
        type="button"
        className="theme-picker-button"
        aria-haspopup="listbox"
        aria-expanded={anchor !== null}
        aria-label={label}
        onClick={(e) => setAnchor(anchor ? null : e.currentTarget)}
      >
        {current ? <Swatch colors={current.swatch} /> : <SystemSwatch light={lightBg} dark={darkBg} />}
        <span className="theme-option-label">{current?.label ?? 'System'}</span>
        <span className="theme-picker-caret" aria-hidden="true">
          ▾
        </span>
      </button>
      {anchor && (
        <Popover
          anchorEl={anchor}
          width={Math.max(anchor.offsetWidth, 240)}
          maxHeight={420}
          onClose={() => setAnchor(null)}
          className="theme-picker-popover"
        >
          <div role="listbox" aria-label={label} className="theme-picker-list">
            {withSystem && option('system', 'System', <SystemSwatch light={lightBg} dark={darkBg} />)}
            {bases.map((base) => (
              <div key={base} role="group" aria-label={base === 'light' ? 'Light' : 'Dark'}>
                {bases.length > 1 && <div className="theme-picker-group">{base === 'light' ? 'Light' : 'Dark'}</div>}
                {THEMES.filter((t) => t.base === base).map((t) => option(t.id, t.label, <Swatch colors={t.swatch} />))}
              </div>
            ))}
          </div>
        </Popover>
      )}
    </>
  )
}

function AccentPicker({ value, onChange }: { value: string; onChange: (id: string) => void }) {
  return (
    <div className="accent-picker" role="radiogroup" aria-label="Accent color">
      {ACCENTS.map((a) => (
        <button
          key={a.id}
          type="button"
          role="radio"
          aria-checked={a.id === value}
          title={a.label}
          aria-label={a.label}
          className={[
            'accent-dot',
            a.id === 'default' && 'accent-dot-default',
            a.id === value && 'selected',
          ]
            .filter(Boolean)
            .join(' ')}
          style={a.color ? { background: a.color } : undefined}
          onClick={() => onChange(a.id)}
        />
      ))}
    </div>
  )
}

function AppearanceTab({
  appearance,
  onChange,
}: {
  appearance: Appearance
  onChange: (patch: Partial<Appearance>, origin?: ToggleOrigin) => void
}) {
  const accentLabel = ACCENTS.find((a) => a.id === appearance.accent)?.label ?? ''
  return (
    <>
      <div className="settings-section">
        <h3>Theme</h3>
        <p className="hint">
          Saved to your account, so it follows you to other devices. Also in the command palette: type “Theme”.
        </p>
        <ThemePicker
          label="Theme"
          value={appearance.theme}
          bases={['light', 'dark']}
          withSystem
          appearance={appearance}
          onChange={(id, origin) => onChange({ theme: id }, origin)}
        />
        {appearance.theme === 'system' && (
          <div className="theme-system-pair">
            <label>
              When your device is light
              <ThemePicker
                label="Light theme"
                value={appearance.lightTheme}
                bases={['light']}
                withSystem={false}
                appearance={appearance}
                onChange={(id, origin) => onChange({ lightTheme: id }, origin)}
              />
            </label>
            <label>
              When your device is dark
              <ThemePicker
                label="Dark theme"
                value={appearance.darkTheme}
                bases={['dark']}
                withSystem={false}
                appearance={appearance}
                onChange={(id, origin) => onChange({ darkTheme: id }, origin)}
              />
            </label>
          </div>
        )}
      </div>
      <div className="settings-section">
        <h3>Accent color</h3>
        <p className="hint">Buttons and highlights. {accentLabel}.</p>
        <AccentPicker value={appearance.accent} onChange={(id) => onChange({ accent: id })} />
      </div>
    </>
  )
}

// ---- Profile ----

// Export everything you can see: Markdown per page (folders by group,
// space and parent page), images and attached files, plus a JSON file
// with the full data. Built by the server from your current access.
function DataTab() {
  return (
    <div className="settings-section">
      <h3>Export</h3>
      <p className="hint">
        Download every page you can access as Markdown files, with images, attached files and databases (as tables), plus a
        complete JSON copy. Archived pages are not included.
      </p>
      <a className="settings-primary settings-link-button" href="/api/export.zip" download>
        Download everything (ZIP)
      </a>
    </div>
  )
}

function ProfileTab({ user, onUserChange }: { user: User; onUserChange: (u: User) => void }) {
  const [displayName, setDisplayName] = useState(user.display_name)
  const [email, setEmail] = useState(user.email)
  const [password, setPassword] = useState('')
  const emailChanged = email.trim().toLowerCase() !== user.email
  const flash = useFlash()

  async function save(e: React.FormEvent) {
    e.preventDefault()
    flash.clear()
    try {
      const changes: { display_name?: string; email?: string; current_password?: string } = {}
      if (displayName.trim() !== user.display_name) changes.display_name = displayName.trim()
      if (emailChanged) {
        changes.email = email.trim()
        changes.current_password = password
      }
      if (changes.display_name === undefined && changes.email === undefined) return
      const u = await updateMe(changes)
      setPassword('')
      onUserChange(u)
      flash.ok(changes.email !== undefined ? 'Saved. We sent a confirmation link to the new address.' : 'Saved.')
    } catch (err) {
      flash.fail(err)
    }
  }

  return (
    <form className="settings-section" onSubmit={save}>
      <label>
        Username
        <input value={user.username} disabled />
      </label>
      <label>
        Display name
        <input value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
      </label>
      <label>
        Email address
        <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} />
      </label>
      {emailChanged && (
        <label>
          Current password (needed to change your email)
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            required
          />
        </label>
      )}
      {user.email && (
        <p className="hint">
          {user.email_verified ? (
            '✓ Confirmed address'
          ) : (
            <>
              Not confirmed yet.{' '}
              <button
                type="button"
                className="link-button"
                onClick={() =>
                  resendVerification()
                    .then(() => flash.ok('Confirmation email sent (if email sending is configured).'))
                    .catch(flash.fail)
                }
              >
                Resend email
              </button>
            </>
          )}
        </p>
      )}
      <Flash msg={flash.msg} />
      <div className="settings-actions">
        <button type="submit" className="settings-primary">
          Save
        </button>
      </div>
    </form>
  )
}

// ---- Password + sessions ----

export function PasswordForm({ onDone, forced }: { onDone?: () => void; forced?: boolean }) {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [next2, setNext2] = useState('')
  const flash = useFlash()

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    flash.clear()
    if (next !== next2) {
      flash.fail(new Error('The two new passwords do not match.'))
      return
    }
    try {
      await changePassword(current, next)
      setCurrent('')
      setNext('')
      setNext2('')
      flash.ok('Password changed. You have been signed out on other devices.')
      onDone?.()
    } catch (err) {
      flash.fail(err)
    }
  }

  return (
    <form className="settings-section" onSubmit={submit}>
      <h3>{forced ? 'Set your own password' : 'Change password'}</h3>
      <label>
        {forced ? 'Temporary password you received' : 'Current password'}
        <input type="password" value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" />
      </label>
      <label>
        New password (min. 8 characters)
        <input type="password" value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" />
      </label>
      <label>
        Confirm new password
        <input type="password" value={next2} onChange={(e) => setNext2(e.target.value)} autoComplete="new-password" />
      </label>
      <Flash msg={flash.msg} />
      <div className="settings-actions">
        <button type="submit" className="settings-primary">
          Save password
        </button>
      </div>
    </form>
  )
}

function SecurityTab() {
  const [sessions, setSessions] = useState<SessionInfo[] | null>(null)
  const flash = useFlash()
  const reload = () => listSessions().then(setSessions).catch(flash.fail)
  useEffect(() => {
    reload()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <>
      <PasswordForm onDone={reload} />
      <div className="settings-section">
        <h3>Active sessions</h3>
        <ul className="settings-list">
          {sessions?.map((s) => (
            <li key={s.id}>
              <div>
                <strong>{deviceLabel(s.user_agent)}</strong>
                {s.current && <span className="badge">this device</span>}
                <div className="hint">
                  {s.ip} · last seen: {fmtDate(s.last_seen_at)}
                </div>
              </div>
              {!s.current && (
                <button type="button" className="settings-secondary" onClick={() => revokeSession(s.id).then(reload).catch(flash.fail)}>
                  Sign out
                </button>
              )}
            </li>
          ))}
        </ul>
        <Flash msg={flash.msg} />
        {sessions && sessions.length > 1 && (
          <div className="settings-actions">
            <button type="button" className="settings-secondary" onClick={() => revokeOtherSessions().then(reload).catch(flash.fail)}>
              Sign out on all other devices
            </button>
          </div>
        )}
      </div>
    </>
  )
}

// ---- Admin: users + invites ----

const STATUS_LABELS: Record<AdminUser['status'], string> = {
  pending: 'Awaiting approval',
  active: 'Active',
  disabled: 'Disabled',
}

const ROLE_LABELS: Record<string, string> = {
  viewer: 'Viewer',
  editor: 'Editor',
  admin: 'Admin',
  owner: 'Owner',
}

function CopyLink({ path }: { path: string }) {
  const url = window.location.origin + path
  const [copied, setCopied] = useState(false)
  return (
    <div className="copy-link">
      <input readOnly value={url} onFocus={(e) => e.target.select()} />
      <button
        type="button"
        className="settings-secondary"
        onClick={() =>
          navigator.clipboard?.writeText(url).then(
            () => setCopied(true),
            () => setCopied(false),
          )
        }
      >
        {copied ? 'Copied ✓' : 'Copy'}
      </button>
    </div>
  )
}

function UsersTab({ me }: { me: User }) {
  const [users, setUsers] = useState<AdminUser[] | null>(null)
  const [invites, setInvites] = useState<Invite[]>([])
  const [groups, setGroups] = useState<Group[]>([])
  const [link, setLink] = useState<{ label: string; path: string } | null>(null)
  const flash = useFlash()

  const [invEmail, setInvEmail] = useState('')
  const [invGroup, setInvGroup] = useState('')
  const [invRole, setInvRole] = useState('editor')

  const [creating, setCreating] = useState(false)
  const [nu, setNu] = useState({ username: '', email: '', display_name: '', password: '' })

  const reload = () => {
    adminListUsers().then(setUsers).catch(flash.fail)
    adminListInvites().then(setInvites).catch(flash.fail)
  }
  useEffect(() => {
    reload()
    listGroups().then(setGroups).catch(() => {})
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const act = (p: Promise<unknown>) =>
    p.then(() => {
      flash.clear()
      reload()
    }, flash.fail)

  async function createInvite(e: React.FormEvent) {
    e.preventDefault()
    try {
      const inv = await adminCreateInvite({
        email: invEmail.trim() || undefined,
        group_id: invGroup || undefined,
        role: invGroup ? invRole : undefined,
      })
      setLink({ label: 'Invite link (valid 7 days, single use):', path: inv.path! })
      setInvEmail('')
      flash.clear()
      reload()
    } catch (err) {
      flash.fail(err)
    }
  }

  async function createUser(e: React.FormEvent) {
    e.preventDefault()
    try {
      await adminCreateUser(nu)
      setNu({ username: '', email: '', display_name: '', password: '' })
      setCreating(false)
      flash.ok('Account created. They will need to choose a new password on first login.')
      reload()
    } catch (err) {
      flash.fail(err)
    }
  }

  const groupName = (id: string) => groups.find((g) => g.id === id)?.name ?? '—'
  const pending = users?.filter((u) => u.status === 'pending') ?? []

  return (
    <>
      <Flash msg={flash.msg} />
      {link && (
        <div className="settings-section">
          <p className="hint">{link.label}</p>
          <CopyLink path={link.path} />
        </div>
      )}

      {pending.length > 0 && (
        <div className="settings-section">
          <h3>Awaiting approval ({pending.length})</h3>
          <ul className="settings-list">
            {pending.map((u) => (
              <li key={u.id}>
                <div>
                  <strong>{u.display_name}</strong> <span className="hint">@{u.username}</span>
                  <div className="hint">{u.email}</div>
                </div>
                <div className="row-actions">
                  <button type="button" className="settings-primary" onClick={() => act(adminUpdateUser(u.id, { status: 'active' }))}>
                    Approve
                  </button>
                  <button type="button" className="settings-secondary" onClick={() => act(adminUpdateUser(u.id, { status: 'disabled' }))}>
                    Reject
                  </button>
                </div>
              </li>
            ))}
          </ul>
        </div>
      )}

      <div className="settings-section">
        <h3>Users</h3>
        <ul className="settings-list">
          {users
            ?.filter((u) => u.status !== 'pending')
            .map((u) => (
              <li key={u.id} className={u.status === 'disabled' ? 'is-disabled' : ''}>
                <div>
                  <strong>{u.display_name}</strong> <span className="hint">@{u.username}</span>
                  {u.is_admin && <span className="badge">admin</span>}
                  {u.status !== 'active' && <span className="badge badge-warn">{STATUS_LABELS[u.status]}</span>}
                  <div className="hint">
                    {u.email || 'no email'}
                    {u.email && !u.email_verified && ' (unconfirmed)'}
                  </div>
                </div>
                {u.id !== me.id && (
                  <div className="row-actions">
                    <button
                      type="button"
                      className="settings-secondary"
                      onClick={() =>
                        adminResetLink(u.id).then(
                          (r) => setLink({ label: `Password reset link for ${u.display_name} (valid 24 hours):`, path: r.path }),
                          flash.fail,
                        )
                      }
                    >
                      Password link
                    </button>
                    <button type="button" className="settings-secondary" onClick={() => act(adminUpdateUser(u.id, { is_admin: !u.is_admin }))}>
                      {u.is_admin ? 'Remove admin' : 'Make admin'}
                    </button>
                    <button
                      type="button"
                      className="settings-secondary danger"
                      onClick={() => act(adminUpdateUser(u.id, { status: u.status === 'disabled' ? 'active' : 'disabled' }))}
                    >
                      {u.status === 'disabled' ? 'Enable' : 'Disable'}
                    </button>
                    {u.status === 'disabled' && (
                      <button
                        type="button"
                        className="settings-secondary danger"
                        onClick={() => {
                          if (
                            window.confirm(
                              `Permanently delete ${u.display_name} (@${u.username})?\n\n` +
                                'Their private groups, spaces and pages are deleted. ' +
                                'Anything they shared with others stays and is handed over to you. ' +
                                'This cannot be undone.',
                            )
                          ) {
                            act(adminDeleteUser(u.id))
                          }
                        }}
                      >
                        Delete
                      </button>
                    )}
                  </div>
                )}
              </li>
            ))}
        </ul>
        {creating ? (
          <form className="settings-grid" onSubmit={createUser}>
            <input placeholder="Username" value={nu.username} onChange={(e) => setNu({ ...nu, username: e.target.value.toLowerCase() })} />
            <input placeholder="Email" type="email" value={nu.email} onChange={(e) => setNu({ ...nu, email: e.target.value })} />
            <input placeholder="Display name" value={nu.display_name} onChange={(e) => setNu({ ...nu, display_name: e.target.value })} />
            <input placeholder="Temporary password" type="text" value={nu.password} onChange={(e) => setNu({ ...nu, password: e.target.value })} />
            <div className="settings-actions">
              <button type="button" className="settings-secondary" onClick={() => setCreating(false)}>
                Cancel
              </button>
              <button type="submit" className="settings-primary">
                Create
              </button>
            </div>
          </form>
        ) : (
          <button type="button" className="link-button" onClick={() => setCreating(true)}>
            + Create account manually
          </button>
        )}
      </div>

      <div className="settings-section">
        <h3>Invites</h3>
        <form className="settings-grid" onSubmit={createInvite}>
          <input placeholder="Email (optional)" type="email" value={invEmail} onChange={(e) => setInvEmail(e.target.value)} />
          <select value={invGroup} onChange={(e) => setInvGroup(e.target.value)}>
            <option value="">No group</option>
            {groups.map((g) => (
              <option key={g.id} value={g.id}>
                {g.icon} {g.name}
              </option>
            ))}
          </select>
          {invGroup && (
            <select value={invRole} onChange={(e) => setInvRole(e.target.value)}>
              {Object.entries(ROLE_LABELS).map(([k, v]) => (
                <option key={k} value={k}>
                  {v}
                </option>
              ))}
            </select>
          )}
          <div className="settings-actions">
            <button type="submit" className="settings-primary">
              Create invite link
            </button>
          </div>
        </form>
        {invites.length > 0 && (
          <ul className="settings-list">
            {invites.map((i) => {
              const expired = !i.used_at && new Date(i.expires_at) < new Date()
              return (
                <li key={i.id} className={i.used_at || expired ? 'is-disabled' : ''}>
                  <div>
                    <strong>{i.email || 'Anyone with the link'}</strong>
                    {i.group_id && (
                      <span className="hint">
                        {' '}
                        → {groupName(i.group_id)} ({ROLE_LABELS[i.group_role] ?? i.group_role})
                      </span>
                    )}
                    <div className="hint">
                      {i.used_at ? `Used: ${fmtDate(i.used_at)}` : expired ? 'Expired' : `Expires: ${fmtDate(i.expires_at)}`}
                    </div>
                  </div>
                  {!i.used_at && (
                    <button type="button" className="settings-secondary" onClick={() => act(adminDeleteInvite(i.id))}>
                      {expired ? 'Delete' : 'Revoke'}
                    </button>
                  )}
                </li>
              )
            })}
          </ul>
        )}
      </div>
    </>
  )
}

export default function SettingsModal({
  user,
  initialTab = 'profile',
  appearance,
  onAppearanceChange,
  onUserChange,
  onClose,
}: {
  user: User
  initialTab?: Tab
  appearance: Appearance
  onAppearanceChange: (patch: Partial<Appearance>, origin?: ToggleOrigin) => void
  onUserChange: (u: User) => void
  onClose: () => void
}) {
  const [tab, setTab] = useState<Tab>(initialTab === 'users' && !user.is_admin ? 'profile' : initialTab)

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const tabs: [Tab, string][] = [
    ['profile', 'Profile'],
    ['appearance', 'Appearance'],
    ['security', 'Security'],
    ['data', 'Your data'],
    ...(user.is_admin ? ([['users', 'Users']] as [Tab, string][]) : []),
  ]

  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="settings-modal" role="dialog" aria-modal="true" aria-label="Settings">
        <nav className="settings-nav">
          <h2>Settings</h2>
          {tabs.map(([k, label]) => (
            <button key={k} type="button" className={tab === k ? 'active' : ''} onClick={() => setTab(k)}>
              {label}
            </button>
          ))}
        </nav>
        <div className="settings-body">
          <button type="button" className="settings-close" onClick={onClose} aria-label="Close">
            ×
          </button>
          {tab === 'profile' && <ProfileTab user={user} onUserChange={onUserChange} />}
          {tab === 'appearance' && <AppearanceTab appearance={appearance} onChange={onAppearanceChange} />}
          {tab === 'security' && <SecurityTab />}
          {tab === 'data' && <DataTab />}
          {tab === 'users' && user.is_admin && <UsersTab me={user} />}
        </div>
      </div>
    </div>
  )
}
