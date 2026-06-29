# Tefnut Reader-UX Enhancement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a collapsible sidebar, a vertical settings scan-mode layout with hour/minute dropdowns, a lazy-loaded bottom thumbnail strip in the reader, and per-comic reading direction (LTR/RTL) — on top of the existing Tefnut comic server.

**Architecture:** A new per-page thumbnail endpoint reuses the `thumb` package with a small in-memory cache. Reading direction is a new `nodes.reading_direction` column (idempotent migration), exposed via comic-detail + the existing PATCH, and applied live in the reader. The rest is frontend: a collapsible sidebar, a restyled settings page, and a reader thumbnail strip + direction toggle.

**Tech Stack:** Go 1.24, echo v4, modernc.org/sqlite, html/template + go:embed, vanilla JS (IntersectionObserver, localStorage).

## Global Constraints

Every task's requirements implicitly include this section.

- **Reading direction** is per-comic: column `reading_direction TEXT NOT NULL DEFAULT 'ltr'` on `nodes`, values `'ltr'` | `'rtl'`. Migration is idempotent (`PRAGMA table_info` then `ALTER TABLE ADD COLUMN`) AND the column is in the `CREATE TABLE` for fresh DBs.
- **Navigation mapping** (logical page index `0..N-1`, page 0 = first; "advance" = index+1):
  - LTR: click RIGHT half = advance; thumb strip ordered left→right (0 leftmost); keyboard → advance, ← back.
  - RTL: click LEFT half = advance; thumb strip ordered right→left (0 rightmost); keyboard ← advance, → back.
  - The bottom-bar buttons are LOGICAL: `上一页` always = back (index−1), `下一页` always = advance (index+1) — they do NOT flip with direction.
  - Single-page display; direction only affects navigation mapping, control mapping, and thumb-strip order.
- **Thumbnail endpoint:** `GET /api/comics/:id/pages/:n/thumb` → page n downscaled to width 120 via `thumb.Generate`, JPEG, with `Cache-Control: public, max-age=86400`, backed by a bounded in-memory cache (key `<id>:<n>`).
- **Collapse state** (sidebar, thumb strip) persisted in `localStorage`; restored on load.
- **Page index is 0-based**; rating 0..5; node types comic=1/dir=2.
- **Style:** small focused files; validate input at boundaries; never silently swallow errors; wrap errors with `store:`/package context; frontend `fetch` surfaces failures (alert on user actions).
- **Commit prefixes:** `feat:` `fix:` `refactor:` `docs:` `test:` `chore:`.

## File Structure

```
internal/store/migrate.go                 CREATE  ensureColumn idempotent migration helper
internal/store/migrate_test.go            CREATE
internal/store/store.go                   MODIFY  add reading_direction to nodes CREATE TABLE + call ensureColumn
internal/store/types.go                   MODIFY  Node.ReadingDirection field
internal/store/node_repo.go               MODIFY  nodeCols + Search list + scanNode + UpdateReadingDirection
internal/store/node_repo_test.go          MODIFY  direction default + update tests
internal/server/thumbcache.go             CREATE  bounded in-memory []byte cache
internal/server/thumbcache_test.go        CREATE
internal/server/api_nodes.go              MODIFY  comicDetailDTO.ReadingDirection + apiPageThumb
internal/server/api_meta.go               MODIFY  metaReq.ReadingDirection + validation + update
internal/server/server.go                 MODIFY  thumb route + Server.thumbs cache field/init
internal/server/server_test.go            MODIFY  thumb + direction + sidebar-toggle tests
internal/server/web/templates/layout.html MODIFY  sidebar collapse toggle
internal/server/web/templates/reader.html MODIFY  data-dir + direction toggle + thumb strip
internal/server/web/templates/settings.html MODIFY vertical scan modes + hour/minute selects + interval hint
internal/server/web/static/js/sidebar.js  CREATE  collapse logic + localStorage
internal/server/web/static/js/reader.js   MODIFY  direction mapping + thumb strip + collapse
internal/server/web/static/js/settings.js MODIFY  hour/minute <-> HH:MM
internal/server/web/static/css/app.css     MODIFY sidebar collapse, settings vertical, thumb strip, direction
```

---

## Task 1: Store — reading_direction column, migration, repo

