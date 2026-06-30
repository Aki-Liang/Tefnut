# Tefnut Reader Display-Modes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add three per-comic reader display modes — single-page paginated (current), vertical continuous (webtoon), and two-page spread — switchable live from the reader.

**Architecture:** A new per-comic `nodes.display_mode` column (idempotent migration, exposed via comic-detail + PATCH) drives a mode-aware `reader.js` rewrite. `setMode/goTo/advance/back` dispatch by mode; single/spread rebuild the stage with 1–2 `<img>`, continuous builds one lazy-loaded vertical scroll column. A `#modetoggle` select and (spread-only) a page-step toggle live in the reader bar.

**Tech Stack:** Go 1.24, echo v4, modernc.org/sqlite, html/template + go:embed, vanilla JS (IntersectionObserver, localStorage).

## Global Constraints

Every task's requirements implicitly include this section.

- **Display mode is per-comic:** column `display_mode TEXT NOT NULL DEFAULT 'single'` on `nodes`, values `'single'|'continuous'|'spread'`. Idempotent migration via the existing `ensureColumn` helper AND the column in the `CREATE TABLE` for fresh DBs. The column is added in the SAME LAST position in three in-sync places: `nodeCols`, the `Search` SELECT list, and `scanNode`'s Scan targets.
- **Modes:**
  - `single`: one page, paginated, direction-aware (existing behavior).
  - `spread`: 1–2 pages side by side. Pairing (step=2, default): `[0]` alone, then `[1,2][3,4]…`. Page-step toggle (`2`|`1`, default 2, persisted in `localStorage` key `spreadStep`): step=1 slides a two-page window by one page (`[cur,cur+1]`). Direction orders the two images: LTR → smaller page on the left; RTL → smaller page on the right.
  - `continuous`: all pages stacked vertically in a scroll container, lazy-loaded (IntersectionObserver); current page = topmost mostly-visible page; progress saved on scroll (debounced); side click-zones hidden; the **direction toggle is hidden** in this mode (vertical has no LTR/RTL).
- **Logical page index 0-based** in all modes; progress saved + restored on load in all modes.
- The thumbnail strip, author/rating/tag editing, and strip-collapse work in all modes.
- Reading direction (`reading_direction`) applies only to single & spread.
- API JSON key is `displayMode` (lowercase camelCase). PATCH validates `∈ {single,continuous,spread}` → 400 otherwise.
- **Style:** validate input at boundaries; never silently swallow errors; wrap errors with package context; frontend `fetch` surfaces failures (alert on user actions). Small focused files.
- Commit prefixes: `feat:` `fix:` `refactor:` `docs:` `test:` `chore:`.

## File Structure

```
internal/store/store.go            MODIFY  add display_mode to nodes CREATE TABLE + ensureColumn call
internal/store/types.go            MODIFY  Node.DisplayMode field
internal/store/node_repo.go        MODIFY  nodeCols + Search list + scanNode + UpdateDisplayMode
internal/store/node_repo_test.go   MODIFY  default + update tests
internal/server/api_nodes.go       MODIFY  comicDetailDTO.DisplayMode
internal/server/api_meta.go        MODIFY  metaReq.DisplayMode + validation
internal/server/server_test.go     MODIFY  detail + PATCH + reader-render tests
internal/server/web/templates/reader.html   MODIFY  data-mode, #modetoggle, #stepbtn, empty stage
internal/server/web/static/js/reader.js      REWRITE mode-aware framework (single/spread/continuous)
internal/server/web/static/css/app.css        MODIFY  spread/continuous/mode-control styles
```

---

## Task 1: Store — display_mode column + repo

**Files:**
- Modify: `internal/store/store.go`, `internal/store/types.go`, `internal/store/node_repo.go`, `internal/store/node_repo_test.go`

**Interfaces:**
- Produces: `store.Node.DisplayMode string`; `(*NodeRepo) UpdateDisplayMode(ctx context.Context, id int64, mode string) error`; `nodes` rows carry `display_mode` (default `'single'`).

> This mirrors the existing `reading_direction` column exactly. The codebase already has `ensureColumn(db, table, column, ddl)` in `internal/store/migrate.go`, a `reading_direction` column wired through `nodeCols`/`Search`/`scanNode`, and `UpdateReadingDirection`. Follow that pattern for `display_mode`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/store/node_repo_test.go`:

```go
func TestNodeDefaultDisplayMode(t *testing.T) {
	ctx := context.Background()
	r := NewNodeRepo(openTemp(t))
	n := mkNode(t, r, 0, "c", "/lib/c.zip", NodeComic)
	got, _ := r.Get(ctx, n.ID)
	if got.DisplayMode != "single" {
		t.Fatalf("default display_mode = %q, want single", got.DisplayMode)
	}
}

