package server

import (
	"archive/zip"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"Tefnut/internal/store"
)

// seedComicWithCount seeds a real 2-image zip whose stored PageCount is
// deliberately wrong, mimicking a PDF whose scan-time count (pdf pages) is an
// estimate that can diverge from the renderable pages actually served.
func seedComicWithCount(t *testing.T, db *store.DB, storedCount int) *store.Node {
	t.Helper()
	zp := filepath.Join(t.TempDir(), "c.zip")
	f, err := os.Create(zp)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, name := range []string{"001.png", "002.png"} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(w, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
			t.Fatal(err)
		}
	}
	zw.Close()
	f.Close()
	n, err := store.NewNodeRepo(db).Create(context.Background(), &store.Node{
		ParentID: 0, Name: "c.zip", Path: zp, Type: store.NodeComic, PageCount: storedCount,
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// TestPageReadReconcilesStalePageCount: the first real read opens the archive
// and knows the true served page list; a stale stored count must be repaired
// so the reader UI and progress bounds match reality from then on.
func TestPageReadReconcilesStalePageCount(t *testing.T) {
	_, e, db := newTestServer(t)
	n := seedComicWithCount(t, db, 5) // truth is 2

	req := httptest.NewRequest(http.MethodGet, "/api/comics/"+itoa(n.ID)+"/pages/0", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("page fetch status = %d", rec.Code)
	}

	got, err := store.NewNodeRepo(db).Get(context.Background(), n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PageCount != 2 {
		t.Errorf("page count after first read = %d, want reconciled 2", got.PageCount)
	}
}

// TestPageReadKeepsMatchingPageCount: no pointless writes when the count is
// already right.
func TestPageReadKeepsMatchingPageCount(t *testing.T) {
	_, e, db := newTestServer(t)
	n := seedComicWithCount(t, db, 2)

	req := httptest.NewRequest(http.MethodGet, "/api/comics/"+itoa(n.ID)+"/pages/1", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("page fetch status = %d", rec.Code)
	}

	got, err := store.NewNodeRepo(db).Get(context.Background(), n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PageCount != 2 {
		t.Errorf("page count = %d, want 2 unchanged", got.PageCount)
	}
}
