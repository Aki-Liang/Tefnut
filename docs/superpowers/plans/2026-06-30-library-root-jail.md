# Library-Path Root Jail Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reject library directories added via `POST /api/settings/paths` unless they resolve inside a configured allowed root, closing the arbitrary-local-directory vector without adding auth.

**Architecture:** A pure, symlink-resolving containment check (`PathWithinRoots`) gates the one mutating handler. Allowed roots come from a new `library.allowedRoots` config (default `[rootPath]`) passed into the server. No DB/scan/reader changes.

**Tech Stack:** Go 1.24, echo v4, `path/filepath`. No new dependencies.

## Global Constraints

- Single-user, NO auth; default bind stays `:8086` — this change ONLY closes the arbitrary-directory vector, it does not add authentication or change binding.
- Containment is fail-closed: any error resolving the requested path → reject (400), never allow, never 500.
- Symlinks in BOTH the requested path and each root are resolved (`filepath.EvalSymlinks`) before comparison — required for correctness on macOS (`/var` → `/private/var`) and to stop a symlink inside a root escaping it.
- The 400 reject message is the fixed string `目录必须在允许的库根之内` with NO path interpolation (no root disclosure).
- Only API adds are gated; the first-run YAML seed (`pathRepo.Add` in main.go) and existing DB rows are NOT re-validated.
- `go build ./... && go vet ./... && go test ./...` green; `gofmt` clean.

## File Structure

- `internal/server/pathguard.go` (new) — `PathWithinRoots`, the pure containment check.
- `internal/server/pathguard_test.go` (new) — its unit tests (the security core).
- `internal/config/config.go` (modify) — `Library.AllowedRoots` field + default/abs in `validate()`.
- `internal/server/server.go` (modify) — `NewServer` gains `allowedRoots []string`; `Server` stores it.
- `internal/server/api_settings.go` (modify) — `apiAddPath` calls the gate.
- `cmd/tefnut/main.go` (modify) — pass `cfg.Library.AllowedRoots`.
- `internal/server/server_test.go` (modify) — update `newTestServer` for the new arg, fix `TestApiAddAndDeletePath`, add the outside-root test.

---

### Task 1: PathWithinRoots containment check

**Files:**
- Create: `internal/server/pathguard.go`
- Test: `internal/server/pathguard_test.go`

**Interfaces:**
- Produces (consumed by Task 2): `func PathWithinRoots(dir string, roots []string) (bool, error)` in package `server`. Returns `true` iff `dir` (absolute, existing) resolves inside one of `roots`. Error is non-nil only when `dir` itself cannot be resolved; callers fail closed on it.

- [ ] **Step 1: Write the failing tests**

Create `internal/server/pathguard_test.go`:

```go
package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathWithinRoots(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "manga", "vol1")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir() // a separate temp dir, not under root
	sibling := root + "-other" // shares root's textual prefix but is NOT inside it
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sibling) })

	cases := []struct {
		name string
		dir  string
		want bool
	}{
		{"subdir inside root", sub, true},
		{"root itself", root, true},
		{"outside root", outside, false},
		{"prefix-trap sibling", sibling, false},
		{"parent of root via ..", filepath.Join(root, ".."), false},
	}
	for _, c := range cases {
		got, err := PathWithinRoots(c.dir, []string{root})
		if err != nil {
			t.Fatalf("%s: unexpected err %v", c.name, err)
		}
		if got != c.want {
			t.Fatalf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestPathWithinRootsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	got, err := PathWithinRoots(link, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("a symlink inside root pointing outside must be rejected")
	}
}

func TestPathWithinRootsMultipleAndEmpty(t *testing.T) {
	r1 := t.TempDir()
	r2 := t.TempDir()
	if got, _ := PathWithinRoots(r2, []string{r1, r2}); !got {
		t.Fatal("second root should match")
	}
	if got, _ := PathWithinRoots(r1, nil); got {
		t.Fatal("empty roots must reject")
	}
}

func TestPathWithinRootsMissingDirErrors(t *testing.T) {
	root := t.TempDir()
	_, err := PathWithinRoots(filepath.Join(root, "does-not-exist"), []string{root})
	if err == nil {
		t.Fatal("a non-existent dir should return an error so the caller fails closed")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -run TestPathWithinRoots -v`
