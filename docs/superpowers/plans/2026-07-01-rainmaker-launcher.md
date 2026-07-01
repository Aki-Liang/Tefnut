# `rainmaker` Launcher Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `start.sh` with `rainmaker`, a self-contained launcher that generates its own `docker-compose.yml` and starts Tefnut from the public GHCR image via a single `curl … | bash` command.

**Architecture:** One executable `rainmaker` at the repo root (rewritten from `start.sh`). It reads prompts from `/dev/tty` (works under `curl | bash`), writes `./docker-compose.yml` with the library path/port/TZ baked in, and runs `docker compose up -d` (auto-pulls the public image — no separate hard-failing pull). The committed `docker-compose.yml` and `.env.example` are removed; the README's one-click section is rewritten.

**Tech Stack:** Bash, Docker Compose v2, public image `ghcr.io/aki-liang/tefnut:latest`. Spec: `docs/superpowers/specs/2026-07-01-rainmaker-launcher-design.md`.

## Global Constraints

- **Self-contained:** `rainmaker` generates `./docker-compose.yml` itself (values baked in); it does not read a committed compose or `.env`.
- **One-command:** primary path `curl -fsSL https://raw.githubusercontent.com/Aki-Liang/Tefnut/main/rainmaker | bash`; prompts read from `/dev/tty`; non-interactive form passes the library path as `$1` (`… | bash -s -- /path`).
- **Robust launch:** no separate `docker compose pull`; `docker compose up -d` auto-pulls; on failure, an actionable message (stale `docker login ghcr.io` → `docker logout ghcr.io`).
- **Runs in the user's CWD** (no `cd` to the script's dir); generated file lands in the current directory.
- **Public image, pull not build**; fixed mounts `<lib>:/comics:ro` + `tefnut-data:/data`; port default 8086; TZ detected, fallback `Asia/Shanghai`.
- **No Go / Dockerfile / CI / deploy-config / 1h-default changes.**
- **Environment note:** no Docker daemon in the implementer's env. Verify daemon-free with `bash -n`, `shellcheck` (if present), and a stubbed-`docker` generation test (below). The real pull+run smoke is a controller step.

---

### Task 1: `rainmaker` script + remove superseded files + `.gitignore`

**Files:**
- Create: `rainmaker` (executable)
- Delete: `start.sh`, `docker-compose.yml`, `.env.example`
- Modify: `.gitignore`

**Interfaces:**
- Produces: the `rainmaker` launcher (repo root) that the README (Task 2) documents.

- [ ] **Step 1: Create `rainmaker`**

