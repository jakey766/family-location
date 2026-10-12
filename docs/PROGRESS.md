# 项目进度

更新日期：2026-10-12。需求以 [PROJECT_CONTEXT.md](PROJECT_CONTEXT.md) 为准，实施任务见[整体计划](superpowers/plans/2026-10-10-family-location-plan.md)及[第二步计划](superpowers/plans/2026-10-12-auth-pairing-plan.md)。本文件记录事实，不将后续方案视为已交付能力。

## 当前状态

第一步已完成，用户于 2026-10-12 反馈 backend 本地运行验证通过，并要求继续。第二步现已实现管理员认证、设备配对、独立设备凭证及权限校验。尚无位置共享业务或客户端；没有重复开始第一步。

## 已完成

- 保存整体设计与分阶段实施计划，建立 `backend/` 目录结构。
- Go HTTP 服务支持环境配置、结构化日志、启动失败退出及 SIGINT/SIGTERM 优雅关闭。
- SQLite 文件自动初始化，使用 WAL、外键约束、busy timeout、单连接池；第一步版本 1 幂等初始化，第二步增量升级至版本 2，拒绝高于程序支持版本的数据库。
- `GET/HEAD /healthz` 表示进程存活；`GET/HEAD /readyz` 查询迁移版本判断数据库就绪，失败返回 503；其他方法 405，未知路径 404。
- 提供 Makefile、真实进程烟雾脚本和运行说明；缓存、临时文件、构建产物放在项目内。
- 已生成本机 macOS arm64 二进制，以及 Linux amd64/arm64 静态二进制，位于 `bin/`。
- 新建 `docs/PROJECT_CONTEXT.md`、本进度文档与根目录 `AGENTS.md`；现有设计与计划已同步设备、离线时间语义、电量和管理员要求。
- 第二步新增 families、users、web_sessions、pairing_codes、devices、auth_limits 表；真实 v1 数据库升级保留原有数据，迁移失败回滚。
- `init-admin` 子命令和 `make init-admin` 交互初始化家庭及管理员；隐藏密码输入、两次确认，也支持显式 stdin 输入；没有默认账户或公开注册。
- Argon2id 密码摘要、24 小时可撤销网页会话、同源 Origin 和 CSRF 校验；会话及设备凭证仅存摘要。
- 一次性配对标识和 8 位数字码、10 分钟过期、最多 5 次错误、同家庭新码替换旧码及并发原子消费；设备独立 Bearer 凭证、列表、跨家庭权限隔离、立即撤销。
- SQLite 持久化登录账号/IP 限流、配对 IP 限流；过期记录清理、限流存储容量和密码计算并发上限。
- 新增 [认证 API 说明](AUTH_API.md)、第二步设计/计划及 `make auth-smoke`；所有测试使用项目内临时库，没有为正式数据库预设管理员或保存真实凭证到文档。

## 已执行验证及结果

以下为 **2026-10-10 第一步实现时实际执行并通过**的历史检查：

| 检查 | 结果与覆盖范围 |
| --- | --- |
| `make test` | 通过配置、存储、HTTP 包测试；包括默认/无效配置、真实数据库持久化和迁移幂等、PRAGMA、损坏库、取消、较新版本拒绝、就绪失败与超时 |
| `make test-race` | 通过竞态检测 |
| `make vet` | 通过静态检查 |
| `make smoke` | 通过真实 HTTP 存活/就绪检查、SQLite 重启持久化、SIGTERM 正常退出、无效数据库路径和端口占用失败；脚本回收了其启动的子进程 |
| `make build-linux GOARCH=amd64` | 构建通过，`file` 确认为 x86-64 静态 ELF |
| `make build-linux GOARCH=arm64` | 构建通过，`file` 确认为 ARM aarch64 静态 ELF |
| 独立代码检查 | 未发现阻塞本次交付的问题 |

