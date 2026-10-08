// Package db owns the SQLite connection and schema migrations.
//
// It intentionally does not know about domain tables beyond running the
// migration files in order; repository code for each domain (users,
// spaces, pages, ...) lives in its own internal package and takes a *sql.DB
// (or a narrower interface) from here.
package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open opens (creating if necessary) the SQLite database at path and
// applies pragmas suited to a single-process, low-resource server:
// WAL journaling for concurrent readers alongside a writer, foreign key
// enforcement (off by default in SQLite), and a busy timeout so concurrent
// writers back off instead of failing immediately.
func Open(path string) (*sql.DB, error) {
	// Per-connection settings, in the DSN so every connection gets them:
	//  - synchronous(NORMAL): with WAL this is still crash-safe for the
	//    database file (a power cut can only lose the last moments of
	//    commits, never corrupt it) and skips an fsync on every commit.
	//  - temp_store(MEMORY): sorts and temp indexes stay off the disk.
	//  - cache_size(-32000): ~32 MB page cache instead of the default ~2 MB.
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"+
		"&_pragma=synchronous(NORMAL)&_pragma=temp_store(MEMORY)&_pragma=cache_size(-32000)", path)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open %s: %w", path, err)
	}

	// SQLite only really supports one writer at a time; a single
	// connection avoids SQLITE_BUSY errors under concurrent requests
	// and is plenty for the target hardware.
	conn.SetMaxOpenConns(1)

	if _, err := conn.Exec(`PRAGMA journal_mode = WAL;`); err != nil {
		conn.Close()
		return nil, fmt.Errorf("db: set WAL mode: %w", err)
	}

	return conn, nil
}

// Migrate applies any migration files under migrations/ that have not yet
// been recorded in the schema_migrations table, in filename order. Each
// migration runs in its own transaction; a failed migration leaves the
// schema_migrations table showing exactly what did and didn't apply.
func Migrate(ctx context.Context, conn *sql.DB) error {
	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename   TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		);
	`); err != nil {
		return fmt.Errorf("db: create schema_migrations: %w", err)
	}

	applied := map[string]bool{}
	rows, err := conn.QueryContext(ctx, `SELECT filename FROM schema_migrations;`)
	if err != nil {
		return fmt.Errorf("db: read schema_migrations: %w", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return fmt.Errorf("db: scan schema_migrations: %w", err)
		}
		applied[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("db: read embedded migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		if applied[name] {
			continue
		}

		sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("db: read migration %s: %w", name, err)
		}

		if err := applyMigration(ctx, conn, name, string(sqlBytes)); err != nil {
			return err
		}
	}

	return nil
}

// ErrNotFound is returned by repository lookups when a row does not exist.
// Domain packages wrap it so callers can use errors.Is(err, db.ErrNotFound)
// regardless of which repository raised it.
var ErrNotFound = errors.New("not found")

// foreignKeysOffMarker, as a line in a migration file, runs that
// migration with foreign key enforcement off: needed to rebuild a table
// (SQLite's documented 12-step procedure, the only way to change e.g. a
// CHECK constraint). With enforcement on, dropping the old table would
// cascade-delete every row referencing it. The check is switched off
// only on a dedicated connection, and PRAGMA foreign_key_check must come
// back clean before the migration commits.
const foreignKeysOffMarker = "-- nook:foreign-keys-off"

func applyMigration(ctx context.Context, conn *sql.DB, name, script string) error {
	noFK := strings.Contains(script, foreignKeysOffMarker)
	c, err := conn.Conn(ctx)
	if err != nil {
		return fmt.Errorf("db: connection for %s: %w", name, err)
	}
	defer c.Close()
	if noFK {
		// Not changeable inside a transaction, hence before BeginTx.
		if _, err := c.ExecContext(ctx, `PRAGMA foreign_keys = OFF;`); err != nil {
			return fmt.Errorf("db: foreign keys off for %s: %w", name, err)
		}
		defer c.ExecContext(context.Background(), `PRAGMA foreign_keys = ON;`)
	}

	tx, err := c.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: begin tx for %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, script); err != nil {
		tx.Rollback()
		return fmt.Errorf("db: apply migration %s: %w", name, err)
	}
	if noFK {
		rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check;`)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("db: foreign key check for %s: %w", name, err)
		}
		bad := rows.Next()
		rows.Close()
		if bad {
			tx.Rollback()
			return fmt.Errorf("db: migration %s left foreign key violations; rolled back", name)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (filename) VALUES (?);`, name); err != nil {
		tx.Rollback()
		return fmt.Errorf("db: record migration %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: commit migration %s: %w", name, err)
	}
	return nil
}
