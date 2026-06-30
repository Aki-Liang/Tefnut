# Comic Detail Page Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the detail page's native `<datalist>` tag autocomplete with a themed suggestions dropdown; add a lazy-loaded tiled grid of page thumbnails that link into the reader at the clicked page; sweep up the dead struct fields / CSS the final review flagged.

**Architecture:** Frontend-mostly: `comic.js`/`comic.html`/`app.css` for the combobox (reusing `.dd-menu`/`.dd-option`) and the thumbnail grid (reusing `/api/comics/:id/pages/:n/thumb`); a one-line `reader.js` addition to honor a `#p=N` hash; a small `pages.go`/`app.css` cleanup. No new API, no DB change.

**Tech Stack:** Go-embedded `html/template`, echo v4, vanilla JS, native `loading="lazy"`.

## Global Constraints

- No new API endpoint, no DB change. Tag add/delete reuse `POST`/`DELETE /api/comics/:id/tags`; suggestions reuse `GET /api/tags`; thumbnails reuse `GET /api/comics/:id/pages/:n/thumb`.
- The tag suggestions popup MUST reuse the existing themed `.dd-menu` container + `.dd-option` row classes (visual consistency — no native `<datalist>`).
- Tag combobox: click a suggestion OR keyboard-Enter on the active one → add that tag immediately; Enter on free text with no active suggestion → create+add; ArrowUp/Down move the active row; Escape / click-outside close. The `+` button still adds the typed text.
- Thumbnail grid: every page (0..PageCount-1), native `loading="lazy"`, each cell links to `/read/<id>#p=<i>`. Distinct classes `.thumb-grid`/`.thumb-cell` (the reader strip's `.thumb`/`.thumbstrip` must stay untouched).
- Reader honors `#p=N` (clamped) as start page, else resumes at progress. Unchanged otherwise.
- `go build/vet/test ./...` green; `node --test` (zoom) green; `gofmt` clean.

## File Structure
- `internal/server/web/static/js/comic.js` (modify) — combobox (replaces the datalist fetch + the add-tag submit) + thumbnail grid build.
- `internal/server/web/templates/comic.html` (modify) — `.combobox`+`#d-suggest` (replace datalist); `.detail[data-pages]`; `#thumb-grid` section.
- `internal/server/web/static/js/reader.js` (modify) — `#p=N` start-page override.
- `internal/server/web/static/css/app.css` (modify) — `.combobox`, `.thumb-grid`/`.thumb-cell`; remove dead rules.
- `internal/server/pages.go` (modify) — drop dead struct fields.
- `internal/server/server_test.go` (modify) — assert thumb-grid + data-pages.

---

### Task 1: Themed tag combobox (replace native datalist)

**Files:**
- Modify: `internal/server/web/templates/comic.html`, `internal/server/web/static/js/comic.js`, `internal/server/web/static/css/app.css`

**Interfaces:**
- Consumes: existing `makeChip(t)`, `tagsBox` (`#d-tags`), `id`, and `POST /api/comics/:id/tags` / `GET /api/tags` in comic.js. Reuses `.dd-menu`/`.dd-option` CSS.

- [ ] **Step 1: Swap the datalist for a themed combobox container in comic.html**

In `internal/server/web/templates/comic.html`, replace the `#d-addtag` form's input+datalist:

```html
    <form id="d-addtag" class="detail-row">
      <input id="d-newtag" list="d-taglist" placeholder="添加标签…" autocomplete="off">
      <datalist id="d-taglist"></datalist>
      <button type="submit">+</button>
    </form>
```

with a `.combobox`-wrapped input + a themed `.dd-menu` popup:

```html
    <form id="d-addtag" class="detail-row">
      <span class="combobox">
        <input id="d-newtag" placeholder="添加标签…" autocomplete="off" role="combobox" aria-expanded="false" aria-autocomplete="list">
        <div id="d-suggest" class="dd-menu" role="listbox" hidden></div>
      </span>
      <button type="submit">+</button>
    </form>
```

- [ ] **Step 2: Replace the add-tag + datalist logic in comic.js with the combobox**

In `internal/server/web/static/js/comic.js`, replace BOTH the `document.getElementById('d-addtag').addEventListener('submit', …)` block AND the `// autocomplete existing tags` `fetch('/api/tags')…` block (the last ~28 lines of the file) with:

```js
// ---- themed tag combobox: filter existing tags, add on pick/Enter ----
const newtag = document.getElementById('d-newtag');
const suggest = document.getElementById('d-suggest');
let allTags = [];      // [{id,name}] of existing tags
let sugItems = [];     // current filtered list
let sugActive = -1;
fetch('/api/tags').then((r) => r.json()).then((j) => { allTags = j.data || []; }).catch(() => {});

function isAttached(t) { return !!tagsBox.querySelector(`.tag[data-id="${t.id}"]`); }
function closeSuggest() { suggest.hidden = true; suggest.innerHTML = ''; sugItems = []; sugActive = -1; newtag.setAttribute('aria-expanded', 'false'); }
function setActive(i) { sugActive = i; [...suggest.children].forEach((c, j) => c.classList.toggle('active', j === i)); }
function renderSuggest() {
  const q = newtag.value.trim().toLowerCase();
  sugItems = !q ? [] : allTags.filter((t) => t.name.toLowerCase().includes(q) && !isAttached(t)).slice(0, 8);
  if (!sugItems.length) { closeSuggest(); return; }
  suggest.innerHTML = '';
  sugItems.forEach((t, i) => {
    const o = document.createElement('div');
    o.className = 'dd-option'; o.setAttribute('role', 'option'); o.textContent = t.name;
    o.addEventListener('mousemove', () => setActive(i));
    o.addEventListener('mousedown', (e) => { e.preventDefault(); addTag(t.name); }); // mousedown beats input blur
    suggest.appendChild(o);
  });
  sugActive = -1;
  suggest.hidden = false; newtag.setAttribute('aria-expanded', 'true');
}
function addTag(name) {
  name = (name || '').trim();
  if (!name) return;
  fetch(`/api/comics/${id}/tags`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name }),
  })
    .then((r) => (r.ok ? r.json() : Promise.reject()))
    .then((j) => {
      const t = j.data;
      if (t && !tagsBox.querySelector(`.tag[data-id="${t.id}"]`)) {
        tagsBox.appendChild(makeChip(t));
        if (!allTags.some((x) => x.id === t.id)) allTags.push(t);
      }
      newtag.value = ''; closeSuggest();
    })
    .catch(() => alert('添加标签失败'));
}
newtag.addEventListener('input', renderSuggest);
newtag.addEventListener('keydown', (e) => {
  if (suggest.hidden) { if (e.key === 'Enter') { e.preventDefault(); addTag(newtag.value); } return; }
  if (e.key === 'ArrowDown') { e.preventDefault(); setActive((sugActive + 1) % sugItems.length); }
  else if (e.key === 'ArrowUp') { e.preventDefault(); setActive((sugActive - 1 + sugItems.length) % sugItems.length); }
  else if (e.key === 'Enter') { e.preventDefault(); addTag(sugActive >= 0 ? sugItems[sugActive].name : newtag.value); }
  else if (e.key === 'Escape') { closeSuggest(); }
});
document.addEventListener('click', (e) => { if (!e.target.closest('.combobox')) closeSuggest(); });
document.getElementById('d-addtag').addEventListener('submit', (e) => { e.preventDefault(); addTag(newtag.value); });
```

- [ ] **Step 3: Add the `.combobox` CSS**

In `internal/server/web/static/css/app.css`, append after the `.read-btn` block:

```css
.combobox { position: relative; display: inline-block; }
.combobox > input { background: var(--ink); color: var(--paper); border: 2px solid var(--line); border-radius: 6px; padding: 7px 10px; font-size: 13px; min-width: 200px; }
.combobox.open > input, .combobox > input:focus { border-color: var(--seal); }
```

(The popup `#d-suggest` reuses `.dd-menu`/`.dd-option` — already styled.)

- [ ] **Step 4: Build, verify served, commit**

Run:
```bash
go build ./... && cp internal/server/web/static/js/comic.js /tmp/c.js && node --check /tmp/c.js && echo SYNTAX-OK; rm -f /tmp/c.js
grep -c 'd-taglist\|<datalist' internal/server/web/templates/comic.html   # expect 0
grep -c 'd-suggest\|combobox' internal/server/web/templates/comic.html     # expect >=2
```
Expected: build OK; comic.js parses; no datalist remains; combobox present. (Controller runs the browser check: suggestions popup is themed, filters, click/Enter adds, keyboard nav, Esc/click-outside close.)

```bash
git add internal/server/web/templates/comic.html internal/server/web/static/js/comic.js internal/server/web/static/css/app.css
git commit -m "feat: themed tag combobox replaces native datalist on detail page"
```

---

### Task 2: Page thumbnail grid + reader #p=N

**Files:**
- Modify: `internal/server/web/templates/comic.html`, `internal/server/web/static/js/comic.js`, `internal/server/web/static/js/reader.js`, `internal/server/web/static/css/app.css`
- Test: `internal/server/server_test.go`

**Interfaces:**
- Consumes: `comicData.PageCount` (already populated), `id`, `el` (`.detail`) from comic.js; `/api/comics/:id/pages/:n/thumb`. Produces: `/read/<id>#p=<n>` links honored by reader.js.

- [ ] **Step 1: Write the failing server assertion**

In `internal/server/server_test.go`, the existing `TestPageComicDetailRenders` has a `want` slice of strings checked in the body. Add two entries to that slice:

```go
		`id="thumb-grid"`,
		`data-pages="1"`,
```

(seedComic creates a comic with `PageCount: 1`, so `data-pages="1"`.)

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/server/ -run TestPageComicDetailRenders -v`
Expected: FAIL — body has neither `id="thumb-grid"` nor `data-pages="1"` yet.

- [ ] **Step 3: Add data-pages and the grid section to comic.html**

In `internal/server/web/templates/comic.html`, add `data-pages` to the `.detail` section opening tag:

```html
<section class="detail" data-id="{{.ID}}" data-rating="{{.Rating}}" data-pages="{{.PageCount}}">
```

And add the grid section immediately AFTER the `.detail` `</section>` (before `{{end}}`):

```html
<section class="thumb-grid" id="thumb-grid" aria-label="页面预览"></section>
```

- [ ] **Step 4: Build the grid in comic.js**

Append to `internal/server/web/static/js/comic.js`:

```js
// ---- page thumbnail grid (lazy) → click opens the reader at that page ----
const pageCount = parseInt(el.dataset.pages, 10) || 0;
const grid = document.getElementById('thumb-grid');
for (let i = 0; i < pageCount; i++) {
  const a = document.createElement('a');
  a.className = 'thumb-cell';
  a.href = `/read/${id}#p=${i}`;
  const im = document.createElement('img');
  im.loading = 'lazy';
  im.src = `/api/comics/${id}/pages/${i}/thumb`;
  im.alt = `第 ${i + 1} 页`;
  a.appendChild(im);
  grid.appendChild(a);
}
```

- [ ] **Step 5: Honor #p=N in reader.js**

In `internal/server/web/static/js/reader.js`, the init line is:

```js
let cur = Math.min(parseInt(el.dataset.start, 10) || 0, Math.max(total - 1, 0));
```

Add immediately AFTER it:

```js
const hashP = location.hash.match(/^#p=(\d+)$/);
if (hashP) cur = Math.max(0, Math.min(parseInt(hashP[1], 10), Math.max(total - 1, 0)));
```

- [ ] **Step 6: Add the grid CSS**

In `internal/server/web/static/css/app.css`, append after the `.combobox` rules:

```css
.thumb-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(100px, 1fr)); gap: 10px; margin-top: 28px; }
.thumb-cell { display: block; border: 2px solid var(--line); border-radius: 3px; overflow: hidden; transition: border-color 0.12s ease; }
.thumb-cell:hover { border-color: var(--seal); }
.thumb-cell img { width: 100%; aspect-ratio: 2/3; object-fit: contain; background: var(--ink); display: block; }
```

- [ ] **Step 7: Run tests, build, verify, commit**

Run:
```bash
go build ./... && go test ./internal/server/ -run TestPageComicDetailRenders -v
cp internal/server/web/static/js/reader.js /tmp/r.mjs && node --check /tmp/r.mjs && echo READER-OK; rm -f /tmp/r.mjs
cp internal/server/web/static/js/comic.js /tmp/c.js && node --check /tmp/c.js && echo COMIC-OK; rm -f /tmp/c.js
```
Expected: test PASS (thumb-grid + data-pages present); both JS files parse. (Controller browser check: grid renders PageCount cells, lazy-loads, a cell click opens `/read/:id#p=N` starting at page N.)

