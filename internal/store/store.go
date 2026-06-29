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

// Open opens the SQLite database at dsn and applies the schema.
func Open(dsn string) (*DB, error) {
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", dsn, err)
	}
	sqldb.SetMaxOpenConns(1)
	if _, err := sqldb.Exec(`PRAGMA busy_timeout = 5000; PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL;`); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("store: pragma: %w", err)
	}
	if err := runMigrations(sqldb); err != nil {
		sqldb.Close()
		return nil, err
	}
	return &DB{db: sqldb}, nil
}
