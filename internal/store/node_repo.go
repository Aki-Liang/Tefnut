package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrNotFound = errors.New("store: not found")

const defaultPageLimit = 500

// appendLimit appends a LIMIT/OFFSET clause to sb and args. limit<0 means no
// limit; limit==0 means the default cap (defaultPageLimit).
func appendLimit(sb *strings.Builder, args *[]any, limit, offset int) {
	if limit < 0 {
		return
	}
	if limit == 0 {
		limit = defaultPageLimit
	}
	sb.WriteString(` LIMIT ? OFFSET ?`)
	*args = append(*args, limit, offset)
}

type NodeRepo struct {
	db *sql.DB
}

func NewNodeRepo(db *DB) *NodeRepo { return &NodeRepo{db: db.SQL()} }

func scanNode(s interface{ Scan(...any) error }) (*Node, error) {
	n := &Node{}
	if err := s.Scan(nodeScanTargets(n)...); err != nil {
		return nil, err
	}
	return n, nil
}

func (r *NodeRepo) Create(ctx context.Context, n *Node) (*Node, error) {
	now := time.Now().Unix()
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO nodes (parent_id, name, path, type, page_count, cover_status,
			author, rating, size, mtime, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		n.ParentID, n.Name, n.Path, n.Type, n.PageCount, n.CoverStatus,
		n.Author, n.Rating, n.Size, n.MTime, now, now)
	if err != nil {
		return nil, fmt.Errorf("store: create node %q: %w", n.Path, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	out := *n
	out.ID = id
	out.CreatedAt, out.UpdatedAt = now, now
	return &out, nil
}

func (r *NodeRepo) Get(ctx context.Context, id int64) (*Node, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+nodeCols+` FROM nodes WHERE id = ?`, id)
	n, err := scanNode(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get node %d: %w", id, err)
	}
	return n, nil
}

func (r *NodeRepo) ListChildren(ctx context.Context, parentID int64, limit, offset int) ([]*Node, error) {
	var sb strings.Builder
	args := []any{parentID}
	sb.WriteString(`SELECT ` + nodeCols + ` FROM nodes WHERE parent_id = ? ORDER BY type DESC, name ASC`)
	appendLimit(&sb, &args, limit, offset)
	rows, err := r.db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("store: list children %d: %w", parentID, err)
	}
	return collectNodes(rows)
}

func (r *NodeRepo) Search(ctx context.Context, q string, tagID int64, minRating, limit, offset int) ([]*Node, error) {
	var sb strings.Builder
	args := []any{}
	sb.WriteString(`SELECT `)
	sb.WriteString(nodeColsPrefixed("n"))
	sb.WriteString(` FROM nodes n`)
	if tagID > 0 {
		sb.WriteString(` JOIN node_tags nt ON nt.node_id = n.id AND nt.tag_id = ?`)
		args = append(args, tagID)
	}
	sb.WriteString(` WHERE n.type = ?`)
	args = append(args, NodeComic)
	if q != "" {
		sb.WriteString(` AND n.name LIKE ?`)
		args = append(args, "%"+q+"%")
	}
	if minRating > 0 {
		sb.WriteString(` AND n.rating >= ?`)
		args = append(args, minRating)
	}
	sb.WriteString(` ORDER BY n.name ASC`)
	appendLimit(&sb, &args, limit, offset)
	rows, err := r.db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("store: search: %w", err)
	}
	return collectNodes(rows)
}

func (r *NodeRepo) UpdateFileAttrs(ctx context.Context, id, size, mtime int64, pageCount, coverStatus int) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE nodes SET size=?, mtime=?, page_count=?, cover_status=?, updated_at=? WHERE id=?`,
		size, mtime, pageCount, coverStatus, time.Now().Unix(), id)
	if err != nil {
		return fmt.Errorf("store: update file attrs %d: %w", id, err)
	}
	return nil
}

func (r *NodeRepo) UpdateName(ctx context.Context, id int64, name string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE nodes SET name=?, updated_at=? WHERE id=?`, name, time.Now().Unix(), id)
	if err != nil {
		return fmt.Errorf("store: update name %d: %w", id, err)
	}
	return nil
}

type NodePatch struct {
	Author           *string
	Rating           *int
	ReadingDirection *string
	DisplayMode      *string
}

// UpdateFields applies only the set fields of p to node id in one statement.
// An empty patch is a no-op.
func (r *NodeRepo) UpdateFields(ctx context.Context, id int64, p NodePatch) error {
	sets := []string{}
	args := []any{}
	if p.Author != nil {
		sets = append(sets, "author=?")
		args = append(args, *p.Author)
	}
	if p.Rating != nil {
		sets = append(sets, "rating=?")
		args = append(args, *p.Rating)
	}
	if p.ReadingDirection != nil {
		sets = append(sets, "reading_direction=?")
		args = append(args, *p.ReadingDirection)
	}
	if p.DisplayMode != nil {
		sets = append(sets, "display_mode=?")
		args = append(args, *p.DisplayMode)
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at=?")
	args = append(args, time.Now().Unix(), id)
	q := `UPDATE nodes SET ` + strings.Join(sets, ", ") + ` WHERE id=?`
	if _, err := r.db.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("store: update fields %d: %w", id, err)
	}
	return nil
}

func (r *NodeRepo) Delete(ctx context.Context, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM node_tags WHERE node_id=?`,
		`DELETE FROM progress WHERE node_id=?`,
		`DELETE FROM nodes WHERE id=?`,
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return fmt.Errorf("store: delete node %d: %w", id, err)
		}
	}
	return tx.Commit()
}

func collectNodes(rows *sql.Rows) ([]*Node, error) {
	defer rows.Close()
	out := []*Node{}
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
