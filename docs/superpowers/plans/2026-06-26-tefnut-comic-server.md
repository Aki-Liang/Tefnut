# Tefnut Comic Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a self-hosted family comic server (a Plex-for-comics): a single Go binary that scans a library directory of comic archives, generates cover thumbnails, and serves a browser UI for browsing, reading, tagging, rating, and tracking reading progress.

**Architecture:** One Go process. A background scanner (on startup + on a cron interval) walks the library root, mirrors the folder tree into SQLite, and pre-generates a cover thumbnail + page count per comic archive. An echo HTTP server renders browse/reader/tag-management pages with `html/template` and serves a JSON+binary API consumed by vanilla JS. Comic pages are served on demand straight from the archive (zip = random access; rar/7z = extract-to-cache on first access).

**Tech Stack:** Go 1.24, echo v4, SQLite via `modernc.org/sqlite` (pure Go) + `database/sql`, `github.com/mholt/archiver/v4` (zip/cbz, rar/cbr, 7z/cb7), `golang.org/x/image` (webp decode + resize), `robfig/cron/v3`, `gopkg.in/yaml.v3`, `go:embed` for templates/static.

## Global Constraints

Every task's requirements implicitly include this section.

- **Go version:** `go 1.24` in `go.mod`.
- **HTTP framework:** echo v4 (`github.com/labstack/echo/v4`).
- **Database:** SQLite via `modernc.org/sqlite` (pure Go, no cgo). Open with `sql.Open("sqlite", dsn)`; blank-import `_ "modernc.org/sqlite"`. Use `database/sql` + hand-written SQL. No ORM.
- **No frontend build tools.** All HTML/CSS/JS is hand-written and embedded via `go:embed`.
- **Module path:** `Tefnut` (unchanged).
- **Node types:** `type=1` comic archive, `type=2` directory. `cover_status`: `0=none, 1=ready, 2=failed`.
- **Page index is 0-based** everywhere (API and UI).
- **Rating is an integer 0..5** (0 = unrated).
- **Reading progress is global** (no users, no auth).
- **Style:** immutable patterns where reasonable; files focused (<800 lines); validate all external input at boundaries; never silently swallow errors; log server-side with context.
- **Commit type prefixes:** `feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `chore:`.
- **Supported archive extensions** (case-insensitive): `.zip .cbz .rar .cbr .7z .cb7`.
- **Supported image extensions** (case-insensitive): `.jpg .jpeg .png .gif .webp .bmp`.

---

## File Structure

```
go.mod                                    Go 1.24, new deps
cmd/tefnut/main.go                        wiring: config → store → scanner → cron → echo
cmd/tefnut/config.yaml                    default config

internal/config/config.go                 Config struct, Load(), Validate()
internal/config/config_test.go

internal/store/types.go                   Node, Tag, TagCount, NodeType consts
internal/store/store.go                   Open(dsn), Migrate(), Close()
internal/store/store_test.go
internal/store/node_repo.go               NodeRepo: Create/Get/ListChildren/Search/Update*/Delete
internal/store/node_repo_test.go
internal/store/tag_repo.go                TagRepo: Upsert/List/Rename/Delete/Add/Remove/ListForNode
internal/store/tag_repo_test.go
internal/store/progress_repo.go           ProgressRepo: Get/Set
internal/store/progress_repo_test.go

internal/archive/natsort.go               natural string ordering
internal/archive/natsort_test.go
internal/archive/formats.go               extension classification + filters
internal/archive/formats_test.go
internal/archive/archive.go               Reader abstraction: Open/List/Open(name)/Close
internal/archive/archive_test.go

internal/thumb/thumb.go                   Generate(src io.Reader, width) ([]byte, error)
internal/thumb/thumb_test.go

internal/library/scanner.go               Scanner.Scan(): walk + diff + cover/pagecount
internal/library/scanner_test.go

internal/server/response.go               ok()/fail() JSON envelope helpers
internal/server/server.go                 Server struct, NewServer, Routes()
internal/server/api_nodes.go              list / detail / cover / pages
internal/server/api_meta.go               PATCH author+rating, add/remove tag
internal/server/api_tags.go               tag CRUD
internal/server/api_progress.go           get/set progress
internal/server/pages.go                  rendered pages: browse / reader / tags
internal/server/server_test.go            handler integration tests
internal/server/web/web.go                go:embed templates + static, parsed templates
internal/server/web/templates/layout.html
internal/server/web/templates/browse.html
internal/server/web/templates/reader.html
internal/server/web/templates/tags.html
internal/server/web/static/css/app.css
internal/server/web/static/js/browse.js
internal/server/web/static/js/reader.js
internal/server/web/static/js/tags.js
internal/server/web/static/img/placeholder.svg
```

Legacy directories (`application/`, `configs/`, `common/`, `public/`, and the old `internal/domain`, `internal/handler`, `internal/infrastructure`) are removed in Task 1 and replaced by the structure above.

---

## Task 1: Project skeleton — go.mod, config, legacy removal

**Files:**
- Modify: `go.mod`
- Delete: `application/`, `configs/`, `common/`, `public/`, `internal/domain/`, `internal/handler/`, `internal/infrastructure/`
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`
- Modify: `cmd/tefnut/main.go` (temporary minimal), `cmd/tefnut/config.yaml`

**Interfaces:**
- Produces:
  - `config.Config` struct with nested `Library{RootPath string}`, `Server{Addr string}`, `Scan{Interval string}`, `Thumbnail{Width int}`, and top-level `DataDir string`.
  - `config.Load(path string) (*Config, error)` — reads YAML, then validates.
  - `(*Config) ScanInterval() (time.Duration, error)` — parses `Scan.Interval`.

- [ ] **Step 1: Remove legacy code and reset module file**

```bash
git rm -r application configs common public internal/domain internal/handler internal/infrastructure
```

Replace `go.mod` with:

```
module Tefnut

go 1.24

require (
	github.com/labstack/echo/v4 v4.7.2
	github.com/mholt/archiver/v4 v4.0.0-alpha.7
	github.com/robfig/cron/v3 v3.0.0
	golang.org/x/image v0.18.0
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.34.4
)
```

- [ ] **Step 2: Write the failing config test**

Create `internal/config/config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "COMIC")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "config.yaml")
	body = body + "\nlibrary:\n  rootPath: " + root + "\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValid(t *testing.T) {
	p := writeTemp(t, "dataDir: "+t.TempDir()+"\nserver:\n  addr: \":9000\"\nscan:\n  interval: \"3m\"\nthumbnail:\n  width: 300")
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Addr != ":9000" {
		t.Errorf("addr = %q", cfg.Server.Addr)
	}
	d, err := cfg.ScanInterval()
	if err != nil || d != 3*time.Minute {
		t.Errorf("interval = %v, err %v", d, err)
	}
}

func TestLoadRejectsMissingRoot(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	os.WriteFile(p, []byte("library:\n  rootPath: /no/such/path\ndataDir: "+dir), 0o644)
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for missing rootPath")
	}
}

func TestLoadRejectsBadInterval(t *testing.T) {
	p := writeTemp(t, "dataDir: "+t.TempDir()+"\nscan:\n  interval: \"notaduration\"")
	if _, err := Load(p); err == nil {
		t.Fatal("expected error for bad interval")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/config/...`
Expected: FAIL (package `config` does not compile — `Load` undefined).

- [ ] **Step 4: Implement config**

Create `internal/config/config.go`:

```go
package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Library struct {
	RootPath string `yaml:"rootPath"`
}

type Server struct {
	Addr string `yaml:"addr"`
}

type Scan struct {
	Interval string `yaml:"interval"`
}

type Thumbnail struct {
	Width int `yaml:"width"`
}

type Config struct {
	Library   Library   `yaml:"library"`
	DataDir   string    `yaml:"dataDir"`
	Server    Server    `yaml:"server"`
	Scan      Scan      `yaml:"scan"`
	Thumbnail Thumbnail `yaml:"thumbnail"`
}

func defaults() *Config {
	return &Config{
		DataDir:   "./data",
		Server:    Server{Addr: ":8086"},
		Scan:      Scan{Interval: "2m"},
		Thumbnail: Thumbnail{Width: 400},
	}
}

// Load reads YAML at path on top of defaults, then validates.
func Load(path string) (*Config, error) {
	cfg := defaults()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.Library.RootPath == "" {
		return errors.New("config: library.rootPath is required")
	}
	info, err := os.Stat(c.Library.RootPath)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("config: library.rootPath %q is not a readable directory", c.Library.RootPath)
	}
	if c.DataDir == "" {
		return errors.New("config: dataDir is required")
	}
	if err := os.MkdirAll(c.DataDir, 0o755); err != nil {
		return fmt.Errorf("config: cannot create dataDir %q: %w", c.DataDir, err)
	}
	if c.Thumbnail.Width <= 0 {
		return errors.New("config: thumbnail.width must be > 0")
	}
	if _, err := c.ScanInterval(); err != nil {
		return fmt.Errorf("config: scan.interval %q invalid: %w", c.Scan.Interval, err)
	}
	return nil
}

// ScanInterval parses Scan.Interval into a Duration.
func (c *Config) ScanInterval() (time.Duration, error) {
	return time.ParseDuration(c.Scan.Interval)
}
```

- [ ] **Step 5: Replace main.go with a temporary minimal entrypoint**

Create `cmd/tefnut/main.go`:

```go
package main

import (
	"flag"
	"log"

	"Tefnut/internal/config"
)

func main() {
	cfgPath := flag.String("config", "./config.yaml", "path to config.yaml")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("tefnut: loaded config, library=%s dataDir=%s addr=%s",
		cfg.Library.RootPath, cfg.DataDir, cfg.Server.Addr)
}
```

Replace `cmd/tefnut/config.yaml`:

```yaml
library:
  rootPath: "./COMIC"
dataDir: "./data"
server:
  addr: ":8086"
scan:
  interval: "2m"
thumbnail:
  width: 400
```

- [ ] **Step 6: Tidy and verify build + tests**

Run:
```bash
go mod tidy
go build ./...
go test ./internal/config/...
```
Expected: tidy resolves deps; build succeeds; config tests PASS.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "refactor: reset to Go 1.24 skeleton with config package"
```

---

## Task 2: SQLite store — open + migrate

**Files:**
- Create: `internal/store/types.go`, `internal/store/store.go`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `store.NodeType` int with consts `NodeComic NodeType = 1`, `NodeDir NodeType = 2`.
  - `store.CoverNone=0`, `store.CoverReady=1`, `store.CoverFailed=2` (int consts).
  - `store.Node` struct: `ID int64; ParentID int64; Name string; Path string; Type NodeType; PageCount int; CoverStatus int; Author string; Rating int; Size int64; MTime int64; CreatedAt int64; UpdatedAt int64`.
  - `store.Tag` struct: `ID int64; Name string`.
  - `store.TagCount` struct: `Tag; Count int`.
  - `store.Open(dsn string) (*store.DB, error)` — opens sqlite, runs migrations, returns wrapper.
  - `store.DB` wraps `*sql.DB`; method `Close() error`; field access via `(*DB).SQL() *sql.DB` for repos.

- [ ] **Step 1: Write the failing test**

Create `internal/store/store_test.go`:

```go
package store

