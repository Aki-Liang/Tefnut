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
	"strconv"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"

	"Tefnut/internal/store"
)

func newTestServer(t *testing.T) (*Server, *echo.Echo, *store.DB) {
	t.Helper()
	data := t.TempDir()
	db, err := store.Open(filepath.Join(data, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := NewServer(store.NewNodeRepo(db), store.NewTagRepo(db), store.NewProgressRepo(db), data, 400)
	e := echo.New()
	s.Register(e)
	return s, e, db
}

func TestHealthz(t *testing.T) {
	_, e, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func seedComic(t *testing.T, db *store.DB, dataDir string) *store.Node {
	t.Helper()
	repo := store.NewNodeRepo(db)
	// build a real zip on disk so page serving works
	zp := filepath.Join(t.TempDir(), "c.zip")
	f, _ := os.Create(zp)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("001.txt") // not an image; ensure filtered
	w.Write([]byte("x"))
	w2, _ := zw.Create("001.png")
	png.Encode(w2, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	zw.Close()
	f.Close()
	n, _ := repo.Create(context.Background(), &store.Node{
		ParentID: 0, Name: "c.zip", Path: zp, Type: store.NodeComic, PageCount: 1,
	})
	return n
}

func TestApiNodesBrowse(t *testing.T) {
	_, e, db := newTestServer(t)
	store.NewNodeRepo(db).Create(context.Background(), &store.Node{Name: "D", Path: "/d", Type: store.NodeDir})
	req := httptest.NewRequest(http.MethodGet, "/api/nodes?parent=0", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "\"name\":\"D\"") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestApiPageStreamsImage(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	req := httptest.NewRequest(http.MethodGet, "/api/comics/"+itoa(n.ID)+"/pages/0", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
		t.Fatalf("content-type=%s", ct)
	}
}

func TestApiCoverPlaceholderRedirect(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	req := httptest.NewRequest(http.MethodGet, "/api/comics/"+itoa(n.ID)+"/cover", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
}

func TestApiComicDetailTagsLowercase(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)

	tags := store.NewTagRepo(db)
	tg, err := tags.Upsert(context.Background(), "action")
	if err != nil {
		t.Fatal(err)
	}
	if err := tags.AddToNode(context.Background(), n.ID, tg.ID); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/comics/"+itoa(n.ID), nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"tags":[{"id":`) {
		t.Fatalf("expected lowercase tags array, got: %s", body)
	}
	if !strings.Contains(body, `"name":"action"`) {
		t.Fatalf("expected tag name 'action', got: %s", body)
	}
	if strings.Contains(body, `"ID":`) {
		t.Fatalf("unexpected uppercase 'ID' key in response: %s", body)
	}
	if strings.Contains(body, `"Name":`) {
		t.Fatalf("unexpected uppercase 'Name' key in response: %s", body)
	}
}

func itoa(i int64) string { return strconv.FormatInt(i, 10) }

func TestPageBrowseRenders(t *testing.T) {
	_, e, db := newTestServer(t)
	store.NewNodeRepo(db).Create(context.Background(), &store.Node{Name: "MyDir", Path: "/x", Type: store.NodeDir})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "MyDir") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPageReaderRenders(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	req := httptest.NewRequest(http.MethodGet, "/read/"+itoa(n.ID), nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "data-id=\""+itoa(n.ID)+"\"") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
