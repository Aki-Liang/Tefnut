package library

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"Tefnut/internal/store"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 20, 30))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func writeZip(t *testing.T, path string, pages map[string][]byte) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, body := range pages {
		w, _ := zw.Create(name)
		w.Write(body)
	}
	zw.Close()
}

func newTestScanner(t *testing.T) (*Scanner, *store.NodeRepo, *store.LibraryPathRepo, string) {
	t.Helper()
	data := t.TempDir()
	db, err := store.Open(filepath.Join(data, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	nodes := store.NewNodeRepo(db)
	paths := store.NewLibraryPathRepo(db)
	return NewScanner(nodes, paths, data, 400), nodes, paths, data
}

func TestScanCreatesComicWithCoverAndPages(t *testing.T) {
	ctx := context.Background()
	sc, nodes, paths, data := newTestScanner(t)
	root := t.TempDir()
	writeZip(t, filepath.Join(root, "Series", "Vol1.zip"), map[string][]byte{
		"002.png": pngBytes(t), "001.png": pngBytes(t),
	})
	paths.Add(ctx, "Lib", root)
	if err := sc.Scan(ctx); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	// top-level: library node
	libs, _ := nodes.ListChildren(ctx, 0)
	if len(libs) != 1 || libs[0].Type != store.NodeDir {
		t.Fatalf("expected 1 library node, got %+v", libs)
	}
	// series dir
	series, _ := nodes.ListChildren(ctx, libs[0].ID)
	if len(series) != 1 || series[0].Type != store.NodeDir {
		t.Fatalf("series children = %+v", series)
	}
	// comic
	kids, _ := nodes.ListChildren(ctx, series[0].ID)
	if len(kids) != 1 || kids[0].Type != store.NodeComic {
		t.Fatalf("comic children = %+v", kids)
	}
	comic := kids[0]
	if comic.PageCount != 2 {
		t.Errorf("page count = %d, want 2", comic.PageCount)
	}
	if comic.CoverStatus != store.CoverReady {
		t.Errorf("cover status = %d, want ready", comic.CoverStatus)
	}
	if _, err := os.Stat(filepath.Join(data, "thumbs", itoa(comic.ID)+".jpg")); err != nil {
		t.Errorf("thumb not written: %v", err)
	}
}

func TestScanRemovesDeletedFiles(t *testing.T) {
	ctx := context.Background()
	sc, nodes, paths, data := newTestScanner(t)
	root := t.TempDir()
	zp := filepath.Join(root, "a.zip")
	writeZip(t, zp, map[string][]byte{"001.png": pngBytes(t)})
	paths.Add(ctx, "Lib", root)
	sc.Scan(ctx)

	// After first scan: 1 library node containing 1 comic
	libs, _ := nodes.ListChildren(ctx, 0)
	if len(libs) != 1 {
		t.Fatalf("expected 1 library root after first scan, got %d", len(libs))
	}
	kids, _ := nodes.ListChildren(ctx, libs[0].ID)
	if len(kids) != 1 {
		t.Fatalf("expected 1 comic after first scan, got %d", len(kids))
	}
	comicID := kids[0].ID

	os.Remove(zp)
	sc.Scan(ctx)

	// Comic should be gone; library node still present (it's still configured)
	kids2, _ := nodes.ListChildren(ctx, libs[0].ID)
	if len(kids2) != 0 {
		t.Fatalf("expected 0 comics after delete, got %d", len(kids2))
	}
	if _, err := os.Stat(filepath.Join(data, "thumbs", strconvFormat(comicID)+".jpg")); !os.IsNotExist(err) {
		t.Errorf("thumb file should be removed after deletion, stat err = %v", err)
	}
}

func TestScanIsIdempotent(t *testing.T) {
	ctx := context.Background()
	sc, nodes, paths, _ := newTestScanner(t)
	root := t.TempDir()
	writeZip(t, filepath.Join(root, "a.zip"), map[string][]byte{"001.png": pngBytes(t)})
	paths.Add(ctx, "Lib", root)
	sc.Scan(ctx)
	sc.Scan(ctx)

	// Still 1 library node with 1 comic child
	libs, _ := nodes.ListChildren(ctx, 0)
	if len(libs) != 1 {
		t.Fatalf("expected 1 library node after double scan, got %d", len(libs))
	}
	kids, _ := nodes.ListChildren(ctx, libs[0].ID)
	if len(kids) != 1 {
		t.Fatalf("expected 1 comic node after double scan, got %d", len(kids))
	}
}

func TestScanMultipleLibraries(t *testing.T) {
	ctx := context.Background()
	sc, nodes, paths, _ := newTestScanner(t)
	libA := t.TempDir()
	libB := t.TempDir()
	writeZip(t, filepath.Join(libA, "a.zip"), map[string][]byte{"001.png": pngBytes(t)})
	writeZip(t, filepath.Join(libB, "b.zip"), map[string][]byte{"001.png": pngBytes(t)})
	paths.Add(ctx, "LibA", libA)
	paths.Add(ctx, "LibB", libB)

	if err := sc.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	roots, _ := nodes.ListChildren(ctx, 0)
	if len(roots) != 2 {
		t.Fatalf("expected 2 top-level library nodes, got %d", len(roots))
	}
	names := map[string]bool{roots[0].Name: true, roots[1].Name: true}
	if !names["LibA"] || !names["LibB"] {
		t.Fatalf("library nodes named wrong: %+v", roots)
	}
	// each library node has its comic child
	for _, root := range roots {
		kids, _ := nodes.ListChildren(ctx, root.ID)
		if len(kids) != 1 || kids[0].Type != store.NodeComic {
			t.Fatalf("library %s children = %+v", root.Name, kids)
		}
	}
}

func TestScanRemovesDeconfiguredLibrary(t *testing.T) {
	ctx := context.Background()
	sc, nodes, paths, _ := newTestScanner(t)
	lib := t.TempDir()
	writeZip(t, filepath.Join(lib, "a.zip"), map[string][]byte{"001.png": pngBytes(t)})
	lp, _ := paths.Add(ctx, "Lib", lib)
	sc.Scan(ctx)
	if roots, _ := nodes.ListChildren(ctx, 0); len(roots) != 1 {
		t.Fatalf("expected 1 root after add")
	}
	paths.Delete(ctx, lp.ID)
	sc.Scan(ctx)
	if roots, _ := nodes.ListChildren(ctx, 0); len(roots) != 0 {
		t.Fatalf("expected 0 roots after delete, got %d", len(roots))
	}
}

func TestScanRenamesLibraryNode(t *testing.T) {
	ctx := context.Background()
	sc, nodes, paths, _ := newTestScanner(t)
	lib := t.TempDir()
	writeZip(t, filepath.Join(lib, "a.zip"), map[string][]byte{"001.png": pngBytes(t)})
	lp, _ := paths.Add(ctx, "Old", lib)
	sc.Scan(ctx)
	paths.Rename(ctx, lp.ID, "New")
	sc.Scan(ctx)
	roots, _ := nodes.ListChildren(ctx, 0)
	if len(roots) != 1 || roots[0].Name != "New" {
		t.Fatalf("expected renamed node, got %+v", roots)
	}
}

func itoa(i int64) string { return strconvFormat(i) }

// writeTestZip builds a zip at path with one minimal PNG per filename in names.
func writeTestZip(t *testing.T, path string, names []string) {
	t.Helper()
	pages := make(map[string][]byte, len(names))
	for _, n := range names {
		pages[n] = pngBytes(t)
	}
	writeZip(t, path, pages)
}

// pageCountOf finds the comic node whose path matches comicPath and returns its PageCount.
func pageCountOf(t *testing.T, repo *store.NodeRepo, comicPath string) int {
	t.Helper()
	ctx := context.Background()
	roots, err := repo.ListChildren(ctx, 0)
	if err != nil {
		t.Fatalf("pageCountOf: list roots: %v", err)
	}
	var search func(parentID int64) *store.Node
	search = func(parentID int64) *store.Node {
		kids, err := repo.ListChildren(ctx, parentID)
		if err != nil {
			return nil
		}
		for _, k := range kids {
			if k.Path == comicPath {
				return k
			}
			if k.Type == store.NodeDir {
				if found := search(k.ID); found != nil {
					return found
				}
			}
		}
		return nil
	}
	for _, r := range roots {
		if n := search(r.ID); n != nil {
			return int(n.PageCount)
		}
	}
	t.Fatalf("pageCountOf: comic not found for path %s", comicPath)
	return 0
}

func TestRescanReplacedArchiveUpdatesPageCount(t *testing.T) {
	ctx := context.Background()
	sc, repo, paths, _ := newTestScanner(t)
	libDir := t.TempDir()
	if _, err := paths.Add(ctx, "lib", libDir); err != nil {
		t.Fatal(err)
	}
	comic := filepath.Join(libDir, "c.cbz")
	writeTestZip(t, comic, []string{"01.png", "02.png"})
	if err := sc.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	got := pageCountOf(t, repo, comic)
	if got != 2 {
		t.Fatalf("page count after first scan = %d, want 2", got)
	}
	// Replace the archive with a 3-page one (changes size/mtime so buildComic re-runs).
	writeTestZip(t, comic, []string{"01.png", "02.png", "03.png"})
	if err := sc.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if got := pageCountOf(t, repo, comic); got != 3 {
		t.Fatalf("page count after replace = %d, want 3", got)
	}
}