**Files:**
- Create: `internal/store/migrate.go`, `internal/store/migrate_test.go`
- Modify: `internal/store/store.go`, `internal/store/types.go`, `internal/store/node_repo.go`, `internal/store/node_repo_test.go`

**Interfaces:**
- Produces:
  - `store.ensureColumn(db *sql.DB, table, column, ddl string) error` — idempotent ADD COLUMN.
  - `store.Node.ReadingDirection string` field.
  - `(*NodeRepo) UpdateReadingDirection(ctx context.Context, id int64, dir string) error`.
  - `nodes` rows now carry `reading_direction` (default `'ltr'`); `scanNode` reads it.

- [ ] **Step 1: Write the failing migration test**

Create `internal/store/migrate_test.go`:

```go
package store

import (
	"context"
	"testing"
)

func TestEnsureColumnIdempotent(t *testing.T) {
	db := openTemp(t)
	// reading_direction already added by Open(); calling ensureColumn again must be a no-op (no error).
	if err := ensureColumn(db.SQL(), "nodes", "reading_direction", "TEXT NOT NULL DEFAULT 'ltr'"); err != nil {
		t.Fatalf("second ensureColumn: %v", err)
	}
	// a brand-new column should be added without error
	if err := ensureColumn(db.SQL(), "nodes", "extra_col", "TEXT NOT NULL DEFAULT ''"); err != nil {
		t.Fatalf("add extra_col: %v", err)
	}
	if err := ensureColumn(db.SQL(), "nodes", "extra_col", "TEXT NOT NULL DEFAULT ''"); err != nil {
		t.Fatalf("re-add extra_col: %v", err)
	}
}

func TestNodeDefaultReadingDirection(t *testing.T) {
	ctx := context.Background()
	r := NewNodeRepo(openTemp(t))
	n := mkNode(t, r, 0, "c", "/lib/c.zip", NodeComic)
	got, _ := r.Get(ctx, n.ID)
	if got.ReadingDirection != "ltr" {
		t.Fatalf("default direction = %q, want ltr", got.ReadingDirection)
	}
}

func TestUpdateReadingDirection(t *testing.T) {
	ctx := context.Background()
	r := NewNodeRepo(openTemp(t))
	n := mkNode(t, r, 0, "c", "/lib/c.zip", NodeComic)
	if err := r.UpdateReadingDirection(ctx, n.ID, "rtl"); err != nil {
		t.Fatal(err)
	}
	got, _ := r.Get(ctx, n.ID)
	if got.ReadingDirection != "rtl" {
		t.Fatalf("direction = %q, want rtl", got.ReadingDirection)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/store/ -run 'EnsureColumn|ReadingDirection' -v`
Expected: FAIL (undefined `ensureColumn` / `UpdateReadingDirection` / `ReadingDirection`).

- [ ] **Step 3: Create the migration helper**

Create `internal/store/migrate.go`:

```go
package store

import (
	"database/sql"
	"fmt"
)

// ensureColumn adds `column ddl` to `table` if it does not already exist.
// table/column/ddl are internal constants, never user input.
func ensureColumn(db *sql.DB, table, column, ddl string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return fmt.Errorf("store: table_info %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("store: scan table_info: %w", err)
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + ddl); err != nil {
		return fmt.Errorf("store: add column %s.%s: %w", table, column, err)
	}
	return nil
}
```

- [ ] **Step 4: Wire the column into store.go**

In `internal/store/store.go`, add the column to the `nodes` `CREATE TABLE` (after `rating ...`):

```sql
  reading_direction TEXT NOT NULL DEFAULT 'ltr',
```

And in `Open`, after the `sqldb.Exec(schema)` succeeds and before `return &DB{...}`:

```go
	if err := ensureColumn(sqldb, "nodes", "reading_direction", "TEXT NOT NULL DEFAULT 'ltr'"); err != nil {
		sqldb.Close()
		return nil, err
	}
```

- [ ] **Step 5: Add the field + repo method + scan**

In `internal/store/types.go`, add to `Node` (after `Rating int`):

```go
	ReadingDirection string
```

In `internal/store/node_repo.go`:
- Change `nodeCols` to append the new column:

```go
const nodeCols = `id, parent_id, name, path, type, page_count, cover_status,
	author, rating, size, mtime, created_at, updated_at, reading_direction`
```