第一步的入口仅通过烟雾脚本验证，当时 `backend/cmd/server` 没有单独的 Go 单元测试；第二步现已增加 CLI 单元测试。Linux 二进制仅完成交叉构建，尚未在公网服务器运行。测试数据库与日志位于 `.cache/tmp/`，由测试清理。

2026-10-10 文档归档时，6 份相关文档的本地链接、确认需求和原有规则检查通过。2026-10-12 用户反馈本地 backend 验证通过；该反馈不代表公网部署、安卓或 iPhone 验证完成。

### 第二步验证（2026-10-12）

| 检查 | 实际结果 |
| --- | --- |
| `make test` | 通过全部 Go 包，覆盖 CLI、认证、HTTP、配置和 SQLite；含过期注销、摘要存储、5 次错误配对失效、8 路并发仅一次成功、跨家庭隔离、撤销、持久限流、容量清理及事务回滚 |
| `make test-race` | 全部包通过竞态检测 |
| `make vet` | 通过 |
| `make smoke` | 基础 HTTP、schema v2 重启持久化、SIGTERM、无效路径及端口占用验证通过 |
| `make auth-smoke` | 真实二进制验证 v1→v2 数据保留、CLI 初始化、登录/CSRF、一次性配对、设备权限、重启后两类凭证有效、撤销/注销通过；检查日志未含测试凭证；PTY 验证密码提示处 SIGINT/SIGTERM 后终端属性恢复且未创建数据库 |
| Linux amd64 / arm64 构建 | 两种架构的 `make build-linux` 均通过；没有远程运行验证 |
| 需求符合性审核 | 独立审核通过，核心第二步需求均在实现及测试中落实 |
| 代码质量审核 | 未发现阻塞项；发现的密码输入中断不恢复终端回显问题已补失败用例、修复并复核通过 |

HTTP 层覆盖匿名访问、账号/设备凭证互用拒绝、Origin/CSRF、JSON 类型/字段/大小限制及忽略伪造转发 IP，也验证 HTTP Cookie 与 TLS 请求下 Secure Cookie 的登录/注销属性。TLS 请求测试不代表已部署 HTTPS。第二步还未经过用户手动验收。

## 当前启动与检查

需要 Go 1.26.0 或更高版本及 make；烟雾脚本需要 Python 3。以下命令在本机执行：

```bash
cd /Users/jiangkunpeng/dev/jk_space/family-location
make init-admin
make run
```

先停止仍在运行的旧服务；首次创建管理员后，后续只需 `make run`，已有用户名初始化会报错且不覆盖账户。`make init-admin` 交互询问用户名、家庭和密码，密码不回显。运行与 API 操作详见 [AUTH_API.md](AUTH_API.md)。

在另一终端检查：

```bash
curl -i http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:8080/readyz
```

正常均返回 200。默认监听 `:8080`，使用项目内 `data/family-location.db`；Ctrl+C 停止。更多环境配置及服务器运行方法见 [README](../README.md)。

## 未完成与未验证

- 位置业务表、上传/查询、每台设备仅保留最新值、乱序保护、离线原始定位时间、精度/电量字段及共享租约尚未实现。
- Kotlin App、高德定位 SDK、共享启停、后台定位策略和签名 APK 尚未实现。
- iPhone Safari 网页、高德地图、多人展示、精度/电量/更新时间和位置过期提示尚未实现。
- Mi 11 Lite（Android 13）、Redmi Note 13 Pro（Android 13）、vivo X9（Android 7.1.2）均未进行本项目真机验证；30 秒／3 分钟效果、耗电和后台稳定性尚无实测结论。
- 公网服务器部署、HTTP/IP 端到端联调未执行；HTTPS 按已确认范围留待后续。

## 下一步

沿用现有 `backend/` 继续第三步：最新位置唯一设备行、共享会话与主动启停、上传/查询、原始定位时间与单调序号的乱序保护、精度/电量、租约和位置过期处理。停止共享与在途上传必须在事务内协调，防止位置被旧请求恢复。随后再推进安卓和网页。

本次交付只到第二步，不将配对成功表述为定位共享已完成。

汪汪