```bash
gofmt -w internal/server/server_test.go
git add internal/server/web/templates/comic.html internal/server/web/static/js/comic.js internal/server/web/static/js/reader.js internal/server/web/static/css/app.css internal/server/server_test.go
git commit -m "feat: detail-page thumbnail grid; reader honors #p=N start page"
```

---

### Task 3: Cleanup — dead struct fields and dead CSS

**Files:**
- Modify: `internal/server/pages.go`, `internal/server/web/static/css/app.css`

**Interfaces:** none new — pure removal of inert code.

- [ ] **Step 1: Drop dead fields from the data structs**

In `internal/server/pages.go`, `readerData` no longer renders author/rating (the reader is reading-only). Remove `Author`, `Rating`, and `Ratings` from the struct:

```go
type readerData struct {
	ID               int64
	Name             string
	PageCount        int
	LastPage         int
	ReadingDirection string
	DisplayMode      string
}
```

Remove the matching lines from `pageReader`'s render literal (delete the `Author:`, `Rating:`, and `Ratings:` lines):

```go
	return render(c, "reader.html", readerData{
		ID:               n.ID,
		Name:             n.Name,
		PageCount:        n.PageCount,
		LastPage:         last,
		ReadingDirection: dir,
		DisplayMode:      displayMode,
	})
```

