# Tefnut Optimization Pass Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Apply the approved fixes from the four-dimension code review (performance, maintainability, correctness, robustness) to Tefnut without changing its single-user, no-auth product shape.

**Architecture:** Harden the persistence layer (versioned migrations, single-source column list, transactional node patch, read/write connection split, pagination) and the serving/scan hot paths (cached open archive readers, HTTP cache headers, disk-persisted page thumbnails with a real LRU, a concurrency+pixel guard on decoding, a bounded extract-cache sweeper, and parallel cover generation), plus engineering hygiene (generic 5xx errors, security headers, gzip, gofmt, CI). Each task is independently testable and keeps `go build/vet/test` green.

**Tech Stack:** Go 1.24, echo v4, modernc.org/sqlite (pure Go, WAL), database/sql + hand-written SQL, golang.org/x/image (decode/resize), github.com/hashicorp/golang-lru/v2 (already a transitive dependency — promote to direct).

## Global Constraints

- **Single-user, NO authentication, NO multi-user / per-user state.** Do not add auth middleware, login, sessions, or per-user columns. (FTS5 search and targeted-watch incremental rescan are explicitly deferred — out of scope.)
- Go module path is `Tefnut`; Go 1.24; pure-Go sqlite (`modernc.org/sqlite`), no cgo.
- Errors wrapped with `%w` and operand context; **never silently swallow** an error — at minimum `log.Printf`. Validate at boundaries. User-facing 4xx messages stay specific; raw internal errors must not reach 5xx response bodies.
- Many small focused files (<800 lines), functions <50 lines, no magic numbers — new tunables become named constants (and config where it already belongs).
- Immutability where idiomatic (copy-before-mutate as `NodeRepo.Create` already does); pragmatic Go, not dogmatic.
- **Build-green gate:** every task ends with `go build ./... && go vet ./... && go test ./...` passing, and `gofmt -l` clean on touched files.
- TDD: write the failing test first, watch it fail, implement, watch it pass, commit.
- SQLite already runs WAL + `busy_timeout=5000` + `foreign_keys=ON` on the write connection. The read-only pool (Task 6) connects with `file:<path>?mode=ro`.
- Commit message format: `<type>: <description>` (types: feat, fix, refactor, perf, chore, ci, test). Frequent commits.

---

## File Structure (what each task touches)

- `internal/store/migrate.go` — **rewritten** to a versioned migration runner (Task 2). `schema` const moves here as migration v1.
- `internal/store/store.go` — `Open` calls the migration runner (Task 2); `DB` gains read/write handle split (Task 6).
- `internal/store/columns.go` — **new** single source of node column names + scan-target builder (Task 3).
- `internal/store/node_repo.go` — derive SQL from `columns.go` (Task 3); `UpdateFields` transactional patch (Task 4); pagination on `ListChildren`/`Search` (Task 5); read/write handle use (Task 6).
- `internal/store/{tag,progress,settings,library_path}_repo.go` — read/write handle use (Task 6).
- `internal/server/api_meta.go` — use `UpdateFields` (Task 4).
- `internal/server/api_nodes.go` — pagination params (Task 5); cached archive reader + DRY `openPage` (Task 7); cache headers (Task 8); disk page thumbs (Task 9); decode guard wiring (Task 10).
- `internal/server/pages.go` — pass pagination to browse (Task 5).
- `internal/archive/readercache.go` — **new** LRU of open `Reader`s with refcounting (Task 7).
- `internal/server/thumbcache.go` — **rewritten** to golang-lru + disk persistence helpers (Task 9).
- `internal/thumb/thumb.go` — pixel-budget guard (Task 10).
- `internal/server/server.go` — wire reader cache, decode semaphore, page-thumb dir (Tasks 7, 9, 10).
- `internal/cache/cache.go` — **new** bounded extract-cache sweeper (Task 11).
- `internal/scan/manager.go` — invoke sweeper after scans (Task 11).
- `internal/library/scanner.go` — log swallowed removals (Task 1); parallel cover build (Task 12).
- `internal/server/response.go` — generic 5xx body + server-side log (Task 13).
- `cmd/tefnut/main.go` — gzip + nosniff middleware (Task 13).
- `.github/workflows/ci.yml` — **new** (Task 13).
- `internal/config/config.go` — add `Cache.MaxBytes` tunable (Task 11) and `Thumbnail.PageWidth` (Task 9).

---

### Task 1: Log swallowed filesystem removals in the scanner

Fixes the correctness bug where a failed `os.RemoveAll` of a stale extract cache silently yields a wrong page count (a non-empty cacheDir makes `ensureExtracted` skip re-extraction). Also covers `removeNode`'s ignored removals.

**Files:**
- Modify: `internal/library/scanner.go:158-160` (`buildComic`), `:206-207` (`removeNode`)
- Test: `internal/library/scanner_test.go`

**Interfaces:**
- Consumes: existing `Scanner` methods. No signature changes.
- Produces: nothing new; behavior only (errors are now logged, not swallowed).

- [ ] **Step 1: Write the failing test**

Add to `internal/library/scanner_test.go`. The test asserts that after a comic's underlying archive is replaced with one that has a different page count, a rescan reports the NEW page count (i.e. the stale extract cache was evicted). Use a zip helper if present in the test file; reuse the existing archive-building helpers.

```go
func TestRescanReplacedArchiveUpdatesPageCount(t *testing.T) {
	dir := t.TempDir()
	dataDir := t.TempDir()
	sc, repo, paths := newScannerForTest(t, dataDir) // existing helper; adapt name to the file's helpers
	libDir := filepath.Join(dir, "lib")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := paths.Add(context.Background(), "lib", libDir); err != nil {
		t.Fatal(err)
	}
	comic := filepath.Join(libDir, "c.cbz")
	writeTestZip(t, comic, []string{"01.jpg", "02.jpg"}) // existing helper: builds a zip with N image entries
	if err := sc.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := pageCountOf(t, repo, comic) // helper: Search/ListChildren then read PageCount; adapt
	if got != 2 {
		t.Fatalf("page count after first scan = %d, want 2", got)
	}
	// Replace the archive with a 3-page one (changes size/mtime so buildComic re-runs).
	writeTestZip(t, comic, []string{"01.jpg", "02.jpg", "03.jpg"})
	if err := sc.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := pageCountOf(t, repo, comic); got != 3 {
		t.Fatalf("page count after replace = %d, want 3", got)
	}
}
```

If the test file lacks `newScannerForTest`/`writeTestZip`/`pageCountOf`, write minimal local helpers mirroring the existing test style in `scanner_test.go` (which already builds archives and constructs a `Scanner`). Keep them in the test file.

- [ ] **Step 2: Run test to verify it fails or passes**

Run: `go test ./internal/library/ -run TestRescanReplacedArchiveUpdatesPageCount -v`
Expected: PASS already if removal currently succeeds (it usually does) — this test mainly guards the behavior. The substantive change is making the removal failure observable. If it passes, proceed; the value is the logging change plus this regression guard.

- [ ] **Step 3: Make removals checked**

In `internal/library/scanner.go`, `buildComic` (line ~160):

```go
	// Reset any stale extract cache so page count reflects current file.
	if err := os.RemoveAll(s.cacheDir(node.ID)); err != nil {
		log.Printf("scanner: evict extract cache %d: %v", node.ID, err)
	}
```

In `removeNode` (lines ~206-207):

```go
	if err := os.Remove(s.thumbPath(n.ID)); err != nil && !os.IsNotExist(err) {
		log.Printf("scanner: remove thumb %d: %v", n.ID, err)
	}
	if err := os.RemoveAll(s.cacheDir(n.ID)); err != nil {
		log.Printf("scanner: remove extract cache %d: %v", n.ID, err)
	}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/library/... && go vet ./internal/library/...`
