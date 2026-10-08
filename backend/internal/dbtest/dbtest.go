// Package dbtest provides a throwaway migrated SQLite database for tests.
// Only ever imported from _test.go files.
package dbtest

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"nook/internal/db"
)

// New opens a fresh, fully-migrated SQLite database backed by a file in
// t.TempDir() (not :memory: — modernc.org/sqlite's :memory: databases
// don't share state across the multiple connections database/sql may
// open, which has bitten people; a temp file sidesteps that entirely).
// The database is closed automatically via t.Cleanup.
func New(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")
	conn, err := db.Open(path)
	if err != nil {
		t.Fatalf("dbtest: open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	if err := db.Migrate(context.Background(), conn); err != nil {
		t.Fatalf("dbtest: migrate: %v", err)
	}
	return conn
}
