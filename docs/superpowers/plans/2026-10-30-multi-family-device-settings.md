# 多家庭与设备配置实施计划

> 使用 superpowers:subagent-driven-development 分工实施并审阅；用户已批准设计，直接在当前工作区推进，不执行 git add/commit。

**目标：** 多家庭设备共享、H5 管理和按设备动态下发定位/暂停配置。

**架构：** SQLite schema v4 增加家庭管理员与设备成员关联，保留现有设备的原始家庭作为管理归属兼容字段；同账号管理的家庭可复用设备。设备配置单独保存，位置响应携带版本化配置。H5 显式选家庭，Android 原子保存配置并重排调度。

**技术：** Go/SQLite、现有原生 ES modules、Kotlin/Android/高德；使用现有 Makefile 和项目内缓存。

## 统一接口

```text
GET /api/v1/families -> {families:[{id:数字, public_id:字符串, name:字符串}]}
POST /api/v1/families {name} -> 201 {id,public_id,name}
GET /api/v1/devices?scope=owned -> {devices:[原设备字段及families数组]}
GET /api/v1/devices?family=<public_id> -> {devices:[原设备字段]}
GET /api/v1/locations?family=<public_id> -> 原快照，位置增加 stale_after_seconds
POST /api/v1/pairing-codes?family=<public_id> {} -> 原配对响应
PUT /api/v1/families/{public_id}/devices/{device_id} {} -> 204
DELETE /api/v1/families/{public_id}/devices/{device_id} -> 204
GET /api/v1/devices/{device_id}/config -> 配置对象
PUT /api/v1/devices/{device_id}/config -> 配置对象（版本由服务端递增）
DELETE /api/v1/devices/{id} -> 原有全局撤销
```

未传 family 时兼容旧客户端默认家庭；传入空值/无效值/无权限值返回 404，不能回退。所有管理写请求保留 Origin/CSRF。GET scope=owned 仅列管理归属范围内设备，不使用其结果绘制当前家庭位置。

配置写请求包含以下五个字段（version 为只读）：

```json
{"moving_interval_seconds":120,"stationary_interval_seconds":300,"moving_high_accuracy":false,"quiet_hours_start":"22:00","quiet_hours_end":"08:00"}
```

响应包含五个字段和整数 version。位置成功响应为 `{"accepted":true,"config":{...}}`，重复上报 false 也携带配置。秒数均为 60～3600，整分钟，静止不小于移动；暂停时间合法 HH:mm 且不同，按设备本地时区的每日单个 [start,end) 时段。

## 任务 1：后端与数据迁移

- [x] 在 `backend/internal/storage/family_migration_test.go` 和 `backend/internal/httpapi/families_test.go` 写失败测试：旧库迁移、跨账号拒绝、成员复用/移出/撤销、配置校验和上下游默认值。
- [x] 运行 `make test`，确认缺失表/路由/响应导致失败。
- [x] 在 `storage/sqlite.go` 增加 v4；在 `auth/families.go`、`auth/settings.go` 中实现授权和配置服务；现有 `auth/devices.go` 与 `credentials.go` 维护新关系。
- [x] 在 `httpapi/families.go` 注册以上路由，调整默认/显式家庭选择；位置上传通过同一事务读取配置，查询和租约按 `max(420, stationary+120)` 判断。
- [x] 运行 `make test vet test-race`；更新 smoke 中 schema 预期与上传响应断言，保留真实隔离数据库验证。

## 任务 2：Android 客户端

- [x] 新增 `core/DeviceConfig.kt` 和对应测试；先验证默认、非法输入、版本及分钟级暂停边界，再实现。
- [x] 扩展 `DeviceApi` 上传结果兼容旧服务端和无效配置；新增实际 HTTP 响应解析测试。
- [x] 调整 `MotionPolicy`、`QuietHours`，独立表达运动状态、间隔、精度和时段；覆盖相同间隔、同日/跨夜、时区变化和窗口划分。
- [x] `CredentialStore` 保存当前设备配置，`SharingController/Service/Schedule` 接入版本与共享代次保护、暂停生效、重排边界；`ScheduledPositionSource/AmapSource` 接入独立精度参数。
- [x] 页面展示当前有效配置，更新隐私文案；版本为 0.6.0/code9。
- [x] 运行 `make android-test android-smoke android-release` 和 Release lint；复制 `bin/family-location-0.6.0-release.apk`，验证签名及摘要。保留旧 APK，不安装设备。

## 任务 3：H5 管理

- [x] 在 现有 `scripts/web_dom_test.mjs` 增加 DOM 回归：链接过滤、家庭切换迟到请求、配置表单、错误保存、成员移除和撤销的区分。
- [x] 调整 `assets/index.html/app.mjs/styles.css`，拆分为 `management.mjs`；实现创建/选择家庭、复制链接、添加已配对设备和配置页面。
- [x] `state.mjs` 按每台设备 stale_after_seconds 判断；家庭切换立即清空位置并使旧异步结果失效。
- [x] 运行 `make web-test web-smoke`，增加真实 HTTP 联调覆盖两个家庭共用设备、不同配置及暂停字段往返。

## 任务 4：集成、审阅与交付

- [x] 逐项核对设计，安排独立需求审阅，再进行代码质量审阅；修复实际发现并补回归。
- [x] 执行必要的完整回归和最终 APK 构建校验，记录实际检查结果和真机限制。
- [x] 同步 `AUTH_API.md/LOCATION_API.md/WEB_UI.md/ANDROID.md/DEPLOYMENT.md/PROJECT_CONTEXT.md/PROGRESS.md`。
- [x] 交付启动命令 `make run`、签名 APK 路径、验证结果与后续服务器更新/设备安装验收事项。

汪汪
