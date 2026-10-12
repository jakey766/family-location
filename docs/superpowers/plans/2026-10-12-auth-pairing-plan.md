# 第二步实施计划

> 使用 subagent-driven-development 分工执行：认证与持久化服务作为独立任务，主代理负责 HTTP/CLI 集成与交付；测试先行，完成后分别核对需求和代码质量。不创建 worktree，不执行 git add/commit。

**目标：** 在已验证的 backend 基础上交付管理员登录、一次性配对和可撤销设备凭证。

**架构：** `internal/auth` 管理密码、数据库会话、配对、设备和限流；`internal/httpapi` 处理 JSON、Cookie、CSRF、路由；`cmd/server` 提供初始化子命令。SQLite v2 增量迁移保留现有数据。

**技术栈：** 当前 Go/SQLite，增加官方 x/crypto Argon2id、x/term 隐藏终端输入；依赖版本与 Go 1.26 兼容并锁定。

设计和具体接口见 [第二步设计](../specs/2026-10-12-auth-pairing-design.md)。本文件勾选表示实际完成，真实命令输出摘要归档到 [PROGRESS.md](../../PROGRESS.md)。

## 1. 数据迁移与认证服务

- [x] 在 `backend/internal/storage` 先添加 v1→v2 数据保留和幂等测试，确认旧实现不满足，再实现按版本逐项迁移和业务表；调整原版本断言。
- [x] 在 `backend/internal/auth` 先编写真实库测试：创建管理员、登录正确/错误、过期/注销、令牌摘要、用户名冲突、输入约束，运行失败后实现。
- [x] 增加配对过期、尝试耗尽、并发消费、跨家庭隔离、撤销测试，再实现配对/设备服务；增加持久化限流和过期窗口恢复测试。
- [x] 运行 `make test` 或通过相同项目内缓存变量执行指定包测试；完成需求符合性及代码检查。

## 2. HTTP 与初始化命令

- [x] `backend/internal/httpapi/auth_test.go` 先编写完整登录/配对/设备/撤销/注销流程、未登录/凭证混用、Origin/CSRF、Cookie、JSON/体积限制测试，确认端点缺失时失败。
- [x] 实现 `auth.go`、`request.go`，接入现有 `handler.go`；认证后由服务器推导家庭归属，所有错误只返回固定码。
- [x] `backend/cmd/server/admin_test.go` 测试安全 stdin 读入、参数错误、初始化幂等拒绝及成功不泄漏密码，然后增加 `admin.go`、main 子命令分发。make 增加 `init-admin` 入口，交互输入用户名、家庭与隐藏密码，不把密码放参数。
- [x] 更新 `scripts/smoke.py` 的 schema v2 断言；新增 `scripts/auth_smoke.py` 验证真实二进制初始化、HTTP 全流程及服务重启持久化，测试结束回收进程和临时文件。

## 3. 验证与交付

- [x] `make fmt`、`make test`、`make test-race`、`make vet` 全部通过。
- [x] `make smoke`、`make auth-smoke` 通过真实进程验证，测试不接触现有正式数据库。
- [x] `make build-linux GOARCH=amd64` 与 `GOARCH=arm64` 构建通过。
- [x] 按需求检查及代码质量检查处理实质问题，更新 README、API 使用说明、PROGRESS 和整体计划；记录用户对第一步的本地验证，第二步与未完成位置功能明确区分。

汪汪
