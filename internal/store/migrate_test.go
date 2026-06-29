package store

import (
	"context"
	"testing"
)

func TestEnsureColumnIdempotent(t *testing.T) {
	db := openTemp(t)
	// reading_direction already added by Open(); calling ensureColumn again must be a no-op (no error).
	if err := ensureColumn(db.SQL(), "nodes", "reading_direction", "TEXT NOT NULL DEFAULT 'ltr'"); err != nil {
		t.Fatalf("second ensureColumn: %v", err)
	}
	// a brand-new column should be added without error
	if err := ensureColumn(db.SQL(), "nodes", "extra_col", "TEXT NOT NULL DEFAULT ''"); err != nil {
		t.Fatalf("add extra_col: %v", err)
	}
	if err := ensureColumn(db.SQL(), "nodes", "extra_col", "TEXT NOT NULL DEFAULT ''"); err != nil {
		t.Fatalf("re-add extra_col: %v", err)
	}
}

func TestNodeDefaultReadingDirection(t *testing.T) {
	ctx := context.Background()
	r := NewNodeRepo(openTemp(t))
	n := mkNode(t, r, 0, "c", "/lib/c.zip", NodeComic)
	got, _ := r.Get(ctx, n.ID)
	if got.ReadingDirection != "ltr" {
		t.Fatalf("default direction = %q, want ltr", got.ReadingDirection)
	}
}

func TestUpdateReadingDirection(t *testing.T) {
	ctx := context.Background()
	r := NewNodeRepo(openTemp(t))
	n := mkNode(t, r, 0, "c", "/lib/c.zip", NodeComic)
	if err := r.UpdateReadingDirection(ctx, n.ID, "rtl"); err != nil {
		t.Fatal(err)
	}
	got, _ := r.Get(ctx, n.ID)
	if got.ReadingDirection != "rtl" {
		t.Fatalf("direction = %q, want rtl", got.ReadingDirection)
	}
}
