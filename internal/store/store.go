package store

import (
	"database/sql"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const maxReadConns = 4

type DB struct {
	write *sql.DB
	read  *sql.DB
}

func (d *DB) Write() *sql.DB { return d.write }
func (d *DB) Read() *sql.DB  { return d.read }

func (d *DB) Close() error {
	rerr := d.read.Close()
	werr := d.write.Close()
	if werr != nil {
		return werr
	}
	return rerr
}

// Open opens the SQLite database at dsn and applies the schema.
func Open(dsn string) (*DB, error) {
	write, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", dsn, err)
	}
	write.SetMaxOpenConns(1)
	if _, err := write.Exec(`PRAGMA busy_timeout = 5000; PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL;`); err != nil {
		write.Close()
		return nil, fmt.Errorf("store: pragma: %w", err)
	}
	if err := runMigrations(write); err != nil {
		write.Close()
		return nil, err
	}
	abs, err := filepath.Abs(dsn)
	if err != nil {
		write.Close()
		return nil, fmt.Errorf("store: abs path %s: %w", dsn, err)
	}
	read, err := sql.Open("sqlite", "file:"+abs+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		write.Close()
		return nil, fmt.Errorf("store: open read %s: %w", dsn, err)
	}
	read.SetMaxOpenConns(maxReadConns)
	if err := read.Ping(); err != nil {
		read.Close()
		write.Close()
		return nil, fmt.Errorf("store: ping read %s: %w", dsn, err)
	}
	return &DB{write: write, read: read}, nil
}
