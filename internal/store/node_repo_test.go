package store

import (
	"context"
	"errors"
	"testing"
)

func mkNode(t *testing.T, r *NodeRepo, parent int64, name, path string, typ NodeType) *Node {
	t.Helper()
	n, err := r.Create(context.Background(), &Node{
		ParentID: parent, Name: name, Path: path, Type: typ,
	})
	if err != nil {
		t.Fatalf("Create %s: %v", name, err)
	}
	return n
}

func TestNodeCreateGet(t *testing.T) {
	r := NewNodeRepo(openTemp(t))
	n := mkNode(t, r, 0, "Series", "/lib/Series", NodeDir)
	if n.ID == 0 {
		t.Fatal("expected non-zero id")
	}
	got, err := r.Get(context.Background(), n.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Series" || got.Type != NodeDir {
		t.Errorf("got %+v", got)
	}
}

func TestNodeGetNotFound(t *testing.T) {
	r := NewNodeRepo(openTemp(t))
	_, err := r.Get(context.Background(), 999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListChildrenDirsFirst(t *testing.T) {
	r := NewNodeRepo(openTemp(t))
	mkNode(t, r, 0, "b-comic", "/lib/b.zip", NodeComic)
	mkNode(t, r, 0, "a-dir", "/lib/a", NodeDir)
	kids, err := r.ListChildren(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != 2 || kids[0].Type != NodeDir {
		t.Fatalf("expected dir first, got %+v", kids)
	}
}

func TestSearchByNameAndRating(t *testing.T) {
	ctx := context.Background()
	r := NewNodeRepo(openTemp(t))
	a := mkNode(t, r, 0, "Naruto Vol1", "/lib/n1.zip", NodeComic)
	mkNode(t, r, 0, "Bleach Vol1", "/lib/b1.zip", NodeComic)
	if err := r.UpdateMeta(ctx, a.ID, "Kishimoto", 5); err != nil {
		t.Fatal(err)
	}
	res, err := r.Search(ctx, "naruto", 0, 0)
	if err != nil || len(res) != 1 || res[0].ID != a.ID {
		t.Fatalf("name search: %v / %+v", err, res)
	}
	res, err = r.Search(ctx, "", 0, 3)
	if err != nil || len(res) != 1 || res[0].ID != a.ID {
		t.Fatalf("rating search: %v / %+v", err, res)
	}
}

func TestUpdateFileAttrs(t *testing.T) {
	ctx := context.Background()
	r := NewNodeRepo(openTemp(t))
	n := mkNode(t, r, 0, "c", "/lib/c.zip", NodeComic)
	if err := r.UpdateFileAttrs(ctx, n.ID, 1234, 99, 20, CoverReady); err != nil {
		t.Fatal(err)
	}
	got, _ := r.Get(ctx, n.ID)
	if got.PageCount != 20 || got.CoverStatus != CoverReady || got.Size != 1234 {
		t.Errorf("got %+v", got)
	}
}

func TestDeleteRemovesNode(t *testing.T) {
	ctx := context.Background()
	r := NewNodeRepo(openTemp(t))
	n := mkNode(t, r, 0, "c", "/lib/c.zip", NodeComic)
	if err := r.Delete(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(ctx, n.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected gone, got %v", err)
	}
}

func TestNodeDefaultDisplayMode(t *testing.T) {
	ctx := context.Background()
	r := NewNodeRepo(openTemp(t))
	n := mkNode(t, r, 0, "c", "/lib/c.zip", NodeComic)
	got, _ := r.Get(ctx, n.ID)
	if got.DisplayMode != "single" {
		t.Fatalf("default display_mode = %q, want single", got.DisplayMode)
	}
}

func TestUpdateDisplayMode(t *testing.T) {
	ctx := context.Background()
	r := NewNodeRepo(openTemp(t))
	n := mkNode(t, r, 0, "c", "/lib/c.zip", NodeComic)
	if err := r.UpdateDisplayMode(ctx, n.ID, "spread"); err != nil {
		t.Fatal(err)
	}
	got, _ := r.Get(ctx, n.ID)
	if got.DisplayMode != "spread" {
		t.Fatalf("display_mode = %q, want spread", got.DisplayMode)
	}
}
