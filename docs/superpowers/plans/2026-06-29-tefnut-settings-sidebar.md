# Tefnut Settings / Sidebar / Scan-Mode Enhancement — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a sidebar nav, a DB-backed Settings page (multiple library paths + selectable scan mode that takes effect live without restart), and visible reader paging buttons — on top of the existing Tefnut comic server.

**Architecture:** Settings live in SQLite (`library_paths` + `settings` tables) and become the source of truth; `config.yaml` is bootstrap-only. The scanner becomes multi-root (each configured path is a top-level node named by a custom label). A new `scan.Manager` owns scheduling and is reconfigured live (interval / daily / watch) whenever settings change. The frontend gains a left sidebar, a settings page, and visible reader paging controls.

**Tech Stack:** Go 1.24, echo v4, modernc.org/sqlite, robfig/cron/v3, github.com/fsnotify/fsnotify, html/template + go:embed.

## Global Constraints

Every task's requirements implicitly include this section.

- **Settings source of truth is SQLite.** `config.yaml` only bootstraps `server.addr`, `dataDir`, `thumbnail.width` (and seeds the first library path + scan settings on first run). Changing settings via the API must take effect live (no restart) by calling the scan manager's `Reconfigure`.
- **Each configured library path = one top-level node** (`parent_id=0`, `type=2` directory) whose `name` is the user's custom label (default = path basename), `path` = the absolute path.
- **Scan modes are mutually exclusive:** exactly one of `interval` | `daily` | `watch`. Startup always runs one full blocking scan first, then starts the active mode.
- **Scan settings keys + defaults:** `scan_mode`=`interval`, `scan_interval`=`2m` (Go duration), `scan_daily_time`=`03:00` (`HH:MM`, 24h).
- **Watch mode** = `fsnotify` recursive watch of each library path + ~3s debounce; **no** fallback periodic rescan.
- **Reader is full-screen (no sidebar).** Sidebar appears on library / tags / settings pages only.
- **Node types:** comic=1, directory=2. Page index is 0-based. Rating 0..5.
- **Style:** small focused files (<800 lines); validate all input at boundaries; never silently swallow errors; wrap errors with a package-prefixed context; immutable patterns where reasonable; frontend `fetch` calls surface failures (alert on user actions).
- **Commit prefixes:** `feat:` `fix:` `refactor:` `docs:` `test:` `chore:`.

## File Structure

```
internal/store/store.go                 MODIFY  add library_paths + settings tables to schema
internal/store/settings_repo.go         CREATE  SettingsRepo: GetScan / SetScan (defaults applied)
internal/store/settings_repo_test.go    CREATE
internal/store/library_path_repo.go     CREATE  LibraryPathRepo: List/Get/Add/Rename/Delete
internal/store/library_path_repo_test.go CREATE
internal/store/node_repo.go             MODIFY  add UpdateName(ctx,id,name)
internal/library/scanner.go             MODIFY  multi-root scan (depends on LibraryPathRepo)
internal/library/scanner_test.go        MODIFY  multi-root tests
internal/scan/manager.go                CREATE  Manager (Start/Stop/Reconfigure) + interval/daily
internal/scan/manager_test.go           CREATE
internal/scan/watch.go                  CREATE  fsnotify recursive watch + debouncer
internal/scan/watch_test.go             CREATE
internal/server/api_settings.go         CREATE  settings + library-path API handlers
internal/server/server.go               MODIFY  Server fields (reconf, settings, paths) + routes
internal/server/server_test.go          MODIFY  newTestServer wiring + tests
internal/server/pages.go                MODIFY  pageSettings
internal/server/web/templates/layout.html    MODIFY  sidebar
internal/server/web/templates/reader.html     MODIFY  blank sidebar + visible paging buttons
internal/server/web/templates/settings.html   CREATE
internal/server/web/static/css/app.css         MODIFY  sidebar / settings / reader buttons
internal/server/web/static/js/settings.js      CREATE
internal/server/web/static/js/reader.js        MODIFY  bind visible paging buttons
cmd/tefnut/main.go                      MODIFY  seed + multi-root scanner + ScanManager wiring
```

---

## Task 1: Store schema + SettingsRepo

**Files:**
- Modify: `internal/store/store.go` (schema)
- Create: `internal/store/settings_repo.go`
- Test: `internal/store/settings_repo_test.go`

**Interfaces:**
- Produces:
  - In `store.go` schema, two new tables: `library_paths(id PK, name TEXT NOT NULL, path TEXT NOT NULL UNIQUE, created_at INTEGER NOT NULL)` and `settings(key TEXT PRIMARY KEY, value TEXT NOT NULL)`.
  - `store.ScanSettings` struct: `Mode string; Interval string; DailyTime string`.
  - `store.NewSettingsRepo(db *DB) *SettingsRepo`.
  - `(*SettingsRepo) GetScan(ctx) (ScanSettings, error)` — missing keys fall back to defaults (`interval`/`2m`/`03:00`).
  - `(*SettingsRepo) SetScan(ctx, ScanSettings) error` — upserts the three keys in one transaction.

- [ ] **Step 1: Add the two tables to the schema**

In `internal/store/store.go`, append to the `schema` const string (before the closing backtick):

```sql

CREATE TABLE IF NOT EXISTS library_paths (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  name       TEXT    NOT NULL,
  path       TEXT    NOT NULL UNIQUE,
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
```

- [ ] **Step 2: Write the failing test**

Create `internal/store/settings_repo_test.go`:

```go
package store

import (
	"context"
	"testing"
)

func TestGetScanDefaults(t *testing.T) {
	r := NewSettingsRepo(openTemp(t))
	s, err := r.GetScan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Mode != "interval" || s.Interval != "2m" || s.DailyTime != "03:00" {
		t.Fatalf("defaults wrong: %+v", s)
	}
}

func TestSetGetScan(t *testing.T) {
	ctx := context.Background()
	r := NewSettingsRepo(openTemp(t))
	want := ScanSettings{Mode: "daily", Interval: "1h", DailyTime: "04:30"}
	if err := r.SetScan(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := r.GetScan(ctx)
	if err != nil || got != want {
		t.Fatalf("got %+v err %v", got, err)
	}
	// partial override still returns stored values for set keys
	if err := r.SetScan(ctx, ScanSettings{Mode: "watch", Interval: "1h", DailyTime: "04:30"}); err != nil {
		t.Fatal(err)
	}
	got, _ = r.GetScan(ctx)
	if got.Mode != "watch" {
		t.Fatalf("mode = %s", got.Mode)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestSetGetScan -v` → FAIL (undefined `NewSettingsRepo`).

- [ ] **Step 4: Implement SettingsRepo**

Create `internal/store/settings_repo.go`:

