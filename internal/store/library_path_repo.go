package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type LibraryPath struct {
	ID   int64
	Name string
	Path string
}

type LibraryPathRepo struct {
	db *sql.DB
}

func NewLibraryPathRepo(db *DB) *LibraryPathRepo { return &LibraryPathRepo{db: db.SQL()} }

func (r *LibraryPathRepo) List(ctx context.Context) ([]*LibraryPath, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, name, path FROM library_paths ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list library paths: %w", err)
	}
	defer rows.Close()
	out := []*LibraryPath{}
	for rows.Next() {
		lp := &LibraryPath{}
		if err := rows.Scan(&lp.ID, &lp.Name, &lp.Path); err != nil {
			return nil, fmt.Errorf("store: scan library path: %w", err)
		}
		out = append(out, lp)
	}
	return out, rows.Err()
}

func (r *LibraryPathRepo) Get(ctx context.Context, id int64) (*LibraryPath, error) {
	lp := &LibraryPath{}
	err := r.db.QueryRowContext(ctx, `SELECT id, name, path FROM library_paths WHERE id = ?`, id).
		Scan(&lp.ID, &lp.Name, &lp.Path)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get library path %d: %w", id, err)
	}
	return lp, nil
}

func (r *LibraryPathRepo) Add(ctx context.Context, name, path string) (*LibraryPath, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO library_paths (name, path, created_at) VALUES (?, ?, ?)`,
		name, path, time.Now().Unix())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrDuplicate
		}
		return nil, fmt.Errorf("store: add library path %q: %w", path, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("store: add library path last id: %w", err)
	}
	return &LibraryPath{ID: id, Name: name, Path: path}, nil
}

func (r *LibraryPathRepo) Rename(ctx context.Context, id int64, name string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE library_paths SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		return fmt.Errorf("store: rename library path %d: %w", id, err)
	}
	return nil
}

func (r *LibraryPathRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM library_paths WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete library path %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete library path %d rows: %w", id, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
