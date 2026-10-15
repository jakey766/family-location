# 第二步运行与认证 API

本文描述已实现的管理员认证与设备配对。最新交付状态见 [PROGRESS.md](PROGRESS.md)，设计依据见[第二步设计](superpowers/specs/2026-10-12-auth-pairing-design.md)。第四步已提供[登录及管理网页](WEB_UI.md)，安卓 App 尚未实现。

## 启动

如果第一步服务仍在运行，先在其终端按 Ctrl+C，再执行以下命令。命令均在项目根目录运行：

```bash
cd /Users/jiangkunpeng/dev/jk_space/family-location
make init-admin
make run
```

`make init-admin` 会依次提示用户名、家庭名称及两次密码，密码输入不回显。用户名允许 3–64 位 ASCII 字母、数字、点、下划线和连字符，统一转小写；密码 12–128 字节（中文字符占多个字节），家庭名称 1–80 字符。没有默认账户、默认密码或公开注册接口。

每个新用户名创建一个独立家庭。已有用户名会报错，不会覆盖原账户或重置密码；以后重启只需 `make run`，无需重复初始化。管理员初始化和服务须使用相同的 `FAMILY_DB_PATH`。

也可运行 `./bin/family-location init-admin --username admin --family 我的家庭`（用户名与家庭名称自行替换），密码仍由终端隐藏输入；自动化提供 `--password-stdin` 从标准输入读一行，并必须同时提供 username/family。不要把密码放命令参数、脚本源码、shell 历史或文档中。

启动会自动将原 SQLite v1/v2 事务化升级至 v3，保留原有数据；迁移失败不会记录为已成功。升级后的库不能由仅支持 v1/v2 的旧二进制打开。健康接口仍为 `/healthz`、`/readyz`。

## 登录及请求约定

以下路径相对服务地址，例如本机 `http://127.0.0.1:8080` 或公网 IP 地址。包含 JSON 的请求要求 `Content-Type: application/json`，最大 8 KiB，拒绝未知字段、非对象和多个 JSON 值。

管理员登录必须带 `Origin`，其值是请求实际协议和 Host（包含非默认端口），例如访问本机时为 `http://127.0.0.1:8080`。网页登录后浏览器自动携带 `family_session` Cookie；状态修改还需把登录或当前用户响应中的 `csrf_token` 放入 `X-CSRF-Token`。

会话有效期 24 小时，Cookie 为 HttpOnly、SameSite=Strict；直连 HTTP 不设置 Secure，TLS 请求会设置。当前按直连 IP 服务实现，不信任 X-Forwarded-*，不开放跨域。HTTP 的已知传输限制见[整体设计](superpowers/specs/2026-10-10-family-location-design.md#http-和平台限制)。

| 方法与路径 | 输入与要求 | 成功结果 |
| --- | --- | --- |
| POST `/api/v1/auth/login` | JSON 字段 `username`、`password`；同源 Origin | 200；Set-Cookie；响应字段 `user`、`csrf_token`、`expires_at` |
| GET `/api/v1/auth/me` | 会话 Cookie | 200；当前 user、csrf_token、expires_at，可用于页面刷新后恢复 CSRF token |
| POST `/api/v1/auth/logout` | 会话 Cookie、Origin、X-CSRF-Token；不需要请求体 | 204；清除 Cookie 并撤销当前会话 |
| POST `/api/v1/pairing-codes` | 管理员会话、Origin、CSRF；JSON 空对象 | 201；`pairing_id`、`code`、`expires_at` |
| POST `/api/v1/devices/pair` | JSON 字段 `pairing_id`、`code`、`name`；无管理员 Cookie 依赖 | 201；`device`、`device_token`，凭证仅在本次响应返回 |
| GET `/api/v1/devices` | 管理员会话 | 200；`devices` 数组，仅本家庭，包含已撤销设备 |
| DELETE `/api/v1/devices/{id}` | 管理员会话、Origin、CSRF；无请求体 | 204；撤销本家庭设备，重复撤销仍为 204 |
| GET `/api/v1/device/me` | `Authorization: Bearer` 携带设备凭证 | 200；`device`；撤销或凭证错误时 401 |

`user` 包含 `id`、`family_id`、`username`。`device` 包含 `id`、`family_id`、`name`、`created_at`、`revoked_at`；未撤销时 revoked_at 为 null。返回时间为 RFC3339。列表和身份查询不返回密码摘要或设备凭证。

## 配对、权限与失败处理

- 必须同时提交 `pairing_id` 和 8 位数字 `code`。code 是字符串，保留前导零；name 为 1–80 字符。后续客户端将封装这两个字段，本步暂不提供扫码 UI。
- 配对信息 10 分钟有效，最多 5 次错误码尝试；每个家庭新建配对信息会使旧信息失效。成功消费一次后无法重用，并发请求也只能一次成功。
- 设备凭证只返回一次，由客户端安全保存。若成功响应丢失，需要管理员重新生成配对信息；不会通过查询接口找回凭证。
- 设备凭证不能登录管理员接口，管理员会话也不能当成设备凭证。其他家庭的设备不能列出或撤销，撤销访问按 404 处理。
- 设备配对不代表开启位置共享；位置接口已在第三步实现，见 [LOCATION_API.md](LOCATION_API.md)。
- 登录按用户名及直连 IP 分别限流：首次请求起 10 分钟内各最多 5 次，成功与失败均计入；配对按 IP 最多 20 次／10 分钟。限流状态写入 SQLite，重启不清零。429 带 `Retry-After: 600`，哈希并发繁忙也返回 429；客户端应退避。
- 超过 5 次失败的配对记录不会自动恢复，须重新生成；过期会话重新登录。账号错误与密码错误使用相同响应。

错误响应只包含固定 `error` 码，不带 SQL、文件路径或提交的凭证：

| 状态 | error 值 |
| --- | --- |
| 400 | `invalid_json`、`invalid_input`、`invalid_pairing` |
| 401 | `invalid_credentials`、`unauthorized` |
| 403 | `forbidden`（Origin 或 CSRF 校验失败） |
| 404 | `not_found`（撤销目标不属于当前家庭或不存在） |
| 409 | `conflict`（保留的业务冲突映射，CLI 重复用户名也会失败） |
| 413 | `body_too_large` |
| 415 | `unsupported_media_type` |
| 429 | `rate_limited` |
| 500 | `internal_error` |

未知路由及错误 HTTP 方法沿用 Go 路由器的 404/405 响应，正文不是上述业务 JSON。所有响应均禁止缓存。

## 无敏感输出的自动验证

```bash
make test
make test-race
make vet
make smoke
make auth-smoke
```

`auth-smoke` 在项目内临时库中生成临时测试账户、会话及设备，只在内存中使用测试凭证，不打印或写入文档；验证 v1→v3、初始化、登录/CSRF、一次性配对、权限隔离、重启、设备撤销和注销，结束回收子进程及临时文件，不操作正式数据库。该脚本在 macOS/Linux 运行，还会使用 PTY 检查密码输入处被 SIGINT/SIGTERM 中断后终端属性正常恢复。

位置后端和网页代码已实现，实际验证边界见 [PROGRESS.md](PROGRESS.md)，后续继续安卓 App。

汪汪
