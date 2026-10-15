# 第三步位置后端实施计划

> 执行：使用 executing-plans / subagent-driven-development，当前工作区直接实现，不执行 git add/commit。范围已由项目上下文和“进行下一阶段实现”授权。

**目标：** 交付共享启停、唯一最新位置、时间/序号保护、家庭查询、租约清理及隔离模拟工具。

**架构：** 在 backend/internal/location 独立封装事务；HTTP 复用认证和 JSON 边界；入口管理清理生命周期。迁移增量升级 v3，契约见[第三步设计](../specs/2026-10-15-location-design.md)。

**技术栈：** 现有 Go、database/sql、SQLite、Python 标准库，不增加依赖。

## 1. 数据库升级

- [x] 先修改 storage/sqlite_test.go 当前版本断言并增加真实 v2 升级/失败回滚/约束测试，执行 `make test` 观察缺少 v3 失败。
- [x] 修改 storage/sqlite.go 中的迁移和 locationSchema：devices 共享会话与水位、latest_locations 唯一行及租约索引；保留旧账户/设备。
- [x] 更新 scripts/smoke.py、auth_smoke.py 的 schema 断言，验证旧版本与重复打开。

## 2. 共享与位置服务

- [x] 新建 location/service_test.go、cleanup_test.go，真实 SQLite + 可控 now；覆盖生命周期、校验、序号、过期、家庭隔离与并发停止/撤销，先运行失败。
- [x] 新建 location/service.go、sharing.go、locations.go、cleanup.go，实现 `Start(ctx, device)`、`Stop(ctx, device, sessionID)`、`Upload(ctx, device, input)`、`List(ctx, admin)`、`Cleanup(ctx)`，每次写入事务内复核设备身份和当前会话。
- [x] 修改 auth/devices.go，撤销事务同时清空共享状态、水位、位置；复核重试不续租与清理后水位保留。

## 3. API 与生命周期

- [x] 新建 httpapi/location_test.go，先通过真实处理器请求证明新路由缺失；覆盖权限分离、字段缺失、跨家庭、停止、响应字段。
- [x] 新建 httpapi/location.go，handler.go 注册；auth.go 提取共用设备身份检查。
- [x] main.go 挂接清理任务，测试启动清理和取消，确保关闭数据库前退出。

## 4. 真实进程验证与交付

- [x] 新建 scripts/location_smoke.py：隔离临时库、随机临时凭证、模拟位置、登录配对、启动上传、重启、停止与撤销，检查日志；demo 模式为后续网页提供项目内模拟服务。
- [x] Makefile 增加 location-smoke、location-demo，所有缓存/临时文件仍在项目内。
- [x] 执行 `make test`、`make test-race`、`make vet`、`make smoke`、`make auth-smoke`、`make location-smoke`、两架构 Linux 构建；独立评审规格和实现并修复问题。
- [x] 新建 docs/LOCATION_API.md，更新 README.md、PROGRESS.md、整体设计/计划；保留尚未客户端/真机/公网验证边界，下一步为 iPhone 网页。

## 实际结果

2026-10-15：所有任务完成，测试先观察缺少迁移/业务类型/路由/清理循环的失败，再实现。全包 test/race/vet、三类真实进程烟雾验证、两种 Linux 架构构建通过；需求与质量独立评审通过。详细结果与未验证边界统一见 [PROGRESS.md](../../PROGRESS.md)。没有创建客户端或进行真机/公网验收，没有执行 git add/commit。

汪汪