import (
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) *DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMigrateCreatesTables(t *testing.T) {
	db := openTemp(t)
	for _, table := range []string{"nodes", "tags", "node_tags", "progress"} {
		var name string
		err := db.SQL().QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %s missing: %v", table, err)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/...`
Expected: FAIL (undefined `Open`).

- [ ] **Step 3: Implement types and store**

Create `internal/store/types.go`:

```go
package store

type NodeType int

const (
	NodeComic NodeType = 1
	NodeDir   NodeType = 2
)

const (
	CoverNone   = 0
	CoverReady  = 1
	CoverFailed = 2
)

type Node struct {
	ID          int64
	ParentID    int64
	Name        string
	Path        string
	Type        NodeType
	PageCount   int
	CoverStatus int
	Author      string
	Rating      int
	Size        int64
	MTime       int64
	CreatedAt   int64
	UpdatedAt   int64
}

type Tag struct {
	ID   int64
	Name string
}

type TagCount struct {
	Tag
	Count int
}
```

Create `internal/store/store.go`:

```go
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type DB struct {
	db *sql.DB
}

func (d *DB) SQL() *sql.DB { return d.db }

func (d *DB) Close() error { return d.db.Close() }

const schema = `
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
  size         INTEGER NOT NULL DEFAULT 0,
  mtime        INTEGER NOT NULL DEFAULT 0,
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL
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
`

// Open opens the SQLite database at dsn and applies the schema.
func Open(dsn string) (*DB, error) {
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", dsn, err)
	}
	if _, err := sqldb.Exec(`PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL;`); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("store: pragma: %w", err)
	}
	if _, err := sqldb.Exec(schema); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return &DB{db: sqldb}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store
git commit -m "feat: add sqlite store with schema migration"
```

---

## Task 3: Node repository

**Files:**
- Create: `internal/store/node_repo.go`
- Test: `internal/store/node_repo_test.go`

**Interfaces:**
- Consumes: `store.DB`, `store.Node`, `store.NodeType`.
- Produces (all methods on `*NodeRepo`, constructed by `NewNodeRepo(db *DB) *NodeRepo`):
  - `Create(ctx context.Context, n *Node) (*Node, error)` — inserts, returns copy with `ID` set.
  - `Get(ctx context.Context, id int64) (*Node, error)` — returns `ErrNotFound` if absent.
  - `ListChildren(ctx context.Context, parentID int64) ([]*Node, error)` — ordered by `type DESC, name ASC` (dirs first).
  - `Search(ctx context.Context, q string, tagID int64, minRating int) ([]*Node, error)` — comics only (`type=1`) across whole library; `q` LIKE on name (empty = no name filter); `tagID>0` joins `node_tags`; `minRating>0` filters `rating>=`.
  - `UpdateFileAttrs(ctx context.Context, id, size, mtime int64, pageCount, coverStatus int) error`.
  - `UpdateMeta(ctx context.Context, id int64, author string, rating int) error`.
  - `Delete(ctx context.Context, id int64) error` — deletes the node row and its `node_tags` + `progress` rows.
  - Package var `ErrNotFound = errors.New("store: not found")`.

- [ ] **Step 1: Write the failing test**

Create `internal/store/node_repo_test.go`:

```go
package store

import (
	"context"
	"errors"
	"testing"
)

func mkNode(t *testing.T, r *NodeRepo, parent int64, name, path string, typ NodeType) *Node {
	t.Helper()
	n, err := r.Create(context.Background(), &Node{
		ParentID: parent, Name: name, Path: path, Type: typ,
	})
	if err != nil {
		t.Fatalf("Create %s: %v", name, err)
	}
	return n
}

func TestNodeCreateGet(t *testing.T) {
	r := NewNodeRepo(openTemp(t))
	n := mkNode(t, r, 0, "Series", "/lib/Series", NodeDir)
	if n.ID == 0 {
		t.Fatal("expected non-zero id")
	}
	got, err := r.Get(context.Background(), n.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Series" || got.Type != NodeDir {
		t.Errorf("got %+v", got)
	}
}

func TestNodeGetNotFound(t *testing.T) {
	r := NewNodeRepo(openTemp(t))
	_, err := r.Get(context.Background(), 999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListChildrenDirsFirst(t *testing.T) {
	r := NewNodeRepo(openTemp(t))
	mkNode(t, r, 0, "b-comic", "/lib/b.zip", NodeComic)
	mkNode(t, r, 0, "a-dir", "/lib/a", NodeDir)
	kids, err := r.ListChildren(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != 2 || kids[0].Type != NodeDir {
		t.Fatalf("expected dir first, got %+v", kids)
	}
}

func TestSearchByNameAndRating(t *testing.T) {
	ctx := context.Background()
	r := NewNodeRepo(openTemp(t))
	a := mkNode(t, r, 0, "Naruto Vol1", "/lib/n1.zip", NodeComic)
	mkNode(t, r, 0, "Bleach Vol1", "/lib/b1.zip", NodeComic)
	if err := r.UpdateMeta(ctx, a.ID, "Kishimoto", 5); err != nil {
		t.Fatal(err)
	}
	res, err := r.Search(ctx, "naruto", 0, 0)
	if err != nil || len(res) != 1 || res[0].ID != a.ID {
		t.Fatalf("name search: %v / %+v", err, res)
	}
	res, err = r.Search(ctx, "", 0, 3)
	if err != nil || len(res) != 1 || res[0].ID != a.ID {
		t.Fatalf("rating search: %v / %+v", err, res)
	}
}

func TestUpdateFileAttrs(t *testing.T) {
	ctx := context.Background()
	r := NewNodeRepo(openTemp(t))
	n := mkNode(t, r, 0, "c", "/lib/c.zip", NodeComic)
	if err := r.UpdateFileAttrs(ctx, n.ID, 1234, 99, 20, CoverReady); err != nil {
		t.Fatal(err)
	}
	got, _ := r.Get(ctx, n.ID)
	if got.PageCount != 20 || got.CoverStatus != CoverReady || got.Size != 1234 {
		t.Errorf("got %+v", got)
	}
}

func TestDeleteRemovesNode(t *testing.T) {
	ctx := context.Background()
	r := NewNodeRepo(openTemp(t))
	n := mkNode(t, r, 0, "c", "/lib/c.zip", NodeComic)
	if err := r.Delete(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(ctx, n.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected gone, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestNode -v`
Expected: FAIL (undefined `NewNodeRepo`).

- [ ] **Step 3: Implement node repo**

Create `internal/store/node_repo.go`:

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

var ErrNotFound = errors.New("store: not found")

type NodeRepo struct {
	db *sql.DB
}

func NewNodeRepo(db *DB) *NodeRepo { return &NodeRepo{db: db.SQL()} }

const nodeCols = `id, parent_id, name, path, type, page_count, cover_status,
	author, rating, size, mtime, created_at, updated_at`

func scanNode(s interface{ Scan(...any) error }) (*Node, error) {
	n := &Node{}
	err := s.Scan(&n.ID, &n.ParentID, &n.Name, &n.Path, &n.Type, &n.PageCount,
		&n.CoverStatus, &n.Author, &n.Rating, &n.Size, &n.MTime, &n.CreatedAt, &n.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return n, nil
}

func (r *NodeRepo) Create(ctx context.Context, n *Node) (*Node, error) {
	now := time.Now().Unix()
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO nodes (parent_id, name, path, type, page_count, cover_status,
			author, rating, size, mtime, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		n.ParentID, n.Name, n.Path, n.Type, n.PageCount, n.CoverStatus,
		n.Author, n.Rating, n.Size, n.MTime, now, now)
	if err != nil {
		return nil, fmt.Errorf("store: create node %q: %w", n.Path, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	out := *n
	out.ID = id
	out.CreatedAt, out.UpdatedAt = now, now
	return &out, nil
}

func (r *NodeRepo) Get(ctx context.Context, id int64) (*Node, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+nodeCols+` FROM nodes WHERE id = ?`, id)
	n, err := scanNode(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get node %d: %w", id, err)
	}
	return n, nil
}

func (r *NodeRepo) ListChildren(ctx context.Context, parentID int64) ([]*Node, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+nodeCols+` FROM nodes WHERE parent_id = ? ORDER BY type DESC, name ASC`, parentID)
	if err != nil {
		return nil, fmt.Errorf("store: list children %d: %w", parentID, err)
	}
	return collectNodes(rows)
}

func (r *NodeRepo) Search(ctx context.Context, q string, tagID int64, minRating int) ([]*Node, error) {
	var sb strings.Builder
	args := []any{}
	sb.WriteString(`SELECT `)
	prefixed := strings.ReplaceAll(nodeCols, "id,", "n.id,")
	_ = prefixed
	sb.WriteString(`n.id, n.parent_id, n.name, n.path, n.type, n.page_count, n.cover_status,
		n.author, n.rating, n.size, n.mtime, n.created_at, n.updated_at FROM nodes n`)
	if tagID > 0 {
		sb.WriteString(` JOIN node_tags nt ON nt.node_id = n.id AND nt.tag_id = ?`)
		args = append(args, tagID)
	}
	sb.WriteString(` WHERE n.type = ?`)
	args = append(args, NodeComic)
	if q != "" {
		sb.WriteString(` AND n.name LIKE ?`)
		args = append(args, "%"+q+"%")
	}
	if minRating > 0 {
		sb.WriteString(` AND n.rating >= ?`)
		args = append(args, minRating)
	}
	sb.WriteString(` ORDER BY n.name ASC`)
	rows, err := r.db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("store: search: %w", err)
	}
	return collectNodes(rows)
}

func (r *NodeRepo) UpdateFileAttrs(ctx context.Context, id, size, mtime int64, pageCount, coverStatus int) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE nodes SET size=?, mtime=?, page_count=?, cover_status=?, updated_at=? WHERE id=?`,
		size, mtime, pageCount, coverStatus, time.Now().Unix(), id)
	if err != nil {
		return fmt.Errorf("store: update file attrs %d: %w", id, err)
	}
	return nil
}

func (r *NodeRepo) UpdateMeta(ctx context.Context, id int64, author string, rating int) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE nodes SET author=?, rating=?, updated_at=? WHERE id=?`,
		author, rating, time.Now().Unix(), id)
	if err != nil {
		return fmt.Errorf("store: update meta %d: %w", id, err)
	}
	return nil
}

