package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type DB struct {
	db *sql.DB
}

func (d *DB) SQL() *sql.DB { return d.db }

func (d *DB) Close() error { return d.db.Close() }

const schema = `
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
  size         INTEGER NOT NULL DEFAULT 0,
  mtime        INTEGER NOT NULL DEFAULT 0,
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL
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
`

// Open opens the SQLite database at dsn and applies the schema.
func Open(dsn string) (*DB, error) {
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", dsn, err)
	}
	if _, err := sqldb.Exec(`PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL;`); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("store: pragma: %w", err)
	}
	if _, err := sqldb.Exec(schema); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return &DB{db: sqldb}, nil
}
