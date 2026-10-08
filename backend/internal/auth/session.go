package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"nook/internal/db"
)

// SessionTTL is how long a session stays valid after its last renewal.
const SessionTTL = 30 * 24 * time.Hour

// sessionRenewAfter: Get pushes expires_at forward at most this often
// (sliding expiry without a DB write on every request).
const sessionRenewAfter = 24 * time.Hour

// SessionMaxLifetime caps a session's total age, however actively it is
// used. Sliding expiry alone (SessionTTL) would let a stolen cookie
// stay valid forever as long as the thief keeps using it; this forces a
// fresh password login at least this often.
const SessionMaxLifetime = 90 * 24 * time.Hour

// Session is one row of the sessions table.
//
// The cookie carries Token (random, never stored); the DB row's id is
// sha256(Token), so a leaked DB or backup doesn't contain usable
// cookies. ID doubles as the public handle for "revoke this session".
type Session struct {
	ID         string
	Token      string // only set by Create
	UserID     string
	ExpiresAt  time.Time
	CreatedAt  string
	LastSeenAt string
	UserAgent  string
	IP         string
}

type SessionStore struct {
	db *sql.DB
}

func NewSessionStore(conn *sql.DB) *SessionStore {
	return &SessionStore{db: conn}
}

// HashToken returns the hex sha256 of a session token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewToken returns 32 random bytes, base64url-encoded.
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func nowStr() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// Create issues a new session for userID, valid for SessionTTL.
func (s *SessionStore) Create(ctx context.Context, userID string) (Session, error) {
	return s.CreateWithMeta(ctx, userID, "", "")
}

// CreateWithMeta is Create plus the user agent / IP shown in the
// profile's "active sessions" list.
func (s *SessionStore) CreateWithMeta(ctx context.Context, userID, userAgent, ip string) (Session, error) {
	token, err := NewToken()
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	id := HashToken(token)
	expiresAt := time.Now().UTC().Add(SessionTTL)
	now := nowStr()

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, expires_at, last_seen_at, user_agent, ip) VALUES (?, ?, ?, ?, ?, ?);`,
		id, userID, expiresAt.Format(time.RFC3339Nano), now, userAgent, ip)
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}
	return Session{ID: id, Token: token, UserID: userID, ExpiresAt: expiresAt, LastSeenAt: now, UserAgent: userAgent, IP: ip}, nil
}

// Get looks up a session by its cookie token.
//
// Expired sessions are deleted lazily here. A session last renewed more
// than sessionRenewAfter ago gets its expiry pushed forward.
func (s *SessionStore) Get(ctx context.Context, token string) (Session, error) {
	id := HashToken(token)
	var (
		sess      Session
		expiresAt string
		lastSeen  sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, expires_at, created_at, last_seen_at, user_agent, ip FROM sessions WHERE id = ?;`, id,
	).Scan(&sess.ID, &sess.UserID, &expiresAt, &sess.CreatedAt, &lastSeen, &sess.UserAgent, &sess.IP)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Session{}, fmt.Errorf("get session: %w", db.ErrNotFound)
		}
		return Session{}, fmt.Errorf("get session: %w", err)
	}
	sess.LastSeenAt = lastSeen.String

	expiry, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return Session{}, fmt.Errorf("parse session expiry: %w", err)
	}
	now := time.Now().UTC()
	if now.After(expiry) {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?;`, id); err != nil {
			return Session{}, fmt.Errorf("delete expired session: %w", err)
		}
		return Session{}, fmt.Errorf("get session: %w", db.ErrNotFound)
	}
	if created, err := time.Parse(time.RFC3339Nano, sess.CreatedAt); err == nil && now.Sub(created) > SessionMaxLifetime {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?;`, id); err != nil {
			return Session{}, fmt.Errorf("delete aged-out session: %w", err)
		}
		return Session{}, fmt.Errorf("get session: %w", db.ErrNotFound)
	}
	sess.ExpiresAt = expiry

	if last, err := time.Parse(time.RFC3339Nano, sess.LastSeenAt); err != nil || now.Sub(last) > sessionRenewAfter {
		sess.ExpiresAt = now.Add(SessionTTL)
		sess.LastSeenAt = now.Format(time.RFC3339Nano)
		// Best effort: a failed renewal must not fail the request.
		_, _ = s.db.ExecContext(ctx, `UPDATE sessions SET expires_at = ?, last_seen_at = ? WHERE id = ?;`,
			sess.ExpiresAt.Format(time.RFC3339Nano), sess.LastSeenAt, id)
	}
	return sess, nil
}

// Delete removes a session by its cookie token (logout). Idempotent.
func (s *SessionStore) Delete(ctx context.Context, token string) error {
	return s.DeleteByID(ctx, HashToken(token))
}

// DeleteByID removes a session by its public ID (hash). Idempotent.
func (s *SessionStore) DeleteByID(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?;`, id); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteForUser removes one of userID's sessions by public ID; reports
// whether it existed (so one user can't probe another's session IDs).
func (s *SessionStore) DeleteForUser(ctx context.Context, userID, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ? AND user_id = ?;`, id, userID)
	if err != nil {
		return false, fmt.Errorf("delete session: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeleteAllForUser removes every session of userID except exceptID
// (pass "" to remove all).
func (s *SessionStore) DeleteAllForUser(ctx context.Context, userID, exceptID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND id != ?;`, userID, exceptID); err != nil {
		return fmt.Errorf("delete user sessions: %w", err)
	}
	return nil
}

// ListForUser returns userID's unexpired sessions, most recent first.
func (s *SessionStore) ListForUser(ctx context.Context, userID string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, expires_at, created_at, COALESCE(last_seen_at, created_at), user_agent, ip
		FROM sessions WHERE user_id = ? AND expires_at > ?
		ORDER BY COALESCE(last_seen_at, created_at) DESC;`, userID, nowStr())
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var sess Session
		var exp string
		if err := rows.Scan(&sess.ID, &sess.UserID, &exp, &sess.CreatedAt, &sess.LastSeenAt, &sess.UserAgent, &sess.IP); err != nil {
			return nil, err
		}
		sess.ExpiresAt, _ = time.Parse(time.RFC3339Nano, exp)
		out = append(out, sess)
	}
	return out, rows.Err()
}