func (r *NodeRepo) Delete(ctx context.Context, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM node_tags WHERE node_id=?`,
		`DELETE FROM progress WHERE node_id=?`,
		`DELETE FROM nodes WHERE id=?`,
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return fmt.Errorf("store: delete node %d: %w", id, err)
		}
	}
	return tx.Commit()
}

func collectNodes(rows *sql.Rows) ([]*Node, error) {
	defer rows.Close()
	out := []*Node{}
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
```

> Note: the `prefixed`/`_ = prefixed` lines are dead scaffolding — delete them; the explicit `n.`-qualified column list below is what is used.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -run TestNode -v` then `go test ./internal/store/...`
Expected: PASS. (Remove the dead `prefixed` lines noted above before running.)

- [ ] **Step 5: Commit**

```bash
git add internal/store/node_repo.go internal/store/node_repo_test.go
git commit -m "feat: add node repository with search and metadata updates"
```

---

## Task 4: Tag repository

**Files:**
- Create: `internal/store/tag_repo.go`
- Test: `internal/store/tag_repo_test.go`

**Interfaces:**
- Consumes: `store.DB`, `store.Tag`, `store.TagCount`.
- Produces (methods on `*TagRepo`, `NewTagRepo(db *DB) *TagRepo`):
  - `Upsert(ctx, name string) (*Tag, error)` — returns existing or newly created tag (trims input; caller validates non-empty).
  - `List(ctx) ([]*TagCount, error)` — all tags with usage count, ordered by name.
  - `Rename(ctx, id int64, name string) error` — returns `ErrDuplicate` on name clash.
  - `Delete(ctx, id int64) error` — removes tag and its `node_tags`.
  - `AddToNode(ctx, nodeID, tagID int64) error` — idempotent (INSERT OR IGNORE).
  - `RemoveFromNode(ctx, nodeID, tagID int64) error`.
  - `ListForNode(ctx, nodeID int64) ([]*Tag, error)` — ordered by name.
  - Package var `ErrDuplicate = errors.New("store: duplicate")`.

- [ ] **Step 1: Write the failing test**

Create `internal/store/tag_repo_test.go`:

```go
package store

import (
	"context"
	"errors"
	"testing"
)

func TestTagUpsertIdempotent(t *testing.T) {
	ctx := context.Background()
	r := NewTagRepo(openTemp(t))
	a, err := r.Upsert(ctx, "shounen")
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.Upsert(ctx, "shounen")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID {
		t.Fatalf("expected same id, got %d and %d", a.ID, b.ID)
	}
}

func TestTagAddListForNodeAndCount(t *testing.T) {
	ctx := context.Background()
	db := openTemp(t)
	nodes := NewNodeRepo(db)
	tags := NewTagRepo(db)
	n := mkNode(t, nodes, 0, "c", "/lib/c.zip", NodeComic)
	tg, _ := tags.Upsert(ctx, "action")
	if err := tags.AddToNode(ctx, n.ID, tg.ID); err != nil {
		t.Fatal(err)
	}
	// idempotent second add
	if err := tags.AddToNode(ctx, n.ID, tg.ID); err != nil {
		t.Fatal(err)
	}
	forNode, _ := tags.ListForNode(ctx, n.ID)
	if len(forNode) != 1 || forNode[0].Name != "action" {
		t.Fatalf("ListForNode = %+v", forNode)
	}
	counts, _ := tags.List(ctx)
	if len(counts) != 1 || counts[0].Count != 1 {
		t.Fatalf("List = %+v", counts)
	}
}

func TestTagRenameDuplicate(t *testing.T) {
	ctx := context.Background()
	r := NewTagRepo(openTemp(t))
	a, _ := r.Upsert(ctx, "a")
	r.Upsert(ctx, "b")
	if err := r.Rename(ctx, a.ID, "b"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}
}

func TestTagDeleteRemovesLinks(t *testing.T) {
	ctx := context.Background()
	db := openTemp(t)
	nodes := NewNodeRepo(db)
	tags := NewTagRepo(db)
	n := mkNode(t, nodes, 0, "c", "/lib/c.zip", NodeComic)
	tg, _ := tags.Upsert(ctx, "x")
	tags.AddToNode(ctx, n.ID, tg.ID)
	if err := tags.Delete(ctx, tg.ID); err != nil {
		t.Fatal(err)
	}
	forNode, _ := tags.ListForNode(ctx, n.ID)
	if len(forNode) != 0 {
		t.Fatalf("expected no tags after delete, got %+v", forNode)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestTag -v`
Expected: FAIL (undefined `NewTagRepo`).

- [ ] **Step 3: Implement tag repo**

Create `internal/store/tag_repo.go`:

```go
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var ErrDuplicate = errors.New("store: duplicate")

type TagRepo struct {
	db *sql.DB
}

func NewTagRepo(db *DB) *TagRepo { return &TagRepo{db: db.SQL()} }

func (r *TagRepo) Upsert(ctx context.Context, name string) (*Tag, error) {
	name = strings.TrimSpace(name)
	var t Tag
	err := r.db.QueryRowContext(ctx, `SELECT id, name FROM tags WHERE name = ?`, name).
		Scan(&t.ID, &t.Name)
	if err == nil {
		return &t, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("store: upsert tag lookup: %w", err)
	}
	res, err := r.db.ExecContext(ctx, `INSERT INTO tags (name) VALUES (?)`, name)
	if err != nil {
		return nil, fmt.Errorf("store: insert tag %q: %w", name, err)
	}
	id, _ := res.LastInsertId()
	return &Tag{ID: id, Name: name}, nil
}

func (r *TagRepo) List(ctx context.Context) ([]*TagCount, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT t.id, t.name, COUNT(nt.node_id)
		 FROM tags t LEFT JOIN node_tags nt ON nt.tag_id = t.id
		 GROUP BY t.id, t.name ORDER BY t.name ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: list tags: %w", err)
	}
	defer rows.Close()
	out := []*TagCount{}
	for rows.Next() {
		tc := &TagCount{}
		if err := rows.Scan(&tc.ID, &tc.Name, &tc.Count); err != nil {
			return nil, err
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}

func (r *TagRepo) Rename(ctx context.Context, id int64, name string) error {
	name = strings.TrimSpace(name)
	_, err := r.db.ExecContext(ctx, `UPDATE tags SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrDuplicate
		}
		return fmt.Errorf("store: rename tag %d: %w", id, err)
	}
	return nil
}

func (r *TagRepo) Delete(ctx context.Context, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM node_tags WHERE tag_id = ?`, id); err != nil {
		return fmt.Errorf("store: delete tag links %d: %w", id, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tags WHERE id = ?`, id); err != nil {
		return fmt.Errorf("store: delete tag %d: %w", id, err)
	}
	return tx.Commit()
}

func (r *TagRepo) AddToNode(ctx context.Context, nodeID, tagID int64) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO node_tags (node_id, tag_id) VALUES (?, ?)`, nodeID, tagID)
	if err != nil {
		return fmt.Errorf("store: add tag %d to node %d: %w", tagID, nodeID, err)
	}
	return nil
}

func (r *TagRepo) RemoveFromNode(ctx context.Context, nodeID, tagID int64) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM node_tags WHERE node_id = ? AND tag_id = ?`, nodeID, tagID)
	if err != nil {
		return fmt.Errorf("store: remove tag %d from node %d: %w", tagID, nodeID, err)
	}
	return nil
}

func (r *TagRepo) ListForNode(ctx context.Context, nodeID int64) ([]*Tag, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT t.id, t.name FROM tags t
		 JOIN node_tags nt ON nt.tag_id = t.id
		 WHERE nt.node_id = ? ORDER BY t.name ASC`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("store: list tags for node %d: %w", nodeID, err)
	}
	defer rows.Close()
	out := []*Tag{}
	for rows.Next() {
		t := &Tag{}
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/tag_repo.go internal/store/tag_repo_test.go
git commit -m "feat: add tag repository"
```

---

## Task 5: Progress repository

**Files:**
- Create: `internal/store/progress_repo.go`
- Test: `internal/store/progress_repo_test.go`

**Interfaces:**
- Consumes: `store.DB`.
- Produces (`NewProgressRepo(db *DB) *ProgressRepo`):
  - `Get(ctx, nodeID int64) (int, error)` — returns `0` if no row.
  - `Set(ctx, nodeID int64, page int) error` — upsert.

- [ ] **Step 1: Write the failing test**

Create `internal/store/progress_repo_test.go`:

```go
package store

import (
	"context"
	"testing"
)

func TestProgressDefaultZero(t *testing.T) {
	r := NewProgressRepo(openTemp(t))
	p, err := r.Get(context.Background(), 1)
	if err != nil || p != 0 {
		t.Fatalf("expected 0/nil, got %d/%v", p, err)
	}
}

func TestProgressSetGet(t *testing.T) {
	ctx := context.Background()
	r := NewProgressRepo(openTemp(t))
	if err := r.Set(ctx, 7, 12); err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, 7, 15); err != nil {
		t.Fatal(err)
	}
	p, _ := r.Get(ctx, 7)
	if p != 15 {
		t.Fatalf("expected 15, got %d", p)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestProgress -v`
Expected: FAIL (undefined `NewProgressRepo`).

- [ ] **Step 3: Implement progress repo**

Create `internal/store/progress_repo.go`:

```go
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type ProgressRepo struct {
	db *sql.DB
}

func NewProgressRepo(db *DB) *ProgressRepo { return &ProgressRepo{db: db.SQL()} }

func (r *ProgressRepo) Get(ctx context.Context, nodeID int64) (int, error) {
	var page int
	err := r.db.QueryRowContext(ctx,
		`SELECT last_page FROM progress WHERE node_id = ?`, nodeID).Scan(&page)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("store: get progress %d: %w", nodeID, err)
	}
	return page, nil
}

func (r *ProgressRepo) Set(ctx context.Context, nodeID int64, page int) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO progress (node_id, last_page, updated_at) VALUES (?,?,?)
		 ON CONFLICT(node_id) DO UPDATE SET last_page=excluded.last_page, updated_at=excluded.updated_at`,
		nodeID, page, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("store: set progress %d: %w", nodeID, err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/progress_repo.go internal/store/progress_repo_test.go
git commit -m "feat: add progress repository"
```

---

## Task 6: Archive natural sort + format classification

**Files:**
- Create: `internal/archive/natsort.go`, `internal/archive/formats.go`
- Test: `internal/archive/natsort_test.go`, `internal/archive/formats_test.go`

**Interfaces:**
- Produces:
  - `archive.NatLess(a, b string) bool` — natural ordering (digit runs compared numerically).
  - `archive.SortNatural(names []string)` — in-place sort using `NatLess`.
  - `archive.IsArchive(name string) bool` — true for supported archive extensions.
  - `archive.IsImage(name string) bool` — true for supported image extensions.
  - `archive.IsJunk(name string) bool` — true for `__MACOSX`, hidden dotfiles, or directory-like trailing slash.

- [ ] **Step 1: Write failing tests**

Create `internal/archive/natsort_test.go`:

```go
package archive

import (
	"reflect"
	"testing"
)

func TestSortNatural(t *testing.T) {
	in := []string{"10.jpg", "2.jpg", "1.jpg", "page-100.png", "page-9.png"}
	SortNatural(in)
	want := []string{"1.jpg", "2.jpg", "10.jpg", "page-9.png", "page-100.png"}
	if !reflect.DeepEqual(in, want) {
		t.Fatalf("got %v want %v", in, want)
	}
}
```

Create `internal/archive/formats_test.go`:

```go
package archive

import "testing"

func TestIsArchive(t *testing.T) {
	for _, n := range []string{"a.zip", "a.CBZ", "b.rar", "b.cbr", "c.7z", "c.cb7"} {
		if !IsArchive(n) {
			t.Errorf("%s should be archive", n)
		}
	}
	for _, n := range []string{"a.txt", "a.jpg", "folder"} {
		if IsArchive(n) {
			t.Errorf("%s should NOT be archive", n)
		}
	}
}

func TestIsImage(t *testing.T) {
	for _, n := range []string{"a.jpg", "a.JPEG", "b.png", "c.webp", "d.gif", "e.bmp"} {
		if !IsImage(n) {
			t.Errorf("%s should be image", n)
		}
	}
	if IsImage("a.txt") {
		t.Error("txt is not image")
	}
}

func TestIsJunk(t *testing.T) {
	if !IsJunk("__MACOSX/._x.jpg") {
		t.Error("__MACOSX is junk")
	}
	if !IsJunk(".hidden.jpg") {
		t.Error("dotfile is junk")
	}
	if IsJunk("001.jpg") {
		t.Error("normal file is not junk")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/archive/...`
Expected: FAIL (undefined symbols).

- [ ] **Step 3: Implement natsort and formats**

Create `internal/archive/natsort.go`:

```go
package archive

import (
	"sort"
	"strings"
)

// NatLess reports whether a sorts before b using natural ordering:
// embedded digit runs are compared by numeric value, other runs lexically.
func NatLess(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		ai, bi := a[i], b[j]
		if isDigit(ai) && isDigit(bi) {
			as, ae := digitRun(a, i)
			bs, be := digitRun(b, j)
			an := strings.TrimLeft(a[as:ae], "0")
			bn := strings.TrimLeft(b[bs:be], "0")
			if len(an) != len(bn) {
				return len(an) < len(bn)
			}
			if an != bn {
				return an < bn
			}
			i, j = ae, be
			continue
		}
		if ai != bi {
			return ai < bi
		}
		i++
		j++
	}
	return len(a)-i < len(b)-j
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func digitRun(s string, start int) (int, int) {
	end := start
	for end < len(s) && isDigit(s[end]) {
		end++
	}
	return start, end
}

// SortNatural sorts names in place using NatLess.
func SortNatural(names []string) {
	sort.Slice(names, func(i, j int) bool { return NatLess(names[i], names[j]) })
}
```

Create `internal/archive/formats.go`:

```go
package archive

import (
	"path"
	"strings"
)

var archiveExts = map[string]bool{
	".zip": true, ".cbz": true,
	".rar": true, ".cbr": true,
	".7z": true, ".cb7": true,
}

var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true,
	".gif": true, ".webp": true, ".bmp": true,
}

func ext(name string) string { return strings.ToLower(path.Ext(name)) }

// IsArchive reports whether name has a supported comic archive extension.
func IsArchive(name string) bool { return archiveExts[ext(name)] }

// IsImage reports whether name has a supported image extension.
func IsImage(name string) bool { return imageExts[ext(name)] }

// IsJunk reports whether an in-archive entry should be ignored.
func IsJunk(name string) bool {
	if strings.Contains(name, "__MACOSX") {
		return true
	}
	base := path.Base(name)
	return strings.HasPrefix(base, ".")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/archive/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/archive/natsort.go internal/archive/natsort_test.go internal/archive/formats.go internal/archive/formats_test.go
git commit -m "feat: add archive natural sort and format classification"
```

---

## Task 7: Archive reader (list + open image entries)

**Files:**
- Create: `internal/archive/archive.go`
- Test: `internal/archive/archive_test.go`

**Interfaces:**
- Consumes: `IsImage`, `IsJunk`, `SortNatural` from Task 6.
- Produces:
  - `archive.Reader` interface: `List() []string` (natural-sorted image entry names), `Open(name string) (io.ReadCloser, error)`, `Close() error`.
  - `archive.Open(ctx context.Context, archivePath, cacheDir string) (Reader, error)` — for `.zip/.cbz` returns a random-access zip reader (ignores `cacheDir`); for `.rar/.cbr/.7z/.cb7` extracts into `cacheDir` on first call and serves from disk.
  - `archive.FirstImage(ctx context.Context, archivePath, cacheDir string) (io.ReadCloser, string, int, error)` — convenience: opens, returns the first image's reader, its name, and the total image count. Returns error if no images.

- [ ] **Step 1: Write the failing test (zip path is fully unit-tested)**

Create `internal/archive/archive_test.go`:

```go
package archive

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// makeZip writes a .zip at dir/name.zip containing the given files.
func makeZip(t *testing.T, dir, name string, files map[string]string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for n, body := range files {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, body)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestZipListSortedAndFiltered(t *testing.T) {
	dir := t.TempDir()
	zp := makeZip(t, dir, "c.zip", map[string]string{
		"10.jpg":            "a",
		"2.jpg":             "b",
		"1.jpg":             "c",
		"notes.txt":         "ignore",
		"__MACOSX/._1.jpg":  "junk",
	})
	r, err := Open(context.Background(), zp, "")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got := r.List()
	want := []string{"1.jpg", "2.jpg", "10.jpg"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestZipOpenEntry(t *testing.T) {
	dir := t.TempDir()
	zp := makeZip(t, dir, "c.zip", map[string]string{"1.jpg": "hello"})
	r, err := Open(context.Background(), zp, "")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	rc, err := r.Open("1.jpg")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	if string(b) != "hello" {
		t.Fatalf("got %q", b)
	}
}

func TestFirstImage(t *testing.T) {
	dir := t.TempDir()
	zp := makeZip(t, dir, "c.zip", map[string]string{"2.jpg": "two", "1.jpg": "one"})
	rc, name, count, err := FirstImage(context.Background(), zp, "")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if name != "1.jpg" || count != 2 {
		t.Fatalf("name=%q count=%d", name, count)
	}
	b, _ := io.ReadAll(rc)
	if string(b) != "one" {
		t.Fatalf("got %q", b)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/archive/ -run 'Zip|FirstImage' -v`
Expected: FAIL (undefined `Open`/`FirstImage`).

- [ ] **Step 3: Implement archive reader**

Create `internal/archive/archive.go`:

```go
package archive

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mholt/archiver/v4"
)

type Reader interface {
	List() []string
	Open(name string) (io.ReadCloser, error)
	Close() error
}

// Open returns a Reader for the archive at archivePath. zip/cbz use random
// access; rar/cbr/7z/cb7 are extracted into cacheDir on first use.
func Open(ctx context.Context, archivePath, cacheDir string) (Reader, error) {
	switch ext(archivePath) {
	case ".zip", ".cbz":
		return openZip(archivePath)
	default:
		return openExtracted(ctx, archivePath, cacheDir)
	}
}

// FirstImage opens the archive and returns the first image entry reader,
// its name, and the total image count.
func FirstImage(ctx context.Context, archivePath, cacheDir string) (io.ReadCloser, string, int, error) {
	r, err := Open(ctx, archivePath, cacheDir)
	if err != nil {
		return nil, "", 0, err
	}
	names := r.List()
	if len(names) == 0 {
		r.Close()
		return nil, "", 0, fmt.Errorf("archive: %s has no images", archivePath)
	}
	rc, err := r.Open(names[0])
	if err != nil {
		r.Close()
		return nil, "", 0, err
	}
	// Wrap so closing the page reader also closes the archive.
	return &closer{rc: rc, also: r}, names[0], len(names), nil
}

type closer struct {
	rc   io.ReadCloser
	also Reader
}

func (c *closer) Read(p []byte) (int, error) { return c.rc.Read(p) }
func (c *closer) Close() error {
	err := c.rc.Close()
	c.also.Close()
	return err
}

// ---- zip (random access) ----

type zipReader struct {
	zc    *zip.ReadCloser
	files map[string]*zip.File
	names []string
}

func openZip(path string) (Reader, error) {
	zc, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("archive: open zip %s: %w", path, err)
	}
	zr := &zipReader{zc: zc, files: map[string]*zip.File{}}
	for _, f := range zc.File {
		if f.FileInfo().IsDir() || IsJunk(f.Name) || !IsImage(f.Name) {
			continue
		}
		zr.files[f.Name] = f
		zr.names = append(zr.names, f.Name)
	}
	SortNatural(zr.names)
	return zr, nil
}

func (z *zipReader) List() []string { return z.names }

func (z *zipReader) Open(name string) (io.ReadCloser, error) {
	f, ok := z.files[name]
	if !ok {
		return nil, fmt.Errorf("archive: entry %q not found", name)
	}
	return f.Open()
}

func (z *zipReader) Close() error { return z.zc.Close() }

// ---- extracted (rar / 7z) ----

type dirReader struct {
	dir   string
	names []string
}

func openExtracted(ctx context.Context, archivePath, cacheDir string) (Reader, error) {
	if cacheDir == "" {
		return nil, errors.New("archive: cacheDir required for rar/7z")
	}
	if err := ensureExtracted(ctx, archivePath, cacheDir); err != nil {
		return nil, err
	}
	dr := &dirReader{dir: cacheDir}
	walkErr := filepath.WalkDir(cacheDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(cacheDir, p)
		if IsJunk(rel) || !IsImage(rel) {
			return nil
		}
		dr.names = append(dr.names, filepath.ToSlash(rel))
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("archive: walk cache %s: %w", cacheDir, walkErr)
	}
	SortNatural(dr.names)
	return dr, nil
}

func (d *dirReader) List() []string { return d.names }

func (d *dirReader) Open(name string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(d.dir, filepath.FromSlash(name)))
}

func (d *dirReader) Close() error { return nil }

// ensureExtracted extracts archivePath into cacheDir once; if cacheDir
// already contains files it is treated as already extracted.
func ensureExtracted(ctx context.Context, archivePath, cacheDir string) error {
	if entries, err := os.ReadDir(cacheDir); err == nil && len(entries) > 0 {
		return nil
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	src, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("archive: open %s: %w", archivePath, err)
	}
	defer src.Close()
	format, reader, err := archiver.Identify(filepath.Base(archivePath), src)
	if err != nil {
		return fmt.Errorf("archive: identify %s: %w", archivePath, err)
	}
	ex, ok := format.(archiver.Extractor)
	if !ok {
		return fmt.Errorf("archive: %s not extractable", archivePath)
	}
	return ex.Extract(ctx, reader, nil, func(ctx context.Context, f archiver.File) error {
		if f.IsDir() || IsJunk(f.NameInArchive) || !IsImage(f.NameInArchive) {
			return nil
		}
		dst := filepath.Join(cacheDir, filepath.FromSlash(f.NameInArchive))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		out, err := os.Create(dst)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, rc)
		return err
	})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/archive/...`
Expected: PASS. (zip path fully covered; rar/7z `openExtracted` shares the same `Reader` contract and is exercised end-to-end by the scanner in manual testing — note this gap in the test plan rather than faking rar/7z fixtures, which Go cannot write.)

- [ ] **Step 5: Commit**

```bash
git add internal/archive/archive.go internal/archive/archive_test.go
git commit -m "feat: add archive reader with zip random access and rar/7z extract cache"
```

---

## Task 8: Thumbnail generation

**Files:**
- Create: `internal/thumb/thumb.go`
- Test: `internal/thumb/thumb_test.go`

**Interfaces:**
- Produces:
  - `thumb.Generate(src io.Reader, width int) ([]byte, error)` — decodes an image (jpeg/png/gif/webp/bmp via registered decoders), scales proportionally to `width` px wide using CatmullRom, and returns JPEG bytes (quality 85). Returns error on undecodable input.

- [ ] **Step 1: Write the failing test**

Create `internal/thumb/thumb_test.go`:

```go
package thumb

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func samplePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), 0, 0, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestGenerateScalesWidth(t *testing.T) {
	src := samplePNG(t, 800, 1200)
	out, err := Generate(bytes.NewReader(src), 400)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("output not jpeg: %v", err)
	}
	if img.Bounds().Dx() != 400 {
		t.Errorf("width = %d, want 400", img.Bounds().Dx())
	}
	if img.Bounds().Dy() != 600 {
		t.Errorf("height = %d, want 600 (proportional)", img.Bounds().Dy())
	}
}

func TestGenerateRejectsGarbage(t *testing.T) {
	if _, err := Generate(bytes.NewReader([]byte("not an image")), 400); err == nil {
		t.Fatal("expected decode error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/thumb/...`
Expected: FAIL (undefined `Generate`).

- [ ] **Step 3: Implement thumb.Generate**

Create `internal/thumb/thumb.go`:

```go
package thumb

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"io"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

func init() {
	// bmp registers itself via blank import below; reference to silence unused.
	_ = bmp.Decode
}

// Generate decodes src, scales it to width px wide (preserving aspect ratio),
// and returns JPEG-encoded bytes.
func Generate(src io.Reader, width int) ([]byte, error) {
	if width <= 0 {
		return nil, fmt.Errorf("thumb: width must be > 0")
	}
	img, _, err := image.Decode(src)
	if err != nil {
		return nil, fmt.Errorf("thumb: decode: %w", err)
	}
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return nil, fmt.Errorf("thumb: empty image")
	}
	height := int(float64(width) * float64(b.Dy()) / float64(b.Dx()))
	if height < 1 {
		height = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, fmt.Errorf("thumb: encode: %w", err)
	}
	return buf.Bytes(), nil
}
```

> Note: `golang.org/x/image/bmp` does not self-register with `image.Decode` on blank import in all versions; the `init` referencing `bmp.Decode` keeps the import and documents intent. If `image.Decode` returns "unknown format" for BMP during manual testing, that is acceptable — BMP comics are rare; jpeg/png/gif/webp are the priority and are covered.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/thumb/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/thumb
git commit -m "feat: add thumbnail generation"
```

---

## Task 9: Library scanner

**Files:**
- Create: `internal/library/scanner.go`
- Test: `internal/library/scanner_test.go`

**Interfaces:**
- Consumes: `store.NodeRepo`, `store.Node`, `store.NodeType`, `store.CoverReady/CoverFailed`, `archive.IsArchive`, `archive.FirstImage`, `thumb.Generate`.
- Produces (`NewScanner(repo *store.NodeRepo, root, dataDir string, thumbWidth int) *Scanner`):
  - `(*Scanner) Scan(ctx context.Context) error` — idempotent full sync of `root` into the DB tree; safe to call repeatedly; serialized internally with a mutex.
  - `(*Scanner) thumbPath(id int64) string` → `<dataDir>/thumbs/<id>.jpg`.
  - `(*Scanner) cacheDir(id int64) string` → `<dataDir>/cache/<id>`.

- [ ] **Step 1: Write the failing test**

Create `internal/library/scanner_test.go`:

```go
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
```

Also add a tiny helper file so the test compiles. Create `internal/library/util_test.go`:

```go
package library

import "strconv"

func strconvFormat(i int64) string { return strconv.FormatInt(i, 10) }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/library/...`
Expected: FAIL (undefined `NewScanner`).

- [ ] **Step 3: Implement scanner**

Create `internal/library/scanner.go`:

```go
package library

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"Tefnut/internal/archive"
	"Tefnut/internal/store"
	"Tefnut/internal/thumb"
)

type Scanner struct {
	repo       *store.NodeRepo
	root       string
	dataDir    string
	thumbWidth int
	mu         sync.Mutex
}

func NewScanner(repo *store.NodeRepo, root, dataDir string, thumbWidth int) *Scanner {
	return &Scanner{repo: repo, root: root, dataDir: dataDir, thumbWidth: thumbWidth}
}

func (s *Scanner) thumbPath(id int64) string {
	return filepath.Join(s.dataDir, "thumbs", strconv.FormatInt(id, 10)+".jpg")
}

func (s *Scanner) cacheDir(id int64) string {
	return filepath.Join(s.dataDir, "cache", strconv.FormatInt(id, 10))
}

// Scan performs a full idempotent sync of root into the DB tree.
func (s *Scanner) Scan(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	abs, err := filepath.Abs(s.root)
	if err != nil {
		return fmt.Errorf("scanner: abs root: %w", err)
	}
	return s.scanDir(ctx, abs, 0)
}

func (s *Scanner) scanDir(ctx context.Context, dir string, parentID int64) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("scanner: read dir %s: %w", dir, err)
	}

	existing, err := s.repo.ListChildren(ctx, parentID)
	if err != nil {
		return err
	}
	byPath := map[string]*store.Node{}
	for _, n := range existing {
		byPath[n.Path] = n
	}

	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		isDir := e.IsDir()
		if !isDir && !archive.IsArchive(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			log.Printf("scanner: stat %s: %v", p, err)
			continue
		}

		node, seen := byPath[p]
		if seen {
			delete(byPath, p)
		} else {
			typ := store.NodeComic
			if isDir {
				typ = store.NodeDir
			}
			node, err = s.repo.Create(ctx, &store.Node{
				ParentID: parentID, Name: e.Name(), Path: p, Type: typ,
				Size: info.Size(), MTime: info.ModTime().Unix(),
			})
			if err != nil {
				log.Printf("scanner: create %s: %v", p, err)
				continue
			}
		}

		if isDir {
			if err := s.scanDir(ctx, p, node.ID); err != nil {
				log.Printf("scanner: recurse %s: %v", p, err)
			}
			continue
		}

		// Comic: (re)build cover + page count if new or changed.
		if !seen || node.Size != info.Size() || node.MTime != info.ModTime().Unix() {
			s.buildComic(ctx, node, info.Size(), info.ModTime().Unix())
		}
	}

	// Anything still in byPath no longer exists on disk: remove subtree.
	for _, n := range byPath {
		s.removeNode(ctx, n)
	}
	return nil
}

func (s *Scanner) buildComic(ctx context.Context, node *store.Node, size, mtime int64) {
	// Reset any stale extract cache so page count reflects current file.
	os.RemoveAll(s.cacheDir(node.ID))

	rc, _, count, err := archive.FirstImage(ctx, node.Path, s.cacheDir(node.ID))
	if err != nil {
		log.Printf("scanner: first image %s: %v", node.Path, err)
		s.repo.UpdateFileAttrs(ctx, node.ID, size, mtime, 0, store.CoverFailed)
		return
	}
	coverStatus := store.CoverReady
	if err := s.writeThumb(node.ID, rc); err != nil {
		log.Printf("scanner: thumb %s: %v", node.Path, err)
		coverStatus = store.CoverFailed
	}
	rc.Close()
	if err := s.repo.UpdateFileAttrs(ctx, node.ID, size, mtime, count, coverStatus); err != nil {
		log.Printf("scanner: update attrs %s: %v", node.Path, err)
	}
}

func (s *Scanner) writeThumb(id int64, rc interface{ Read([]byte) (int, error) }) error {
	data, err := thumb.Generate(readerOnly{rc}, s.thumbWidth)
	if err != nil {
		return err
	}
	dst := s.thumbPath(id)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

type readerOnly struct{ r interface{ Read([]byte) (int, error) } }

func (r readerOnly) Read(p []byte) (int, error) { return r.r.Read(p) }

func (s *Scanner) removeNode(ctx context.Context, n *store.Node) {
	if n.Type == store.NodeDir {
		kids, err := s.repo.ListChildren(ctx, n.ID)
		if err == nil {
			for _, k := range kids {
				s.removeNode(ctx, k)
			}
		}
	}
	os.Remove(s.thumbPath(n.ID))
	os.RemoveAll(s.cacheDir(n.ID))
	if err := s.repo.Delete(ctx, n.ID); err != nil {
		log.Printf("scanner: delete node %d: %v", n.ID, err)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/library/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/library
git commit -m "feat: add library scanner with cover and page-count generation"
```

---

## Task 10: Server scaffolding — embed, response helpers, server struct

**Files:**
- Create: `internal/server/web/web.go`, `internal/server/response.go`, `internal/server/server.go`
- Create stub assets: `internal/server/web/templates/layout.html`, `browse.html`, `reader.html`, `tags.html`, `internal/server/web/static/css/app.css`, `static/js/browse.js`, `static/js/reader.js`, `static/js/tags.js`, `static/img/placeholder.svg`
- Test: `internal/server/server_test.go` (health route)

**Interfaces:**
- Produces:
  - `web.Templates *template.Template` (parsed) and `web.Static fs.FS` (sub-filesystem rooted at `static`).
  - `server.ok(c echo.Context, data any) error` → `{code:0,message:"success",data:...}`.
  - `server.fail(c echo.Context, status int, err error) error` → `{code:-1,message:err}` with HTTP status.
  - `server.Server` struct holding `nodes *store.NodeRepo`, `tags *store.TagRepo`, `progress *store.ProgressRepo`, `dataDir string`, `thumbWidth int`.
  - `server.NewServer(nodes, tags, progress, dataDir string, thumbWidth int) *Server`.
  - `(*Server) Register(e *echo.Echo)` — wires static assets + routes (routes filled in later tasks; for now registers `/healthz` and static).

- [ ] **Step 1: Create embed assets (stubs that later tasks flesh out)**

Create `internal/server/web/templates/layout.html`:

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
<header class="topbar"><a href="/" class="brand">Tefnut</a><a href="/tags" class="navlink">标签管理</a></header>
<main>{{block "content" .}}{{end}}</main>
{{block "scripts" .}}{{end}}
</body>
</html>{{end}}
```

Create `internal/server/web/templates/browse.html`:

```html
{{define "title"}}{{.Title}} · Tefnut{{end}}
{{define "content"}}
<section class="toolbar">
  <form method="get" action="" class="filters">
    <input type="hidden" name="parent" value="{{.ParentID}}">
    <input type="search" name="q" placeholder="按名称搜索" value="{{.Q}}">
    <select name="tag">
      <option value="">全部标签</option>
      {{range .Tags}}<option value="{{.ID}}" {{if eq .ID $.TagID}}selected{{end}}>{{.Name}} ({{.Count}})</option>{{end}}
    </select>
    <select name="minRating">
      {{range .Ratings}}<option value="{{.}}" {{if eq . $.MinRating}}selected{{end}}>{{if eq . 0}}任意评分{{else}}{{.}}★+{{end}}</option>{{end}}
    </select>
    <button type="submit">筛选</button>
  </form>
</section>
{{if .Breadcrumb}}<nav class="crumbs">{{range .Breadcrumb}}<a href="/folder/{{.ID}}">{{.Name}}</a> / {{end}}</nav>{{end}}
<ul class="grid" id="grid">
  {{range .Items}}
  <li class="card {{if eq .Type 2}}dir{{end}}">
    {{if eq .Type 2}}
      <a href="/folder/{{.ID}}"><div class="cover folder">📁</div><div class="name">{{.Name}}</div></a>
    {{else}}
      <a href="/read/{{.ID}}"><img class="cover" loading="lazy" src="/api/comics/{{.ID}}/cover" alt="{{.Name}}"><div class="name">{{.Name}}</div><div class="stars">{{.Rating}}★</div></a>
    {{end}}
  </li>
  {{end}}
</ul>
{{if not .Items}}<p class="empty">没有内容</p>{{end}}
{{end}}
{{define "scripts"}}{{end}}
```

Create `internal/server/web/templates/reader.html`:

```html
{{define "title"}}{{.Name}} · 阅读{{end}}
{{define "content"}}
<div id="reader" data-id="{{.ID}}" data-pages="{{.PageCount}}" data-start="{{.LastPage}}">
  <div class="reader-stage">
    <button class="nav prev" id="prev">‹</button>
    <img id="page" alt="page">
    <button class="nav next" id="next">›</button>
  </div>
  <div class="reader-bar">
    <span id="counter"></span>
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

Create `internal/server/web/templates/tags.html`:

```html
{{define "title"}}标签管理 · Tefnut{{end}}
{{define "content"}}
<h1>标签管理</h1>
<form id="newtagform"><input id="newtagname" placeholder="新建标签"><button>新建</button></form>
<ul id="taglist">
  {{range .Tags}}<li data-id="{{.ID}}"><span class="cnt">{{.Count}}</span><input class="tname" value="{{.Name}}"><button class="rename">重命名</button><button class="del">删除</button></li>{{end}}
</ul>
{{end}}
{{define "scripts"}}<script src="/static/js/tags.js"></script>{{end}}
```

Create `internal/server/web/static/css/app.css`:

```css
* { box-sizing: border-box; }
body { margin: 0; font-family: system-ui, sans-serif; background: #14161a; color: #e6e6e6; }
.topbar { display: flex; gap: 16px; align-items: center; padding: 12px 20px; background: #1c1f26; }
.brand { font-weight: 700; color: #fff; text-decoration: none; }
.navlink, .crumbs a { color: #8ab4f8; text-decoration: none; }
main { padding: 20px; }
.toolbar .filters { display: flex; gap: 8px; flex-wrap: wrap; margin-bottom: 16px; }
.toolbar input, .toolbar select, .toolbar button { padding: 6px 10px; background: #222; color: #eee; border: 1px solid #333; border-radius: 6px; }
.grid { list-style: none; padding: 0; margin: 0; display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: 16px; }
.card a { color: inherit; text-decoration: none; display: block; }
.cover { width: 100%; aspect-ratio: 2/3; object-fit: cover; background: #222; border-radius: 8px; display: flex; align-items: center; justify-content: center; font-size: 48px; }
.name { margin-top: 6px; font-size: 13px; word-break: break-all; }
.stars { font-size: 12px; color: #f5c518; }
.empty { color: #888; }
.reader-stage { position: relative; display: flex; justify-content: center; align-items: center; min-height: 70vh; }
#page { max-height: 85vh; max-width: 100%; }
.nav { position: absolute; top: 0; bottom: 0; width: 25%; background: transparent; border: 0; color: transparent; font-size: 40px; cursor: pointer; }
.nav.prev { left: 0; } .nav.next { right: 0; }
.reader-bar { display: flex; gap: 16px; flex-wrap: wrap; align-items: center; padding: 10px; background: #1c1f26; }
.reader-bar input, .reader-bar select { background: #222; color: #eee; border: 1px solid #333; border-radius: 6px; padding: 4px 8px; }
.tags .tag { display: inline-flex; gap: 4px; background: #2a2f3a; padding: 2px 8px; border-radius: 12px; margin: 2px; }
.tags .tag button { background: none; border: 0; color: #f88; cursor: pointer; }
#taglist { list-style: none; padding: 0; } #taglist li { display: flex; gap: 8px; align-items: center; margin: 6px 0; }
#taglist .cnt { color: #888; min-width: 28px; }
```

Create `internal/server/web/static/js/browse.js` with a single line `// browse uses server-rendered forms; no JS required for v1`.

Create `internal/server/web/static/js/reader.js` and `static/js/tags.js` as empty stubs `// filled in later tasks` (real content added in Tasks 12/14/16). Create `static/img/placeholder.svg`:

```svg
<svg xmlns="http://www.w3.org/2000/svg" width="300" height="450"><rect width="100%" height="100%" fill="#222"/><text x="50%" y="50%" fill="#666" font-size="20" text-anchor="middle" dominant-baseline="middle">No Cover</text></svg>
```

- [ ] **Step 2: Write the failing health test**

Create `internal/server/server_test.go`:

```go
package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/server/...`
Expected: FAIL (undefined `NewServer`).

- [ ] **Step 4: Implement web embed, response helpers, server**

Create `internal/server/web/web.go`:

```go
package web

import (
	"embed"
	"html/template"
	"io/fs"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

// Templates holds all parsed templates.
var Templates = template.Must(template.ParseFS(templatesFS, "templates/*.html"))

// Static is the embedded static asset filesystem rooted at "static".
var Static fs.FS = mustSub(staticFS, "static")

func mustSub(f embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
```

Create `internal/server/response.go`:

```go
package server

import "github.com/labstack/echo/v4"

func ok(c echo.Context, data any) error {
	return c.JSON(200, map[string]any{"code": 0, "message": "success", "data": data})
}

func fail(c echo.Context, status int, err error) error {
	return c.JSON(status, map[string]any{"code": -1, "message": err.Error()})
}
```

Create `internal/server/server.go`:

```go
package server

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"Tefnut/internal/server/web"
	"Tefnut/internal/store"
)

type Server struct {
	nodes      *store.NodeRepo
	tags       *store.TagRepo
	progress   *store.ProgressRepo
	dataDir    string
	thumbWidth int
}

func NewServer(nodes *store.NodeRepo, tags *store.TagRepo, progress *store.ProgressRepo, dataDir string, thumbWidth int) *Server {
	return &Server{nodes: nodes, tags: tags, progress: progress, dataDir: dataDir, thumbWidth: thumbWidth}
}

// Register wires routes and static assets onto e.
func (s *Server) Register(e *echo.Echo) {
	e.GET("/healthz", func(c echo.Context) error { return c.String(200, "ok") })
	e.StaticFS("/static", web.Static)

	// Rendered pages (implemented in pages.go).
	e.GET("/", s.pageBrowse)
	e.GET("/folder/:id", s.pageBrowse)
	e.GET("/read/:id", s.pageReader)
	e.GET("/tags", s.pageTags)

	// JSON / binary API.
	api := e.Group("/api")
	api.GET("/nodes", s.apiNodes)
	api.GET("/comics/:id", s.apiComicDetail)
	api.GET("/comics/:id/cover", s.apiCover)
	api.GET("/comics/:id/pages/:n", s.apiPage)
	api.PATCH("/comics/:id", s.apiUpdateMeta)
	api.POST("/comics/:id/tags", s.apiAddTag)
	api.DELETE("/comics/:id/tags/:tagId", s.apiRemoveTag)
	api.PUT("/comics/:id/progress", s.apiSetProgress)
	api.GET("/tags", s.apiListTags)
	api.POST("/tags", s.apiCreateTag)
	api.PATCH("/tags/:id", s.apiRenameTag)
	api.DELETE("/tags/:id", s.apiDeleteTag)

	_ = http.StatusOK
}
```

> The handler methods referenced above (`s.pageBrowse`, `s.apiNodes`, etc.) are implemented in Tasks 11–16. To keep this task compiling on its own, add a temporary `internal/server/stubs.go` returning `c.NoContent(501)` for each, then delete each stub as its real implementation lands.

Create `internal/server/stubs.go` (temporary):

```go
package server

import "github.com/labstack/echo/v4"

func (s *Server) pageBrowse(c echo.Context) error    { return c.NoContent(501) }
func (s *Server) pageReader(c echo.Context) error    { return c.NoContent(501) }
func (s *Server) pageTags(c echo.Context) error      { return c.NoContent(501) }
func (s *Server) apiNodes(c echo.Context) error      { return c.NoContent(501) }
func (s *Server) apiComicDetail(c echo.Context) error { return c.NoContent(501) }
func (s *Server) apiCover(c echo.Context) error      { return c.NoContent(501) }
func (s *Server) apiPage(c echo.Context) error       { return c.NoContent(501) }
func (s *Server) apiUpdateMeta(c echo.Context) error { return c.NoContent(501) }
func (s *Server) apiAddTag(c echo.Context) error     { return c.NoContent(501) }
func (s *Server) apiRemoveTag(c echo.Context) error  { return c.NoContent(501) }
func (s *Server) apiSetProgress(c echo.Context) error { return c.NoContent(501) }
func (s *Server) apiListTags(c echo.Context) error   { return c.NoContent(501) }
func (s *Server) apiCreateTag(c echo.Context) error  { return c.NoContent(501) }
func (s *Server) apiRenameTag(c echo.Context) error  { return c.NoContent(501) }
func (s *Server) apiDeleteTag(c echo.Context) error  { return c.NoContent(501) }
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/server/...`
Expected: PASS (healthz returns 200).

- [ ] **Step 6: Commit**

```bash
git add internal/server
git commit -m "feat: add server scaffolding with embedded assets and routes"
```

---

## Task 11: Node list, comic detail, cover, and page serving

**Files:**
- Create: `internal/server/api_nodes.go`
- Modify: `internal/server/stubs.go` (remove the 5 stubs now implemented)
- Test: extend `internal/server/server_test.go`

**Interfaces:**
- Consumes: `store.NodeRepo`, `store.ProgressRepo`, `store.TagRepo`, `archive.Open`, `web.Static` placeholder.
- Produces these handlers + helpers:
  - `s.apiNodes` — `GET /api/nodes?parent=&q=&tag=&minRating=`. When any of `q/tag/minRating` is set → `nodes.Search`; else → `nodes.ListChildren(parent)`. Returns `[]nodeDTO`.
  - `s.apiComicDetail` — `GET /api/comics/:id` → `{id,name,author,rating,pageCount,lastPage,tags:[{id,name}]}`.
  - `s.apiCover` — streams `<dataDir>/thumbs/<id>.jpg`, or 302-redirects to `/static/img/placeholder.svg` when absent.
  - `s.apiPage` — `GET /api/comics/:id/pages/:n` streams the nth image (0-based) from the archive.
  - helper `parseID(c, "id") (int64, error)`; `nodeDTO` struct with json tags `id,name,type,pageCount,rating,coverStatus`.

- [ ] **Step 1: Write failing tests**

Append to `internal/server/server_test.go`:

```go
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

func itoa(i int64) string { return strconv.FormatInt(i, 10) }
```

Add the imports this test now needs at the top of `server_test.go`: `archive/zip`, `context`, `image`, `image/png`, `os`, `strconv`, `strings`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -run 'ApiNodes|ApiPage|ApiCover' -v`
Expected: FAIL (stubs return 501).

- [ ] **Step 3: Implement api_nodes.go and remove the now-real stubs**

Create `internal/server/api_nodes.go`:

```go
package server

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/labstack/echo/v4"

	"Tefnut/internal/archive"
	"Tefnut/internal/store"
)

type nodeDTO struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Type        int    `json:"type"`
	PageCount   int    `json:"pageCount"`
	Rating      int    `json:"rating"`
	CoverStatus int    `json:"coverStatus"`
}

func toNodeDTO(n *store.Node) nodeDTO {
	return nodeDTO{ID: n.ID, Name: n.Name, Type: int(n.Type),
		PageCount: n.PageCount, Rating: n.Rating, CoverStatus: n.CoverStatus}
}

func parseID(c echo.Context, name string) (int64, error) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id < 0 {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return id, nil
}

func (s *Server) apiNodes(c echo.Context) error {
	ctx := c.Request().Context()
	q := c.QueryParam("q")
	tagID, _ := strconv.ParseInt(c.QueryParam("tag"), 10, 64)
	minRating, _ := strconv.Atoi(c.QueryParam("minRating"))

	var nodes []*store.Node
	var err error
	if q != "" || tagID > 0 || minRating > 0 {
		nodes, err = s.nodes.Search(ctx, q, tagID, minRating)
	} else {
		parent, _ := strconv.ParseInt(c.QueryParam("parent"), 10, 64)
		nodes, err = s.nodes.ListChildren(ctx, parent)
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	out := make([]nodeDTO, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, toNodeDTO(n))
	}
	return ok(c, out)
}

type comicDetailDTO struct {
	ID        int64       `json:"id"`
	Name      string      `json:"name"`
	Author    string      `json:"author"`
	Rating    int         `json:"rating"`
	PageCount int         `json:"pageCount"`
	LastPage  int         `json:"lastPage"`
	Tags      []*store.Tag `json:"tags"`
}

func (s *Server) apiComicDetail(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parseID(c, "id")
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	n, err := s.nodes.Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return fail(c, http.StatusNotFound, err)
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	last, _ := s.progress.Get(ctx, id)
	tags, _ := s.tags.ListForNode(ctx, id)
	return ok(c, comicDetailDTO{
		ID: n.ID, Name: n.Name, Author: n.Author, Rating: n.Rating,
		PageCount: n.PageCount, LastPage: last, Tags: tags,
	})
}

func (s *Server) apiCover(c echo.Context) error {
	id, err := parseID(c, "id")
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	p := filepath.Join(s.dataDir, "thumbs", strconv.FormatInt(id, 10)+".jpg")
	if _, statErr := filepathStat(p); statErr != nil {
		return c.Redirect(http.StatusFound, "/static/img/placeholder.svg")
	}
	return c.File(p)
}

func (s *Server) apiPage(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parseID(c, "id")
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	n, err := s.nodes.Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return fail(c, http.StatusNotFound, err)
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	pageNum, err := strconv.Atoi(c.Param("n"))
	if err != nil || pageNum < 0 {
		return fail(c, http.StatusBadRequest, fmt.Errorf("invalid page"))
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
	return c.Stream(http.StatusOK, contentType(names[pageNum]), rc)
}
```

Add a tiny file `internal/server/fsutil.go` (keeps `os` import isolated and testable):

```go
package server

import (
	"os"
	"path"
	"strings"
)

func filepathStat(p string) (os.FileInfo, error) { return os.Stat(p) }

func contentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	default:
		return "image/jpeg"
	}
}
```

Now delete these five methods from `internal/server/stubs.go`: `apiNodes`, `apiComicDetail`, `apiCover`, `apiPage` (keep the rest). 

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server
git commit -m "feat: add node list, comic detail, cover and page serving"
```

---

## Task 12: Rendered pages — browse + reader

**Files:**
- Create: `internal/server/pages.go`
- Modify: `internal/server/stubs.go` (remove `pageBrowse`, `pageReader`)
- Replace: `internal/server/web/static/js/reader.js`
- Test: extend `internal/server/server_test.go`

**Interfaces:**
- Consumes: `web.Templates`, `store.NodeRepo`, `store.TagRepo`, `store.ProgressRepo`.
- Produces:
  - `s.pageBrowse` — renders `browse.html` for `/` (parent 0) and `/folder/:id`. Builds `Items` from `ListChildren` or `Search` (same rule as API), `Tags` for the filter dropdown, `Ratings = [0,1,2,3,4,5]`, `Breadcrumb` of ancestors, `Title`.
  - `s.pageReader` — renders `reader.html` with `ID,Name,Author,Rating,PageCount,LastPage,Ratings`.
  - helper `render(c, name string, data any) error` that executes the `layout` template after the page template's blocks are defined (uses a per-request cloned template set).

- [ ] **Step 1: Write failing tests**

Append to `internal/server/server_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -run 'PageBrowse|PageReader' -v`
Expected: FAIL (stubs return 501).

- [ ] **Step 3: Implement pages.go**

Create `internal/server/pages.go`:

```go
package server

import (
	"bytes"
	"html/template"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"Tefnut/internal/server/web"
	"Tefnut/internal/store"
)

var ratingChoices = []int{0, 1, 2, 3, 4, 5}

type crumb struct {
	ID   int64
	Name string
}

type browseData struct {
	Title      string
	ParentID   int64
	Q          string
	TagID      int64
	MinRating  int
	Ratings    []int
	Tags       []*store.TagCount
	Items      []*store.Node
	Breadcrumb []crumb
}

type readerData struct {
	ID        int64
	Name      string
	Author    string
	Rating    int
	PageCount int
	LastPage  int
	Ratings   []int
}

// render clones the parsed template set, applies the page template, and
// writes the composed "layout" output.
func render(c echo.Context, page string, data any) error {
	t, err := web.Templates.Clone()
	if err != nil {
		return err
	}
	if _, err := t.ParseFS(templatesSub, "templates/"+page); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", data); err != nil {
		return err
	}
	return c.HTMLBlob(http.StatusOK, buf.Bytes())
}

func (s *Server) pageBrowse(c echo.Context) error {
	ctx := c.Request().Context()
	parent := int64(0)
	if c.Param("id") != "" {
		if id, err := parseID(c, "id"); err == nil {
			parent = id
		}
	}
	q := c.QueryParam("q")
	tagID, _ := strconv.ParseInt(c.QueryParam("tag"), 10, 64)
	minRating, _ := strconv.Atoi(c.QueryParam("minRating"))

	var items []*store.Node
	var err error
	title := "漫画库"
	if q != "" || tagID > 0 || minRating > 0 {
		items, err = s.nodes.Search(ctx, q, tagID, minRating)
		title = "搜索结果"
	} else {
		items, err = s.nodes.ListChildren(ctx, parent)
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	tags, _ := s.tags.List(ctx)

	data := browseData{
		Title: title, ParentID: parent, Q: q, TagID: tagID, MinRating: minRating,
		Ratings: ratingChoices, Tags: tags, Items: items,
		Breadcrumb: s.breadcrumb(ctx, parent),
	}
	return render(c, "browse.html", data)
}

func (s *Server) breadcrumb(ctx echoContext, parent int64) []crumb {
	var crumbs []crumb
	for id := parent; id != 0; {
		n, err := s.nodes.Get(ctx, id)
		if err != nil {
			break
		}
		crumbs = append([]crumb{{ID: n.ID, Name: n.Name}}, crumbs...)
		id = n.ParentID
	}
	return crumbs
}

func (s *Server) pageReader(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parseID(c, "id")
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	n, err := s.nodes.Get(ctx, id)
	if err != nil {
		return fail(c, http.StatusNotFound, err)
	}
	last, _ := s.progress.Get(ctx, id)
	return render(c, "reader.html", readerData{
		ID: n.ID, Name: n.Name, Author: n.Author, Rating: n.Rating,
		PageCount: n.PageCount, LastPage: last, Ratings: ratingChoices,
	})
}

var _ = template.HTMLEscapeString
```

The `breadcrumb` helper takes a minimal context interface. Add to `internal/server/pages.go` imports nothing extra; define the alias near the top of the file:

```go
type echoContext = interface {
	Done() <-chan struct{}
	Err() error
	Value(any) any
	Deadline() (deadlineUnsupported, bool)
}
```

> The above context alias is fragile. SIMPLER: change `breadcrumb` to accept `context.Context` and pass `c.Request().Context()`. Replace the `echoContext` alias and the `breadcrumb(ctx echoContext,...)` signature with `breadcrumb(ctx context.Context, parent int64)` and add `"context"` to imports. Remove the `deadlineUnsupported` placeholder entirely.

Also expose the embedded templates FS for `render`'s `ParseFS`. Add to `internal/server/web/web.go`:

```go
// FS exposes the embedded template files for per-request cloning.
var FS = templatesFS
```

and in `pages.go` add `templatesSub = web.FS` via:

```go
var templatesSub = web.FS
```

> Because `browse.html` and `reader.html` each `{{define}}` the same block names ("title","content","scripts"), parsing ALL page templates at once (as `web.Templates` does) means the LAST-parsed file wins those block names. That is why `render` clones the base set and re-parses ONLY the one page file needed, so its blocks override correctly. Ensure `layout.html` is the only file parsed into the base `web.Templates`. CHANGE `web.go`'s `Templates` to parse only the layout:
> ```go
> var Templates = template.Must(template.ParseFS(templatesFS, "templates/layout.html"))
> ```
> Then `render` re-parses the specific page file on the clone. This is the key correctness detail for templating.

- [ ] **Step 4: Replace reader.js with the working reader**

Replace `internal/server/web/static/js/reader.js`:

```js
const el = document.getElementById('reader');
const id = el.dataset.id;
const total = parseInt(el.dataset.pages, 10);
let cur = Math.min(parseInt(el.dataset.start, 10) || 0, Math.max(total - 1, 0));
const img = document.getElementById('page');
const counter = document.getElementById('counter');

function pageURL(n) { return `/api/comics/${id}/pages/${n}`; }

function preload(n) {
  if (n >= 0 && n < total) { const i = new Image(); i.src = pageURL(n); }
}

let saveTimer = null;
function saveProgress(n) {
  clearTimeout(saveTimer);
  saveTimer = setTimeout(() => {
    fetch(`/api/comics/${id}/progress`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ page: n })
    });
  }, 400);
}

function show(n) {
  if (n < 0 || n >= total) return;
  cur = n;
  img.src = pageURL(cur);
  counter.textContent = `${cur + 1} / ${total}`;
  preload(cur + 1);
  saveProgress(cur);
}

document.getElementById('next').onclick = () => show(cur + 1);
document.getElementById('prev').onclick = () => show(cur - 1);
document.addEventListener('keydown', (e) => {
  if (e.key === 'ArrowRight') show(cur + 1);
  if (e.key === 'ArrowLeft') show(cur - 1);
});

if (total > 0) show(cur); else counter.textContent = '无可显示页面';
```

- [ ] **Step 5: Remove the two now-real stubs and run tests**

Delete `pageBrowse` and `pageReader` from `internal/server/stubs.go`.

Run: `go test ./internal/server/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/server
git commit -m "feat: render browse and reader pages"
```

---

## Task 13: Metadata editing API (author, rating, tags on a comic)

**Files:**
- Create: `internal/server/api_meta.go`
- Modify: `internal/server/stubs.go` (remove `apiUpdateMeta`, `apiAddTag`, `apiRemoveTag`)
- Append reader.js meta wiring
- Test: extend `internal/server/server_test.go`

**Interfaces:**
- Consumes: `store.NodeRepo.UpdateMeta`, `store.TagRepo.Upsert/AddToNode/RemoveFromNode`.
- Produces:
  - `s.apiUpdateMeta` — `PATCH /api/comics/:id` body `{author?:string, rating?:int}`. Validates `rating` in 0..5 when present. Loads current node to preserve the unspecified field.
  - `s.apiAddTag` — `POST /api/comics/:id/tags` body `{name:string}`. Trims; rejects empty; upserts tag; links to node; returns the tag.
  - `s.apiRemoveTag` — `DELETE /api/comics/:id/tags/:tagId`.

- [ ] **Step 1: Write failing tests**

Append to `internal/server/server_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -run 'UpdateMeta|AddAndRemoveTag' -v`
Expected: FAIL (stubs 501).

- [ ] **Step 3: Implement api_meta.go**

Create `internal/server/api_meta.go`:

```go
package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"

	"Tefnut/internal/store"
)

type metaReq struct {
	Author *string `json:"author"`
	Rating *int    `json:"rating"`
}

func (s *Server) apiUpdateMeta(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parseID(c, "id")
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	var body metaReq
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	if body.Rating != nil && (*body.Rating < 0 || *body.Rating > 5) {
		return fail(c, http.StatusBadRequest, errors.New("rating must be 0..5"))
	}
	n, err := s.nodes.Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return fail(c, http.StatusNotFound, err)
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	author := n.Author
	rating := n.Rating
	if body.Author != nil {
		author = *body.Author
	}
	if body.Rating != nil {
		rating = *body.Rating
	}
	if err := s.nodes.UpdateMeta(ctx, id, author, rating); err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, map[string]any{"author": author, "rating": rating})
}

type addTagReq struct {
	Name string `json:"name"`
}

func (s *Server) apiAddTag(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parseID(c, "id")
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	var body addTagReq
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		return fail(c, http.StatusBadRequest, errors.New("tag name required"))
	}
	if len(name) > 64 {
		return fail(c, http.StatusBadRequest, errors.New("tag name too long"))
	}
	tag, err := s.tags.Upsert(ctx, name)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	if err := s.tags.AddToNode(ctx, id, tag.ID); err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, tag)
}

func (s *Server) apiRemoveTag(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parseID(c, "id")
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	tagID, err := strconv.ParseInt(c.Param("tagId"), 10, 64)
	if err != nil {
		return fail(c, http.StatusBadRequest, errors.New("invalid tagId"))
	}
	if err := s.tags.RemoveFromNode(ctx, id, tagID); err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, nil)
}
```

Delete `apiUpdateMeta`, `apiAddTag`, `apiRemoveTag` from `stubs.go`.

- [ ] **Step 4: Append meta wiring to reader.js**

Append to `internal/server/web/static/js/reader.js`:

```js
// --- metadata editing ---
const authorInput = document.getElementById('author');
const ratingSel = document.getElementById('rating');
const tagsBox = document.getElementById('tags');

function patchMeta(payload) {
  fetch(`/api/comics/${id}`, {
    method: 'PATCH', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  });
}
authorInput.addEventListener('change', () => patchMeta({ author: authorInput.value }));
ratingSel.addEventListener('change', () => patchMeta({ rating: parseInt(ratingSel.value, 10) }));

function renderTags(tags) {
  tagsBox.innerHTML = '';
  (tags || []).forEach(t => {
    const span = document.createElement('span');
    span.className = 'tag';
    span.textContent = t.name + ' ';
    const x = document.createElement('button');
    x.textContent = '×';
    x.onclick = () => fetch(`/api/comics/${id}/tags/${t.id}`, { method: 'DELETE' }).then(loadDetail);
    span.appendChild(x);
    tagsBox.appendChild(span);
  });
}

function loadDetail() {
  fetch(`/api/comics/${id}`).then(r => r.json()).then(j => renderTags(j.data.tags));
}

document.getElementById('addtag').addEventListener('submit', (e) => {
  e.preventDefault();
  const input = document.getElementById('newtag');
  const name = input.value.trim();
  if (!name) return;
  fetch(`/api/comics/${id}/tags`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name })
  }).then(() => { input.value = ''; loadDetail(); });
});

loadDetail();
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/server/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/server
git commit -m "feat: add comic metadata and tag editing API"
```

---

## Task 14: Tag management API + page

**Files:**
- Create: `internal/server/api_tags.go`
- Modify: `internal/server/stubs.go` (remove `pageTags`, `apiListTags`, `apiCreateTag`, `apiRenameTag`, `apiDeleteTag`)
- Replace: `internal/server/web/static/js/tags.js`
- Test: extend `internal/server/server_test.go`

**Interfaces:**
- Consumes: `store.TagRepo.List/Upsert/Rename/Delete`, `ErrDuplicate`.
- Produces:
  - `s.apiListTags` — `GET /api/tags` → `[]*store.TagCount`.
  - `s.apiCreateTag` — `POST /api/tags` body `{name}` (validate non-empty, ≤64).
  - `s.apiRenameTag` — `PATCH /api/tags/:id` body `{name}`; 409 on `ErrDuplicate`.
  - `s.apiDeleteTag` — `DELETE /api/tags/:id`.
  - `s.pageTags` — renders `tags.html` with `{Tags []*store.TagCount}`.

- [ ] **Step 1: Write failing tests**

Append to `internal/server/server_test.go`:

```go
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
	store.NewTagRepo(db).Upsert(context.Background(), "demo")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tags", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "demo") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -run 'TagCRUD|PageTags' -v`
Expected: FAIL (stubs 501).

- [ ] **Step 3: Implement api_tags.go**

Create `internal/server/api_tags.go`:

```go
package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"

	"Tefnut/internal/store"
)

func validTagName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", errors.New("tag name required")
	}
	if len(name) > 64 {
		return "", errors.New("tag name too long")
	}
	return name, nil
}

func (s *Server) apiListTags(c echo.Context) error {
	tags, err := s.tags.List(c.Request().Context())
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, tags)
}

func (s *Server) apiCreateTag(c echo.Context) error {
	var body struct {
		Name string `json:"name"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	name, err := validTagName(body.Name)
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	tag, err := s.tags.Upsert(c.Request().Context(), name)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, tag)
}

func (s *Server) apiRenameTag(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return fail(c, http.StatusBadRequest, errors.New("invalid id"))
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	name, err := validTagName(body.Name)
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	if err := s.tags.Rename(c.Request().Context(), id, name); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			return fail(c, http.StatusConflict, err)
		}
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, map[string]any{"id": id, "name": name})
}

func (s *Server) apiDeleteTag(c echo.Context) error {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return fail(c, http.StatusBadRequest, errors.New("invalid id"))
	}
	if err := s.tags.Delete(c.Request().Context(), id); err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, nil)
}

func (s *Server) pageTags(c echo.Context) error {
	tags, err := s.tags.List(c.Request().Context())
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return render(c, "tags.html", map[string]any{"Tags": tags})
}
```

Delete `pageTags`, `apiListTags`, `apiCreateTag`, `apiRenameTag`, `apiDeleteTag` from `stubs.go`. At this point `stubs.go` should contain only `apiSetProgress`; if so, the file is removed entirely in Task 16.

- [ ] **Step 4: Replace tags.js**

Replace `internal/server/web/static/js/tags.js`:

```js
document.getElementById('newtagform').addEventListener('submit', (e) => {
  e.preventDefault();
  const input = document.getElementById('newtagname');
  const name = input.value.trim();
  if (!name) return;
  fetch('/api/tags', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name })
  }).then(() => location.reload());
});

document.getElementById('taglist').addEventListener('click', (e) => {
  const li = e.target.closest('li');
  if (!li) return;
  const id = li.dataset.id;
  if (e.target.classList.contains('rename')) {
    const name = li.querySelector('.tname').value.trim();
    fetch(`/api/tags/${id}`, {
      method: 'PATCH', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name })
    }).then(r => { if (!r.ok) alert('重命名失败（可能重名）'); else location.reload(); });
  }
  if (e.target.classList.contains('del')) {
    if (!confirm('确认删除该标签？')) return;
    fetch(`/api/tags/${id}`, { method: 'DELETE' }).then(() => location.reload());
  }
});
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/server/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/server
git commit -m "feat: add tag management API and page"
```

---

## Task 15: Reading progress API

**Files:**
- Create: `internal/server/api_progress.go`
- Modify/Delete: `internal/server/stubs.go` (remove `apiSetProgress`; delete file if now empty)
- Test: extend `internal/server/server_test.go`

**Interfaces:**
- Consumes: `store.ProgressRepo.Set`, `store.NodeRepo.Get` (validate comic exists + page in range).
- Produces:
  - `s.apiSetProgress` — `PUT /api/comics/:id/progress` body `{page:int}`. Validates page ≥ 0 and < pageCount (clamps to pageCount-1 is not done; reject out of range with 400).

- [ ] **Step 1: Write failing test**

Append to `internal/server/server_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -run 'SetProgress' -v`
Expected: FAIL (stub 501).

- [ ] **Step 3: Implement api_progress.go**

Create `internal/server/api_progress.go`:

```go
package server

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"Tefnut/internal/store"
)

func (s *Server) apiSetProgress(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parseID(c, "id")
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	var body struct {
		Page int `json:"page"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	n, err := s.nodes.Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return fail(c, http.StatusNotFound, err)
	}
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	if body.Page < 0 || (n.PageCount > 0 && body.Page >= n.PageCount) {
		return fail(c, http.StatusBadRequest, errors.New("page out of range"))
	}
	if err := s.progress.Set(ctx, id, body.Page); err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, nil)
}
```

Remove `apiSetProgress` from `stubs.go`. If `stubs.go` is now empty (only `package server` left), delete it:

```bash
git rm internal/server/stubs.go
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A internal/server
git commit -m "feat: add reading progress API"
```

---

## Task 16: Main wiring — DI, cron, startup scan, echo server

**Files:**
- Modify: `cmd/tefnut/main.go`
- Test: manual smoke run (no unit test; verified by `go build` + run)

**Interfaces:**
- Consumes: `config.Load`, `store.Open`, `store.New*Repo`, `library.NewScanner`, `server.NewServer`.

- [ ] **Step 1: Implement full main.go**

Replace `cmd/tefnut/main.go`:

```go
package main

import (
	"context"
	"flag"
	"log"
	"path/filepath"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/robfig/cron/v3"

	"Tefnut/internal/config"
	"Tefnut/internal/library"
	"Tefnut/internal/server"
	"Tefnut/internal/store"
)

func main() {
	cfgPath := flag.String("config", "./config.yaml", "path to config.yaml")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatal(err)
	}

	db, err := store.Open(filepath.Join(cfg.DataDir, "tefnut.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	nodes := store.NewNodeRepo(db)
	tags := store.NewTagRepo(db)
	progress := store.NewProgressRepo(db)

	scanner := library.NewScanner(nodes, cfg.Library.RootPath, cfg.DataDir, cfg.Thumbnail.Width)

	// Startup scan (blocking once, so the library is populated before serving).
	if err := scanner.Scan(context.Background()); err != nil {
		log.Printf("initial scan: %v", err)
	}

	// Periodic scan.
	interval, _ := cfg.ScanInterval()
	c := cron.New()
	if _, err := c.AddFunc("@every "+interval.String(), func() {
		if err := scanner.Scan(context.Background()); err != nil {
			log.Printf("scan: %v", err)
		}
	}); err != nil {
		log.Fatalf("cron schedule: %v", err)
	}
	c.Start()
	defer c.Stop()

	srv := server.NewServer(nodes, tags, progress, cfg.DataDir, cfg.Thumbnail.Width)

	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	srv.Register(e)

	log.Printf("tefnut listening on %s, library=%s", cfg.Server.Addr, cfg.Library.RootPath)
	if err := e.Start(cfg.Server.Addr); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 2: Build everything**

Run: `go build ./... && go vet ./...`
Expected: no errors.

- [ ] **Step 3: Smoke test end-to-end**

```bash
mkdir -p /tmp/tefnut-smoke/COMIC/Series
# create a tiny zip with one png page using the test helper or any zip tool
cd /tmp/tefnut-smoke
cat > config.yaml <<'EOF'
library:
  rootPath: "./COMIC"
dataDir: "./data"
server:
  addr: ":8086"
scan:
  interval: "2m"
thumbnail:
  width: 400
EOF
# Put at least one .zip of images into ./COMIC/Series first, then:
go run Tefnut/cmd/tefnut -config ./config.yaml
```

Verify in a browser: `http://localhost:8086/` lists the folder; entering it shows the comic with a cover; clicking opens the reader; arrow keys page; author/rating/tags persist after reload; `/tags` lists and manages tags.

Expected: all flows work. Fix any wiring issues surfaced here.

- [ ] **Step 4: Run the full test suite**

Run: `go test ./...`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/tefnut/main.go
git commit -m "feat: wire main with scanner, cron, and echo server"
```

---

## Task 17: README + run docs

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Write README**

Replace `README.md`:

```markdown
# Tefnut

A self-hosted family comic server (Plex-for-comics). Point it at a directory of
comic archives (`.zip/.cbz`, `.rar/.cbr`, `.7z/.cb7`) and read them in your
browser. Single Go binary + a SQLite file; no external database.

## Quick start

1. Edit `cmd/tefnut/config.yaml` (or copy it next to the binary):
   - `library.rootPath` — your comic library directory
   - `dataDir` — where the DB, thumbnails, and extract cache live
   - `server.addr` — listen address (default `:8086`)
   - `scan.interval` — rescan period (default `2m`)
   - `thumbnail.width` — cover width in px (default `400`)
2. Run:
   ```bash
   go run ./cmd/tefnut -config ./cmd/tefnut/config.yaml
   ```
3. Open http://localhost:8086

Drop new comic archives into the library directory; they appear after the next
scan (and immediately on restart).

## Features
- Folder-based browsing of the library tree
- Auto-generated cover thumbnails (first page of each archive)
- In-browser reader with keyboard paging and remembered progress
- Per-comic author, 0–5★ rating, and free-text tags
- Search by name; filter by tag and minimum rating
- Tag management page (rename / delete / counts)

## Build
```bash
go build -o tefnut ./cmd/tefnut
```
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: rewrite README for comic server"
```

---

## Self-Review

**Spec coverage:**
- §1 goals: library dir + auto reflect → Task 9 + 16; folder browse → Task 12; filename+cover in list → Tasks 9/11/12; reading + progress → Tasks 12/15; author/rating/tags → Tasks 13/14; search/tag/rating filter → Tasks 3/11/12; tag management → Task 14. ✓
- §3 stack: Go 1.24 (T1), echo (T10/16), modernc sqlite (T2), archiver formats (T7), x/image (T8), cron+yaml (T1/16), embed (T10). ✓
- §6 schema: nodes/tags/node_tags/progress → T2. ✓
- §8 archive (zip random / rar-7z extract cache, natsort, junk filter) → T6/T7. ✓
- §9 thumbnails → T8. ✓
- §10 every route → T10 registers; T11–T15 implement. ✓
- §11 reader behavior → T12 reader.js + T13 meta wiring. ✓
- §12 tag management → T14. ✓
- §14 test plan ≥80% on core packages → tests in T1–T15; rar/7z extraction gap explicitly noted in T7. ✓

**Placeholder scan:** Task 9 `itoa`/`strconvFormat` helper and Task 11 `itoa` could collide if both define `itoa` in the same package — note: `internal/library` defines `strconvFormat` (test helper) and `internal/server` defines `itoa` (test helper); they are in different packages, no collision. The Task 10 stubs are intentional temporary scaffolding, each removed in the task that implements it. The Task 12 `echoContext` alias is explicitly replaced with `context.Context` in the same step (follow the SIMPLER note). No `TODO`/`TBD` remain in production code.

**Type consistency:** Repo constructors `NewNodeRepo/NewTagRepo/NewProgressRepo` take `*store.DB` consistently (T2–T5). `archive.Open(ctx, path, cacheDir)` and `archive.FirstImage(ctx, path, cacheDir)` signatures match between T7 producer and T9/T11 consumers. `store.Node` field names (`PageCount`, `CoverStatus`, `MTime`) are identical across scanner and handlers. Handler method names in `server.go`'s `Register` exactly match those implemented in T11–T15. Cover/page/cache path construction (`<dataDir>/thumbs/<id>.jpg`, `<dataDir>/cache/<id>`) is identical in scanner (T9) and handlers (T11).

Fixes applied inline above (template block-override note in T12, dead-code note in T3, rar/7z test-gap note in T7).
