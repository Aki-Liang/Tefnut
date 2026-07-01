# Comic Detail Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A server-rendered comic detail page (`/comic/:id`) with a large cover, filename, and editable rating/author/tags (existing-tag autocomplete) plus a 阅读 button; the library grid links to it, and the reader is slimmed to reading-only.

**Architecture:** New `pageComicDetail` handler + `comic.html` template + `comic.js`, consistent with the existing browse/reader/settings pages. The browse comic card links to `/comic/:id`; the detail 阅读 button links to `/read/:id`. All editing reuses existing APIs (`PATCH /api/comics/:id`, `POST/DELETE /api/comics/:id/tags`, `GET /api/tags`). No new API, no DB change.

**Tech Stack:** Go-embedded `html/template`, echo v4, vanilla JS, `<datalist>`.

## Global Constraints

- No new API endpoints and no DB/library-model change — only the page route `GET /comic/:id` and frontend.
- Editing reuses: `PATCH /api/comics/:id` (`{author}` / `{rating:int}`), `POST /api/comics/:id/tags` (`{name}`, returns the tag), `DELETE /api/comics/:id/tags/:tagId`, `GET /api/tags` (existing tags for autocomplete). The add endpoint dedupes by name (`Upsert`).
- Rating: 5 clickable stars; clicking star n sets rating n, clicking the current rating n clears it to 0.
- The reader becomes reading-only: remove author/rating/tags/add-tag from `reader.html` and the matching `reader.js` logic; keep all reading controls.
- `go build/vet/test ./...` green; `gofmt` clean; `node --test` (zoom module) stays green.

## File Structure

- `internal/server/pages.go` (modify) — `comicData` struct + `pageComicDetail`.
- `internal/server/server.go` (modify) — route `GET /comic/:id`.
- `internal/server/web/templates/comic.html` (create) — the detail page.
- `internal/server/web/static/js/comic.js` (create) — rating stars, author, tag add/delete, datalist.
- `internal/server/web/static/css/app.css` (modify) — detail layout + star styles.
- `internal/server/web/templates/browse.html` (modify) — comic card link → `/comic/:id`.
- `internal/server/web/templates/reader.html` + `internal/server/web/static/js/reader.js` (modify) — remove metadata controls.
- `internal/server/server_test.go` (modify) — detail render/404, browse-link, reader-cleanup tests.

---

### Task 1: Detail page handler, template, and route

**Files:**
- Modify: `internal/server/pages.go`, `internal/server/server.go`
- Create: `internal/server/web/templates/comic.html`
- Test: `internal/server/server_test.go`

**Interfaces:**
- Produces: route `GET /comic/:id` → `pageComicDetail` rendering `comic.html` with `comicData{ID, Name, Author, Rating, PageCount, Tags []*store.Tag, Ratings []int}`. The page has `.detail[data-id][data-rating]`, `#d-author`, `#d-stars`, `#d-tags` (server-rendered `.tag[data-id]` chips with a `.del` button), `#d-addtag`/`#d-newtag`/`#d-taglist`, a `.read-btn` → `/read/:id`, and `#backlink`. (Task 2 binds these.)

- [ ] **Step 1: Write the failing tests**

Append to `internal/server/server_test.go`:

```go
func TestPageComicDetailRenders(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	tr := store.NewTagRepo(db)
	tag, err := tr.Upsert(context.Background(), "action")
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.AddToNode(context.Background(), n.ID, tag.ID); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/comic/"+itoa(n.ID), nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		n.Name,
		`src="/api/comics/` + itoa(n.ID) + `/cover"`,
		`href="/read/` + itoa(n.ID) + `"`,
		"action",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("detail body missing %q", want)
		}
	}
}

func TestPageComicDetailNotFound(t *testing.T) {
	_, e, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/comic/99999", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rec.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -run TestPageComicDetail -v`
Expected: FAIL — route `/comic/:id` not registered (404 for the render test as well, or a routing miss).

- [ ] **Step 3: Add the handler and struct**

In `internal/server/pages.go`, add after the `readerData` struct:

```go
type comicData struct {
	ID        int64
	Name      string
	Author    string
	Rating    int
	PageCount int
	Tags      []*store.Tag
	Ratings   []int
}
```