```go
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type ScanSettings struct {
	Mode      string
	Interval  string
	DailyTime string
}

const (
	keyScanMode     = "scan_mode"
	keyScanInterval = "scan_interval"
	keyScanDaily    = "scan_daily_time"

	defScanMode     = "interval"
	defScanInterval = "2m"
	defScanDaily    = "03:00"
)

type SettingsRepo struct {
	db *sql.DB
}

func NewSettingsRepo(db *DB) *SettingsRepo { return &SettingsRepo{db: db.SQL()} }

func (r *SettingsRepo) get(ctx context.Context, key, def string) (string, error) {
	var v string
	err := r.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return def, nil
	}
	if err != nil {
		return "", fmt.Errorf("store: get setting %q: %w", key, err)
	}
	return v, nil
}

func (r *SettingsRepo) GetScan(ctx context.Context) (ScanSettings, error) {
	mode, err := r.get(ctx, keyScanMode, defScanMode)
	if err != nil {
		return ScanSettings{}, err
	}
	interval, err := r.get(ctx, keyScanInterval, defScanInterval)
	if err != nil {
		return ScanSettings{}, err
	}
	daily, err := r.get(ctx, keyScanDaily, defScanDaily)
	if err != nil {
		return ScanSettings{}, err
	}
	return ScanSettings{Mode: mode, Interval: interval, DailyTime: daily}, nil
}

func (r *SettingsRepo) SetScan(ctx context.Context, s ScanSettings) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback()
	pairs := [][2]string{
		{keyScanMode, s.Mode},
		{keyScanInterval, s.Interval},
		{keyScanDaily, s.DailyTime},
	}
	for _, p := range pairs {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO settings (key, value) VALUES (?, ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, p[0], p[1])
		if err != nil {
			return fmt.Errorf("store: set setting %q: %w", p[0], err)
		}
	}
	return tx.Commit()
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/store/...` → PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/store/store.go internal/store/settings_repo.go internal/store/settings_repo_test.go
git commit -m "feat: add settings table and SettingsRepo"
```

---

## Task 2: LibraryPathRepo

**Files:**
- Create: `internal/store/library_path_repo.go`
- Test: `internal/store/library_path_repo_test.go`

**Interfaces:**
- Consumes: `store.DB`, `ErrNotFound`, `ErrDuplicate` (from v1).
- Produces (`NewLibraryPathRepo(db *DB) *LibraryPathRepo`):
  - `store.LibraryPath` struct: `ID int64; Name string; Path string`.
  - `List(ctx) ([]*LibraryPath, error)` — ordered by `id`.
  - `Get(ctx, id int64) (*LibraryPath, error)` — `ErrNotFound` if absent.
  - `Add(ctx, name, path string) (*LibraryPath, error)` — returns `ErrDuplicate` if `path` already exists.
  - `Rename(ctx, id int64, name string) error`.
  - `Delete(ctx, id int64) error`.

- [ ] **Step 1: Write the failing test**

Create `internal/store/library_path_repo_test.go`:

```go
package store

import (
	"context"
	"errors"
	"testing"
)

func TestLibraryPathAddListGet(t *testing.T) {
	ctx := context.Background()
	r := NewLibraryPathRepo(openTemp(t))
	lp, err := r.Add(ctx, "我的漫画", "/Users/x/comic")
	if err != nil {
		t.Fatal(err)
	}
	if lp.ID == 0 || lp.Name != "我的漫画" {
		t.Fatalf("got %+v", lp)
	}
	got, err := r.Get(ctx, lp.ID)
	if err != nil || got.Path != "/Users/x/comic" {
		t.Fatalf("get %+v err %v", got, err)
	}
	list, _ := r.List(ctx)
	if len(list) != 1 {
		t.Fatalf("list len %d", len(list))
	}
}

func TestLibraryPathDuplicate(t *testing.T) {
	ctx := context.Background()
	r := NewLibraryPathRepo(openTemp(t))
	r.Add(ctx, "a", "/p")
	if _, err := r.Add(ctx, "b", "/p"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}
}

