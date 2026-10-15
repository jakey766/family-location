# 第三步：最新位置后端

沿用已确认的[整体设计](2026-10-10-family-location-design.md)及[项目上下文](../../PROJECT_CONTEXT.md)，本次只实现位置后端，不涉及客户端或公网部署。

## 存储与事务

采用现有 SQLite 单连接池与事务，schema v3 为 devices 增加 `sharing_session_id`（可空）、`last_sequence`（默认 0）、`last_captured_at`（可空，Unix 毫秒）。`latest_locations` 以 device_id 为主键，保存 latitude、longitude、accuracy_m、battery_percent（可空）、captured_at、received_at、lease_expires_at；时间均为 Unix 毫秒，坐标系固定 GCJ-02。序号保留在 devices，过期清理不能清除水位，避免旧包复活。

每次显式开始生成新的随机会话并清除上一会话的位置与水位；配对不会自动开始。停止须携带当前会话 ID，重复停止已关闭的会话成功，迟到的旧停止不能关闭新会话。共享启停、上传均在事务中复核设备未撤销，撤销与删除位置也在同一事务内完成。

不选用单纯客户端时间排序（手机时钟不可靠），也不引入位置历史/队列。上传序号必须为正、最多 JavaScript 安全整数；低序号返回冲突，同序号返回 `accepted:false` 且不修改任何字段、不续租。更高序号还须保证原始定位时间不倒退。重试需保留原始采集时间和序号。

## HTTP 契约

- `POST /api/v1/device/sharing/start`，设备 Bearer，空对象，201 返回 sharing_session_id；请求成功但响应丢失可重新开始，会替换会话，客户端只使用最后一次开始的结果、串行执行启停。
- `POST /api/v1/device/sharing/stop`，设备 Bearer，sharing_session_id，204。
- `PUT /api/v1/device/location`，设备 Bearer，sharing_session_id、sequence、latitude、longitude、coordinate_system="GCJ-02"、accuracy_m、battery_percent（可空或缺省）、captured_at（RFC3339，UTC 语义）；200 返回 accepted。
- `GET /api/v1/locations`，管理员 Cookie，只查询同家庭有效位置；返回 locations 数组、server_time，位置包含 device_id、name、sequence、坐标、精度、电量、captured_at、received_at、lease_expires_at、stale。不返回设备凭证或共享会话 ID。

共用现有 8 KiB JSON 限制、禁止未知字段、no-store。必填数值使用指针识别缺省/null，避免缺坐标被误解为零坐标。错误：401 未认证/撤销，400 非法字段，409 会话错误、低序号或定位时间倒退；内部错误不泄漏 SQL。

## 时间与清理

租约在接受新的序号时设置为服务端当前时间 +7 分钟，查询严格要求租约未过期。原始定位时间距服务端达到 5 分钟即 `stale:true`；断网重传不能改变该时间。接受最多过去 24 小时、未来 2 分钟的采集时间（包含边界）；超出返回 400。过期同序号重试也不延长租约；客户端需采集新点。

启动立即清理一次、后台每分钟删除过期位置，保留会话和序号水位以便网络恢复后上传新点；读取过滤独立于清理。后台任务随服务退出取消并等待结束；清理错误只记录固定消息，不记录位置和令牌。

## 验证与交付

真实临时 SQLite 测试迁移、唯一行、空电量、字段范围、时间边界、乱序/重试、会话切换、跨家庭、停止/撤销竞态、清理后重放与服务重启。真实 HTTP 烟雾脚本在项目内隔离数据库生成明确标记的模拟设备与坐标，默认退出清理；可用 demo 模式保留临时服务供后续网页调试，不连接正式数据库。Go 测试、race、vet、原有烟雾、位置烟雾与 Linux 两架构构建完成后更新进度。

汪汪
