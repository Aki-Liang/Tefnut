# Comic Detail Page Design

**Goal:** Insert a comic detail page between the library grid and the reader. It shows a large cover, the filename, and editable rating/author/tags (with existing-tag autocomplete), and a 「阅读」 button that opens the reader. Metadata editing moves out of the reader, which becomes reading-only.

**Architecture:** A new server-rendered page `GET /comic/:id` (handler `pageComicDetail` + template `comic.html` + `comic.js`), consistent with the existing browse/reader/settings pages. The library grid links comics to `/comic/:id` instead of `/read/:id`; the detail page's 「阅读」 links to `/read/:id`. All editing reuses existing APIs. No new API endpoints, no DB changes.

**Tech Stack:** Existing — Go-embedded `html/template`, echo v4, vanilla JS. `<datalist>` for tag autocomplete.

## Navigation flow

```
folder grid → /comic/:id (detail) → 「阅读」 → /read/:id (reader)
```
- The library grid's comic card link changes from `/read/:id` to `/comic/:id` (`browse.html`, the comic `{{else}}` branch). Folder cards are unchanged (`/folder/:id`).
- 「返回」 keeps its history-back behavior everywhere: reader 返回 → detail, detail 返回 → folder.
- `/read/:id` remains directly reachable (the 阅读 button links to it).

## Detail page (`GET /comic/:id` → `pageComicDetail` → `comic.html`)

**Data** (from `s.nodes.Get(id)` + `s.tags.ListForNode(id)`; 404 via `store.ErrNotFound` → `fail(c, 404, …)`):
`ID, Name, Author string, Rating int, PageCount int, Tags []*store.Tag, Ratings []int` (Ratings = `ratingChoices`).

**Layout:**
```
┌─ ← 返回 ─────────────────────────────────────────┐
│  ┌────────┐   <Name 文件名 标题>                  │
│  │ 大封面  │   作者 [ <Author>            ]        │
│  │  2:3   │   评分 ★ ★ ★ ☆ ☆  (点星设置)          │
│  │        │   标签 [动作 ×][科幻 ×]               │
│  │        │        [ 添加标签… ] (datalist 补全)  │
│  └────────┘   [  阅 读  ]                          │
└───────────────────────────────────────────────────┘
```
- Cover: `<img src="/api/comics/{{.ID}}/cover">`, large (aspect-ratio 2/3), the existing cover/placeholder behavior applies.
- Title: `{{.Name}}` (the filename).
- 阅读 button: `<a href="/read/{{.ID}}">阅读</a>`, primary styling.
- 返回: a link with `id="backlink"` reusing the reader's history-back enhancement (same JS pattern).

## Editing (`comic.js`, reusing existing APIs)
- **Author:** an input; on `change` → `PATCH /api/comics/:id` `{author}`.
- **Rating:** five clickable stars reflecting `Rating`. Clicking star *n* sets rating *n* via `PATCH /api/comics/:id` `{rating:n}`; clicking the current rating *n* again clears it to `0`. Stars re-render to the new value.
- **Tags:** existing tags render as chips with a `×` delete (`DELETE /api/comics/:id/tags/:tagId`). An add input + button (`POST /api/comics/:id/tags` `{name}`) with a `<datalist>` populated from `GET /api/tags` so existing tags autocomplete as you type (this resolves the "can't pick an existing tag" gap — the backend already dedupes by name via `Upsert`). After add/delete, the tag list reloads from `GET /api/comics/:id` (which returns `data.tags`).
- All mutations show an alert on failure (matching the existing reader/settings pattern).

## Reader simplification
- `reader.html`: remove the metadata block from `.reader-bar` — the 作者 input, 评分 select, the `#tags` container, and the `#addtag` form. Keep all reading controls (返回, 上一页/下一页, counter, 方向, 单页/连续/双页, 步进, zoom group, fit toggle).
- `reader.js`: remove the now-dead metadata logic — `authorInput`/`ratingSel`/`tagsBox` lookups, `patchMeta`, `renderTags`, `loadDetail`, the `#addtag` submit handler, and the `loadDetail()` init call. No other reader behavior changes.

## Error Handling
- `/comic/:id` for a missing or non-comic id → 404 (generic, via the existing `fail` 5xx-generic / 4xx-passthrough rules; "not found" is a safe 4xx message).
- Editing failures → client alert, no page break.

## Files
- Create: `internal/server/web/templates/comic.html`, `internal/server/web/static/js/comic.js`.
- Modify: `internal/server/pages.go` (add `pageComicDetail` + its data struct), `internal/server/server.go` (route `GET /comic/:id`), `internal/server/web/templates/browse.html` (card link → `/comic/:id`), `internal/server/web/templates/reader.html` + `internal/server/web/static/js/reader.js` (remove metadata), `internal/server/web/static/css/app.css` (detail-page + star styles).

## Testing
- **Server:** `pageComicDetail` renders for a seeded comic — body contains the filename, `src="/api/comics/<id>/cover"`, a 阅读 link `href="/read/<id>"`, and attached tag names; a missing id → 404. The browse page's comic card links to `/comic/<id>` (not `/read/<id>`). The reader page no longer serves the `#addtag`/`#rating` controls.
- **Frontend:** `comic.js` interactions (star click → PATCH, author change → PATCH, tag add/delete, datalist populated from `/api/tags`) verified in a real browser / headless Chrome, as with the other vanilla-JS pages (no JS unit harness in the repo).
- `go build/vet/test ./...` green; `gofmt` clean; `node --test` for the existing zoom module stays green.

## Out of Scope
- Changing the reader's reading controls or the tag-management page (`/tags`).
- Per-comic single-path or any backend/library-model change.
- Hover-preview on the rating stars (click-to-set only).