func TestLibraryPathRenameDelete(t *testing.T) {
	ctx := context.Background()
	r := NewLibraryPathRepo(openTemp(t))
	lp, _ := r.Add(ctx, "old", "/p")
	if err := r.Rename(ctx, lp.ID, "new"); err != nil {
		t.Fatal(err)
	}
	got, _ := r.Get(ctx, lp.ID)
	if got.Name != "new" {
		t.Fatalf("name = %s", got.Name)
	}
	if err := r.Delete(ctx, lp.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(ctx, lp.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected gone, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestLibraryPath -v` → FAIL (undefined `NewLibraryPathRepo`).

- [ ] **Step 3: Implement LibraryPathRepo**

Create `internal/store/library_path_repo.go`:

```go
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type LibraryPath struct {
	ID   int64
	Name string
	Path string
}

type LibraryPathRepo struct {
	db *sql.DB
}

func NewLibraryPathRepo(db *DB) *LibraryPathRepo { return &LibraryPathRepo{db: db.SQL()} }

func (r *LibraryPathRepo) List(ctx context.Context) ([]*LibraryPath, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, name, path FROM library_paths ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list library paths: %w", err)
	}
	defer rows.Close()
	out := []*LibraryPath{}
	for rows.Next() {
		lp := &LibraryPath{}
		if err := rows.Scan(&lp.ID, &lp.Name, &lp.Path); err != nil {
			return nil, fmt.Errorf("store: scan library path: %w", err)
		}
		out = append(out, lp)
	}
	return out, rows.Err()
}

func (r *LibraryPathRepo) Get(ctx context.Context, id int64) (*LibraryPath, error) {
	lp := &LibraryPath{}
	err := r.db.QueryRowContext(ctx, `SELECT id, name, path FROM library_paths WHERE id = ?`, id).
		Scan(&lp.ID, &lp.Name, &lp.Path)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get library path %d: %w", id, err)
	}
	return lp, nil
}

func (r *LibraryPathRepo) Add(ctx context.Context, name, path string) (*LibraryPath, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO library_paths (name, path, created_at) VALUES (?, ?, ?)`,
		name, path, time.Now().Unix())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrDuplicate
		}
		return nil, fmt.Errorf("store: add library path %q: %w", path, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("store: add library path last id: %w", err)
	}
	return &LibraryPath{ID: id, Name: name, Path: path}, nil
}

func (r *LibraryPathRepo) Rename(ctx context.Context, id int64, name string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE library_paths SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		return fmt.Errorf("store: rename library path %d: %w", id, err)
	}
	return nil
}

func (r *LibraryPathRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM library_paths WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete library path %d: %w", id, err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/...` → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/library_path_repo.go internal/store/library_path_repo_test.go
git commit -m "feat: add LibraryPathRepo"
```

---

## Task 3: Multi-root scanner

**Files:**
- Modify: `internal/store/node_repo.go` (add `UpdateName`)
- Modify: `internal/library/scanner.go`
- Modify: `internal/library/scanner_test.go`
- Modify: `cmd/tefnut/main.go` (construct pathRepo + seed + new scanner; keep existing cron)

**Interfaces:**
- Consumes: `store.LibraryPathRepo.List`, `store.NodeRepo`, `archive`, `thumb`.
- Produces:
  - `(*store.NodeRepo) UpdateName(ctx, id int64, name string) error`.
  - `library.NewScanner(repo *store.NodeRepo, paths *store.LibraryPathRepo, dataDir string, thumbWidth int) *Scanner` — **new signature** (replaces the old `root string` param with `paths`).
  - `(*Scanner) Scan(ctx) error` — upserts one top-level node per configured library path (named by `LibraryPath.Name`), scans each into its subtree, removes nodes for deconfigured paths.

- [ ] **Step 1: Add `UpdateName` to NodeRepo**

In `internal/store/node_repo.go`, add:

```go
func (r *NodeRepo) UpdateName(ctx context.Context, id int64, name string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE nodes SET name=?, updated_at=? WHERE id=?`, name, time.Now().Unix(), id)
	if err != nil {
		return fmt.Errorf("store: update name %d: %w", id, err)
	}
	return nil
}
```

- [ ] **Step 2: Rewrite the scanner test for multi-root**

Replace the helper + first test in `internal/library/scanner_test.go`. Change `newTestScanner` and add multi-root tests (keep `pngBytes`, `writeZip` helpers and `util_test.go` as-is):

```go
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
```

> The old single-root tests (`TestScanCreatesComicWithCoverAndPages`, `TestScanRemovesDeletedFiles`, `TestScanIsIdempotent`) call `newTestScanner` and reference a single `root`. Update each: capture `paths` from the new `newTestScanner` signature and, before scanning, register the temp library with `paths.Add(ctx, "Lib", root)` where `root` is the temp dir those tests write zips into. Keep their assertions but account for the extra top-level library node (their comic is now one level deeper, under the library node). Concretely, in each, after `sc.Scan`, get the library root then its children. Adjust assertions to navigate `ListChildren(0)[0].ID` → the comic.

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/library/...` → FAIL (NewScanner signature mismatch / undefined).

- [ ] **Step 4: Rewrite scanner.go Scan for multi-root**

In `internal/library/scanner.go`, change the struct + constructor + `Scan` (keep `scanDir`, `buildComic`, `writeThumb`, `readerOnly`, `removeNode`, `thumbPath`, `cacheDir` unchanged):

```go
type Scanner struct {
	repo       *store.NodeRepo
	paths      *store.LibraryPathRepo
	dataDir    string
	thumbWidth int
	mu         sync.Mutex
}

func NewScanner(repo *store.NodeRepo, paths *store.LibraryPathRepo, dataDir string, thumbWidth int) *Scanner {
	return &Scanner{repo: repo, paths: paths, dataDir: dataDir, thumbWidth: thumbWidth}
}

// Scan performs a full idempotent sync of all configured library paths.
func (s *Scanner) Scan(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	libs, err := s.paths.List(ctx)
	if err != nil {
		return fmt.Errorf("scanner: list library paths: %w", err)
	}
	roots, err := s.repo.ListChildren(ctx, 0)
	if err != nil {
		return err
	}
	byPath := map[string]*store.Node{}
	for _, n := range roots {
		byPath[n.Path] = n
	}

	for _, lib := range libs {
		abs, err := filepath.Abs(lib.Path)
		if err != nil {
			log.Printf("scanner: abs %s: %v", lib.Path, err)
			continue
		}
		node, seen := byPath[abs]
		if seen {
			delete(byPath, abs)
			if node.Name != lib.Name {
				if err := s.repo.UpdateName(ctx, node.ID, lib.Name); err != nil {
					log.Printf("scanner: rename root %d: %v", node.ID, err)
				}
			}
		} else {
			node, err = s.repo.Create(ctx, &store.Node{
				ParentID: 0, Name: lib.Name, Path: abs, Type: store.NodeDir,
			})
			if err != nil {
				log.Printf("scanner: create library node %s: %v", abs, err)
				continue
			}
		}
		if _, err := os.Stat(abs); err != nil {
			log.Printf("scanner: library path %s unavailable: %v", abs, err)
			continue
		}
		if err := s.scanDir(ctx, abs, node.ID); err != nil {
			log.Printf("scanner: scan library %s: %v", abs, err)
		}
	}

	for _, n := range byPath {
		s.removeNode(ctx, n)
	}
	return nil
}
```

Ensure imports include `fmt`, `log`, `os`, `path/filepath` (already present except possibly `fmt` — add it).

- [ ] **Step 5: Update main.go to construct the new scanner (keep existing cron)**

In `cmd/tefnut/main.go`, replace the scanner-construction region. After opening the DB and building `nodes/tags/progress`, add:

```go
	settingsRepo := store.NewSettingsRepo(db)
	pathRepo := store.NewLibraryPathRepo(db)

	// First-run seed: import the yaml rootPath as the first library.
	if libs, err := pathRepo.List(context.Background()); err == nil && len(libs) == 0 && cfg.Library.RootPath != "" {
		if _, err := pathRepo.Add(context.Background(), filepath.Base(cfg.Library.RootPath), cfg.Library.RootPath); err != nil {
			log.Printf("seed library path: %v", err)
		}
	}

	scanner := library.NewScanner(nodes, pathRepo, cfg.DataDir, cfg.Thumbnail.Width)
```

Leave the existing startup `scanner.Scan(...)` call, the `cron.New()` interval block, and `server.NewServer(...)` exactly as they are for now (they still compile — `scanner.Scan` is unchanged). `settingsRepo` is unused this task; add `_ = settingsRepo` right after its declaration to keep the build clean (removed in Task 6).

- [ ] **Step 6: Run tests + build**

Run: `go build ./... && go test ./internal/library/... ./internal/store/...`
Expected: build clean, tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/store/node_repo.go internal/library/scanner.go internal/library/scanner_test.go cmd/tefnut/main.go
git commit -m "feat: scan multiple configured library paths as top-level nodes"
```

---

## Task 4: ScanManager core (interval + daily)

**Files:**
- Create: `internal/scan/manager.go`
- Test: `internal/scan/manager_test.go`

**Interfaces:**
- Consumes: `store.SettingsRepo.GetScan`, `store.LibraryPathRepo`, `store.ScanSettings`, `robfig/cron/v3`.
- Produces:
  - `scan.Scanner` interface: `Scan(ctx context.Context) error`.
  - `scan.New(sc Scanner, settings *store.SettingsRepo, paths *store.LibraryPathRepo) *Manager`.
  - `(*Manager) Start(ctx context.Context) error` — runs one blocking `Scan`, then starts the active mode.
  - `(*Manager) Reconfigure(ctx context.Context) error` — stops the current mode, starts the mode from current settings, then triggers an async `Scan`.
  - `(*Manager) Stop()`.
  - `scan.cronSpec(s store.ScanSettings) (string, error)` — pure: `interval`→`@every <interval>`; `daily`→`<min> <hour> * * *`; errors on bad input. (Watch returns `"", nil` — handled separately; see Task 5.)

- [ ] **Step 1: Write failing tests**

Create `internal/scan/manager_test.go`:

```go
package scan

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"Tefnut/internal/store"
)

type fakeScanner struct {
	mu sync.Mutex
	n  int
	ch chan struct{}
}

func (f *fakeScanner) Scan(ctx context.Context) error {
	f.mu.Lock()
	f.n++
	f.mu.Unlock()
	if f.ch != nil {
		select {
		case f.ch <- struct{}{}:
		default:
		}
	}
	return nil
}
func (f *fakeScanner) count() int { f.mu.Lock(); defer f.mu.Unlock(); return f.n }

func newRepos(t *testing.T) (*store.SettingsRepo, *store.LibraryPathRepo) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return store.NewSettingsRepo(db), store.NewLibraryPathRepo(db)
}

func TestCronSpec(t *testing.T) {
	cases := []struct {
		in   store.ScanSettings
		want string
		err  bool
	}{
		{store.ScanSettings{Mode: "interval", Interval: "30m"}, "@every 30m", false},
		{store.ScanSettings{Mode: "interval", Interval: "bad"}, "", true},
		{store.ScanSettings{Mode: "daily", DailyTime: "03:05"}, "5 3 * * *", false},
		{store.ScanSettings{Mode: "daily", DailyTime: "9:99"}, "", true},
	}
	for _, c := range cases {
		got, err := cronSpec(c.in)
		if c.err && err == nil {
			t.Errorf("%+v: expected error", c.in)
		}
		if !c.err && got != c.want {
			t.Errorf("%+v: got %q want %q", c.in, got, c.want)
		}
	}
}

func TestStartRunsInitialScan(t *testing.T) {
	settings, paths := newRepos(t)
	fs := &fakeScanner{}
	m := New(fs, settings, paths)
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	if fs.count() < 1 {
		t.Fatalf("expected initial scan, count=%d", fs.count())
	}
}

func TestReconfigureTriggersScan(t *testing.T) {
	settings, paths := newRepos(t)
	fs := &fakeScanner{ch: make(chan struct{}, 4)}
	m := New(fs, settings, paths)
	m.Start(context.Background())
	defer m.Stop()
	<-fs.ch // initial
	settings.SetScan(context.Background(), store.ScanSettings{Mode: "interval", Interval: "1h", DailyTime: "03:00"})
	if err := m.Reconfigure(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fs.ch:
	case <-time.After(2 * time.Second):
		t.Fatal("reconfigure did not trigger a scan")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/scan/...` → FAIL (package/symbols undefined).

- [ ] **Step 3: Implement manager.go**

Create `internal/scan/manager.go`:

```go
package scan

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"Tefnut/internal/store"
)

// Scanner is the subset of library.Scanner the manager drives.
type Scanner interface {
	Scan(ctx context.Context) error
}

type Manager struct {
	scanner  Scanner
	settings *store.SettingsRepo
	paths    *store.LibraryPathRepo

	mu       sync.Mutex
	cron     *cron.Cron
	stopMode func() // tears down the current mode (cron stop / watcher close)
	debounce time.Duration
	baseCtx  context.Context
}

func New(sc Scanner, settings *store.SettingsRepo, paths *store.LibraryPathRepo) *Manager {
	return &Manager{scanner: sc, settings: settings, paths: paths, debounce: 3 * time.Second}
}

// Start runs one blocking scan, then starts the active mode.
func (m *Manager) Start(ctx context.Context) error {
	m.baseCtx = ctx
	if err := m.scanner.Scan(ctx); err != nil {
		log.Printf("scan: initial scan: %v", err)
	}
	return m.applyMode(ctx)
}

// Reconfigure tears down the current mode, starts the mode from current
// settings, then triggers an async rescan.
func (m *Manager) Reconfigure(ctx context.Context) error {
	if err := m.applyMode(ctx); err != nil {
		return err
	}
	go func() {
		if err := m.scanner.Scan(ctx); err != nil {
			log.Printf("scan: reconfigure rescan: %v", err)
		}
	}()
	return nil
}

func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.teardownLocked()
}

func (m *Manager) teardownLocked() {
	if m.stopMode != nil {
		m.stopMode()
		m.stopMode = nil
	}
	m.cron = nil
}

func (m *Manager) applyMode(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.teardownLocked()

	settings, err := m.settings.GetScan(ctx)
	if err != nil {
		return fmt.Errorf("scan: read settings: %w", err)
	}

	switch settings.Mode {
	case "watch":
		return m.startWatchLocked(ctx)
	case "interval", "daily":
		spec, err := cronSpec(settings)
		if err != nil {
			return err
		}
		c := cron.New()
		if _, err := c.AddFunc(spec, func() {
			if err := m.scanner.Scan(ctx); err != nil {
				log.Printf("scan: scheduled scan: %v", err)
			}
		}); err != nil {
			return fmt.Errorf("scan: schedule %q: %w", spec, err)
		}
		c.Start()
		m.cron = c
		m.stopMode = func() { c.Stop() }
		return nil
	default:
		return fmt.Errorf("scan: unknown mode %q", settings.Mode)
	}
}

// cronSpec converts settings into a robfig/cron spec. Watch mode returns "".
func cronSpec(s store.ScanSettings) (string, error) {
	switch s.Mode {
	case "interval":
		d, err := time.ParseDuration(s.Interval)
		if err != nil || d <= 0 {
			return "", fmt.Errorf("scan: invalid interval %q", s.Interval)
		}
		return "@every " + s.Interval, nil
	case "daily":
		h, min, err := parseHHMM(s.DailyTime)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d %d * * *", min, h), nil
	case "watch":
		return "", nil
	default:
		return "", fmt.Errorf("scan: unknown mode %q", s.Mode)
	}
}

func parseHHMM(v string) (hour, min int, err error) {
	parts := strings.SplitN(v, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("scan: invalid time %q", v)
	}
	hour, err1 := strconv.Atoi(parts[0])
	min, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || hour < 0 || hour > 23 || min < 0 || min > 59 {
		return 0, 0, fmt.Errorf("scan: invalid time %q", v)
	}
	return hour, min, nil
}
```

> `startWatchLocked` is implemented in Task 5. For THIS task, add a temporary stub at the bottom of `manager.go` so it compiles, then Task 5 replaces it:
> ```go
> func (m *Manager) startWatchLocked(ctx context.Context) error { return fmt.Errorf("scan: watch mode not yet implemented") }
> ```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/scan/...`
Expected: PASS (interval/daily + cronSpec + start/reconfigure).

- [ ] **Step 5: Commit**

```bash
git add internal/scan/manager.go internal/scan/manager_test.go
git commit -m "feat: add scan manager with interval and daily modes"
```

---

## Task 5: Watch mode (fsnotify + debounce)

**Files:**
- Create: `internal/scan/watch.go`
- Test: `internal/scan/watch_test.go`
- Modify: `internal/scan/manager.go` (remove the `startWatchLocked` stub; the real one lives in watch.go)

**Interfaces:**
- Consumes: `github.com/fsnotify/fsnotify`, `m.paths.List`, `m.scanner.Scan`, `m.debounce`.
- Produces:
  - `(*Manager) startWatchLocked(ctx context.Context) error` — creates a recursive fsnotify watch over every configured library path and triggers a debounced `Scan` on any change.
  - `debouncer` type: `newDebouncer(d time.Duration, fn func()) *debouncer`; `(*debouncer) trigger()`; `(*debouncer) stop()` — calls `fn` once after `d` of quiet following the last `trigger()`.

- [ ] **Step 1: Ensure fsnotify is a direct dependency**

Run: `go get github.com/fsnotify/fsnotify@latest`
(fsnotify is currently indirect/old; this makes it direct and modern. Keep `go mod tidy` OFF per the project's convention; `go get` adds the go.sum entries.)

- [ ] **Step 2: Write failing tests (debouncer is the deterministic unit)**

Create `internal/scan/watch_test.go`:

```go
package scan

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestDebouncerFiresOnceAfterQuiet(t *testing.T) {
	var calls int32
	d := newDebouncer(50*time.Millisecond, func() { atomic.AddInt32(&calls, 1) })
	defer d.stop()
	// rapid triggers should collapse into a single call
	for i := 0; i < 5; i++ {
		d.trigger()
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(120 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 call, got %d", got)
	}
}

func TestDebouncerSeparateBurstsFireSeparately(t *testing.T) {
	var calls int32
	d := newDebouncer(40*time.Millisecond, func() { atomic.AddInt32(&calls, 1) })
	defer d.stop()
	d.trigger()
	time.Sleep(100 * time.Millisecond)
	d.trigger()
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 calls, got %d", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/scan/ -run Debouncer -v` → FAIL (undefined `newDebouncer`).

- [ ] **Step 4: Implement watch.go (and delete the stub in manager.go)**

Delete the temporary `startWatchLocked` stub at the bottom of `manager.go`. Create `internal/scan/watch.go`:

```go
package scan

import (
	"context"
	"io/fs"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// debouncer calls fn once after d of quiet following the last trigger().
type debouncer struct {
	d     time.Duration
	fn    func()
	mu    sync.Mutex
	timer *time.Timer
}

func newDebouncer(d time.Duration, fn func()) *debouncer {
	return &debouncer{d: d, fn: fn}
}

func (db *debouncer) trigger() {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.timer != nil {
		db.timer.Stop()
	}
	db.timer = time.AfterFunc(db.d, db.fn)
}

func (db *debouncer) stop() {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.timer != nil {
		db.timer.Stop()
		db.timer = nil
	}
}

// startWatchLocked must be called with m.mu held.
func (m *Manager) startWatchLocked(ctx context.Context) error {
	libs, err := m.paths.List(ctx)
	if err != nil {
		return err
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	addTree := func(root string) {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if aerr := w.Add(p); aerr != nil {
					log.Printf("scan: watch add %s: %v", p, aerr)
				}
			}
			return nil
		})
	}
	for _, lib := range libs {
		addTree(lib.Path)
	}

	deb := newDebouncer(m.debounce, func() {
		if err := m.scanner.Scan(ctx); err != nil {
			log.Printf("scan: watch-triggered scan: %v", err)
		}
	})

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				// newly created directories must be watched too
				if ev.Op&fsnotify.Create == fsnotify.Create {
					if info, statErr := filepath.Glob(ev.Name); statErr == nil && len(info) > 0 {
						addTree(ev.Name)
					}
				}
				deb.trigger()
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				log.Printf("scan: watcher error: %v", err)
			}
		}
	}()

	m.stopMode = func() {
		close(done)
		deb.stop()
		w.Close()
	}
	return nil
}
```

- [ ] **Step 5: Run tests + build**

Run: `go test ./internal/scan/... && go build ./...`
Expected: PASS, build clean. (Debouncer covered by unit tests; the fsnotify wiring is integration/manual-tested — note this gap.)

- [ ] **Step 6: Commit**

```bash
git add internal/scan/watch.go internal/scan/watch_test.go internal/scan/manager.go go.mod go.sum
git commit -m "feat: add watch scan mode with fsnotify and debounce"
```

---

## Task 6: Wire ScanManager into main

**Files:**
- Modify: `cmd/tefnut/main.go`

**Interfaces:**
- Consumes: `scan.New`, `(*Manager).Start/Stop`, the scanner + repos built earlier.

- [ ] **Step 1: Replace the cron block with the manager**

In `cmd/tefnut/main.go`: remove the `_ = settingsRepo` line, the `interval, _ := cfg.ScanInterval()` block, the `cron.New()`/`AddFunc`/`c.Start()`/`defer c.Stop()` block, AND the standalone startup `scanner.Scan(...)` call (the manager's `Start` now does the initial scan). Replace with:

```go
	manager := scan.New(scanner, settingsRepo, pathRepo)
	if err := manager.Start(context.Background()); err != nil {
		log.Printf("scan manager start: %v", err)
	}
	defer manager.Stop()
```

Remove the now-unused `cron` and (if unused) `time` imports; add `"Tefnut/internal/scan"`. Keep `server.NewServer(...)` exactly as-is for now (updated in Task 7).

- [ ] **Step 2: Build + vet + smoke**

Run: `go build ./... && go vet ./...`
Expected: clean.

Smoke (interval mode still works through the manager): build the binary, point it at a temp library with one zip-of-PNG (as in the v1 Task 16 smoke), start it, `curl /healthz` → 200 and `curl '/api/nodes?parent=0'` → the library node. Kill it. Confirm the startup scan ran (a thumb file exists under `dataDir/thumbs`).

- [ ] **Step 3: Run full test suite**

Run: `go test ./...` → all PASS.

- [ ] **Step 4: Commit**

```bash
git add cmd/tefnut/main.go
git commit -m "refactor: drive scanning through ScanManager in main"
```

---

## Task 7: Settings API + Server wiring

**Files:**
- Create: `internal/server/api_settings.go`
- Modify: `internal/server/server.go` (Server fields, NewServer signature, routes)
- Modify: `internal/server/server_test.go` (newTestServer wiring + tests)
- Modify: `cmd/tefnut/main.go` (pass manager + repos to NewServer)

**Interfaces:**
- Consumes: `store.SettingsRepo`, `store.LibraryPathRepo`, `store.ScanSettings`, `store.ErrDuplicate`.
- Produces:
  - `server.Reconfigurer` interface: `Reconfigure(ctx context.Context) error` (satisfied by `*scan.Manager`).
  - `NewServer(nodes *store.NodeRepo, tags *store.TagRepo, progress *store.ProgressRepo, settings *store.SettingsRepo, paths *store.LibraryPathRepo, reconf Reconfigurer, dataDir string, thumbWidth int) *Server`.
  - Handlers: `apiGetSettings`, `apiUpdateSettings` (PUT), `apiAddPath` (POST), `apiDeletePath` (DELETE) — registered under `/api/settings`.

- [ ] **Step 1: Write failing tests**

In `internal/server/server_test.go`, update `newTestServer` to construct the new repos + a stub reconfigurer, and add tests. Change the helper:

```go
type stubReconf struct{ calls int }

func (s *stubReconf) Reconfigure(ctx context.Context) error { s.calls++; return nil }

func newTestServer(t *testing.T) (*Server, *echo.Echo, *store.DB) {
	t.Helper()
	data := t.TempDir()
	db, err := store.Open(filepath.Join(data, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := NewServer(store.NewNodeRepo(db), store.NewTagRepo(db), store.NewProgressRepo(db),
		store.NewSettingsRepo(db), store.NewLibraryPathRepo(db), &stubReconf{}, data, 400)
	e := echo.New()
	s.Register(e)
	return s, e, db
}
```

Add tests:

```go
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
	_, e, db := newTestServer(t)
	dir := t.TempDir()
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/...` → FAIL (NewServer signature / routes undefined).

- [ ] **Step 3: Implement api_settings.go**

Create `internal/server/api_settings.go`:

```go
package server

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"Tefnut/internal/store"
)

type settingsDTO struct {
	LibraryPaths  []*store.LibraryPath `json:"libraryPaths"`
	ScanMode      string               `json:"scanMode"`
	ScanInterval  string               `json:"scanInterval"`
	ScanDailyTime string               `json:"scanDailyTime"`
}

func (s *Server) apiGetSettings(c echo.Context) error {
	ctx := c.Request().Context()
	scan, err := s.settings.GetScan(ctx)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	paths, err := s.paths.List(ctx)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, settingsDTO{
		LibraryPaths: paths, ScanMode: scan.Mode,
		ScanInterval: scan.Interval, ScanDailyTime: scan.DailyTime,
	})
}

func validScanSettings(mode, interval, daily string) error {
	switch mode {
	case "interval":
		if d, err := time.ParseDuration(interval); err != nil || d <= 0 {
			return errors.New("scanInterval must be a positive duration like 30m or 2h")
		}
	case "daily":
		if !validHHMM(daily) {
			return errors.New("scanDailyTime must be HH:MM (00:00-23:59)")
		}
	case "watch":
		// no extra params
	default:
		return errors.New("scanMode must be interval, daily, or watch")
	}
	return nil
}

func validHHMM(v string) bool {
	parts := strings.SplitN(v, ":", 2)
	if len(parts) != 2 {
		return false
	}
	h, e1 := strconv.Atoi(parts[0])
	m, e2 := strconv.Atoi(parts[1])
	return e1 == nil && e2 == nil && h >= 0 && h <= 23 && m >= 0 && m <= 59
}

func (s *Server) apiUpdateSettings(c echo.Context) error {
	ctx := c.Request().Context()
	var body struct {
		ScanMode      string `json:"scanMode"`
		ScanInterval  string `json:"scanInterval"`
		ScanDailyTime string `json:"scanDailyTime"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	if err := validScanSettings(body.ScanMode, body.ScanInterval, body.ScanDailyTime); err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	if err := s.settings.SetScan(ctx, store.ScanSettings{
		Mode: body.ScanMode, Interval: body.ScanInterval, DailyTime: body.ScanDailyTime,
	}); err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	if err := s.reconf.Reconfigure(ctx); err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, nil)
}

func (s *Server) apiAddPath(c echo.Context) error {
	ctx := c.Request().Context()
	var body struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	p := strings.TrimSpace(body.Path)
	if p == "" {
		return fail(c, http.StatusBadRequest, errors.New("path is required"))
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return fail(c, http.StatusBadRequest, errors.New("path is not a readable directory"))
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = filepath.Base(abs)
	}
	lp, err := s.paths.Add(ctx, name, abs)
	if errors.Is(err, store.ErrDuplicate) {
		return fail(c, http.StatusConflict, errors.New("this path is already a library"))
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	if err := s.reconf.Reconfigure(ctx); err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, lp)
}

func (s *Server) apiDeletePath(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return fail(c, http.StatusBadRequest, errors.New("invalid id"))
	}
	if err := s.paths.Delete(ctx, id); err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	if err := s.reconf.Reconfigure(ctx); err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, nil)
}
```

- [ ] **Step 4: Update server.go (fields, constructor, routes)**

In `internal/server/server.go`: add the interface + fields + constructor params + routes.

```go
type Reconfigurer interface {
	Reconfigure(ctx context.Context) error
}

type Server struct {
	nodes      *store.NodeRepo
	tags       *store.TagRepo
	progress   *store.ProgressRepo
	settings   *store.SettingsRepo
	paths      *store.LibraryPathRepo
	reconf     Reconfigurer
	dataDir    string
	thumbWidth int
}

func NewServer(nodes *store.NodeRepo, tags *store.TagRepo, progress *store.ProgressRepo,
	settings *store.SettingsRepo, paths *store.LibraryPathRepo, reconf Reconfigurer,
	dataDir string, thumbWidth int) *Server {
	return &Server{nodes: nodes, tags: tags, progress: progress, settings: settings,
		paths: paths, reconf: reconf, dataDir: dataDir, thumbWidth: thumbWidth}
}
```

Add `"context"` to imports. In `Register`, add under the `/api` group:

```go
	api.GET("/settings", s.apiGetSettings)
	api.PUT("/settings", s.apiUpdateSettings)
	api.POST("/settings/paths", s.apiAddPath)
	api.DELETE("/settings/paths/:id", s.apiDeletePath)
```

- [ ] **Step 5: Update main.go to pass the manager + repos**

In `cmd/tefnut/main.go`, change the `server.NewServer(...)` call to:

```go
	srv := server.NewServer(nodes, tags, progress, settingsRepo, pathRepo, manager, cfg.DataDir, cfg.Thumbnail.Width)
```

- [ ] **Step 6: Run tests + build**

Run: `go build ./... && go test ./...`
Expected: build clean, all tests PASS (incl. the 4 new settings tests).

- [ ] **Step 7: Commit**

```bash
git add internal/server/api_settings.go internal/server/server.go internal/server/server_test.go cmd/tefnut/main.go
git commit -m "feat: add settings API and wire ScanManager reconfigure"
```

---

## Task 8: Settings page

**Files:**
- Modify: `internal/server/server.go` (route `GET /settings`)
- Modify: `internal/server/pages.go` (pageSettings)
- Create: `internal/server/web/templates/settings.html`
- Create: `internal/server/web/static/js/settings.js`
- Test: `internal/server/server_test.go` (render test)

**Interfaces:**
- Consumes: `render(c, "settings.html", data)` (from v1 pages.go), `s.settings.GetScan`, `s.paths.List`.
- Produces: `s.pageSettings(c echo.Context) error` rendering `settings.html`; route `GET /settings`.

- [ ] **Step 1: Write the failing render test**

Append to `internal/server/server_test.go`:

```go
func TestPageSettingsRenders(t *testing.T) {
	_, e, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "扫描方式") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestPageSettingsRenders -v` → FAIL (route 404 / pageSettings undefined).

- [ ] **Step 3: Create the settings template**

Create `internal/server/web/templates/settings.html`:

```html
{{define "title"}}设置 · Tefnut{{end}}
{{define "content"}}
<h1>设置</h1>

<section class="card-box">
  <h2>漫画库路径</h2>
  <ul id="paths">
    {{range .LibraryPaths}}
    <li data-id="{{.ID}}"><span class="lib-name">{{.Name}}</span><span class="lib-path">{{.Path}}</span><button class="del">删除</button></li>
    {{end}}
  </ul>
  <form id="addpath" class="row">
    <input id="np-path" placeholder="库目录绝对路径，如 /Users/me/comic">
    <input id="np-name" placeholder="显示名（默认取末段）">
    <button>添加</button>
  </form>
</section>

<section class="card-box">
  <h2>扫描方式</h2>
  <form id="scanform">
    <label><input type="radio" name="mode" value="interval"> 定时间隔</label>
    <span class="mode-args" data-mode="interval"><input id="iv" placeholder="如 30m / 2h"></span>
    <label><input type="radio" name="mode" value="daily"> 每日定时</label>
    <span class="mode-args" data-mode="daily"><input id="daily" placeholder="HH:MM，如 03:00"></span>
    <label><input type="radio" name="mode" value="watch"> 监控路径（文件变化即扫描）</label>
    <div><button type="submit">保存</button></div>
  </form>
</section>

<script id="scan-data" type="application/json">{"mode":"{{.ScanMode}}","interval":"{{.ScanInterval}}","daily":"{{.ScanDailyTime}}"}</script>
{{end}}
{{define "scripts"}}<script src="/static/js/settings.js"></script>{{end}}
```

- [ ] **Step 4: Create settings.js**

Create `internal/server/web/static/js/settings.js`:

```js
const initial = JSON.parse(document.getElementById('scan-data').textContent);

// reflect current scan settings into the form
document.querySelector(`input[name="mode"][value="${initial.mode}"]`).checked = true;
document.getElementById('iv').value = initial.interval;
document.getElementById('daily').value = initial.daily;

function syncModeArgs() {
  const mode = document.querySelector('input[name="mode"]:checked').value;
  document.querySelectorAll('.mode-args').forEach(el => {
    el.style.display = el.dataset.mode === mode ? '' : 'none';
  });
}
document.querySelectorAll('input[name="mode"]').forEach(r => r.addEventListener('change', syncModeArgs));
syncModeArgs();

// auto-fill name from path basename
document.getElementById('np-path').addEventListener('input', (e) => {
  const nameEl = document.getElementById('np-name');
  if (!nameEl.dataset.touched) {
    const parts = e.target.value.replace(/\/+$/, '').split('/');
    nameEl.value = parts[parts.length - 1] || '';
  }
});
document.getElementById('np-name').addEventListener('input', (e) => { e.target.dataset.touched = '1'; });

document.getElementById('addpath').addEventListener('submit', (e) => {
  e.preventDefault();
  const path = document.getElementById('np-path').value.trim();
  const name = document.getElementById('np-name').value.trim();
  if (!path) return;
  fetch('/api/settings/paths', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, path })
  }).then(r => {
    if (!r.ok) { r.json().then(j => alert(j.message || '添加失败')).catch(() => alert('添加失败')); return; }
    location.reload();
  }).catch(() => alert('添加失败'));
});

