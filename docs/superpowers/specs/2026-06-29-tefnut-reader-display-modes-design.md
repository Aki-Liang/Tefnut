# Tefnut 阅读展示方式（单页 / 连续 / 双页）— 设计方案

日期：2026-06-29
状态：已与用户确认，待评审
基于：`feature/comic-server`

## 1. 概述与目标

阅读器支持三种展示方式，**每本漫画各自记忆**：

- **单页左右翻**（`single`，默认，现状）：单张页、分页、随阅读方向 LTR/RTL。
- **单页上下连续**（`continuous`，webtoon）：所有页竖向堆叠、滚动阅读、懒加载。
- **双页左右翻**（`spread`）：两页并排、按方向左右排列；配对 `[0] [1,2] [3,4]…`（封面单页，其后两页一对）。

## 2. 已确认的设计决策

- 展示方式**每本各存**（`nodes.display_mode` 列，同 `reading_direction` 套路）。
- 双页配对 = **封面单页 + 其后成对**：第 0 页单独，之后 `(1,2)(3,4)…`。
- **continuous 模式下隐藏方向控件**（竖向滚动无左右之分）。

## 3. 数据模型（与 reading_direction 同套路）

`nodes` 加列：

```sql
display_mode TEXT NOT NULL DEFAULT 'single'   -- 'single' | 'continuous' | 'spread'
```

幂等迁移：复用 `ensureColumn(db, "nodes", "display_mode", "TEXT NOT NULL DEFAULT 'single'")`（已有该辅助函数），并把列加进 `nodes` 的 `CREATE TABLE`。`store.Node` 加字段 `DisplayMode string`；`NodeRepo` 三处同步（`nodeCols`、`Search` 列表、`scanNode`）；新增 `(*NodeRepo) UpdateDisplayMode(ctx, id, mode string) error`。

## 4. 后端接口

- `GET /api/comics/:id` 响应加 `displayMode`（`single`/`continuous`/`spread`）。
- `PATCH /api/comics/:id` 请求体支持可选 `displayMode`（指针字段）；校验 ∈ 三值，否则 400；存在则 `UpdateDisplayMode`。与 `readingDirection`、作者/评分互不影响，可单独更新。

## 5. 阅读器交互

阅读栏新增**展示方式**控件 `#modetoggle`（一个 `<select>`：单页 / 连续 / 双页）。切换即 `PATCH /api/comics/:id {displayMode}` 并**实时重渲**当前模式（无需刷新）。`#reader` 带 `data-mode="{{.DisplayMode}}"`。

逻辑页号 0..N-1 贯穿三种模式；"前进/后退"与方向（LTR/RTL）的语义沿用现有约定。三种模式都：保存逻辑页进度、进入时恢复、缩略图带可点跳、作者/评分/标签编辑与预览栏收起照常工作、当前页标记随模式更新。

### 5.1 single（现状）
单张 `#page`、分页；方向感知（点击区/键盘/底部按钮/缩略图）。维持现有行为，纳入模式框架。

### 5.2 spread
- 舞台并排显示 1–2 张图。配对：第 0 页单独成"对"，其后 `(1,2)(3,4)…`。
- "前进/后退"以**对**为单位移动。
- **方向决定左右顺序**：LTR 时小页号在左、大页号在右；RTL 时小页号在右、大页号在左。
- 点击区 / 键盘 / 底部按钮按方向前进/后退一对；缩略图点击跳到该页所在的对。
- 进度 = 当前对的首页（较小页号）。

### 5.3 continuous
- 舞台变为**竖向滚动容器**，全部页竖排堆叠，IntersectionObserver **懒加载**仅接近视口的页。
- 隐藏两侧翻页点击区；底部「上一页/下一页」改为**滚动**到上一/下一页；键盘 ↑/↓（及 ←/→）滚动。
- 当前页 = **最顶部主可见页**（IntersectionObserver 追踪）；进度随滚动**去抖**保存。
- 缩略图点击 = 滚动到该页；进入时滚动到上次阅读页。
- **方向控件隐藏**（竖向无左右）。

## 6. 前端结构

- `reader.html`：`#reader` 加 `data-mode`；阅读栏加 `#modetoggle`；舞台结构调整为可容纳"单图 / 并排两图 / 竖向滚动容器"（由 JS 按模式构建/替换内容）。
- `reader.js`：重构为**模式感知**渲染——`setMode(mode)` 拆建舞台并重绑导航；`goTo(page)` 统一"跳到逻辑页"（single 显示该页、spread 显示其所在对、continuous 滚动到该页）；`advance()/back()` 按模式分派。保留现有进度/预加载/方向/缩略图/收起/元数据编辑逻辑。`#modetoggle` 改动→PATCH→`setMode` 重渲。continuous 模式隐藏 `#dirtoggle`。
- `app.css`：spread 并排布局、continuous 滚动容器、`#modetoggle` 样式（沿用已统一的 select 外观）。

## 7. 错误处理与测试

- 沿用约定：边界校验、错误带上下文、不静默吞错；前端 fetch 失败 `alert`。
- 测试（核心 ≥80%）：
  - `store`：`display_mode` 默认 `single`、`UpdateDisplayMode`、迁移幂等。
  - `server`：`apiComicDetail` 含 `displayMode`；`PATCH` 改 displayMode（合法/非法 400）。
  - `reader` 渲染测试：含 `#modetoggle` + `data-mode`。
  - 三种模式的交互（spread 配对/方向、continuous 滚动/进度/懒加载）以手测为主，关键 DOM 结构以渲染测试覆盖。
- TDD：先测后实现。

## 8. 实现顺序（里程碑）

1. **store**：迁移 + 列 + 字段 + `UpdateDisplayMode` + 三处同步 + 单测。
2. **API**：detail 加 `displayMode`、PATCH 加 `displayMode` + 测试。
3. **阅读器模式框架**：`#modetoggle` 控件 + `data-mode` + 把现有单页逻辑重构进 `setMode/goTo/advance/back` 框架（single 行为不变）+ 渲染测试。
4. **spread 模式**：并排布局 + 配对 + 方向排序。
5. **continuous 模式**：竖向滚动 + 懒加载 + 滚动进度 + 隐藏方向控件 + 收尾。

每个里程碑可独立编译运行与测试。
