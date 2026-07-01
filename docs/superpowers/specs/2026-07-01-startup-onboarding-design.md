# One-Click Startup Script + Scan-Interval Default Design

**Goal:** An interactive `start.sh` that guides a self-hoster through the minimum configuration (library path, port, timezone), writes a `.env`, and launches the app with `docker compose up -d` (pulling the published GHCR image), then prints the URL. Plus: change the default scheduled-scan interval from 2 minutes to 1 hour.

**Architecture:** Two small, independent changes on top of the merged Docker deployment. (1) A Bash `start.sh` + a `.env.example` + parameterizing the existing `docker-compose.yml` with `.env` variables (`${COMICS_PATH}`, `${TEFNUT_PORT}`, `${TZ}`), so the script and manual users both drive compose through one `.env`. (2) A one-line default change in the settings repository (the effective source of the scan schedule when the DB has no stored value). No changes to the application's runtime code paths beyond the default constant.

**Tech Stack:** Bash (macOS + Linux), Docker Compose v2, and the existing Go `internal/store/settings_repo.go`.

## Global Constraints

- **The script targets Docker Compose only** (user decision). It does not build/run the native binary.
- **Image source = pull the published GHCR image** (`docker compose up -d`, no `--build`). The image is `ghcr.io/aki-liang/tefnut:latest`, published by CI after the Docker PR merged. If the pull fails (image not yet built/published, or a private package), the script prints a clear, actionable message (and points at the `# build: .` fallback already in the compose file).
- **Fixed mounts stay** as the Docker feature defined them: `/comics` (read-only library) and `/data` (named volume `tefnut-data`).
- **Minimal prompts:** library path (required, validated), web port (default 8086), timezone (default detected from the host, fallback `Asia/Shanghai`). Data uses the named volume — not prompted.
- **Idempotent & re-runnable:** if a `.env` already exists, its values become the prompt defaults (press Enter to keep). Running `start.sh` again reconfigures and restarts cleanly.
- **`.env` is git-ignored** (it holds a user-specific path); `.env.example` is committed as the template.
- **No breaking change to running instances:** the interval default only affects a *fresh* install (empty DB); an existing instance already has a stored `scan_interval` value and is unaffected.
- **Compose must still be usable without the script:** a user who copies `.env.example`→`.env` and edits it, or who sets the vars in their shell, can run `docker compose up -d` directly.

## Components

### 1. `start.sh` (repo root, executable)

Bash script, `set -euo pipefail`. Flow:
1. **Preflight:** verify `docker` and `docker compose` (v2) are available; if not, print an install pointer and exit non-zero.
2. **Load existing `.env`** (if present) to seed defaults.
3. **Prompt** (each shows its current/default value; Enter keeps it):
   - **Comic library path** — required; expand a leading `~`; validate it is an existing directory, else re-prompt.
   - **Web port** — default `8086`; validate it's a number in `1..65535`.
   - **Timezone** — default detected via `readlink /etc/localtime` (strip to the `Region/City` after `zoneinfo/`), fallback `Asia/Shanghai`.
4. **Write `.env`** with `COMICS_PATH`, `TEFNUT_PORT`, `TZ`.
5. **Launch:** `docker compose pull` then `docker compose up -d`. On `pull` failure, print the actionable message and stop (don't silently `--build`).
6. **Report:** poll `http://127.0.0.1:<port>/healthz` for up to ~30s; print `docker compose ps` and the URL `http://localhost:<port>` (note it may take a few minutes on first run while the initial library scan completes).

### 2. `.env.example` (committed template)

```dotenv
# Copy to .env (or run ./start.sh) and edit.
# Absolute path to your comic library (mounted read-only at /comics).
COMICS_PATH=/path/to/your/comics
# Host port to publish the web UI on.
TEFNUT_PORT=8086
# Timezone for the scheduled-scan clock.
TZ=Asia/Shanghai
```
`.env` is added to `.gitignore`.

### 3. `docker-compose.yml` (parameterize the merged file)

Change three lines to read from `.env`, keeping sensible defaults so port/TZ are optional but the library path is required:
- `ports: "8086:8086"` → `ports: "${TEFNUT_PORT:-8086}:8086"`
- `volumes: /path/to/your/comics:/comics:ro` → `${COMICS_PATH:?COMICS_PATH is required — run ./start.sh or set it in .env}:/comics:ro`
- `environment: TZ=Asia/Shanghai` → `TZ=${TZ:-Asia/Shanghai}`

The `${COMICS_PATH:?…}` form makes `docker compose` fail with a clear message if the library path is unset, so a user who skips the script still gets guidance. `image:`/`container_name`/`restart`/the `# build: .` comment/the named volume are unchanged.

### 4. Scan-interval default → 1 hour

- `internal/store/settings_repo.go`: `defScanInterval = "2m"` → `"1h"`. This is the effective default: `GetScan` returns it when the DB has no `scan_interval` key, and the scan manager turns it into `@every 1h`.
- `internal/config/config.go`: the `defaults()` `Scan.Interval` `"2m"` → `"1h"` for documentation consistency (this field is validated but not consumed by the scan manager — noting so, not adding new wiring).

### 5. README — "一键启动" section

Add a short section above/within the existing Docker section: `./start.sh` walks you through it; or copy `.env.example`→`.env`, edit, `docker compose up -d`. Note the default scan interval is now 1 hour (changeable in Settings).

## Data Flow

- `start.sh` → `.env` → `docker compose` variable substitution → container gets the host comics path bound read-only at `/comics`, the chosen port published, and `TZ` set.
- On a fresh install the container's empty DB makes `GetScan` return the `1h`/`interval` default → the scan manager schedules `@every 1h`. The user can still change mode/interval in the Settings UI (stored in the DB, overriding the default thereafter).

## Error Handling

- **Docker/compose missing** → explicit message + install link, non-zero exit.
- **Library path invalid/nonexistent** → re-prompt (loop) rather than proceed.
- **Port not a valid number** → re-prompt.
- **`docker compose pull` fails** (image absent/private) → message: confirm the image is published (CI ran on `main`) and the GHCR package is accessible, or `docker login ghcr.io`; mention the `# build: .` fallback. Exit non-zero.
- **Empty/whitespace inputs** → treated as "keep default" where a default exists; rejected where required (library path).

## Testing

- **Script static checks:** `bash -n start.sh` (syntax); `shellcheck start.sh` if available.
- **Compose substitution:** with a temp `.env` (a real sample dir as `COMICS_PATH`), `docker compose config` renders the mounts/port/TZ correctly; `${COMICS_PATH:?}` errors when `.env` omits it.
- **Interval default:** a Go unit test asserts `SettingsRepo.GetScan` on a fresh DB returns `Interval == "1h"` (mode `interval`). `go build/vet/test ./...` + `gofmt` stay green.
- **Full one-click smoke (controller, Docker daemon):** since the GHCR image may not be published yet, build the image locally and tag it `ghcr.io/aki-liang/tefnut:latest` to stand in for the pull; run `./start.sh` against a small sample library (feeding the prompts), verify `/healthz` 200 + the browse page lists the sample + a page serves; then tear down (container, volume, the local tag, `.env`).

## Out of Scope

- Non-interactive/flag or CI mode for `start.sh`; Windows `.bat`/PowerShell.
- An uninstall/reset script.
- Native-binary launch path (Docker-only, per decision).
- Reverse proxy / TLS / auth.
- Refactoring the vestigial `config.Scan.Interval` out (only its default value is updated here).