document.getElementById('paths').addEventListener('click', (e) => {
  if (!e.target.classList.contains('del')) return;
  const li = e.target.closest('li');
  if (!confirm('删除该库路径？其下漫画记录会在重扫后移除。')) return;
  fetch(`/api/settings/paths/${li.dataset.id}`, { method: 'DELETE' })
    .then(r => { if (!r.ok) { alert('删除失败'); return; } location.reload(); })
    .catch(() => alert('删除失败'));
});

document.getElementById('scanform').addEventListener('submit', (e) => {
  e.preventDefault();
  const mode = document.querySelector('input[name="mode"]:checked').value;
  const payload = {
    scanMode: mode,
    scanInterval: document.getElementById('iv').value.trim(),
    scanDailyTime: document.getElementById('daily').value.trim()
  };
  fetch('/api/settings', {
    method: 'PUT', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  }).then(r => {
    if (!r.ok) { r.json().then(j => alert(j.message || '保存失败')).catch(() => alert('保存失败')); return; }
    alert('已保存，扫描设置已生效');
  }).catch(() => alert('保存失败'));
});
```

- [ ] **Step 5: Implement pageSettings + route**

In `internal/server/pages.go`, add:

```go
func (s *Server) pageSettings(c echo.Context) error {
	ctx := c.Request().Context()
	scan, err := s.settings.GetScan(ctx)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	paths, err := s.paths.List(ctx)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return render(c, "settings.html", map[string]any{
		"LibraryPaths": paths, "ScanMode": scan.Mode,
		"ScanInterval": scan.Interval, "ScanDailyTime": scan.DailyTime,
	})
}
```

(Ensure `net/http` is imported in pages.go — it is from v1.) In `internal/server/server.go` `Register`, add with the other page routes:

```go
	e.GET("/settings", s.pageSettings)
