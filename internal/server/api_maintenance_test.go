package server

import (
	"archive/zip"
	"context"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Tefnut/internal/store"
)

func TestApiScanStatus(t *testing.T) {
	s, e, _ := newTestServer(t)
	stub := s.reconf.(*stubReconf)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/scan/status", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"scanning":false`) {
		t.Fatalf("idle: code=%d body=%s", rec.Code, rec.Body.String())
	}

	stub.scanning = true
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/scan/status", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"scanning":true`) {
		t.Fatalf("in flight: code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestApiCacheClear(t *testing.T) {
	s, e, _ := newTestServer(t)
	seed := func(rel string, n int) string {
		p := filepath.Join(s.dataDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	seed("cache/1/page.bin", 100)
	seed("thumbs/pages/1/0.jpg", 50)
	cover := seed("thumbs/9.jpg", 30) // cover thumb must survive

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/cache/clear", nil))
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"freedBytes":150`) {
		t.Fatalf("freedBytes missing/wrong: %s", rec.Body.String())
	}
	for _, dir := range []string{"cache", filepath.Join("thumbs", "pages")} {
		entries, _ := os.ReadDir(filepath.Join(s.dataDir, dir))
		if len(entries) != 0 {
			t.Errorf("%s not emptied: %v", dir, entries)
		}
	}
	if _, err := os.Stat(cover); err != nil {
		t.Errorf("cover thumb must survive clear: %v", err)
	}
}

// TestApiCacheClearResetsOpenReaders: clearing must also reset cached archive
// readers — otherwise the first page request after a clear hits a reader whose
// extracted files are gone and returns a one-off 500 before self-healing.
func TestApiCacheClearResetsOpenReaders(t *testing.T) {
	_, e, db := newTestServer(t)
	// A zip named .cbr takes the extract-to-cache path (archiver sniffs content),
	// so its cached reader points at files under dataDir/cache/<id>.
	zp := filepath.Join(t.TempDir(), "c.cbr")
	f, err := os.Create(zp)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("001.png")
	pngData(t, w)
	zw.Close()
	f.Close()
	n, err := store.NewNodeRepo(db).Create(context.Background(), &store.Node{
		ParentID: 0, Name: "c.cbr", Path: zp, Type: store.NodeComic, PageCount: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	get := func() int {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/comics/"+itoa(n.ID)+"/pages/0", nil))
		return rec.Code
	}
	if code := get(); code != 200 {
		t.Fatalf("first read = %d, want 200", code)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/cache/clear", nil))
	if rec.Code != 200 {
		t.Fatalf("clear = %d", rec.Code)
	}
	if code := get(); code != 200 {
		t.Fatalf("read right after clear = %d, want 200 (reader cache must be reset, not stale)", code)
	}
}

// pngData writes a minimal decodable PNG to w.
func pngData(t *testing.T, w io.Writer) {
	t.Helper()
	if err := png.Encode(w, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
}
