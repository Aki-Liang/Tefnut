# 漫画库目录选择弹窗 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 设置页添加漫画库路径时，用服务器端目录浏览弹窗取代手动输入绝对路径。

**Architecture:** 新增只读接口 `GET /api/fs/dirs` 列出 `allowedRoots` 内的目录（复用 `PathWithinRoots` 做 fail-closed 越界防护）；前端把路径文本框换成触发按钮，点击弹出自绘 格/PANEL 风格模态弹窗，逐级浏览或粘贴路径快速跳转，选定后回填表单，提交流程与服务器校验不变。

**Tech Stack:** Go 1.x + echo v4 + httptest（后端）；原生 DOM + fetch、无构建链（前端）。

**Spec:** `docs/superpowers/specs/2026-07-03-library-dir-picker-design.md`

## Global Constraints

- 路径校验一律走现有 `PathWithinRoots`（`internal/server/pathguard.go`），错误或 `false` 一律拒绝（fail closed）。
- 越界错误消息与 `apiAddPath` 完全一致：`目录必须在允许的库根之内`。
- API 响应用现有 `ok()` / `fail()` 助手（`internal/server/response.go`）；4xx 消息必须用户可读（中文）。
- 只暴露目录名与路径，不暴露文件、大小、权限；`.` 开头的隐藏目录不列出。
- 前端不用任何原生弹层控件（`datalist`/`<dialog>` 原生样式等），弹窗自绘，视觉沿用 `--panel`/`--line`/`--tone`/硬偏移阴影这套 dd-menu 语言。
- 前端错误在弹窗内联展示，不用 `alert`。
- 提交信息用 conventional commits（`feat:`/`test:` 等），不带署名尾注。

---

### Task 1: 后端 `GET /api/fs/dirs` 目录列举接口

**Files:**
- Create: `internal/server/api_fs.go`
- Create: `internal/server/api_fs_test.go`
- Modify: `internal/server/server.go`（`Register` 的 `/api` 组内加一行路由）

**Interfaces:**
- Consumes: `PathWithinRoots(dir string, roots []string) (bool, error)`（pathguard.go）、`ok(c, data)` / `fail(c, status, err)`（response.go）、`s.allowedRoots []string`（Server 字段）。
- Produces: 路由 `GET /api/fs/dirs`。无 `path` 参数 → `{"code":0,"data":{"roots":[{"name","path"}]}}`；`path=<abs>` → `{"code":0,"data":{"path":<清理后的绝对路径>,"parent":<上级或"">,"dirs":[{"name","path"}]}}`。类型 `dirEntryDTO{Name, Path string}`、`fsRootsDTO{Roots []dirEntryDTO}`、`fsDirsDTO{Path, Parent string; Dirs []dirEntryDTO}`（Task 2 的前端按此 JSON 形状消费）。

- [ ] **Step 1: 写失败测试**

创建 `internal/server/api_fs_test.go`（与现有 `server_test.go` 同包，复用其 `newTestServer`）：

```go
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
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/server/ -run TestApiFsDirs -v`
Expected: 编译失败 —— `undefined: fsDirsDTO`（实现文件尚不存在）。

- [ ] **Step 3: 写最小实现**

创建 `internal/server/api_fs.go`：