```

- [ ] **Step 6: Run tests + build**

Run: `go build ./... && go test ./internal/server/...` → PASS (incl. TestPageSettingsRenders).

- [ ] **Step 7: Commit**

```bash
git add internal/server/pages.go internal/server/server.go internal/server/server_test.go internal/server/web/templates/settings.html internal/server/web/static/js/settings.js
git commit -m "feat: add settings page"
```

---

## Task 9: Sidebar layout + full-screen reader with paging buttons

**Files:**
- Modify: `internal/server/web/templates/layout.html`
- Modify: `internal/server/web/templates/reader.html`
- Modify: `internal/server/web/static/css/app.css`
- Modify: `internal/server/web/static/js/reader.js`
- Test: `internal/server/server_test.go` (assert sidebar present on browse, absent on reader)

**Interfaces:**
- Consumes: existing `render` + the `{{block "sidebar" .}}` mechanism.
- Produces: a left sidebar (图书馆 / 标签管理 / 设置) on non-reader pages; the reader blanks the sidebar and shows visible prev/next buttons.

- [ ] **Step 1: Write the failing tests**

Append to `internal/server/server_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestSidebarOnBrowseNotReader -v` → FAIL.

- [ ] **Step 3: Rewrite layout.html with a sidebar block**

Replace `internal/server/web/templates/layout.html`:

```html
{{define "layout"}}<!doctype html>
<html lang="zh">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{block "title" .}}Tefnut{{end}}</title>
<link rel="stylesheet" href="/static/css/app.css">
</head>
<body>
{{block "sidebar" .}}
<aside class="sidebar">
  <div class="brand">Tefnut</div>
  <nav>
    <a href="/" data-path="/">图书馆</a>
    <a href="/tags" data-path="/tags">标签管理</a>
    <a href="/settings" data-path="/settings">设置</a>
  </nav>
