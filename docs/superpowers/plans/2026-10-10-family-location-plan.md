# 家庭位置共享实施计划

> 执行方式：使用 executing-plans 在当前会话逐项执行；按用户要求在当前目录工作，不创建 worktree，不执行 git add/commit。本次完成第一步，后续阶段在对应任务开始时细化并实现。

**目标：** 先保存整体设计，再交付可启动、可验证、可部署构建的 Go + SQLite 基础服务。

**架构：** 单个 Go HTTP 进程使用 SQLite 持久化。入口负责生命周期，配置包负责环境变量，存储包负责初始化与迁移，HTTP 包负责状态接口。后续安卓和网页共享同一套认证 API。

**技术栈：** Go 1.26.0、net/http、database/sql、modernc.org/sqlite v1.60.1；后续 Kotlin、高德定位 SDK、iPhone Safari 网页。

已确认需求统一见 [PROJECT_CONTEXT.md](../../PROJECT_CONTEXT.md)，真实完成状态与验证记录见 [PROGRESS.md](../../PROGRESS.md)。本计划中的后续路线不是已完成项，沿用 `backend/` 目录继续实施。

## 第一步实施清单

### 1. 保存设计与工程工具

- [x] 保存 `docs/superpowers/specs/2026-10-10-family-location-design.md` 与本计划，确认只实现第一步。
- [x] 创建 `backend/go.mod` 固定模块与驱动版本；生成 `backend/go.sum` 校验依赖。
- [x] 创建根目录 `.gitignore`、`Makefile`，把 GOCACHE/GOMODCACHE/GOPATH/GOTMPDIR/TMPDIR/临时构建及二进制放入项目。Makefile 包含 deps/test/test-race/vet/build/build-linux/run/smoke/fmt。

### 2. 配置与持久化（测试先行）

文件：`backend/internal/config/config.go`、`config_test.go`；`backend/internal/storage/sqlite.go`、`sqlite_test.go`。

- [x] 配置测试覆盖默认值、环境覆盖、空地址/非法端口/空路径/内存数据库的拒绝；执行 `make test` 确認因缺少实现而失败，然后实现 `config.Load() (Config, error)`。
- [x] 存储测试使用真实项目内临时 SQLite 文件，验证 `Open(ctx, path) (*sql.DB, error)` 自动建目录、版本 1、幂等重开和数据保留；配置 WAL、外键、等待锁；验证连接重建也保留 PRAGMA。执行测试先确认缺少功能。
- [x] 实现文件预创建、0600 权限、URL 编码 DSN、单连接池、事务迁移；初始化失败释放句柄；现有高版本库拒绝打开，避免旧程序误操作新结构。
- [x] 存储失败用例覆盖非法/不可创建路径、损坏数据库、取消上下文、高版本数据库；`make test` 通过。

验证示例（在根目录通过 Makefile 配置的环境执行）：

```go
db, err := storage.Open(ctx, path)
// 检查 err；SELECT version FROM schema_migrations 得到 1。
// 写入测试用记录、关闭、重开后读取，确认真实落盘。
// 执行违反外键的 INSERT，确认返回错误。
```

### 3. HTTP 健康与就绪（测试先行）

文件：`backend/internal/httpapi/handler.go`、`handler_test.go`。

- [x] 使用 httptest 与真实数据库编写成功路径、关闭数据库后的 503、关闭后 healthz 仍为 200、缺少版本表返回 503；检查状态码、JSON、Content-Type、Cache-Control。
- [x] 测试仅接受 GET/HEAD，POST 返回 405；未知路径返回 404；耗尽单连接池时请求在就绪超时内结束。
- [x] 先运行 `make test` 确认测试失败，再实现 `httpapi.New(db *sql.DB) http.Handler`；readiness 使用请求上下文及 1 秒 deadline，响应不输出内部错误。
- [x] 运行 `make test`，所有用例通过。

HTTP 契约：

