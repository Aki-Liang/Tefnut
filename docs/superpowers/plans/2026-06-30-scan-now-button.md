# Immediate-Scan Button Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A 「立即扫描」 button on the settings page triggers an immediate background scan of the configured library, skipping repeat clicks while a scan is already running.

**Architecture:** A guarded async `Manager.ScanNow() bool` reuses the existing `runScan`; a `POST /api/scan` endpoint calls it and returns whether it started; the settings page button calls the endpoint and shows feedback. No DB/scanner-internals/library-model changes.

**Tech Stack:** Go 1.24, echo v4, vanilla JS. No new dependencies.

## Global Constraints

- Async fire-and-forget: the endpoint returns 200 immediately; the scan runs in a goroutine. No progress/completion reporting.
- Skip-if-running guard: a manager-level `scanning bool` guarded by the existing `m.mu`, cleared via `defer` so a failing scan still resets it. `ScanNow()` returns `true` if it started a scan, `false` if one was already in flight.
- The guard covers repeat manual clicks; cross-trigger overlap stays safe via the scanner's own `Scan` mutex (existing). Do NOT change the cron/watch/Reconfigure/Start triggers.
- `ScanNow` uses the long-lived base context (`m.baseCtx`, falling back to `context.Background()`), never a request context.
- `go build ./... && go vet ./... && go test ./...` green; `gofmt` clean.

## File Structure

- `internal/scan/manager.go` (modify) — `scanning` field + `ScanNow()`.
- `internal/scan/manager_test.go` (modify) — blockable `fakeScanner` + `TestScanNowGuard`.
- `internal/server/server.go` (modify) — `Reconfigurer` += `ScanNow()`; route `POST /api/scan`.
- `internal/server/api_settings.go` (modify) — `apiScanNow` handler.
- `internal/server/server_test.go` (modify) — `stubReconf` += `ScanNow`; `TestApiScanNow`.
- `internal/server/web/templates/settings.html` (modify) — button + status span.
- `internal/server/web/static/js/settings.js` (modify) — click handler.

---

### Task 1: Manager.ScanNow with skip-if-running guard

**Files:**
- Modify: `internal/scan/manager.go`
- Test: `internal/scan/manager_test.go`

**Interfaces:**
- Produces (consumed by Task 2): `func (m *Manager) ScanNow() bool` — starts a background scan unless one is already running; returns whether it started.

- [ ] **Step 1: Make the test fakeScanner blockable, and write the failing test**

In `internal/scan/manager_test.go`, change the `fakeScanner` struct and its `Scan` to support an optional blocking gate (guarded by the existing `f.mu` so setting it is race-free), and add a setter:

```go
type fakeScanner struct {
	mu    sync.Mutex
	n     int
	ch    chan struct{}
	block chan struct{} // if non-nil, Scan waits on it before returning
}

func (f *fakeScanner) Scan(ctx context.Context) error {
	f.mu.Lock()
	f.n++
	b := f.block
	f.mu.Unlock()
	if f.ch != nil {
		select {
		case f.ch <- struct{}{}:
		default:
		}
	}
	if b != nil {
		<-b
	}
	return nil
}

func (f *fakeScanner) setBlock(ch chan struct{}) { f.mu.Lock(); f.block = ch; f.mu.Unlock() }
```

