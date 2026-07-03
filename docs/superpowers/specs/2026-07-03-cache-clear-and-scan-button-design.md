# 缓存清理 + 图书馆扫描按钮 + 单位同行 — 设计(紧凑)

日期:2026-07-03;分支 `feature/cache-clear-scan-button`(基于 main)

## 1. 单位下拉与输入框同行

设置页缓存两行改用 `.budget-row`(`display:flex; align-items:center; gap:8px`,label 固定不换行),替换目前继承自 `.mode-row` 的纵向布局。DOM 顺序不变(label → input → select),纯样式修复。

## 2. 一键清理缓存

- `internal/cache.Clear(root) (freed int64, err error)`:删除 root 下全部一级子目录/文件,返回释放字节数;root 不存在 → (0, nil);root 本身保留。
- `POST /api/cache/clear`:对 `data/cache` 与 `data/thumbs/pages` 各执行 Clear,返回 `{"freedBytes": N}`。封面缩略图(`thumbs/*.jpg`)不清(元数据,重建需全库重扫)。打开中的阅读器依赖既有自愈(openPage 失败 → Drop → 重解压),不额外干预。
- UI:设置页「缓存」区底部「立即清理」按钮(danger 样式)+ `confirm()`;成功后 alert「已清理 X MiB/GiB」(JS 格式化)。

## 3. 图书馆页扫描按钮(转圈)

- `scan.Manager.Scanning() bool`(mu 下读 `m.scanning`);server `Reconfigurer` 接口增该方法(stubReconf 同步)。
- `GET /api/scan/status` → `{"scanning": bool}`。
- browse 页 toolbar 右侧图标按钮「↻ 扫描」:点击 → `POST /api/scan` → 加 `.spinning`(CSS keyframes 旋转 ↻)+ disabled → 每 2s 轮询 status → 结束 → `location.reload()`。页面加载时先查一次 status,后台扫描中则直接呈现转圈态。

## 测试

cache.Clear 两例;Manager.Scanning 阻塞扫描器一例;server:status 端点(stub 两态)、clear 端点(种子文件 → 清空 + freed>0 + 封面不动)。UI 手工 + 真机验证。

## 部署遗留(随本次处理,不入 PR)

生产 compose 移除 `TEFNUT_CACHE_MAX_BYTES=1`(1 字节地雷)与 `TEFNUT_THUMB_PAGES_MAX_BYTES` 行,加 `./config:/config` 挂载;新镜像发布后一次性 force-recreate。
