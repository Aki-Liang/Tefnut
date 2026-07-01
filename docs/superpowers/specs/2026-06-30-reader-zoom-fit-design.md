# Reader Zoom & Fit-Mode Design

**Goal:** Give the comic reader an explicit, consistent page-sizing model — a fit mode (适配屏幕宽度 / 适配屏幕高度) plus free zoom (放大/缩小) — that preserves each image's aspect ratio and behaves identically across the single / continuous / spread layout modes.

**Architecture:** A frontend-only change. Page *layout* (single/continuous/spread, already persisted per-comic via `displayMode`) and page *sizing* (fit-mode + zoom, new) become two independent dimensions. Sizing is driven entirely by two values applied to the reader root: a `data-fit` attribute (`width|height`) and a `--page-zoom` CSS custom property. CSS rules size every page image from those; switching fit or zoom mutates only those two values — no DOM rebuild. No database, API, or Go changes.

**Tech Stack:** Existing stack — Go-embedded `reader.html` template, `app.css`, vanilla-JS `reader.js`. `localStorage` for persistence.

## Motivation

Today each layout mode hard-codes a different sizing strategy: single mode fits to viewport height (`#page { max-height:100%; max-width:100% }`, app.css:216), continuous mode fits to a fixed 760px column (`.continuous .cpage { width:min(100%,760px) }`, app.css:293). Switching single→continuous therefore makes a tall page visibly *wider* with no way to change it, and `.continuous .cpage { min-height:60vh }` can stretch short/wide pages off-ratio. There is no zoom at all.

## Decisions (confirmed)

- **Two dimensions.** Layout (single/continuous/spread) is unchanged and stays per-comic in the DB. Fit-mode + zoom are new and apply on top of *every* layout.
- **Global persistence.** Fit-mode and zoom are a global viewing preference in `localStorage` (not per-comic, no DB schema change).
- **Fit modes:** `height` (适配屏幕高度) and `width` (适配屏幕宽度). Default `height` (matches the familiar single-page look). Aspect ratio is always preserved.
- **Zoom:** `1.0` = 100% = the fit baseline. Step ±10%. Floor 10% (`0.1`) to prevent zero/negative; **no upper cap**. Direct numeric entry allowed.
- **Switching fit resets zoom to 100%.**
- **All three input methods:** toolbar buttons, `Ctrl + mouse wheel`, and keyboard.

## View State

A small view-state holder in `reader.js`:

- `fit`: `'height' | 'width'`, read from `localStorage['readerFit']`, default `'height'`.
- `zoom`: number, read from `localStorage['readerZoom']`, default `1.0`, floored at `0.1`.

Both are written back to `localStorage` on every change. `applyView()` sets `el.dataset.fit = fit`, sets `--page-zoom` on the reader root, updates the percent display, and (for fit=height) keeps `--stage-h` current. It is called on init, on fit change, on zoom change, and on window resize.

## Sizing Rules (aspect ratio preserved)

A JS-measured `--stage-h` (= `.reader-stage` client height, updated on resize) makes height-fit accurate despite the toolbar/thumbstrip. CSS keyed off `data-fit` and `--page-zoom`:

- **Fit height:** image `height = var(--stage-h) * var(--page-zoom)`, `width: auto`, `max-width/height: none`.
- **Fit width:** image `width = 100% * var(--page-zoom)`, `height: auto`, `max-width/height: none`.

Applied per layout:
- **Single** (`#page`): the one image follows the rule above against the stage.
- **Continuous** (`.cpage`): every stacked page follows the rule; zoom>100% in width-fit produces horizontal scroll, in height-fit produces taller-than-screen pages.
- **Spread** (`.spread img`, two side-by-side): fit-width sizes each image to `(50% of stage) * zoom`; fit-height sizes each to `stage-h * zoom`. The other dimension stays `auto`.

Because exactly one dimension is set and the other is `auto`, images never distort. The `min-height: 60vh` rule is removed; lazy-load placeholders in continuous mode reserve space via the same height-driven sizing (height-fit) or an `object-fit: contain` fallback so an unloaded/odd image is never stretched.

## Toolbar Controls

Added to `.reader-bar` (in `reader.html`), next to the existing mode dropdown:

- **Fit toggle:** a control switching 适配高度 ⟷ 适配宽度 (button or two-option select).
- **Zoom group:** `[ − ] [ NNN% ▭ ] [ + ]` — minus button, an editable percent input, plus button.

## Interactions

- **Buttons:** `−` / `+` change zoom by ∓/±0.1 (floored at 0.1). The percent input commits on `change`/`Enter`: parse integer percent → `zoom = pct/100`, floor 0.1; invalid/empty reverts to the current value.
- **Ctrl + wheel:** a `wheel` listener on `.reader-stage` with `ctrlKey` calls `preventDefault()` and steps zoom by the wheel direction (±0.1). (This also suppresses the browser's own ctrl-wheel zoom over the reader.)
- **Keyboard:** `+`/`=` zoom in, `-` zoom out, `0` reset to 100%. These work in all layouts (including continuous, where arrow keys remain native scroll). A guard ignores these keys when focus is in an `input`/`select`/`textarea`, so typing an author/tag/zoom value is unaffected. Existing arrow-key page navigation in single/spread is unchanged.

## Unchanged

Database, HTTP API, all Go code, and the per-comic `displayMode` (single/continuous/spread) are untouched. The feature lives entirely in `reader.html`, `app.css`, and `reader.js`.

## Testing

- **Go suite:** unaffected; `go build/vet/test ./...` must stay green (a guard, not new coverage).
- **Frontend:** the repo has no JS unit harness, so verification is headless-Chrome driven against the running server: load `/read/<id>`, for each layout mode switch fit-mode and zoom via every input path (button, Ctrl+wheel, keyboard, direct input), assert the rendered image's natural-vs-displayed dimensions stay in aspect ratio, assert `localStorage` keys persist across reload, and screenshot for visual confirmation. The plan will spell out the concrete checks. Limits (no JS unit tests) are reported honestly rather than papered over.

## Out of Scope

- Per-comic persistence of fit/zoom (chosen global instead).
- Pan-by-drag when zoomed (native scrollbars handle overflow; revisit only if requested).
- Changing the layout modes themselves or their per-comic persistence.
- Any backend/API/schema change.
