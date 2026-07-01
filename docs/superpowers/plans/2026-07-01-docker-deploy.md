# Docker Deployment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Package Tefnut as a small Alpine container with a `docker-compose.yml` for one-command self-hosting and a GitHub Actions workflow that builds multi-arch images and pushes them to GHCR.

**Architecture:** Purely additive infrastructure — **no Go code changes**. Tefnut is already a CGO-free, pure-Go binary with embedded web assets, so a `CGO_ENABLED=0` static binary runs on Alpine. A baked default `config.yaml` points at fixed mount points (`/comics` read-only, `/data` persistent). CI cross-compiles per arch and publishes to `ghcr.io/aki-liang/tefnut`.

**Tech Stack:** Docker multi-stage build (`golang:1.24-alpine` → `alpine:3.20`), Docker Compose v2, GitHub Actions (buildx, GHCR). Spec: `docs/superpowers/specs/2026-07-01-docker-deploy-design.md`.

## Global Constraints

- **No Go code changes.** Only Docker/CI/docs files are added. `go.mod`, Go sources, and tests are untouched. Never run `go get` or edit `.go` files.
- **CGO-free static build:** `CGO_ENABLED=0`, `-trimpath -ldflags="-s -w"`.
- **Builder ≥ the toolchain floor:** `go.mod` has `go 1.24.0` + `toolchain go1.24.6`. Build with `GOTOOLCHAIN=local` on `golang:1.24-alpine` (which tracks the latest 1.24.x, ≥ 1.24.6). If a build ever errors that the toolchain is too old, pin `golang:1.24.6-alpine`.
- **Non-root runtime**; `/data` must be writable by that user.
- **Fixed mounts:** `/comics` (library, read-only), `/data` (DB + thumbs + cache).
- **Image name:** `ghcr.io/aki-liang/tefnut` (lowercase). CI uses the built-in `GITHUB_TOKEN` (`packages: write`) — no extra secrets.
- **Multi-arch:** `linux/amd64,linux/arm64`.
- **Environment note:** the implementer has NO Docker daemon. Verify each task with daemon-free checks (compile the build target, parse YAML, `docker compose config`). The real `docker build`/run smoke is done by the controller (with Docker Desktop up) and by CI on push — do not block a task on a daemon.

---

### Task 1: Container image — Dockerfile + baked config + .dockerignore

**Files:**
- Create: `deploy/config.yaml`
- Create: `Dockerfile`
- Create: `.dockerignore`

**Interfaces:**
- Consumes: the existing `./cmd/tefnut` main (reads `-config <path>`; config schema `library.rootPath`, `dataDir`, `server.addr`, `thumbnail.width`/`pageWidth`, `cache.maxBytes`), the `/healthz` endpoint, port `:8086`.
- Produces: an image whose `ENTRYPOINT` is `tefnut -config /etc/tefnut/config.yaml`, mounts `/comics` + `/data`, exposes 8086. Later tasks (compose, CI, docs) reference this image and its mount points.

- [ ] **Step 1: Create the baked default config**

Create `deploy/config.yaml`:
```yaml
# Default in-container configuration.
# Mount your library read-only at /comics and a persistent volume at /data.
# To change these, mount your own file over /etc/tefnut/config.yaml.
library:
  rootPath: /comics
dataDir: /data
server:
  addr: ":8086"
thumbnail:
  width: 400
  pageWidth: 120
cache:
  maxBytes: 2147483648  # 2 GiB
```

- [ ] **Step 2: Verify the config parses and matches the schema**

Run:
```bash
python3 -c "import yaml; d=yaml.safe_load(open('deploy/config.yaml')); assert d['library']['rootPath']=='/comics'; assert d['dataDir']=='/data'; assert d['server']['addr']==':8086'; assert d['cache']['maxBytes']==2147483648; print('config ok')"
```
Expected: `config ok`.

- [ ] **Step 3: Create the Dockerfile**

Create `Dockerfile`:
```dockerfile
# syntax=docker/dockerfile:1

# ---- build: static, CGO-free, cross-compiled to the target arch ----
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build
ENV GOTOOLCHAIN=local CGO_ENABLED=0
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/tefnut ./cmd/tefnut

# ---- runtime ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -S tefnut && adduser -S -G tefnut tefnut \
 && mkdir -p /comics /data && chown -R tefnut:tefnut /data
COPY --from=build /out/tefnut /usr/local/bin/tefnut
COPY deploy/config.yaml /etc/tefnut/config.yaml
EXPOSE 8086
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8086/healthz || exit 1
USER tefnut
ENTRYPOINT ["tefnut", "-config", "/etc/tefnut/config.yaml"]
```

