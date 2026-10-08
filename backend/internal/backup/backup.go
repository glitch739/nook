// Package backup writes a consistent, self-contained snapshot of a Nook
// instance -- the SQLite database plus the uploads directory -- into one
// .tar.gz file, and prunes old snapshots.
//
// Safe to run while the server is up: the database is copied with
// SQLite's VACUUM INTO, which reads inside one transaction, so the
// snapshot is consistent even under concurrent writes (copying the .db
// file directly could catch it mid-write and miss the WAL).
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const filePrefix = "nook-backup-"

// Create writes <dir>/nook-backup-<local timestamp>.tar.gz containing
// nook.db and uploads/..., and returns its path. The file is written
// under a temporary name and renamed only once complete, so a crash
// never leaves a truncated archive that looks like a real backup.
func Create(ctx context.Context, conn *sql.DB, uploadsDir, dir string, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("backup dir: %w", err)
	}
	tmpDir, err := os.MkdirTemp(dir, ".tmp-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	dbCopy := filepath.Join(tmpDir, "nook.db")
	if _, err := conn.ExecContext(ctx, `VACUUM INTO ?;`, dbCopy); err != nil {
		return "", fmt.Errorf("snapshot database: %w", err)
	}

	// Local time (the container's TZ, Europe/Budapest in the shipped
	// .env), so the name matches the clock the user reads. Names still
	// sort chronologically for Prune, except within the one repeated
	// hour when DST ends; the nightly run at 03:30 never falls in it.
	name := filePrefix + now.Local().Format("20060102-150405") + ".tar.gz"
	final := filepath.Join(dir, name)
	partial := filepath.Join(tmpDir, name)
	f, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	if err := addFile(tw, dbCopy, "nook.db"); err != nil {
		f.Close()
		return "", err
	}
	if uploadsDir != "" {
		if _, err := os.Stat(uploadsDir); err == nil {
			err = filepath.WalkDir(uploadsDir, func(path string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() || !d.Type().IsRegular() {
					return err
				}
				rel, err := filepath.Rel(uploadsDir, path)
				if err != nil {
					return err
				}
				return addFile(tw, path, filepath.ToSlash(filepath.Join("uploads", rel)))
			})
			if err != nil {
				f.Close()
				return "", fmt.Errorf("archive uploads: %w", err)
			}
		}
	}

	if err := tw.Close(); err != nil {
		f.Close()
		return "", err
	}
	if err := gz.Close(); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(partial, final); err != nil {
		return "", err
	}
	return final, nil
}

func addFile(tw *tar.Writer, path, name string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	hdr := &tar.Header{Name: name, Mode: 0o600, Size: info.Size(), ModTime: info.ModTime()}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

// Prune deletes all but the newest keep backups in dir and returns the
// paths it removed. keep < 1 deletes nothing.
func Prune(dir string, keep int) ([]string, error) {
	if keep < 1 {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), filePrefix) && strings.HasSuffix(e.Name(), ".tar.gz") {
			names = append(names, e.Name())
		}
	}
	// The timestamp in the name sorts chronologically.
	sort.Strings(names)
	var removed []string
	for len(names) > keep {
		p := filepath.Join(dir, names[0])
		if err := os.Remove(p); err != nil {
			return removed, err
		}
		removed = append(removed, p)
		names = names[1:]
	}
	return removed, nil
}
