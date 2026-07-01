# Docker Deployment Design

**Goal:** Ship Tefnut as a container: a multi-stage `Dockerfile` producing a small Alpine image, a `docker-compose.yml` for one-command self-hosting, and a GitHub Actions workflow that builds **multi-arch** (linux/amd64 + linux/arm64) images and pushes them to **GHCR** (`ghcr.io/aki-liang/tefnut`). Users run `docker compose up -d` and browse at `:8086`.

**Architecture:** Purely additive infrastructure — **no Go code changes**. Tefnut is already a CGO-free, pure-Go binary with `go:embed`ded web assets, so a statically-linked binary (`CGO_ENABLED=0`) runs on Alpine with zero libc dependency. The container provides everything the runtime needs that the binary doesn't: `tzdata` (for the daily-scan cron's timezone), `ca-certificates`, a non-root user, and `wget` (busybox) for the healthcheck. Configuration ships as a baked-in default `config.yaml` pointing at fixed volume mount points (`/comics` read-only, `/data` persistent) — the Komga/Jellyfin convention — so no config editing is required.

**Tech Stack:** Docker multi-stage build (`golang:1.24-alpine` builder → `alpine:3.20` runtime), Docker Compose v2, GitHub Actions (`docker/setup-qemu-action`, `setup-buildx-action`, `login-action`, `metadata-action`, `build-push-action`).

## Global Constraints

- **No Go code changes.** This feature adds only Docker/CI/docs files. The Go build, tests, and `go.mod` are untouched.
- **CGO-free static binary.** Build with `CGO_ENABLED=0` so the binary is fully static and libc-independent (required for Alpine/musl and for multi-arch cross-compilation). Use `-trimpath -ldflags="-s -w"` to shrink it.
- **Pin the Go builder to the project's toolchain floor.** `go.mod` declares `go 1.24.0` + `toolchain go1.24.6`. The builder image must be ≥ 1.24.6; build with `GOTOOLCHAIN=local` so CI never auto-downloads a newer toolchain. Use `golang:1.24-alpine` (tracks the latest 1.24.x).
- **Non-root runtime.** The container runs as a non-root user; the `/data` volume must be writable by that user.
- **Fixed mount points:** `/comics` (library, read-only) and `/data` (sqlite DB + thumbs + cache, persistent). The baked config points `library.rootPath: /comics` and `dataDir: /data`.
- **Single-user, no auth** (unchanged project stance): the container exposes `:8086` on the host LAN; no authentication is added.
- **Image name:** `ghcr.io/aki-liang/tefnut` (lowercase, per GHCR/Docker rules). CI pushes with the repo's built-in `GITHUB_TOKEN` (`packages: write`), no extra secrets.

## Components

### 1. `Dockerfile` (multi-stage)

**Builder stage** (`golang:1.24-alpine AS build`):
- `ENV GOTOOLCHAIN=local CGO_ENABLED=0`.
- Copy `go.mod`/`go.sum`, `go mod download` (layer-cached), then copy the source.
- Cross-compile using buildx's `TARGETOS`/`TARGETARCH` build args: `GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/tefnut ./cmd/tefnut`.

