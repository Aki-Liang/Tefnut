# Tefnut

自托管的家庭漫画服务器（漫画版 Plex）。把它指向一个装漫画的目录 —— 支持 `.zip/.cbz`、`.rar/.cbr`、`.7z/.cb7` 压缩包，以及 `.pdf`、`.epub`、`.mobi` —— 就能在浏览器里阅读。单个 Go 二进制 + 一个 SQLite 文件，无需外部数据库。

## 快速开始

1. 编辑 `cmd/tefnut/config.yaml`（或复制一份放到二进制旁边）：
   - `library.rootPath` — 你的漫画库目录
   - `dataDir` — 数据库、缩略图、解压缓存的存放位置
   - `server.addr` — 监听地址（默认 `:8086`）
   - `scan.interval` — 重新扫描周期（默认 `1h`）
   - `thumbnail.width` — 封面宽度，像素（默认 `400`）
2. 运行：
   ```bash
   go run ./cmd/tefnut -config ./cmd/tefnut/config.yaml
   ```
3. 打开 http://localhost:8086

把新漫画放进库目录，下次扫描后就会出现（重启时立即生效）。

## 功能特性
- 按文件夹浏览整个漫画库树
- 自动生成封面缩略图（每本漫画的第一页）
- 浏览器内阅读器：键盘翻页、记忆阅读进度
- 每本漫画可设作者、0–5★ 评分、自由文本标签
- 按名称搜索；按标签和最低评分筛选
- 标签管理页（重命名 / 删除 / 计数）

## 构建
```bash
go build -o tefnut ./cmd/tefnut
```

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
