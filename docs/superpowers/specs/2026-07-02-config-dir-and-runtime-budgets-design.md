# 配置目录挂载 + 缓存预算运行时可改 — 设计

日期:2026-07-02
分支:`feature/config-dir-runtime-budgets`(基于 `fix/nonblocking-initial-scan`,依赖 PR #8 的 `scan.Budgets` / `config.parseSize`)

## 需求

1. 配置文件放进一个可被 docker 挂载的**目录**,宿主机直接编辑,改完重启容器生效。
2. 缓存大小(解压缓存、页缩略图)在配置文件里可配(已有,纳入新配置目录方案)。
3. 系统**设置页**里也能修改缓存大小,保存后生效,无需重启。

## 已确认的决策

| 决策点 | 结论 |
|---|---|
| 三来源优先级 | **DB(设置页保存过) > env > yaml > 内置默认**;fallback 链而非一次性种子——UI 从未保存过时,改 yaml/env 重启即生效;保存过则以 DB 为准 |
| 容器配置目录 | **`/config/config.yaml` + 二进制自写默认**(文件缺失时写入带注释模板再继续启动,LinuxServer.io 风格) |
| 设置页输入形态 | **数字 + 单位下拉(MiB/GiB)**,下拉用项目自定义格/PANEL 主题组件(不用原生 select),`0 = 不限制` |
| 生效值解析位置 | 复用 SettingsRepo 模式:`GetBudgets(ctx, defaults)`,不引入新组件 |

## 架构与数据流

```
config.LoadOrInit(path)          # 文件缺失 → 自写模板;产出 env>yaml>默认 的 Budgets(= defaults)
        │
        ▼
scan.Manager{defaults Budgets}   # enforceBudgets() 每次执行现查:
        │                        #   settings.GetBudgets(ctx, defaults)
        ▼                        #   → DB 有键用 DB,无键用 defaults
cache.Enforce(cache/, b.ExtractCacheBytes)
cache.Enforce(thumbs/pages/, b.PageThumbBytes)

设置页 PUT /api/settings ──► SettingsRepo.SetBudgets ──► Reconfigure()
                                                          └─► 后台重扫 → enforceBudgets → 新预算立即执行
```

## 组件改动

### 1. `internal/store/settings_repo.go`

- 新键:`cache_max_bytes`、`thumb_pages_max_bytes`(TEXT 存十进制字节数,与现有 k-v 表一致)。
- `GetBudgets(ctx context.Context, defCache, defPageThumb int64) (cache, pageThumb int64, err error)`:逐键查询,`sql.ErrNoRows` → 用对应 default;值解析失败按错误处理(不静默回落)。**签名用两个 int64**,避免 store → scan 的反向依赖;scan/server 侧各自组装。
- `SetBudgets(ctx, cacheMax, pageThumbMax int64) error`:事务 upsert 两键(同 `SetScan` 手法)。

### 2. `internal/config`

- 新增 `LoadOrInit(path string) (*Config, error)`:
  - 文件存在 → 等同 `Load`。
  - 不存在 → 写入 embedded 默认模板(0644,目录不存在则 MkdirAll)再 `Load`;写入失败(只读挂载等)→ 报错并指明路径与原因。
- 模板(embedded 字符串,带中文注释):`library.rootPath: /comics`、`dataDir: /data`、`server.addr: ":8086"`、`thumbnail`(width/pageWidth/pagesMaxBytes)、`cache.maxBytes`;**不含 `scan:` 段**(该段从未接入运行时;Go 结构保留解析以兼容旧文件)。
- env 覆盖(`applyEnv`)逻辑不变,继续作用于 Load 产出的 defaults。

### 3. `internal/scan/manager.go`

- `Manager.budgets` 语义从"固定值"改为 **defaults**(命名改为 `defaults` 以明晰)。
- `enforceBudgets()` 开头调 `m.settings.GetBudgets(...)` 取生效值;查询失败:log + 本轮跳过清理(不许 fallback 到 defaults 静默清库——预算读不到时宁可不删)。

### 4. `internal/server`(API)

- `settingsDTO` 增:`cacheMaxBytes int64`、`thumbPagesMaxBytes int64`(**生效值**,原始字节)。
- `apiGetSettings`:经 `GetBudgets` 返回生效值。**NewServer 增两个 int64 参数**(defCacheMax、defPageThumbMax),server 不引入 scan 包依赖(其 Reconfigurer 是本地接口,保持现状)。
- `apiUpdateSettings` body 增两字段(`*int64`,缺省 = 不改,兼容旧客户端只发 scan 字段):校验 `>= 0`,`SetBudgets` 持久化(与 `SetScan` 同请求内先后执行,各自事务),末尾沿用现有 `Reconfigure` 调用。
- 错误提示中文、具体(如「缓存上限必须 ≥ 0 字节」)。

### 5. 设置页 UI(`settings.html` / `settings.js`)

- 新「缓存」区,置于扫描设置之后:
  ```
  缓存
  ─────────────────────
  解压缓存上限   [ 2    ] [GiB ▾]
  页缩略图上限   [ 512  ] [MiB ▾]
  (0 = 不限制)          [保存]
  ```
- 下拉复用 `dropdown.js` 的格/PANEL 主题组件;单位仅 MiB/GiB。
- 字节 ↔ 显示换算:整除 GiB 显示 GiB;否则整除 MiB 显示 MiB;否则**仅显示**向上取整为 MiB(存储不变)。保存时前端换算成字节提交。
- 客户端校验非负整数;服务端为准。

### 6. Dockerfile

- 去掉 `COPY deploy/config.yaml /etc/tefnut/config.yaml`。
- `mkdir -p /config && chown tefnut /config`;`ENTRYPOINT ["tefnut", "-config", "/config/config.yaml"]`。
- 向后兼容:未挂载 `/config` 的旧 compose 也能跑(自写进容器层;`/comics`、`/data` 不变)。`deploy/config.yaml` 删除——其内容并入 embedded 模板;全仓唯一引用就是 Dockerfile 这一行 COPY(已核实),CI 无其他依赖。

### 7. rainmaker

- 生成宿主 `./config/config.yaml`(printf 逐行,防元字符,同 compose 手法),预算提示的值**写进 yaml**;`./config` 已有 config.yaml 则不覆写(提示沿用现有配置,跳过预算提示)。
- compose:`volumes` 增 `./config:/config`;**删除** `TEFNUT_*` env 行(应用仍支持 env,留给高级用户)。

## 错误处理

- `/config` 不可写:启动失败,错误含路径与建议(检查挂载权限)。
- DB 预算键值损坏(非数字):GetBudgets 报错 → API 500 / enforce 跳过并 log,不静默清库。
- UI 非法输入:400 + 中文提示;负数拒绝。
- 旧配置文件含 `scan:` / 缺新键:正常解析,缺键走默认。

## 测试(TDD,全部先 RED)

| 层 | 用例 |
|---|---|
| store | GetBudgets 无键回落 defaults;有键覆盖;SetBudgets 往返;坏值报错 |
| config | LoadOrInit 缺文件自写(可再 Load、含预算键、rootPath=/comics);已存在不覆写;目录只读报错 |
| server | GET 返回生效预算(DB 空 → defaults;保存后 → DB 值);PUT 校验负数 400、持久化、触发 Reconfigure;旧 body(无预算字段)不动预算 |
| scan | enforceBudgets 用 DB 值覆盖 defaults(真实 SettingsRepo + 临时目录淘汰断言);GetBudgets 出错时跳过清理 |
| rainmaker | shim 干跑:生成 ./config/config.yaml、compose 含 `./config:/config`、无 `TEFNUT_*` 行;已有 config.yaml 不覆写 |
| 真机 | 首启自写 /config/config.yaml;设置页改预算保存 → 淘汰日志;重启后 UI 值保持 |

## 范围外

- scan.interval 等 yaml 死配置的清理(仅模板不再包含,结构保留)。
- KiB/TiB 单位、按库/按格式的差异化预算。
- 设置页显示"当前值来源"(DB/env/yaml)标注。