```go
package server

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/labstack/echo/v4"
)

type dirEntryDTO struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type fsRootsDTO struct {
	Roots []dirEntryDTO `json:"roots"`
}

type fsDirsDTO struct {
	Path   string        `json:"path"`
	Parent string        `json:"parent"`
	Dirs   []dirEntryDTO `json:"dirs"`
}

// apiFsDirs lists server-side directories for the library path picker.
// Without ?path it returns the configured allowed roots; with ?path it lists
// that directory's immediate subdirectories. Every path is validated against
// allowedRoots with the same fail-closed guard as apiAddPath, and only
// directory names are exposed — never files or metadata.
func (s *Server) apiFsDirs(c echo.Context) error {
	q := strings.TrimSpace(c.QueryParam("path"))
	if q == "" {
		return ok(c, fsRootsDTO{Roots: s.existingRoots()})
	}
	if !filepath.IsAbs(q) {
		return fail(c, http.StatusBadRequest, errors.New("path 必须是绝对路径"))
	}
	abs := filepath.Clean(q)
	if within, err := PathWithinRoots(abs, s.allowedRoots); err != nil || !within {
		return fail(c, http.StatusBadRequest, errors.New("目录必须在允许的库根之内"))
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return fail(c, http.StatusBadRequest, errors.New("目录不可读"))
	}
	dirs := make([]dirEntryDTO, 0, len(entries))
	for _, ent := range entries {
		if strings.HasPrefix(ent.Name(), ".") {
			continue
		}
		p := filepath.Join(abs, ent.Name())
		if !entryIsDir(ent, p) {
			continue
		}
		dirs = append(dirs, dirEntryDTO{Name: ent.Name(), Path: p})
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	return ok(c, fsDirsDTO{Path: abs, Parent: s.parentWithinRoots(abs), Dirs: dirs})
}

// existingRoots returns the configured allowedRoots that exist as directories,
// in config order, for the picker's initial view.
func (s *Server) existingRoots() []dirEntryDTO {
	roots := make([]dirEntryDTO, 0, len(s.allowedRoots))
	for _, r := range s.allowedRoots {
		info, err := os.Stat(r)
		if err != nil || !info.IsDir() {
			continue
		}
		roots = append(roots, dirEntryDTO{Name: filepath.Base(r), Path: r})
	}
	return roots
}

// parentWithinRoots returns abs's parent directory, or "" when the parent
// leaves every allowed root, so the UI falls back to the roots view instead
// of climbing out of the jail.
func (s *Server) parentWithinRoots(abs string) string {
	parent := filepath.Dir(abs)
	if parent == abs {
		return ""
	}
	if within, err := PathWithinRoots(parent, s.allowedRoots); err != nil || !within {
		return ""
	}
	return parent
}

// entryIsDir reports whether ent names a directory, following symlinks so a
// symlinked library folder still shows up in the picker. Entering it remains
// guarded by PathWithinRoots on the follow-up request.
func entryIsDir(ent os.DirEntry, path string) bool {
	if ent.IsDir() {
		return true
	}
	if ent.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
```

在 `internal/server/server.go` 的 `Register` 中，`api.POST("/settings/paths", s.apiAddPath)` 一行之前加：

```go
	api.GET("/fs/dirs", s.apiFsDirs)
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/server/ -run TestApiFsDirs -v`
Expected: 5 个测试全部 PASS（symlink 测试在不支持的平台上 SKIP）。

Run: `go test ./...`
Expected: 全部 PASS（确认没有破坏现有测试）。

- [ ] **Step 5: 提交**

```bash
git add internal/server/api_fs.go internal/server/api_fs_test.go internal/server/server.go
git commit -m "feat: add GET /api/fs/dirs for server-side directory browsing"
```

---

### Task 2: 前端目录选择弹窗与表单改造

**Files:**
- Create: `internal/server/web/static/js/dirpicker.js`
- Modify: `internal/server/web/templates/settings.html`
- Modify: `internal/server/web/static/js/settings.js`
- Modify: `internal/server/web/static/css/app.css`（文件末尾追加一段）

**Interfaces:**
- Consumes: `GET /api/fs/dirs`（Task 1 的 JSON 形状：`data.roots[{name,path}]` / `data.{path,parent,dirs[{name,path}]}`；失败时 `{code:-1,message}`）。
- Produces: 全局函数 `window.openDirPicker(onPick)` —— 打开弹窗，用户确认后以所选绝对路径字符串调用 `onPick(path)`；取消/关闭则不调用。`settings.html` 中触发按钮 `#np-pick`（`data-path` 存所选值）。

前端无构建链与 JS 测试基建（与现有 settings.js 等一致，spec 已确认），本任务以 `go build` + 运行服务后的手动/浏览器验证收尾。

- [ ] **Step 1: 修改 `settings.html` —— 移除路径输入框，加触发按钮与脚本引用**