Add the handler (place after `pageReader`):

```go
func (s *Server) pageComicDetail(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parseID(c, "id")
	if err != nil {
		return fail(c, http.StatusBadRequest, err)
	}
	n, err := s.nodes.Get(ctx, id)
	if err != nil {
		return fail(c, http.StatusNotFound, err)
	}
	tags, _ := s.tags.ListForNode(ctx, id)
	return render(c, "comic.html", comicData{
		ID: n.ID, Name: n.Name, Author: n.Author, Rating: n.Rating,
		PageCount: n.PageCount, Tags: tags, Ratings: ratingChoices,
	})
}
```

(This matches `pageReader`'s pattern of returning 404 on any `Get` error; no new import needed.)

- [ ] **Step 4: Register the route**

In `internal/server/server.go`, after `e.GET("/read/:id", s.pageReader)`, add:

```go
	e.GET("/comic/:id", s.pageComicDetail)
```

- [ ] **Step 5: Create the template**

Create `internal/server/web/templates/comic.html`:

```html
{{define "title"}}{{.Name}} · Tefnut{{end}}
{{define "content"}}
<a class="crumbs back-link" href="/" id="backlink">← 返回</a>
<section class="detail" data-id="{{.ID}}" data-rating="{{.Rating}}">
  <div class="detail-cover">
    <img src="/api/comics/{{.ID}}/cover" alt="{{.Name}}">
  </div>
  <div class="detail-meta">
    <h1 class="detail-title">{{.Name}}</h1>
    <label class="detail-row">作者 <input id="d-author" value="{{.Author}}"></label>
    <div class="detail-row">评分 <span id="d-stars" class="stars-edit"></span></div>
    <div class="detail-row tags-row">标签
      <span id="d-tags" class="tags">{{range .Tags}}<span class="tag" data-id="{{.ID}}">{{.Name}} <button class="del" type="button" aria-label="删除">×</button></span>{{end}}</span>
    </div>
    <form id="d-addtag" class="detail-row">
      <input id="d-newtag" list="d-taglist" placeholder="添加标签…" autocomplete="off">
      <datalist id="d-taglist"></datalist>
      <button type="submit">+</button>
    </form>
    <a class="read-btn" href="/read/{{.ID}}">阅 读</a>
  </div>
</section>
{{end}}
{{define "scripts"}}<script src="/static/js/comic.js"></script>{{end}}
```

- [ ] **Step 6: Run tests + build**

Run: `go build ./... && go test ./internal/server/ -run TestPageComicDetail -v`
Expected: both PASS — `TestPageComicDetailRenders` (200, body has name/cover/read-link/tag) and `TestPageComicDetailNotFound` (404).

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/server/pages.go internal/server/server.go internal/server/server_test.go
git add internal/server/pages.go internal/server/server.go internal/server/web/templates/comic.html internal/server/server_test.go
git commit -m "feat: comic detail page handler, template, route"
```

---

### Task 2: Detail-page interactions and styles

**Files:**
- Create: `internal/server/web/static/js/comic.js`
- Modify: `internal/server/web/static/css/app.css`

**Interfaces:**
- Consumes: the `comic.html` DOM from Task 1 (`.detail`, `#d-author`, `#d-stars`, `#d-tags`, `#d-addtag`/`#d-newtag`/`#d-taglist`, `#backlink`); APIs `PATCH /api/comics/:id`, `POST /api/comics/:id/tags` (returns `{data:{id,name}}`), `DELETE /api/comics/:id/tags/:tagId`, `GET /api/tags` (returns `{data:[{id,name,...}]}`).

- [ ] **Step 1: Create comic.js**

Create `internal/server/web/static/js/comic.js`:

```js
const el = document.querySelector('.detail');
const id = el.dataset.id;
let rating = parseInt(el.dataset.rating, 10) || 0;

// back: return to where you came from, else fall back to the href
document.getElementById('backlink').addEventListener('click', (e) => {
  let sameOrigin = false;
  try { sameOrigin = !!document.referrer && new URL(document.referrer).origin === location.origin; } catch (_) {}
  if (sameOrigin && history.length > 1) { e.preventDefault(); history.back(); }
});

function patchMeta(payload) {
  return fetch(`/api/comics/${id}`, {
    method: 'PATCH', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  }).then((r) => { if (!r.ok) throw new Error('save'); });
}

// rating stars: click n sets n; click the current rating clears to 0
const starsBox = document.getElementById('d-stars');
function renderStars() {
  starsBox.innerHTML = '';
  for (let i = 1; i <= 5; i++) {
    const s = document.createElement('button');
    s.type = 'button';
    s.className = 'star' + (i <= rating ? ' on' : '');
    s.textContent = i <= rating ? '★' : '☆';
    s.addEventListener('click', () => {
      const next = rating === i ? 0 : i;
      patchMeta({ rating: next }).then(() => { rating = next; renderStars(); }).catch(() => alert('保存失败'));
    });
    starsBox.appendChild(s);
  }
}
renderStars();

// author
const author = document.getElementById('d-author');
author.addEventListener('change', () => patchMeta({ author: author.value }).catch(() => alert('保存失败')));

// tags: delete via event delegation; add appends a chip from the POST response
const tagsBox = document.getElementById('d-tags');
function makeChip(t) {
  const span = document.createElement('span');
  span.className = 'tag'; span.dataset.id = t.id;
  span.append(document.createTextNode(t.name + ' '));
  const x = document.createElement('button');
  x.className = 'del'; x.type = 'button'; x.setAttribute('aria-label', '删除'); x.textContent = '×';
  span.appendChild(x);
  return span;
}
tagsBox.addEventListener('click', (e) => {
  if (!e.target.classList.contains('del')) return;
  const chip = e.target.closest('.tag');
  fetch(`/api/comics/${id}/tags/${chip.dataset.id}`, { method: 'DELETE' })
    .then((r) => { if (!r.ok) throw new Error(); chip.remove(); })
    .catch(() => alert('删除标签失败'));
});
document.getElementById('d-addtag').addEventListener('submit', (e) => {
  e.preventDefault();
  const input = document.getElementById('d-newtag');
  const name = input.value.trim();
  if (!name) return;
  fetch(`/api/comics/${id}/tags`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
    .then((r) => (r.ok ? r.json() : Promise.reject()))
    .then((j) => {
      const t = j.data;
      if (t && !tagsBox.querySelector(`.tag[data-id="${t.id}"]`)) tagsBox.appendChild(makeChip(t));
      input.value = '';
    })
    .catch(() => alert('添加标签失败'));
});

// autocomplete existing tags
fetch('/api/tags')
  .then((r) => r.json())
  .then((j) => {
    const dl = document.getElementById('d-taglist');
    (j.data || []).forEach((t) => { const o = document.createElement('option'); o.value = t.name; dl.appendChild(o); });
  })
  .catch(() => {});
```

- [ ] **Step 2: Add the detail-page styles**

Append to `internal/server/web/static/css/app.css`:

```css
/* ---- comic detail page ---- */
.back-link { display: inline-block; margin: -8px 0 16px; }
.detail { display: flex; gap: 28px; align-items: flex-start; max-width: 920px; }
.detail-cover { flex: 0 0 300px; }
.detail-cover img {
  width: 100%; aspect-ratio: 2/3; object-fit: cover; display: block;
  border: 2px solid var(--line); border-radius: 4px; background: var(--panel);
}
.detail-meta { flex: 1 1 auto; min-width: 0; }
.detail-title { font-family: var(--font-display); font-weight: 400; font-size: 22px; line-height: 1.2; margin: 0 0 18px; word-break: break-all; }
.detail-row { display: flex; align-items: center; gap: 10px; margin-bottom: 14px; color: var(--muted); flex-wrap: wrap; }
.detail-row input {
  background: var(--ink); color: var(--paper); border: 2px solid var(--line);
  border-radius: 6px; padding: 7px 10px; font-size: 13px;
}
#d-author { min-width: 240px; }
.stars-edit { display: inline-flex; gap: 2px; }
.star { background: none; border: none; cursor: pointer; font-size: 22px; line-height: 1; padding: 0 1px; color: var(--muted); }
.star.on { color: var(--seal); }
.tags-row .tags { display: inline-flex; flex-wrap: wrap; gap: 6px; }
.tag { display: inline-flex; align-items: center; gap: 2px; background: var(--panel-2); color: var(--paper); border: 2px solid var(--line); border-radius: 6px; padding: 3px 8px; font-size: 12px; }
.tag .del { background: none; border: none; color: var(--muted); cursor: pointer; font-size: 13px; line-height: 1; }
.tag .del:hover { color: var(--seal); }
.read-btn {
  display: inline-block; margin-top: 10px; padding: 11px 30px;
  background: var(--seal); color: var(--ink-2); text-decoration: none;
  font-family: var(--font-display); letter-spacing: 0.08em; border-radius: 6px;
  border: 2px solid var(--seal); font-size: 16px;
}
.read-btn:hover { background: var(--seal-dim); border-color: var(--seal-dim); }
```

- [ ] **Step 3: Build (re-embed) and verify served assets**

Run:
```bash
go build ./... && echo OK
SCRATCH=/private/tmp/claude-501/-Users-liangrenzhi-ws-gomod-Tefnut/8f5a5c6e-2e5d-497c-aac6-ad47d6fcc976/scratchpad
go build -o "$SCRATCH/tefnut" ./cmd/tefnut
curl -fsS http://127.0.0.1:8086/static/js/comic.js | grep -c "renderStars"
```
(The controller restarts the server.) Expected: build OK; `comic.js` served (`1`).

- [ ] **Step 4: Browser Check (controller runs this)**

Open `http://127.0.0.1:8086/comic/3` in Chrome. Verify: cover renders large; clicking a star sets the rating (and persists — re-open shows it); clicking the current rating clears to 0; editing 作者 and blurring persists; typing in 添加标签 shows existing-tag suggestions (datalist); adding a tag appends a chip; clicking a chip's × removes it; 阅读 opens the reader. No console errors.

- [ ] **Step 5: Commit**

```bash
git add internal/server/web/static/js/comic.js internal/server/web/static/css/app.css
git commit -m "feat: comic detail interactions (stars, author, tags, autocomplete)"
```

---

### Task 3: Point the grid at the detail page and slim the reader

**Files:**
- Modify: `internal/server/web/templates/browse.html`
- Modify: `internal/server/web/templates/reader.html`
- Modify: `internal/server/web/static/js/reader.js`
- Test: `internal/server/server_test.go`

**Interfaces:**
- Consumes: `/comic/:id` (Task 1). Produces: browse comic card → `/comic/:id`; reader bar without metadata controls.

- [ ] **Step 1: Write the failing tests**

Append to `internal/server/server_test.go`:

```go
func TestBrowseCardLinksToComicDetail(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, `href="/comic/`+itoa(n.ID)+`"`) {
		t.Fatalf("comic card should link to /comic/<id>; body=%s", body)
	}
	if strings.Contains(body, `href="/read/`+itoa(n.ID)+`"`) {
		t.Fatalf("comic card should NOT link straight to /read/<id>")
	}
}

func TestReaderHasNoMetadataControls(t *testing.T) {
	s, e, db := newTestServer(t)
	n := seedComic(t, db, s.dataDir)
	req := httptest.NewRequest(http.MethodGet, "/read/"+itoa(n.ID), nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, gone := range []string{`id="addtag"`, `id="rating"`, `id="author"`} {
		if strings.Contains(body, gone) {
			t.Fatalf("reader bar should no longer contain %q", gone)
		}
	}
	if !strings.Contains(body, `id="modetoggle"`) {
		t.Fatalf("reader should still have reading controls (modetoggle)")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -run 'TestBrowseCardLinksToComicDetail|TestReaderHasNoMetadataControls' -v`
Expected: FAIL — the card still links to `/read/<id>`; the reader still has `id="author"`/`id="rating"`/`id="addtag"`.

- [ ] **Step 3: Point the browse comic card at the detail page**

In `internal/server/web/templates/browse.html`, the comic `{{else}}` branch currently reads:

```html
      <a href="/read/{{.ID}}"><img class="cover" loading="lazy" src="/api/comics/{{.ID}}/cover" alt="{{.Name}}"><div class="name">{{.Name}}</div><div class="stars">{{.Rating}}★</div></a>
```

Change the `href` to the detail page:

```html
      <a href="/comic/{{.ID}}"><img class="cover" loading="lazy" src="/api/comics/{{.ID}}/cover" alt="{{.Name}}"><div class="name">{{.Name}}</div><div class="stars">{{.Rating}}★</div></a>
```

- [ ] **Step 4: Remove the metadata controls from reader.html**

In `internal/server/web/templates/reader.html`, delete these three lines from `.reader-bar` (the `<div class="meta">…</div>`, the `#tags` div, and the `#addtag` form):

```html
    <div class="meta">
      <label>作者 <input id="author" value="{{.Author}}"></label>
      <label>评分 <select id="rating">{{range .Ratings}}<option value="{{.}}" {{if eq . $.Rating}}selected{{end}}>{{.}}★</option>{{end}}</select></label>
    </div>
    <div class="tags" id="tags"></div>
    <form id="addtag"><input id="newtag" placeholder="添加标签"><button>+</button></form>
```

(The line immediately after — `<button class="pagebtn" id="dirtoggle" …>` — stays; the bar now goes counter/下一页 → 方向 → mode → zoom.)

- [ ] **Step 5: Remove the metadata logic from reader.js**

In `internal/server/web/static/js/reader.js`, delete the entire `// ---- metadata editing ----` block (from `const authorInput = …` through the end of the `#addtag` submit handler — `authorInput`, `ratingSel`, `tagsBox`, `patchMeta`, `renderTags`, `loadDetail`, and the `addtag` listener). Then in the `// ---- init ----` block, delete the `loadDetail();` line.

- [ ] **Step 6: Run the suite + build**

Run: `go build ./... && go vet ./... && go test ./... && node --test internal/server/web/static/js/*.test.mjs`
Then verify reader.js is still valid module syntax:
```bash
cp internal/server/web/static/js/reader.js /tmp/rc.mjs && node --check /tmp/rc.mjs && echo SYNTAX-OK; rm -f /tmp/rc.mjs
```
Expected: all green; `TestBrowseCardLinksToComicDetail` + `TestReaderHasNoMetadataControls` pass; reader.js parses (no reference to the removed `loadDetail`/`authorInput` etc. remains).

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/server/server_test.go
git add internal/server/web/templates/browse.html internal/server/web/templates/reader.html internal/server/web/static/js/reader.js internal/server/server_test.go
git commit -m "feat: grid links to detail page; reader slimmed to reading-only"
```

---

## Self-Review

**Spec coverage:**
- Server-rendered `/comic/:id` page (cover/filename/rating/tags + 阅读) — Task 1. ✓
- Editing reuses PATCH/tags APIs; rating stars (click n / clear on current); tag add/delete + datalist autocomplete from `/api/tags` — Task 2. ✓
- Browse card → `/comic/:id`; 阅读 → `/read/:id` — Task 1 (read-btn) + Task 3 (card). ✓
- Reader slimmed (remove author/rating/tags/addtag from html + js) — Task 3. ✓
- 返回 history-back on detail (and reader unchanged) — Task 1 template `#backlink` + Task 2 JS. ✓
- 404 for missing id — Task 1. ✓
- Tests: detail render/404, browse-link, reader-cleanup — Tasks 1 & 3. ✓

**Placeholder scan:** No TBD/TODO; every step shows complete code/markup and exact commands. The Browser Check (Task 2 Step 4) is controller-run, consistent with the repo's no-JS-unit-harness reality, and is explicitly labeled.

**Type consistency:** `comicData` fields match the `comic.html` template (`.ID/.Name/.Author/.Rating/.Tags`) and the handler. The DOM ids used in `comic.html` (`#d-author`, `#d-stars`, `#d-tags`, `#d-addtag`, `#d-newtag`, `#d-taglist`, `#backlink`, `.detail[data-id][data-rating]`) match exactly the ids `comic.js` queries. The POST-tag response shape `{data:{id,name}}` matches `makeChip(j.data)`. `seedComic(t, db, s.dataDir)`, `itoa`, and `store.NewTagRepo(db).Upsert/AddToNode` match the existing test helpers and store API. The removed reader ids (`author`/`rating`/`addtag`) are exactly what `TestReaderHasNoMetadataControls` asserts are gone.
