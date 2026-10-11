import { useEffect, useState } from 'react'
import {
  inviteGroupMember,
  listGroupMembers,
  removeGroupMember,
  updateGroupMemberRole,
  type GroupMember,
} from './api'
import './GroupMembers.css'
import Popover from './Popover'

const ROLE_LABELS: Record<GroupMember['role'], string> = {
  viewer: 'Viewer',
  editor: 'Editor',
  admin: 'Admin',
  owner: 'Owner',
}

// A group's member list + invite form, self-contained like
// RowDetailPanel/RowPropertiesBar (fetches its own data, saves its own
// changes) rather than threading member state through App.tsx --
// nothing outside this popover ever needs to know who's in a group.
// Adding a member takes an existing account's username -- people sign
// up themselves (ADR 0005); every error the server can return (no such
// user, already a member, role above your own, can't demote/remove the
// last owner) is surfaced as-is via err.message rather than translated,
// since these are already the specific, actionable messages the
// backend chose to send.
export default function GroupMembersPopover({
  groupId,
  anchorEl,
  onClose,
}: {
  groupId: string
  anchorEl: HTMLElement
  onClose: () => void
}) {
  const [members, setMembers] = useState<GroupMember[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [inviting, setInviting] = useState(false)
  const [username, setUsername] = useState('')
  const [role, setRole] = useState<GroupMember['role']>('editor')

  function reload() {
    listGroupMembers(groupId)
      .then(setMembers)
      .catch((e) => setError(e instanceof Error ? e.message : 'Failed to load members'))
  }

  useEffect(() => {
    reload()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [groupId])

  async function submitInvite(e: React.FormEvent) {
    e.preventDefault()
    setError(null)
    try {
      await inviteGroupMember(groupId, { username: username.trim(), role })
      setUsername('')
      setInviting(false)
      reload()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to add member')
    }
  }

  async function handleRemove(userId: string) {
    if (!window.confirm('Remove this member from the group?')) return
    setError(null)
    try {
      await removeGroupMember(groupId, userId)
      reload()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to remove member')
    }
  }

  async function handleRoleChange(userId: string, newRole: GroupMember['role']) {
    setError(null)
    try {
      await updateGroupMemberRole(groupId, userId, newRole)
      reload()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update role')
    }
  }

  return (
    <Popover anchorEl={anchorEl} width={320} maxHeight={460} onClose={onClose} className="group-members-popover">
      <div className="group-members-title">Group members</div>
      {error && <p className="group-members-error">{error}</p>}

      {members === null ? (
        <p className="hint">Loading…</p>
      ) : (
        <div className="group-members-list">
          {members.map((m) => (
            <div className="group-member-row" key={m.user_id}>
              <div className="group-member-info">
                <span className="group-member-name">{m.display_name}</span>
                <span className="group-member-username">@{m.username}</span>
              </div>
              <select
                className="group-member-role"
                value={m.role}
                onChange={(e) => handleRoleChange(m.user_id, e.target.value as GroupMember['role'])}
              >
                {(Object.keys(ROLE_LABELS) as GroupMember['role'][]).map((r) => (
                  <option key={r} value={r}>
                    {ROLE_LABELS[r]}
                  </option>
                ))}
              </select>
              <button
                type="button"
                className="group-member-remove"
                onClick={() => handleRemove(m.user_id)}
                aria-label="Remove"
                title="Remove"
              >
                ×
              </button>
            </div>
          ))}
        </div>
      )}

      {inviting ? (
        <form className="group-invite-form" onSubmit={submitInvite}>
          <input
            className="group-invite-input"
            placeholder="Registered username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoFocus
          />
          <select
            className="group-invite-role"
            value={role}
            onChange={(e) => setRole(e.target.value as GroupMember['role'])}
          >
            {(Object.keys(ROLE_LABELS) as GroupMember['role'][]).map((r) => (
              <option key={r} value={r}>
                {ROLE_LABELS[r]}
              </option>
            ))}
          </select>
          <div className="group-invite-actions">
            <button type="submit" className="group-invite-submit">
              Invite
            </button>
            <button type="button" className="group-invite-cancel" onClick={() => setInviting(false)}>
              Cancel
            </button>
          </div>
        </form>
      ) : (
        <button type="button" className="group-invite-open" onClick={() => setInviting(true)}>
          + Add member
        </button>
      )}
    </Popover>
  )
}