Add this test (it does NOT call `Start`, so there's no initial scan to block; `ScanNow` falls back to `context.Background()`):

```go
func TestScanNowGuard(t *testing.T) {
	settings, paths := newRepos(t)
	fs := &fakeScanner{}
	m := New(fs, settings, paths, t.TempDir(), 0)

	block := make(chan struct{})
	fs.setBlock(block)

	if !m.ScanNow() {
		t.Fatal("first ScanNow should start a scan")
	}
	if m.ScanNow() {
		t.Fatal("second ScanNow should be skipped while a scan is running")
	}

	close(block) // release the held scan so the goroutine can clear the flag

	started := false
	for i := 0; i < 200; i++ {
		if m.ScanNow() {
			started = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !started {
		t.Fatal("ScanNow should start again after the prior scan finished")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/scan/ -run TestScanNowGuard -v`
Expected: FAIL — `m.ScanNow undefined (type *Manager has no field or method ScanNow)`.

- [ ] **Step 3: Add the scanning field and ScanNow**

In `internal/scan/manager.go`, add a field to the `Manager` struct in the mutex-guarded group (after `debounce`):

```go
	debounce time.Duration
	scanning bool // guarded by mu; true while a ScanNow scan is in flight
```

Add the method (place it near `Reconfigure`):

```go
// ScanNow starts a background scan of the configured libraries unless one is
// already running. It returns true if it started a scan, false if one was
// already in flight. The scan uses the long-lived base context, never a
// request context.
func (m *Manager) ScanNow() bool {
	m.mu.Lock()
	if m.scanning {
		m.mu.Unlock()
		return false
	}
	m.scanning = true
	base := m.baseCtx
	m.mu.Unlock()
	if base == nil {
		base = context.Background()
	}
	go func() {
		defer func() {
			m.mu.Lock()
			m.scanning = false
			m.mu.Unlock()
		}()
		if err := m.runScan(base); err != nil {
			log.Printf("scan: manual scan: %v", err)
		}
	}()
	return true
}
```

(`context` and `log` are already imported in `manager.go`.)

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/scan/ -run TestScanNowGuard -v` then `go test -race ./internal/scan/`
Expected: PASS; race-clean (the `scanning` flag and `fakeScanner.block` are both mutex-guarded).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/scan/manager.go internal/scan/manager_test.go
git add internal/scan/manager.go internal/scan/manager_test.go
git commit -m "feat: Manager.ScanNow with skip-if-running guard"
```

---

### Task 2: Endpoint, interface, and settings button

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/api_settings.go`
- Modify: `internal/server/server_test.go`
- Modify: `internal/server/web/templates/settings.html`
- Modify: `internal/server/web/static/js/settings.js`

**Interfaces:**
- Consumes: `func (m *Manager) ScanNow() bool` (Task 1).
- Produces: `Reconfigurer` interface gains `ScanNow() bool`; route `POST /api/scan` → `apiScanNow` returning `{"triggered": bool}`.

- [ ] **Step 1: Extend the interface and add the route**

In `internal/server/server.go`, extend the `Reconfigurer` interface:

```go
// Reconfigurer is satisfied by *scan.Manager.
type Reconfigurer interface {
	Reconfigure(ctx context.Context) error
	ScanNow() bool
}
```

In the route block (after `api.DELETE("/settings/paths/:id", s.apiDeletePath)`), add:

```go
	api.POST("/scan", s.apiScanNow)
```

- [ ] **Step 2: Add the handler**

In `internal/server/api_settings.go`, add:

```go
func (s *Server) apiScanNow(c echo.Context) error {
	started := s.reconf.ScanNow()
	return ok(c, map[string]any{"triggered": started})
}
```

- [ ] **Step 3: Update the test stub and write the endpoint test**

In `internal/server/server_test.go`, the stub currently is `type stubReconf struct{ calls int }` with a `Reconfigure` method. Add a counter and the new method:

```go
type stubReconf struct {
	calls int
	scans int
}

func (s *stubReconf) Reconfigure(ctx context.Context) error { s.calls++; return nil }
func (s *stubReconf) ScanNow() bool                          { s.scans++; return true }
```

Add the endpoint test:

```go
func TestApiScanNow(t *testing.T) {
	_, e, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/scan", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"triggered":true`) {
		t.Fatalf("body=%s, want triggered:true", rec.Body.String())
	}
}
```

- [ ] **Step 4: Run the server tests**

Run: `go build ./... && go vet ./... && go test ./internal/server/ -run 'TestApiScanNow|TestSettings|TestApiAdd' -v`
Expected: PASS (the `Reconfigurer` interface now requires `ScanNow`, which `*scan.Manager` provides via Task 1, and `stubReconf` provides for tests).

- [ ] **Step 5: Add the settings button**

In `internal/server/web/templates/settings.html`, inside the 「扫描方式」 `<section class="card-box">`, immediately AFTER the closing `</form>` of `#scanform` and BEFORE the section's closing `</section>`, add:

```html
  <div class="scan-now-row">
    <button type="button" id="scan-now" class="card-box-btn">立即扫描</button>
    <span id="scan-now-status" class="hint"></span>
  </div>
```

- [ ] **Step 6: Wire the button in settings.js**

Append to `internal/server/web/static/js/settings.js`:

```js
// immediate scan
const scanNowBtn = document.getElementById('scan-now');
const scanNowStatus = document.getElementById('scan-now-status');
scanNowBtn.addEventListener('click', () => {
  scanNowBtn.disabled = true;
  scanNowStatus.textContent = '触发中…';
  fetch('/api/scan', { method: 'POST' })
    .then((r) => r.json())
    .then((j) => {
      scanNowStatus.textContent = (j.data && j.data.triggered)
        ? '已触发扫描，后台进行中'
        : '扫描进行中，请稍候';
    })
    .catch(() => { scanNowStatus.textContent = '触发失败'; })
    .finally(() => { scanNowBtn.disabled = false; });
});
```

- [ ] **Step 7: Build, full suite, gofmt, commit**

```bash
go build ./... && go vet ./... && go test ./...
gofmt -w internal/server/server.go internal/server/api_settings.go internal/server/server_test.go
git add internal/server/server.go internal/server/api_settings.go internal/server/server_test.go internal/server/web/templates/settings.html internal/server/web/static/js/settings.js
git commit -m "feat: settings 立即扫描 button via POST /api/scan"
```

Expected: all green; `gofmt -l .` prints nothing.

---

## Self-Review

**Spec coverage:**
- `Manager.ScanNow() bool` guarded async, returns started? — Task 1. ✓
- `scanning` flag under `m.mu`, cleared via `defer` — Task 1 Step 3. ✓
- base context, not request context — Task 1 Step 3 (`m.baseCtx` / `context.Background()`). ✓
- `Reconfigurer` += `ScanNow()`; `POST /api/scan` → `{triggered}` — Task 2 Steps 1-2. ✓
- Button in 扫描方式 card; toast on triggered true/false; 触发失败 on error — Task 2 Steps 5-6. ✓
- Guard tested deterministically (blockable fake); endpoint tested (200 + triggered) — Tasks 1 & 2. ✓
- Other triggers untouched — neither task modifies cron/watch/Reconfigure/Start. ✓

**Placeholder scan:** No TBD/TODO; every step shows complete code and exact commands.

**Type consistency:** `ScanNow() bool` is defined in Task 1, declared on the interface and called via `s.reconf.ScanNow()` in Task 2, and implemented on `stubReconf` in Task 2 — all `() bool`. `apiScanNow` returns `{"triggered": started}`; settings.js reads `j.data.triggered`; the test asserts `"triggered":true` — consistent. Route `POST /api/scan` matches the handler and the fetch URL.
