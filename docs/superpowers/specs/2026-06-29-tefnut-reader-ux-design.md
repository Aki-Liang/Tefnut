# Tefnut 阅读体验增强：侧栏折叠 + 设置布局 + 缩略图预览栏 + 翻页方向 — 设计方案

日期：2026-06-29
状态：已与用户确认，待评审
基于：`feature/comic-server`（v1 + 设置/侧栏增强已完成）

## 1. 概述与目标

四项 UI/UX 增强：

1. **侧边栏可折叠** —— 一个常驻 ☰ 按钮收起/展开侧栏，折叠状态记忆。
2. **设置页扫描方式布局** —— 三种模式纵向排列；定时间隔给格式说明；每日定时用「小时+分钟」下拉框。
3. **阅读器底部缩略图预览栏** —— 横向缩略图带、当前页居中高亮、点图跳转、可收起为带箭头的窄边、懒加载、按需生成小图。
4. **每本漫画的翻页方向（LTR/RTL）** —— 每本各自存储与切换，影响导航映射、控件位置、缩略图顺序。

## 2. 已确认的设计决策

- **翻页方向每本各存**：`nodes` 加 `reading_direction` 列，默认 `ltr`；在阅读器里切换并记住。
- **缩略图按需生成**：新接口 `/api/comics/:id/pages/:n/thumb` 复用 `thumb` 包缩放该页，懒加载 + 浏览器缓存 + 进程内小缓存。不预生成、不额外占磁盘。
- **键盘/点击映射**（逻辑页号 0..N-1，0=第一页，"前进"=页号+1）：
  - **LTR**：点右半屏=前进；下一页按钮在右；缩略图带 左→右（0 最左）；键盘 → 前进、← 后退。
  - **RTL**：点左半屏=前进；下一页按钮在左；缩略图带 右→左（0 最右）；键盘 ← 前进、→ 后退。
  - 图片仍单页显示；方向只影响导航映射、控件位置、缩略图顺序。
- 折叠状态（侧栏、缩略图栏）存 `localStorage`，刷新保持。

## 3. 非目标（本次不做）

- 双页对开（spread）显示。
- 连续滚动（webtoon）模式。
- 预生成全部页缩略图 / 缩略图持久化到磁盘。
- 每本漫画独立的其它阅读偏好（仅方向）。

## 4. 数据模型

`nodes` 表新增一列：

```sql
reading_direction TEXT NOT NULL DEFAULT 'ltr'   -- 'ltr' | 'rtl'
```

**迁移（幂等，兼容已部署的 DB）**：在 `store.Open` 的 schema 之后运行一个 `ensureColumn(db, "nodes", "reading_direction", "TEXT NOT NULL DEFAULT 'ltr'")`：用 `PRAGMA table_info(nodes)` 检查列是否存在，缺失则 `ALTER TABLE nodes ADD COLUMN ...`。同时把该列也加进 `nodes` 的 `CREATE TABLE`（新 DB 直接带上；迁移检查到已存在则跳过）。

`store.Node` 结构加字段 `ReadingDirection string`；`NodeRepo` 的列清单/`scanNode` 读取该列；新增 `(*NodeRepo) UpdateReadingDirection(ctx, id int64, dir string) error`。`Create` 不显式写该列（用默认 `ltr`）。

## 5. 后端接口

### 5.1 每页缩略图
`GET /api/comics/:id/pages/:n/thumb` → 打开压缩包取第 n 页（0 基），`thumb.Generate(reader, 120)` 返回 JPEG。
- 响应头 `Cache-Control: public, max-age=86400`（浏览器缓存）。
- 进程内小缓存：`Server` 持有一个有界缓存（key=`<id>:<n>`，上限约 256 项，超出丢弃最旧/清理），避免同进程重复解码。
- 校验：`:id`/`:n` 合法整数、n 在范围内（越界 404）；节点不存在 404。

### 5.2 翻页方向
- `GET /api/comics/:id` 的响应加 `readingDirection`（`ltr`/`rtl`）。
- `PATCH /api/comics/:id` 的请求体支持可选 `readingDirection`（指针字段）；校验 ∈ {`ltr`,`rtl`}，否则 400；存在则调用 `UpdateReadingDirection`。作者/评分仍走原逻辑（三个字段都用指针，互不影响、可单独更新）。

## 6. 前端：侧边栏折叠

`layout.html` 在侧栏块内加一个常驻 ☰ 切换按钮（固定左上，z-index 高于侧栏，折叠后仍可见用于展开）。`static/js/sidebar.js`：点击切换 `document.body` 的 `.sidebar-collapsed` 类，写入 `localStorage`，页面加载时读取并还原。CSS：`.sidebar-collapsed .sidebar { transform: translateX(-100%) }`、`.sidebar-collapsed main.with-sidebar { margin-left: 0 }`，过渡动画。阅读器整页置空了侧栏块，因此不受影响。

## 7. 前端：设置页布局