`#addpath` 表单改为（替换原 `<input id="np-path" …>` 行）：

```html
  <form id="addpath" class="row">
    <button type="button" id="np-pick" class="pick-trigger empty">选择库目录…</button>
    <input id="np-name" placeholder="显示名（默认取末段）" autocomplete="off">
    <button>添加</button>
  </form>
```

底部 scripts 块加载 dirpicker（在 settings.js 之前）：

```html
{{define "scripts"}}<script src="/static/js/dirpicker.js"></script><script src="/static/js/settings.js"></script>{{end}}
```

- [ ] **Step 2: 创建 `dirpicker.js` —— 自绘模态目录浏览器**

创建 `internal/server/web/static/js/dirpicker.js`：

```js
// Server-side directory picker modal in the 格/PANEL style. The browser's
// native folder dialog can only pick client-side folders (and hides absolute
// paths), so library paths are chosen by browsing the server's allowedRoots
// via GET /api/fs/dirs. Exposes window.openDirPicker(onPick).
(function () {
  let overlay = null;
  let onPick = null;
  let current = null;   // absolute path being viewed; null = roots view
  let rootCount = 0;    // >1 roots → 上级 at a root returns to the roots view
  let els = null;

  function h(tag, cls, text) {
    const el = document.createElement(tag);
    if (cls) el.className = cls;
    if (text !== undefined) el.textContent = text;
    return el;
  }

  function build() {
    overlay = h('div', 'dp-overlay');
    overlay.hidden = true;
    const modal = h('div', 'dp-modal');

    const head = h('div', 'dp-head');
    const title = h('h3', '', '选择库目录');
    const close = h('button', 'dp-close', '×');
    close.type = 'button';
    close.setAttribute('aria-label', '关闭');
    head.append(title, close);

    const jumpRow = h('div', 'dp-jump-row');
    const jump = h('input');
    jump.placeholder = '输入绝对路径回车直达';
    jump.autocomplete = 'off';
    jump.spellcheck = false;
    const go = h('button', '', '前往');
    go.type = 'button';
    jumpRow.append(jump, go);

    const cur = h('div', 'dp-path-row');
    const up = h('button', 'dp-up', '上级');
    up.type = 'button';
    const curPath = h('span', 'dp-cur', '');
    cur.append(up, curPath);

    const list = h('ul', 'dp-list');
    const err = h('div', 'dp-err', '');

    const foot = h('div', 'dp-foot');
    const cancel = h('button', '', '取消');
    cancel.type = 'button';
    const confirm = h('button', 'dp-confirm', '选定当前目录');
    confirm.type = 'button';
    foot.append(cancel, confirm);

    modal.append(head, jumpRow, cur, list, err, foot);
    overlay.appendChild(modal);
    document.body.appendChild(overlay);
    els = { jump, go, up, curPath, list, err, confirm };

    close.addEventListener('click', hide);
    cancel.addEventListener('click', hide);
    overlay.addEventListener('click', (e) => { if (e.target === overlay) hide(); });
    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && !overlay.hidden) hide();
    });
    confirm.addEventListener('click', () => {
      if (!current) return;
      const cb = onPick;
      hide();
      if (cb) cb(current);
    });
    up.addEventListener('click', () => {
      if (up.dataset.to) loadDir(up.dataset.to);
      else loadRoots(false);
    });
    go.addEventListener('click', () => { if (jump.value.trim()) loadDir(jump.value.trim()); });
    jump.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') { e.preventDefault(); if (jump.value.trim()) loadDir(jump.value.trim()); }
    });
    list.addEventListener('click', (e) => {
      const li = e.target.closest('li[data-path]');
      if (li) loadDir(li.dataset.path);
    });
  }

  function hide() { overlay.hidden = true; onPick = null; }

  function setError(msg) { els.err.textContent = msg || ''; }

  function api(url) {
    return fetch(url).then((r) =>
      r.json().then((j) => {
        if (!r.ok || j.code !== 0) throw new Error(j.message || '加载失败');
        return j.data;
      })
    );
  }

  function renderList(items, enterable) {
    els.list.innerHTML = '';
    if (items.length === 0) {
      els.list.appendChild(h('li', 'dp-empty', enterable ? '无子目录' : '没有可用的库根，请检查 library.allowedRoots 配置'));
      return;
    }
    items.forEach((d) => {
      const li = h('li', '', d.name);
      li.dataset.path = d.path;
      els.list.appendChild(li);
    });
  }

  // Roots view: current = null, confirm disabled. With a single root we jump
  // straight into it (per spec) unless the user explicitly navigated up.
  function loadRoots(autoEnter) {
    setError('');
    api('/api/fs/dirs')
      .then((data) => {
        rootCount = data.roots.length;
        if (autoEnter && rootCount === 1) { loadDir(data.roots[0].path); return; }
        current = null;
        els.curPath.textContent = '库根列表';
        els.up.disabled = true;
        els.up.dataset.to = '';
        els.confirm.disabled = true;
        renderList(data.roots, false);
      })
      .catch((e) => setError(e.message));
  }

  function loadDir(path) {
    setError('');
    api('/api/fs/dirs?path=' + encodeURIComponent(path))
      .then((data) => {
        current = data.path;
        els.curPath.textContent = data.path;
        els.up.dataset.to = data.parent || '';
        // at a root: multi-root setups go back to the roots view; with a
        // single root there is nowhere up to go.
        els.up.disabled = !data.parent && rootCount <= 1;
        els.confirm.disabled = false;
        els.jump.value = '';
        renderList(data.dirs, true);
      })
      .catch((e) => setError(e.message));
  }

  window.openDirPicker = function (cb) {
    if (!overlay) build();
    onPick = cb;
    setError('');
    overlay.hidden = false;
    loadRoots(true);
    els.jump.focus();
  };
})();
```