**Runtime stage** (`alpine:3.20`):
- `RUN apk add --no-cache ca-certificates tzdata` (Alpine's BusyBox already provides `wget` for the healthcheck — no separate install).
- Create a non-root user/group (e.g. `addgroup -S tefnut && adduser -S -G tefnut tefnut`).
- `RUN mkdir -p /comics /data && chown -R tefnut:tefnut /data` (so the paths exist even if a volume isn't mounted, and `/data` is writable).
- `COPY --from=build /out/tefnut /usr/local/bin/tefnut` and `COPY deploy/config.yaml /etc/tefnut/config.yaml`.
- `EXPOSE 8086`.
- `HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 CMD wget -qO- http://127.0.0.1:8086/healthz || exit 1`.
- `USER tefnut`.
- `ENTRYPOINT ["tefnut", "-config", "/etc/tefnut/config.yaml"]`.

### 2. `deploy/config.yaml` (baked default)

```yaml
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
(`scan.interval` defaults to `2m` in code; the daily/interval schedule is set in the Settings UI and stored in the DB, so it is intentionally omitted here.) An operator who needs different values mounts their own file over `/etc/tefnut/config.yaml`.

### 3. `.dockerignore`

Keep the build context small and reproducible by excluding what the `go build` doesn't need: `.git`, `docs/`, `.superpowers/`, `.github/`, `README*`, any local runtime state (`data/`, root-level `config.yaml`, `*.db`), and editor/OS cruft (`.DS_Store`). The Go sources (including `*_test.go`) stay — `go build ./cmd/tefnut` ignores test files anyway, and excluding them buys nothing.

### 4. `docker-compose.yml`

```yaml
services:
  tefnut:
    image: ghcr.io/aki-liang/tefnut:latest
    # To build from source instead of pulling, comment `image:` above and uncomment:
    # build: .
    container_name: tefnut
    ports:
      - "8086:8086"
    volumes:
      - /path/to/your/comics:/comics:ro   # <-- edit: your library, read-only
      - tefnut-data:/data                  # persistent DB + thumbnails + cache
    environment:
      - TZ=Asia/Shanghai                   # daily-scan schedule uses this timezone
    restart: unless-stopped
volumes:
  tefnut-data:
```
The user edits the one host path. `/comics` is read-only (Tefnut never writes to the library). `/data` is a named volume so the DB/thumbnails/cache survive container recreation.

### 5. `.github/workflows/docker.yml`

- **Triggers:** `push` to `main`, `push` of tags matching `v*`, and `workflow_dispatch`.
- **Permissions:** `contents: read`, `packages: write`.
- **Steps:** checkout → `docker/setup-qemu-action` (for arm64 emulation) → `docker/setup-buildx-action` → `docker/login-action` to `ghcr.io` with `${{ github.actor }}` / `${{ secrets.GITHUB_TOKEN }}` → `docker/metadata-action` (images: `ghcr.io/aki-liang/tefnut`; tags: `type=raw,value=latest,enable={{is_default_branch}}`, `type=semver,pattern={{version}}`, `type=sha`) → `docker/build-push-action` with `platforms: linux/amd64,linux/arm64`, `push: true`, and GitHub Actions layer cache (`cache-from/to: type=gha`).

### 6. Docs — README "Docker 部署" section

Add a section: pull-and-run with compose (edit the comics path + TZ, `docker compose up -d`, open `http://<host>:8086`), what each volume/port/env means, the GHCR image name and supported architectures, and the "build from source" alternative. Note the first scan of many large PDFs extracts covers to `/data` (disk/time), consistent with the app's extract-to-cache behavior.

## Configuration & Data Flow

- Container starts → `tefnut -config /etc/tefnut/config.yaml` → config validates `rootPath: /comics` exists (the Dockerfile `mkdir`s it, so an unmounted `/comics` is an empty—but valid—library) and creates `/data` structure.
- The library is mounted read-only at `/comics`; scans read it, covers/thumbnails/cache/DB are written under `/data`.
- `TZ` env → Alpine `tzdata` → the daily-scan cron fires at the operator's local time.
- Healthcheck polls `/healthz`; Docker/compose report container health; `restart: unless-stopped` recovers from crashes.

## Error Handling

- **Missing library mount:** `/comics` exists (baked `mkdir`) but is empty → app starts with an empty library rather than failing; the operator sees no comics until they mount and scan. (Fail-soft, matches the app's "empty library is valid" behavior.)
- **`/data` not writable by the non-root user:** the app's `config.validate()` `MkdirAll`/DB open fails fast with a clear error in the container logs. The Dockerfile `chown`s `/data`; a bind-mounted host dir with wrong ownership is the operator's responsibility (documented).
- **Wrong-arch pull:** multi-arch manifest means `docker` selects the right image automatically; no action needed.
- **CI push failure** (e.g. permissions): the workflow fails loudly; images are only pushed on `main`/tags, not PRs.

## Testing

- **Structural (controller, no daemon needed):** `docker compose config` validates the compose file; the Dockerfile and workflow YAML are reviewed for correctness (stage names, build args, `TARGETARCH` usage, metadata tags, permissions).
- **Local image smoke (requires Docker daemon — run if the operator starts Docker Desktop):** `docker build -t tefnut:dev .` for the host arch; `docker run` it with a small sample comics dir bind-mounted at `/comics:ro` and a data volume at `/data`; verify `/healthz` returns 200, the browse page lists the sample, and a page image serves — the same smoke checks used for prior features, but through the container. Confirm it runs as non-root (`docker exec … id`) and that `TZ` is honored.
- **CI (real multi-arch build):** the workflow performs the actual `linux/amd64,linux/arm64` build+push on `main`; a successful run is the end-to-end validation that the Dockerfile cross-compiles and the image publishes to GHCR.
- Go `build/vet/test` are unaffected (no code change) but should stay green as a sanity check.

## Out of Scope

- Kubernetes/Helm manifests.
- Reverse proxy / TLS termination (operators front it with their own nginx/Caddy/Traefik).
- Authentication (project remains single-user, no-auth).
- Refactoring `config.go` for env-var overrides (baked config + optional file mount covers configuration; env is limited to `TZ`).
- Publishing to Docker Hub (GHCR only).
