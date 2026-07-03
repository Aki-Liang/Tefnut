# 配置目录挂载 + 缓存预算运行时可改 — 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 配置文件搬进可 docker 挂载的 `/config` 目录(缺失自写模板);缓存两预算(解压缓存/页缩略图)优先级 DB(设置页) > env > yaml,设置页可改、保存即生效。

**Architecture:** 复用 SettingsRepo k-v 模式存预算(fallback 到 config 解析出的 defaults,不做一次性种子);`scan.Manager.enforceBudgets` 每轮现查生效值;`config.LoadOrInit` 自写带注释模板;yaml 预算字段升级为 `ByteSize` 类型(接受 `2GiB` 后缀写法,与 env 同一 parseSize),rainmaker 因此可把提示值原样写进 yaml。

**Tech Stack:** Go 1.24、echo v4、gopkg.in/yaml.v3、SQLite(现有 store)、原生 JS + 项目自有 dropdown.js(格/PANEL 主题渐进增强,layout.html 已全局加载)。

**Spec:** `docs/superpowers/specs/2026-07-02-config-dir-and-runtime-budgets-design.md`

## Global Constraints

- 分支 `feature/config-dir-runtime-budgets`(基于 `fix/nonblocking-initial-scan`,依赖其 `scan.Budgets`、`config.parseSize`、`internal/cache.Enforce`)。
- TDD:每个行为先写失败测试并**运行看到失败**,再实现。
- 预算语义:`<= 0` = 不限制(与 `cache.Enforce` 现约定一致)。
- 优先级:DB(设置页保存过) > env(`TEFNUT_CACHE_MAX_BYTES`/`TEFNUT_THUMB_PAGES_MAX_BYTES`) > yaml > 内置默认(2GiB / 512MiB)。fallback 链,非种子。
- UI 下拉必须是原生 `<select>`(dropdown.js 自动做格/PANEL 主题增强),不得使用其他组件。
- 用户界面文案与错误提示用中文;`GetBudgets` 读到坏值必须报错,禁止静默回落。
- 提交信息 conventional commits;无 Co-Authored-By(全局已禁用)。

---

### Task 1: config.ByteSize — yaml 预算字段接受 `2GiB` 后缀

**Files:**
- Create: `internal/config/bytesize.go`
- Test: `internal/config/bytesize_test.go`
- Modify: `internal/config/config.go`(Cache/Thumbnail 字段类型)、`internal/config/env.go`(applyEnv 赋值处)
- Modify: `cmd/tefnut/main.go`(`int64(...)` 显式转换)

**Interfaces:**
- Consumes: `parseSize(s string) (int64, error)`(已在 `internal/config/env.go`)
- Produces: `type ByteSize int64`,实现 `UnmarshalYAML(*yaml.Node) error`;`Config.Cache.MaxBytes`、`Config.Thumbnail.PagesMaxBytes` 类型变为 `ByteSize`

- [ ] **Step 1: 写失败测试**

`internal/config/bytesize_test.go`:

```go
package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestByteSizeUnmarshalYAML(t *testing.T) {
	var v struct {
		N ByteSize `yaml:"n"`
	}
	cases := []struct {
		in   string
		want int64
		err  bool
	}{
		{"n: 2147483648", 2147483648, false},
		{"n: 2GiB", 2 << 30, false},
		{"n: \"512MiB\"", 512 << 20, false},
		{"n: 0", 0, false},
		{"n: banana", 0, true},
		{"n: [1,2]", 0, true},
	}
	for _, c := range cases {
		v.N = 0
		err := yaml.Unmarshal([]byte(c.in), &v)
		if c.err != (err != nil) {
			t.Errorf("%q: err = %v, want err=%v", c.in, err, c.err)
			continue
		}
		if !c.err && int64(v.N) != c.want {
			t.Errorf("%q = %d, want %d", c.in, v.N, c.want)
		}
	}
}

func TestLoadAcceptsSuffixedBudgets(t *testing.T) {
	p := writeTemp(t, "dataDir: "+t.TempDir()+"\ncache:\n  maxBytes: 2GiB\nthumbnail:\n  pagesMaxBytes: 512MiB")
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if int64(cfg.Cache.MaxBytes) != 2<<30 || int64(cfg.Thumbnail.PagesMaxBytes) != 512<<20 {
		t.Errorf("budgets = %d/%d", cfg.Cache.MaxBytes, cfg.Thumbnail.PagesMaxBytes)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/config/ -run 'TestByteSize|TestLoadAcceptsSuffixed' -v`
Expected: 编译失败 `undefined: ByteSize`

- [ ] **Step 3: 实现**

`internal/config/bytesize.go`:

```go
package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// ByteSize is an int64 byte count that unmarshals from either a raw number
// (2147483648) or a binary-unit size string ("2GiB", "512MiB", "0") — the
// same forms the TEFNUT_* env overrides accept.
type ByteSize int64

func (b *ByteSize) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("config: size must be a number or string like 2GiB, got %s", value.Tag)
	}
	n, err := parseSize(value.Value)
	if err != nil {
		return err
	}
	*b = ByteSize(n)
	return nil
}
```

`internal/config/config.go` 字段改类型(其余不动):

