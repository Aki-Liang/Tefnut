# Reader Zoom & Fit-Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a fit mode (适配屏幕宽度 / 适配屏幕高度) plus free zoom to the comic reader, preserving each image's aspect ratio, consistently across the single / continuous / spread layout modes.

**Architecture:** Frontend-only. Page *layout* (per-comic `displayMode`, unchanged) and page *sizing* (new global fit + zoom) are independent. Sizing is driven by a `data-fit` attribute and a `--page-zoom` CSS variable on the reader root; CSS sizes every page image from those plus a JS-measured `--stage-h`. Pure zoom math lives in a testable ES module. No database, API, or Go logic changes.

**Tech Stack:** Go-embedded `reader.html` template, `app.css`, vanilla-JS `reader.js` (loaded as an ES module), new `zoom.js` ES module, `node --test` for unit tests, `localStorage` for persistence.

## Global Constraints

- Frontend-only. Touch ONLY: `internal/server/web/templates/reader.html`, `internal/server/web/static/css/app.css`, `internal/server/web/static/js/reader.js`, new `internal/server/web/static/js/zoom.js` + `zoom.test.mjs`, and `.github/workflows/ci.yml`. NO database, HTTP API, or Go logic changes.
- Default fit mode: `height`. Zoom: `1.0` = 100% = fit baseline. Step ±10% (`0.1`). Floor `0.1` (10%). NO upper cap. Switching fit resets zoom to `1.0`.
- Aspect ratio ALWAYS preserved: exactly one CSS dimension is set, the other is `auto`; `object-fit: contain` as a safety net. Remove `.continuous .cpage { min-height: 60vh }`.
- Persistence is GLOBAL `localStorage`, keys `readerFit` and `readerZoom` — NOT per-comic, no DB schema change.
- Three input paths plus direct entry: toolbar `−`/`+` buttons, an editable percent input, `Ctrl + wheel`, and keyboard `+`/`=` (in), `-` (out), `0` (reset). Keyboard zoom and Ctrl+wheel must be ignored when focus is in an `input`/`select`/`textarea`; existing arrow-key page nav in single/spread is unchanged.
- Layout modes (single/continuous/spread) and per-comic `displayMode` are untouched.
- `go build ./...`, `go vet ./...`, `go test ./...` stay green (a guard — the changed files are `go:embed`-ed). `node --test` passes for the zoom module.

## File Structure

- `internal/server/web/static/js/zoom.js` (new) — pure, DOM-free zoom/fit math: `clampZoom`, `stepZoom`, `parsePercent`, `normFit`, constants. The only unit-testable unit.
- `internal/server/web/static/js/zoom.test.mjs` (new) — `node --test` unit tests for zoom.js.
- `internal/server/web/static/js/reader.js` (modify) — becomes an ES module; gains view-state (`fit`, `zoom`), `applyView`, `--stage-h` measurement, and all control handlers.
- `internal/server/web/templates/reader.html` (modify) — script tag → `type="module"`; add fit toggle + zoom controls to the toolbar.
- `internal/server/web/static/css/app.css` (modify) — replace the three per-mode sizing rules with `data-fit`×`--page-zoom` rules; remove `min-height`.
- `.github/workflows/ci.yml` (modify) — add a `node --test` step.

## Notes on Testing (read before starting)

The pure zoom math (Task 1) gets real RED/GREEN unit tests via Node's built-in runner — no dependencies. The DOM/CSS/control behavior (Tasks 2–4) has no JS DOM-test harness in this repo and we deliberately do NOT add one (YAGNI). For those tasks the automated gate is: `go build ./...` succeeds (assets re-embed) AND a `curl` against the running server confirms the new asset content is served. Visual/behavioral correctness is verified in a real Chrome browser using each task's **Browser Check** block — exact steps with expected observations. The controller performs the Browser Check between tasks.

To run the server for Browser Checks (a build already exists from earlier; rebuild after asset changes so `go:embed` picks them up):

