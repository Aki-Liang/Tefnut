package store

import (
	"context"
	"errors"
	"testing"
)

func TestTagUpsertIdempotent(t *testing.T) {
	ctx := context.Background()
	r := NewTagRepo(openTemp(t))
	a, err := r.Upsert(ctx, "shounen")
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.Upsert(ctx, "shounen")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID {
		t.Fatalf("expected same id, got %d and %d", a.ID, b.ID)
	}
}

func TestTagAddListForNodeAndCount(t *testing.T) {
	ctx := context.Background()
	db := openTemp(t)
	nodes := NewNodeRepo(db)
	tags := NewTagRepo(db)
	n := mkNode(t, nodes, 0, "c", "/lib/c.zip", NodeComic)
	tg, _ := tags.Upsert(ctx, "action")
	if err := tags.AddToNode(ctx, n.ID, tg.ID); err != nil {
		t.Fatal(err)
	}
	// idempotent second add
	if err := tags.AddToNode(ctx, n.ID, tg.ID); err != nil {
		t.Fatal(err)
	}
	forNode, _ := tags.ListForNode(ctx, n.ID)
	if len(forNode) != 1 || forNode[0].Name != "action" {
		t.Fatalf("ListForNode = %+v", forNode)
	}
	counts, _ := tags.List(ctx)
	if len(counts) != 1 || counts[0].Count != 1 {
		t.Fatalf("List = %+v", counts)
	}
}

func TestTagRenameDuplicate(t *testing.T) {
	ctx := context.Background()
	r := NewTagRepo(openTemp(t))
	a, _ := r.Upsert(ctx, "a")
	r.Upsert(ctx, "b")
	if err := r.Rename(ctx, a.ID, "b"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}
}

func TestTagDeleteRemovesLinks(t *testing.T) {
	ctx := context.Background()
	db := openTemp(t)
	nodes := NewNodeRepo(db)
	tags := NewTagRepo(db)
	n := mkNode(t, nodes, 0, "c", "/lib/c.zip", NodeComic)
	tg, _ := tags.Upsert(ctx, "x")
	tags.AddToNode(ctx, n.ID, tg.ID)
	if err := tags.Delete(ctx, tg.ID); err != nil {
		t.Fatal(err)
	}
	forNode, _ := tags.ListForNode(ctx, n.ID)
	if len(forNode) != 0 {
		t.Fatalf("expected no tags after delete, got %+v", forNode)
	}
}
