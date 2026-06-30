# Comic Detail Page Polish Design

**Goal:** Three detail-page improvements: (A) replace the native `<datalist>` tag autocomplete with a themed suggestions dropdown matching the rest of the app; (B) add a lazy-loaded, tiled grid of page thumbnails at the bottom of the detail page, each linking into the reader at that page; (C) sweep up the inert dead struct fields / dead CSS the detail-page final review flagged.

**Architecture:** Frontend-mostly. The tag combobox and thumbnail grid live in `comic.js` + `comic.html` + CSS, reusing the existing themed `.dd-menu`/`.dd-option` styles for the suggestions popup and the existing `/api/comics/:id/pages/:n/thumb` endpoint for thumbnails. A small `reader.js` addition makes the reader honor a `#p=N` URL hash as its start page. The cleanup trims `pages.go` structs and dead `app.css` rules. No new API, no DB change.

**Tech Stack:** Go-embedded `html/template`, echo v4, vanilla JS. Reuses existing `.dd-menu`/`.dd-option` CSS, native `loading="lazy"` images.

## A. Themed tag autocomplete (replace native datalist)

The detail page's 添加标签 input currently uses `<input list="d-taglist">` + `<datalist>`, which renders the light native OS popup — inconsistent with the app's custom themed dropdowns (project memory: themed-dropdowns-not-native).

- Remove `list="d-taglist"` and the `<datalist id="d-taglist">` from `comic.html`. Wrap `#d-newtag` in a `.combobox` (`position: relative`) and add a hidden `<div id="d-suggest" class="dd-menu" hidden>` inside it — reusing the existing `.dd-menu` container style and `.dd-option` row style for visual consistency.
- `comic.js` builds the suggestions:
  - On load, fetch `GET /api/tags` → keep the `[{id,name}]` list.
  - On input, filter existing tags by case-insensitive substring of the typed text, EXCLUDING tags already attached (already a chip in `#d-tags`); render up to ~8 matches as `.dd-option` rows in `#d-suggest`; show the popup (hide when the input is empty or no matches).
  - **Click a suggestion (or keyboard Enter on the active one) → add that tag immediately** (`POST /api/comics/:id/tags {name}` → append chip via the existing `makeChip`), clear the input, hide the popup.
  - **Enter on free text with no active suggestion → create+add** (same POST; `Upsert` dedupes).
  - Keyboard: ArrowDown/ArrowUp move the active `.dd-option` (`.active` class, same as dropdown.js); Enter commits; Escape closes. Click-outside closes.
  - The `+` button keeps working as "add the typed text".
- The chip append + dedupe guard (`.tag[data-id]`) is the existing Task-2 logic, unchanged.

## B. Page thumbnail grid

Below the `.detail` section, a tiled responsive grid of every page's thumbnail.

- `comic.html`: add `data-pages="{{.PageCount}}"` to the `.detail` section, and a `<section class="thumb-grid" id="thumb-grid" aria-label="页面预览"></section>` after it.
- `comic.js`: for `i` in `0 .. PageCount-1`, build `<a class="thumb-cell" href="/read/<id>#p=<i>"><img loading="lazy" src="/api/comics/<id>/pages/<i>/thumb" alt="第 i+1 页"></a>` and append to `#thumb-grid`. Native `loading="lazy"` defers off-screen thumbnails (a vertical grid, where native lazy works well), so all pages can be listed without freezing on large comics.
- Clicking a cell navigates to `/read/<id>#p=<i>` — the reader opens at that page (part C-reader below).
- CSS `.thumb-grid`: `display: grid; grid-template-columns: repeat(auto-fill, minmax(100px, 1fr)); gap: 10px; margin-top: 28px;`. `.thumb-cell`: a framed cell (2px line border, panel bg); `.thumb-cell img { width:100%; aspect-ratio: 2/3; object-fit: contain; background: var(--ink); display:block; }`. Hover: border → seal. Distinct from the reader's `.thumb`/`.thumbstrip` (no class clash — verified).

## Reader: honor `#p=N` start page

- `reader.js` init: after `total` is computed, if `location.hash` matches `^#p=(\d+)$`, use that number (clamped to `[0, total-1]`) as the initial `cur`, overriding the saved-progress `data-start`. Otherwise behavior is unchanged (resume at progress).

## C. Cleanup (final-review minors)

- `internal/server/pages.go`: drop `readerData.Author`, `.Rating`, `.Ratings` (the reader no longer renders them) and the matching assignments in `pageReader`; drop `comicData.Ratings` and its assignment in `pageComicDetail`. KEEP `comicData.PageCount` (now used by the thumbnail grid) and add it to the template's `.detail[data-pages]`.
- `internal/server/web/static/css/app.css`: remove the orphaned rules `.reader-bar .meta label`, `#addtag`, `#addtag input`, `#addtag button`, and `.tags .tag` / `.tags .tag button` (all reference elements removed when the reader was slimmed; verified `/tags` page does not use `.tags .tag`). Removing `.tags .tag` also resolves the detail chips' double-margin (its `margin:2px` no longer applies).

## Error Handling
- Tag add/delete failures alert and leave the UI unchanged (existing pattern). A failed `GET /api/tags` leaves the suggestions empty (typing still works to create new tags).
- A thumbnail that fails to load shows the browser's broken-image affordance in its cell; it does not break the grid or the page.
- `#p=N` out of range is clamped; a malformed hash is ignored (resume at progress).

## Testing
- **Server:** the existing `TestPageComicDetailRenders`/`TestReaderHasNoMetadataControls`/`TestBrowseCardLinksToComicDetail` stay green; add an assertion that `comic.html` serves `id="thumb-grid"` and `data-pages`. The struct-field removal must keep `go build/vet/test ./...` green.
- **Frontend (headless Chrome / browser, controller-run — no JS unit harness):** themed suggestions popup appears + filters + click-adds + Enter-creates + keyboard nav + Esc/click-outside close, and is styled like `.dd-menu` (not native); the thumbnail grid renders `PageCount` cells, lazy-loads, and a cell click opens `/read/:id#p=N` with the reader starting at page N; the cleanup didn't break the reader or detail page.
- `node --test` (zoom module) stays green; `gofmt` clean.

## Out of Scope
- The reader's existing horizontal thumbnail strip (unchanged).
- The `/tags` management page.
- Per-thumbnail selection/rating or any backend thumbnail change.
