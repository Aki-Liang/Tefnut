package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type ProgressRepo struct {
	db *sql.DB
}

func NewProgressRepo(db *DB) *ProgressRepo { return &ProgressRepo{db: db.SQL()} }

func (r *ProgressRepo) Get(ctx context.Context, nodeID int64) (int, error) {
	var page int
	err := r.db.QueryRowContext(ctx,
		`SELECT last_page FROM progress WHERE node_id = ?`, nodeID).Scan(&page)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("store: get progress %d: %w", nodeID, err)
	}
	return page, nil
}

func (r *ProgressRepo) Set(ctx context.Context, nodeID int64, page int) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO progress (node_id, last_page, updated_at) VALUES (?,?,?)
		 ON CONFLICT(node_id) DO UPDATE SET last_page=excluded.last_page, updated_at=excluded.updated_at`,
		nodeID, page, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("store: set progress %d: %w", nodeID, err)
	}
	return nil
}