</aside>
<script>
  (function () {
    var p = location.pathname;
    document.querySelectorAll('.sidebar nav a').forEach(function (a) {
      var d = a.getAttribute('data-path');
      if (d === '/' ? p === '/' || p.indexOf('/folder') === 0 : p.indexOf(d) === 0) a.classList.add('active');
    });
  })();
</script>
{{end}}
<main class="{{block "mainclass" .}}with-sidebar{{end}}">{{block "content" .}}{{end}}</main>
{{block "scripts" .}}{{end}}
</body>
</html>{{end}}
```

> Removes the old top bar. The sidebar is a `{{block}}` so the reader can blank it. The active-link script is inline (no per-page data field needed). `browse.html`, `tags.html`, `settings.html` are unaffected — they define `title`/`content`/`scripts` only, so they inherit the default sidebar block.

- [ ] **Step 4: Update reader.html — blank sidebar + visible paging buttons**

Edit `internal/server/web/templates/reader.html`: add a blank sidebar override and a full-screen main class at the top of the file, and add visible buttons. Replace the file with:

```html
{{define "title"}}{{.Name}} · 阅读{{end}}
{{define "sidebar"}}{{end}}
{{define "mainclass"}}reader-main{{end}}
{{define "content"}}
<div id="reader" data-id="{{.ID}}" data-pages="{{.PageCount}}" data-start="{{.LastPage}}">
  <div class="reader-stage">
    <button class="nav prev" id="prev" aria-label="上一页">‹</button>
    <img id="page" alt="page">
    <button class="nav next" id="next" aria-label="下一页">›</button>
  </div>
  <div class="reader-bar">
    <a class="back" href="/">← 返回</a>
    <button class="pagebtn" id="prevbtn">上一页</button>
    <span id="counter"></span>
    <button class="pagebtn" id="nextbtn">下一页</button>
    <div class="meta">
      <label>作者 <input id="author" value="{{.Author}}"></label>
      <label>评分 <select id="rating">{{range .Ratings}}<option value="{{.}}" {{if eq . $.Rating}}selected{{end}}>{{.}}★</option>{{end}}</select></label>
    </div>
    <div class="tags" id="tags"></div>
    <form id="addtag"><input id="newtag" placeholder="添加标签"><button>+</button></form>
  </div>
