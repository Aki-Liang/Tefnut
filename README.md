# Tefnut

A self-hosted family comic server (Plex-for-comics). Point it at a directory of
comic archives (`.zip/.cbz`, `.rar/.cbr`, `.7z/.cb7`) and read them in your
browser. Single Go binary + a SQLite file; no external database.

## Quick start

1. Edit `cmd/tefnut/config.yaml` (or copy it next to the binary):
   - `library.rootPath` — your comic library directory
   - `dataDir` — where the DB, thumbnails, and extract cache live
   - `server.addr` — listen address (default `:8086`)
   - `scan.interval` — rescan period (default `2m`)
   - `thumbnail.width` — cover width in px (default `400`)
2. Run:
   ```bash
   go run ./cmd/tefnut -config ./cmd/tefnut/config.yaml
   ```
3. Open http://localhost:8086

Drop new comic archives into the library directory; they appear after the next
scan (and immediately on restart).

## Features
- Folder-based browsing of the library tree
- Auto-generated cover thumbnails (first page of each archive)
- In-browser reader with keyboard paging and remembered progress
- Per-comic author, 0–5★ rating, and free-text tags
- Search by name; filter by tag and minimum rating
- Tag management page (rename / delete / counts)

## Build
```bash
go build -o tefnut ./cmd/tefnut
```

## Docker 部署

### 一键启动（推荐）

```bash
./start.sh
```

脚本会引导你填写漫画库路径、端口、时区，写入 `.env`，然后 `docker compose up -d` 启动，并打印访问地址。再次运行会以现有 `.env` 为默认值重新配置。

手动方式：复制 `.env.example` 为 `.env` 并编辑，再 `docker compose up -d`。

> 默认定时扫描间隔为 **1 小时**（可在「设置」页调整）。

镜像发布在 GHCR：`ghcr.io/aki-liang/tefnut`（支持 `linux/amd64` 和 `linux/arm64`）。

启动后，浏览器打开 `http://<主机IP>:8086`（端口默认 8086，可在 `.env` 里用 `TEFNUT_PORT` 修改）。

**卷与端口：**

- `/comics`（只读）— 你的漫画库；Tefnut 从不写入。
- `/data`（命名卷 `tefnut-data`）— SQLite 数据库、缩略图、页面缓存，容器重建后仍保留。容器以非-root 用户运行；用命名卷时权限自动就绪。若改用主机目录绑定挂载（如 `./data:/data`），需先把该目录的属主设成容器用户（否则数据库无法写入）。
- `8086` — Web 端口。
- `TZ` — 每日定时扫描按此时区触发（如 `Asia/Shanghai`）。

**从源码构建**（不拉镜像）：注释 `docker-compose.yml` 里的 `image:`，取消注释 `build: .`，再 `docker compose up -d --build`。

> 首次扫描包含大量大 PDF 时，会把每本的页面抽取到 `/data` 缓存（占磁盘、需要时间），这是抽取式缓存的预期行为。