```bash
SCRATCH=/private/tmp/claude-501/-Users-liangrenzhi-ws-gomod-Tefnut/8f5a5c6e-2e5d-497c-aac6-ad47d6fcc976/scratchpad
go build -o "$SCRATCH/tefnut" ./cmd/tefnut
# config already at $SCRATCH/tefnut.yaml (library=/Users/liangrenzhi/comic, dataDir=$SCRATCH/tefnut-data)
"$SCRATCH/tefnut" -config "$SCRATCH/tefnut.yaml"   # serves http://127.0.0.1:8086 ; comic id 3 has 127 pages
```

---

### Task 1: Pure zoom/fit math module + unit tests

**Files:**
- Create: `internal/server/web/static/js/zoom.js`
- Test: `internal/server/web/static/js/zoom.test.mjs`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Produces (imported by reader.js in Task 2):
  - `MIN_ZOOM` = `0.1`, `ZOOM_STEP` = `0.1` (numbers)
  - `clampZoom(z: number) => number` — floors at `MIN_ZOOM`, no upper cap, rounds to 2 decimals, `NaN`/non-finite → `1`
  - `stepZoom(z: number, dir: number) => number` — `dir` is `+1`/`-1`; returns `clampZoom(z + dir*ZOOM_STEP)`
  - `parsePercent(str: string, fallback: number) => number` — integer percent → `/100` → `clampZoom`; unparseable → `fallback`
  - `normFit(s: string|null) => 'width'|'height'` — `'width'` only if `s === 'width'`, else `'height'`

- [ ] **Step 1: Write the failing unit tests**

Create `internal/server/web/static/js/zoom.test.mjs`:

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import { clampZoom, stepZoom, parsePercent, normFit, MIN_ZOOM, ZOOM_STEP } from './zoom.js';

test('constants', () => {
  assert.equal(MIN_ZOOM, 0.1);
  assert.equal(ZOOM_STEP, 0.1);
});

test('clampZoom floors at MIN_ZOOM, no upper cap, NaN->1', () => {
  assert.equal(clampZoom(0.05), 0.1);
  assert.equal(clampZoom(0.1), 0.1);
  assert.equal(clampZoom(1), 1);
  assert.equal(clampZoom(50), 50);        // 5000%, uncapped
  assert.equal(clampZoom(NaN), 1);
  assert.equal(clampZoom(Infinity), Infinity); // finite check lets +Inf through as-is? see impl note
});

test('stepZoom steps by 0.1 with no float drift and floors', () => {
  assert.equal(stepZoom(1, 1), 1.1);
  assert.equal(stepZoom(1.2, -1), 1.1);   // 1.2-0.1 float drift -> rounded
  assert.equal(stepZoom(0.1, -1), 0.1);   // floored, cannot go below 0.1
});

test('parsePercent parses, floors, and falls back', () => {
  assert.equal(parsePercent('150', 1), 1.5);
  assert.equal(parsePercent('100', 1), 1);
  assert.equal(parsePercent('5', 1), 0.1);   // 5% floored to 10%
  assert.equal(parsePercent('', 1.3), 1.3);  // empty -> fallback
  assert.equal(parsePercent('abc', 1.3), 1.3);
  assert.equal(parsePercent('150abc', 1), 1.5); // parseInt stops at junk
});

test('normFit defaults to height', () => {
  assert.equal(normFit('width'), 'width');
  assert.equal(normFit('height'), 'height');
  assert.equal(normFit(null), 'height');
  assert.equal(normFit('nonsense'), 'height');
});
```

> Implementation note for the `Infinity` assertion: keep it simple — `clampZoom` only special-cases `NaN`→1 (via `Number.isNaN`), not `Infinity`. `Infinity` is a finite-check edge no real input produces (percent input is `parseInt`, buttons step by 0.1). If you prefer `Number.isFinite` (which would map `Infinity`→1), change BOTH the impl and this assertion to expect `1`. Pick one and keep them consistent.

- [ ] **Step 2: Run tests to verify they fail**

Run: `node --test internal/server/web/static/js/zoom.test.mjs`
Expected: FAIL — `Cannot find module '.../zoom.js'` (module not created yet).

- [ ] **Step 3: Implement zoom.js**

Create `internal/server/web/static/js/zoom.js`:

```js
// Pure zoom/fit math for the reader view. No DOM access — unit-testable in node.
export const MIN_ZOOM = 0.1;
export const ZOOM_STEP = 0.1;