Expected: FAIL — `undefined: PathWithinRoots`.

- [ ] **Step 3: Implement pathguard.go**

Create `internal/server/pathguard.go`:

```go
package server

import (
	"os"
	"path/filepath"
	"strings"
)

// PathWithinRoots reports whether dir (an absolute, existing path) resolves to a
// location inside one of roots. Symlinks in both dir and each root are resolved
// first, so a symlink inside a root cannot point outside it, and so comparison
// is correct on platforms where temp paths are themselves symlinks (macOS
// /var -> /private/var). A root that fails to resolve (e.g. it does not exist)
// is skipped. Returns a non-nil error only when dir itself cannot be resolved;
// callers MUST fail closed (reject) on a non-nil error.
func PathWithinRoots(dir string, roots []string) (bool, error) {
	rp, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false, err
	}
	for _, root := range roots {
		rr, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(rr, rp)
		if err != nil {
			continue
		}
		// inside iff rel is "." (equal) or a descendant path that does not
		// climb out via "..". This rejects both "/lib/../etc" escapes and the
		// "/lib" vs "/lib-other" prefix trap a naive HasPrefix would allow.
		if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))) {
			return true, nil
		}
	}
	return false, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/ -run TestPathWithinRoots -v`
Expected: PASS — all four `TestPathWithinRoots*` tests.

- [ ] **Step 5: Commit**

```bash
git add internal/server/pathguard.go internal/server/pathguard_test.go
git commit -m "feat: PathWithinRoots symlink-resolving containment check"
```

---

### Task 2: Config root, server wiring, and the handler gate

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/server/server.go`
- Modify: `internal/server/api_settings.go`
- Modify: `cmd/tefnut/main.go`
- Modify: `internal/server/server_test.go`

**Interfaces:**
- Consumes: `PathWithinRoots(dir string, roots []string) (bool, error)` (Task 1).
- Produces: `NewServer(..., pageThumbWidth int, allowedRoots []string) *Server` (trailing param added); `Server.allowedRoots []string`; `config.Library.AllowedRoots []string`.

- [ ] **Step 1: Add AllowedRoots to config with default + abs**

In `internal/config/config.go`, add `"path/filepath"` to the imports, and change the `Library` struct:

```go
type Library struct {
	RootPath     string   `yaml:"rootPath"`
	AllowedRoots []string `yaml:"allowedRoots"`
}
```

In `validate()`, immediately AFTER the `RootPath` `os.Stat`/`IsDir` check and BEFORE the `DataDir` check, insert:

```go
	// AllowedRoots gate which directories the path API may add as libraries.
	// Default to the configured library root; resolve all entries to absolute.
	if len(c.Library.AllowedRoots) == 0 {
		c.Library.AllowedRoots = []string{c.Library.RootPath}
	}
	for i, r := range c.Library.AllowedRoots {
		abs, err := filepath.Abs(r)
		if err != nil {
			return fmt.Errorf("config: library.allowedRoots %q: %w", r, err)
		}
		c.Library.AllowedRoots[i] = abs
	}
```

- [ ] **Step 2: Thread allowedRoots through the server**

In `internal/server/server.go`, add the field to `Server`:

```go
	decodeSem      chan struct{}
	allowedRoots   []string
```

Change `NewServer`'s signature to add a trailing parameter and store it:

```go
func NewServer(nodes *store.NodeRepo, tags *store.TagRepo, progress *store.ProgressRepo,
	settings *store.SettingsRepo, paths *store.LibraryPathRepo, reconf Reconfigurer,
	dataDir string, thumbWidth int, pageThumbWidth int, allowedRoots []string) *Server {
	tc, _ := newThumbCache(thumbCacheMaxEntries, filepath.Join(dataDir, "thumbs"))
	return &Server{nodes: nodes, tags: tags, progress: progress, settings: settings,
		paths: paths, reconf: reconf, dataDir: dataDir, thumbWidth: thumbWidth,
		pageThumbWidth: pageThumbWidth,
		thumbs:         tc, readers: archive.NewReaderCache(archiveCacheSize),
		decodeSem:    make(chan struct{}, decodeConcurrency),
		allowedRoots: allowedRoots}
}
```

In `cmd/tefnut/main.go`, update the `NewServer` call (currently ends `..., cfg.Thumbnail.PageWidth)`) to pass the roots:

```go
	srv := server.NewServer(nodes, tags, progress, settingsRepo, pathRepo, manager, cfg.DataDir, cfg.Thumbnail.Width, cfg.Thumbnail.PageWidth, cfg.Library.AllowedRoots)