```go
type Thumbnail struct {
	Width         int      `yaml:"width"`
	PageWidth     int      `yaml:"pageWidth"`
	PagesMaxBytes ByteSize `yaml:"pagesMaxBytes"` // budget for data/thumbs/pages; <=0 disables eviction
}

type Cache struct {
	MaxBytes ByteSize `yaml:"maxBytes"`
}
```

`internal/config/env.go` applyEnv 两处赋值加转换:

```go
		c.Cache.MaxBytes = ByteSize(n)
```
```go
		c.Thumbnail.PagesMaxBytes = ByteSize(n)
```

`cmd/tefnut/main.go` scan.Budgets 组装处显式转换:

```go
	manager := scan.New(scanner, settingsRepo, pathRepo, cfg.DataDir, scan.Budgets{
		ExtractCacheBytes: int64(cfg.Cache.MaxBytes),
		PageThumbBytes:    int64(cfg.Thumbnail.PagesMaxBytes),
	})
```

- [ ] **Step 4: 运行确认通过(含既有 config 测试不回归)**

Run: `go build ./... && go test ./internal/config/ -v`
Expected: 全 PASS(`TestLoadPageThumbBudget`、`TestEnvOverridesBudgets` 等不回归——untyped 常量与 ByteSize 可直接比较)

- [ ] **Step 5: Commit**

```bash
git add internal/config/ cmd/tefnut/main.go
git commit -m "feat: yaml budget fields accept 2GiB-style sizes (ByteSize)"
```

---

### Task 2: store — GetBudgets / SetBudgets

**Files:**
- Modify: `internal/store/settings_repo.go`
- Test: `internal/store/settings_repo_test.go`(追加)

**Interfaces:**
- Consumes: 现有 `settings` k-v 表、`openTemp(t)` 测试助手(store 包内)
- Produces:
  - `func (r *SettingsRepo) GetBudgets(ctx context.Context, defCache, defPageThumb int64) (int64, int64, error)`
  - `func (r *SettingsRepo) SetBudgets(ctx context.Context, cacheMax, pageThumbMax int64) error`
  - 键名:`cache_max_bytes`、`thumb_pages_max_bytes`(TEXT,十进制字节数)

- [ ] **Step 1: 写失败测试**

追加到 `internal/store/settings_repo_test.go`:

```go
func TestGetBudgetsFallsBackToDefaults(t *testing.T) {
	r := NewSettingsRepo(openTemp(t))
	cache, thumb, err := r.GetBudgets(context.Background(), 111, 222)
	if err != nil {
		t.Fatalf("GetBudgets: %v", err)
	}
	if cache != 111 || thumb != 222 {
		t.Errorf("got %d/%d, want defaults 111/222", cache, thumb)
	}
}

func TestSetBudgetsRoundTrip(t *testing.T) {
	r := NewSettingsRepo(openTemp(t))
	if err := r.SetBudgets(context.Background(), 1<<30, 64<<20); err != nil {
		t.Fatalf("SetBudgets: %v", err)
	}
	cache, thumb, err := r.GetBudgets(context.Background(), 111, 222)
	if err != nil {
		t.Fatalf("GetBudgets: %v", err)
	}
	if cache != 1<<30 || thumb != 64<<20 {
		t.Errorf("got %d/%d, want saved values (DB must beat defaults)", cache, thumb)
	}
}

func TestGetBudgetsRejectsCorruptValue(t *testing.T) {
	db := openTemp(t)
	r := NewSettingsRepo(db)
	if _, err := db.Write().Exec(
		`INSERT INTO settings (key, value) VALUES ('cache_max_bytes', 'garbage')`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.GetBudgets(context.Background(), 1, 2); err == nil {
		t.Fatal("expected error for non-numeric stored value, got nil (must not silently fall back)")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/store/ -run 'Budgets' -v`
Expected: 编译失败 `r.GetBudgets undefined`

- [ ] **Step 3: 实现**

追加到 `internal/store/settings_repo.go`(import 增 `strconv`):

```go
const (
	keyCacheMaxBytes      = "cache_max_bytes"
	keyThumbPagesMaxBytes = "thumb_pages_max_bytes"
)

// lookup returns (value, found) without a default, distinguishing a missing
// row from any stored value.
func (r *SettingsRepo) lookup(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := r.rdb.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store: get setting %q: %w", key, err)
	}
	return v, true, nil
}

func (r *SettingsRepo) getInt64(ctx context.Context, key string, def int64) (int64, error) {
	v, found, err := r.lookup(ctx, key)
	if err != nil {
		return 0, err
	}
	if !found {
		return def, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		// A corrupt stored value must surface, not silently fall back: the
		// caller may be about to evict caches against this number.
		return 0, fmt.Errorf("store: setting %q has non-numeric value %q", key, v)
	}
	return n, nil
}

// GetBudgets returns the effective disk budgets: values saved from the
// settings UI win; keys never saved fall back to the given defaults
// (config file / env, resolved by the caller). <=0 means unlimited.
func (r *SettingsRepo) GetBudgets(ctx context.Context, defCache, defPageThumb int64) (int64, int64, error) {
	cache, err := r.getInt64(ctx, keyCacheMaxBytes, defCache)
	if err != nil {
		return 0, 0, err
	}
	pageThumb, err := r.getInt64(ctx, keyThumbPagesMaxBytes, defPageThumb)
	if err != nil {
		return 0, 0, err
	}
	return cache, pageThumb, nil
}

// SetBudgets persists both budgets (bytes) in one transaction.
func (r *SettingsRepo) SetBudgets(ctx context.Context, cacheMax, pageThumbMax int64) error {
	tx, err := r.wdb.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback()
	pairs := [][2]string{
		{keyCacheMaxBytes, strconv.FormatInt(cacheMax, 10)},
		{keyThumbPagesMaxBytes, strconv.FormatInt(pageThumbMax, 10)},
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

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/store/ -v -run 'Budgets|Scan'`
Expected: 新 3 例 PASS,既有 settings 测试不回归