- In `Search`, append `n.reading_direction` to the explicit column list (the `SELECT n.id, ... n.updated_at` line) so it ends `..., n.updated_at, n.reading_direction FROM nodes n`.
- In `scanNode`, add `&n.ReadingDirection` as the LAST scan target (after `&n.UpdatedAt`).
- Add the method:

```go
func (r *NodeRepo) UpdateReadingDirection(ctx context.Context, id int64, dir string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE nodes SET reading_direction=?, updated_at=? WHERE id=?`, dir, time.Now().Unix(), id)
	if err != nil {
		return fmt.Errorf("store: update reading_direction %d: %w", id, err)
	}
	return nil
}
```

- [ ] **Step 6: Run tests + build**

Run: `go build ./... && go test ./internal/store/...`
Expected: build clean, all store tests PASS (existing + 3 new).

- [ ] **Step 7: Commit**

```bash
git add internal/store
git commit -m "feat: add per-comic reading_direction column and migration"
```

---

## Task 2: Per-page thumbnail endpoint + cache

**Files:**
- Create: `internal/server/thumbcache.go`, `internal/server/thumbcache_test.go`
- Modify: `internal/server/api_nodes.go` (apiPageThumb), `internal/server/server.go` (route + cache field)
- Test: extend `internal/server/server_test.go`

**Interfaces:**
- Produces:
  - `server.thumbCache` with `newThumbCache(max int) *thumbCache`, `(*thumbCache) get(key string) ([]byte, bool)`, `(*thumbCache) put(key string, b []byte)`.
  - `Server.thumbs *thumbCache` field, initialized in `NewServer` (signature UNCHANGED).
  - `s.apiPageThumb(c echo.Context) error` for `GET /api/comics/:id/pages/:n/thumb`.

- [ ] **Step 1: Write the failing cache test**

Create `internal/server/thumbcache_test.go`:

```go
package server

import "testing"

func TestThumbCachePutGet(t *testing.T) {
	c := newThumbCache(2)
	c.put("a", []byte("x"))
	if b, ok := c.get("a"); !ok || string(b) != "x" {
		t.Fatalf("get a = %q %v", b, ok)
	}
	if _, ok := c.get("missing"); ok {
		t.Fatal("missing should not be present")
	}
}