Create `rainmaker` with exactly this content:
```bash
#!/usr/bin/env bash
# rainmaker — one-command Tefnut launcher.
# Generates ./docker-compose.yml in the current directory and starts Tefnut
# from the published GHCR image.
#   curl -fsSL https://raw.githubusercontent.com/Aki-Liang/Tefnut/main/rainmaker | bash
#   curl -fsSL https://raw.githubusercontent.com/Aki-Liang/Tefnut/main/rainmaker | bash -s -- /path/to/your/comics
set -euo pipefail

IMAGE="ghcr.io/aki-liang/tefnut:latest"
COMPOSE_FILE="docker-compose.yml"
DEF_PORT="8086"

err()  { printf '\033[31m%s\033[0m\n' "$*" >&2; }
info() { printf '\033[36m%s\033[0m\n' "$*"; }

# --- preflight ---
command -v docker >/dev/null 2>&1 || { err "未找到 docker。请先安装 Docker：https://docs.docker.com/get-docker/"; exit 1; }
docker compose version >/dev/null 2>&1 || { err "未找到 docker compose (v2)。请升级 Docker 到包含 Compose V2 的版本。"; exit 1; }

# --- prompts read from /dev/tty so 'curl | bash' still works interactively ---
TTY=""; [ -r /dev/tty ] && TTY=/dev/tty
ask() { # $1 prompt  $2 default -> answer on stdout
  local ans=""
  if [ -n "$TTY" ]; then printf '%s' "$1" >"$TTY"; IFS= read -r ans <"$TTY" || true; fi
  [ -z "$ans" ] && ans="$2"
  printf '%s' "$ans"
}
expand_tilde() { case "$1" in "~"*) printf '%s' "${HOME}${1#\~}";; *) printf '%s' "$1";; esac; }

# --- comic library path: arg $1, else prompt, else error ---
COMICS_PATH="$(expand_tilde "${1:-}")"
if [ -n "$COMICS_PATH" ]; then
  [ -d "$COMICS_PATH" ] || { err "目录不存在: $COMICS_PATH"; exit 1; }
elif [ -n "$TTY" ]; then
  while :; do
    COMICS_PATH="$(expand_tilde "$(ask '漫画库目录绝对路径: ' '')")"
    [ -z "$COMICS_PATH" ] && { err "库路径必填。"; continue; }
    [ -d "$COMICS_PATH" ] || { err "目录不存在: $COMICS_PATH"; continue; }
    break
  done
else
  err "没有可交互终端，请把漫画库目录作为参数传入："
  err "  curl -fsSL .../rainmaker | bash -s -- /你的漫画目录"
  exit 1
fi

# --- port + timezone ---
detect_tz() {
  local tz=""
  [ -L /etc/localtime ] && tz="$(readlink /etc/localtime | sed 's#.*/zoneinfo/##')"
  [ -n "$tz" ] && printf '%s' "$tz" || printf 'Asia/Shanghai'
}
while :; do
  PORT="$(ask "Web 端口 [$DEF_PORT]: " "$DEF_PORT")"
  if printf '%s' "$PORT" | grep -qE '^[0-9]+$' && [ "$PORT" -ge 1 ] && [ "$PORT" -le 65535 ]; then break; fi
  err "端口需为 1-65535 的数字。"
  [ -z "$TTY" ] && exit 1
done
DTZ="$(detect_tz)"
TZ_VAL="$(ask "时区 [$DTZ]: " "$DTZ")"

# --- generate docker-compose.yml (values baked in) ---
cat >"$COMPOSE_FILE" <<YAML
services:
  tefnut:
    image: $IMAGE
    container_name: tefnut
    ports:
      - "$PORT:8086"
    volumes:
      - $COMICS_PATH:/comics:ro
      - tefnut-data:/data
    environment:
      - TZ=$TZ_VAL
    restart: unless-stopped
volumes:
  tefnut-data:
YAML
info "已生成 $(pwd)/$COMPOSE_FILE"

# --- launch (up -d auto-pulls the public image; no separate hard-failing pull) ---
info "启动 Tefnut…"
if ! docker compose up -d; then
  err "启动失败。若是拉取镜像报错：确认能访问 ghcr.io；若你曾 'docker login ghcr.io' 且凭证已过期，执行 'docker logout ghcr.io' 后重试。"
  exit 1
fi

# --- report ---
if command -v curl >/dev/null 2>&1; then
  for _ in $(seq 1 30); do curl -fsS -o /dev/null "http://127.0.0.1:$PORT/healthz" 2>/dev/null && break; sleep 1; done
fi
docker compose ps
info "打开: http://localhost:$PORT"
info "（首次启动会在后台扫描漫画库并生成封面，大库可能需要几分钟。）"
```

- [ ] **Step 2: Make it executable**

Run: `chmod +x rainmaker`

- [ ] **Step 3: Remove the superseded files**

Run:
```bash
git rm start.sh docker-compose.yml .env.example
```

- [ ] **Step 4: Rewrite `.gitignore`**

Replace the entire contents of `.gitignore` with:
```gitignore
# Generated by ./rainmaker in a deployment directory
/docker-compose.yml
/.env
```

- [ ] **Step 5: Syntax + lint checks**

Run: `bash -n rainmaker && echo "syntax ok"`
Expected: `syntax ok`.
If `shellcheck` is installed: `shellcheck rainmaker` — expect no errors (skip if not installed).

- [ ] **Step 6: Stubbed-docker generation test (daemon-free)**

Verify `rainmaker` generates a correct compose and calls `up -d`, using a fake `docker` + `curl` on PATH (no real daemon, and fast — the fake `curl` makes the health-poll exit immediately):
```bash
set -e
REPO="$PWD"   # run this block from the repo root; rainmaker lives here
T=$(mktemp -d); BIN="$T/bin"; RUN="$T/run"; SAMPLE="$T/comics"
mkdir -p "$BIN" "$RUN" "$SAMPLE"
cat >"$BIN/docker" <<'EOF'
#!/usr/bin/env bash
[ "$1 $2" = "compose up" ] && echo "up $*" >>"$FAKE_LOG"
exit 0
EOF
cat >"$BIN/curl" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$BIN/docker" "$BIN/curl"
export FAKE_LOG="$T/log"
( cd "$RUN" && PATH="$BIN:$PATH" bash "$REPO/rainmaker" "$SAMPLE" >/dev/null 2>&1 ) || { echo "rainmaker exited non-zero"; exit 1; }
# assertions
grep -q "image: ghcr.io/aki-liang/tefnut:latest" "$RUN/docker-compose.yml" \
  && grep -q "\"8086:8086\"" "$RUN/docker-compose.yml" \
  && grep -q "$SAMPLE:/comics:ro" "$RUN/docker-compose.yml" \
  && grep -q "tefnut-data:/data" "$RUN/docker-compose.yml" \
  && grep -q "up -d" "$FAKE_LOG" \
  && echo "generation ok"
rm -rf "$T"
```
Expected: `generation ok`.