function round2(z) { return Math.round(z * 100) / 100; }

// clampZoom: floor at MIN_ZOOM, no upper cap, round to 2 decimals. NaN -> 1.
export function clampZoom(z) {
  if (Number.isNaN(z)) return 1;
  if (z < MIN_ZOOM) return MIN_ZOOM;
  return round2(z);
}

// stepZoom: one ±ZOOM_STEP increment, drift-corrected and floored.
export function stepZoom(z, dir) {
  return clampZoom(round2(z) + dir * ZOOM_STEP);
}

// parsePercent: integer percent string -> fraction, floored. Unparseable -> fallback.
export function parsePercent(str, fallback) {
  const pct = parseInt(str, 10);
  if (Number.isNaN(pct)) return fallback;
  return clampZoom(pct / 100);
}

// normFit: 'width' only when explicitly 'width', else default 'height'.
export function normFit(s) {
  return s === 'width' ? 'width' : 'height';
}
```

> This uses `Number.isNaN` (not `Number.isFinite`), so `clampZoom(Infinity)` returns `Infinity` — matching the test as written. If you switched the test to expect `1`, use `if (!Number.isFinite(z)) return 1;` instead of the `isNaN` line.

- [ ] **Step 4: Run tests to verify they pass**

Run: `node --test internal/server/web/static/js/zoom.test.mjs`
Expected: PASS — all 5 tests (`# pass 5`, `# fail 0`).

- [ ] **Step 5: Add the JS test step to CI**

In `.github/workflows/ci.yml`, add a step after the existing `test` step (ubuntu-latest ships Node ≥20, no install needed):

```yaml
      - name: js-test
        run: node --test internal/server/web/static/js/
```

- [ ] **Step 6: Commit**

```bash
git add internal/server/web/static/js/zoom.js internal/server/web/static/js/zoom.test.mjs .github/workflows/ci.yml
git commit -m "feat: pure zoom/fit math module with unit tests"
```

---

### Task 2: Reader view-state (fit/zoom) wired into reader.js as a module

**Files:**
- Modify: `internal/server/web/static/js/reader.js`
- Modify: `internal/server/web/templates/reader.html`

**Interfaces:**
- Consumes: `clampZoom`, `stepZoom`, `parsePercent`, `normFit` from `./zoom.js` (Task 1).
- Produces (used by Tasks 3–4):
  - reader root `el` (`#reader`) carries `data-fit` (`'width'|'height'`) and inline `--page-zoom` and `--stage-h` CSS properties.
  - `applyView()` — sets `el.dataset.fit`, `--page-zoom`, `--stage-h`, updates `#fitlabel`/`#zoominput`, writes `localStorage`.
  - `setZoom(z)`, `setFit(f)` — mutate state then `applyView()`. `setFit` resets zoom to `1`.
  - module-scope `let fit`, `let zoom`.

- [ ] **Step 1: Convert the reader script to a module**

In `internal/server/web/templates/reader.html`, change the scripts block:

```html
{{define "scripts"}}<script type="module" src="/static/js/reader.js"></script>{{end}}
```

- [ ] **Step 2: Add the import and view-state to reader.js**

At the TOP of `internal/server/web/static/js/reader.js`, before the existing `const el = ...` line, add:

```js
import { clampZoom, stepZoom, parsePercent, normFit } from './zoom.js';
```

Immediately after the existing block that defines `stage`, `counter`, `thumbsEl`, `stripEl` (near the top), add the view-state and helpers:

```js
// ---- view state: fit mode + zoom (global, localStorage) ----
let fit = normFit(localStorage.getItem('readerFit'));
let zoom = clampZoom(parseFloat(localStorage.getItem('readerZoom')));

function measureStage() {
  el.style.setProperty('--stage-h', stage.clientHeight + 'px');
}
function updateZoomLabel() {
  const input = document.getElementById('zoominput');
  if (input && document.activeElement !== input) input.value = String(Math.round(zoom * 100));
}
function applyView() {
  el.dataset.fit = fit;
  el.style.setProperty('--page-zoom', String(zoom));
  measureStage();
  const fl = document.getElementById('fitlabel');
  if (fl) fl.textContent = fit === 'width' ? '宽' : '高';
  updateZoomLabel();
  localStorage.setItem('readerFit', fit);
  localStorage.setItem('readerZoom', String(zoom));
}
function setZoom(z) { zoom = clampZoom(z); applyView(); }
function setFit(f) { fit = normFit(f); zoom = 1; applyView(); }
window.addEventListener('resize', measureStage);
```

> `parseFloat(null)` is `NaN`, and `clampZoom(NaN)` is `1`, so a first-ever load defaults to 100%. The `#fitlabel`/`#zoominput` lookups are null-safe so this task works before Task 4 adds those elements.

- [ ] **Step 3: Call applyView() during init**

In the init block at the bottom of `reader.js`, the last line is:

```js
if (total > 0) setMode(mode); else counter.textContent = '无可显示页面';
```

Change it to apply the view after the first render:

```js
if (total > 0) { setMode(mode); applyView(); } else counter.textContent = '无可显示页面';
```

- [ ] **Step 4: Rebuild and verify the module loads & serves**

Run:
```bash
go build ./... && echo BUILD-OK
SCRATCH=/private/tmp/claude-501/-Users-liangrenzhi-ws-gomod-Tefnut/8f5a5c6e-2e5d-497c-aac6-ad47d6fcc976/scratchpad
go build -o "$SCRATCH/tefnut" ./cmd/tefnut
```
Expected: `BUILD-OK`, binary builds (the embedded assets now include zoom.js).

Then start the server (see "Notes on Testing") and verify the asset wiring:
```bash
curl -fsS -D - -o /dev/null http://127.0.0.1:8086/static/js/zoom.js | grep -iE 'HTTP/|content-type'
curl -fsS http://127.0.0.1:8086/static/js/reader.js | grep -c "from './zoom.js'"
```
Expected: zoom.js returns `200` with a JavaScript `content-type` (e.g. `text/javascript`) — required for ES-module imports to load; reader.js contains the import (`1`).

- [ ] **Step 5: Browser Check**

Open `http://127.0.0.1:8086/read/3` in Chrome. In DevTools console:
```js
getComputedStyle(document.getElementById('reader')).getPropertyValue('--page-zoom'); // "1"
document.getElementById('reader').dataset.fit;                                       // "height"
localStorage.getItem('readerFit');   // "height"
localStorage.getItem('readerZoom');  // "1"
```
Expected: values as commented. No console errors (a module-load or import failure would show here). Page still renders pages as before (CSS not changed yet).

- [ ] **Step 6: Commit**

```bash
git add internal/server/web/static/js/reader.js internal/server/web/templates/reader.html
git commit -m "feat: reader view-state for fit mode and zoom"
```

---

### Task 3: CSS sizing rules — data-fit × zoom, aspect ratio preserved

**Files:**
- Modify: `internal/server/web/static/css/app.css`

**Interfaces:**
- Consumes: `#reader[data-fit]`, `--page-zoom`, `--stage-h` (Task 2); `#page`, `.cpage`, `.spread`, `.continuous` DOM (existing render code).
- Produces: every page image sized by fit × zoom with aspect ratio preserved (consumed visually by Task 4).

- [ ] **Step 1: Replace the single-mode rule**

In `internal/server/web/static/css/app.css`, replace the existing line:

```css
#page { max-height: 100%; max-width: 100%; box-shadow: 0 0 0 1px var(--line); }
```

