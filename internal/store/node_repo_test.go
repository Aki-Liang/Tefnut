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
	kids, err := r.ListChildren(context.Background(), 0, -1, 0)
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
	kishimoto := "Kishimoto"
	five := 5
	if err := r.UpdateFields(ctx, a.ID, NodePatch{Author: &kishimoto, Rating: &five}); err != nil {
		t.Fatal(err)
	}
	res, err := r.Search(ctx, "naruto", 0, 0, -1, 0)
	if err != nil || len(res) != 1 || res[0].ID != a.ID {
		t.Fatalf("name search: %v / %+v", err, res)
	}
	res, err = r.Search(ctx, "", 0, 3, -1, 0)
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
	spread := "spread"
	if err := r.UpdateFields(ctx, n.ID, NodePatch{DisplayMode: &spread}); err != nil {
		t.Fatal(err)
	}
	got, _ := r.Get(ctx, n.ID)
	if got.DisplayMode != "spread" {
		t.Fatalf("display_mode = %q, want spread", got.DisplayMode)
	}
}

func TestNodeScanTargetsMatchColumnNames(t *testing.T) {
	n := &Node{}
	if got := len(nodeScanTargets(n)); got != len(nodeColNames) {
		t.Fatalf("scan targets = %d, column names = %d", got, len(nodeColNames))
	}
}

func TestUpdateFieldsPartialAndAtomic(t *testing.T) {
	repo := NewNodeRepo(openTemp(t))
	ctx := context.Background()
	n, err := repo.Create(ctx, &Node{Name: "C", Path: "/c.cbz", Type: NodeComic})
	if err != nil {
		t.Fatal(err)
	}
	author := "Author"
	rating := 5
	if err := repo.UpdateFields(ctx, n.ID, NodePatch{Author: &author, Rating: &rating}); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.Get(ctx, n.ID)
	if got.Author != "Author" || got.Rating != 5 {
		t.Fatalf("after author/rating patch: %+v", got)
	}
	if got.DisplayMode != "single" || got.ReadingDirection != "ltr" {
		t.Fatalf("unrelated fields changed: %+v", got)
	}
	mode := "spread"
	dir := "rtl"
	if err := repo.UpdateFields(ctx, n.ID, NodePatch{DisplayMode: &mode, ReadingDirection: &dir}); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.Get(ctx, n.ID)
	if got.DisplayMode != "spread" || got.ReadingDirection != "rtl" || got.Author != "Author" {
		t.Fatalf("after mode/dir patch: %+v", got)
	}
	// No-op patch is allowed and changes nothing.
	if err := repo.UpdateFields(ctx, n.ID, NodePatch{}); err != nil {
		t.Fatalf("empty patch: %v", err)
	}
}

func TestSearchAndGetReturnSameFields(t *testing.T) {
	repo := NewNodeRepo(openTemp(t))
	ctx := context.Background()
	created, err := repo.Create(ctx, &Node{ParentID: 0, Name: "Alpha", Path: "/a.cbz", Type: NodeComic, Author: "Me", Rating: 4})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateFileAttrs(ctx, created.ID, 10, 20, 7, CoverReady); err != nil {
		t.Fatal(err)
	}
	viaGet, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	results, err := repo.Search(ctx, "Alpha", 0, 0, -1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("search returned %d, want 1", len(results))
	}
	viaSearch := results[0]
	// Every field that crosses the column boundary must match between paths.
	if *viaGet != *viaSearch {
		t.Fatalf("Get vs Search node mismatch:\n get=%+v\n srch=%+v", viaGet, viaSearch)
	}
}

func TestListChildrenPagination(t *testing.T) {
	repo := NewNodeRepo(openTemp(t))
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := repo.Create(ctx, &Node{ParentID: 1, Name: "n" + itoa(i), Path: "/p" + itoa(i), Type: NodeComic}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := repo.ListChildren(ctx, 1, 2, 1) // limit 2, offset 1
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 {
		t.Fatalf("page len = %d, want 2", len(page))
	}
	all, err := repo.ListChildren(ctx, 1, -1, 0) // no limit
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Fatalf("all len = %d, want 5", len(all))
	}
}
