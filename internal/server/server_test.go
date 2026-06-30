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

type stubReconf struct {
	calls int
	scans int
}

func (s *stubReconf) Reconfigure(ctx context.Context) error { s.calls++; return nil }
func (s *stubReconf) ScanNow() bool                         { s.scans++; return true }

func newTestServer(t *testing.T) (*Server, *echo.Echo, *store.DB) {
	t.Helper()
	data := t.TempDir()
	db, err := store.Open(filepath.Join(data, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := NewServer(store.NewNodeRepo(db), store.NewTagRepo(db), store.NewProgressRepo(db),
		store.NewSettingsRepo(db), store.NewLibraryPathRepo(db), &stubReconf{}, data, 400, 120, nil)
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

func TestPageSetsImmutableCacheHeadersAndETag(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	url := "/api/comics/" + itoa(n.ID) + "/pages/0"

	// First request: expect 200 with Cache-Control and ETag.
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc == "" {
		t.Fatal("missing Cache-Control")
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}

	// Conditional request: expect 304.
	req2 := httptest.NewRequest(http.MethodGet, url, nil)
	req2.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, req2)
	if rec2.Code != 304 {
		t.Fatalf("conditional status = %d, want 304", rec2.Code)
	}
}

func TestApiPageThumb(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	req := httptest.NewRequest(http.MethodGet, "/api/comics/"+itoa(n.ID)+"/pages/0/thumb", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Fatalf("content-type=%s", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=86400" {
		t.Fatalf("Cache-Control=%q", cc)
	}
}

func TestApiPageThumbCacheHit(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	url := "/api/comics/" + itoa(n.ID) + "/pages/0/thumb"
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
		if rec.Code != 200 {
			t.Fatalf("attempt %d: status=%d body=%s", i, rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
			t.Fatalf("attempt %d: content-type=%s", i, ct)
		}
		if rec.Header().Get("Cache-Control") != "public, max-age=86400" {
			t.Fatalf("attempt %d: missing/wrong Cache-Control", i)
		}
	}
}

func TestApiPageThumbOutOfRange(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	req := httptest.NewRequest(http.MethodGet, "/api/comics/"+itoa(n.ID)+"/pages/999/thumb", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestApiSetProgress(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	store.NewNodeRepo(db).UpdateFileAttrs(context.Background(), n.ID, 1, 1, 5, store.CoverReady)
	req := httptest.NewRequest(http.MethodPut, "/api/comics/"+itoa(n.ID)+"/progress",
		strings.NewReader(`{"page":3}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	got, _ := store.NewProgressRepo(db).Get(context.Background(), n.ID)
	if got != 3 {
		t.Fatalf("progress = %d, want 3", got)
	}
}

func TestApiSetProgressOutOfRange(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	store.NewNodeRepo(db).UpdateFileAttrs(context.Background(), n.ID, 1, 1, 5, store.CoverReady)
	req := httptest.NewRequest(http.MethodPut, "/api/comics/"+itoa(n.ID)+"/progress",
		strings.NewReader(`{"page":99}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestApiTagCRUD(t *testing.T) {
	_, e, _ := newTestServer(t)
	// create
	req := httptest.NewRequest(http.MethodPost, "/api/tags", strings.NewReader(`{"name":"isekai"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	// list
	lrec := httptest.NewRecorder()
	e.ServeHTTP(lrec, httptest.NewRequest(http.MethodGet, "/api/tags", nil))
	if !strings.Contains(lrec.Body.String(), "isekai") {
		t.Fatalf("list body=%s", lrec.Body.String())
	}
}

func TestPageTagsRenders(t *testing.T) {
	_, e, db := newTestServer(t)
	if _, err := store.NewTagRepo(db).Upsert(context.Background(), "demo"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tags", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "demo") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestApiRenameTagDuplicate(t *testing.T) {
	_, e, db := newTestServer(t)
	tags := store.NewTagRepo(db)
	a, err := tags.Upsert(context.Background(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tags.Upsert(context.Background(), "beta"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/tags/"+itoa(a.ID),
		strings.NewReader(`{"name":"beta"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestApiUpdateMetaRating(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	req := httptest.NewRequest(http.MethodPatch, "/api/comics/"+itoa(n.ID),
		strings.NewReader(`{"author":"Aki","rating":4}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	got, _ := store.NewNodeRepo(db).Get(context.Background(), n.ID)
	if got.Author != "Aki" || got.Rating != 4 {
		t.Fatalf("got %+v", got)
	}
}

func TestApiUpdateMetaRejectsBadRating(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	req := httptest.NewRequest(http.MethodPatch, "/api/comics/"+itoa(n.ID),
		strings.NewReader(`{"rating":9}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestApiAddAndRemoveTag(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	req := httptest.NewRequest(http.MethodPost, "/api/comics/"+itoa(n.ID)+"/tags",
		strings.NewReader(`{"name":"action"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("add status=%d body=%s", rec.Code, rec.Body.String())
	}
	tags, _ := store.NewTagRepo(db).ListForNode(context.Background(), n.ID)
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(tags))
	}
	del := httptest.NewRequest(http.MethodDelete,
		"/api/comics/"+itoa(n.ID)+"/tags/"+itoa(tags[0].ID), nil)
	drec := httptest.NewRecorder()
	e.ServeHTTP(drec, del)
	if drec.Code != 200 {
		t.Fatalf("del status=%d", drec.Code)
	}
	tags, _ = store.NewTagRepo(db).ListForNode(context.Background(), n.ID)
	if len(tags) != 0 {
		t.Fatalf("expected 0 tags after remove")
	}
}

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

func TestApiGetSettingsDefaults(t *testing.T) {
	_, e, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"scanMode":"interval"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestApiUpdateSettingsValidatesMode(t *testing.T) {
	_, e, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPut, "/api/settings",
		strings.NewReader(`{"scanMode":"bogus","scanInterval":"2m","scanDailyTime":"03:00"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestApiAddAndDeletePath(t *testing.T) {
	s, e, db := newTestServer(t)
	dir := t.TempDir()
	s.allowedRoots = []string{dir}
	body := `{"name":"L","path":"` + dir + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/settings/paths", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("add status=%d body=%s", rec.Code, rec.Body.String())
	}
	list, _ := store.NewLibraryPathRepo(db).List(context.Background())
	if len(list) != 1 {
		t.Fatalf("expected 1 path, got %d", len(list))
	}
	del := httptest.NewRequest(http.MethodDelete, "/api/settings/paths/"+itoa(list[0].ID), nil)
	drec := httptest.NewRecorder()
	e.ServeHTTP(drec, del)
	if drec.Code != 200 {
		t.Fatalf("delete status=%d", drec.Code)
	}
}

func TestApiAddPathRejectsMissingDir(t *testing.T) {
	_, e, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/settings/paths",
		strings.NewReader(`{"name":"L","path":"/no/such/dir/xyz"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSettingsUpdateTriggersReconfigure(t *testing.T) {
	data := t.TempDir()
	db, err := store.Open(filepath.Join(data, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rc := &stubReconf{}
	s := NewServer(store.NewNodeRepo(db), store.NewTagRepo(db), store.NewProgressRepo(db),
		store.NewSettingsRepo(db), store.NewLibraryPathRepo(db), rc, data, 400, 120, nil)
	e := echo.New()
	s.Register(e)
	req := httptest.NewRequest(http.MethodPut, "/api/settings",
		strings.NewReader(`{"scanMode":"watch","scanInterval":"2m","scanDailyTime":"03:00"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rc.calls != 1 {
		t.Fatalf("expected Reconfigure called once, got %d", rc.calls)
	}
}

func TestApiScanNow(t *testing.T) {
	_, e, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/scan", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"triggered":true`) {
		t.Fatalf("body=%s, want triggered:true", rec.Body.String())
	}
}

func TestPageSettingsRenders(t *testing.T) {
	_, e, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "扫描方式") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestApiComicDetailHasDirection(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/comics/"+itoa(n.ID), nil))
	if !strings.Contains(rec.Body.String(), `"readingDirection":"ltr"`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestApiUpdateMetaSetsDirection(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	req := httptest.NewRequest(http.MethodPatch, "/api/comics/"+itoa(n.ID),
		strings.NewReader(`{"readingDirection":"rtl"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	got, _ := store.NewNodeRepo(db).Get(context.Background(), n.ID)
	if got.ReadingDirection != "rtl" {
		t.Fatalf("direction=%q", got.ReadingDirection)
	}
}

func TestApiUpdateMetaRejectsBadDirection(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	req := httptest.NewRequest(http.MethodPatch, "/api/comics/"+itoa(n.ID),
		strings.NewReader(`{"readingDirection":"sideways"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSidebarHasToggle(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	brec := httptest.NewRecorder()
	e.ServeHTTP(brec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(brec.Body.String(), `id="sidebar-toggle"`) {
		t.Fatalf("browse should have sidebar toggle: %s", brec.Body.String())
	}
	// reader (full-screen, sidebar blanked) must NOT have the toggle
	rrec := httptest.NewRecorder()
	e.ServeHTTP(rrec, httptest.NewRequest(http.MethodGet, "/read/"+itoa(n.ID), nil))
	if strings.Contains(rrec.Body.String(), `id="sidebar-toggle"`) {
		t.Fatal("reader should not have sidebar toggle")
	}
}

func TestSettingsHasHourMinuteSelects(t *testing.T) {
	_, e, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `id="daily-h"`) || !strings.Contains(body, `id="daily-m"`) {
		t.Fatalf("settings should have hour/minute selects: %s", body)
	}
}

func TestReaderHasStripAndDirection(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/read/"+itoa(n.ID), nil))
	body := rec.Body.String()
	if !strings.Contains(body, `id="thumbstrip"`) {
		t.Fatalf("reader should have thumbnail strip: %s", body)
	}
	if !strings.Contains(body, `id="dirtoggle"`) {
		t.Fatal("reader should have a direction toggle")
	}
	if !strings.Contains(body, `data-dir="ltr"`) {
		t.Fatal("reader should carry data-dir")
	}
}

func TestApiComicDetailHasDisplayMode(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/comics/"+itoa(n.ID), nil))
	if !strings.Contains(rec.Body.String(), `"displayMode":"single"`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestApiUpdateMetaSetsDisplayMode(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	req := httptest.NewRequest(http.MethodPatch, "/api/comics/"+itoa(n.ID),
		strings.NewReader(`{"displayMode":"spread"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	got, _ := store.NewNodeRepo(db).Get(context.Background(), n.ID)
	if got.DisplayMode != "spread" {
		t.Fatalf("display_mode=%q", got.DisplayMode)
	}
}

func TestApiUpdateMetaRejectsBadDisplayMode(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	req := httptest.NewRequest(http.MethodPatch, "/api/comics/"+itoa(n.ID),
		strings.NewReader(`{"displayMode":"flip"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestReaderHasModeToggle(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/read/"+itoa(n.ID), nil))
	body := rec.Body.String()
	if !strings.Contains(body, `id="modetoggle"`) {
		t.Fatalf("reader should have mode toggle: %s", body)
	}
	if !strings.Contains(body, `data-mode="single"`) {
		t.Fatal("reader should carry data-mode")
	}
}

func TestServerErrorBodyIsGeneric(t *testing.T) {
	_, e, db := newTestServer(t)
	repo := store.NewNodeRepo(db)
	n, err := repo.Create(context.Background(), &store.Node{
		ParentID: 0, Name: "broken.cbz", Path: "/nonexistent/path/broken.cbz",
		Type: store.NodeComic, PageCount: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/comics/"+itoa(n.ID)+"/pages/0", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code < 500 {
		t.Fatalf("expected a 5xx, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), ".cbz") || strings.Contains(rec.Body.String(), "/nonexistent") {
		t.Fatalf("5xx body leaked a path: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "internal error") {
		t.Fatalf("5xx body = %s, want generic message", rec.Body.String())
	}
}

func TestApiAddPathRejectsOutsideRoot(t *testing.T) {
	s, e, _ := newTestServer(t)
	root := t.TempDir()
	s.allowedRoots = []string{root}

	post := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/settings/paths",
			strings.NewReader(`{"name":"L","path":"`+path+`"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	inside := filepath.Join(root, "lib")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if rec := post(inside); rec.Code != 200 {
		t.Fatalf("inside root: status=%d body=%s", rec.Code, rec.Body.String())
	}

	outside := t.TempDir() // exists, readable, but not under root
	rec := post(outside)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("outside root: status=%d, want 400", rec.Code)
	}
	if strings.Contains(rec.Body.String(), root) {
		t.Fatalf("400 body leaked the root path: %s", rec.Body.String())
	}
}

func TestSidebarOnBrowseNotReader(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	// browse has the sidebar nav
	brec := httptest.NewRecorder()
	e.ServeHTTP(brec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(brec.Body.String(), `class="sidebar"`) {
		t.Fatalf("browse should have sidebar: %s", brec.Body.String())
	}
	if !strings.Contains(brec.Body.String(), "设置") {
		t.Fatal("sidebar should link 设置")
	}
	// reader has no sidebar
	rrec := httptest.NewRecorder()
	e.ServeHTTP(rrec, httptest.NewRequest(http.MethodGet, "/read/"+itoa(n.ID), nil))
	if strings.Contains(rrec.Body.String(), `class="sidebar"`) {
		t.Fatal("reader should NOT have sidebar")
	}
	if !strings.Contains(rrec.Body.String(), "下一页") {
		t.Fatal("reader should have a visible 下一页 button")
	}
}