- [ ] **Step 5: Commit**

```bash
git add internal/store/settings_repo.go internal/store/settings_repo_test.go
git commit -m "feat: settings repo stores cache budgets (DB > defaults fallback)"
```

---

### Task 3: config.LoadOrInit — 缺文件自写带注释模板

**Files:**
- Create: `internal/config/template.go`
- Test: `internal/config/template_test.go`

**Interfaces:**
- Consumes: `Load(path)`(现有)、Task 1 的 ByteSize(模板里用 `2GiB` 写法)
- Produces: `func LoadOrInit(path string) (*Config, error)`;`defaultTemplate` 常量(内部)

- [ ] **Step 1: 写失败测试**

`internal/config/template_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// 模板本身必须可解析且值正确(不经 validate,因 /comics 在测试机不存在)。
func TestDefaultTemplateParses(t *testing.T) {
	cfg := defaults()
	if err := yaml.Unmarshal([]byte(defaultTemplate), cfg); err != nil {
		t.Fatalf("template does not parse: %v", err)
	}
	if cfg.Library.RootPath != "/comics" || cfg.DataDir != "/data" {
		t.Errorf("paths = %q/%q", cfg.Library.RootPath, cfg.DataDir)
	}
	if int64(cfg.Cache.MaxBytes) != 2<<30 || int64(cfg.Thumbnail.PagesMaxBytes) != 512<<20 {
		t.Errorf("budgets = %d/%d", cfg.Cache.MaxBytes, cfg.Thumbnail.PagesMaxBytes)
	}
	if strings.Contains(defaultTemplate, "scan:") {
		t.Error("template must not contain the dead scan: section")
	}
}

func TestLoadOrInitSeedsMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cfgdir", "config.yaml")
	_, err := LoadOrInit(p)
	// /comics 不存在于测试机 → 期望 rootPath 校验错误;但文件必须已生成。
	if err == nil || !strings.Contains(err.Error(), "rootPath") {
		t.Fatalf("expected rootPath validation error on this machine, got %v", err)
	}
	b, rerr := os.ReadFile(p)
	if rerr != nil {
		t.Fatalf("template file not seeded: %v", rerr)
	}
	if !strings.Contains(string(b), "pagesMaxBytes") || !strings.Contains(string(b), "maxBytes") {
		t.Errorf("seeded file missing budget keys:\n%s", b)
	}
}

func TestLoadOrInitReadOnlyDirErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	_, err := LoadOrInit(filepath.Join(dir, "config.yaml"))
	if err == nil || !strings.Contains(err.Error(), "检查配置目录挂载与权限") {
		t.Fatalf("expected write-failure error mentioning mount/permissions, got %v", err)
	}
}

func TestLoadOrInitKeepsExistingFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	custom := "library:\n  rootPath: " + dir + "\ndataDir: " + dir + "\ncache:\n  maxBytes: 7\n"
	if err := os.WriteFile(p, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadOrInit(p)
	if err != nil {
		t.Fatalf("LoadOrInit: %v", err)
	}
	if int64(cfg.Cache.MaxBytes) != 7 {
		t.Errorf("existing file must be used untouched, maxBytes = %d", cfg.Cache.MaxBytes)
	}
	b, _ := os.ReadFile(p)
	if string(b) != custom {
		t.Error("existing file content was rewritten")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/config/ -run 'Template|LoadOrInit' -v`
Expected: 编译失败 `undefined: defaultTemplate` / `LoadOrInit`

- [ ] **Step 3: 实现**

`internal/config/template.go`:

```go
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// defaultTemplate is seeded into the config path on first start when no file
// exists (e.g. a freshly mounted /config volume), so users get an editable,
// commented file. No scan: section — scan settings live in the database and
// are edited on the settings page.
const defaultTemplate = `# Tefnut 配置文件。修改后重启容器生效。
# 缓存上限也可在 Web 设置页修改；设置页保存过的值优先于本文件。
library:
  rootPath: /comics # 漫画库目录（容器内路径，docker 只读挂载）
dataDir: /data # 数据库/缩略图/缓存（需持久卷）
server:
  addr: ":8086"
thumbnail:
  width: 400 # 封面宽度（像素）
  pageWidth: 120 # 页缩略图宽度（像素）
  pagesMaxBytes: 512MiB # 页缩略图缓存上限；0 = 不限制
cache:
  maxBytes: 2GiB # 解压缓存上限；0 = 不限制