</div>
{{end}}
{{define "scripts"}}<script src="/static/js/reader.js"></script>{{end}}
```

- [ ] **Step 5: Bind the new buttons in reader.js**

In `internal/server/web/static/js/reader.js`, find the existing button wiring:

```js
document.getElementById('next').onclick = () => show(cur + 1);
document.getElementById('prev').onclick = () => show(cur - 1);
```

Replace it with binding both the arrow zones and the bar buttons:

```js
document.getElementById('next').onclick = () => show(cur + 1);
document.getElementById('prev').onclick = () => show(cur - 1);
document.getElementById('nextbtn').onclick = () => show(cur + 1);
document.getElementById('prevbtn').onclick = () => show(cur - 1);
```

- [ ] **Step 6: Update CSS — sidebar, visible arrows, settings, buttons**

In `internal/server/web/static/css/app.css`, remove the old `.topbar` rules and append:

```css
/* sidebar layout */
.sidebar { position: fixed; top: 0; left: 0; width: 180px; height: 100vh; background: #1c1f26; padding: 16px; box-sizing: border-box; }
.sidebar .brand { font-weight: 700; color: #fff; margin-bottom: 18px; font-size: 18px; }
.sidebar nav { display: flex; flex-direction: column; gap: 4px; }
.sidebar nav a { color: #c7c7c7; text-decoration: none; padding: 8px 10px; border-radius: 6px; }
.sidebar nav a:hover { background: #262b35; }
.sidebar nav a.active { background: #2f3645; color: #fff; }
main.with-sidebar { margin-left: 180px; padding: 20px; }
main.reader-main { margin-left: 0; padding: 0; }

/* visible reader paging buttons */
.nav { background: rgba(40,46,58,0.55); color: #fff; }
.nav:hover { background: rgba(60,70,90,0.8); }
.pagebtn { background: #2a2f3a; color: #eee; border: 1px solid #3a4150; border-radius: 6px; padding: 6px 14px; cursor: pointer; }
.pagebtn:hover { background: #343b48; }
.reader-bar .back { color: #8ab4f8; text-decoration: none; margin-right: 8px; }

/* settings page */
.card-box { background: #1c1f26; border-radius: 10px; padding: 16px; margin-bottom: 18px; }
.card-box h2 { margin-top: 0; font-size: 16px; }
#paths { list-style: none; padding: 0; }
#paths li { display: flex; gap: 10px; align-items: center; padding: 6px 0; }
#paths .lib-name { font-weight: 600; min-width: 90px; }
#paths .lib-path { color: #9aa3b2; flex: 1; word-break: break-all; }
#addpath.row, #scanform { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
#scanform label { color: #ddd; }
.card-box input, .card-box button { background: #222; color: #eee; border: 1px solid #333; border-radius: 6px; padding: 6px 10px; }
```

> The existing `.nav` rule sets `color: transparent` (invisible) in v1 — make sure to update/override it so the arrows are visible. Confirm the v1 `.nav { ... color: transparent; }` is replaced; the arrows must show their `‹`/`›` glyphs.

- [ ] **Step 7: Run tests + build**

Run: `go build ./... && go test ./internal/server/...` → PASS (incl. sidebar test).

- [ ] **Step 8: Commit**

```bash
git add internal/server/web/templates/layout.html internal/server/web/templates/reader.html internal/server/web/static/css/app.css internal/server/web/static/js/reader.js internal/server/server_test.go
git commit -m "feat: add sidebar nav and full-screen reader with visible paging buttons"
```

---

## Self-Review

**Spec coverage:**
- §4 schema (library_paths + settings) → Task 1. ✓
- §4 SettingsRepo / §4 LibraryPathRepo → Tasks 1, 2. ✓
- §6 multi-root scanner (top-level node per path, named by config, remove deconfigured, rename) → Task 3. ✓
- §5 yaml bootstrap + first-run seed → Task 3 (main seeding). ✓
- §7 ScanManager Start/Stop/Reconfigure + interval/daily → Task 4; watch (fsnotify + debounce, no fallback) → Task 5; wired in main → Task 6. ✓
- §8 settings API (GET/PUT/POST/DELETE) + validation + Reconfigure trigger → Task 7. ✓
- §9 sidebar (图书馆/标签管理/设置, remove top bar) → Task 9; settings page → Task 8; reader full-screen + visible paging buttons → Task 9. ✓
- §13 tests → each task. fsnotify timing gap noted in Task 5. ✓
- "save takes effect live" → Reconfigure called in every settings-mutating handler (Task 7), Reconfigure restarts mode + rescans (Task 4). ✓

**Placeholder scan:** Task 4 ships a temporary `startWatchLocked` stub explicitly removed in Task 5 (flagged). Task 3 `_ = settingsRepo` placeholder explicitly removed in Task 6 (flagged). No `TODO`/`TBD` in production code. The Task 3 note to update the three pre-existing single-root scanner tests is descriptive but actionable (navigate one level deeper through the library node); if an implementer prefers, those three tests may be deleted and replaced by the three new multi-root tests since they assert the same scanner internals — either way the deliverable is green multi-root scanner tests.

**Type consistency:** `NewScanner(repo, paths, dataDir, thumbWidth)` consistent across Task 3 producer and Task 4/6 consumers. `scan.New(sc, settings, paths)` and `Manager.Start/Stop/Reconfigure(ctx)` consistent across Tasks 4/5/6. `Reconfigurer` interface (`Reconfigure(ctx) error`) matches `*scan.Manager`'s method (Task 4) and the server's stub (Task 7). `store.ScanSettings{Mode,Interval,DailyTime}` identical in store, scan, and server. `LibraryPath{ID,Name,Path}` identical across store/scanner/server. `cronSpec` daily output `"<min> <hour> * * *"` matches the test (`"5 3 * * *"` for `03:05`). Settings JSON keys (`scanMode/scanInterval/scanDailyTime/libraryPaths`) consistent between `settingsDTO` (Task 7), settings.js + settings.html data (Task 8).

Fixes applied inline (watch stub + `_ = settingsRepo` flagged for removal; pre-existing scanner-test migration spelled out in Task 3).
