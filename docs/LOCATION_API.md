# 位置共享 API（第三步）

已实现 Go + SQLite 位置后端、第四步网页和第五步安卓客户端代码；安卓三台真机尚未验收。启动服务使用 `make run`；已有管理员不需要再次初始化，旧 v1/v2 数据库自动升级至 v3。认证、登录和设备配对见 [AUTH_API.md](AUTH_API.md)，技术边界见[第三步设计](superpowers/specs/2026-10-15-location-design.md)。

## 不影响正式数据的验证

在项目根目录运行：

```bash
make location-smoke
make location-demo
```

`location-smoke` 自动创建项目内临时库和明确标记的模拟设备，验证完成后退出并清理。`location-demo` 在独立 localhost 临时服务中模拟两台设备，终端显示 URL、临时管理员用户名和随机密码，每 30 秒上报；Ctrl+C 停止并清理。两者均不访问正式 `data/` 数据库。模拟设备及模拟坐标只用于接口和后续网页调试，不代表家人真实位置或安卓定位效果。浏览器可打开该 URL 登录查看模拟设备；网页说明见 [WEB_UI.md](WEB_UI.md)。

## 设备接口

设备接口使用 `Authorization: Bearer <设备凭证>`，不能用管理员 Cookie 替代。下列写请求均需 `Content-Type: application/json`；请求体上限 8 KiB，未知字段或错误类型被拒绝。配对成功后共享默认关闭。

| 接口 | 请求体 | 成功响应 |
| --- | --- | --- |
| `POST /api/v1/device/sharing/start` | `{}` | 201，`sharing_session_id` 字符串 |
| `POST /api/v1/device/sharing/stop` | `sharing_session_id` | 204，无响应体 |
| `PUT /api/v1/device/location` | 下表全部必填字段 | 200，`{"accepted":true}` 或 `{"accepted":false}` |

每次 start 生成新会话、删除之前位置、重置序号。客户端必须串行处理启停，使用最后一次成功开始的会话；start 响应丢失时重新开始会替换会话。停止携带当前 ID，重复停止已关闭的会话成功；新会话开启后，旧停止返回 409，不会关闭新会话。

停止时客户端先停止本地定位、取消上传、清空待传点，再通知后端。在线停止成功立即删除位置；离线无法通知时，后端保留最后位置并在过期后标记陈旧；需管理员网页撤销，或设备联网重新开启后停止，以确认远程清除。管理员撤销设备也立即删除位置并禁止其继续上报。

上传字段：

| 字段 | 类型与约束 |
| --- | --- |
| `sharing_session_id` | 当前 start 返回的 ID；不能以此替代设备凭证 |
| `sequence` | 整数，1～9007199254740991，同一会话每采集新点递增，重试保持原值 |
| `latitude` / `longitude` | 有限数值，分别在 [-90,90] / [-180,180]；必填，不能为 null |
| `coordinate_system` | 固定字符串 `GCJ-02` |
| `accuracy_m` | 非负有限数值，单位米；必填，不能为 null |
| `battery_percent` | 0～100 整数，不可获取时传 null 或省略 |
| `captured_at` | 原始定位时间，RFC3339 字符串；可含时区，服务端按 UTC 保存/返回，精度保留至毫秒 |

采集时间允许服务端当前时间前 24 小时至后 2 分钟（含边界），超出返回 400。不要把原始定位时间替换为上传时间；时钟倒退需要客户端纠正时钟后采集新点，不能改写旧点的时间。

同一会话低序号返回 409；相同序号返回 `accepted:false`，忽略请求内容且不改变坐标、接收时间和租约。该重复处理仍要求请求字段合法、采集时间在允许窗口内、会话有效。更高序号要求原始定位时间不早于上一接受点。断网仅保留最新一个点，网络恢复时发送它；不补传轨迹。超时仍保留最后点和序号水位；重试旧点不能修改最后点或刷新新鲜度。

## 管理员查询

`GET /api/v1/locations` 使用登录后的管理员 Cookie；设备 Bearer 不具有查询权限。响应含 `server_time` 和 `locations` 数组，空结果为 `[]`。

每项包含 `device_id`、`name`、`sequence`、`latitude`、`longitude`、`coordinate_system`、`accuracy_m`、`battery_percent`、`captured_at`、`received_at`、`lease_expires_at`、`stale`。不返回设备凭证或共享会话 ID。只能查询登录管理员所属家庭，客户端不传 family_id。

- `captured_at`：定位发生时间，页面应据此显示“多久前定位”。
- `received_at`：后端接受新点时间，只描述同步时间。
- `stale`：原始定位时间距服务端已达到 5 分钟，或上报间隔达到 7 分钟。即使刚恢复网络上传，旧点也会标记为陈旧。
- `lease_expires_at`：接受新序号后 7 分钟；2026-10-19 起仅表示上报中断阈值，到期仍返回最后位置并标为陈旧。保留字段名兼容旧协议。
- 服务启动和之后每分钟仅清理已停止共享或撤销设备的残留位置；活跃会话的最后位置不因超时删除。旧版已经删除的位置无法恢复，需要手机再次上报。

网页后续应轮询查询并依据 `server_time` 计算时间差；不能把上一次响应无限保持为正在共享，也不能用 `received_at` 替代定位新鲜度。

## 错误与数据边界

| HTTP 状态 | error | 含义 |
| --- | --- | --- |
| 400 | `invalid_json` / `invalid_input` | 非法 JSON、缺失/null 必填字段、范围或时间不合法 |
| 401 | `unauthorized` | 未认证、身份类型不符或设备被撤销 |
| 409 | `sharing_session_conflict` | 未开始、已停止或共享会话不是当前会话 |
| 409 | `out_of_order` | 序号倒退，或较高序号的原始定位时间倒退 |
| 413 / 415 | `body_too_large` / `unsupported_media_type` | 请求过大或非 JSON 类型 |
| 500 | `internal_error` | 内部故障；不向客户端暴露 SQL/路径 |

所有响应禁止缓存。每个设备最多一条位置，不保存历史轨迹，应用日志不记录坐标、请求体、密码或令牌。SQLite 底层页/WAL 不保证取证级擦除。首版 HTTP 的传输限制沿用已确认设计。

## 实际验收边界

本阶段自动测试和模拟工具验证服务端逻辑；未验证 iPhone Safari、高德地图、安卓 SDK、三台目标真机、耗电、30/180 秒实际定位间隔或公网端到端链路。第四步网页已使用上述隔离模拟服务和 DOM 自动化验证，真实高德与 iPhone Safari 尚未验收，后续安卓阶段再接入真机。

汪汪