func TestThumbCacheBounded(t *testing.T) {
	c := newThumbCache(2)
	c.put("a", []byte("1"))
	c.put("b", []byte("2"))
	c.put("c", []byte("3")) // exceeds max → cache cleared, then c stored
	if c.size() > 2 {
		t.Fatalf("cache size %d exceeds max", c.size())
	}
	if _, ok := c.get("c"); !ok {
		t.Fatal("most recent put must be retained")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/server/ -run TestThumbCache -v`
Expected: FAIL (undefined `newThumbCache`).

- [ ] **Step 3: Implement the cache**

Create `internal/server/thumbcache.go`:

```go
package server

import "sync"

// thumbCache is a tiny bounded in-memory byte cache. When it would exceed max
// entries it clears itself (simple, allocation-free eviction adequate for a
// single-user home app).
type thumbCache struct {
	mu  sync.Mutex
	m   map[string][]byte
	max int
}

func newThumbCache(max int) *thumbCache {
	return &thumbCache{m: make(map[string][]byte), max: max}
}

func (c *thumbCache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.m[key]
	return b, ok
}

func (c *thumbCache) put(key string, b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= c.max {
		c.m = make(map[string][]byte)
	}
	c.m[key] = b
}

func (c *thumbCache) size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.m)
}
```

- [ ] **Step 4: Add the cache field + init**

In `internal/server/server.go`, add `thumbs *thumbCache` to the `Server` struct, and in `NewServer` initialize it in the returned struct literal: `thumbs: newThumbCache(256)`. (NewServer's parameter list is unchanged.)

- [ ] **Step 5: Write the failing handler test**

Append to `internal/server/server_test.go`:

```go
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
	if rec.Header().Get("Cache-Control") == "" {
		t.Fatal("expected Cache-Control header")
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
```

> `seedComic` builds a zip whose first image is a real PNG (`001.png`), so page 0 is a decodable image suitable for thumbnailing.

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./internal/server/ -run TestApiPageThumb -v`
Expected: FAIL (route 404 / handler undefined).

- [ ] **Step 7: Implement apiPageThumb + route**

In `internal/server/api_nodes.go`, add (reuse the existing imports plus `bytes`, `Tefnut/internal/thumb`):

```go
func (s *Server) apiPageThumb(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parseID(c, "id")
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	pageNum, err := strconv.Atoi(c.Param("n"))
	if err != nil || pageNum < 0 {
		return fail(c, http.StatusBadRequest, fmt.Errorf("invalid page"))
	}
	key := strconv.FormatInt(id, 10) + ":" + strconv.Itoa(pageNum)
	if b, ok := s.thumbs.get(key); ok {
		c.Response().Header().Set("Cache-Control", "public, max-age=86400")
		return c.Blob(http.StatusOK, "image/jpeg", b)
	}
	n, err := s.nodes.Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return fail(c, http.StatusNotFound, err)
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	cacheDir := filepath.Join(s.dataDir, "cache", strconv.FormatInt(id, 10))
	r, err := archive.Open(ctx, n.Path, cacheDir)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	defer r.Close()
	names := r.List()
	if pageNum >= len(names) {
		return fail(c, http.StatusNotFound, fmt.Errorf("page out of range"))
	}
	rc, err := r.Open(names[pageNum])
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	defer rc.Close()
	data, err := thumb.Generate(rc, 120)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	s.thumbs.put(key, data)
	c.Response().Header().Set("Cache-Control", "public, max-age=86400")
	return c.Blob(http.StatusOK, "image/jpeg", data)
}
```

Add `"Tefnut/internal/thumb"` to the api_nodes.go imports. In `internal/server/server.go` `Register`, add (near the other `/comics/:id/pages` route):

```go
	api.GET("/comics/:id/pages/:n/thumb", s.apiPageThumb)
```

- [ ] **Step 8: Run tests + build**

Run: `go build ./... && go test ./internal/server/...`
Expected: PASS (thumb cache + endpoint tests).

- [ ] **Step 9: Commit**

```bash
git add internal/server
git commit -m "feat: add per-page thumbnail endpoint with in-memory cache"
```

---

## Task 3: Reading direction in comic detail + PATCH

**Files:**
- Modify: `internal/server/api_nodes.go` (comicDetailDTO + apiComicDetail), `internal/server/api_meta.go` (metaReq + validation)
- Test: extend `internal/server/server_test.go`

**Interfaces:**
- Consumes: `store.NodeRepo.UpdateReadingDirection`.
- Produces: `comicDetailDTO.ReadingDirection` (json `readingDirection`); PATCH `/api/comics/:id` accepts `readingDirection` (`ltr`/`rtl`).

- [ ] **Step 1: Write failing tests**

Append to `internal/server/server_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/server/ -run 'Direction' -v`
Expected: FAIL.

- [ ] **Step 3: Add direction to comic detail**

In `internal/server/api_nodes.go`, add to `comicDetailDTO`:

```go
	ReadingDirection string       `json:"readingDirection"`
```

and set it in `apiComicDetail`'s returned struct: `ReadingDirection: n.ReadingDirection,`.

- [ ] **Step 4: Add direction to PATCH**

In `internal/server/api_meta.go`, add to `metaReq`:

```go
	ReadingDirection *string `json:"readingDirection"`
```

In `apiUpdateMeta`, after the rating-range validation and after loading the node, before/alongside the `UpdateMeta` call, handle direction:

```go
	if body.ReadingDirection != nil {
		dir := *body.ReadingDirection
		if dir != "ltr" && dir != "rtl" {
			return fail(c, http.StatusBadRequest, errors.New("readingDirection must be ltr or rtl"))
		}
		if err := s.nodes.UpdateReadingDirection(ctx, id, dir); err != nil {
			return fail(c, http.StatusInternalServerError, err)
		}
	}
```

(Keep the existing author/rating `UpdateMeta` logic; a PATCH may carry only `readingDirection`, only author/rating, or any combination. If the body has only `readingDirection`, author/rating are preserved by the existing load-then-merge logic.)

- [ ] **Step 5: Run tests + build**

Run: `go build ./... && go test ./internal/server/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/server
git commit -m "feat: expose and update per-comic reading direction via API"
```

---

## Task 4: Collapsible sidebar

**Files:**
- Modify: `internal/server/web/templates/layout.html`
- Create: `internal/server/web/static/js/sidebar.js`
- Modify: `internal/server/web/static/css/app.css`
- Test: extend `internal/server/server_test.go`

**Interfaces:**
- Produces: a `#sidebar-toggle` button in the sidebar block; `sidebar.js` toggles `body.sidebar-collapsed` and persists to `localStorage`.

- [ ] **Step 1: Write the failing test**

Append to `internal/server/server_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/server/ -run TestSidebarHasToggle -v`
Expected: FAIL.

- [ ] **Step 3: Add the toggle + script to the sidebar block**

In `internal/server/web/templates/layout.html`, inside the `{{block "sidebar" .}}` block, add the toggle button as the FIRST child (before `<aside class="sidebar">`) and reference `sidebar.js` at the end of the block (after the existing inline active-link script):

```html
<button id="sidebar-toggle" class="sidebar-toggle" aria-label="切换侧栏">☰</button>
```

and just before the block's closing `{{end}}`:

```html
<script src="/static/js/sidebar.js"></script>
```

- [ ] **Step 4: Create sidebar.js**

Create `internal/server/web/static/js/sidebar.js`:

```js
(function () {
  var KEY = 'sidebarCollapsed';
  if (localStorage.getItem(KEY) === '1') {
    document.body.classList.add('sidebar-collapsed');
  }
  var btn = document.getElementById('sidebar-toggle');
  if (btn) {
    btn.addEventListener('click', function () {
      var collapsed = document.body.classList.toggle('sidebar-collapsed');
      localStorage.setItem(KEY, collapsed ? '1' : '0');
    });
  }
})();
```

- [ ] **Step 5: Add collapse CSS**

In `internal/server/web/static/css/app.css`, append:

```css
.sidebar-toggle { position: fixed; top: 10px; left: 10px; z-index: 30; background: #2a2f3a; color: #eee; border: 1px solid #3a4150; border-radius: 6px; width: 34px; height: 34px; font-size: 16px; cursor: pointer; }
.sidebar { transition: transform 0.2s ease; }
.sidebar .brand { padding-left: 40px; }
main.with-sidebar { transition: margin-left 0.2s ease; }
.sidebar-collapsed .sidebar { transform: translateX(-100%); }
.sidebar-collapsed main.with-sidebar { margin-left: 0; }
```

- [ ] **Step 6: Run tests + build**

Run: `go build ./... && go test ./internal/server/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/server
git commit -m "feat: add collapsible sidebar with persisted state"
```

---

## Task 5: Settings scan-mode layout (vertical + hour/minute selects)

**Files:**
- Modify: `internal/server/web/templates/settings.html`, `internal/server/web/static/js/settings.js`, `internal/server/web/static/css/app.css`
- Test: extend `internal/server/server_test.go`

**Interfaces:**
- Consumes: the existing `scan-data` data-attributes (`data-mode/data-interval/data-daily`) and the `PUT /api/settings` payload (`scanMode/scanInterval/scanDailyTime`).
- Produces: vertical scan-mode blocks; daily time entered via `#daily-h` + `#daily-m` selects (still serialized to `HH:MM`); an interval format hint.

- [ ] **Step 1: Write the failing test**

Append to `internal/server/server_test.go`:

```go
func TestSettingsHasHourMinuteSelects(t *testing.T) {
	_, e, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/settings", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `id="daily-h"`) || !strings.Contains(body, `id="daily-m"`) {
		t.Fatalf("settings should have hour/minute selects: %s", body)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/server/ -run TestSettingsHasHourMinuteSelects -v`
Expected: FAIL.

- [ ] **Step 3: Restructure the scan-mode section in settings.html**

In `internal/server/web/templates/settings.html`, replace the scan-mode `<form id="scanform">` body so each mode is its own vertical block, the interval block has a hint, and the daily block uses two selects. Use this `<form id="scanform">`:

```html
  <form id="scanform" class="scanform">
    <div class="mode-row">
      <label><input type="radio" name="mode" value="interval"> 定时间隔</label>
      <span class="mode-args" data-mode="interval">
        <input id="iv" placeholder="如 30m">
        <small class="hint">格式：如 30m、2h、90m；单位 s/m/h</small>
      </span>
    </div>
    <div class="mode-row">
      <label><input type="radio" name="mode" value="daily"> 每日定时</label>
      <span class="mode-args" data-mode="daily">
        <select id="daily-h"></select> 时
        <select id="daily-m"></select> 分
      </span>
    </div>
    <div class="mode-row">
      <label><input type="radio" name="mode" value="watch"> 监控路径（文件变化即扫描）</label>
    </div>
    <div><button type="submit">保存</button></div>
  </form>
```

(Keep the `<div id="scan-data" data-mode=... data-interval=... data-daily=... hidden></div>` line unchanged.)

- [ ] **Step 4: Update settings.js for hour/minute**

In `internal/server/web/static/js/settings.js`, the top reads `initial` from the `scan-data` dataset (unchanged). Replace the daily-input handling. Add a populate-and-parse helper and use it:

```js
// populate hour (00-23) and minute (00-59) selects
function fill(sel, max, value) {
  for (var i = 0; i <= max; i++) {
    var v = String(i).padStart(2, '0');
    var opt = document.createElement('option');
    opt.value = v; opt.textContent = v;
    sel.appendChild(opt);
  }
  sel.value = value;
}
var dh = document.getElementById('daily-h');
var dm = document.getElementById('daily-m');
var parts = (initial.daily || '03:00').split(':');
fill(dh, 23, String(parts[0] || '03').padStart(2, '0'));
fill(dm, 59, String(parts[1] || '00').padStart(2, '0'));
```

Remove the old `document.getElementById('daily').value = initial.daily;` line (that element no longer exists). In the `#scanform` submit handler, build `scanDailyTime` from the selects instead of `#daily`:

```js
    scanDailyTime: document.getElementById('daily-h').value + ':' + document.getElementById('daily-m').value
```

(Leave the interval read `document.getElementById('iv').value` and `scanMode` read unchanged.)

- [ ] **Step 5: Add vertical-layout CSS**

In `internal/server/web/static/css/app.css`, append:

```css
.scanform { display: flex; flex-direction: column; gap: 12px; align-items: flex-start; }
.scanform .mode-row { display: flex; flex-direction: column; gap: 4px; }
.scanform .mode-args { display: flex; align-items: center; gap: 6px; margin-left: 22px; }
.scanform .hint { color: #8a93a2; font-size: 12px; }
.scanform select { background: #222; color: #eee; border: 1px solid #333; border-radius: 6px; padding: 4px 8px; }
```

- [ ] **Step 6: Run tests + build**

Run: `go build ./... && go test ./internal/server/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/server
git commit -m "feat: vertical settings scan modes with hour/minute selects"
```

---

## Task 6: Reader — reading direction + thumbnail strip + strip collapse

**Files:**
- Modify: `internal/server/web/templates/reader.html`, `internal/server/web/static/js/reader.js`, `internal/server/web/static/css/app.css`
- Test: extend `internal/server/server_test.go`

**Interfaces:**
- Consumes: `data-dir` from the reader template (`{{.ReadingDirection}}`), `GET /api/comics/:id/pages/:n/thumb`, `PATCH /api/comics/:id {readingDirection}`.
- Produces: a direction-aware reader with a `#thumbstrip` lazy-loaded strip, a `#dirtoggle` control, and a collapsible strip.

> The implementer MUST read the current `reader.html` and `reader.js` first (they evolved across prior tasks; `reader.js` already has `show(n)`, `cur`, `total`, `id`, `pageURL`, side `#prev`/`#next`, bottom `#prevbtn`/`#nextbtn`, a `keydown` handler, and meta-editing code). The changes below ADD to that; do not delete the meta-editing or progress logic.

- [ ] **Step 1: Write the failing test**

Append to `internal/server/server_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/server/ -run TestReaderHasStripAndDirection -v`
Expected: FAIL.

- [ ] **Step 3: Update reader.html**

In `internal/server/web/templates/reader.html`: add `data-dir="{{.ReadingDirection}}"` to the `#reader` element; add a `#dirtoggle` button into the `.reader-bar` (near the meta controls); and add the thumbnail strip just inside `#reader`, AFTER the `.reader-bar`. Concretely, add `data-dir="{{.ReadingDirection}}"` to the opening `<div id="reader" ...>` tag, add inside `.reader-bar`:

```html
      <button class="pagebtn" id="dirtoggle" title="翻页方向">方向: <span id="dirlabel">LTR</span></button>
```

and add, as the last child of `#reader` (after the closing `</div>` of `.reader-bar`):

```html
  <div id="thumbstrip" class="thumbstrip">
    <button id="stripToggle" class="strip-toggle" aria-label="收起/展开预览">▼</button>
    <div class="thumbs" id="thumbs"></div>
  </div>
```

- [ ] **Step 4: Add the direction + strip logic to reader.js**

In `internal/server/web/static/js/reader.js`:

(a) Near the top, after `let cur = ...`, add:

```js
let dir = el.dataset.dir || 'ltr';
```

(b) Replace the existing four side/bottom button bindings (the `#next`/`#prev`/`#nextbtn`/`#prevbtn` `.onclick` lines) with a direction-aware binder, and call it:

```js
function advance() { show(cur + 1); }
function back() { show(cur - 1); }

function bindControls() {
  // bottom bar buttons are LOGICAL (上一页 = back, 下一页 = advance) regardless of direction
  document.getElementById('nextbtn').onclick = advance;
  document.getElementById('prevbtn').onclick = back;
  // side click zones are PHYSICAL and flip with direction
  if (dir === 'rtl') {
    document.getElementById('prev').onclick = advance; // left zone advances
    document.getElementById('next').onclick = back;    // right zone goes back
  } else {
    document.getElementById('prev').onclick = back;
    document.getElementById('next').onclick = advance;
  }
}
bindControls();
```

(c) Replace the existing `keydown` handler with a direction-aware one:

```js
document.addEventListener('keydown', (e) => {
  if (e.key === 'ArrowRight') { dir === 'rtl' ? back() : advance(); }
  if (e.key === 'ArrowLeft')  { dir === 'rtl' ? advance() : back(); }
});
```

(d) Add the thumbnail strip (build once; lazy-load via IntersectionObserver; highlight + center current). Add after the strip elements exist in the DOM:

```js
const thumbsEl = document.getElementById('thumbs');
const stripEl = document.getElementById('thumbstrip');

const thumbObserver = new IntersectionObserver((entries) => {
  entries.forEach((entry) => {
    if (entry.isIntersecting) {
      const img = entry.target;
      if (!img.src && img.dataset.src) img.src = img.dataset.src;
      thumbObserver.unobserve(img);
    }
  });
}, { root: stripEl });

function buildStrip() {
  thumbsEl.innerHTML = '';
  thumbsEl.classList.toggle('rtl', dir === 'rtl');
  for (let i = 0; i < total; i++) {
    const fig = document.createElement('div');
    fig.className = 'thumb';
    fig.dataset.page = i;
    const img = document.createElement('img');
    img.dataset.src = `/api/comics/${id}/pages/${i}/thumb`;
    img.alt = `第 ${i + 1} 页`;
    fig.appendChild(img);
    fig.onclick = () => show(i);
    thumbsEl.appendChild(fig);
    thumbObserver.observe(img);
  }
}

function updateStripActive() {
  const figs = thumbsEl.children;
  for (let i = 0; i < figs.length; i++) figs[i].classList.toggle('active', i === cur);
  const active = figs[cur];
  if (active) active.scrollIntoView({ inline: 'center', block: 'nearest', behavior: 'smooth' });
}
```

(e) Inside the existing `show(n)` function, add a call to `updateStripActive();` at the end (after the page image is set and progress saved).

(f) Add the direction toggle handler + initialize the label and strip. Add near the end of the file:

```js
function applyDirLabel() { document.getElementById('dirlabel').textContent = dir.toUpperCase(); }

document.getElementById('dirtoggle').onclick = () => {
  const next = dir === 'ltr' ? 'rtl' : 'ltr';
  fetch(`/api/comics/${id}`, {
    method: 'PATCH', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ readingDirection: next })
  }).then(r => {
    if (!r.ok) { alert('切换方向失败'); return; }
    dir = next;
    applyDirLabel();
    bindControls();
    buildStrip();
    updateStripActive();
  }).catch(() => alert('切换方向失败'));
};

let stripCollapsed = localStorage.getItem('stripCollapsed') === '1';
function applyStripCollapsed() {
  stripEl.classList.toggle('collapsed', stripCollapsed);
  document.getElementById('stripToggle').textContent = stripCollapsed ? '▲' : '▼';
}
document.getElementById('stripToggle').onclick = () => {
  stripCollapsed = !stripCollapsed;
  localStorage.setItem('stripCollapsed', stripCollapsed ? '1' : '0');
  applyStripCollapsed();
};

applyDirLabel();
applyStripCollapsed();
buildStrip();
```

(g) Ensure `buildStrip()` runs after `total` is known and the initial `show(cur)` runs `updateStripActive()` (it will, via the show() edit in (e)). If the file's existing `if (total > 0) show(cur);` runs before `buildStrip()` is defined/called, move the final `buildStrip();` (from block (f)) so it executes; then call `updateStripActive()` once after the initial show. The order at the end of the file should be: definitions → `applyDirLabel(); applyStripCollapsed(); buildStrip();` → then the existing `if (total > 0) show(cur); else ...`. Adjust if needed so `buildStrip()` is called before the initial `show`.

- [ ] **Step 5: Add reader strip + direction CSS**

In `internal/server/web/static/css/app.css`, append:

```css
.thumbstrip { position: relative; background: #1a1d24; border-top: 1px solid #2a2f3a; }
.strip-toggle { position: absolute; top: -22px; left: 50%; transform: translateX(-50%); background: #2a2f3a; color: #eee; border: 1px solid #3a4150; border-bottom: 0; border-radius: 6px 6px 0 0; width: 44px; height: 22px; cursor: pointer; font-size: 12px; }
.thumbs { display: flex; gap: 6px; overflow-x: auto; padding: 8px; height: 96px; }
.thumbs.rtl { flex-direction: row-reverse; }
.thumb { flex: 0 0 auto; cursor: pointer; border: 2px solid transparent; border-radius: 4px; }
.thumb img { height: 76px; display: block; border-radius: 2px; background: #222; }
.thumb.active { border-color: #8ab4f8; }
.thumbstrip.collapsed .thumbs { display: none; }
.thumbstrip.collapsed { height: 8px; }
#dirtoggle { margin-left: 8px; }
```

- [ ] **Step 6: Run tests + build**

Run: `go build ./... && go vet ./... && go test ./internal/server/...`
Expected: PASS (incl. `TestReaderHasStripAndDirection` and all prior reader tests).

- [ ] **Step 7: Commit**

```bash
git add internal/server
git commit -m "feat: reader thumbnail strip and per-comic reading direction"
```

---

## Self-Review

**Spec coverage:**
- §4 data model (reading_direction column, idempotent migration, Node field, UpdateReadingDirection) → Task 1. ✓
- §5.1 thumbnail endpoint + cache + Cache-Control + bounds → Task 2. ✓
- §5.2 direction in detail + PATCH validation → Task 3. ✓
- §6 sidebar collapse (toggle + localStorage + CSS) → Task 4. ✓
- §7 settings vertical + interval hint + hour/minute selects → Task 5. ✓
- §8 reader direction mapping (§2 conventions) + thumbnail strip (lazy/center/highlight/click/collapse) + direction toggle → Task 6. ✓
- §10 tests → each task; frontend interaction noted as manual + DOM render tests. ✓

**Placeholder scan:** No `TODO`/`TBD`. Task 6 step (g) is an ordering instruction (not a placeholder) — it gives the exact end-of-file order and tells the implementer to read the evolved file first, which is the correct handling for a file built across prior tasks. No "add error handling"-style vagueness: every fetch in the JS has explicit `!r.ok`/`.catch` alerts.

**Type consistency:** `reading_direction` (DB) ↔ `Node.ReadingDirection` (Go) ↔ `readingDirection` (JSON, both detail DTO and PATCH metaReq) ↔ `data-dir`/`dir` (reader.js) — consistent. `thumbCache` methods (`get`/`put`/`size`) match between thumbcache.go and its test and `apiPageThumb`. The thumb endpoint path `/api/comics/:id/pages/:n/thumb` is identical in Task 2's route and Task 6's `img.dataset.src`. `UpdateReadingDirection(ctx,id,dir)` signature matches between Task 1 producer and Task 3 consumer. `nodeCols` + Search list + `scanNode` all gain `reading_direction` in the same (last) position. NewServer signature is unchanged (cache field initialized internally), so no caller churn.