- [ ] **Step 7: Confirm the removals**

Run: `ls start.sh docker-compose.yml .env.example 2>&1 | grep -c 'No such file' | grep -qx 3 && echo "removed ok"`
Expected: `removed ok` (all three gone).

- [ ] **Step 8: Commit**

```bash
git add rainmaker .gitignore
git commit -m "feat: self-contained rainmaker launcher (generates compose, pulls GHCR image)"
```
(The `git rm` from Step 3 is already staged and will be included in this commit.)

---

### Task 2: README — rewrite the Docker section

**Files:**
- Modify: `README.md` (replace the whole `## Docker 部署` section)

**Interfaces:**
- Consumes: the `rainmaker` launcher (Task 1) and its `curl … | bash` invocation.

- [ ] **Step 1: Replace the `## Docker 部署` section**

In `README.md`, replace everything from the line `## Docker 部署` to the end of the file with:
```markdown
## Docker 部署

### 一键启动（推荐）

在一个空目录里执行 —— 它会在当前目录生成 `docker-compose.yml` 并启动：

```bash
curl -fsSL https://raw.githubusercontent.com/Aki-Liang/Tefnut/main/rainmaker | bash
```

按提示填漫画库路径、端口、时区即可。也可非交互，直接把库路径作为参数传入：

```bash
curl -fsSL https://raw.githubusercontent.com/Aki-Liang/Tefnut/main/rainmaker | bash -s -- ~/comics
```

镜像发布在 GHCR：`ghcr.io/aki-liang/tefnut`（公开可拉，支持 `linux/amd64` 和 `linux/arm64`）。启动后浏览器打开 `http://<主机IP>:8086`。

> 默认定时扫描间隔为 **1 小时**（可在「设置」页调整）。

**卷与端口：**

- `/comics`（只读）— 你的漫画库；Tefnut 从不写入。
- `/data`（命名卷 `tefnut-data`）— SQLite 数据库、缩略图、页面缓存，容器重建后仍保留；容器以非-root 用户运行，命名卷权限自动就绪。
- `8086` — Web 端口（在生成的 `docker-compose.yml` 里可改端口映射）。
- `TZ` — 定时扫描按此时区触发（如 `Asia/Shanghai`）。

**从源码构建**（不拉镜像）：`git clone` 后本地构建并打上镜像 tag，`rainmaker` 便会直接用本地镜像：

```bash
docker build -t ghcr.io/aki-liang/tefnut:latest .
./rainmaker ~/comics
```

> 首次扫描包含大量大 PDF 时，会把每本的页面抽取到 `/data` 缓存（占磁盘、需要时间），这是抽取式缓存的预期行为。
```

- [ ] **Step 2: Verify**

Run:
```bash
grep -q 'curl -fsSL https://raw.githubusercontent.com/Aki-Liang/Tefnut/main/rainmaker | bash' README.md \
  && ! grep -q 'start.sh' README.md \
  && ! grep -q '\.env\.example' README.md \
  && echo "readme ok"
```
Expected: `readme ok` (new one-command present; no stale `start.sh` / `.env.example` references).

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: README one-command rainmaker launch"
```

---

## Post-plan verification (controller-run, after Task 2)

Real one-command smoke (Docker daemon; the GHCR image is published + public):
1. In a temp dir, `bash <repo>/rainmaker <sample-comics-dir>` (non-interactive path arg) → it generates `docker-compose.yml` and `docker compose up -d` pulls the **real public GHCR image**.
2. Verify `/healthz` 200, the browse page lists the sample, a page serves; container is non-root; `docker inspect` health becomes healthy.
3. Tear down: `docker compose down -v` in the temp dir, remove the temp dir. (Do not leave a `.env`/`docker-compose.yml` in the repo.)

## Notes for the executor

- **No `.env` anywhere** now — `rainmaker` bakes values into the generated compose.
- **Do not leave generated `docker-compose.yml`/`.env` in the repo** — Task 1's `.gitignore` ignores them, but if a test creates them at the repo root, delete them before committing.
- The `git rm` (Task 1 Step 3) stages the deletions; they land in the Task 1 commit alongside `rainmaker` + `.gitignore`.
