package auth

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"nook/internal/db"
	"nook/internal/dbtest"
	"nook/internal/users"
)

func newTestUser(t *testing.T, conn *sql.DB) users.User {
	t.Helper()
	u, err := users.NewStore(conn).Create(context.Background(), "alice", "irrelevant-hash", "Alice")
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	return u
}

func TestSessionStore_CreateAndGet(t *testing.T) {
	conn := dbtest.New(t)
	user := newTestUser(t, conn)
	store := NewSessionStore(conn)

	session, err := store.Create(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if session.UserID != user.ID {
		t.Fatalf("session.UserID = %q, want %q", session.UserID, user.ID)
	}
	if session.ExpiresAt.Before(time.Now()) {
		t.Fatal("session.ExpiresAt is in the past")
	}

	got, err := store.Get(context.Background(), session.Token)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.UserID != user.ID {
		t.Fatalf("Get returned UserID %q, want %q", got.UserID, user.ID)
	}
}

func TestSessionStore_GetUnknownID(t *testing.T) {
	conn := dbtest.New(t)
	store := NewSessionStore(conn)

	_, err := store.Get(context.Background(), "does-not-exist")
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("Get(unknown) error = %v, want wrapping db.ErrNotFound", err)
	}
}

func TestSessionStore_DeleteThenGet(t *testing.T) {
	conn := dbtest.New(t)
	user := newTestUser(t, conn)
	store := NewSessionStore(conn)

	session, err := store.Create(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.Delete(context.Background(), session.Token); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := store.Get(context.Background(), session.Token); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("Get after Delete: error = %v, want wrapping db.ErrNotFound", err)
	}
}

func TestSessionStore_DeleteUnknownIDIsNotAnError(t *testing.T) {
	conn := dbtest.New(t)
	store := NewSessionStore(conn)

	if err := store.Delete(context.Background(), "does-not-exist"); err != nil {
		t.Fatalf("Delete(unknown) = %v, want nil (logout must be idempotent)", err)
	}
}

// A session that is kept alive by constant use still ends once it is
// older than SessionMaxLifetime.
func TestSessionStore_MaxLifetime(t *testing.T) {
	conn := dbtest.New(t)
	user := newTestUser(t, conn)
	store := NewSessionStore(conn)
	ctx := context.Background()

	session, err := store.Create(ctx, user.ID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	old := time.Now().UTC().Add(-SessionMaxLifetime - time.Hour).Format(time.RFC3339Nano)
	if _, err := conn.ExecContext(ctx, `UPDATE sessions SET created_at = ? WHERE id = ?;`, old, session.ID); err != nil {
		t.Fatalf("age session: %v", err)
	}
	if _, err := store.Get(ctx, session.Token); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("Get on an aged-out session: err = %v, want ErrNotFound", err)
	}
	var n int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?;`, session.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("aged-out session row still present (n=%d, err=%v)", n, err)
	}
}