Key points (do not change without reason):
- `--platform=$BUILDPLATFORM` makes the Go build run on the native runner arch and **cross-compile** to `$TARGETOS/$TARGETARCH` (fast; no QEMU for compilation). The runtime `alpine` stage is pulled per target arch; its `apk add` runs under QEMU emulation for the foreign arch (handled by CI's `setup-qemu-action`).
- BusyBox `wget` (built into Alpine) powers the healthcheck — no extra package.
- `mkdir /comics /data` so the paths exist even when a volume isn't mounted (an unmounted `/comics` is an empty but valid library); `chown /data` so the non-root user can write the DB/cache.

- [ ] **Step 4: Create the .dockerignore**

Create `.dockerignore`:
```
.git
.github
.gitignore
docs
.superpowers
README.md
*.md
data
config.yaml
*.db
*.db-shm
*.db-wal
.DS_Store
```
This keeps `go.mod`/`go.sum`, `cmd/`, `internal/`, and `deploy/config.yaml` (all needed by the build) while dropping VCS, docs, scratch, and local runtime state. **Do not** add `deploy` or any source dir here.

- [ ] **Step 5: Verify the build-stage target compiles (daemon-free)**

The Dockerfile's build command is `go build ... ./cmd/tefnut`. Prove that exact target compiles statically:
```bash
GOTOOLCHAIN=local CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tmp/tefnut-static ./cmd/tefnut && echo "static build ok" && rm -f /tmp/tefnut-static
```
Expected: `static build ok`. (This validates the Dockerfile's compile step without a Docker daemon. A `.dockerignore` sanity check: `grep -qxF deploy .dockerignore && echo "BUG: deploy excluded" || echo "deploy kept ok"` → `deploy kept ok`.)

- [ ] **Step 6: Commit**

```bash
git add Dockerfile .dockerignore deploy/config.yaml
git commit -m "feat: containerize tefnut (multi-stage alpine image + baked config)"
```

---

### Task 2: docker-compose.yml + README section

**Files:**
- Create: `docker-compose.yml`
- Modify: `README.md` (append a "Docker 部署" section; if `README.md` does not exist, create it with just that section)

**Interfaces:**
- Consumes: the image from Task 1 (`ghcr.io/aki-liang/tefnut`), its mounts (`/comics`, `/data`), port 8086, and `TZ`.
- Produces: a compose service `tefnut` and user-facing run instructions.

- [ ] **Step 1: Create docker-compose.yml**

Create `docker-compose.yml`:
```yaml
services:
  tefnut:
    image: ghcr.io/aki-liang/tefnut:latest
    # To build from source instead of pulling the published image,
    # comment out `image:` above and uncomment the next line:
    # build: .
    container_name: tefnut
    ports:
      - "8086:8086"
    volumes:
      - /path/to/your/comics:/comics:ro   # EDIT: your comic library (read-only)
      - tefnut-data:/data                  # persistent DB, thumbnails, cache
    environment:
      - TZ=Asia/Shanghai                   # timezone for the daily-scan schedule
    restart: unless-stopped

volumes:
  tefnut-data:
```

- [ ] **Step 2: Validate the compose file**

Run (client-side, no daemon needed):
```bash
docker compose -f docker-compose.yml config >/dev/null && echo "compose ok"
```
Expected: `compose ok`. If `docker compose config` errors because it cannot reach a daemon, fall back to a YAML/structure check:
```bash
python3 -c "import yaml; c=yaml.safe_load(open('docker-compose.yml')); s=c['services']['tefnut']; assert s['image']=='ghcr.io/aki-liang/tefnut:latest'; assert '8086:8086' in s['ports']; assert any(v.endswith('/comics:ro') for v in s['volumes']); assert 'tefnut-data:/data' in s['volumes']; assert c['volumes']['tefnut-data'] is None or isinstance(c['volumes']['tefnut-data'],(dict,type(None))); print('compose structure ok')"
```
Expected: `compose ok` (or `compose structure ok`).

- [ ] **Step 3: Append the README "Docker 部署" section**

Append to `README.md`:
```markdown

## Docker 部署

镜像发布在 GHCR：`ghcr.io/aki-liang/tefnut`（支持 `linux/amd64` 和 `linux/arm64`）。

1. 编辑 `docker-compose.yml`，把 `/path/to/your/comics` 改成你的漫画库目录，按需改 `TZ`。
2. 启动：

   ```bash
   docker compose up -d
   ```

3. 浏览器打开 `http://<主机IP>:8086`。

**卷与端口：**

- `/comics`（只读）— 你的漫画库；Tefnut 从不写入。
- `/data`（命名卷 `tefnut-data`）— SQLite 数据库、缩略图、页面缓存，容器重建后仍保留。
- `8086` — Web 端口。
- `TZ` — 每日定时扫描按此时区触发（如 `Asia/Shanghai`）。

**从源码构建**（不拉镜像）：注释 `docker-compose.yml` 里的 `image:`，取消注释 `build: .`，再 `docker compose up -d --build`。

> 首次扫描包含大量大 PDF 时，会把每本的页面抽取到 `/data` 缓存（占磁盘、需要时间），这是抽取式缓存的预期行为。
```
(If `README.md` does not exist, create it with a top-level `# Tefnut` heading followed by this section.)

- [ ] **Step 4: Verify the README edit**

Run:
```bash
grep -q "ghcr.io/aki-liang/tefnut" README.md && grep -q "docker compose up -d" README.md && echo "readme ok"
```
Expected: `readme ok`.

- [ ] **Step 5: Commit**

```bash
git add docker-compose.yml README.md
git commit -m "docs: docker-compose + README deployment section"
```

---

### Task 3: GitHub Actions — multi-arch build & push to GHCR

**Files:**
- Create: `.github/workflows/docker.yml`

**Interfaces:**
- Consumes: the `Dockerfile` and build context from Task 1.
- Produces: the CI pipeline that publishes `ghcr.io/aki-liang/tefnut` on `main` and version tags.

- [ ] **Step 1: Create the workflow**

Create `.github/workflows/docker.yml`:
```yaml
name: docker

on:
  push:
    branches: [main]
    tags: ['v*']
  workflow_dispatch:

permissions:
  contents: read
  packages: write

jobs:
  build-push:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Set up QEMU
        uses: docker/setup-qemu-action@v3

      - name: Set up Buildx
        uses: docker/setup-buildx-action@v3

      - name: Log in to GHCR
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Docker metadata
        id: meta
        uses: docker/metadata-action@v5
        with:
          images: ghcr.io/${{ github.repository }}
          tags: |
            type=raw,value=latest,enable={{is_default_branch}}
            type=semver,pattern={{version}}
            type=sha

      - name: Build and push
        uses: docker/build-push-action@v6
        with:
          context: .
          platforms: linux/amd64,linux/arm64
          push: true
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          cache-from: type=gha
          cache-to: type=gha,mode=max
```

Notes:
- `images: ghcr.io/${{ github.repository }}` resolves to `ghcr.io/Aki-Liang/Tefnut`; `docker/metadata-action` lowercases it to `ghcr.io/aki-liang/tefnut` automatically (container names must be lowercase).
- `${{ secrets.GITHUB_TOKEN }}` is auto-provisioned; combined with `permissions: packages: write` it can push to the repo's GHCR namespace — no user-configured secret.
- Only `main`, `v*` tags, and manual dispatch trigger it (no `pull_request`), so `push: true` never fires on untrusted PRs.

- [ ] **Step 2: Validate the workflow YAML**

Run:
```bash
python3 -c "import yaml; w=yaml.safe_load(open('.github/workflows/docker.yml')); assert w['permissions']['packages']=='write'; j=w['jobs']['build-push']['steps']; assert any(s.get('uses','').startswith('docker/build-push-action') for s in j); bp=[s for s in j if s.get('uses','').startswith('docker/build-push-action')][0]; assert bp['with']['platforms']=='linux/amd64,linux/arm64'; assert bp['with']['push'] is True; print('workflow ok')"
```
Expected: `workflow ok`. (If `actionlint` is installed, also run `actionlint .github/workflows/docker.yml` and expect no errors; if not installed, the YAML/structure check above suffices.)

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/docker.yml
git commit -m "ci: build and push multi-arch image to GHCR"
```

---

## Post-plan verification (controller-run, after Task 3)

Daemon-dependent smoke that the implementer can't do (no daemon in its env):

1. **If Docker Desktop is up:** `docker build -t tefnut:dev .` (host arch) → succeeds. Then run it against a small sample library:
   ```bash
   docker run -d --name tefnut-smoke -p 8086:8086 \
     -v <sample-comics>:/comics:ro -v tefnut-smoke-data:/data -e TZ=Asia/Shanghai tefnut:dev
   ```
   Verify: `/healthz` returns 200, the browse page lists the sample, a page image serves (CDP screenshot as with prior features), the container runs as non-root (`docker exec tefnut-smoke id` → uid≠0), and `docker inspect` shows `Health: healthy`. Tear down (`docker rm -f`, `docker volume rm`).
2. **CI (real multi-arch):** after the branch merges/pushes, the `docker` workflow builds `linux/amd64,linux/arm64` and pushes to GHCR — a green run is the end-to-end proof that the Dockerfile cross-compiles and publishes. (Note: GHCR package visibility defaults to private/inheriting the repo; the first push may need the package made public in repo settings if anonymous pulls are wanted — a one-time GitHub UI step, out of scope for code.)

## Notes for the executor

- **No Go code, no `go get`, no `.go` edits.** If a task tempts you to change application code, stop — this feature is infra only.
- **`.dockerignore` must not exclude `deploy/`, `cmd/`, `internal/`, `go.mod`, or `go.sum`** — the build needs them.
- The image name is lowercase `ghcr.io/aki-liang/tefnut` everywhere it's hardcoded (compose, README); the CI workflow derives it via `github.repository` + metadata-action lowercasing.
