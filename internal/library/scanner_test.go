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

func newTestScanner(t *testing.T) (*Scanner, *store.NodeRepo, string, string) {
	t.Helper()
	root := t.TempDir()
	data := t.TempDir()
	db, err := store.Open(filepath.Join(data, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	repo := store.NewNodeRepo(db)
	return NewScanner(repo, root, data, 400), repo, root, data
}

func TestScanCreatesComicWithCoverAndPages(t *testing.T) {
	sc, repo, root, data := newTestScanner(t)
	png := pngBytes(t)
	writeZip(t, filepath.Join(root, "Series", "Vol1.zip"), map[string][]byte{
		"002.png": png, "001.png": png,
	})
	if err := sc.Scan(context.Background()); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	roots, _ := repo.ListChildren(context.Background(), 0)
	if len(roots) != 1 || roots[0].Type != store.NodeDir {
		t.Fatalf("root children = %+v", roots)
	}
	kids, _ := repo.ListChildren(context.Background(), roots[0].ID)
	if len(kids) != 1 || kids[0].Type != store.NodeComic {
		t.Fatalf("series children = %+v", kids)
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
	sc, repo, root, _ := newTestScanner(t)
	zp := filepath.Join(root, "a.zip")
	writeZip(t, zp, map[string][]byte{"001.png": pngBytes(t)})
	sc.Scan(context.Background())
	if got, _ := repo.ListChildren(context.Background(), 0); len(got) != 1 {
		t.Fatalf("expected 1 after first scan, got %d", len(got))
	}
	os.Remove(zp)
	sc.Scan(context.Background())
	if got, _ := repo.ListChildren(context.Background(), 0); len(got) != 0 {
		t.Fatalf("expected 0 after delete, got %d", len(got))
	}
}

func TestScanIsIdempotent(t *testing.T) {
	sc, repo, root, _ := newTestScanner(t)
	writeZip(t, filepath.Join(root, "a.zip"), map[string][]byte{"001.png": pngBytes(t)})
	sc.Scan(context.Background())
	sc.Scan(context.Background())
	got, _ := repo.ListChildren(context.Background(), 0)
	if len(got) != 1 {
		t.Fatalf("expected 1 node after double scan, got %d", len(got))
	}
}

func itoa(i int64) string { return strconvFormat(i) }
