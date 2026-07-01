# Immediate-Scan Button Design

**Goal:** Add a 「立即扫描」 button to the settings page that triggers an immediate background scan of the configured library path(s), with a guard that ignores repeat clicks while a manual scan is already running.

**Architecture:** A new `ScanNow()` method on the scan manager fires the existing `runScan` in a background goroutine, guarded by a manager-level "already scanning" flag. A new `POST /api/scan` endpoint calls it and reports whether a scan was actually started. The settings page gets a button that calls the endpoint and shows feedback. No DB, scanner-internals, or library-model changes.

**Tech Stack:** Existing — Go 1.24, echo v4, vanilla JS. No new dependencies.

## Background

The scan manager (`internal/scan/manager.go`) already has `runScan(ctx)` (scan + cache enforce) and four triggers (initial `Start`, async `Reconfigure`, cron, watch debouncer). There is no on-demand "scan now". The server reaches the manager through the `Reconfigurer` interface (`Reconfigure(ctx) error`). Saving scan settings already triggers an async rescan as a side effect, but there is no explicit button.

## Decisions (confirmed)

- **Async, fire-and-forget:** the HTTP request returns immediately; the scan runs in the background (a scan can take tens of seconds to minutes). No completion/progress reporting in this change.
- **Skip-if-running guard:** a manager-level `scanning` flag. `ScanNow()` starts a scan only if one isn't already in flight; otherwise it's a no-op. It returns whether it actually started, so the UI can distinguish 「已触发」 from 「扫描进行中」. The guard covers repeat manual clicks (the stated need); cross-trigger overlap (e.g. a cron scan running when the button is pressed) remains safe because the scanner's own `sync.Mutex` serializes the actual `Scan`.
- **Button placement:** bottom of the 「扫描方式」 card on the settings page.

## Components

### 1. Manager `ScanNow`
`internal/scan/manager.go`: add `scanning bool` to `Manager` (guarded by the existing `m.mu`).

```
// ScanNow starts a background scan of the configured libraries unless one is
// already running. Returns true if it started a scan, false if one was already
// in flight.
func (m *Manager) ScanNow() bool
```

Behavior: lock `m.mu`; if `m.scanning`, unlock and return `false`; else set `m.scanning = true`, capture the base context, unlock; launch a goroutine that runs `m.runScan(base)` (logging any error) and, via `defer`, clears `m.scanning` under the lock; return `true`. It uses `baseContext()` (the long-lived context from `Start`), never a request context.

### 2. Interface + endpoint
- `internal/server/server.go`: extend the `Reconfigurer` interface with `ScanNow() bool` (the manager already satisfies it after Component 1).
- Route `POST /api/scan` → handler `apiScanNow`: calls `s.reconf.ScanNow()` and returns `ok(c, map[string]any{"triggered": started})` (HTTP 200 either way).

### 3. Settings UI
- `internal/server/web/templates/settings.html`: add a button (id `scan-now`) at the bottom of the 「扫描方式」 card, plus a small status span.
- `internal/server/web/static/js/settings.js`: on click, briefly disable the button, `POST /api/scan`, and on the JSON response show 「已触发扫描，后台进行中」 when `triggered` is true, or 「扫描进行中，请稍候」 when false; re-enable the button. On network error, show 「触发失败」.

## Error Handling
- The endpoint never fails on a normal trigger — it returns 200 with `triggered: true|false`.
- A scan error inside the background goroutine is logged server-side (as the other triggers already do); it does not surface to the button (fire-and-forget).
- `runScan` already logs and continues on cache-enforce errors (unchanged).

## Concurrency
- `m.scanning` is read/written only under `m.mu`. The goroutine clears it via `defer` so a panicking/failing scan still resets the flag.
- The flag prevents repeat **manual** scans from piling up. Other triggers do not set the flag, but the scanner's `Scan` mutex already serializes overlapping scans safely, so no race is introduced.

## Testing
- **Manager** (`internal/scan/manager_test.go`): the existing `fakeScanner` is non-blocking, so the test adds an optional gate to it — a `block chan struct{}` whose `Scan` waits on it (when set) before returning. With the gate set, assert the first `ScanNow()` returns `true` (scan goroutine starts and blocks, holding `scanning` true); a second `ScanNow()` while it's held returns `false` (guard skips); after releasing the gate and letting the goroutine finish, a later `ScanNow()` returns `true` again. This holds the scan open so the guard is tested deterministically rather than racing the goroutine's flag-clear.
- **Server** (`internal/server/server_test.go`): `POST /api/scan` returns 200 with a body containing `"triggered"`; the `stubReconf` records that `ScanNow` was called. `stubReconf` gains a `ScanNow() bool` implementation.
- `go build/vet/test ./...` green; `gofmt` clean.

## Out of Scope
- Scan progress / completion / status reporting (fire-and-forget + toast only).
- Single-library-path change (`#2`) — explicitly deferred by the user.
- Changing the existing trigger behavior (cron/watch/Reconfigure/Start) — they are untouched.