`comicData`'s stars are JS-rendered from `data-rating`, so `Ratings` is unused — remove it (KEEP `PageCount`, used by the thumbnail grid):

```go
type comicData struct {
	ID        int64
	Name      string
	Author    string
	Rating    int
	PageCount int
	Tags      []*store.Tag
}
```

Remove `Ratings: ratingChoices` from `pageComicDetail`'s render literal:

```go
	return render(c, "comic.html", comicData{
		ID: n.ID, Name: n.Name, Author: n.Author, Rating: n.Rating,
		PageCount: n.PageCount, Tags: tags,
	})
```

(`ratingChoices` is still used by `browseData`/`pageBrowse`, so leave the package-level var.)

- [ ] **Step 2: Remove the orphaned reader CSS**

In `internal/server/web/static/css/app.css`, delete these five now-dead rules (the reader's `.meta` block and `#addtag` form were removed; `.tags .tag` is only referenced by removed reader markup — `/tags` page uses `.cnt`/`.tname`, and the detail chips use `.detail .tag`):

```css
.reader-bar .meta label { color: var(--muted); font-size: 13px; }
```
```css
.tags .tag { display: inline-flex; gap: 4px; background: var(--panel-2); padding: 3px 9px; border-radius: 12px; margin: 2px; font-size: 12px; }
.tags .tag button { background: none; border: 0; color: var(--seal); cursor: pointer; padding: 0; }
#addtag { display: inline-flex; gap: 4px; }
#addtag input { width: 110px; }
#addtag button { background: var(--panel-2); color: var(--paper); border: 2px solid var(--line); border-radius: 6px; cursor: pointer; padding: 4px 10px; }
```

- [ ] **Step 3: Build, full suite, verify reader/detail unaffected, commit**

Run:
```bash
go build ./... && go vet ./... && go test ./... && node --test internal/server/web/static/js/*.test.mjs
gofmt -l .
```
Expected: all green; `gofmt -l .` prints nothing. (The reader.html template no longer references `{{.Author}}`/`{{.Ratings}}`/`{{.Rating}}`, so removing those struct fields cannot break rendering; `go build` re-embeds and the full suite confirms.)

```bash
gofmt -w internal/server/pages.go
git add internal/server/pages.go internal/server/web/static/css/app.css
git commit -m "chore: drop dead reader/detail struct fields and orphaned CSS"
```

---

## Self-Review

**Spec coverage:**
- A — themed tag combobox replacing datalist (filter, pick-adds, Enter-creates, keyboard, close) — Task 1. ✓
- B — thumbnail grid (lazy, all pages, links to `/read/:id#p=N`) + reader `#p=N` — Task 2. ✓
- C — drop dead `readerData.Author/Rating/Ratings` + `comicData.Ratings` + orphaned CSS — Task 3. ✓
- Reuse `.dd-menu`/`.dd-option` (A), distinct `.thumb-grid`/`.thumb-cell` (B), keep `comicData.PageCount` — covered. ✓
- No new API / no DB change — none of the tasks add either. ✓
- Tests: server assertion for thumb-grid+data-pages (Task 2); existing detail/reader/browse tests stay green (Task 3 build/suite). ✓

**Placeholder scan:** No TBD/TODO; every step shows complete code and exact commands. Browser checks are controller-run (no JS DOM harness in repo), explicitly labeled.

**Type consistency:** `addTag(name)`/`renderSuggest`/`setActive`/`closeSuggest`/`isAttached` defined and called consistently in Task 1; `makeChip`/`tagsBox`/`id` are the existing Task-2(detail) symbols reused unchanged. `el.dataset.pages` (Task 2 comic.js) matches `data-pages="{{.PageCount}}"` (Task 2 comic.html) and `comicData.PageCount` (kept in Task 3). `#p=(\d+)` regex in reader.js (Task 2) matches the `#p=${i}` link built in comic.js (Task 2). The removed `readerData` fields (Task 3) are exactly Author/Rating/Ratings, which the slimmed reader.html no longer uses.