with a shared base plus fit-driven sizing (defaults for the vars so the rules are robust if JS hasn't set them yet):

```css
#reader { --page-zoom: 1; --stage-h: 100vh; }
#page, .cpage, .spread img { box-shadow: 0 0 0 1px var(--line); display: block; object-fit: contain; }

/* fit height: image height = stage height × zoom, width follows (no distortion) */
#reader[data-fit="height"] #page,
#reader[data-fit="height"] .cpage {
  height: calc(var(--stage-h) * var(--page-zoom)); width: auto;
  max-width: none; max-height: none;
}
/* fit width: image width = container width × zoom, height follows */
#reader[data-fit="width"] #page,
#reader[data-fit="width"] .cpage {
  width: calc(100% * var(--page-zoom)); height: auto;
  max-width: none; max-height: none;
}
```

- [ ] **Step 2: Replace the continuous and spread rules**

In the `/* reader display modes */` section, replace these existing lines:

```css
.spread img { max-height: 100%; max-width: 50%; object-fit: contain; box-shadow: 0 0 0 1px var(--line); }
.continuous { height: 100%; width: 100%; overflow-y: auto; display: flex; flex-direction: column; align-items: center; gap: 10px; padding: 10px 0; scrollbar-width: thin; }
.continuous .cpage { width: min(100%, 760px); height: auto; min-height: 60vh; display: block; box-shadow: 0 0 0 1px var(--line); }
```

with (note: `min-height` removed; continuous scrolls both axes; `safe center` keeps a zoomed-wider-than-viewport page scrollable to its left edge; spread sizing follows fit × zoom):

```css
.continuous { height: 100%; width: 100%; overflow: auto; display: flex; flex-direction: column; align-items: safe center; gap: 10px; padding: 10px 0; scrollbar-width: thin; }
#reader[data-fit="height"] .spread img { height: calc(var(--stage-h) * var(--page-zoom)); width: auto; max-width: none; max-height: none; }
#reader[data-fit="width"]  .spread img { width: calc(50% * var(--page-zoom)); height: auto; max-width: none; max-height: none; }
```

> `.spread img`'s shared `box-shadow`/`object-fit`/`display` now come from the base rule added in Step 1, so they are not repeated here. The `.spread` flex container rule (`display:flex; gap:2px; align-items:center; justify-content:center; height:100%; width:100%`) is unchanged.

- [ ] **Step 3: Rebuild and verify served CSS**

Run:
```bash
go build ./... && echo BUILD-OK
SCRATCH=/private/tmp/claude-501/-Users-liangrenzhi-ws-gomod-Tefnut/8f5a5c6e-2e5d-497c-aac6-ad47d6fcc976/scratchpad
go build -o "$SCRATCH/tefnut" ./cmd/tefnut
curl -fsS http://127.0.0.1:8086/static/css/app.css | grep -c 'data-fit="height"'
curl -fsS http://127.0.0.1:8086/static/css/app.css | grep -c 'min-height: 60vh'
```
(Restart the server first so the rebuilt binary serves the new CSS.) Expected: `data-fit="height"` present (`2`), `min-height: 60vh` gone (`0`).

- [ ] **Step 4: Browser Check (aspect ratio preserved)**

Open `http://127.0.0.1:8086/read/3` (single mode). In DevTools console:
```js
const im = document.getElementById('page');
(im.clientWidth / im.clientHeight).toFixed(3) === (im.naturalWidth / im.naturalHeight).toFixed(3); // true
```
Expected: `true` — displayed ratio equals the image's natural ratio (no distortion). Temporarily exercise the vars:
```js
const r = document.getElementById('reader');
r.dataset.fit = 'width'; // page should widen to fill the stage width
r.style.setProperty('--page-zoom', '1.5'); // page should grow ~1.5×, ratio still preserved
```
Re-check the ratio equality (still `true`). Then reload to restore. Repeat the ratio check in continuous (`/read/3` after setting displayMode continuous via the dropdown) and spread modes. Screenshot single+continuous for the record.

- [ ] **Step 5: Commit**

```bash
git add internal/server/web/static/css/app.css
git commit -m "feat: fit-mode + zoom CSS sizing, drop min-height distortion"
```

---

### Task 4: Toolbar controls and all input handlers

**Files:**
- Modify: `internal/server/web/templates/reader.html`
- Modify: `internal/server/web/static/js/reader.js`

**Interfaces:**
- Consumes: `setZoom`, `setFit`, `stepZoom`, `parsePercent`, `zoom`, `fit`, `applyView` (Task 2); the `#fitlabel`/`#zoominput` elements this task adds (referenced null-safely in Task 2).
- Produces: user-facing fit toggle, zoom buttons, editable percent input, Ctrl+wheel zoom, keyboard zoom.

- [ ] **Step 1: Add the controls to the toolbar**

In `internal/server/web/templates/reader.html`, inside `.reader-bar`, immediately AFTER the `</select>` of `#modetoggle` and before `<button class="pagebtn" id="stepbtn"...>`, insert:

```html
      <button class="pagebtn" id="fittoggle" title="适配方式">适配: <span id="fitlabel">高</span></button>
      <span class="zoomgroup">
        <button class="pagebtn" id="zoomout" title="缩小" aria-label="缩小">−</button>
        <input id="zoominput" class="zoominput" inputmode="numeric" title="缩放百分比" value="100" aria-label="缩放百分比">
        <span class="zoompct">%</span>
        <button class="pagebtn" id="zoomin" title="放大" aria-label="放大">+</button>
      </span>
```

- [ ] **Step 2: Style the zoom group**

In `internal/server/web/static/css/app.css`, append near the other `.reader-bar` rules:

```css
.zoomgroup { display: inline-flex; align-items: center; gap: 4px; }
.zoominput { width: 52px; text-align: right; background: var(--ink); color: var(--paper); border: 2px solid var(--line); border-radius: 6px; padding: 5px 6px; font-family: var(--font-mono); font-size: 13px; }
.zoompct { color: var(--muted); font-family: var(--font-mono); font-size: 13px; }
```

- [ ] **Step 3: Wire the control handlers in reader.js**

In `internal/server/web/static/js/reader.js`, after the view-state block from Task 2 (after the `window.addEventListener('resize', measureStage);` line), add:

```js
// ---- view controls: fit toggle, zoom buttons, percent input, Ctrl+wheel ----
document.getElementById('zoomin').onclick = () => setZoom(stepZoom(zoom, 1));
document.getElementById('zoomout').onclick = () => setZoom(stepZoom(zoom, -1));
document.getElementById('fittoggle').onclick = () => setFit(fit === 'width' ? 'height' : 'width');
const zoomInput = document.getElementById('zoominput');
zoomInput.addEventListener('change', () => {
  setZoom(parsePercent(zoomInput.value, zoom));
  zoomInput.value = String(Math.round(zoom * 100)); // force-normalize the field on explicit commit
});
stage.addEventListener('wheel', (e) => {
  if (!e.ctrlKey) return;        // only Ctrl+wheel zooms; plain wheel scrolls
  e.preventDefault();            // suppress the browser's own ctrl-wheel zoom over the reader
  setZoom(stepZoom(zoom, e.deltaY < 0 ? 1 : -1));
}, { passive: false });
```

- [ ] **Step 4: Add keyboard zoom to the existing keydown handler**

In `reader.js`, find the existing keyboard handler:

```js
document.addEventListener('keydown', (e) => {
  if (mode === 'continuous') return; // native scroll
  if (e.key === 'ArrowRight') { dir === 'rtl' ? back() : advance(); }
  if (e.key === 'ArrowLeft') { dir === 'rtl' ? advance() : back(); }
});
```

Replace it with (zoom keys work in ALL modes; guarded against typing; arrows unchanged):

```js
document.addEventListener('keydown', (e) => {
  const t = e.target;
  const typing = t && (t.tagName === 'INPUT' || t.tagName === 'SELECT' || t.tagName === 'TEXTAREA');
  if (!typing) {
    if (e.key === '+' || e.key === '=') { setZoom(stepZoom(zoom, 1)); return; }
    if (e.key === '-') { setZoom(stepZoom(zoom, -1)); return; }
    if (e.key === '0') { setZoom(1); return; }
  }
  if (mode === 'continuous') return; // native scroll for arrows
  if (e.key === 'ArrowRight') { dir === 'rtl' ? back() : advance(); }
  if (e.key === 'ArrowLeft') { dir === 'rtl' ? advance() : back(); }
});
```

- [ ] **Step 5: Rebuild and verify served markup**

Run:
```bash
go build ./... && go vet ./... && go test ./... 2>&1 | tail -3
SCRATCH=/private/tmp/claude-501/-Users-liangrenzhi-ws-gomod-Tefnut/8f5a5c6e-2e5d-497c-aac6-ad47d6fcc976/scratchpad
go build -o "$SCRATCH/tefnut" ./cmd/tefnut
node --test internal/server/web/static/js/   # zoom unit tests still green
```
Expected: Go all green; `node --test` passes. Restart the server, then:
```bash
curl -fsS http://127.0.0.1:8086/read/3 | grep -c 'id="zoominput"'   # 1
```
Expected: the zoom input is in the served reader HTML (`1`).

- [ ] **Step 6: Browser Check (full behavior across modes)**

Open `http://127.0.0.1:8086/read/3` in Chrome. For EACH layout mode (单页, then switch to 连续, then 双页 via the mode dropdown), verify:
1. **Fit toggle:** click 适配 — label flips 高⟷宽, the page resizes accordingly, and `localStorage.getItem('readerFit')` updates. Switching fit resets the percent field to `100`.
2. **Buttons:** `+`/`−` change the page size by 10% per click; the percent field tracks (e.g. `110`, `120` …). Below 100% keeps shrinking; the floor is `10`.
3. **Direct input:** type `250` + Enter → page jumps to 250%; type `5` + Enter → field normalizes to `10` (floor).
4. **Ctrl+wheel:** hold Ctrl and scroll the wheel over the page — zoom changes, the browser's own zoom does NOT trigger.
5. **Keyboard:** `+`/`−`/`0` change/reset zoom in every mode (including 连续); but while focus is in the 作者/标签/缩放 fields, typing `-`/`0` edits text and does NOT zoom.
6. **Aspect ratio:** at any zoom, `im.clientWidth/im.clientHeight ≈ im.naturalWidth/im.naturalHeight` for a visible page image (no distortion).
7. **Persistence:** reload the page — fit mode and zoom are restored from `localStorage`.

Record a screenshot of 连续 mode at fit=高 100% (the original complaint) and at fit=宽 to show the fix. Reset zoom to 100% and fit to 高 before finishing so the demo state is clean.

- [ ] **Step 7: Commit**

```bash
git add internal/server/web/templates/reader.html internal/server/web/static/css/app.css internal/server/web/static/js/reader.js
git commit -m "feat: reader fit toggle and zoom controls (buttons, wheel, keyboard, input)"
```

---

## Self-Review

**Spec coverage:**
- Two independent dimensions (layout vs sizing) — Task 2/3 (data-fit + zoom independent of displayMode). ✓
- Global localStorage persistence (readerFit/readerZoom) — Task 2. ✓
- Fit modes height/width, default height — Task 1 (normFit) + Task 2/3. ✓
- Zoom 100% baseline, ±10% step, floor 10%, no cap, switch-fit resets — Task 1 (clamp/step) + Task 2 (setFit resets). ✓
- Aspect ratio preserved + min-height removed — Task 3. ✓
- Toolbar controls + buttons/wheel/keyboard/input — Task 4. ✓
- Keyboard/wheel guarded vs typing; arrows unchanged — Task 4 Step 4. ✓
- Go/build/test green + node --test — Tasks 1/4 gates. ✓
- Nothing touches DB/API/Go/displayMode — Global Constraints + file list. ✓

**Placeholder scan:** No TBD/TODO; every code step shows complete code; the only judgment call (Infinity handling) is explicitly bounded with a "pick one, keep consistent" note. ✓

**Type consistency:** `clampZoom`/`stepZoom`/`parsePercent`/`normFit` signatures match between Task 1 (definition), the Task 1 tests, and Task 2/4 call sites. `setZoom`/`setFit`/`applyView`/`measureStage`/`updateZoomLabel` defined in Task 2 and used in Task 4. CSS vars `--page-zoom`/`--stage-h`/`data-fit` set in Task 2, consumed in Task 3. Element ids `fittoggle`/`fitlabel`/`zoomin`/`zoomout`/`zoominput` consistent between Task 4 markup and Task 2/4 JS. ✓
