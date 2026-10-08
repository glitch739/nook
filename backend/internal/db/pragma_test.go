package db

import (
	"path/filepath"
	"testing"
)

func TestOpenPragmas(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	checks := map[string]string{
		"journal_mode": "wal",
		"synchronous":  "1", // NORMAL
		"foreign_keys": "1",
		"temp_store":   "2", // MEMORY
		"cache_size":   "-32000",
		"busy_timeout": "5000",
	}
	for pragma, want := range checks {
		var got string
		if err := conn.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil {
			t.Fatalf("%s: %v", pragma, err)
		}
		if got != want {
			t.Errorf("PRAGMA %s = %s, want %s", pragma, got, want)
		}
	}
}