```

- [ ] **Step 3: Gate apiAddPath**

In `internal/server/api_settings.go` `apiAddPath`, immediately AFTER the existing block:

```go
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return fail(c, http.StatusBadRequest, errors.New("path is not a readable directory"))
	}
```

insert the gate (fail closed on error OR not-contained):

```go
	if ok, err := PathWithinRoots(abs, s.allowedRoots); err != nil || !ok {
		return fail(c, http.StatusBadRequest, errors.New("目录必须在允许的库根之内"))
	}
```

- [ ] **Step 4: Update the test helper and the existing add test**

In `internal/server/server_test.go`, the `newTestServer` call to `NewServer` currently ends `..., data, 400, 120)`. Add `nil` for the new arg:

```go
	s := NewServer(store.NewNodeRepo(db), store.NewTagRepo(db), store.NewProgressRepo(db),
		store.NewSettingsRepo(db), store.NewLibraryPathRepo(db), &stubReconf{}, data, 400, 120, nil)
```

`TestApiAddAndDeletePath` adds an arbitrary `t.TempDir()` and expects 200; with the gate and `nil` roots it would now be rejected. Make the dir an allowed root by capturing `s` and setting `allowedRoots` (this is a white-box test in package `server`). Change its first line and add one line:

```go
func TestApiAddAndDeletePath(t *testing.T) {
	s, e, db := newTestServer(t)
	dir := t.TempDir()
	s.allowedRoots = []string{dir}
	body := `{"name":"L","path":"` + dir + `"}`
```

(The rest of `TestApiAddAndDeletePath` is unchanged. `TestApiAddPathRejectsMissingDir` is unaffected — its path fails `os.Stat` before the gate.)

- [ ] **Step 5: Add the outside-root rejection test**

Append to `internal/server/server_test.go` (imports `os`, `path/filepath`, `strings`, `net/http`, `net/http/httptest` are already present in the file):

```go
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
```

- [ ] **Step 6: Run the suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all green — including `TestPathWithinRoots*`, `TestApiAddAndDeletePath`, `TestApiAddPathRejectsMissingDir`, `TestApiAddPathRejectsOutsideRoot`.

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/config/config.go internal/server/server.go internal/server/api_settings.go cmd/tefnut/main.go internal/server/server_test.go
git add internal/config/config.go internal/server/server.go internal/server/api_settings.go cmd/tefnut/main.go internal/server/server_test.go
git commit -m "feat: jail library-path adds to configured allowedRoots"
```

---

## Self-Review

**Spec coverage:**
- Config `allowedRoots` default `[rootPath]`, abs-resolved — Task 2 Step 1. ✓
- Symlink resolution both sides + `filepath.Rel` containment — Task 1 Step 3. ✓
- 400 reject, fixed message, no path interpolation, fail-closed on error — Task 2 Step 3 (`err != nil || !ok`) + the literal string. ✓
- Only API adds gated; seed/existing rows untouched — gate is only in `apiAddPath`; main.go seed and DB rows are not touched. ✓
- No auth, bind unchanged — nothing in either task touches auth or `cfg.Server.Addr`. ✓
- Tests: containment unit cases (inside, root, outside, prefix-trap, `..`, symlink escape, multiple, empty, missing-dir error) + integration (inside 200 / outside 400 / no leak) — Tasks 1 & 2. ✓

**Placeholder scan:** No TBD/TODO; every step shows complete code and exact commands.

**Type consistency:** `PathWithinRoots(dir string, roots []string) (bool, error)` is defined in Task 1 and called identically in Task 2 Step 3. `NewServer(..., allowedRoots []string)` defined in Task 2 Step 2 and the call sites (main.go, newTestServer) updated in the same task. `Server.allowedRoots` set in `NewServer` and read in `apiAddPath` and the tests. `config.Library.AllowedRoots` defined in Step 1 and consumed in main.go Step 2.