`

// LoadOrInit loads the config at path; when the file does not exist it first
// writes the commented default template there (creating parent dirs), so a
// freshly mounted config volume self-seeds an editable file.
func LoadOrInit(path string) (*Config, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("config: create dir for %s: %w（检查配置目录挂载与权限）", path, err)
		}
		if err := os.WriteFile(path, []byte(defaultTemplate), 0o644); err != nil {
			return nil, fmt.Errorf("config: write default %s: %w（检查配置目录挂载与权限）", path, err)
		}
	}
	return Load(path)
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/config/ -v`
Expected: 全 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/template.go internal/config/template_test.go
git commit -m "feat: LoadOrInit self-seeds a commented config template"
```

---

### Task 4: scan.Manager — enforceBudgets 每轮现查 DB 生效值

**Files:**
- Modify: `internal/scan/manager.go`(字段 `budgets` → `defaults`;enforceBudgets 查 GetBudgets)
- Test: `internal/scan/budgets_test.go`(追加)

**Interfaces:**
- Consumes: Task 2 的 `SettingsRepo.GetBudgets/SetBudgets`;现有 `m.settings`、`m.baseContext()`、`cache.Enforce`
- Produces: 行为——DB 保存过的预算覆盖启动 defaults,下次扫描即生效;GetBudgets 出错时跳过清理(log,不清)

- [ ] **Step 1: 写失败测试**

追加到 `internal/scan/budgets_test.go`:

```go
// UI 保存的预算(DB)必须覆盖启动时的 defaults,且下次扫描即生效。
func TestEnforceBudgetsUsesDBOverrides(t *testing.T) {
	settings, paths := newRepos(t)
	dataDir := t.TempDir()
	oldCache := filepath.Join(dataDir, "cache", "1")
	oldThumb := filepath.Join(dataDir, "thumbs", "pages", "1")
	writeDirOfSize(t, oldCache, 600, time.Now().Add(-time.Hour))
	writeDirOfSize(t, filepath.Join(dataDir, "cache", "2"), 600, time.Now())
	writeDirOfSize(t, oldThumb, 600, time.Now().Add(-time.Hour))
	writeDirOfSize(t, filepath.Join(dataDir, "thumbs", "pages", "2"), 600, time.Now())

	// defaults 大到不会触发清理;DB 里保存小预算 → 必须按 DB 清。
	if err := settings.SetBudgets(context.Background(), 1000, 1000); err != nil {
		t.Fatal(err)
	}
	m := New(&fakeScanner{}, settings, paths, dataDir,
		Budgets{ExtractCacheBytes: 1 << 40, PageThumbBytes: 1 << 40})
	if err := m.runScan(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(oldCache); !os.IsNotExist(err) {
		t.Errorf("DB budget must evict oldest extract dir, stat err = %v", err)
	}
	if _, err := os.Stat(oldThumb); !os.IsNotExist(err) {
		t.Errorf("DB budget must evict oldest page-thumb dir, stat err = %v", err)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/scan/ -run TestEnforceBudgetsUsesDBOverrides -v`
Expected: FAIL——defaults 1<<40 不清理,两个 stat 都还在(DB 值未被采用)

- [ ] **Step 3: 实现**

`internal/scan/manager.go`:字段与构造改名(`budgets` → `defaults`,含 `New` 内赋值),`enforceBudgets` 整体替换为:

```go
// enforceBudgets bounds the scan-refreshed disk caches, evicting whole
// per-comic subdirectories oldest-modified first (see cache.Enforce). The
// effective budgets are read per run: values saved on the settings page (DB)
// win over the startup defaults (config file / env).
func (m *Manager) enforceBudgets() {
	cacheMax, pageThumbMax, err := m.settings.GetBudgets(
		m.baseContext(), m.defaults.ExtractCacheBytes, m.defaults.PageThumbBytes)
	if err != nil {
		// Refuse to sweep with unknown budgets: skipping is safe (retried next
		// scan); evicting against a wrong limit is not.
		log.Printf("scan: read budgets: %v (skipping cache sweep)", err)
		return
	}
	caps := []struct {
		root string
		max  int64
		what string
	}{
		{filepath.Join(m.dataDir, "cache"), cacheMax, "extract"},
		{filepath.Join(m.dataDir, "thumbs", "pages"), pageThumbMax, "page-thumb"},
	}
	for _, c := range caps {
		if n, err := cache.Enforce(c.root, c.max); err != nil {
			log.Printf("scan: enforce %s budget: %v", c.what, err)
		} else if n > 0 {
			log.Printf("scan: evicted %d %s dir(s)", n, c.what)
		}
	}
}
```

- [ ] **Step 4: 运行确认通过(含既有 budgets 测试)**

Run: `go test ./internal/scan/ -v`
Expected: 全 PASS(`TestRunScanEnforcesPageThumbBudget` 等 DB 为空 → 走 defaults,不回归)

- [ ] **Step 5: Commit**

```bash
git add internal/scan/
git commit -m "feat: scan reads effective cache budgets from DB per run"
```

---

### Task 5: server API — 设置读写预算 + NewServer 默认值参数

**Files:**
- Modify: `internal/server/server.go`(struct 两字段 + NewServer 两参数)
- Modify: `internal/server/api_settings.go`(DTO/GET/PUT)
- Modify: `internal/server/server_test.go`(`newTestServer` 调用处)
- Modify: `cmd/tefnut/main.go`(NewServer 调用处)
- Test: `internal/server/api_settings_budgets_test.go`(新)

**Interfaces:**
- Consumes: Task 2 `GetBudgets/SetBudgets`
- Produces:
  - `NewServer(..., allowedRoots []string, defCacheMax, defPageThumbMax int64) *Server`(追加在末尾)
  - GET `/api/settings` 增返 `cacheMaxBytes`、`thumbPagesMaxBytes`(生效值,字节)
  - PUT `/api/settings` body 增 `cacheMaxBytes`、`thumbPagesMaxBytes`(`*int64` 可选;scan 组在 `scanMode != ""` 时才校验/保存;任一预算提供即持久化两键——未提供的一侧取当前生效值;末尾统一 `Reconfigure`)
  - 测试 defaults 约定:`newTestServer` 传 `1<<30, 64<<20`

- [ ] **Step 1: 写失败测试**

`internal/server/api_settings_budgets_test.go`:

```go
package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"Tefnut/internal/store"
)

func TestGetSettingsReturnsDefaultBudgets(t *testing.T) {
	_, e, _ := newTestServer(t) // defaults: 1<<30 / 64<<20
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"cacheMaxBytes":1073741824`) ||
		!strings.Contains(body, `"thumbPagesMaxBytes":67108864`) {
		t.Fatalf("defaults missing in body: %s", body)
	}
}

func TestPutSettingsSavesBudgetsAndReconfigures(t *testing.T) {
	s, e, db := newTestServer(t)
	stub := s.reconf.(*stubReconf)
	req := httptest.NewRequest(http.MethodPut, "/api/settings",
		strings.NewReader(`{"cacheMaxBytes":2048,"thumbPagesMaxBytes":1024}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	cache, thumb, err := store.NewSettingsRepo(db).GetBudgets(context.Background(), 0, 0)
	if err != nil || cache != 2048 || thumb != 1024 {
		t.Fatalf("persisted = %d/%d err=%v", cache, thumb, err)
	}
	if stub.calls != 1 {
		t.Fatalf("Reconfigure calls = %d, want 1", stub.calls)
	}
}

func TestPutSettingsRejectsNegativeBudget(t *testing.T) {
	_, e, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPut, "/api/settings",
		strings.NewReader(`{"cacheMaxBytes":-1}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// 旧客户端只发 scan 字段 → 预算不被触碰。
func TestPutScanOnlyBodyLeavesBudgetsUnset(t *testing.T) {
	_, e, db := newTestServer(t)
	req := httptest.NewRequest(http.MethodPut, "/api/settings",
		strings.NewReader(`{"scanMode":"watch"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	cache, thumb, err := store.NewSettingsRepo(db).GetBudgets(context.Background(), 7, 8)
	if err != nil || cache != 7 || thumb != 8 {
		t.Fatalf("budgets must stay unset (fall back to 7/8), got %d/%d err=%v", cache, thumb, err)
	}
}
```

注:`stubReconf` 与 `newTestServer` 已在 `server_test.go`;`s.reconf` 为包内字段可直接断言。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/server/ -run 'Budget|ScanOnly' -v`
Expected: 编译通过但 FAIL(GET 无预算字段;PUT 负数 200;等)——若 `newTestServer` 未改先编译失败,属预期

- [ ] **Step 3: 实现**

`internal/server/server.go`:struct 增字段、NewServer 增参(签名末尾追加两个 int64):

```go
	allowedRoots   []string
	defCacheMax      int64 // effective-budget fallback (config/env) for settings API
	defPageThumbMax  int64
```

```go
func NewServer(nodes *store.NodeRepo, tags *store.TagRepo, progress *store.ProgressRepo,
	settings *store.SettingsRepo, paths *store.LibraryPathRepo, reconf Reconfigurer,
	dataDir string, thumbWidth int, pageThumbWidth int, allowedRoots []string,
	defCacheMax, defPageThumbMax int64) *Server {
```
(构造体字面量里补 `defCacheMax: defCacheMax, defPageThumbMax: defPageThumbMax`)

`internal/server/server_test.go` `newTestServer`:

```go
	s := NewServer(store.NewNodeRepo(db), store.NewTagRepo(db), store.NewProgressRepo(db),
		store.NewSettingsRepo(db), store.NewLibraryPathRepo(db), &stubReconf{}, data, 400, 120, nil,
		1<<30, 64<<20)
```

`cmd/tefnut/main.go`:

```go
	srv := server.NewServer(nodes, tags, progress, settingsRepo, pathRepo, manager,
		cfg.DataDir, cfg.Thumbnail.Width, cfg.Thumbnail.PageWidth, cfg.Library.AllowedRoots,
		int64(cfg.Cache.MaxBytes), int64(cfg.Thumbnail.PagesMaxBytes))
```

`internal/server/api_settings.go`:

```go
type settingsDTO struct {
	LibraryPaths       []*store.LibraryPath `json:"libraryPaths"`
	ScanMode           string               `json:"scanMode"`
	ScanInterval       string               `json:"scanInterval"`
	ScanDailyTime      string               `json:"scanDailyTime"`
	CacheMaxBytes      int64                `json:"cacheMaxBytes"`
	ThumbPagesMaxBytes int64                `json:"thumbPagesMaxBytes"`
}
```

`apiGetSettings` 在读 scan/paths 后增:

```go
	cacheMax, pageThumbMax, err := s.settings.GetBudgets(ctx, s.defCacheMax, s.defPageThumbMax)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, settingsDTO{
		LibraryPaths: paths, ScanMode: scan.Mode,
		ScanInterval: scan.Interval, ScanDailyTime: scan.DailyTime,
		CacheMaxBytes: cacheMax, ThumbPagesMaxBytes: pageThumbMax,
	})
```

`apiUpdateSettings` 整体替换:

```go
func (s *Server) apiUpdateSettings(c echo.Context) error {
	ctx := c.Request().Context()
	var body struct {
		ScanMode           string `json:"scanMode"`
		ScanInterval       string `json:"scanInterval"`
		ScanDailyTime      string `json:"scanDailyTime"`
		CacheMaxBytes      *int64 `json:"cacheMaxBytes"`
		ThumbPagesMaxBytes *int64 `json:"thumbPagesMaxBytes"`
	}
	if err := c.Bind(&body); err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	// Each group is optional: old clients send only scan fields, the cache
	// form sends only budgets. Validate everything before writing anything.
	if body.ScanMode != "" {
		if err := validScanSettings(body.ScanMode, body.ScanInterval, body.ScanDailyTime); err != nil {
			return fail(c, http.StatusBadRequest, err)
		}
	}
	if (body.CacheMaxBytes != nil && *body.CacheMaxBytes < 0) ||
		(body.ThumbPagesMaxBytes != nil && *body.ThumbPagesMaxBytes < 0) {
		return fail(c, http.StatusBadRequest, errors.New("缓存上限必须是 ≥ 0 的字节数（0 = 不限制）"))
	}
	if body.ScanMode != "" {
		if err := s.settings.SetScan(ctx, store.ScanSettings{
			Mode: body.ScanMode, Interval: body.ScanInterval, DailyTime: body.ScanDailyTime,
		}); err != nil {
			return fail(c, http.StatusInternalServerError, err)
		}
	}
	if body.CacheMaxBytes != nil || body.ThumbPagesMaxBytes != nil {
		// A partial body pins the omitted side to its current effective value
		// so SetBudgets can atomically write both keys. The UI always sends both.
		curCache, curThumb, err := s.settings.GetBudgets(ctx, s.defCacheMax, s.defPageThumbMax)
		if err != nil {
			return fail(c, http.StatusInternalServerError, err)
		}
		cacheMax, pageThumbMax := curCache, curThumb
		if body.CacheMaxBytes != nil {
			cacheMax = *body.CacheMaxBytes
		}
		if body.ThumbPagesMaxBytes != nil {
			pageThumbMax = *body.ThumbPagesMaxBytes
		}
		if err := s.settings.SetBudgets(ctx, cacheMax, pageThumbMax); err != nil {
			return fail(c, http.StatusInternalServerError, err)
		}
	}
	if err := s.reconf.Reconfigure(ctx); err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return ok(c, nil)
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go build ./... && go test ./internal/server/ -v -run 'Settings|Budget|ScanOnly'`
Expected: 新 4 例 + 既有 settings 测试全 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/ cmd/tefnut/main.go
git commit -m "feat: settings API reads/writes cache budgets"
```

---

### Task 6: 设置页 UI — 「缓存」区(数字 + 单位下拉)

**Files:**
- Modify: `internal/server/pages.go`(`pageSettings` 传预算)
- Modify: `internal/server/web/templates/settings.html`
- Modify: `internal/server/web/static/js/settings.js`

**Interfaces:**
- Consumes: Task 5 的 `GetBudgets`(server 字段)、PUT body(`cacheMaxBytes`/`thumbPagesMaxBytes` 同时发)
- Produces: `#cache-data` div(`data-cache-max`/`data-pthumb-max`,字节);原生 `<select>` 单位下拉(dropdown.js 全局自动增强为格/PANEL 主题,无需额外接线)

- [ ] **Step 1: `pageSettings` 传预算(internal/server/pages.go)**

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
	cacheMax, pageThumbMax, err := s.settings.GetBudgets(ctx, s.defCacheMax, s.defPageThumbMax)
	if err != nil {
		return fail(c, http.StatusInternalServerError, err)
	}
	return render(c, "settings.html", map[string]any{
		"LibraryPaths": paths, "ScanMode": scan.Mode,
		"ScanInterval": scan.Interval, "ScanDailyTime": scan.DailyTime,
		"CacheMaxBytes": cacheMax, "ThumbPagesMaxBytes": pageThumbMax,
	})
}
```

- [ ] **Step 2: settings.html 增「缓存」区(插在扫描 section 之后、`#scan-data` 之前)**

```html
<section class="card-box">
  <div class="card-head">
    <h2>缓存</h2>
    <button type="submit" form="cacheform">保存</button>
  </div>
  <form id="cacheform" class="scanform">
    <div class="mode-row">
      <label for="cache-max">解压缓存上限</label>
      <input id="cache-max" type="number" min="0" step="1" autocomplete="off">
      <select id="cache-max-unit" title="单位">
        <option value="1048576">MiB</option>
        <option value="1073741824">GiB</option>
      </select>
    </div>
    <div class="mode-row">
      <label for="pthumb-max">页缩略图上限</label>
      <input id="pthumb-max" type="number" min="0" step="1" autocomplete="off">
      <select id="pthumb-max-unit" title="单位">
        <option value="1048576">MiB</option>
        <option value="1073741824">GiB</option>
      </select>
    </div>
    <small class="hint">0 = 不限制。超限时在每次扫描结束后按最旧优先整本清理。</small>
  </form>
</section>
```

并把 `#scan-data` 行后追加:

```html
<div id="cache-data" data-cache-max="{{.CacheMaxBytes}}" data-pthumb-max="{{.ThumbPagesMaxBytes}}" hidden></div>
```

- [ ] **Step 3: settings.js 追加缓存表单逻辑(文件末尾)**

```js
// ---- cache budgets: bytes <-> number + unit (MiB/GiB) ----
const GiB = 1073741824, MiB = 1048576;
const cd = document.getElementById('cache-data').dataset;
const cacheNum = document.getElementById('cache-max');
const cacheUnit = document.getElementById('cache-max-unit');
const pthumbNum = document.getElementById('pthumb-max');
const pthumbUnit = document.getElementById('pthumb-max-unit');

// 整除 GiB 显示 GiB,否则 MiB(不整除时仅显示向上取整,存储不变)
function setSize(numEl, unitEl, bytes) {
  const unit = (bytes > 0 && bytes % GiB === 0) ? GiB : MiB;
  unitEl.value = String(unit);
  numEl.value = String(bytes > 0 ? Math.ceil(bytes / unit) : 0);
  unitEl.dispatchEvent(new Event('change', { bubbles: true })); // sync themed dropdown label
}
setSize(cacheNum, cacheUnit, Number(cd.cacheMax || 0));
setSize(pthumbNum, pthumbUnit, Number(cd.pthumbMax || 0));

document.getElementById('cacheform').addEventListener('submit', (e) => {
  e.preventDefault();
  const cache = Math.round(Number(cacheNum.value)) * Number(cacheUnit.value);
  const pthumb = Math.round(Number(pthumbNum.value)) * Number(pthumbUnit.value);
  if (!Number.isInteger(cache) || cache < 0 || !Number.isInteger(pthumb) || pthumb < 0) {
    alert('缓存上限需为非负整数');
    return;
  }
  fetch('/api/settings', {
    method: 'PUT', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ cacheMaxBytes: cache, thumbPagesMaxBytes: pthumb })
  }).then(r => {
    if (!r.ok) { r.json().then(j => alert(j.message || '保存失败')).catch(() => alert('保存失败')); return; }
    alert('已保存，已触发后台重扫，新上限随本次扫描生效');
  }).catch(() => alert('保存失败'));
});
```

- [ ] **Step 4: 构建 + 全量测试(模板嵌入经 embed,go build 即校验模板语法)**

Run: `go build ./... && go test ./internal/server/`
Expected: PASS;`pageSettings` 相关既有渲染测试不回归

- [ ] **Step 5: Commit**

```bash
git add internal/server/pages.go internal/server/web/
git commit -m "feat: settings page edits cache budgets (number + themed unit dropdown)"
```

---

### Task 7: main.go LoadOrInit + Dockerfile /config + 删除 deploy/config.yaml

**Files:**
- Modify: `cmd/tefnut/main.go`(`config.Load` → `config.LoadOrInit`)
- Modify: `Dockerfile`
- Delete: `deploy/config.yaml`

**Interfaces:**
- Consumes: Task 3 `LoadOrInit`
- Produces: 容器约定——`ENTRYPOINT tefnut -config /config/config.yaml`;镜像内建空 `/config`(tefnut 属主,未挂载也能自写进容器层)

- [ ] **Step 1: main.go 换 LoadOrInit**

```go
	cfg, err := config.LoadOrInit(*cfgPath)
```

- [ ] **Step 2: Dockerfile 修改 runtime 段**

```dockerfile
# ---- runtime ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -S tefnut && adduser -S -G tefnut tefnut \
 && mkdir -p /comics /data /config && chown -R tefnut:tefnut /data /config
COPY --from=build /out/tefnut /usr/local/bin/tefnut
EXPOSE 8086
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8086/healthz || exit 1
USER tefnut
ENTRYPOINT ["tefnut", "-config", "/config/config.yaml"]
```

(即:`mkdir/chown` 增 `/config`;删除 `COPY deploy/config.yaml /etc/tefnut/config.yaml` 行;ENTRYPOINT 路径改 `/config/config.yaml`。)

- [ ] **Step 3: 删除 deploy/config.yaml(全仓唯一引用是上面已删的 COPY 行,spec 已核实)**

```bash
git rm deploy/config.yaml
```

- [ ] **Step 4: 构建验证**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全 PASS
Run(如本机 docker 可用): `docker build -t tefnut-test . && docker run --rm --entrypoint sh tefnut-test -c 'ls -ld /config && tefnut -config /config/config.yaml & sleep 2; cat /config/config.yaml | head -5'`
Expected: `/config` 属 tefnut;自写模板出现(rootPath 校验会因 /comics 空目录存在而通过——镜像里有 mkdir /comics)

- [ ] **Step 5: Commit**

```bash
git add cmd/tefnut/main.go Dockerfile
git commit -m "feat: container reads /config/config.yaml, self-seeds template"
```

---

### Task 8: rainmaker — 生成 ./config/config.yaml,挂载目录,去 env 行

**Files:**
- Modify: `rainmaker`

**Interfaces:**
- Consumes: Task 1(yaml 接受 `2GiB` 写法——提示值原样写入,shell 不做单位换算);Task 7 的 `/config` 约定
- Produces: 宿主 `./config/config.yaml`(已存在则不覆写并跳过预算提示);compose `volumes` 增 `./config:/config`;compose 不再写 `TEFNUT_*` env

- [ ] **Step 1: 预算提示改为条件执行 + 生成配置文件**

把现有 `CACHE_MAX=...`/`PTHUMB_MAX=...` 两行替换为(`ask_size`/`size_ok` 函数保留):

```bash
# --- config dir: ./config/config.yaml, mounted at /config ---
CONFIG_DIR="config"
CONFIG_FILE="$CONFIG_DIR/config.yaml"
if [ -f "$CONFIG_FILE" ]; then
  info "检测到已有 $CONFIG_FILE，沿用其中配置（缓存上限以该文件/设置页为准）。"
else
  CACHE_MAX="$(ask_size '解压缓存上限（漫画页缓存）' "${TEFNUT_CACHE_MAX_BYTES:-2GiB}")"
  PTHUMB_MAX="$(ask_size '页缩略图缓存上限' "${TEFNUT_THUMB_PAGES_MAX_BYTES:-512MiB}")"
  mkdir -p "$CONFIG_DIR"
  {
    printf '# Tefnut 配置文件。修改后 docker compose restart 生效。\n'
    printf '# 缓存上限也可在 Web 设置页修改；设置页保存过的值优先于本文件。\n'
    printf 'library:\n'
    printf '  rootPath: /comics\n'
    printf 'dataDir: /data\n'
    printf 'server:\n'
    printf '  addr: ":8086"\n'
    printf 'thumbnail:\n'
    printf '  width: 400\n'
    printf '  pageWidth: 120\n'
    printf '  pagesMaxBytes: %s # 页缩略图缓存上限；0 = 不限制\n' "$PTHUMB_MAX"
    printf 'cache:\n'
    printf '  maxBytes: %s # 解压缓存上限；0 = 不限制\n' "$CACHE_MAX"
  } >"$CONFIG_FILE"
  info "已生成 $(pwd)/$CONFIG_FILE"
fi
```

- [ ] **Step 2: compose 生成块——volumes 增挂载,删 env 两行**

volumes 三行变四行:

```bash
  printf '    volumes:\n'
  printf '      - ./%s:/config\n' "$CONFIG_DIR"
  printf '      - %s:/comics:ro\n' "$COMICS_PATH"
  printf '      - tefnut-data:/data\n'
```

environment 块只留 TZ(删除 `TEFNUT_CACHE_MAX_BYTES`/`TEFNUT_THUMB_PAGES_MAX_BYTES` 两行 printf)。

- [ ] **Step 3: 语法检查 + shim 干跑**

Run:
```bash
bash -n rainmaker
SB=$(mktemp -d) && mkdir -p "$SB/bin" "$SB/run" "$SB/comics"
printf '#!/bin/sh\ncase "$1" in compose) case "$2" in version) exit 0;; up) exit 1;; esac;; esac\nexit 0\n' > "$SB/bin/docker" && chmod +x "$SB/bin/docker"
cd "$SB/run" && PATH="$SB/bin:$PATH" TEFNUT_CACHE_MAX_BYTES=4GiB bash /Users/liangrenzhi/ws/gomod/Tefnut/rainmaker "$SB/comics" < /dev/null; echo "exit=$?"
cat config/config.yaml && cat docker-compose.yml
# 二跑:已有 config.yaml 不覆写、不再提示
PATH="$SB/bin:$PATH" bash /Users/liangrenzhi/ws/gomod/Tefnut/rainmaker "$SB/comics" < /dev/null; grep maxBytes config/config.yaml
```
Expected: config.yaml 含 `maxBytes: 4GiB`;compose 含 `./config:/config`、无 `TEFNUT_` 行;二跑 config.yaml 内容不变

- [ ] **Step 4: Commit**

```bash
git add rainmaker
git commit -m "feat: rainmaker generates mounted ./config/config.yaml (no compose env)"
```

---

### Task 9: README 更新 + 全量收尾验证

**Files:**
- Modify: `README.md`

**Interfaces:**
- Consumes: 全部前序任务的最终行为

- [ ] **Step 1: README「Docker 部署」段更新**

- 「卷与端口」列表**新增一条**(放在 `/comics` 之前):
  ```markdown
  - `/config`（挂载 `./config`）— `config.yaml` 所在；首次启动自动生成带注释模板，宿主机直接编辑，`docker compose restart` 生效。
  ```
- 两个 `TEFNUT_*` 条目末尾补一句:`设置页保存过的值优先于 env 与配置文件。`
- 非交互示例后补一句:`缓存上限也可启动后在「设置」页修改，保存即生效。`

- [ ] **Step 2: 全量验证**

Run: `go build ./... && go vet ./... && go test ./... && bash -n rainmaker`
Expected: 全 PASS

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: config dir mount and runtime-editable cache budgets"
```

---

## 收尾(计划外,人工触发)

- `/verify` 真机验证:首启自写 `/config/config.yaml`;设置页改预算保存 → 淘汰日志;重启后 UI 值保持;env 仅在 UI 未保存时生效。
- push + PR(基于 PR #8 之上,待其合并后 rebase 到 main)。
