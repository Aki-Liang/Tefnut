# One-Click Startup + 1h Scan-Interval Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An interactive `start.sh` that configures + launches Tefnut via Docker Compose (pulling the GHCR image), plus change the default scheduled-scan interval from 2 minutes to 1 hour.

**Architecture:** Two independent changes on the merged Docker deployment. (1) A one-line Go default change (settings repo) + doc-consistency change (config). (2) Parameterize the existing `docker-compose.yml` with `.env` variables, add `.env.example` + `.gitignore`, and add an interactive `start.sh`. Minimal, additive; only the scan-default constant touches runtime code.

**Tech Stack:** Bash, Docker Compose v2, Go (`internal/store/settings_repo.go`). Spec: `docs/superpowers/specs/2026-07-01-startup-onboarding-design.md`.

## Global Constraints

- **Docker Compose only** (no native-launch path in the script).
- **Image = pull GHCR** (`docker compose pull` + `up -d`, not `--build`). Image `ghcr.io/aki-liang/tefnut:latest`. On pull failure, print an actionable message and exit non-zero (point at the `# build: .` fallback in the compose file).
- **Fixed mounts:** `/comics` (read-only), `/data` (named volume `tefnut-data`).
- **Prompts:** library path (required, must be an existing dir; expand leading `~`), web port (default `8086`, numeric 1-65535), timezone (default detected from `/etc/localtime`, fallback `Asia/Shanghai`).
- **`.env` git-ignored; `.env.example` committed.** `.env.example` must NOT be ignored (the `.env` gitignore pattern doesn't match `.env.example`).
- **No breaking change to running instances:** the interval default only affects a fresh DB.
- **Compose usable without the script:** `${COMICS_PATH:?…}` errors clearly if unset; port/TZ default via `${VAR:-default}`.
- **Environment note:** the implementer may or may not have a Docker daemon. `docker compose config` is client-side (works without a daemon). Verify daemon-free (`docker compose config`, `bash -n`); the real pull+run smoke is a controller step.

---

### Task 1: Default scan interval → 1 hour

**Files:**
- Modify: `internal/store/settings_repo.go` (the `defScanInterval` constant)
- Modify: `internal/config/config.go:48` (the `defaults()` Scan interval, for doc consistency)
- Modify: `internal/store/settings_repo_test.go` (`TestGetScanDefaults` expectation)

**Interfaces:**
- Consumes: `SettingsRepo.GetScan(ctx) (ScanSettings, error)`, `ScanSettings{Mode, Interval, DailyTime}`, the `openTemp(t)` test helper (in `internal/store/store_test.go`).
- Produces: nothing new; only the default value changes.

- [ ] **Step 1: Update the defaults test to expect 1h (this is the failing test)**

In `internal/store/settings_repo_test.go`, in `TestGetScanDefaults`, change the assertion from `"2m"` to `"1h"`:
```go
	if s.Mode != "interval" || s.Interval != "1h" || s.DailyTime != "03:00" {
		t.Fatalf("defaults wrong: %+v", s)
	}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/store/ -run TestGetScanDefaults`
Expected: FAIL — `defaults wrong: {Mode:interval Interval:2m DailyTime:03:00}` (code still defaults to `2m`).

- [ ] **Step 3: Change the effective default in the settings repo**

In `internal/store/settings_repo.go`, change:
```go
	defScanInterval = "2m"
```
to:
```go
	defScanInterval = "1h"
```

- [ ] **Step 4: Change the config default for doc consistency**

In `internal/config/config.go` line ~48, change:
```go
		Scan:      Scan{Interval: "2m"},
```
to:
```go
		Scan:      Scan{Interval: "1h"},
```
(This field is validated but not consumed by the scan manager — the settings-repo default in Step 3 is the effective one. Updating it keeps the documented default consistent.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/store/ ./internal/config/ && go build ./... && go vet ./... && gofmt -l internal`
Expected: PASS; `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/store/settings_repo.go internal/store/settings_repo_test.go internal/config/config.go
git commit -m "feat: default scheduled-scan interval to 1h"
```

---

### Task 2: Parameterize compose + .env.example + .gitignore

**Files:**
- Modify: `docker-compose.yml` (3 lines)
- Create: `.env.example`
- Create: `.gitignore`

**Interfaces:**
- Consumes: the merged `docker-compose.yml` (service `tefnut`, image `ghcr.io/aki-liang/tefnut:latest`, `/comics`+`/data`, port 8086, TZ).
- Produces: the `.env` variable contract (`COMICS_PATH`, `TEFNUT_PORT`, `TZ`) that `start.sh` (Task 3) writes.

- [ ] **Step 1: Parameterize the three compose lines**

In `docker-compose.yml`, change the ports line:
```yaml
      - "8086:8086"
```
to:
```yaml
      - "${TEFNUT_PORT:-8086}:8086"
```
Change the comics volume line:
```yaml
      - /path/to/your/comics:/comics:ro   # EDIT: your comic library (read-only)
```
to:
```yaml
      - ${COMICS_PATH:?COMICS_PATH is required - run ./start.sh or set it in .env}:/comics:ro
```
Change the TZ env line:
```yaml
      - TZ=Asia/Shanghai                   # timezone for the daily-scan schedule
```
to:
```yaml
      - TZ=${TZ:-Asia/Shanghai}
```
Leave `image:`, the `# build: .` comment, `container_name`, `restart`, the `tefnut-data:/data` volume, and the top-level `volumes:` unchanged.

- [ ] **Step 2: Create `.env.example`**

Create `.env.example`:
```dotenv
# Copy to .env (or run ./start.sh) and edit these values.
# Absolute path to your comic library (mounted read-only at /comics).
COMICS_PATH=/path/to/your/comics
# Host port to publish the web UI on.
TEFNUT_PORT=8086
# Timezone for the scheduled-scan clock (e.g. Asia/Shanghai, UTC).
TZ=Asia/Shanghai
```

- [ ] **Step 3: Create `.gitignore`**

Create `.gitignore` (the repo currently has none):
```gitignore
# Local runtime configuration (holds a user-specific library path)
.env
```

- [ ] **Step 4: Verify substitution + the required-var guard (daemon-free)**

Run (passes vars via the environment, so no `.env` file is written into the repo). Note `docker compose config` normalizes ports/volumes to long form, so grep the three values *loosely*, not as exact `a:b` strings:
```bash
out=$(COMICS_PATH=/tmp TEFNUT_PORT=9090 TZ=UTC docker compose config 2>&1) \
  && echo "$out" | grep -q 9090 && echo "$out" | grep -q '/tmp' && echo "$out" | grep -q 'UTC' \
  && echo "substitution ok"
```
Expected: `substitution ok` (config renders with COMICS_PATH→/tmp, port→9090, TZ→UTC).

Then the required-var guard:
```bash
env -u COMICS_PATH docker compose config >/dev/null 2>err.txt; grep -q COMICS_PATH err.txt && echo "guard ok"; rm -f err.txt
```
Expected: `guard ok` (compose errors mentioning `COMICS_PATH` when it's unset).

Confirm `.env.example` is not ignored: `git check-ignore .env.example || echo ".env.example tracked ok"` → prints `.env.example tracked ok`. And `git check-ignore .env && echo ".env ignored ok"` → prints `.env ignored ok`.

- [ ] **Step 5: Commit**

```bash
git add docker-compose.yml .env.example .gitignore
git commit -m "feat: drive docker-compose from .env (COMICS_PATH/TEFNUT_PORT/TZ)"
```

---

### Task 3: `start.sh` + README section

**Files:**
- Create: `start.sh` (executable)
- Modify: `README.md` (add a "一键启动" subsection)

**Interfaces:**
- Consumes: the `.env` contract from Task 2 (`COMICS_PATH`, `TEFNUT_PORT`, `TZ`) and `docker-compose.yml`.
- Produces: the user-facing entrypoint `./start.sh`.

- [ ] **Step 1: Write `start.sh`**

Create `start.sh`:
```bash
#!/usr/bin/env bash
# Tefnut one-click setup: configure + launch via Docker Compose.
set -euo pipefail
cd "$(dirname "$0")"

ENV_FILE=".env"
DEF_PORT="8086"

err()  { printf '\033[31m%s\033[0m\n' "$*" >&2; }
info() { printf '\033[36m%s\033[0m\n' "$*"; }

# --- preflight ---
if ! command -v docker >/dev/null 2>&1; then
  err "未找到 docker。请先安装 Docker：https://docs.docker.com/get-docker/"
  exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
  err "未找到 docker compose (v2)。请升级 Docker 到包含 Compose V2 的版本。"
  exit 1
fi

# --- read existing .env values as defaults (grep/cut, not sourcing — safe for spaces) ---
envval() { [ -f "$ENV_FILE" ] && grep -E "^$1=" "$ENV_FILE" | head -1 | cut -d= -f2- || true; }
CUR_COMICS="$(envval COMICS_PATH)"
CUR_PORT="$(envval TEFNUT_PORT)"; CUR_PORT="${CUR_PORT:-$DEF_PORT}"
CUR_TZ="$(envval TZ)"

detect_tz() {
  local tz=""
  if [ -L /etc/localtime ]; then tz="$(readlink /etc/localtime | sed 's#.*/zoneinfo/##')"; fi
  [ -n "$tz" ] && printf '%s' "$tz" || printf 'Asia/Shanghai'
}
DEF_TZ="${CUR_TZ:-$(detect_tz)}"

# --- prompt: comic library path (required, must exist) ---
while :; do
  if [ -n "$CUR_COMICS" ]; then printf '漫画库目录绝对路径 [%s]: ' "$CUR_COMICS"; else printf '漫画库目录绝对路径: '; fi
  read -r COMICS_PATH || true
  [ -z "$COMICS_PATH" ] && COMICS_PATH="$CUR_COMICS"
  case "$COMICS_PATH" in "~"*) COMICS_PATH="${HOME}${COMICS_PATH#\~}";; esac
  if [ -z "$COMICS_PATH" ]; then err "库路径必填。"; continue; fi
  if [ ! -d "$COMICS_PATH" ]; then err "目录不存在: $COMICS_PATH"; continue; fi
  break
done

# --- prompt: port ---
while :; do
  printf 'Web 端口 [%s]: ' "$CUR_PORT"
  read -r TEFNUT_PORT || true
  [ -z "$TEFNUT_PORT" ] && TEFNUT_PORT="$CUR_PORT"
  if printf '%s' "$TEFNUT_PORT" | grep -qE '^[0-9]+$' && [ "$TEFNUT_PORT" -ge 1 ] && [ "$TEFNUT_PORT" -le 65535 ]; then break; fi
  err "端口需为 1-65535 的数字。"
done

# --- prompt: timezone ---
printf '时区 [%s]: ' "$DEF_TZ"
read -r TZ_IN || true
TZ_VAL="${TZ_IN:-$DEF_TZ}"

# --- write .env ---
{
  printf 'COMICS_PATH=%s\n' "$COMICS_PATH"
  printf 'TEFNUT_PORT=%s\n' "$TEFNUT_PORT"
  printf 'TZ=%s\n' "$TZ_VAL"
} > "$ENV_FILE"
info "已写入 $ENV_FILE"

# --- launch ---
info "拉取镜像…"
if ! docker compose pull; then
  err "拉取镜像失败。请确认镜像已由 CI 发布到 GHCR、且 GHCR 包可访问（或先 'docker login ghcr.io'）。"
  err "也可从源码构建: 在 docker-compose.yml 注释 image、启用 'build: .'，再 'docker compose up -d --build'。"
  exit 1
fi
info "启动容器…"
docker compose up -d

# --- report ---
info "等待服务就绪…"
if command -v curl >/dev/null 2>&1; then
  for _ in $(seq 1 30); do
    curl -fsS -o /dev/null "http://127.0.0.1:${TEFNUT_PORT}/healthz" 2>/dev/null && break
    sleep 1
  done
fi
docker compose ps
info "打开: http://localhost:${TEFNUT_PORT}"
info "（首次启动会在后台扫描漫画库并生成封面，大库可能需要几分钟。）"
```

- [ ] **Step 2: Make it executable**

Run: `chmod +x start.sh`

- [ ] **Step 3: Verify syntax (and lint if available)**

Run: `bash -n start.sh && echo "syntax ok"`
Expected: `syntax ok`.
If `shellcheck` is installed: `shellcheck start.sh` — expect no errors (if not installed, skip; the syntax check suffices).

- [ ] **Step 4: Add the README "一键启动" subsection**

Insert into `README.md`, immediately under the `## Docker 部署` heading (before the existing numbered steps), this subsection:
```markdown
### 一键启动（推荐）

```bash
./start.sh
```

脚本会引导你填写漫画库路径、端口、时区，写入 `.env`，然后 `docker compose up -d` 启动，并打印访问地址。再次运行会以现有 `.env` 为默认值重新配置。

手动方式：复制 `.env.example` 为 `.env` 并编辑，再 `docker compose up -d`。

> 默认定时扫描间隔为 **1 小时**（可在「设置」页调整）。

```
(Keep the existing Docker section content below this subsection.)

- [ ] **Step 5: Verify the README edit**

Run: `grep -q "一键启动" README.md && grep -q "./start.sh" README.md && echo "readme ok"`
Expected: `readme ok`.

- [ ] **Step 6: Commit**

```bash
git add start.sh README.md
git commit -m "feat: interactive start.sh one-click launcher + README"
```

---

## Post-plan verification (controller-run, after Task 3)

Daemon-dependent full smoke (the GHCR image may not be published yet):

1. Build the image locally and tag it as the pull target so `docker compose up -d` finds it without pulling:
   `docker build -t ghcr.io/aki-liang/tefnut:latest .`
2. Run `./start.sh`, feeding the prompts (a small sample comics dir for `COMICS_PATH`, a spare port, a TZ). Confirm it writes `.env`, `docker compose up -d` starts the container, `/healthz` returns 200, the browse page lists the sample, and a page serves. Verify a re-run of `./start.sh` picks up the existing `.env` values as defaults.
3. Tear down: `docker compose down -v`, remove the local `ghcr.io/aki-liang/tefnut:latest` tag, delete the generated `.env` and the sample dir.

## Notes for the executor

- **`.env` must never be committed** — Task 2 adds it to `.gitignore`; if a smoke test creates one, delete it before committing.
- The scan-interval change (Task 1) is the only runtime-code change; Tasks 2-3 are infra/docs.
- Read `.env` in `start.sh` via `grep`/`cut`, not by sourcing, so a library path containing spaces can't break the script.