Expected: PASS, vet clean.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/library/scanner.go internal/library/scanner_test.go
git add internal/library/scanner.go internal/library/scanner_test.go
git commit -m "fix: log swallowed extract-cache/thumb removals in scanner"
```

---

### Task 2: Versioned schema migrations

Replaces the ad-hoc `schema` exec + `ensureColumn` calls with an ordered, version-tracked migration runner that baselines existing databases.

**Files:**
- Rewrite: `internal/store/migrate.go`
- Modify: `internal/store/store.go:69-93` (`Open`), remove the inline `ensureColumn` calls; move the `schema` const into `migrate.go` as migration v1.
- Test: `internal/store/migrate_test.go` (replace the `ensureColumn` tests)

**Interfaces:**
- Consumes: `*sql.DB` (the write connection).
- Produces: `func runMigrations(db *sql.DB) error`; a `schema_migrations(version INTEGER PRIMARY KEY)` table. `ensureColumn` is removed.

- [ ] **Step 1: Write the failing test**

Replace the contents of `internal/store/migrate_test.go`:

```go
package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func openRaw(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestRunMigrationsFreshCreatesSchemaAndRecordsVersion(t *testing.T) {
	db := openRaw(t)
	if err := runMigrations(db); err != nil {
		t.Fatal(err)
	}
	var v int
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != latestVersion() {
		t.Fatalf("version = %d, want %d", v, latestVersion())
	}
	// nodes table exists with the display_mode column.
	if _, err := db.Exec(`INSERT INTO nodes (parent_id,name,path,type,created_at,updated_at) VALUES (0,'n','/p',1,0,0)`); err != nil {
		t.Fatalf("insert into nodes: %v", err)
	}
	var mode string
	if err := db.QueryRow(`SELECT display_mode FROM nodes WHERE path='/p'`).Scan(&mode); err != nil {
		t.Fatalf("select display_mode: %v", err)
	}
	if mode != "single" {
		t.Fatalf("display_mode default = %q, want single", mode)
	}
}

func TestRunMigrationsIsIdempotent(t *testing.T) {
	db := openRaw(t)
	if err := runMigrations(db); err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(db); err != nil {
		t.Fatalf("second run: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != latestVersion() {
		t.Fatalf("migration rows = %d, want %d", n, latestVersion())
	}
}

func TestRunMigrationsBaselinesLegacyDB(t *testing.T) {
	db := openRaw(t)
	// Simulate a pre-migration database: tables already present, no schema_migrations.
	if _, err := db.Exec(schemaV1); err != nil {
		t.Fatal(err)
	}
	if err := runMigrations(db); err != nil {
		t.Fatal(err)
	}
	var v int
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != latestVersion() {
		t.Fatalf("legacy baseline version = %d, want %d", v, latestVersion())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestRunMigrations -v`
Expected: FAIL — `runMigrations`, `latestVersion`, `schemaV1` undefined.

- [ ] **Step 3: Rewrite `migrate.go`**

```go
package store

import (
	"database/sql"
	"fmt"
)

// schemaV1 is the full base schema. A fresh database is created entirely by
// this migration. Existing pre-migration databases (created by the old
// schema+ensureColumn path) are already at this schema and are baselined, not
// re-run. NEVER edit schemaV1 to add columns to an existing table — append a
// new migration with the next version instead.
const schemaV1 = `
CREATE TABLE IF NOT EXISTS nodes (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  parent_id    INTEGER NOT NULL DEFAULT 0,
  name         TEXT    NOT NULL,
  path         TEXT    NOT NULL UNIQUE,
  type         INTEGER NOT NULL,
  page_count   INTEGER NOT NULL DEFAULT 0,
  cover_status INTEGER NOT NULL DEFAULT 0,
  author       TEXT    NOT NULL DEFAULT '',
  rating       INTEGER NOT NULL DEFAULT 0,
  size              INTEGER NOT NULL DEFAULT 0,
  mtime             INTEGER NOT NULL DEFAULT 0,
  created_at        INTEGER NOT NULL,
  updated_at        INTEGER NOT NULL,
  reading_direction TEXT    NOT NULL DEFAULT 'ltr',
  display_mode TEXT NOT NULL DEFAULT 'single'
);
CREATE INDEX IF NOT EXISTS idx_nodes_parent ON nodes(parent_id);

CREATE TABLE IF NOT EXISTS tags (
  id   INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS node_tags (
  node_id INTEGER NOT NULL,
  tag_id  INTEGER NOT NULL,
  PRIMARY KEY (node_id, tag_id)
);
CREATE INDEX IF NOT EXISTS idx_node_tags_tag ON node_tags(tag_id);

CREATE TABLE IF NOT EXISTS progress (
  node_id    INTEGER PRIMARY KEY,
  last_page  INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL
);

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
`

type migration struct {
	version int
	sql     string
}

// migrations is the ordered migration list. Append new entries with the next
// version number; never edit or reorder existing entries.
var migrations = []migration{
	{1, schemaV1},
}

func latestVersion() int { return migrations[len(migrations)-1].version }

func tableExists(db *sql.DB, name string) (bool, error) {
	var n int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("store: check table %s: %w", name, err)
	}
	return n > 0, nil
}

// runMigrations brings the database schema up to latestVersion(). A database
// that already has application tables but no schema_migrations table is treated
// as a legacy database at the latest schema and is baselined (no DDL re-run).
func runMigrations(db *sql.DB) error {
	hasMig, err := tableExists(db, "schema_migrations")
	if err != nil {
		return err
	}
	if !hasMig {
		hasNodes, err := tableExists(db, "nodes")
		if err != nil {
			return err
		}
		if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
			return fmt.Errorf("store: create schema_migrations: %w", err)
		}
		if hasNodes {
			for _, m := range migrations {
				if _, err := db.Exec(`INSERT INTO schema_migrations(version) VALUES (?)`, m.version); err != nil {
					return fmt.Errorf("store: baseline legacy db at %d: %w", m.version, err)
				}
			}
			return nil
		}
	}
	var current int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("store: read schema version: %w", err)
	}
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("store: begin migration %d: %w", m.version, err)
		}
		if _, err := tx.Exec(m.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: migration %d: %w", m.version, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version) VALUES (?)`, m.version); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: record migration %d: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: commit migration %d: %w", m.version, err)
		}
	}
	return nil
}
```

- [ ] **Step 4: Update `store.go` `Open`**

In `internal/store/store.go`: delete the `schema` const (now `schemaV1` in `migrate.go`) and replace the body of `Open` after the pragma exec:

```go
	sqldb.SetMaxOpenConns(1)
	if _, err := sqldb.Exec(`PRAGMA busy_timeout = 5000; PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL;`); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("store: pragma: %w", err)
	}
	if err := runMigrations(sqldb); err != nil {
		sqldb.Close()
		return nil, err
	}
	return &DB{db: sqldb}, nil
```

Remove the two `ensureColumn(...)` blocks and the `schema` exec.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/store/... && go vet ./internal/store/...`
Expected: PASS (existing store tests still pass against the migrated schema), vet clean.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/store/migrate.go internal/store/store.go internal/store/migrate_test.go
git add internal/store/migrate.go internal/store/store.go internal/store/migrate_test.go
git commit -m "refactor: versioned schema migrations with legacy baseline"
```

---

### Task 3: Single-source node column list + startup assertion

Eliminates the three-place positional column sync. Column names live once; the `Search` SELECT and `scanNode` both derive from it, and a runtime assertion catches drift.

**Files:**
- Create: `internal/store/columns.go`
- Modify: `internal/store/node_repo.go:20-31` (`nodeCols`, `scanNode`), `:78-80` (`Search` SELECT list)
- Test: `internal/store/node_repo_test.go`

**Interfaces:**
- Consumes: `Node` struct fields.
- Produces: `nodeColNames []string`; `nodeCols` (joined, unprefixed); `nodeColsPrefixed(alias string) string`; `nodeScanTargets(n *Node) []any` (pointers in `nodeColNames` order); `assertNodeColumns()` (panics on mismatch, called from `init`).

- [ ] **Step 1: Write the failing test**

Add to `internal/store/node_repo_test.go`:

```go
func TestNodeScanTargetsMatchColumnNames(t *testing.T) {
	n := &Node{}
	if got := len(nodeScanTargets(n)); got != len(nodeColNames) {
		t.Fatalf("scan targets = %d, column names = %d", got, len(nodeColNames))
	}
}