- [ ] **Step 3: 修改 `settings.js` —— 接上触发按钮，移除旧输入框逻辑**

把 settings.js 中这两段（原 33-41 行的路径输入自动填名逻辑）：

```js
// auto-fill name from path basename
document.getElementById('np-path').addEventListener('input', (e) => {
  const nameEl = document.getElementById('np-name');
  if (!nameEl.dataset.touched) {
    const parts = e.target.value.replace(/\/+$/, '').split('/');
    nameEl.value = parts[parts.length - 1] || '';
  }
});
document.getElementById('np-name').addEventListener('input', (e) => { e.target.dataset.touched = '1'; });
```

替换为：

```js
// pick a library directory via the server-side dir picker modal; the picked
// path lands on the trigger button, and the display name auto-fills from the
// basename unless the user already edited it.
const pickBtn = document.getElementById('np-pick');
pickBtn.addEventListener('click', () => {
  window.openDirPicker((path) => {
    pickBtn.dataset.path = path;
    pickBtn.textContent = path;
    pickBtn.classList.remove('empty');
    const nameEl = document.getElementById('np-name');
    if (!nameEl.dataset.touched) {
      const parts = path.replace(/\/+$/, '').split('/');
      nameEl.value = parts[parts.length - 1] || '';
    }
  });
});
document.getElementById('np-name').addEventListener('input', (e) => { e.target.dataset.touched = '1'; });
```

并把提交处理里的取值一行：

```js
  const path = document.getElementById('np-path').value.trim();
```

替换为：

```js
  const path = (pickBtn.dataset.path || '').trim();
```

（提交后 `location.reload()` 会重置表单状态，无需手动清空。）

- [ ] **Step 4: `app.css` 末尾追加弹窗样式**