```text
GET /healthz -> 200 {"status":"ok"}
GET /readyz  -> 200 {"status":"ok","database":"ok"}
数据库不可查询或版本不匹配 -> 503 {"status":"unavailable","database":"unavailable"}
```

### 4. 进程入口、文档与烟雾验证

文件：`backend/cmd/server/main.go`、`scripts/smoke.py`、`README.md`。

- [x] 编写烟雾脚本：自动选本地空闲端口、启动构建二进制、轮询 readiness、检查两个端点、发 SIGTERM，验证进程 0 退出；用同一数据库再次启动，验证重启成功。无效数据库路径和占用端口须非零退出。
- [x] 脚本执行失败后实现 main：读取配置，10 秒初始化数据库；启动 HTTP，设置超时；处理 SIGINT/SIGTERM，10 秒优雅关闭并释放数据库。
- [x] README 写明依赖、执行位置、环境变量、接口、Linux amd64/arm64 构建、公网 IP 访问及当前未实现范围；不假称已部署服务器。
- [x] 执行 `make test`、`make test-race`、`make vet`、`make smoke`、`make build-linux GOARCH=amd64`、`make build-linux GOARCH=arm64`；记录实际结果。
- [x] 检查所有生成文件均位于项目内，烟雾脚本已回收其启动的子进程；独立代码检查未发现阻塞项，更新清单状态。

## 后续阶段路线（按 PROGRESS.md 持续更新实际状态）

2026-10-13：第二步已按[独立实施计划](2026-10-12-auth-pairing-plan.md)实现；第三步现已按[位置后端计划](2026-10-15-location-plan.md)实现（2026-10-15），第四步已按[网页计划](2026-10-15-web-plan.md)实现代码并做 DOM 自动化验证，高德真实 Key 与 iPhone Safari 验收未完成；安卓阶段尚未执行。按用户确认将网页提前到安卓开发之前，网页用隔离的模拟位置验证，真实手机上报在安卓阶段联调。

| 顺序 | 工作与交付 | 关键验收 |
| --- | --- | --- |
| 第二步 | 迁移家庭/账户/会话/配对/设备表，管理员初始化 CLI，登录注销、CSRF、一次性配对码、独立设备凭证与撤销、权限校验及限流 | 未登录拒绝、家庭隔离、配对一次性、过期、重试限制、凭证撤销 |
| 第三步 | latest_locations 唯一设备行、共享会话、上传/查看/停止、精度/电量、原始定位与接收时间、租约与清理、乱序保护 | 多次上传一行、旧数据不覆盖新数据、停止并发不复活、断网重试保留原始定位时间、位置过期与租约分别判断、无轨迹和坐标日志 |
| 第四步 | iPhone Safari 同源网页、管理员登录、设备配对入口、高德多人地图/列表、精度/电量/更新时间和过期提示 | Safari、登录退出、陈旧位置不伪装为新位置、GCJ-02 地图一致、移动布局 |
| 第五步 | Kotlin App、高德 SDK、权限与前台服务、配对、共享开关、30/180 秒策略、断网仅保留最新待传点、直接安装签名 APK | 分别验证 Mi 11 Lite / Redmi Note 13 Pro 的 Android 13 和 vivo X9 的 Android 7.1.2；权限、停止、锁屏、断网和省电表现 |
| 第六步 | 公网服务器安装、进程守护、HTTP/IP 联调、排除位置历史的备份策略 | 安卓到 iPhone 端到端、重启恢复、停止共享、设备撤销 |

后续才需要：服务器 CPU 架构/连接信息、高德 Android Key、包名与签名 SHA-1、网页地图 Key。第一步不依赖这些信息。

## 第一步验证记录（2026-10-10）

已将实际执行结果统一归档至 [PROGRESS.md 的验证记录](../../PROGRESS.md#已执行验证及结果)，保留上方任务勾选作为第一步执行清单，后续不在本计划重复维护验证结果。

汪汪
