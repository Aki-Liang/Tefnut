package store

import (
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) *DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMigrateCreatesTables(t *testing.T) {
	db := openTemp(t)
	for _, table := range []string{"nodes", "tags", "node_tags", "progress"} {
		var name string
		err := db.Write().QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %s missing: %v", table, err)
		}
	}
}

func TestReadWriteSplit(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "rw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if db.Read() == nil || db.Write() == nil {
		t.Fatal("read/write handles must be non-nil")
	}
	if db.Read() == db.Write() {
		t.Fatal("read and write handles must be distinct")
	}
	// A write is visible through the read handle.
	if _, err := db.Write().Exec(`INSERT INTO settings(key,value) VALUES('k','v')`); err != nil {
		t.Fatal(err)
	}
	var v string
	if err := db.Read().QueryRow(`SELECT value FROM settings WHERE key='k'`).Scan(&v); err != nil {
		t.Fatalf("read after write: %v", err)
	}
	if v != "v" {
		t.Fatalf("read value = %q, want v", v)
	}
	// The read handle is read-only.
	if _, err := db.Read().Exec(`INSERT INTO settings(key,value) VALUES('x','y')`); err == nil {
		t.Fatal("expected write through read handle to fail (mode=ro)")
	}
}
