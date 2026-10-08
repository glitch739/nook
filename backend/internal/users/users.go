// Package users manages Nook accounts: creation and lookup by username or
// ID. It deliberately knows nothing about passwords beyond storing
// whatever hash it's handed — hashing and verification live in
// internal/auth, which is the only package that should ever see a
// plaintext password.
package users

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"nook/internal/db"
)

// Account statuses (users.status, migration 0011).
const (
	StatusPending  = "pending"
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

// User is a Nook account.
type User struct {
	ID                 string
	Username           string
	PasswordHash       string
	DisplayName        string
	Email              string // "" only for accounts created before migration 0011
	EmailVerified      bool
	Status             string
	IsAdmin            bool
	MustChangePassword bool
	CreatedAt          string // ISO-8601 UTC, as stored; parse with time.Parse(time.RFC3339Nano, ...) if a time.Time is needed.
}

// ErrUsernameTaken is returned by Create when the username is already in use.
var ErrUsernameTaken = errors.New("users: username already taken")

// ErrEmailTaken is returned by Create/UpdateEmail when the email is already in use.
var ErrEmailTaken = errors.New("users: email already taken")

type Store struct {
	db *sql.DB
}

func NewStore(conn *sql.DB) *Store {
	return &Store{db: conn}
}

// NewUser is the input to CreateAccount.
type NewUser struct {
	Username           string // normalize with NormalizeUsername first
	PasswordHash       string // from auth.HashPassword
	DisplayName        string
	Email              string // normalize with NormalizeEmail first; "" allowed only for bootstrap
	Status             string // defaults to StatusActive
	IsAdmin            bool
	MustChangePassword bool
}

// Create inserts a new active user without an email. passwordHash must
// already be a hash produced by auth.HashPassword. Kept for bootstrap and
// tests; real sign-ups go through CreateAccount.
func (s *Store) Create(ctx context.Context, username, passwordHash, displayName string) (User, error) {
	return s.CreateAccount(ctx, NewUser{Username: username, PasswordHash: passwordHash, DisplayName: displayName})
}

// CreateAccount inserts a new user.
func (s *Store) CreateAccount(ctx context.Context, n NewUser) (User, error) {
	if n.Status == "" {
		n.Status = StatusActive
	}
	id := db.NewID()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash, display_name, email, status, is_admin, must_change_password)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?);
	`, id, n.Username, n.PasswordHash, n.DisplayName, nullIfEmpty(n.Email), n.Status, n.IsAdmin, n.MustChangePassword)
	if err != nil {
		return User{}, s.mapUniqueErr(err, "create")
	}
	return s.GetByID(ctx, id)
}

const selectCols = `SELECT id, username, password_hash, display_name, email,
	email_verified_at IS NOT NULL, status, is_admin, must_change_password, created_at FROM users `

// GetByUsername looks up a user by username. Returns an error wrapping
// db.ErrNotFound if no such user exists.
func (s *Store) GetByUsername(ctx context.Context, username string) (User, error) {
	return s.scanOne(s.db.QueryRowContext(ctx, selectCols+`WHERE username = ?;`, NormalizeUsername(username)))
}

// GetByEmail looks up a user by (normalized) email.
func (s *Store) GetByEmail(ctx context.Context, email string) (User, error) {
	return s.scanOne(s.db.QueryRowContext(ctx, selectCols+`WHERE email = ?;`, NormalizeEmail(email)))
}

// GetByLogin looks up a user by username, or by email if login contains "@".
func (s *Store) GetByLogin(ctx context.Context, login string) (User, error) {
	if strings.Contains(login, "@") {
		return s.GetByEmail(ctx, login)
	}
	return s.GetByUsername(ctx, login)
}

// GetByID looks up a user by ID. Returns an error wrapping db.ErrNotFound
// if no such user exists.
func (s *Store) GetByID(ctx context.Context, id string) (User, error) {
	return s.scanOne(s.db.QueryRowContext(ctx, selectCols+`WHERE id = ?;`, id))
}

// List returns every account, oldest first (admin panel).
func (s *Store) List(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, selectCols+`ORDER BY created_at ASC;`)
	if err != nil {
		return nil, fmt.Errorf("users: list: %w", err)
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CountActiveAdmins backs the "never remove the last admin" guard.
func (s *Store) CountActiveAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE is_admin = 1 AND status = 'active';`).Scan(&n)
	return n, err
}

func (s *Store) exec(ctx context.Context, op, q string, args ...any) error {
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return s.mapUniqueErr(err, op)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("users: %s: %w", op, db.ErrNotFound)
	}
	return nil
}

func (s *Store) SetStatus(ctx context.Context, id, status string) error {
	return s.exec(ctx, "set status", `UPDATE users SET status = ? WHERE id = ?;`, status, id)
}

func (s *Store) SetAdmin(ctx context.Context, id string, admin bool) error {
	return s.exec(ctx, "set admin", `UPDATE users SET is_admin = ? WHERE id = ?;`, admin, id)
}

func (s *Store) UpdateDisplayName(ctx context.Context, id, name string) error {
	return s.exec(ctx, "update display name", `UPDATE users SET display_name = ? WHERE id = ?;`, name, id)
}

// UpdateEmail changes the address and clears its verified flag.
func (s *Store) UpdateEmail(ctx context.Context, id, email string) error {
	return s.exec(ctx, "update email", `UPDATE users SET email = ?, email_verified_at = NULL WHERE id = ?;`, NormalizeEmail(email), id)
}

func (s *Store) MarkEmailVerified(ctx context.Context, id string) error {
	return s.exec(ctx, "verify email", `UPDATE users SET email_verified_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?;`, id)
}

// UpdatePassword stores a new hash and clears must_change_password.
func (s *Store) UpdatePassword(ctx context.Context, id, passwordHash string) error {
	return s.exec(ctx, "update password", `UPDATE users SET password_hash = ?, must_change_password = 0 WHERE id = ?;`, passwordHash, id)
}

type scanner interface{ Scan(dest ...any) error }

func scan(row scanner) (User, error) {
	var u User
	var email sql.NullString
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &email,
		&u.EmailVerified, &u.Status, &u.IsAdmin, &u.MustChangePassword, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, fmt.Errorf("users: %w", db.ErrNotFound)
	}
	if err != nil {
		return User{}, fmt.Errorf("users: scan: %w", err)
	}
	u.Email = email.String
	return u, nil
}

func (s *Store) scanOne(row *sql.Row) (User, error) { return scan(row) }

func (s *Store) mapUniqueErr(err error, op string) error {
	if isUniqueConstraintErr(err) {
		if strings.Contains(err.Error(), "email") {
			return ErrEmailTaken
		}
		return ErrUsernameTaken
	}
	return fmt.Errorf("users: %s: %w", op, err)
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// isUniqueConstraintErr reports whether err is a SQLite UNIQUE constraint
// violation. modernc.org/sqlite doesn't export a typed sentinel for this,
// so we match on the driver's error text (stable across versions in
// practice — it's SQLite's own error string).
func isUniqueConstraintErr(err error) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