```css
/* ============================================================
   Directory picker — server-side folder browser for library paths.
   Same panel language as the dd-menu: ink gutters, halftone, hard shadow.
   ============================================================ */
.pick-trigger {
  font-family: var(--font-mono); text-transform: none; letter-spacing: 0;
  max-width: 360px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.pick-trigger.empty { color: var(--muted); }

.dp-overlay {
  position: fixed; inset: 0; z-index: 80; padding: 20px;
  background: rgba(10, 13, 17, 0.62);
  display: flex; align-items: center; justify-content: center;
}
.dp-modal {
  width: 560px; max-width: 100%; max-height: 80vh;
  display: flex; flex-direction: column;
  background-color: var(--panel); background-image: var(--tone); background-size: 11px 11px;
  border: 2px solid var(--line); border-radius: 6px;
  box-shadow: 6px 6px 0 rgba(0, 0, 0, 0.38);
}
.dp-modal input, .dp-modal button {
  background: var(--ink); color: var(--paper);
  border: 2px solid var(--line); border-radius: 6px; padding: 7px 10px; font-size: 13px;
}
.dp-modal button { cursor: pointer; background: var(--panel-2); font-family: var(--font-display); letter-spacing: .03em; text-transform: uppercase; }
.dp-modal button:hover:not(:disabled) { border-color: var(--seal); }
.dp-modal button:disabled { opacity: 0.55; cursor: default; }
.dp-modal input:focus { border-color: var(--muted); }

.dp-head { display: flex; align-items: center; justify-content: space-between; padding: 12px 14px; border-bottom: 2px solid var(--line); }
.dp-head h3 { margin: 0; font-family: var(--font-display); font-weight: 400; letter-spacing: 0.02em; font-size: 18px; }
.dp-close { line-height: 1; padding: 4px 10px; }

.dp-jump-row { display: flex; gap: 8px; padding: 12px 14px 0; }
.dp-jump-row input { flex: 1; font-family: var(--font-mono); }

.dp-path-row { display: flex; align-items: center; gap: 10px; padding: 10px 14px 0; }
.dp-cur { flex: 1; font-family: var(--font-mono); font-size: 12px; color: var(--muted); word-break: break-all; }

.dp-list {
  flex: 1; min-height: 180px; overflow-y: auto; scrollbar-width: thin;
  list-style: none; margin: 10px 8px; padding: 0;
}
.dp-list li { padding: 8px 12px 8px 30px; border-radius: 4px; position: relative; font-family: var(--font-mono); font-size: 13px; cursor: pointer; }
.dp-list li[data-path]:hover { background: var(--panel-2); }
.dp-list li[data-path]::before { /* folder tab glyph, same square accent family as the dd seal */
  content: ""; position: absolute; left: 12px; top: 50%; transform: translateY(-50%);
  width: 9px; height: 9px; border: 2px solid var(--muted); border-radius: 0 2px 2px 2px;
}
.dp-list li.dp-empty { color: var(--muted); cursor: default; padding-left: 12px; }

.dp-err { color: var(--warn); font-size: 12px; padding: 0 14px; min-height: 1.2em; }
.dp-foot { display: flex; justify-content: flex-end; gap: 8px; padding: 12px 14px; border-top: 2px solid var(--line); }
```

- [ ] **Step 5: 构建与全量测试**

Run: `go build ./... && go test ./...`
Expected: 构建成功，全部测试 PASS。

- [ ] **Step 6: 运行验证（真实配置）**

用用户的真实配置启动服务（**不要**编造示例库；参见项目惯例——运行/演示时用真实数据）：

Run: `go run ./cmd/tefnut`（按 README/config 目录的现有配置）
浏览器打开 `/settings` 验证：

1. 「选择库目录…」按钮弹出弹窗；单根时直接进入根目录，多根时先列根。
2. 点击子目录逐级进入，「上级」正确回退；根处多根回到根列表、单根置灰。
3. 粘贴合法绝对路径回车直达；粘贴根外/不存在路径显示内联错误且视图不变。
4. 「选定当前目录」回填按钮文本与 `data-path`，显示名自动填末段；「添加」成功入库。
5. Esc、遮罩点击、取消、× 均能关闭。

- [ ] **Step 7: 提交**

```bash
git add internal/server/web/templates/settings.html internal/server/web/static/js/dirpicker.js internal/server/web/static/js/settings.js internal/server/web/static/css/app.css
git commit -m "feat: pick library dirs via server-side browser modal"
```
