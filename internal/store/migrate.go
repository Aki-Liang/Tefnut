package store

import (
	"database/sql"
	"fmt"
)

// schemaV1 is the full base schema. A fresh database is created entirely by
// this migration. Existing pre-migration databases (created by the old
// schema+ensureColumn path) are already at this schema and are baselined, not
// re-run. NEVER edit schemaV1 to add columns to an existing table — append a
// new migration with the next version instead.
const schemaV1 = `
CREATE TABLE IF NOT EXISTS nodes (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  parent_id    INTEGER NOT NULL DEFAULT 0,
  name         TEXT    NOT NULL,
  path         TEXT    NOT NULL UNIQUE,
  type         INTEGER NOT NULL,
  page_count   INTEGER NOT NULL DEFAULT 0,
  cover_status INTEGER NOT NULL DEFAULT 0,
  author       TEXT    NOT NULL DEFAULT '',
  rating       INTEGER NOT NULL DEFAULT 0,
  size              INTEGER NOT NULL DEFAULT 0,
  mtime             INTEGER NOT NULL DEFAULT 0,
  created_at        INTEGER NOT NULL,
  updated_at        INTEGER NOT NULL,
  reading_direction TEXT    NOT NULL DEFAULT 'ltr',
  display_mode TEXT NOT NULL DEFAULT 'single'
);
CREATE INDEX IF NOT EXISTS idx_nodes_parent ON nodes(parent_id);

CREATE TABLE IF NOT EXISTS tags (
  id   INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS node_tags (
  node_id INTEGER NOT NULL,
  tag_id  INTEGER NOT NULL,
  PRIMARY KEY (node_id, tag_id)
);
CREATE INDEX IF NOT EXISTS idx_node_tags_tag ON node_tags(tag_id);

CREATE TABLE IF NOT EXISTS progress (
  node_id    INTEGER PRIMARY KEY,
  last_page  INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS library_paths (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  name       TEXT    NOT NULL,
  path       TEXT    NOT NULL UNIQUE,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
`

type migration struct {
	version int
	sql     string
}

// migrations is the ordered migration list. Append new entries with the next
// version number; never edit or reorder existing entries.
var migrations = []migration{
	{1, schemaV1},
}

func latestVersion() int { return migrations[len(migrations)-1].version }

func tableExists(db *sql.DB, name string) (bool, error) {
	var n int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("store: check table %s: %w", name, err)
	}
	return n > 0, nil
}

// runMigrations brings the database schema up to latestVersion(). A database
// that already has application tables but no schema_migrations table is treated
// as a legacy database at the latest schema and is baselined (no DDL re-run).
func runMigrations(db *sql.DB) error {
	hasMig, err := tableExists(db, "schema_migrations")
	if err != nil {
		return err
	}
	if !hasMig {
		hasNodes, err := tableExists(db, "nodes")
		if err != nil {
			return err
		}
		if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
			return fmt.Errorf("store: create schema_migrations: %w", err)
		}
		if hasNodes {
			for _, m := range migrations {
				if _, err := db.Exec(`INSERT INTO schema_migrations(version) VALUES (?)`, m.version); err != nil {
					return fmt.Errorf("store: baseline legacy db at %d: %w", m.version, err)
				}
			}
			return nil
		}
	}
	var current int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("store: read schema version: %w", err)
	}
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("store: begin migration %d: %w", m.version, err)
		}
		if _, err := tx.Exec(m.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: migration %d: %w", m.version, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version) VALUES (?)`, m.version); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: record migration %d: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: commit migration %d: %w", m.version, err)
		}
	}
	return nil
}