func TestSearchAndGetReturnSameFields(t *testing.T) {
	st := newTestStore(t) // existing helper that returns repos/db; adapt to file's helpers
	repo := st.nodes
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
	results, err := repo.Search(ctx, "Alpha", 0, 0)
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
```

If `newTestStore` doesn't exist, mirror the existing store-test setup in `node_repo_test.go` (open a temp DB via `store.Open`, build `NewNodeRepo`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run 'TestNodeScanTargets|TestSearchAndGet' -v`
Expected: FAIL — `nodeColNames`, `nodeScanTargets` undefined.

- [ ] **Step 3: Create `columns.go`**

```go
package store

import "strings"

// nodeColNames is the single source of truth for nodes columns and their order.
// scanNode, the Get/ListChildren SELECT, and the Search SELECT all derive from
// this list. To add a column: append it here, add the matching pointer in
// nodeScanTargets in the SAME position, and add a migration (Task 2). The
// startup assertion (assertNodeColumns) guards the count.
var nodeColNames = []string{
	"id", "parent_id", "name", "path", "type", "page_count", "cover_status",
	"author", "rating", "size", "mtime", "created_at", "updated_at",
	"reading_direction", "display_mode",
}

// nodeCols is the unprefixed, comma-joined column list (for single-table SELECTs).
var nodeCols = strings.Join(nodeColNames, ", ")

// nodeColsPrefixed returns the column list with each name prefixed by alias+"."
// (for joined queries, e.g. Search's "n." alias).
func nodeColsPrefixed(alias string) string {
	parts := make([]string, len(nodeColNames))
	for i, c := range nodeColNames {
		parts[i] = alias + "." + c
	}
	return strings.Join(parts, ", ")
}

// nodeScanTargets returns scan destinations for n in nodeColNames order.
func nodeScanTargets(n *Node) []any {
	return []any{
		&n.ID, &n.ParentID, &n.Name, &n.Path, &n.Type, &n.PageCount, &n.CoverStatus,
		&n.Author, &n.Rating, &n.Size, &n.MTime, &n.CreatedAt, &n.UpdatedAt,
		&n.ReadingDirection, &n.DisplayMode,
	}
}

func init() { assertNodeColumns() }

// assertNodeColumns fails fast at startup if the column list and the scan-target
// list have drifted out of sync.
func assertNodeColumns() {
	if got := len(nodeScanTargets(&Node{})); got != len(nodeColNames) {
		panic("store: nodeScanTargets length " + itoa(got) + " != nodeColNames length " + itoa(len(nodeColNames)))
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}
```

(Note: `itoa` avoids importing `strconv` into this tiny file; if `node_repo.go` already imports `strconv` you may instead use `strconv.Itoa` and drop the helper. Keep it self-contained.)

- [ ] **Step 4: Update `node_repo.go`**

Delete the old `const nodeCols = ...` block (lines ~20-21) — it now lives in `columns.go`. Replace `scanNode`:

```go
func scanNode(s interface{ Scan(...any) error }) (*Node, error) {
	n := &Node{}
	if err := s.Scan(nodeScanTargets(n)...); err != nil {
		return nil, err
	}
	return n, nil
}
```

In `Search`, replace the hand-typed prefixed list (lines ~78-80):

```go
	sb.WriteString(`SELECT `)
	sb.WriteString(nodeColsPrefixed("n"))
	sb.WriteString(` FROM nodes n`)
```

`Get` and `ListChildren` already use `nodeCols` (now sourced from `columns.go`) — no change.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/store/... && go vet ./internal/store/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/store/columns.go internal/store/node_repo.go internal/store/node_repo_test.go
git add internal/store/columns.go internal/store/node_repo.go internal/store/node_repo_test.go
git commit -m "refactor: single-source node column list with drift assertion"
```

---

### Task 4: Transactional node-field patch

Replaces the three single-field updaters used by the meta handler (`UpdateMeta`, `UpdateReadingDirection`, `UpdateDisplayMode`) with one transactional `UpdateFields` that writes only the provided columns in a single statement, removing the documented non-atomic split write.

**Files:**
- Modify: `internal/store/node_repo.go` (add `NodePatch` + `UpdateFields`; remove `UpdateMeta`, `UpdateReadingDirection`, `UpdateDisplayMode`)
- Modify: `internal/server/api_meta.go:22-78` (`apiUpdateMeta`)
- Test: `internal/store/node_repo_test.go`, `internal/server/server_test.go`

**Interfaces:**
- Consumes: `NodeRepo`.
- Produces:
  ```go
  type NodePatch struct {
      Author           *string
      Rating           *int
      ReadingDirection *string
      DisplayMode      *string
  }
  func (r *NodeRepo) UpdateFields(ctx context.Context, id int64, p NodePatch) error
  ```
  Keep `UpdateName` and `UpdateFileAttrs` (used by the scanner) unchanged.

- [ ] **Step 1: Write the failing test**

Add to `internal/store/node_repo_test.go`:

```go
func TestUpdateFieldsPartialAndAtomic(t *testing.T) {
	st := newTestStore(t)
	repo := st.nodes
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestUpdateFieldsPartial -v`
Expected: FAIL — `NodePatch`, `UpdateFields` undefined.

- [ ] **Step 3: Implement `UpdateFields`; remove the three updaters**

In `internal/store/node_repo.go`, delete `UpdateMeta`, `UpdateReadingDirection`, `UpdateDisplayMode`. Add:

```go
type NodePatch struct {
	Author           *string
	Rating           *int
	ReadingDirection *string
	DisplayMode      *string
}

// UpdateFields applies only the set fields of p to node id in one statement.
// An empty patch is a no-op.
func (r *NodeRepo) UpdateFields(ctx context.Context, id int64, p NodePatch) error {
	sets := []string{}
	args := []any{}
	if p.Author != nil {
		sets = append(sets, "author=?")
		args = append(args, *p.Author)
	}
	if p.Rating != nil {
		sets = append(sets, "rating=?")
		args = append(args, *p.Rating)
	}
	if p.ReadingDirection != nil {
		sets = append(sets, "reading_direction=?")
		args = append(args, *p.ReadingDirection)
	}
	if p.DisplayMode != nil {
		sets = append(sets, "display_mode=?")
		args = append(args, *p.DisplayMode)
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at=?")
	args = append(args, time.Now().Unix(), id)
	q := `UPDATE nodes SET ` + strings.Join(sets, ", ") + ` WHERE id=?`
	if _, err := r.db.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("store: update fields %d: %w", id, err)
	}
	return nil
}
```

(`strings` is already imported by `node_repo.go`.)

- [ ] **Step 4: Update `apiUpdateMeta`**

Rewrite `internal/server/api_meta.go` `apiUpdateMeta` to validate then call `UpdateFields` once. Keep the existing validations (rating 0..5, direction ltr/rtl, displayMode enum). Replace the body from the `Get` onward:

```go
	if body.DisplayMode != nil {
		m := *body.DisplayMode
		if m != "single" && m != "continuous" && m != "spread" {
			return fail(c, http.StatusBadRequest, errors.New("displayMode must be single, continuous, or spread"))
		}
	}
	n, err := s.nodes.Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return fail(c, http.StatusNotFound, err)
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	patch := store.NodePatch{
		Author:           body.Author,
		Rating:           body.Rating,
		ReadingDirection: body.ReadingDirection,
		DisplayMode:      body.DisplayMode,
	}
	if err := s.nodes.UpdateFields(ctx, id, patch); err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	author, rating := n.Author, n.Rating
	if body.Author != nil {
		author = *body.Author
	}
	if body.Rating != nil {
		rating = *body.Rating
	}
	return ok(c, map[string]any{"author": author, "rating": rating})
```

Move the `displayMode` validation up to sit beside the other validations (before `Get`), as shown. Delete the now-stale split-write rationale comment.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/store/... ./internal/server/... && go vet ./...`
Expected: PASS. (The server PATCH tests exercising author/rating/direction/displayMode independence still pass against `UpdateFields`.)

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/store/node_repo.go internal/server/api_meta.go
git add internal/store/node_repo.go internal/server/api_meta.go internal/store/node_repo_test.go internal/server/server_test.go
git commit -m "refactor: transactional UpdateFields node patch replaces split writes"
```

---

### Task 5: Pagination on ListChildren and Search

Adds a default-capped `limit`/`offset` to the list and search queries to bound slice and HTML size on large folders. The browse UI keeps working with a sane default cap.

**Files:**
- Modify: `internal/store/node_repo.go` (`ListChildren`, `Search` signatures gain `limit, offset int`)
- Modify: `internal/server/api_nodes.go` (`apiNodes` reads `limit`/`offset` query params), `internal/server/pages.go` (browse passes defaults)
- Test: `internal/store/node_repo_test.go`

**Interfaces:**
- Consumes: callers of `ListChildren`/`Search` (scanner uses `ListChildren`).
- Produces:
  ```go
  func (r *NodeRepo) ListChildren(ctx context.Context, parentID int64, limit, offset int) ([]*Node, error)
  func (r *NodeRepo) Search(ctx context.Context, q string, tagID int64, minRating, limit, offset int) ([]*Node, error)
  ```
  Convention: `limit <= 0` means "use the default cap" (`defaultPageLimit = 500`); the scanner passes `0, 0` to mean "default cap, from start" — acceptable because a single folder is not expected to exceed 500 entries; if it might, the scanner should page. To stay correct for the scanner's full-subtree diff, add a sentinel: `limit < 0` means "no limit". The scanner passes `-1, 0`.

- [ ] **Step 1: Write the failing test**

Add to `internal/store/node_repo_test.go`:

```go
func TestListChildrenPagination(t *testing.T) {
	st := newTestStore(t)
	repo := st.nodes
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestListChildrenPagination -v`
Expected: FAIL — signature mismatch (too few args) / compile error.

- [ ] **Step 3: Add limit/offset to the queries**

In `internal/store/node_repo.go`, add a constant and a helper, and thread `limit, offset` through both queries:

```go
const defaultPageLimit = 500

// limitClause appends a LIMIT/OFFSET to sb and args. limit<0 means no limit;
// limit==0 means the default cap.
func appendLimit(sb *strings.Builder, args *[]any, limit, offset int) {
	if limit < 0 {
		return
	}
	if limit == 0 {
		limit = defaultPageLimit
	}
	sb.WriteString(` LIMIT ? OFFSET ?`)
	*args = append(*args, limit, offset)
}
```

`ListChildren`:

```go
func (r *NodeRepo) ListChildren(ctx context.Context, parentID int64, limit, offset int) ([]*Node, error) {
	var sb strings.Builder
	args := []any{parentID}
	sb.WriteString(`SELECT ` + nodeCols + ` FROM nodes WHERE parent_id = ? ORDER BY type DESC, name ASC`)
	appendLimit(&sb, &args, limit, offset)
	rows, err := r.db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("store: list children %d: %w", parentID, err)
	}
	return collectNodes(rows)
}
```

`Search`: before the final `QueryContext`, after the `ORDER BY n.name ASC`, add `appendLimit(&sb, &args, limit, offset)` and add the two params to the signature.

- [ ] **Step 4: Update all callers**

- `internal/library/scanner.go`: both `s.repo.ListChildren(ctx, 0)` and `s.repo.ListChildren(ctx, parentID)` and the deletion-walk `ListChildren(ctx, n.ID)` → pass `-1, 0` (no limit — the scanner needs the full set for its diff).
- `internal/server/api_nodes.go` `apiNodes`: read optional `limit`/`offset`:
  ```go
  limit, _ := strconv.Atoi(c.QueryParam("limit"))   // 0 => default cap
  offset, _ := strconv.Atoi(c.QueryParam("offset"))
  if offset < 0 { offset = 0 }
  ...
  nodes, err = s.nodes.Search(ctx, q, tagID, minRating, limit, offset)
  ...
  nodes, err = s.nodes.ListChildren(ctx, parent, limit, offset)
  ```
- `internal/server/pages.go` `pageBrowse`: pass `0, 0` (default cap) to whichever of `ListChildren`/`Search` it calls. Find the calls and add the two args.

Grep to be exhaustive: `grep -rn "ListChildren(\|\.Search(" internal/` and update every call site.

- [ ] **Step 5: Run tests**

Run: `go test ./... && go vet ./...`
Expected: PASS across all packages.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/store/node_repo.go internal/server/api_nodes.go internal/server/pages.go internal/library/scanner.go internal/store/node_repo_test.go
git add -A
git commit -m "feat: paginate ListChildren/Search with a default cap"
```

---

### Task 6: Read/write SQLite connection split

Opens a separate read-only connection pool so background scans and deletes (on the single write connection) no longer serialize ahead of reads. Exploits WAL's concurrent-reader capability.

**Files:**
- Modify: `internal/store/store.go` (`DB` struct + `Open`)
- Modify: `internal/store/{node,tag,progress,settings,library_path}_repo.go` (hold read+write handles)
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: `runMigrations` (Task 2).
- Produces:
  ```go
  func (d *DB) Write() *sql.DB
  func (d *DB) Read() *sql.DB
  ```
  Repos store `rdb` (read) and `wdb` (write). `SQL()` is removed. Constructors unchanged in signature (`NewNodeRepo(db *DB)` etc.).

- [ ] **Step 1: Write the failing test**

Add to `internal/store/store_test.go`:

```go
func TestReadWriteSplit(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "rw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if db.Read() == nil || db.Write() == nil {
		t.Fatal("read/write handles must be non-nil")
	}
	if db.Read() == db.Write() {
		t.Fatal("read and write handles must be distinct")
	}
	// A write is visible through the read handle.
	if _, err := db.Write().Exec(`INSERT INTO settings(key,value) VALUES('k','v')`); err != nil {
		t.Fatal(err)
	}
	var v string
	if err := db.Read().QueryRow(`SELECT value FROM settings WHERE key='k'`).Scan(&v); err != nil {
		t.Fatalf("read after write: %v", err)
	}
	if v != "v" {
		t.Fatalf("read value = %q, want v", v)
	}
	// The read handle is read-only.
	if _, err := db.Read().Exec(`INSERT INTO settings(key,value) VALUES('x','y')`); err == nil {
		t.Fatal("expected write through read handle to fail (mode=ro)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestReadWriteSplit -v`
Expected: FAIL — `Read`/`Write` undefined.

- [ ] **Step 3: Rewrite `DB`/`Open`**

```go
const maxReadConns = 4

type DB struct {
	write *sql.DB
	read  *sql.DB
}

func (d *DB) Write() *sql.DB { return d.write }
func (d *DB) Read() *sql.DB  { return d.read }

func (d *DB) Close() error {
	rerr := d.read.Close()
	werr := d.write.Close()
	if werr != nil {
		return werr
	}
	return rerr
}

func Open(dsn string) (*DB, error) {
	write, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", dsn, err)
	}
	write.SetMaxOpenConns(1)
	if _, err := write.Exec(`PRAGMA busy_timeout = 5000; PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL;`); err != nil {
		write.Close()
		return nil, fmt.Errorf("store: pragma: %w", err)
	}
	if err := runMigrations(write); err != nil {
		write.Close()
		return nil, err
	}
	read, err := sql.Open("sqlite", "file:"+dsn+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		write.Close()
		return nil, fmt.Errorf("store: open read %s: %w", dsn, err)
	}
	read.SetMaxOpenConns(maxReadConns)
	if err := read.Ping(); err != nil {
		read.Close()
		write.Close()
		return nil, fmt.Errorf("store: ping read %s: %w", dsn, err)
	}
	return &DB{write: write, read: read}, nil
}
```

Remove the old `SQL()` method.

(Note on DSN: `dsn` is a plain filesystem path like `./data/tefnut.db`. The `file:` URI form `file:./data/tefnut.db?mode=ro&_pragma=...` is accepted by modernc.org/sqlite. If the implementer finds the relative `file:` form rejected on this platform, resolve `dsn` to an absolute path via `filepath.Abs` before composing the read DSN, and use `"file:" + abs + "?mode=ro&_pragma=busy_timeout(5000)"`. Verify the test passes either way.)

- [ ] **Step 4: Update every repo**

For each of `node_repo.go`, `tag_repo.go`, `progress_repo.go`, `settings_repo.go`, `library_path_repo.go`:
- Change the struct from `db *sql.DB` to `rdb, wdb *sql.DB`.
- Change the constructor: e.g. `func NewNodeRepo(db *DB) *NodeRepo { return &NodeRepo{rdb: db.Read(), wdb: db.Write()} }`.
- In each method, use `r.rdb` for pure reads (`QueryContext`, `QueryRowContext` in `Get`/`List`/`Search`/`ListForNode`/`Count`/getters) and `r.wdb` for writes (`ExecContext`, `BeginTx` in `Create`/`Update*`/`Delete`/`Add*`/`Remove*`/`Upsert`/`Set`).

For `node_repo.go` specifically, the read methods are `Get`, `ListChildren`, `Search`; write methods are `Create`, `UpdateName`, `UpdateFileAttrs`, `UpdateFields`, `Delete`. The `Delete` transaction uses `r.wdb.BeginTx`.

Classify the other repos by reading each method: any method whose SQL begins with `SELECT` uses `rdb`; `INSERT`/`UPDATE`/`DELETE`/transactions use `wdb`. Apply mechanically; grep `r.db` in each file and replace each occurrence with the correct handle.

- [ ] **Step 5: Run tests (with race)**

Run: `go test ./... && go test -race ./internal/store/... ./internal/server/... && go vet ./...`
Expected: PASS; race clean.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/store/*.go
git add internal/store/
git commit -m "perf: separate read-only SQLite pool from the single write connection"
```

---

### Task 7: Cached open archive readers (LRU + refcount)

The dominant runtime cost: every page and every thumbnail request re-opens the archive and re-parses the central directory + re-sorts. Cache open `Reader`s keyed by node id + mtime, with refcounting so an in-use reader is never closed mid-stream. Also DRY the duplicated open sequence in `apiPage`/`apiPageThumb`.

**Files:**
- Create: `internal/archive/readercache.go`
- Modify: `internal/server/server.go` (hold a `*archive.ReaderCache`), `internal/server/api_nodes.go` (use it via a new `openPage` helper)
- Test: `internal/archive/readercache_test.go`

**Interfaces:**
- Consumes: `archive.Open(ctx, path, cacheDir) (Reader, error)`, `Reader.Close()`.
- Produces:
  ```go
  type ReaderCache struct { /* ... */ }
  func NewReaderCache(max int) *ReaderCache
  // Acquire returns a shared Reader for (key,path,mtime) and a release func that
  // MUST be called when the caller is done reading (after streaming completes).
  // Entries whose mtime differs from a prior cached value are reopened.
  func (c *ReaderCache) Acquire(ctx context.Context, key, path string, mtime int64, cacheDir string) (Reader, func(), error)
  func (c *ReaderCache) Close()
  ```

- [ ] **Step 1: Write the failing test**

Create `internal/archive/readercache_test.go`:

```go
package archive

import (
	"context"
	"io"
	"path/filepath"
	"testing"
)

func TestReaderCacheReusesAndRefcounts(t *testing.T) {
	dir := t.TempDir()
	zpath := filepath.Join(dir, "a.zip")
	writeImagesZip(t, zpath, 3) // local helper mirroring archive_test.go zip building

	c := NewReaderCache(2)
	defer c.Close()
	ctx := context.Background()

	r1, rel1, err := c.Acquire(ctx, "1", zpath, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(r1.List()) != 3 {
		t.Fatalf("list = %d, want 3", len(r1.List()))
	}
	// Second acquire of the same key+mtime returns the SAME underlying reader.
	r2, rel2, err := c.Acquire(ctx, "1", zpath, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	if r1 != r2 {
		t.Fatal("expected cache hit to reuse the reader")
	}
	// Read a page fully while both refs are held.
	rc, err := r2.Open(r2.List()[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(rc); err != nil {
		t.Fatal(err)
	}
	rc.Close()
	rel1()
	rel2()

	// A changed mtime forces a reopen (new reader instance).
	r3, rel3, err := c.Acquire(ctx, "1", zpath, 200, "")
	if err != nil {
		t.Fatal(err)
	}
	if r3 == r1 {
		t.Fatal("expected reopen on mtime change")
	}
	rel3()
}
```

Add a `writeImagesZip(t, path, n)` helper if the package's test files don't already expose one (mirror the zip-building already in `archive_test.go`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/archive/ -run TestReaderCache -v`
Expected: FAIL — `NewReaderCache` undefined.

- [ ] **Step 3: Implement the cache**

```go
package archive

import (
	"container/list"
	"context"
	"sync"
)

type cacheEntry struct {
	key     string
	mtime   int64
	reader  Reader
	refs    int
	evicted bool
	elem    *list.Element
}

// ReaderCache keeps up to max open archive Readers, keyed by a caller-supplied
// key (e.g. node id). Readers are refcounted: an entry chosen for eviction is
// closed only once its last in-flight reader is released, so a reader is never
// closed mid-stream.
type ReaderCache struct {
	mu      sync.Mutex
	max     int
	entries map[string]*cacheEntry
	lru     *list.List // front = most recently used
}

func NewReaderCache(max int) *ReaderCache {
	if max < 1 {
		max = 1
	}
	return &ReaderCache{max: max, entries: make(map[string]*cacheEntry), lru: list.New()}
}

func (c *ReaderCache) Acquire(ctx context.Context, key, path string, mtime int64, cacheDir string) (Reader, func(), error) {
	c.mu.Lock()
	if e, ok := c.entries[key]; ok && e.mtime == mtime && !e.evicted {
		e.refs++
		c.lru.MoveToFront(e.elem)
		c.mu.Unlock()
		return e.reader, c.releaser(e), nil
	}
	// Stale (mtime changed) or missing: drop the old entry from the map and open fresh.
	if e, ok := c.entries[key]; ok {
		c.dropLocked(e)
	}
	c.mu.Unlock()

	r, err := Open(ctx, path, cacheDir)
	if err != nil {
		return nil, nil, err
	}

	c.mu.Lock()
	// Another goroutine may have inserted concurrently; if so, use ours but it
	// will simply be an extra short-lived reader — acceptable. Insert ours.
	e := &cacheEntry{key: key, mtime: mtime, reader: r, refs: 1}
	e.elem = c.lru.PushFront(e)
	c.entries[key] = e
	c.evictLocked()
	c.mu.Unlock()
	return r, c.releaser(e), nil
}

func (c *ReaderCache) releaser(e *cacheEntry) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			e.refs--
			if e.evicted && e.refs == 0 {
				c.mu.Unlock()
				e.reader.Close()
				return
			}
			c.mu.Unlock()
		})
	}
}

// dropLocked removes e from the map/lru and marks it evicted; it is closed now
// if idle, else closed by the last releaser.
func (c *ReaderCache) dropLocked(e *cacheEntry) {
	delete(c.entries, e.key)
	c.lru.Remove(e.elem)
	e.evicted = true
	if e.refs == 0 {
		// Close outside the lock to avoid holding it during IO.
		go e.reader.Close()
	}
}

func (c *ReaderCache) evictLocked() {
	for c.lru.Len() > c.max {
		back := c.lru.Back()
		if back == nil {
			return
		}
		e := back.Value.(*cacheEntry)
		c.dropLocked(e)
	}
}

func (c *ReaderCache) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.entries {
		e.evicted = true
		if e.refs == 0 {
			e.reader.Close()
		}
	}
	c.entries = make(map[string]*cacheEntry)
	c.lru.Init()
}
```

(The `go e.reader.Close()` in `dropLocked` is a deliberate fire-and-forget close of an idle, already-removed reader; it cannot race a live read because `refs==0`.)

- [ ] **Step 4: Wire into the server + DRY `openPage`**

In `internal/server/server.go`: add `readers *archive.ReaderCache` to `Server`, construct with `archive.NewReaderCache(archiveCacheSize)` (add `const archiveCacheSize = 8` in server.go), and import `Tefnut/internal/archive` if not already.

In `internal/server/api_nodes.go`, add a helper and use it in both handlers:

```go
// openPage acquires a (cached) archive reader for node n, returns the entry
// reader for pageNum, its name, and a release func that frees the archive
// reader. Callers MUST defer release() AND close the returned entry reader.
func (s *Server) openPage(ctx context.Context, n *store.Node, pageNum int) (io.ReadCloser, string, func(), error) {
	cacheDir := filepath.Join(s.dataDir, "cache", strconv.FormatInt(n.ID, 10))
	key := strconv.FormatInt(n.ID, 10)
	r, release, err := s.readers.Acquire(ctx, key, n.Path, n.MTime, cacheDir)
	if err != nil {
		return nil, "", nil, err
	}
	names := r.List()
	if pageNum < 0 || pageNum >= len(names) {
		release()
		return nil, "", nil, errPageRange
	}
	rc, err := r.Open(names[pageNum])
	if err != nil {
		release()
		return nil, "", nil, err
	}
	return rc, names[pageNum], release, nil
}

var errPageRange = errors.New("page out of range")
```

Rewrite `apiPage`'s archive section to use it (note: release is deferred AFTER the stream, so the archive stays open across `c.Stream`):

```go
	rc, name, release, err := s.openPage(ctx, n, pageNum)
	if err != nil {
		if errors.Is(err, errPageRange) {
			return fail(c, http.StatusNotFound, err)
		}
		return fail(c, http.StatusInternalServerError, err)
	}
	defer release()
	defer rc.Close()
	return c.Stream(http.StatusOK, contentType(name), rc)
```

Rewrite `apiPageThumb`'s archive section likewise (read the entry, generate, then the deferred `release()`/`rc.Close()` run after). Remove the long per-thumb-open rationale comment (no longer true). Keep the existing in-memory thumb cache check for now (Task 9 replaces it).

Add `"io"` to the `api_nodes.go` imports.

- [ ] **Step 5: Run tests (with race)**

Run: `go test ./... && go test -race ./internal/archive/... ./internal/server/... && go vet ./...`
Expected: PASS; race clean.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/archive/readercache.go internal/server/server.go internal/server/api_nodes.go internal/archive/readercache_test.go
git add internal/archive/readercache.go internal/archive/readercache_test.go internal/server/server.go internal/server/api_nodes.go
git commit -m "perf: cache open archive readers (LRU+refcount), DRY page open"
```

---

### Task 8: HTTP cache headers on pages and covers

Comic pages and covers are immutable for a given archive/cover file. Add strong validators so re-reads are served from the browser cache and skip the server + archive entirely.

**Files:**
- Modify: `internal/server/api_nodes.go` (`apiPage`, `apiCover`)
- Test: `internal/server/server_test.go`

**Interfaces:** no new exported symbols. Headers/ETag only.

- [ ] **Step 1: Write the failing test**

Add to `internal/server/server_test.go` (use the existing `newTestServer`/`seedComic` helpers and `httptest`):

```go
func TestPageSetsImmutableCacheHeadersAndETag(t *testing.T) {
	ts := newTestServer(t)
	id := ts.seedComic(t, 3) // existing helper that creates a comic with N pages; adapt name
	rec := ts.get(t, "/api/comics/"+itoa64(id)+"/pages/0")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc == "" {
		t.Fatal("missing Cache-Control")
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}
	// Conditional request returns 304.
	rec2 := ts.getWithHeader(t, "/api/comics/"+itoa64(id)+"/pages/0", "If-None-Match", etag)
	if rec2.Code != 304 {
		t.Fatalf("conditional status = %d, want 304", rec2.Code)
	}
}
```

Adapt helper names to whatever `server_test.go` already provides (`newTestServer`, request helpers). Add a small `itoa64` helper if absent.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestPageSetsImmutable -v`
Expected: FAIL — no ETag / not 304.

- [ ] **Step 3: Add validators**

In `apiPage`, after fetching `n` and validating `pageNum`, set headers and short-circuit on `If-None-Match` BEFORE opening the archive:

```go
	etag := fmt.Sprintf(`"%d-%d-%d"`, n.ID, n.MTime, pageNum)
	c.Response().Header().Set("ETag", etag)
	c.Response().Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	if match := c.Request().Header.Get("If-None-Match"); match == etag {
		return c.NoContent(http.StatusNotModified)
	}
```

(Use `private` rather than `public` — single user, no shared caches; harmless either way.)

For `apiCover`, key the ETag on the cover file's mod time:

```go
	p := filepath.Join(s.dataDir, "thumbs", strconv.FormatInt(id, 10)+".jpg")
	info, statErr := filepathStat(p)
	if statErr != nil {
		return c.Redirect(http.StatusFound, "/static/img/placeholder.svg")
	}
	etag := fmt.Sprintf(`"cover-%d-%d"`, id, info.ModTime().Unix())
	c.Response().Header().Set("ETag", etag)
	c.Response().Header().Set("Cache-Control", "private, max-age=86400")
	if match := c.Request().Header.Get("If-None-Match"); match == etag {
		return c.NoContent(http.StatusNotModified)
	}
	return c.File(p)
```

Confirm `filepathStat` returns an `os.FileInfo` (it wraps `os.Stat`); if it currently returns only an error, widen it to `(os.FileInfo, error)` in `fsutil.go` and update callers.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/server/... && go vet ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/server/api_nodes.go internal/server/fsutil.go internal/server/server_test.go
git add internal/server/api_nodes.go internal/server/fsutil.go internal/server/server_test.go
git commit -m "perf: immutable Cache-Control + ETag/304 on pages and covers"
```

---

### Task 9: Persist page thumbnails to disk + real LRU hot cache

Page thumbnails are regenerated on every restart and thrash the flush-everything in-memory cache. Persist them to disk (survives restart, served via `c.File`) and replace the flush-all map with a bounded LRU hot layer using `hashicorp/golang-lru/v2` (already a transitive dependency).

**Files:**
- Rewrite: `internal/server/thumbcache.go`
- Modify: `internal/server/api_nodes.go` (`apiPageThumb`), `internal/server/server.go` (cache construction), `internal/config/config.go` (add `Thumbnail.PageWidth`), `go.mod`/`go.sum` (promote golang-lru/v2 to direct)
- Test: `internal/server/thumbcache_test.go`

**Interfaces:**
- Produces:
  ```go
  type thumbCache struct { /* lru + dir */ }
  func newThumbCache(maxEntries int, dir string) (*thumbCache, error)
  func (c *thumbCache) get(key string) ([]byte, bool)   // mem then disk
  func (c *thumbCache) put(key string, b []byte) error  // mem + disk
  func (c *thumbCache) path(key string) string
  ```
  Page-thumb disk layout: `<dataDir>/thumbs/pages/<id>/<n>.jpg`. The cache key remains `"<id>:<n>"`; `path` maps it to that file.

- [ ] **Step 1: Promote the dependency**

```bash
go get github.com/hashicorp/golang-lru/v2@v2.0.7
```

(It is already in `go.sum` as indirect; this makes it direct.)

- [ ] **Step 2: Write the failing test**

Replace `internal/server/thumbcache_test.go`:

```go
package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestThumbCacheMemAndDisk(t *testing.T) {
	dir := t.TempDir()
	c, err := newThumbCache(2, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.put("1:0", []byte("aaa")); err != nil {
		t.Fatal(err)
	}
	// Hot read.
	if b, ok := c.get("1:0"); !ok || string(b) != "aaa" {
		t.Fatalf("mem get = %q,%v", b, ok)
	}
	// Persisted to disk at the expected path.
	if _, err := os.Stat(filepath.Join(dir, "pages", "1", "0.jpg")); err != nil {
		t.Fatalf("expected disk file: %v", err)
	}
	// A fresh cache (cold memory) still finds it on disk.
	c2, err := newThumbCache(2, dir)
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := c2.get("1:0"); !ok || string(b) != "aaa" {
		t.Fatalf("disk get = %q,%v", b, ok)
	}
}

func TestThumbCacheLRUEvictsMemNotDisk(t *testing.T) {
	dir := t.TempDir()
	c, _ := newThumbCache(1, dir)
	_ = c.put("1:0", []byte("a"))
	_ = c.put("1:1", []byte("b")) // evicts 1:0 from memory
	// 1:0 gone from memory but still served from disk.
	if b, ok := c.get("1:0"); !ok || string(b) != "a" {
		t.Fatalf("evicted-but-on-disk get = %q,%v", b, ok)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestThumbCache -v`
Expected: FAIL — `newThumbCache` signature changed / undefined.

- [ ] **Step 4: Rewrite `thumbcache.go`**

```go
package server

import (
	"os"
	"path/filepath"
	"strings"

	lru "github.com/hashicorp/golang-lru/v2"
)

// thumbCache is a two-layer page-thumbnail cache: a bounded in-memory LRU in
// front of durable per-page JPEG files under <dir>/pages/<id>/<n>.jpg.
type thumbCache struct {
	mem *lru.Cache[string, []byte]
	dir string
}

func newThumbCache(maxEntries int, dir string) (*thumbCache, error) {
	mem, err := lru.New[string, []byte](maxEntries)
	if err != nil {
		return nil, err
	}
	return &thumbCache{mem: mem, dir: dir}, nil
}

// path maps "<id>:<n>" to <dir>/pages/<id>/<n>.jpg.
func (c *thumbCache) path(key string) string {
	id, n, ok := strings.Cut(key, ":")
	if !ok {
		id, n = "0", key
	}
	return filepath.Join(c.dir, "pages", id, n+".jpg")
}

func (c *thumbCache) get(key string) ([]byte, bool) {
	if b, ok := c.mem.Get(key); ok {
		return b, true
	}
	b, err := os.ReadFile(c.path(key))
	if err != nil {
		return nil, false
	}
	c.mem.Add(key, b)
	return b, true
}

func (c *thumbCache) put(key string, b []byte) error {
	c.mem.Add(key, b)
	p := c.path(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}
```

(The `size()` method is removed; if any test referenced it, drop that reference.)

- [ ] **Step 5: Update server construction + config + handler**

`internal/config/config.go`: add to `Thumbnail`:
```go
type Thumbnail struct {
	Width     int `yaml:"width"`
	PageWidth int `yaml:"pageWidth"`
}
```
In `defaults()`: `Thumbnail: Thumbnail{Width: 400, PageWidth: 120}`. In `validate()`, after the width check: `if c.Thumbnail.PageWidth <= 0 { c.Thumbnail.PageWidth = 120 }` (default-fill rather than error, to stay backward compatible with existing config files).

`internal/server/server.go`: change `newThumbCache(256)` to construct with the dir and handle the error. Since `NewServer` doesn't return an error today, build the cache in `NewServer` and on error fall back to a tiny mem-only dir under dataDir — simplest is to make the dir `filepath.Join(dataDir, "thumbs")` and treat a construction error as fatal-at-wire-time. Cleanest: keep `NewServer` non-error by constructing the LRU with a known-good size:
```go
const thumbCacheMaxEntries = 512
...
tc, _ := newThumbCache(thumbCacheMaxEntries, filepath.Join(dataDir, "thumbs"))
return &Server{..., thumbs: tc, ...}
```
`lru.New` only errors on size<=0, and `thumbCacheMaxEntries` is a positive constant, so the ignored error is safe; add a short comment saying so. Thread `s.thumbPageWidth = thumbWidthOrDefault` — actually reuse the page width from config: pass it into `NewServer`. Update `NewServer` signature to accept `pageThumbWidth int` and `main.go` to pass `cfg.Thumbnail.PageWidth`. Store it on `Server` as `pageThumbWidth`.

`internal/server/api_nodes.go` `apiPageThumb`: replace the literal `120` with `s.pageThumbWidth`, serve disk hits via the cache, and set caching headers:
```go
	key := strconv.FormatInt(id, 10) + ":" + strconv.Itoa(pageNum)
	if b, ok := s.thumbs.get(key); ok {
		c.Response().Header().Set("Cache-Control", "public, max-age=86400")
		return c.Blob(http.StatusOK, "image/jpeg", b)
	}
	... // fetch node, openPage (Task 7), generate
	data, err := thumb.Generate(rc, s.pageThumbWidth)
	...
	if err := s.thumbs.put(key, data); err != nil {
		log.Printf("server: persist thumb %s: %v", key, err)
	}
	c.Response().Header().Set("Cache-Control", "public, max-age=86400")
	return c.Blob(http.StatusOK, "image/jpeg", data)
```
Add `"log"` to imports if needed.

- [ ] **Step 6: Run tests**

Run: `go test ./... && go vet ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/server/thumbcache.go internal/server/api_nodes.go internal/server/server.go internal/config/config.go internal/server/thumbcache_test.go
go mod tidy
git add -A
git commit -m "perf: persist page thumbnails to disk with an LRU hot cache"
```

---

### Task 10: Decode concurrency + pixel-budget guard

A crafted huge-dimension image can exhaust memory on decode, and unbounded concurrent thumbnail decodes can stack many full-page bitmaps. Add a pixel-budget check in `thumb.Generate` (protects scanner and server) and a concurrency semaphore around server-side generation.

**Files:**
- Modify: `internal/thumb/thumb.go` (pixel guard)
- Modify: `internal/server/server.go` (semaphore), `internal/server/api_nodes.go` (acquire around `thumb.Generate`)
- Test: `internal/thumb/thumb_test.go`

**Interfaces:**
- `thumb.Generate` keeps its signature `(src io.Reader, width int) ([]byte, error)` but now rejects images whose pixel count exceeds `maxPixels`.
- Server holds `decodeSem chan struct{}` sized `decodeConcurrency`.

- [ ] **Step 1: Write the failing test**

Add to `internal/thumb/thumb_test.go`:

```go
func TestGenerateRejectsOversizeImage(t *testing.T) {
	// A PNG header declaring enormous dimensions; DecodeConfig reads only the
	// header, so we can assert rejection without allocating the bitmap.
	// Build a minimal valid small PNG and assert it PASSES, then assert the
	// guard constant is enforced via a synthetic DecodeConfig path.
	small := makeTestPNG(t, 10, 10) // existing/local helper producing PNG bytes
	if _, err := Generate(bytesReader(small), 8); err != nil {
		t.Fatalf("small image should pass: %v", err)
	}
	big := makeTestPNG(t, 1, 1) // placeholder; replaced below
	_ = big
}
```

Because fabricating a real >100MP encoded image is heavy, instead test the guard directly by exposing the check. Refactor the pixel test to target a small helper:

```go
func TestPixelBudget(t *testing.T) {
	if withinPixelBudget(10000, 10000) { // 100,000,000 px == 100MP, over budget
		t.Fatal("100MP should be over budget")
	}
	if !withinPixelBudget(4000, 6000) { // 24MP, fine
		t.Fatal("24MP should be within budget")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/thumb/ -run TestPixelBudget -v`
Expected: FAIL — `withinPixelBudget` undefined.

- [ ] **Step 3: Implement the guard**

In `internal/thumb/thumb.go`:

```go
// maxPixels caps the source image area we will decode (guards against
// decompression/pixel-flood bombs). 80 MP comfortably exceeds real comic pages.
const maxPixels = 80 * 1000 * 1000

func withinPixelBudget(w, h int) bool {
	if w <= 0 || h <= 0 {
		return false
	}
	return int64(w)*int64(h) <= maxPixels
}
```

Change `Generate` to read the source once, check dimensions via `DecodeConfig`, then decode:

```go
func Generate(src io.Reader, width int) ([]byte, error) {
	if width <= 0 {
		return nil, fmt.Errorf("thumb: width must be > 0")
	}
	raw, err := io.ReadAll(src)
	if err != nil {
		return nil, fmt.Errorf("thumb: read source: %w", err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("thumb: decode config: %w", err)
	}
	if !withinPixelBudget(cfg.Width, cfg.Height) {
		return nil, fmt.Errorf("thumb: image too large (%dx%d)", cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("thumb: decode: %w", err)
	}
	// ... unchanged bounds/scale/encode ...
}
```

(`bytes` is already imported.)

- [ ] **Step 4: Add the server semaphore**

`internal/server/server.go`: add `const decodeConcurrency = 4` (or compute from `runtime.GOMAXPROCS(0)`), add `decodeSem chan struct{}` to `Server`, initialize `make(chan struct{}, decodeConcurrency)` in `NewServer`.

`internal/server/api_nodes.go` `apiPageThumb`, wrap the generate:
```go
	s.decodeSem <- struct{}{}
	data, err := thumb.Generate(rc, s.pageThumbWidth)
	<-s.decodeSem
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
```
(Acquire before, release immediately after generate; the entry reader `rc` and `release()` are already deferred.)

- [ ] **Step 5: Run tests**

Run: `go test ./... && go test -race ./internal/server/... && go vet ./...`
Expected: PASS; race clean.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/thumb/thumb.go internal/server/server.go internal/server/api_nodes.go internal/thumb/thumb_test.go
git add -A
git commit -m "fix: pixel-budget guard on decode + bounded thumbnail concurrency"
```

---

### Task 11: Bounded extract-cache sweeper

The rar/7z extract cache (`<dataDir>/cache/<id>`) grows without bound — every opened compressed comic leaves its extracted images on disk forever. Add a size-bounded sweeper invoked after each scan.

**Files:**
- Create: `internal/cache/cache.go`
- Modify: `internal/scan/manager.go` (invoke after a scan), `internal/config/config.go` (add `Cache.MaxBytes`)
- Test: `internal/cache/cache_test.go`

**Interfaces:**
- Produces:
  ```go
  // Enforce evicts whole <root>/<id> subdirectories, oldest-modified first,
  // until the total size of root is <= maxBytes. Returns dirs evicted.
  func Enforce(root string, maxBytes int64) (int, error)
  ```

- [ ] **Step 1: Write the failing test**

Create `internal/cache/cache_test.go`:

```go
package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeDir(t *testing.T, root, name string, bytes int, mod time.Time) {
	t.Helper()
	d := filepath.Join(root, name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "data"), make([]byte, bytes), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(d, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestEnforceEvictsOldestUntilUnderBudget(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	writeDir(t, root, "1", 1000, now.Add(-3*time.Hour)) // oldest
	writeDir(t, root, "2", 1000, now.Add(-2*time.Hour))
	writeDir(t, root, "3", 1000, now.Add(-1*time.Hour)) // newest
	evicted, err := Enforce(root, 2200) // keep ~2 dirs
	if err != nil {
		t.Fatal(err)
	}
	if evicted != 1 {
		t.Fatalf("evicted = %d, want 1", evicted)
	}
	if _, err := os.Stat(filepath.Join(root, "1")); !os.IsNotExist(err) {
		t.Fatal("oldest dir should be evicted")
	}
	if _, err := os.Stat(filepath.Join(root, "3")); err != nil {
		t.Fatal("newest dir should remain")
	}
}

func TestEnforceNoopWhenUnderBudget(t *testing.T) {
	root := t.TempDir()
	writeDir(t, root, "1", 100, time.Now())
	n, err := Enforce(root, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("evicted = %d, want 0", n)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cache/ -v`
Expected: FAIL — package/`Enforce` missing.

- [ ] **Step 3: Implement `Enforce`**

```go
// Package cache bounds the on-disk extract cache so it cannot grow without limit.
package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type dirInfo struct {
	path  string
	size  int64
	mtime int64
}

func dirSize(path string) (int64, error) {
	var total int64
	err := filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	return total, err
}

// Enforce evicts whole top-level subdirectories of root, oldest-modified first,
// until root's total size is <= maxBytes. maxBytes <= 0 disables eviction.
func Enforce(root string, maxBytes int64) (int, error) {
	if maxBytes <= 0 {
		return 0, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("cache: read %s: %w", root, err)
	}
	var dirs []dirInfo
	var total int64
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(root, e.Name())
		sz, err := dirSize(p)
		if err != nil {
			return 0, fmt.Errorf("cache: size %s: %w", p, err)
		}
		info, err := e.Info()
		if err != nil {
			return 0, fmt.Errorf("cache: stat %s: %w", p, err)
		}
		dirs = append(dirs, dirInfo{path: p, size: sz, mtime: info.ModTime().Unix()})
		total += sz
	}
	if total <= maxBytes {
		return 0, nil
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].mtime < dirs[j].mtime })
	evicted := 0
	for _, d := range dirs {
		if total <= maxBytes {
			break
		}
		if err := os.RemoveAll(d.path); err != nil {
			return evicted, fmt.Errorf("cache: evict %s: %w", d.path, err)
		}
		total -= d.size
		evicted++
	}
	return evicted, nil
}
```

- [ ] **Step 4: Config + wire into the scan flow**

`internal/config/config.go`: add
```go
type Cache struct {
	MaxBytes int64 `yaml:"maxBytes"`
}
```
to `Config` (`Cache Cache \`yaml:"cache"\``), default `Cache{MaxBytes: 2 << 30}` (2 GiB) in `defaults()`. No hard validation (0 = disabled is legal).

`internal/scan/manager.go`: after a successful scan completes, call the sweeper on `<dataDir>/cache`. The manager already knows `dataDir`? If not, thread it in: the manager is constructed in `main.go` — pass `dataDir` and `cacheMaxBytes` into `scan.New(...)` and store them. After the scanner's `Scan` returns in the manager's run path, call:
```go
if n, err := cache.Enforce(filepath.Join(m.dataDir, "cache"), m.cacheMaxBytes); err != nil {
	log.Printf("scan: enforce cache budget: %v", err)
} else if n > 0 {
	log.Printf("scan: evicted %d cached extract dir(s)", n)
}
```
Update `scan.New` signature and the `main.go` call accordingly (`scan.New(scanner, settingsRepo, pathRepo, cfg.DataDir, cfg.Cache.MaxBytes)`). Import `Tefnut/internal/cache` and `path/filepath` in `manager.go`.

(If the manager runs the scan in multiple places — interval, daily, watch — put the Enforce call in the single shared `runScan` path the scanner is invoked through, so all modes get it.)

- [ ] **Step 5: Run tests**

Run: `go test ./... && go vet ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/cache/cache.go internal/cache/cache_test.go internal/scan/manager.go internal/config/config.go cmd/tefnut/main.go
git add -A
git commit -m "feat: bound the on-disk extract cache with a post-scan sweeper"
```

---

### Task 12: Parallel cover generation during scan

Cover decode+resize is CPU-bound and serial today, making first-scan of a large library slow. Build covers for new/changed comics through a bounded worker pool.

**Files:**
- Modify: `internal/library/scanner.go` (`Scan`/`scanDir`/`buildComic`)
- Test: `internal/library/scanner_test.go`

**Interfaces:** internal only. `buildComic` becomes safe to call concurrently (it already writes to distinct files/cache dirs and rows; `database/sql` handles concurrent use).

- [ ] **Step 1: Write the failing test**

Add to `internal/library/scanner_test.go` — assert that a scan of several new comics produces all covers/page counts (correctness under the new concurrent path):

```go
func TestScanBuildsAllCoversConcurrently(t *testing.T) {
	dir := t.TempDir()
	dataDir := t.TempDir()
	sc, repo, paths := newScannerForTest(t, dataDir)
	libDir := filepath.Join(dir, "lib")
	os.MkdirAll(libDir, 0o755)
	paths.Add(context.Background(), "lib", libDir)
	const n = 12
	for i := 0; i < n; i++ {
		writeTestZip(t, filepath.Join(libDir, "c"+itoa(i)+".cbz"), []string{"01.jpg", "02.jpg"})
	}
	if err := sc.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	comics, err := repo.ListChildren(context.Background(), rootID(t, repo, libDir), -1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(comics) != n {
		t.Fatalf("comics = %d, want %d", len(comics), n)
	}
	for _, cm := range comics {
		if cm.PageCount != 2 || cm.CoverStatus != store.CoverReady {
			t.Fatalf("comic %s: pages=%d cover=%d", cm.Name, cm.PageCount, cm.CoverStatus)
		}
	}
}
```

Adapt `newScannerForTest`/`writeTestZip`/`rootID`/`itoa` to the file's helpers (reuse those added in Task 1).

- [ ] **Step 2: Run test to verify it fails or passes**

Run: `go test ./internal/library/ -run TestScanBuildsAllCovers -v`
Expected: PASS with the current serial code (it's a correctness guard). It must still PASS after the concurrency change.

- [ ] **Step 3: Restructure to a worker pool**

Collect build tasks during the walk, run them in parallel after. Add a task type and a queue on the `Scanner` for the duration of one `Scan`:

```go
import (
	"runtime"
	"sync"
	// ...
)

type buildTask struct {
	node  *store.Node
	size  int64
	mtime int64
}
```

In `scanDir`, replace the inline `s.buildComic(...)` call with appending to a queue passed down. Thread a `*[]buildTask` (or a channel) through `Scan`→`scanDir`. Simplest: give `Scan` a local slice and a pointer threaded through `scanDir`:

```go
func (s *Scanner) Scan(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var builds []buildTask
	// ... existing root loop, but call s.scanDir(ctx, abs, node.ID, &builds) ...
	for _, n := range byPath {
		s.removeNode(ctx, n)
	}
	s.runBuilds(ctx, builds)
	return nil
}

func (s *Scanner) scanDir(ctx context.Context, dir string, parentID int64, builds *[]buildTask) error {
	// ... unchanged until the comic branch:
	if !seen || node.Size != info.Size() || node.MTime != info.ModTime().Unix() {
		*builds = append(*builds, buildTask{node: node, size: info.Size(), mtime: info.ModTime().Unix()})
	}
	// recursion passes builds through:
	//   if err := s.scanDir(ctx, p, node.ID, builds); err != nil { ... }
}

func (s *Scanner) runBuilds(ctx context.Context, tasks []buildTask) {
	if len(tasks) == 0 {
		return
	}
	workers := runtime.GOMAXPROCS(0)
	if workers > len(tasks) {
		workers = len(tasks)
	}
	ch := make(chan buildTask)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range ch {
				s.buildComic(ctx, t.node, t.size, t.mtime)
			}
		}()
	}
	for _, t := range tasks {
		ch <- t
	}
	close(ch)
	wg.Wait()
}
```

`buildComic` itself is unchanged (it already operates on distinct nodes/files). Confirm the `repo` (`*store.NodeRepo`) is safe for concurrent use — it is, since each method uses `database/sql` which is concurrency-safe, and writes serialize on the single write connection (Task 6).

- [ ] **Step 4: Run tests (with race)**

Run: `go test ./internal/library/... && go test -race ./internal/library/... && go vet ./...`
Expected: PASS; race clean.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/library/scanner.go internal/library/scanner_test.go
git add internal/library/scanner.go internal/library/scanner_test.go
git commit -m "perf: build comic covers through a bounded worker pool"
```

---

### Task 13: Engineering hygiene — generic 5xx errors, security headers, gzip, gofmt, CI

Stops 5xx bodies from leaking internal paths, adds `nosniff` + gzip, makes the tree gofmt-clean, and adds a CI workflow so regressions are caught.

**Files:**
- Modify: `internal/server/response.go`
- Modify: `cmd/tefnut/main.go` (middleware)
- Create: `.github/workflows/ci.yml`
- Format: `internal/store/types.go`, `internal/library/scanner.go`, and any others `gofmt -l` flags
- Test: `internal/server/server_test.go` (assert 5xx body is generic)

**Interfaces:** `fail` keeps its signature; behavior differs by status class.

- [ ] **Step 1: Write the failing test**

Add to `internal/server/server_test.go`:

```go
func TestServerErrorBodyIsGeneric(t *testing.T) {
	ts := newTestServer(t)
	// Force a 500 by requesting a page from a comic whose archive path is bogus.
	id := ts.seedBrokenComic(t) // helper: insert a node with a non-existent Path
	rec := ts.get(t, "/api/comics/"+itoa64(id)+"/pages/0")
	if rec.Code < 500 {
		t.Skipf("expected a 5xx, got %d (adjust seed)", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "/") && strings.Contains(rec.Body.String(), ".cbz") {
		t.Fatalf("5xx body leaked a path: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "internal error") {
		t.Fatalf("5xx body = %s, want generic message", rec.Body.String())
	}
}
```

If a `seedBrokenComic` helper is awkward, target any handler that returns a 500 with a wrapped path error and assert the body equals the generic message. Keep the assertion: 5xx body contains `"internal error"` and not a filesystem path.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestServerErrorBodyIsGeneric -v`
Expected: FAIL — current body echoes `err.Error()`.

- [ ] **Step 3: Make `fail` class-aware**

```go
package server

import (
	"log"

	"github.com/labstack/echo/v4"
)

func ok(c echo.Context, data any) error {
	return c.JSON(200, map[string]any{"code": 0, "message": "success", "data": data})
}

// fail writes a JSON error. For 5xx the real error is logged server-side and a
// generic message is returned so internal details (paths, SQL) never reach the
// client. For 4xx the (caller-authored, user-safe) message is returned as-is.
func fail(c echo.Context, status int, err error) error {
	msg := err.Error()
	if status >= 500 {
		log.Printf("server: %s %s -> %d: %v", c.Request().Method, c.Request().URL.Path, status, err)
		msg = "internal error"
	}
	return c.JSON(status, map[string]any{"code": -1, "message": msg})
}
```

- [ ] **Step 4: Add middleware**

`cmd/tefnut/main.go`, after `e.Use(middleware.Recover())`:

```go
	e.Use(middleware.Gzip())
	e.Use(middleware.SecureWithConfig(middleware.SecureConfig{
		ContentTypeNosniff: "nosniff",
	}))
```

(Do NOT add a Content-Security-Policy — the templates use an inline script and CSP would break it; nosniff is the safe, free win. No auth is added, per the single-user design.)

- [ ] **Step 5: gofmt the tree + add CI**

```bash
gofmt -w ./...
```

Create `.github/workflows/ci.yml`:

```yaml
name: ci
on:
  push:
  pull_request:
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'
      - name: gofmt
        run: test -z "$(gofmt -l .)" || (gofmt -l . && exit 1)
      - name: vet
        run: go vet ./...
      - name: test
        run: go test ./...
      - name: govulncheck
        run: |
          go install golang.org/x/vuln/cmd/govulncheck@latest
          govulncheck ./... || true
```

(`govulncheck` is advisory here — `|| true` keeps it non-blocking; the maintainer can tighten it once the flagged `x/net`/`x/crypto`/echo bumps land. Dependency bumps themselves are out of scope for this task to keep the diff reviewable.)

- [ ] **Step 6: Run tests + fmt check**

Run: `gofmt -l . ; go build ./... && go vet ./... && go test ./...`
Expected: `gofmt -l .` prints nothing; all green.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "chore: generic 5xx errors, nosniff+gzip, gofmt, CI workflow"
```

---

## Self-Review

**Spec coverage** — every approved item maps to a task: #2→T1, #9→T2, #8→T3, #10→T4, pagination→T5, #11→T6, #4→T7, #5→T8, #6→T9, #7→T10, #3→T11, #12→T12, #13→T13. Dropped by decision: auth (#1), multi-user (#14); deferred: FTS5, targeted-watch.

**Ordering/dependencies** — T2 (migrations) precedes any schema-touching work; T3 (columns) precedes T4/T5 which edit `node_repo` SQL; T6 (read/write split) runs after the `node_repo` changes settle so it sweeps the final method set once; T7 (reader cache + `openPage`) precedes T8/T9/T10 which all build on `openPage`/`apiPageThumb`. Each task is independently testable and leaves the build green.

**Type consistency** — `NodePatch`/`UpdateFields` (T4) names match their use in `api_meta.go`; `ListChildren`/`Search` gain `limit, offset` consistently across store + all call sites (T5, with scanner passing `-1`); `ReaderCache.Acquire` returns `(Reader, func(), error)` used identically in `openPage` (T7); `newThumbCache(maxEntries, dir)` and `get/put` signatures match T9's handler edits; `thumb.Generate` keeps its signature (T10). `DB.Read()/Write()` (T6) replace `SQL()` everywhere repos are constructed.

**Placeholder scan** — no TBD/TODO; every code step shows concrete code; test code is included per task. The one judgment call left to the implementer (relative-vs-absolute read DSN in T6) is explicitly bounded with a verification step.

**Known follow-ups (out of scope, noted for the final review):** page-thumb disk dir (`thumbs/pages`) is not size-bounded by the T11 sweeper (it bounds `cache/` only) — thumbnails are tiny, acceptable; the reader `<img>` `innerHTML` rebuild and continuous-mode `tabindex` keyboard-focus gap are frontend polish not in the approved list; dependency version bumps (echo/x-net/x-crypto) are deferred to a focused follow-up so this diff stays reviewable.