func TestUpdateDisplayMode(t *testing.T) {
	ctx := context.Background()
	r := NewNodeRepo(openTemp(t))
	n := mkNode(t, r, 0, "c", "/lib/c.zip", NodeComic)
	if err := r.UpdateDisplayMode(ctx, n.ID, "spread"); err != nil {
		t.Fatal(err)
	}
	got, _ := r.Get(ctx, n.ID)
	if got.DisplayMode != "spread" {
		t.Fatalf("display_mode = %q, want spread", got.DisplayMode)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/store/ -run DisplayMode -v`
Expected: FAIL (undefined `DisplayMode` / `UpdateDisplayMode`).

- [ ] **Step 3: Add the column to schema + migration**

In `internal/store/store.go`, add to the `nodes` `CREATE TABLE` (right after the `reading_direction` line):

```sql
  display_mode TEXT NOT NULL DEFAULT 'single',
```

And in `Open`, right after the existing `ensureColumn(..., "reading_direction", ...)` call:

```go
	if err := ensureColumn(sqldb, "nodes", "display_mode", "TEXT NOT NULL DEFAULT 'single'"); err != nil {
		sqldb.Close()
		return nil, err
	}
```

- [ ] **Step 4: Add the field + repo method + scan sync**

In `internal/store/types.go`, add to `Node` (right after `ReadingDirection string`):

```go
	DisplayMode string
```

In `internal/store/node_repo.go`:
- Append `, display_mode` to the END of the `nodeCols` const (after `reading_direction`).
- In `Search`, append `, n.display_mode` to the END of the explicit column list (after `n.reading_direction`, before `FROM nodes n`).
- In `scanNode`, append `&n.DisplayMode` as the LAST Scan target (after `&n.ReadingDirection`).
- Add the method:

```go
func (r *NodeRepo) UpdateDisplayMode(ctx context.Context, id int64, mode string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE nodes SET display_mode=?, updated_at=? WHERE id=?`, mode, time.Now().Unix(), id)
	if err != nil {
		return fmt.Errorf("store: update display_mode %d: %w", id, err)
	}
	return nil
}
```

- [ ] **Step 5: Run tests + build**

Run: `go build ./... && go test ./internal/store/...`
Expected: build clean, all store tests PASS (existing + 2 new). The three-place column sync (nodeCols / Search / scanNode) must line up or existing node reads break.

- [ ] **Step 6: Commit**

```bash
git add internal/store
git commit -m "feat: add per-comic display_mode column and migration"
```

---

## Task 2: API — display mode in detail + PATCH

**Files:**
- Modify: `internal/server/api_nodes.go`, `internal/server/api_meta.go`, `internal/server/server_test.go`

**Interfaces:**
- Consumes: `store.NodeRepo.UpdateDisplayMode`.
- Produces: `comicDetailDTO.DisplayMode` (json `displayMode`); PATCH `/api/comics/:id` accepts `displayMode` (`single`/`continuous`/`spread`).

- [ ] **Step 1: Write failing tests**

Append to `internal/server/server_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/server/ -run DisplayMode -v`
Expected: FAIL.

- [ ] **Step 3: Add to comic detail**

In `internal/server/api_nodes.go`, add to `comicDetailDTO` (after `ReadingDirection`):

```go
	DisplayMode      string       `json:"displayMode"`
```

and set it in `apiComicDetail`'s returned struct: `DisplayMode: n.DisplayMode,`.

- [ ] **Step 4: Add to PATCH**

In `internal/server/api_meta.go`, add to `metaReq` (after `ReadingDirection *string`):

```go
	DisplayMode *string `json:"displayMode"`
```

In `apiUpdateMeta`, after the `ReadingDirection` handling block, add:

```go
	if body.DisplayMode != nil {
		m := *body.DisplayMode
		if m != "single" && m != "continuous" && m != "spread" {
			return fail(c, http.StatusBadRequest, errors.New("displayMode must be single, continuous, or spread"))
		}
		if err := s.nodes.UpdateDisplayMode(ctx, id, m); err != nil {
			return fail(c, http.StatusInternalServerError, err)
		}
	}
```

(A PATCH may carry only `displayMode`, only `readingDirection`, only author/rating, or any mix; each is independent.)

- [ ] **Step 5: Run tests + build**

Run: `go build ./... && go test ./internal/server/...`
Expected: PASS (3 new).

- [ ] **Step 6: Commit**

```bash
git add internal/server
git commit -m "feat: expose and update per-comic display mode via API"
```

---

## Task 3: Reader — mode-aware rewrite (single / spread / continuous)

**Files:**
- Modify: `internal/server/web/templates/reader.html`
- Rewrite: `internal/server/web/static/js/reader.js`
- Modify: `internal/server/web/static/css/app.css`
- Test: `internal/server/server_test.go` (render test)

**Interfaces:**
- Consumes: `data-mode`/`data-dir` from the template, `GET /api/comics/:id/pages/:n` + `/thumb`, `PATCH /api/comics/:id {displayMode|readingDirection}`, `PUT /api/comics/:id/progress`.

> The implementer MUST read the current `reader.html` and `reader.js` first. `reader.js` is REPLACED with the complete version below (it re-implements all existing behavior — paging, progress, preload, thumbnail strip, strip collapse, direction toggle, author/rating/tag editing — plus the three modes). Do not lose the meta-editing or progress logic; the version below already includes it.

- [ ] **Step 1: Write the failing render test**

Append to `internal/server/server_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/server/ -run TestReaderHasModeToggle -v`
Expected: FAIL.

- [ ] **Step 3: Update reader.html**

Edit `internal/server/web/templates/reader.html`: add `data-mode="{{.DisplayMode}}"` to the `#reader` element; EMPTY the `.reader-stage` (JS now builds its content); add `#modetoggle` and `#stepbtn` into the `.reader-bar` next to `#dirtoggle`. The `{{define}}` blocks (`title`/`sidebar`/`mainclass`/`content`/`scripts`) and the existing reader-bar controls (返回, 上一页 #prevbtn, #counter, 下一页 #nextbtn, meta author/rating, #tags, #addtag, #dirtoggle) and `#thumbstrip` stay. Concretely, set the reader root + stage to:

```html
<div id="reader" data-id="{{.ID}}" data-pages="{{.PageCount}}" data-start="{{.LastPage}}" data-dir="{{.ReadingDirection}}" data-mode="{{.DisplayMode}}">
  <div class="reader-stage"></div>
```

and inside `.reader-bar`, right after the `#dirtoggle` button, add:

```html
      <select class="pagebtn" id="modetoggle" title="展示方式">
        <option value="single">单页</option>
        <option value="continuous">连续</option>
        <option value="spread">双页</option>
      </select>
      <button class="pagebtn" id="stepbtn" title="翻页步进">步进: <span id="steplabel">两页</span></button>
```

- [ ] **Step 4: Replace reader.js**

Replace the ENTIRE contents of `internal/server/web/static/js/reader.js` with:

```js
const el = document.getElementById('reader');
const id = el.dataset.id;
const total = parseInt(el.dataset.pages, 10);
let cur = Math.min(parseInt(el.dataset.start, 10) || 0, Math.max(total - 1, 0));
let dir = el.dataset.dir || 'ltr';
let mode = el.dataset.mode || 'single';
let spreadStep = localStorage.getItem('spreadStep') === '1' ? 1 : 2;

const stage = document.querySelector('.reader-stage');
const counter = document.getElementById('counter');
const thumbsEl = document.getElementById('thumbs');
const stripEl = document.getElementById('thumbstrip');

let contLazyObs = null, contPageObs = null;

function pageURL(n) { return `/api/comics/${id}/pages/${n}`; }
function clampPage(n) { return Math.max(0, Math.min(n, total - 1)); }
function preload(n) { if (n >= 0 && n < total) { const i = new Image(); i.src = pageURL(n); } }

// ---- progress ----
let saveTimer = null;
function saveProgress(n) {
  clearTimeout(saveTimer);
  saveTimer = setTimeout(() => {
    fetch(`/api/comics/${id}/progress`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ page: n })
    }).catch(() => {});
  }, 400);
}

// ---- thumbnail strip (mode-agnostic) ----
const thumbObserver = new IntersectionObserver((entries) => {
  entries.forEach((e) => {
    if (e.isIntersecting) { const im = e.target; if (!im.src && im.dataset.src) im.src = im.dataset.src; thumbObserver.unobserve(im); }
  });
}, { root: stripEl });

function buildStrip() {
  thumbsEl.innerHTML = '';
  thumbsEl.classList.toggle('rtl', dir === 'rtl');
  for (let i = 0; i < total; i++) {
    const fig = document.createElement('div'); fig.className = 'thumb'; fig.dataset.page = i;
    const im = document.createElement('img'); im.dataset.src = `/api/comics/${id}/pages/${i}/thumb`; im.alt = `第 ${i + 1} 页`;
    fig.appendChild(im); fig.onclick = () => goTo(i); thumbsEl.appendChild(fig); thumbObserver.observe(im);
  }
}
function updateStripActive() {
  const figs = thumbsEl.children;
  for (let i = 0; i < figs.length; i++) figs[i].classList.toggle('active', i === cur);
  const a = figs[cur]; if (a) a.scrollIntoView({ inline: 'center', block: 'nearest', behavior: 'smooth' });
}

// ---- navigation (mode-aware) ----
function spreadStart(n) { if (n <= 0) return 0; return n % 2 === 1 ? n : n - 1; } // [0],[1,2],[3,4]...
function advance() {
  if (mode === 'spread') {
    if (spreadStep === 1) { if (cur + 1 < total) goTo(cur + 1); }
    else { const ns = cur === 0 ? 1 : cur + 2; if (ns < total) goTo(ns); }
  } else { goTo(cur + 1); }
}
function back() {
  if (mode === 'spread') {
    if (spreadStep === 1) { if (cur > 0) goTo(cur - 1); }
    else { goTo(cur <= 1 ? 0 : cur - 2); }
  } else { goTo(cur - 1); }
}

function bindZones() {
  const prev = document.getElementById('prev'), next = document.getElementById('next');
  if (!prev || !next) return;
  if (dir === 'rtl') { prev.onclick = advance; next.onclick = back; }
  else { prev.onclick = back; next.onclick = advance; }
}

// ---- per-mode rendering ----
function renderSingle() {
  stage.innerHTML = '<button class="nav prev" id="prev" aria-label="上一页">‹</button><img id="page" alt="page"><button class="nav next" id="next" aria-label="下一页">›</button>';
  document.getElementById('page').src = pageURL(cur);
  bindZones(); preload(cur + 1);
}
function renderSpread() {
  let start = spreadStep === 2 ? spreadStart(cur) : cur;
  cur = start;
  let pages;
  if (spreadStep === 2 && start === 0) pages = [0];
  else pages = (start + 1 < total) ? [start, start + 1] : [start];
  const ordered = dir === 'rtl' ? pages.slice().reverse() : pages;
  const imgs = ordered.map((p) => `<img src="${pageURL(p)}" alt="page">`).join('');
  stage.innerHTML = '<button class="nav prev" id="prev" aria-label="上一页">‹</button><div class="spread">' + imgs + '</div><button class="nav next" id="next" aria-label="下一页">›</button>';
  bindZones(); preload(start + 2);
}
function buildContinuous() {
  stage.innerHTML = '<div class="continuous" id="cont"></div>';
  const cont = document.getElementById('cont');
  for (let i = 0; i < total; i++) {
    const im = document.createElement('img'); im.className = 'cpage'; im.dataset.page = i;
    im.dataset.src = pageURL(i); im.alt = `第 ${i + 1} 页`;
    cont.appendChild(im);
  }
  contLazyObs = new IntersectionObserver((es) => {
    es.forEach((e) => { if (e.isIntersecting) { const im = e.target; if (!im.src && im.dataset.src) im.src = im.dataset.src; } });
  }, { root: cont, rootMargin: '300px' });
  contPageObs = new IntersectionObserver((es) => {
    es.forEach((e) => {
      if (e.isIntersecting) { cur = parseInt(e.target.dataset.page, 10); counter.textContent = `${cur + 1} / ${total}`; saveProgress(cur); updateStripActive(); }
    });
  }, { root: cont, threshold: 0.5 });
  cont.querySelectorAll('.cpage').forEach((im) => { contLazyObs.observe(im); contPageObs.observe(im); });
}
function scrollToPage(n) {
  const im = document.querySelector(`.cpage[data-page="${n}"]`);
  if (im) im.scrollIntoView({ behavior: 'auto', block: 'start' });
}

// ---- universal "go to logical page" ----
function goTo(page) {
  cur = clampPage(page);
  if (mode === 'single') renderSingle();
  else if (mode === 'spread') renderSpread();
  else { scrollToPage(cur); }
  counter.textContent = `${cur + 1} / ${total}`;
  saveProgress(cur);
  updateStripActive();
}

function teardownContinuous() {
  if (contLazyObs) { contLazyObs.disconnect(); contLazyObs = null; }
  if (contPageObs) { contPageObs.disconnect(); contPageObs = null; }
}

function setMode(m) {
  teardownContinuous();
  mode = m; el.dataset.mode = m;
  document.getElementById('dirtoggle').style.display = (m === 'continuous') ? 'none' : '';
  document.getElementById('stepbtn').style.display = (m === 'spread') ? '' : 'none';
  if (m === 'continuous') { buildContinuous(); scrollToPage(cur); counter.textContent = `${cur + 1} / ${total}`; updateStripActive(); }
  else goTo(cur);
}

// ---- bottom-bar buttons (logical: 下一页 = advance, 上一页 = back) ----
document.getElementById('nextbtn').onclick = advance;
document.getElementById('prevbtn').onclick = back;

// ---- keyboard ----
document.addEventListener('keydown', (e) => {
  if (mode === 'continuous') return; // native scroll
  if (e.key === 'ArrowRight') { dir === 'rtl' ? back() : advance(); }
  if (e.key === 'ArrowLeft') { dir === 'rtl' ? advance() : back(); }
});

// ---- direction toggle ----
function applyDirLabel() { document.getElementById('dirlabel').textContent = dir.toUpperCase(); }
document.getElementById('dirtoggle').onclick = () => {
  const next = dir === 'ltr' ? 'rtl' : 'ltr';
  fetch(`/api/comics/${id}`, { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ readingDirection: next }) })
    .then((r) => { if (!r.ok) { alert('切换方向失败'); return; } dir = next; applyDirLabel(); buildStrip(); goTo(cur); })
    .catch(() => alert('切换方向失败'));
};

// ---- display-mode toggle ----
const modeToggle = document.getElementById('modetoggle');
modeToggle.value = mode;
modeToggle.onchange = () => {
  const next = modeToggle.value;
  fetch(`/api/comics/${id}`, { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ displayMode: next }) })
    .then((r) => { if (!r.ok) { alert('切换展示方式失败'); modeToggle.value = mode; return; } setMode(next); })
    .catch(() => { alert('切换展示方式失败'); modeToggle.value = mode; });
};

// ---- spread page-step toggle ----
function applyStepLabel() { document.getElementById('steplabel').textContent = spreadStep === 1 ? '一页' : '两页'; }
document.getElementById('stepbtn').onclick = () => {
  spreadStep = spreadStep === 2 ? 1 : 2;
  localStorage.setItem('spreadStep', String(spreadStep));
  applyStepLabel();
  if (mode === 'spread') goTo(cur);
};

// ---- metadata editing ----
const authorInput = document.getElementById('author');
const ratingSel = document.getElementById('rating');
const tagsBox = document.getElementById('tags');
function patchMeta(payload) {
  fetch(`/api/comics/${id}`, { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) })
    .then((r) => { if (!r.ok) alert('保存失败'); }).catch(() => alert('保存失败'));
}
authorInput.addEventListener('change', () => patchMeta({ author: authorInput.value }));
ratingSel.addEventListener('change', () => patchMeta({ rating: parseInt(ratingSel.value, 10) }));
function renderTags(tags) {
  tagsBox.innerHTML = '';
  (tags || []).forEach((t) => {
    const span = document.createElement('span'); span.className = 'tag'; span.textContent = t.name + ' ';
    const x = document.createElement('button'); x.textContent = '×';
    x.onclick = () => fetch(`/api/comics/${id}/tags/${t.id}`, { method: 'DELETE' }).then((r) => { if (!r.ok) { alert('删除标签失败'); return; } loadDetail(); }).catch(() => alert('删除标签失败'));
    span.appendChild(x); tagsBox.appendChild(span);
  });
}
function loadDetail() {
  fetch(`/api/comics/${id}`).then((r) => r.json()).then((j) => renderTags(j.data.tags)).catch(() => {});
}
document.getElementById('addtag').addEventListener('submit', (e) => {
  e.preventDefault();
  const input = document.getElementById('newtag'); const name = input.value.trim();
  if (!name) return;
  fetch(`/api/comics/${id}/tags`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name }) })
    .then((r) => { if (!r.ok) { alert('添加标签失败'); return; } input.value = ''; loadDetail(); }).catch(() => alert('添加标签失败'));
});

// ---- strip collapse ----
let stripCollapsed = localStorage.getItem('stripCollapsed') === '1';
function applyStripCollapsed() {
  stripEl.classList.toggle('collapsed', stripCollapsed);
  document.getElementById('stripToggle').title = stripCollapsed ? '展开预览' : '收起预览';
}
document.getElementById('stripToggle').onclick = () => {
  stripCollapsed = !stripCollapsed;
  localStorage.setItem('stripCollapsed', stripCollapsed ? '1' : '0');
  applyStripCollapsed();
};

// ---- init ----
applyDirLabel();
applyStepLabel();
applyStripCollapsed();
buildStrip();
loadDetail();
if (total > 0) setMode(mode); else counter.textContent = '无可显示页面';
```

- [ ] **Step 5: Add the mode CSS**

In `internal/server/web/static/css/app.css`, append:

```css
/* reader display modes */
.spread { display: flex; gap: 2px; align-items: center; justify-content: center; height: 100%; width: 100%; }
.spread img { max-height: 100%; max-width: 50%; object-fit: contain; box-shadow: 0 0 0 1px var(--line); }
.continuous { height: 100%; width: 100%; overflow-y: auto; display: flex; flex-direction: column; align-items: center; gap: 10px; padding: 10px 0; scrollbar-width: thin; }
.continuous .cpage { width: min(100%, 760px); height: auto; display: block; box-shadow: 0 0 0 1px var(--line); }
#modetoggle { font-family: var(--font-mono); }
#stepbtn { font-family: var(--font-mono); }
```

- [ ] **Step 6: Run tests + build + manual smoke**

Run: `go build ./... && go vet ./... && go test ./internal/server/...`
Expected: PASS (incl. `TestReaderHasModeToggle` + all prior reader tests).

Manual smoke (the controller does this with headless Chrome): open `/read/<id>`, switch the 展示方式 select through 单页 / 连续 / 双页, verify: single pages normally; spread shows the cover alone then page pairs (LTR order), the 步进 button flips between 两页/一页; continuous scrolls vertically with lazy-loaded pages and the direction toggle hidden. Confirm the thumbnail strip + author/rating/tags still work in every mode.

- [ ] **Step 7: Commit**

```bash
git add internal/server
git commit -m "feat: mode-aware reader with single, continuous, and spread display modes"
```

---

## Self-Review

**Spec coverage:**
- §3 data model (display_mode column, idempotent migration, Node field, UpdateDisplayMode, 3-place sync) → Task 1. ✓
- §4 API (detail.displayMode + PATCH validation) → Task 2. ✓
- §5.1 single (existing behavior in the framework) → Task 3 (`renderSingle`). ✓
- §5.2 spread (cover-single + pairs, direction ordering, page-step 2/1 via localStorage) → Task 3 (`renderSpread`/`spreadStart`/`advance`/`back`/`stepbtn`). ✓
- §5.3 continuous (vertical scroll, lazy-load, topmost-visible progress, hide direction toggle) → Task 3 (`buildContinuous`/`contPageObs`/`setMode` hides `#dirtoggle`). ✓
- §6 reader.js mode-aware framework (setMode/goTo/advance/back) preserving progress/preload/strip/direction/meta → Task 3 (the rewrite includes all of it). ✓
- §7 tests → store (Task 1), API (Task 2), reader render (Task 3); mode interactions noted as manual + headless smoke. ✓

**Placeholder scan:** No `TODO`/`TBD`. Task 3 is a complete `reader.js` replacement (not a partial). Every `fetch` has `!r.ok`/`.catch`. The continuous-mode keyboard handler intentionally returns early to allow native scroll — documented inline. No vague "handle errors" steps.

**Type consistency:** `display_mode` (DB) ↔ `Node.DisplayMode` (Go) ↔ `displayMode` (JSON detail + PATCH) ↔ `data-mode`/`mode` (reader.js). `UpdateDisplayMode(ctx,id,mode)` matches Task 1 producer ↔ Task 2 consumer. The three column-sync sites (`nodeCols`, `Search`, `scanNode`) all append `display_mode` last. `#modetoggle`/`#stepbtn`/`#steplabel`/`data-mode` IDs match between reader.html (Task 3 step 3) and reader.js (Task 3 step 4). `spreadStep` localStorage key + values (1/2) consistent. `setMode` hides `#dirtoggle` for continuous and `#stepbtn` for non-spread — matching the spec.
