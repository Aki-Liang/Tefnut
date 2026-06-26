# Tefnut 家用漫画服务 — 设计方案

日期：2026-06-26
状态：已与用户确认，待评审

## 1. 概述与目标

Tefnut 是一个自托管的家用漫画服务（类似 Plex 的漫画版）。后台常驻运行，用户通过浏览器访问页面阅读漫画。

核心目标：

- 指定一个目录作为**漫画库**，库内的漫画压缩包新增/删除后能自动反映到页面上。
- 漫画 = 图片打包成的压缩包；页面以**文件夹层级**浏览（库 → 子目录/作品 → 漫画）。
- 列表展示每本漫画的**文件名**与**封面**（压缩包内第一张图）。
- 在线阅读漫画（翻页、记住上次阅读进度）。
- 给漫画维护元数据：**作者**、**0–5 星评分**、**标签（tag）**。
- 列表支持按**名称**搜索、按**标签**筛选、按**最低评分**筛选。
- 提供独立的**标签管理**入口（列出全部 tag、重命名、删除、查看使用数量）。

部署形态：**一个 Go 二进制 + 一个 SQLite 数据库文件 + 一个数据目录**（存放缩略图等缓存）。无需外部数据库，开箱即跑。

## 2. 非目标（v1 不做）

- 用户登录 / 密码保护（家用、局域网内可信，任何访问者都可阅读与编辑元数据）。
- 多用户、按用户隔离的阅读进度（进度为**全局单份**）。
- 标签合并、标签层级、智能刮削（从外部站点抓取作者/封面）。
- fsnotify 实时监听（v1 用"启动时 + 定时"扫描；实时监听列为后续增强）。
- 移动端原生 App（仅浏览器页面，但页面应基本自适应）。

## 3. 技术栈与依赖

- 语言：Go **1.24**（`go.mod` 从 1.15 升级）。
- Web 框架：**echo v4**（保留现有依赖，复用路由/静态/中间件）。
- 数据库：**SQLite**，驱动用 `modernc.org/sqlite`（**纯 Go，无需 cgo/gcc**），配合标准库 `database/sql` + 手写 SQL。**移除 xorm 与 MySQL 驱动**。
- 压缩包：复用 `github.com/mholt/archiver/v4`，统一支持 **zip/cbz、rar/cbr、7z/cb7**，以 `fs.FS` 方式列举与按名读取条目。封装在自有 `archive` 包后面，使上层格式无关。
- 图片：标准库 `image/jpeg`、`image/png`、`image/gif` 解码 + `golang.org/x/image/webp` 解码 + `golang.org/x/image/draw` 缩放，用于生成缩略图。
- 定时任务：`github.com/robfig/cron/v3`（保留）。
- 配置：`gopkg.in/yaml.v3`。
- 前端：Go `html/template` 服务端渲染 + 原生 JS，全部通过 `go:embed` 打进二进制，**无前端构建工具链**。

## 4. 项目结构（多小文件、低耦合）

现有 DDD 代码整体重构。目标布局：

```
cmd/tefnut/main.go              装配与启动（DI、cron、echo 路由）
cmd/tefnut/config.yaml          默认配置

internal/config/                配置结构体、加载与校验
internal/store/                 SQLite：连接、建表(migrate)、各仓库
  store.go                        打开 DB、执行 schema
  node_repo.go                    nodes 增删查、过滤查询
  tag_repo.go                     tags / node_tags
  progress_repo.go                progress
internal/library/               领域层
  model.go                        Node / Comic / Tag 等模型与类型常量
  scanner.go                      遍历库目录、与 DB diff、触发封面/页数生成
internal/archive/               压缩包抽象
  archive.go                      接口：列举图片项、按名打开、取首图
  formats.go                      扩展名 → 格式判断、image 扩展名判断、__MACOSX 过滤
  natsort.go                      自然排序（2.jpg < 10.jpg）
internal/thumb/                  缩略图生成（解码 + 缩放 + 落盘）
internal/server/                HTTP 层
  router.go                       注册 echo 路由
  pages.go                        服务端渲染页面处理器
  api_nodes.go                    列表/详情/封面/取页 API
  api_meta.go                     作者/评分/标签编辑 API
  api_tags.go                     标签管理 API
  api_progress.go                 阅读进度 API
  response.go                     统一 JSON 响应封装
  web/                            go:embed 资源
    templates/*.html
    static/css/*.css
    static/js/*.js
```

