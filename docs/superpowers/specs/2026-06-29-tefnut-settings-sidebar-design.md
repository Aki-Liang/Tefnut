# Tefnut 增强：侧边栏 + 多库设置 + 扫描模式 + 阅读器按钮 — 设计方案

日期：2026-06-29
状态：已与用户确认，待评审
基于：`feature/comic-server`（v1 已完成，PR #2）

## 1. 概述与目标

在 v1（单库、固定定时扫描、顶栏导航、键盘翻页）基础上做四项增强：

1. **侧边栏导航** —— 左侧栏含「图书馆 / 标签管理 / 设置」，移除顶栏的标签管理链接。
2. **多库路径** —— 可在设置页配置多个漫画库路径，每个路径在首页呈现为一个顶层入口。
3. **可配置扫描模式** —— 设置页三选一：定时间隔 / 每日定时 / 监控路径（fsnotify）。
4. **阅读器翻页按钮** —— 在键盘 ←/→ 之外增加可见的翻页按钮。

核心架构变化：**设置以 SQLite 为准、保存即生效**。`config.yaml` 退化为只管启动项；新增 **ScanManager** 在运行时按设置重配调度，无需重启。

## 2. 已确认的设计决策

- 设置存 SQLite，**保存即生效**（运行时 `Reconfigure()` 重建调度并立即重扫）。
- 每个库路径 = 首页一个**顶层文件夹节点**。
- 库入口用**自定义名称**（添加时填，默认路径末段，可改）—— `library_paths` 表带 `name` 列。
- 扫描方式**同时只能选一种**；启动时**总是先全扫一次**，再按所选模式运行。
- **阅读器全屏、不带侧栏**（它有自己的阅读栏）。
- 监控模式**不加**保底定时重扫（纯 fsnotify + 启动全扫）。

## 3. 非目标（本次不做）

- 每个库独立的扫描策略（扫描模式是全局一份）。
- 监控模式的事件持久化 / 崩溃恢复补扫。
- 库路径的鉴权/权限管理（家用、信任局域网，沿用 v1）。
- 设置变更历史/审计。

## 4. 数据模型（新增表）

```sql
CREATE TABLE IF NOT EXISTS library_paths (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  name       TEXT    NOT NULL,          -- 显示名（默认路径末段，可改）
  path       TEXT    NOT NULL UNIQUE,   -- 绝对路径
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
```

`settings` 键与默认值：

| key | 取值 | 默认 |
|-----|------|------|
| `scan_mode` | `interval` \| `daily` \| `watch` | `interval` |
| `scan_interval` | Go duration（分钟/小时），如 `30m`、`2h` | `2m` |
| `scan_daily_time` | `HH:MM`（24 小时） | `03:00` |

**首次启动播种（向后兼容旧 yaml）：** 若 `library_paths` 为空且 yaml `library.rootPath` 非空，插入一行 `{name: basename(rootPath), path: rootPath}`；若 `settings` 为空，插入 `scan_mode=interval`、`scan_interval=`(yaml `scan.interval` 或 `2m`)、`scan_daily_time=03:00`。

## 5. config.yaml 变化

保留：`server.addr`、`dataDir`、`thumbnail.width`。`library.rootPath`、`scan.interval` 变为**仅首次启动的播种值**；之后以 DB 为准。旧配置文件无需改动即可平滑升级。

## 6. 多根扫描器（`internal/library/scanner.go` 改造）

`Scanner` 改为依赖 `store.LibraryPathRepo`（构造时注入）。`Scan(ctx)`：

1. `paths := libraryPathRepo.List()`。
2. `roots := nodeRepo.ListChildren(0)`，按 `path` 建映射。
3. 对每个配置的库路径：
   - upsert 一个**顶层目录节点**（`parent_id=0`、`type=directory`、`path=abs(库路径)`、`name=库的自定义名`）。若节点已存在但名称与配置不一致，则更新 `name`（库被改名时同步）。
   - `scanDir(库路径, 该节点ID)`（复用现有逐层 diff 逻辑扫描其内容）。