`settings.html`/`settings.js`/CSS：
- 三个扫描模式块改为**纵向**（去掉 flex 横向平铺，改为每块独立一行）。
- **定时间隔**：文本框旁/下加格式说明（"如 `30m`、`2h`、`90m`；单位 s/m/h"）。
- **每日定时**：用两个 `<select>`——小时 `00`–`23`、分钟 `00`–`59`。保存时由两个下拉拼出 `HH:MM` 放进 `scanDailyTime`；加载时把已存的 `scan-data` 的 daily 值拆成时/分回填两个下拉。

## 8. 前端：阅读器缩略图预览栏 + 翻页方向

`reader.html`/`reader.js`/CSS（阅读器全屏、无侧栏不变）：

**方向**：`#reader` 加 `data-dir="{{.ReadingDirection}}"`。reader.js 读取方向，定义 `advance()`/`back()`（始终基于逻辑页号 ±1），并按方向把：点击区（左/右半屏哪个是前进）、底部「上一页/下一页」按钮的位置、键盘 ←/→ 的含义、缩略图带的视觉顺序 全部映射到位（见 §2 约定）。阅读栏加一个**方向切换**控件（LTR/RTL），改动即 `PATCH /api/comics/:id {readingDirection}` 并实时重排布局，无需刷新。

**缩略图预览栏**：底部一条横向滚动条，N 个缩略图占位（`<img>`，`src` 指向 `/api/comics/:id/pages/:n/thumb`），用 IntersectionObserver **懒加载**仅可见的几张。当前页缩略图**高亮**并通过 `scrollIntoView({inline:'center'})` **居中**；点任意缩略图跳到该页。顺序按方向（RTL 用 `flex-direction: row-reverse` 或等价）。**可收起**：点收起按钮把预览栏隐藏，只留一条**带箭头(▲/▼)的窄边**，点箭头展开；状态存 `localStorage`。

## 9. 模块改动清单

```
internal/store/store.go          改  CREATE TABLE 加列 + ensureColumn 迁移
internal/store/migrate.go        新  ensureColumn(db, table, column, ddl) 幂等迁移辅助
internal/store/node_repo.go      改  Node 列清单/scanNode 加 reading_direction + UpdateReadingDirection
internal/server/api_nodes.go     改  comicDetailDTO 加 readingDirection；新增 apiPageThumb；路由
internal/server/api_meta.go      改  metaReq 加 ReadingDirection 指针 + 校验 + 调用
internal/server/server.go        改  新增 /pages/:n/thumb 路由；Server 加缩略图缓存字段
internal/server/thumbcache.go    新  有界进程内缩略图缓存
internal/server/web/templates/layout.html   改  侧栏折叠 ☰
internal/server/web/templates/reader.html   改  缩略图带 + 方向切换 + data-dir
internal/server/web/templates/settings.html 改  扫描方式纵向 + 时/分下拉 + 间隔说明
internal/server/web/static/js/sidebar.js     新  折叠逻辑 + localStorage
internal/server/web/static/js/reader.js       改  方向映射 + 缩略图带 + 折叠
internal/server/web/static/js/settings.js     改  时/分下拉 拼/拆 HH:MM
internal/server/web/static/css/app.css        改  侧栏折叠、设置纵向、缩略图带、方向相关样式
```

## 10. 错误处理与测试

- 沿用现有约定：边界校验、错误带上下文、不静默吞错；前端 fetch 失败 `alert`。
- 测试（核心 ≥80%）：
  - `store`：`ensureColumn` 迁移幂等（重复 Open 不报错、列存在）；`UpdateReadingDirection`；`scanNode` 读取方向默认 `ltr`。
  - `server`：`apiPageThumb`（返回 image/jpeg、越界 404）；`apiComicDetail` 含 `readingDirection`；`PATCH` 改方向（合法/非法 400）；缩略图缓存命中。
  - 前端模板渲染测试（reader 含缩略图带容器 + 方向控件 + data-dir；settings 含时/分下拉；layout 含 ☰）。
  - 缩略图懒加载、方向映射、折叠交互以手测为主，关键 DOM 结构以渲染测试覆盖。
- TDD：先测后实现。

## 11. 实现顺序（里程碑）

1. **数据模型**：迁移 + Node 字段 + repo（UpdateReadingDirection、scanNode）+ 单测。
2. **缩略图接口**：apiPageThumb + 缓存 + 路由 + 测试。
3. **方向接口**：comic detail 加字段、PATCH 加方向 + 测试。
4. **侧栏折叠**：layout ☰ + sidebar.js + CSS + 渲染测试。
5. **设置布局**：纵向 + 时/分下拉 + 间隔说明（settings.html/js/css）。
6. **阅读器方向 + 缩略图带**：reader.html/js/css（方向映射 + 缩略图带 + 折叠）+ 渲染测试。
7. **打磨**：样式、端到端冒烟、补测试覆盖。

每个里程碑可独立编译运行与测试。
