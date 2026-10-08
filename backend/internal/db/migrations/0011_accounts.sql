-- 0011_accounts.sql
-- Phase 2 accounts: email, self-registration with account status,
-- instance admin flag, one-time tokens (email verify / password reset /
-- invite), session metadata, and "who changed what" tracking.
-- See docs/adr/0005-accounts-invites-profiles.md.

ALTER TABLE users ADD COLUMN email                TEXT;          -- stored lowercased; NULL only for pre-0011 accounts
ALTER TABLE users ADD COLUMN email_verified_at    TEXT;
ALTER TABLE users ADD COLUMN status               TEXT NOT NULL DEFAULT 'active'
    CHECK (status IN ('pending', 'active', 'disabled'));
ALTER TABLE users ADD COLUMN is_admin             INTEGER NOT NULL DEFAULT 0 CHECK (is_admin IN (0, 1));
ALTER TABLE users ADD COLUMN must_change_password INTEGER NOT NULL DEFAULT 0 CHECK (must_change_password IN (0, 1));

CREATE UNIQUE INDEX idx_users_email ON users(email) WHERE email IS NOT NULL;

-- The oldest account is the one bootstrapAdmin created: make it the
-- instance admin so an existing install doesn't lock itself out.
UPDATE users SET is_admin = 1
WHERE id = (SELECT id FROM users ORDER BY created_at ASC LIMIT 1);

CREATE TABLE auth_tokens (
    id          TEXT PRIMARY KEY,                 -- public handle (UUID), used in admin URLs
    token_hash  TEXT NOT NULL UNIQUE,             -- hex sha256 of the secret; the secret itself is never stored
    purpose     TEXT NOT NULL CHECK (purpose IN ('verify_email', 'reset', 'invite')),
    user_id     TEXT REFERENCES users(id) ON DELETE CASCADE,  -- verify/reset: target user; invite: NULL until used
    created_by  TEXT REFERENCES users(id) ON DELETE CASCADE,  -- NULL when self-requested (forgot password, signup)
    group_id    TEXT REFERENCES groups(id) ON DELETE SET NULL, -- invite only: optional auto-join
    group_role  TEXT CHECK (group_role IN ('owner', 'admin', 'editor', 'viewer')),
    email       TEXT,                             -- invite: optional pre-filled address
    expires_at  TEXT NOT NULL,
    used_at     TEXT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_auth_tokens_user_purpose ON auth_tokens(user_id, purpose);

ALTER TABLE sessions ADD COLUMN last_seen_at TEXT;
ALTER TABLE sessions ADD COLUMN user_agent   TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN ip           TEXT NOT NULL DEFAULT '';

-- Who changed what. pages.updated_by is the cheap "last edited by X";
-- activity is the append-only feed (per space / per page).
ALTER TABLE pages ADD COLUMN updated_by TEXT REFERENCES users(id) ON DELETE SET NULL;
UPDATE pages SET updated_by = created_by;

CREATE TABLE activity (
    id         TEXT PRIMARY KEY,
    actor_id   TEXT REFERENCES users(id) ON DELETE SET NULL,
    space_id   TEXT REFERENCES spaces(id) ON DELETE CASCADE,
    page_id    TEXT REFERENCES pages(id) ON DELETE CASCADE,
    action     TEXT NOT NULL,                     -- e.g. page.create, page.edit, page.move, page.archive, member.add
    meta_json  TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_activity_space ON activity(space_id, created_at);
CREATE INDEX idx_activity_page  ON activity(page_id, created_at);
