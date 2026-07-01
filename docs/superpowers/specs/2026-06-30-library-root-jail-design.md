# Library-Path Root Jail Design

**Goal:** Stop the library-path API from adding *arbitrary* server directories. A directory added via `POST /api/settings/paths` must resolve to a location inside one of a configured set of allowed roots. This closes the local-file-disclosure vector on an untrusted LAN without adding authentication (the app stays single-user, no-auth, and keeps binding `:8086` for family-LAN access).

**Architecture:** A small, pure, well-tested path-containment check gates the one mutating handler (`apiAddPath`). The allowed roots come from config and are passed into the server. No database, scan, or reader changes.

**Tech Stack:** Existing — Go 1.24, echo v4, `path/filepath`. No new dependencies.

## Background

`apiAddPath` (`internal/server/api_settings.go`) today binds `{name, path}`, does `filepath.Abs`, `os.Stat` (must be a readable directory), then `paths.Add(name, abs)` with **no constraint on which directory**. On the default all-interfaces bind (`:8086`), anyone on the LAN can add e.g. `/Users/you/Documents` as a "library" and read its files through the comic API. This spec jails added paths to configured roots.

## Decisions (confirmed)

- **Config source:** a new `library.allowedRoots: []string`. When empty/unset it defaults to `[library.rootPath]`. Each entry is resolved to an absolute path.
- **Symlink resolution:** both the requested path and each root are passed through `filepath.EvalSymlinks` before comparison, so a symlink *inside* an allowed root cannot point *outside* it.
- **Reject behavior:** a path not inside any allowed root returns HTTP **400** with a generic message that does not echo the configured root paths.
- **Not retroactive:** only API adds are checked. The first-run YAML seed (`pathRepo.Add` in `main.go`) bypasses the API and is unaffected; existing DB library rows are not re-validated, so no current setup breaks.
- **Auth and binding unchanged:** no authentication is added; the default address stays `:8086` (Codex finding #1's binding/auth angle is intentionally out of scope — only the arbitrary-directory vector is closed).

## Components

### 1. Containment check (pure, heavily tested)
New file `internal/server/pathguard.go`:

```
// PathWithinRoots reports whether dir (absolute, existing) resolves inside one
// of roots. Symlinks in both are resolved so a link inside a root cannot escape.
func PathWithinRoots(dir string, roots []string) (bool, error)
```

Algorithm: `EvalSymlinks(dir)`; for each root, `EvalSymlinks(root)` (skip a root that fails to resolve — e.g. doesn't exist); `rel, err := filepath.Rel(resolvedRoot, resolvedDir)`; the path is inside iff `rel == "."` **or** (`rel != ".."` and `rel` does not start with `".." + separator`). This rejects `..` escapes and the `/foo` vs `/foobar` prefix trap that a naive `HasPrefix` would allow.

### 2. Config
`internal/config/config.go`: add `AllowedRoots []string` (`yaml:"allowedRoots"`) to `Library`. In `validate()` (after `RootPath` is checked): if `len(AllowedRoots) == 0`, set it to `[RootPath]`; resolve every entry to `filepath.Abs`.

### 3. Server wiring
`NewServer(...)` gains a trailing `allowedRoots []string` parameter, stored on `Server`. `cmd/tefnut/main.go` passes `cfg.Library.AllowedRoots`.

### 4. Handler gate
In `apiAddPath`, after the existing `os.Stat`/`IsDir` check and before `paths.Add`: call `PathWithinRoots(abs, s.allowedRoots)`. On `false`, return `fail(c, http.StatusBadRequest, errors.New("目录必须在允许的库根之内"))`. On a non-nil error (e.g. EvalSymlinks failure on a path that vanished), treat as reject (400, same message) — fail closed.

## Error Handling
- Reject is fail-closed: any error resolving the requested path → 400 reject, not a 500 and not an allow.
- The 400 message is a fixed string with no path interpolation (no root disclosure).
- A configured root that doesn't resolve is skipped (logged once at validation if desired), never causing a 500 on add.

## Testing
- **`pathguard_test.go`** (unit, the security core): inside-root → true; the root itself → true; a sibling like `/lib-other` against root `/lib` → false (prefix trap); `..` escape (`/lib/../etc`) → false; a symlink inside the root pointing outside → false (real temp dirs + `os.Symlink`); multiple roots where the second matches → true; empty roots → false.
- **`api_settings` integration test**: with allowedRoots `[tmpRoot]`, `POST /api/settings/paths` for a subdir of `tmpRoot` → 200; for a dir outside `tmpRoot` → 400 with the generic message; assert the body does not contain the root path.
- `go build ./... && go vet ./... && go test ./...` green; `gofmt` clean.

## Out of Scope
- Authentication / login (deliberately none — single-user).
- Changing the default bind address from `:8086` (LAN access is intended).
- Codex findings #2 (scan.interval), #3 (resource caps), #4 (browse pagination) — separate items.
- Retroactive validation of existing DB library rows.
