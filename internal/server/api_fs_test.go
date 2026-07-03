package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/labstack/echo/v4"
)

// fsGet issues a GET against the fs API and returns the recorder.
func fsGet(t *testing.T, e *echo.Echo, url string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func decodeDirs(t *testing.T, rec *httptest.ResponseRecorder) fsDirsDTO {
	t.Helper()
	var body struct {
		Data fsDirsDTO `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	return body.Data
}

// Without ?path the API returns the configured roots, skipping ones that do
// not exist on disk, so the picker's initial view never shows dead entries.
func TestApiFsDirsListsExistingRoots(t *testing.T) {
	s, e, _ := newTestServer(t)
	r1, r2 := t.TempDir(), t.TempDir()
	s.allowedRoots = []string{r1, r2, filepath.Join(r1, "does-not-exist")}

	rec := fsGet(t, e, "/api/fs/dirs")
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data fsRootsDTO `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data.Roots) != 2 {
		t.Fatalf("roots=%v, want the 2 existing roots", body.Data.Roots)
	}
	if body.Data.Roots[0].Path != r1 || body.Data.Roots[1].Path != r2 {
		t.Fatalf("roots=%v, want [%s %s]", body.Data.Roots, r1, r2)
	}
}

// Listing a root returns only its visible immediate subdirectories, sorted by
// name: files and dot-directories are excluded, and parent is "" at a root so
// the UI falls back to the roots view.
func TestApiFsDirsListsSubdirsOnly(t *testing.T) {
	s, e, _ := newTestServer(t)
	root := t.TempDir()
	s.allowedRoots = []string{root}
	for _, d := range []string{"bbb", "aaa", ".hidden"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := fsGet(t, e, "/api/fs/dirs?path="+root)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	d := decodeDirs(t, rec)
	if d.Path != root || d.Parent != "" {
		t.Fatalf("path=%q parent=%q, want path=%q parent=\"\"", d.Path, d.Parent, root)
	}
	if len(d.Dirs) != 2 || d.Dirs[0].Name != "aaa" || d.Dirs[1].Name != "bbb" {
		t.Fatalf("dirs=%v, want [aaa bbb]", d.Dirs)
	}
	if d.Dirs[0].Path != filepath.Join(root, "aaa") {
		t.Fatalf("dir path=%q", d.Dirs[0].Path)
	}
}

// A subdirectory's parent points one level up so the 上级 button works.
func TestApiFsDirsParentOfSubdir(t *testing.T) {
	s, e, _ := newTestServer(t)
	root := t.TempDir()
	s.allowedRoots = []string{root}
	sub := filepath.Join(root, "manga")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	rec := fsGet(t, e, "/api/fs/dirs?path="+sub)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if d := decodeDirs(t, rec); d.Parent != root {
		t.Fatalf("parent=%q, want %q", d.Parent, root)
	}
}

// Paths outside the allowed roots, relative paths, ".." climbs, and
// non-existent paths must all be rejected with 400 (fail closed).
func TestApiFsDirsRejectsBadPaths(t *testing.T) {
	s, e, _ := newTestServer(t)
	root := t.TempDir()
	outside := t.TempDir()
	s.allowedRoots = []string{root}

	for name, q := range map[string]string{
		"outside root":  outside,
		"relative path": "some/relative",
		"dotdot climb":  filepath.Join(root, ".."),
		"missing dir":   filepath.Join(root, "nope"),
	} {
		rec := fsGet(t, e, "/api/fs/dirs?path="+q)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status=%d body=%s", name, rec.Code, rec.Body.String())
		}
	}
}

// A symlink inside a root that points outside must not be enterable: the
// escape is caught by PathWithinRoots when the picker tries to list it.
func TestApiFsDirsSymlinkEscapeRejected(t *testing.T) {
	s, e, _ := newTestServer(t)
	root := t.TempDir()
	outside := t.TempDir()
	s.allowedRoots = []string{root}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	rec := fsGet(t, e, "/api/fs/dirs?path="+link)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