4. 映射中剩余的旧根节点（对应已删除的库路径）→ `removeNode`（删子树 + 缩略图 + 缓存）。

顶层节点的 `name` 来自库配置而非目录名；更深层级仍用文件系统名（现有逻辑不变）。

## 7. ScanManager（新增 `internal/scan/manager.go`）

运行时可重配的扫描调度器，持有 `*library.Scanner`、`*store.SettingsRepo`、`*store.LibraryPathRepo`。

```go
type Manager struct { /* scanner, repos, mu, active mode cancel */ }
func New(sc *library.Scanner, settings *store.SettingsRepo, paths *store.LibraryPathRepo) *Manager
func (m *Manager) Start(ctx context.Context) error  // 阻塞式首次全扫 → 启动当前模式
func (m *Manager) Reconfigure() error               // 停旧 → 读设置 → 启新 → 异步重扫一次
func (m *Manager) Stop()
```

模式实现（同一时刻仅一种活跃，由 `scan_mode` 决定）：

- **interval**：`cron.New()` + `AddFunc("@every " + scan_interval)` + `Start()`。
- **daily**：解析 `scan_daily_time` 为时分 → `cron.New()` + `AddFunc("M H * * *")` + `Start()`。
- **watch**：`fsnotify.NewWatcher()`，对每个库路径 `filepath.WalkDir` 给所有子目录 `Add`；监听协程：收到事件后**去抖**（每次事件重置一个 ~3s 定时器，静默后触发一次 `scanner.Scan()`）；遇到新建目录事件时把新目录也 `Add` 进监听。

生命周期：`Manager` 内部为当前模式持有一个 `context.CancelFunc` + 该模式的资源句柄（cron 实例 / watcher）。`Reconfigure()`/`Stop()` 先 cancel 旧模式、关闭其资源，再按需启动新模式。`Reconfigure` 在重建后触发一次异步 `Scan`（库路径可能已变）。所有触发最终都走 `scanner.Scan()`，其内部互斥保证不重入。`Manager` 自身用一把锁保护 `Reconfigure/Stop` 的并发。

错误处理：模式启动失败（如非法 cron 串、watcher 创建失败）→ 返回错误并记录日志，保持无活跃调度而非崩溃；单次扫描失败仅记日志（沿用 scanner 行为）。

## 8. 设置 API（新增 `internal/server/api_settings.go`）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/settings` | `{libraryPaths:[{id,name,path}], scanMode, scanInterval, scanDailyTime}` |
| PUT | `/api/settings` | body `{scanMode, scanInterval, scanDailyTime}`；校验后持久化 → `manager.Reconfigure()` |
| POST | `/api/settings/paths` | body `{name?, path}`；校验目录存在/可读；`name` 空则取末段；插入 → `Reconfigure()` |
| DELETE | `/api/settings/paths/:id` | 删除库路径行 → `Reconfigure()`（重扫并清理孤立子树） |

校验（系统边界）：`scanMode` ∈ 三值之一；`scanInterval` 能被 `time.ParseDuration` 解析且 > 0；`scanDailyTime` 匹配 `HH:MM` 且时 0–23、分 0–59；`path` 必须是存在且可读的目录、绝对路径、`library_paths` 内唯一（重复返回 409）；`name` 去空白、限长。非法 → 4xx + 清晰消息。`Server` 结构新增对 `*scan.Manager`、`*store.SettingsRepo`、`*store.LibraryPathRepo` 的引用。

## 9. 前端：侧边栏 + 设置页 + 阅读器按钮

**侧边栏（`layout.html` + CSS）**：左侧固定栏 + 右主区。侧栏链接：图书馆 `/`、标签管理 `/tags`、设置 `/settings`；移除顶栏标签链接。当前页高亮用一小段内联脚本按 `location.pathname` 给匹配链接加 `.active`（无需给各页数据结构加字段）。侧栏作为 `{{block "sidebar" .}}…{{end}}` 定义在 layout 中。

**阅读器全屏（`reader.html`）**：用 `{{define "sidebar"}}{{end}}` 把侧栏块置空，阅读器占满；其余阅读栏不变。

