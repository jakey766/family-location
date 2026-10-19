# 第二步：管理员认证与设备配对

依据 [项目上下文](../../PROJECT_CONTEXT.md) 和整体设计实现第二步。2026-10-12 用户已确认 backend 本地运行验证通过，并要求继续下一步。沿用 `backend/`；不修改用户现有数据库做测试，不执行 git add/commit。

## 实现决策

采用已有设计中的数据库会话，便于立即注销；不采用需要额外撤销列表的 JWT。管理员通过同一二进制的 `init-admin` 子命令创建，无公开注册和默认密码。用户名规范化为小写 ASCII（3–64 字符，字母数字、点、下划线、连字符），密码 12–128 字节；命令行隐藏输入，也支持显式 `--password-stdin`，密码不能作为参数传递。每次初始化创建一个家庭和其管理员，重复用户名返回错误，不覆盖账户。

密码使用 Argon2id 与随机盐，参数 64 MiB、time=1、threads=4、输出 32 字节。登录限制同时应用到账号和直连 IP，10 分钟窗口各 5 次；错误密码和未知用户名统一响应。并发密码计算最多 2 个，繁忙时拒绝，防止内存无界增长。

网页会话 32 字节随机令牌，仅数据库摘要保存，24 小时过期；Cookie 为 `family_session`，HttpOnly、SameSite=Strict、Path=/，直连 HTTP 下不设置 Secure，TLS 连接下设置 Secure。不信任 X-Forwarded-*；本步支持直接访问，反向代理配置后续单独处理。登录返回 CSRF token，GET 当前用户可恢复该 token；认证后的状态修改必须同时具有正确 Origin、会话和 X-CSRF-Token。登录必须提交同源 Origin 及 JSON，避免登录 CSRF。

Origin 必须与请求实际 HTTP/TLS 协议及 Host 一致，拒绝缺失、null、多值和跨源 Origin；管理员接口不提供跨域授权。原生设备配对接口不使用 Cookie，不要求 Origin；设备身份只从 Bearer 凭证解析，不能调用管理员接口。

## 配对与撤销

管理员生成的配对信息包含12 位随机字母/数字 `pairing_id` 和 8 位数字 `code`，10 分钟有效。安卓随后同时提交两者；标识用于定位待配对记录、限制该记录最多 5 次错误尝试，避免无法将输错的码归属到原配对请求。网页已封装完整 JSON；2026-10-19 安卓复制自动填写与旧标识兼容见[补充设计](2026-10-19-pairing-convenience-design.md)。

每个家庭仅保留一个待使用的有效配对记录，新建会让旧记录失效。设备提交合法名称（1–80 字符）后，事务内校验剩余次数、有效期和码摘要，原子消费并创建设备；多个并发请求只能一个成功。IP 配对请求额外限制为 10 分钟 20 次。失败只返回统一配对失败，不回显码。

设备凭证为独立的 32 字节随机数，仅首次成功配对响应返回原文，数据库只存摘要；查询和列表不返回凭证。客户端若丢失成功响应，需重新生成配对信息。管理员只能列出/撤销本家庭设备，其他家庭设备按不存在处理；撤销后设备凭证立即失效。配对不创建共享会话，也不采集位置。

限流窗口存在 SQLite，重启不会清零；清理过期窗口并限制记录总量。会话和配对创建时清理过期记录，防止持续增长。详细实现按测试固化，不引入 Redis。

## 数据与接口

事务化迁移到版本 2，保留版本 1，新增 families、users、web_sessions、pairing_codes、devices、auth_limits。身份与家庭关联来自数据库，拒绝客户端通过请求体指定 family_id 或管理员身份。过期时间存 UTC 时间戳，返回 RFC3339；测试通过受控时钟验证过期，不实际等待 10 分钟。

| 方法及路径 | 请求或身份 | 成功结果 |
| --- | --- | --- |
| POST /api/v1/auth/login | 同源 Origin；JSON username/password | 200，会话 Cookie、user、csrf_token、expires_at |
| GET /api/v1/auth/me | 会话 Cookie | 200，user、csrf_token、expires_at |
| POST /api/v1/auth/logout | 会话、Origin、CSRF | 204，撤销当前会话、清除 Cookie |
| POST /api/v1/pairing-codes | 管理员会话、Origin、CSRF；JSON `{}` | 201，pairing_id、code、expires_at |
| POST /api/v1/devices/pair | JSON pairing_id/code/name；无管理员会话依赖 | 201，device、device_token |
| GET /api/v1/devices | 管理员会话 | 200，devices 数组（含撤销状态） |
| DELETE /api/v1/devices/{id} | 管理员会话、Origin、CSRF | 204，撤销本家庭设备 |
| GET /api/v1/device/me | Authorization Bearer | 200，device；撤销后 401 |

所有响应 no-store；JSON 请求最大 8 KiB，拒绝未知字段、多段 JSON 和错误 Content-Type。业务错误使用 `{"error":"固定错误码"}`：400 输入错误、401 未认证、403 Origin/CSRF 拒绝、404 不存在、409 用户名冲突、413 请求过大、415 类型错误、429 限流、500 内部故障。健康接口保持现有契约，readyz 校验当前 SchemaVersion。

## 验收与边界

真实数据库测试覆盖 v1 升级、幂等、密码摘要/未知用户、会话过期注销、持久化限流、配对过期/5 次错误/并发一次消费、独立凭证/撤销和跨家庭隔离。HTTP 测试覆盖完整流程、Cookie、Origin/CSRF、请求限制及管理员/设备凭证互用拒绝。烟雾脚本使用项目内临时库，随机生成一次性测试密码且不打印；验证 CLI 初始化及服务器重启后凭证仍有效。

本步不实现位置接口、安卓、网页或远程部署。HTTP/IP 按既定要求保留；传输不加密的已知限制仍适用。文档只描述字段与使用方式，不保存任何真实密码、设备令牌或签名私钥。

参考：[Go Argon2 包](https://pkg.go.dev/golang.org/x/crypto/argon2)、[终端隐藏密码输入](https://pkg.go.dev/golang.org/x/term#ReadPassword)。

汪汪
