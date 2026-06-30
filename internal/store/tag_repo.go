package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var ErrDuplicate = errors.New("store: duplicate")

type TagRepo struct {
	rdb *sql.DB
	wdb *sql.DB
}

func NewTagRepo(db *DB) *TagRepo { return &TagRepo{rdb: db.Read(), wdb: db.Write()} }

func (r *TagRepo) Upsert(ctx context.Context, name string) (*Tag, error) {
	name = strings.TrimSpace(name)
	var t Tag
	err := r.wdb.QueryRowContext(ctx, `SELECT id, name FROM tags WHERE name = ?`, name).
		Scan(&t.ID, &t.Name)
	if err == nil {
		return &t, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("store: upsert tag lookup: %w", err)
	}
	res, err := r.wdb.ExecContext(ctx, `INSERT INTO tags (name) VALUES (?)`, name)
	if err != nil {
		return nil, fmt.Errorf("store: insert tag %q: %w", name, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("store: last insert id for tag %q: %w", name, err)
	}
	return &Tag{ID: id, Name: name}, nil
}

func (r *TagRepo) List(ctx context.Context) ([]*TagCount, error) {
	rows, err := r.rdb.QueryContext(ctx,
		`SELECT t.id, t.name, COUNT(nt.node_id)
		 FROM tags t LEFT JOIN node_tags nt ON nt.tag_id = t.id
		 GROUP BY t.id, t.name ORDER BY t.name ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list tags: %w", err)
	}
	defer rows.Close()
	out := []*TagCount{}
	for rows.Next() {
		tc := &TagCount{}
		if err := rows.Scan(&tc.ID, &tc.Name, &tc.Count); err != nil {
			return nil, fmt.Errorf("store: scan tag row: %w", err)
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}

func (r *TagRepo) Rename(ctx context.Context, id int64, name string) error {
	name = strings.TrimSpace(name)
	_, err := r.wdb.ExecContext(ctx, `UPDATE tags SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrDuplicate
		}
		return fmt.Errorf("store: rename tag %d: %w", id, err)
	}
	return nil
}

func (r *TagRepo) Delete(ctx context.Context, id int64) error {
	tx, err := r.wdb.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM node_tags WHERE tag_id = ?`, id); err != nil {
		return fmt.Errorf("store: delete tag links %d: %w", id, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tags WHERE id = ?`, id); err != nil {
		return fmt.Errorf("store: delete tag %d: %w", id, err)
	}
	return tx.Commit()
}

func (r *TagRepo) AddToNode(ctx context.Context, nodeID, tagID int64) error {
	_, err := r.wdb.ExecContext(ctx,
		`INSERT OR IGNORE INTO node_tags (node_id, tag_id) VALUES (?, ?)`, nodeID, tagID)
	if err != nil {
		return fmt.Errorf("store: add tag %d to node %d: %w", tagID, nodeID, err)
	}
	return nil
}

func (r *TagRepo) RemoveFromNode(ctx context.Context, nodeID, tagID int64) error {
	_, err := r.wdb.ExecContext(ctx,
		`DELETE FROM node_tags WHERE node_id = ? AND tag_id = ?`, nodeID, tagID)
	if err != nil {
		return fmt.Errorf("store: remove tag %d from node %d: %w", tagID, nodeID, err)
	}
	return nil
}

func (r *TagRepo) ListForNode(ctx context.Context, nodeID int64) ([]*Tag, error) {
	rows, err := r.rdb.QueryContext(ctx,
		`SELECT t.id, t.name FROM tags t
		 JOIN node_tags nt ON nt.tag_id = t.id
		 WHERE nt.node_id = ? ORDER BY t.name ASC`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("store: list tags for node %d: %w", nodeID, err)
	}
	defer rows.Close()
	out := []*Tag{}
	for rows.Next() {
		t := &Tag{}
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, fmt.Errorf("store: scan tag row: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