**设置页（`settings.html` + `settings.js`）**：
- 库路径区：列表（名称 + 路径 + 删除按钮）+ 添加表单（路径输入 + 名称输入，名称默认随路径末段填充）。
- 扫描方式区：单选 interval/daily/watch，按所选显示对应输入（间隔值+单位 / 每日时间 / 无）。保存调用 `PUT /api/settings`。
- 所有改动后刷新列表；失败 `alert` 提示（沿用 v1 前端错误处理风格）。

**阅读器翻页按钮（`reader.html` + CSS + `reader.js`）**：把现有两侧 `.nav.prev/.next` 点击区做成**可见的半透明箭头按钮**；并在底部阅读栏加「上一页 / 下一页」文字按钮。复用 `reader.js` 现有 `show(cur±1)` 逻辑，新按钮绑定同一处理。

## 10. main.go 装配变化

构建新增的 `settingsRepo`、`libraryPathRepo` → 首次启动播种 → 构建多根 `Scanner`（注入 pathRepo）→ 构建 `scan.Manager` → `manager.Start(ctx)`（阻塞首扫 + 启动模式）→ 构建 `Server`（注入 manager + 两个新仓库）→ echo 启动。原先 main 里的 cron 直接调用移入 ScanManager。`defer manager.Stop()`。

## 11. 模块结构（新增/改动）

```
internal/store/library_path_repo.go   新增  LibraryPathRepo: List/Add/Rename/Delete/Get
internal/store/settings_repo.go       新增  SettingsRepo: Get/Set/GetAll（含默认值）
internal/store/store.go               改    新增两表 schema
internal/scan/manager.go              新增  ScanManager（调度 + fsnotify + 去抖）
internal/library/scanner.go           改    多根扫描
internal/server/api_settings.go       新增  设置 API
internal/server/server.go             改    路由 + Server 字段（manager/repos）
internal/server/pages.go              改    pageSettings
internal/server/web/templates/layout.html   改   侧边栏
internal/server/web/templates/reader.html    改   置空侧栏 + 翻页按钮
internal/server/web/templates/settings.html  新增
internal/server/web/static/css/app.css        改   侧栏 + 设置页 + 按钮样式
internal/server/web/static/js/settings.js     新增
internal/server/web/static/js/reader.js       改   绑定可见翻页按钮
cmd/tefnut/main.go                    改    装配 ScanManager
```

## 12. 错误处理与日志

沿用 v1：边界校验、错误带上下文包装、不静默吞错；扫描/监听的单点失败记日志并继续。设置写入失败回滚并返回 5xx。

## 13. 测试计划（核心包 ≥80%）

- `store.LibraryPathRepo` / `SettingsRepo`：CRUD、唯一约束、默认值回退（临时 SQLite）。
- 多根 `Scanner`：多路径 → 多顶层节点；删路径 → 子树清除；改名 → 顶层节点名更新；空配置 → 无根节点。
- `scan.Manager`：模式解析 + `Reconfigure` 会触发扫描（用计数型假 scanner 注入，断言被调用）；cron 串构造（interval/daily）正确。fsnotify 去抖触发逻辑用可注入事件的方式单测；纯时序部分以集成/手测为主并在报告中标注。
- 设置 API handler：get/put/add/delete、各校验分支、Reconfigure 被调用（用 stub manager）。
- 前端侧栏/设置页/阅读器按钮：模板渲染测试 + 手测。
- TDD：先写测试（RED）→ 实现（GREEN）→ 重构。

## 14. 实现顺序（里程碑）

1. **存储层**：两表 schema + `LibraryPathRepo` + `SettingsRepo` + 单测。
2. **多根扫描**：改造 `Scanner` 读库路径列表 + 单测。
3. **ScanManager**：三模式调度 + fsnotify 去抖 + `Reconfigure` + 单测；改 main 装配。
4. **设置 API**：handler + 校验 + 触发 Reconfigure + 集成测试。
5. **前端**：侧边栏 layout、设置页、阅读器全屏 + 翻页按钮。
6. **打磨**：样式自适应、错误提示、补测试覆盖、端到端冒烟。

每个里程碑可独立编译运行与测试。