每个文件聚焦单一职责，目标 200–400 行、不超过 800 行。

## 5. 配置（cmd/tefnut/config.yaml）

```yaml
library:
  rootPath: "./COMIC"        # 漫画库根目录
dataDir: "./data"            # SQLite 文件、缩略图、解压缓存所在目录
server:
  addr: ":8086"
scan:
  interval: "2m"             # 定时扫描间隔；启动时也会扫一次
thumbnail:
  width: 400                 # 缩略图宽度(px)，按比例缩放
```

加载时校验：`rootPath` 必须存在且可读；`dataDir` 不存在则创建；`interval` 能被解析为 `time.Duration`；`width` > 0。校验失败启动即报错退出（fail fast）。

数据目录内部结构：`dataDir/tefnut.db`、`dataDir/thumbs/{id}.jpg`、`dataDir/cache/{id}/`（rar/7z 首次访问的解压缓存）。

## 6. 数据模型（SQLite schema）

```sql
CREATE TABLE IF NOT EXISTS nodes (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  parent_id    INTEGER NOT NULL DEFAULT 0,      -- 0 表示库根
  name         TEXT    NOT NULL,
  path         TEXT    NOT NULL UNIQUE,          -- 绝对路径
  type         INTEGER NOT NULL,                 -- 1=comic 漫画压缩包, 2=directory 目录
  page_count   INTEGER NOT NULL DEFAULT 0,       -- 漫画页数（目录为 0）
  cover_status INTEGER NOT NULL DEFAULT 0,       -- 0=none, 1=ready, 2=failed
  author       TEXT    NOT NULL DEFAULT '',
  rating       INTEGER NOT NULL DEFAULT 0,       -- 0..5，0 表示未评分
  size         INTEGER NOT NULL DEFAULT 0,       -- 文件字节数（用于变更检测）
  mtime        INTEGER NOT NULL DEFAULT 0,       -- 文件修改时间(unix 秒)
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_nodes_parent ON nodes(parent_id);

CREATE TABLE IF NOT EXISTS tags (
  id   INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS node_tags (
  node_id INTEGER NOT NULL,
  tag_id  INTEGER NOT NULL,
  PRIMARY KEY (node_id, tag_id)
);
CREATE INDEX IF NOT EXISTS idx_node_tags_tag ON node_tags(tag_id);

CREATE TABLE IF NOT EXISTS progress (
  node_id    INTEGER PRIMARY KEY,
  last_page  INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL
);
```

说明：

- `nodes` 沿用自引用树承载文件夹层级；漫画与目录共表，用 `type` 区分。
- 作者/评分/标签为用户编辑的元数据，**扫描不会覆盖**它们（扫描只更新文件属性：name/path/size/mtime/page_count/cover_status）。
- 文件被删除时，对应 `nodes` 行连带其 `node_tags`、`progress`、缩略图一并清除。

## 7. 扫描流程（internal/library/scanner.go）

触发：进程启动时执行一次 + cron 每 `scan.interval` 执行一次。两次扫描串行（加锁，避免并发）。

递归遍历 `rootPath`，对每一层目录：

