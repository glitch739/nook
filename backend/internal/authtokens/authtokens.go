// Package authtokens manages one-time secrets sent to people as links:
// email verification, password reset and sign-up invites (ADR 0005 §4).
// Only sha256(token) is stored; the token itself is returned exactly
// once, by Create.
package authtokens

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"nook/internal/auth"
	"nook/internal/db"
)

const (
	PurposeVerifyEmail = "verify_email"
	PurposeReset       = "reset"
	PurposeInvite      = "invite"
)

// Default lifetimes per purpose.
var TTL = map[string]time.Duration{
	PurposeVerifyEmail: 48 * time.Hour,
	PurposeReset:       time.Hour,
	PurposeInvite:      7 * 24 * time.Hour,
}

// ErrInvalid covers unknown, expired, already-used and wrong-purpose
// tokens alike — callers show one generic message for all of them.
var ErrInvalid = errors.New("authtokens: invalid or expired token")

type Token struct {
	ID        string
	Purpose   string
	UserID    string
	CreatedBy string
	GroupID   string
	GroupRole string
	Email     string
	ExpiresAt string
	UsedAt    string
	CreatedAt string
}

type Store struct{ db *sql.DB }

func NewStore(conn *sql.DB) *Store { return &Store{db: conn} }

// NewParams is the input to Create. TTL 0 means the purpose's default.
type NewParams struct {
	Purpose   string
	UserID    string
	CreatedBy string
	GroupID   string
	GroupRole string
	Email     string
	TTL       time.Duration
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// Create stores a new token and returns the secret (to put in a link).
// For verify/reset, any earlier unused token of the same purpose for
// the same user is invalidated, so only the newest link works.
func (s *Store) Create(ctx context.Context, p NewParams) (string, Token, error) {
	secret, err := auth.NewToken()
	if err != nil {
		return "", Token{}, err
	}
	ttl := p.TTL
	if ttl == 0 {
		ttl = TTL[p.Purpose]
	}
	if p.UserID != "" && p.Purpose != PurposeInvite {
		if _, err := s.db.ExecContext(ctx,
			`DELETE FROM auth_tokens WHERE user_id = ? AND purpose = ? AND used_at IS NULL;`, p.UserID, p.Purpose); err != nil {
			return "", Token{}, fmt.Errorf("authtokens: clear old: %w", err)
		}
	}
	id := db.NewID()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO auth_tokens (id, token_hash, purpose, user_id, created_by, group_id, group_role, email, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);`,
		id, auth.HashToken(secret), p.Purpose, nullable(p.UserID), nullable(p.CreatedBy),
		nullable(p.GroupID), nullable(p.GroupRole), nullable(p.Email),
		time.Now().UTC().Add(ttl).Format(time.RFC3339Nano))
	if err != nil {
		return "", Token{}, fmt.Errorf("authtokens: create: %w", err)
	}
	t, err := s.get(ctx, `WHERE id = ?`, id)
	return secret, t, err
}

const cols = `SELECT id, purpose, COALESCE(user_id,''), COALESCE(created_by,''), COALESCE(group_id,''),
	COALESCE(group_role,''), COALESCE(email,''), expires_at, COALESCE(used_at,''), created_at FROM auth_tokens `

func scan(row interface{ Scan(...any) error }) (Token, error) {
	var t Token
	err := row.Scan(&t.ID, &t.Purpose, &t.UserID, &t.CreatedBy, &t.GroupID, &t.GroupRole, &t.Email, &t.ExpiresAt, &t.UsedAt, &t.CreatedAt)
	return t, err
}

func (s *Store) get(ctx context.Context, where string, args ...any) (Token, error) {
	t, err := scan(s.db.QueryRowContext(ctx, cols+where+`;`, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Token{}, ErrInvalid
	}
	return t, err
}

// Peek returns the token if it's currently valid for purpose, without
// using it up (e.g. to show "this invite is valid" before sign-up).
func (s *Store) Peek(ctx context.Context, purpose, secret string) (Token, error) {
	return s.get(ctx, `WHERE token_hash = ? AND purpose = ? AND used_at IS NULL AND expires_at > ?`,
		auth.HashToken(secret), purpose, now())
}

// Consume atomically marks the token used and returns it. Two
// concurrent calls with the same secret: exactly one succeeds.
func (s *Store) Consume(ctx context.Context, purpose, secret string) (Token, error) {
	h := auth.HashToken(secret)
	res, err := s.db.ExecContext(ctx, `
		UPDATE auth_tokens SET used_at = ?
		WHERE token_hash = ? AND purpose = ? AND used_at IS NULL AND expires_at > ?;`,
		now(), h, purpose, now())
	if err != nil {
		return Token{}, fmt.Errorf("authtokens: consume: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Token{}, ErrInvalid
	}
	return s.get(ctx, `WHERE token_hash = ?`, h)
}

// SetUser records which account an invite was used for.
func (s *Store) SetUser(ctx context.Context, id, userID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE auth_tokens SET user_id = ? WHERE id = ?;`, userID, id)
	return err
}

// RecentlyIssued reports whether a token of purpose was created for
// userID within d — throttles "forgot password" per address.
func (s *Store) RecentlyIssued(ctx context.Context, userID, purpose string, d time.Duration) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_tokens WHERE user_id = ? AND purpose = ? AND created_at > ?;`,
		userID, purpose, time.Now().UTC().Add(-d).Format(time.RFC3339Nano)).Scan(&n)
	return n > 0, err
}

// ListInvites returns invites, newest first.
func (s *Store) ListInvites(ctx context.Context) ([]Token, error) {
	rows, err := s.db.QueryContext(ctx, cols+`WHERE purpose = 'invite' ORDER BY created_at DESC;`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Token
	for rows.Next() {
		t, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteInvite revokes an invite by ID.
func (s *Store) DeleteInvite(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM auth_tokens WHERE id = ? AND purpose = 'invite';`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return db.ErrNotFound
	}
	return nil
}
