package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func openRaw(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestRunMigrationsFreshCreatesSchemaAndRecordsVersion(t *testing.T) {
	db := openRaw(t)
	if err := runMigrations(db); err != nil {
		t.Fatal(err)
	}
	var v int
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != latestVersion() {
		t.Fatalf("version = %d, want %d", v, latestVersion())
	}
	// nodes table exists with the display_mode column.
	if _, err := db.Exec(`INSERT INTO nodes (parent_id,name,path,type,created_at,updated_at) VALUES (0,'n','/p',1,0,0)`); err != nil {
		t.Fatalf("insert into nodes: %v", err)
	}
	var mode string
	if err := db.QueryRow(`SELECT display_mode FROM nodes WHERE path='/p'`).Scan(&mode); err != nil {
		t.Fatalf("select display_mode: %v", err)
	}
	if mode != "single" {
		t.Fatalf("display_mode default = %q, want single", mode)
	}
}

func TestRunMigrationsIsIdempotent(t *testing.T) {
	db := openRaw(t)
	if err := runMigrations(db); err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(db); err != nil {
		t.Fatalf("second run: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != latestVersion() {
		t.Fatalf("migration rows = %d, want %d", n, latestVersion())
	}
}

func TestRunMigrationsBaselinesLegacyDB(t *testing.T) {
	db := openRaw(t)
	// Simulate a pre-migration database: tables already present, no schema_migrations.
	if _, err := db.Exec(schemaV1); err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(db); err != nil {
		t.Fatal(err)
	}
	var v int
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != latestVersion() {
		t.Fatalf("legacy baseline version = %d, want %d", v, latestVersion())
	}
}