1. 读取磁盘当前层条目；目录 → 候选目录节点，`.zip/.cbz/.rar/.cbr/.7z/.cb7` → 候选漫画节点，其它文件忽略。
2. 读取 DB 中该 `parent_id` 下的已存在节点，按 `path` 建映射做 diff：
   - 磁盘有、DB 无 → 新增节点。漫画节点新增后异步/同步生成封面缩略图并统计页数。
   - 两者都有 → 保留；若漫画的 `size` 或 `mtime` 变化 → 重新生成封面、重新统计页数（用户元数据不动）。未变化则跳过。
   - DB 有、磁盘无 → 删除节点（目录递归删除其子树），并清理其缩略图、`node_tags`、`progress`。
3. 目录节点继续向下递归。

封面/页数生成失败（损坏包、无图片、解码失败）：`cover_status=failed`、`page_count` 记为成功列举到的图片数（可能为 0），记录日志但不中断整次扫描。

## 8. 压缩包抽象（internal/archive）

定义与格式无关的接口，屏蔽 zip/rar/7z 差异：

```go
// 列出包内所有图片条目（已过滤非图片与 __MACOSX，并按自然顺序排序）
func ListImages(ctx context.Context, archivePath string) ([]string, error)
// 打开指定条目用于读取（流式）
func OpenImage(ctx context.Context, archivePath, entry string) (io.ReadCloser, error)
```

- 底层用 `archiver.FileSystem(ctx, archivePath)` 得到 `fs.FS`，`fs.WalkDir` 列举、`fsys.Open(name)` 读取。
- **图片判定**：扩展名属于 `.jpg/.jpeg/.png/.gif/.webp/.bmp`（大小写不敏感）。
- **过滤**：路径含 `__MACOSX`、以 `.` 开头的隐藏项、目录项一律跳过。
- **自然排序**（natsort.go）：将文件名中的数字段按数值比较，保证 `2.jpg < 10.jpg`，封面取排序后第一项。
- **随机访问**：zip/cbz 走 central directory 可直接随机读；rar/cbr、7z/cb7 顺序解压，按需在 `dataDir/cache/{id}/` 首次访问时整包解压一次后从缓存目录读取（避免每次翻页重复解压）。
- **页列表缓存**：进程内维护一个小的内存缓存（key = `nodeID+mtime`）保存某漫画的有序图片名列表，减少取页时重复列举的开销。

## 9. 缩略图（internal/thumb）

- 输入：漫画压缩包首图的 `io.Reader`。
- 解码：按内容/扩展名选择 jpeg/png/gif/webp 解码器。
- 缩放：等比缩放到配置宽度（默认 400px），用 `golang.org/x/image/draw` 的 `CatmullRom`。
- 输出：编码为 JPEG（quality ~85）写入 `dataDir/thumbs/{id}.jpg`，置 `cover_status=ready`。
- 失败：置 `cover_status=failed`，前端展示占位图。

## 10. HTTP 路由（echo）

### 服务端渲染页面（html/template）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/` | 库根浏览页（等价 `/folder/0`） |
| GET | `/folder/:id` | 浏览某目录：列出子目录 + 漫画卡片（封面、名称、评分），含搜索/标签/评分过滤控件 |
| GET | `/read/:id` | 阅读器页（模板外壳 + JS） |
| GET | `/tags` | 标签管理页 |

搜索/过滤语义：当 `q`/`tag`/`minRating` 任一存在时，**跨整个库**检索漫画类节点（忽略当前目录）；都不存在时返回当前 `parent` 的直接子节点（目录 + 漫画）。

### JSON / 二进制 API（`/api`）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/nodes?parent=&q=&tag=&minRating=` | 列表，返回子目录与漫画（含封面 URL、页数、评分、进度），支持组合过滤 |
| GET | `/api/comics/:id` | 漫画详情：`{name, author, rating, pageCount, lastPage, tags[]}` |
| GET | `/api/comics/:id/cover` | 返回缩略图字节（`Content-Type: image/jpeg`），无则占位 |
| GET | `/api/comics/:id/pages/:n` | 返回第 n 页图片字节（按需取，n 从 0 起） |
| PATCH | `/api/comics/:id` | 更新 `{author?, rating?}` |
| POST | `/api/comics/:id/tags` | `{name}` 给漫画加标签（标签不存在则创建） |
| DELETE | `/api/comics/:id/tags/:tagId` | 移除漫画上的某标签 |
| PUT | `/api/comics/:id/progress` | `{page}` 保存阅读进度 |
| GET | `/api/tags` | 列出全部标签 `[{id, name, count}]` |
| POST | `/api/tags` | `{name}` 新建标签 |
| PATCH | `/api/tags/:id` | `{name}` 重命名标签 |
| DELETE | `/api/tags/:id` | 删除标签（连带清理 `node_tags` 关联） |

统一响应封装：`{code, message, data}`（`code=0` 成功）。图片/封面/取页接口直接返回二进制流，不套 JSON。

输入校验（系统边界）：`:id`、`:n`、`parent`、`tagId`、`minRating`、`page` 必须为合法整数且在合理范围；`rating` 限定 0–5；标签 `name` 去首尾空白且非空、限定最大长度。非法输入返回 4xx 与清晰错误消息，绝不信任外部数据。

## 11. 阅读器行为（原生 JS）

- 进入 `/read/:id` 时调用 `GET /api/comics/:id` 拿到 `pageCount` 与 `lastPage`，跳到 `lastPage`。
- 单页显示；`←/→` 键、点击页面左右半区、或屏幕按钮翻页。
- 预加载下一页（隐藏 `Image` 预取）以减少等待。
- 翻页时（去抖后）`PUT /api/comics/:id/progress` 上报当前页。
- 顶部/侧栏可编辑：作者、星级评分、标签（添加输入框 + 已有标签可移除）。

## 12. 标签管理（/tags 页）

- 列出所有标签及各自使用数量（`count`）。
- 重命名（`PATCH /api/tags/:id`，名称冲突报错）。
- 删除（`DELETE /api/tags/:id`，二次确认；连带移除所有关联）。
- 新建（`POST /api/tags`）。
- 给漫画打标签时输入新名称会自动建标签（`POST /api/comics/:id/tags` 内部 upsert）。

## 13. 错误处理与日志

- 所有外部边界（HTTP 入参、文件读取、压缩包解析、DB 操作）显式处理错误，不静默吞掉。
- 面向用户的 UI/接口返回友好错误消息；服务端用标准库 `log`（或 `slog`）记录带上下文的详细错误。
- 扫描/缩略图针对单个文件的失败不影响整体流程，仅记日志并标记状态。

## 14. 测试计划（覆盖率目标 ≥80%，核心包）

- `internal/archive`：自然排序、图片过滤、`__MACOSX` 过滤；用测试夹具（小 zip）验证 ListImages/OpenImage。
- `internal/thumb`：对内置小图做解码+缩放，断言输出尺寸与可解码。
- `internal/library`：扫描 diff 逻辑（新增/变更/删除）用临时目录 + 临时 SQLite 驱动。
- `internal/store`：node/tag/progress 仓库的增删查与过滤查询，用内存/临时 SQLite。
- `internal/server`：关键 handler 集成测试（列表过滤、取页、改评分、打/删标签、进度）。
- 遵循 TDD：先写测试（RED）→ 实现（GREEN）→ 重构。

## 15. 实现顺序（里程碑）

1. **骨架**：升级 go.mod 到 1.24；接入 modernc sqlite + store/migrate；config 加载与校验；删除 MySQL/xorm 相关代码。
2. **archive 包**：格式抽象、自然排序、图片过滤、单测。
3. **scanner + thumb**：扫描 diff、封面与页数生成、单测。
4. **API + 页面**：echo 路由、列表/详情/封面/取页、服务端渲染浏览页、阅读器。
5. **元数据**：作者/评分/标签编辑 API + 标签管理页 + 过滤查询。
6. **阅读进度**：进度 API + 阅读器联动。
7. **打磨**：错误处理、占位图、样式自适应、补齐测试覆盖。

每个里程碑可独立编译运行与测试。
