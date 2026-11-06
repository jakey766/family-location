# 第二步运行与认证 API

本文描述已实现的管理员认证与设备配对。最新交付状态见 [PROGRESS.md](PROGRESS.md)，设计依据见[第二步设计](superpowers/specs/2026-10-12-auth-pairing-design.md)。H5 已提供[登录及家庭管理网页](WEB_UI.md)，Android 使用见 [ANDROID.md](ANDROID.md)。

## 启动

如果第一步服务仍在运行，先在其终端按 Ctrl+C，再执行以下命令。命令均在项目根目录运行：

```bash
cd /Users/jiangkunpeng/dev/jk_space/family-location
make init-admin
make run
```

`make init-admin` 会依次提示用户名、家庭名称及两次密码，密码输入不回显。用户名允许 3–64 位 ASCII 字母、数字、点、下划线和连字符，统一转小写；密码 12–128 字节（中文字符占多个字节），家庭名称 1–80 字符。没有默认账户、默认密码或公开注册接口。

每个新用户名创建一个独立的默认家庭；登录后可在 H5 创建和管理更多家庭。已有用户名会报错，不会覆盖原账户或重置密码；以后重启只需 `make run`，无需重复初始化。管理员初始化和服务须使用相同的 `FAMILY_DB_PATH`。

也可运行 `./bin/family-location init-admin --username admin --family 我的家庭`（用户名与家庭名称自行替换），密码仍由终端隐藏输入；自动化提供 `--password-stdin` 从标准输入读一行，并必须同时提供 username/family。不要把密码放命令参数、脚本源码、shell 历史或文档中。

启动会自动将原 SQLite v1/v2/v3/v4 事务化升级至 v5，保留原有数据；迁移失败不会记录为已成功。升级后的库不能由仅支持 v1/v2/v3/v4 的旧二进制打开。健康接口仍为 `/healthz`、`/readyz`。

## 登录及请求约定

以下路径相对服务地址，例如本机 `http://127.0.0.1:8080` 或公网 IP 地址。包含 JSON 的请求要求 `Content-Type: application/json`，最大 8 KiB，拒绝未知字段、非对象和多个 JSON 值。

管理员登录必须带 `Origin`，其值是请求实际协议和 Host（包含非默认端口），例如访问本机时为 `http://127.0.0.1:8080`。网页登录后浏览器自动携带 `family_session` Cookie；状态修改还需把登录或当前用户响应中的 `csrf_token` 放入 `X-CSRF-Token`。

会话有效期 24 小时，Cookie 为 HttpOnly、SameSite=Strict；直连 HTTP 不设置 Secure，TLS 请求会设置。当前按直连 IP 服务实现，不信任 X-Forwarded-*，不开放跨域。HTTP 的已知传输限制见[整体设计](superpowers/specs/2026-10-10-family-location-design.md#http-和平台限制)。

| 方法与路径 | 输入与要求 | 成功结果 |
| --- | --- | --- |
| POST `/api/v1/auth/login` | JSON 字段 `username`、`password`；同源 Origin | 200；Set-Cookie；响应字段 `user`、`csrf_token`、`expires_at` |
| GET `/api/v1/auth/me` | 会话 Cookie | 200；当前 user、csrf_token、expires_at，可用于页面刷新后恢复 CSRF token |
| POST `/api/v1/auth/logout` | 会话 Cookie、Origin、X-CSRF-Token；不需要请求体 | 204；清除 Cookie 并撤销当前会话 |
| POST `/api/v1/pairing-codes?family=<家庭标识>` | 管理员会话、Origin、CSRF；JSON 空对象 | 201；`pairing_id`、`code`、`expires_at` |
| POST `/api/v1/devices/pair` | JSON 字段 `pairing_id`、`code`、`name`；无管理员 Cookie 依赖 | 201；`device`、`device_token`，凭证仅在本次响应返回 |
| GET `/api/v1/devices?family=<家庭标识>` | 管理员会话 | 200；`devices` 数组，仅当前家庭成员，包含已撤销设备 |
| DELETE `/api/v1/devices/{id}` | 管理员会话、Origin、CSRF；无请求体 | 204；撤销该管理员管理的设备，所有家庭位置均清除，重复撤销仍为 204 |
| GET `/api/v1/device/me` | `Authorization: Bearer` 携带设备凭证 | 200；`device`；撤销或凭证错误时 401 |

`user` 包含 `id`、`family_id`、`username`。`device` 包含 `id`、`family_id`、`name`、`created_at`、`revoked_at`；未撤销时 revoked_at 为 null。返回时间为 RFC3339。列表和身份查询不返回密码摘要或设备凭证。

## 多家庭与设备管理（schema v5）

家庭公开标识随机生成，仅用于选择家庭，不是访问凭证。管理员在 `/admin` 登录；家庭查看链接为 `/family?key=<public_id>`，使用独立 7 位口令验证。一个管理员可管理多个家庭，设备可加入同管理员管理的多个家庭；查看授权不授予任何管理权限。

设备列表、位置列表和生成配对码支持 `family` 查询参数。不传参数兼容仍有效的原默认家庭，原家庭已删除则返回 404；显式空值、重复参数、格式错误、不存在或无权限的标识返回 404，不回退默认家庭。配对成功将新设备加入指定家庭；加入其他家庭无需再次配对。

| 方法与路径 | 请求与权限 | 成功结果 |
| --- | --- | --- |
| GET `/api/v1/families` | 管理员会话 | 200；`{"families":[{"id":1,"public_id":"示例标识","name":"我的家庭","passcode_set":false}]}` |
| POST `/api/v1/families` | 管理员会话、Origin、CSRF；`{"name":"父母家"}`，名称 1–80 字符，可选 `passcode` 为 7 位数字字符串，省略则自动生成 | 201；新家庭的 `id`、`public_id`、`name`、`passcode_set` 及仅当次返回的 `passcode` |
| GET `/api/v1/devices?scope=owned` | 管理员会话；不能同时传 family | 200；该管理员管理的全部设备，每台额外含 `families` 数组（家庭对象），没有成员关系时为 `[]` |
| PUT `/api/v1/families/{public_id}/devices/{device_id}` | 管理员会话、Origin、CSRF；`{}` | 204；把自己管理的有效设备加入该家庭，重复加入幂等 |
| DELETE `/api/v1/families/{public_id}/devices/{device_id}` | 管理员会话、Origin、CSRF | 204；仅移出当前家庭，重复移出幂等；不停止设备、清除位置或影响其他家庭 |
| GET `/api/v1/devices/{device_id}/config` | 管理员会话，设备管理权限 | 200；[完整配置对象](DEVICE_CONFIG.md) |
| PUT `/api/v1/devices/{device_id}/config` | 管理员会话、Origin、CSRF，五个配置字段 | 200；服务端递增版本后的完整配置对象 |

`device.family_id` 保留为配对时的历史家庭 ID，不代表当前成员关系或管理权限。设备管理关系独立保存；设备移出所有家庭后仍可从 `scope=owned` 查看位置、管理、配置、重新加入或撤销；跨账号添加、配置、撤销返回 404。撤销是全局操作：设备凭证立即失效、所有家庭均无位置，需重新配对。

旧库迁移保留原家庭、账号、配对、设备凭证、共享会话及最后位置；旧的设备管理家庭授权初始化为独立设备管理关系，新增成员关系不扩大管理权限。设备配置默认值见配置文档。

家庭列表对象增加 `passcode_set` 布尔值，不返回口令或哈希。旧库及 CLI 初始化家庭该值为 false，须在管理页首次设置口令后才能分享。

## 家庭编辑与已撤销设备删除

以下写操作均要求管理员 Cookie、同源 Origin、X-CSRF-Token；操作对象须属于管理员权限范围，无权限或不存在返回 404。

| 方法与路径 | 输入与结果 |
| --- | --- |
| PATCH `/api/v1/families/{public_id}` | `name` 为 1–80 字符的名称；200 返回家庭，标识不变，仅改名不撤销查看授权 |
| PUT `/api/v1/families/{public_id}/passcode` | `{}` 自动生成，或 `passcode` 指定严格 7 位 ASCII 数字字符串；200 返回当次 `passcode`，旧查看授权失效 |
| DELETE `/api/v1/families/{public_id}` | 204；删除家庭可见信息、成员关系、配对信息及查看授权，保留设备、配置、共享与位置 |
| DELETE `/api/v1/devices/{id}/purge` | 204；彻底删除已撤销设备及其关联和配置；未撤销返回 409，必须先通过原 DELETE 设备接口撤销 |

口令允许前导零，不能提交数字类型、空字符串、null、空格或非 ASCII 数字。口令用带盐哈希保存，仅创建或重设成功时返回一次，忘记后重新设置；普通查询不回显口令。请求体和响应不得记录到日志或文档。

最后一个家庭也可删除；管理员仍可登录并管理所有设备、重新创建家庭。设备历史 `family_id` 通过内部家庭删除标记保留兼容，但已删除家庭不再公开可用，也不会恢复或复用其标识。彻底删除设备后旧凭证继续无效，再使用须重新配对。

## 家庭只读访问

浏览器页面为 `/family?key=<public_id>`，必须提供单个有效 key，不回退其他家庭。以下接口的 key 为路径参数；口令仅在 POST JSON 中传递，不放入 URL。家庭查看 Cookie 名为 `family_viewer`，独立于管理员 Cookie，HttpOnly、SameSite=Strict，TLS 时 Secure。

| 方法与路径 | 输入与成功结果 |
| --- | --- |
| POST `/api/v1/family/{key}/session` | 同源 Origin，JSON `passcode` 字符串；200 设置浏览器查看 Cookie，返回该家庭 `expires_at` |
| GET `/api/v1/family/{key}/snapshot` | 有效家庭查看 Cookie；200 返回 `family`（名称与公开标识）、精简 `devices`、`locations`、`server_time`、`expires_at` |
| GET `/api/v1/family/{key}/web-config` | 对应家庭查看 Cookie；200 返回地图开关、JS Key、代理入口与刷新间隔，结构与管理员地图配置一致 |
| DELETE `/api/v1/family/{key}/session` | 同源 Origin 与查看 Cookie；204 退出当前家庭，不影响其他家庭或管理员会话 |

每个家庭授权从口令验证成功起固定有效 7×24 小时；浏览、快照轮询或给另一个家庭授权均不延长已有家庭期限。授权保存在 SQLite，重启不会重置；浏览器可同时记住多个家庭，清除 Cookie 或换浏览器需重新验证。改口令、删除家庭后下一次请求即拒绝旧授权；页面按既有轮询周期感知并清屏，不能撤回访问者此前自行保存的内容。

不存在/已删除家庭返回 404，未授权、已过期或已撤销授权返回 401；口令错误返回 401，非法输入返回 400，验证限流返回 429。验证同时检查 IP（30 次/10 分钟）与家庭/IP 组合（5 次/10 分钟），成功和失败都计数，限流写入 SQLite；哈希并发繁忙也可能返回 429。管理员登录不自动授予家庭页查看授权，家庭 Cookie 不具有管理员或设备接口权限。成员移出家庭、设备撤销后，家庭快照不再返回其位置。

地图代理也可接受有效家庭授权，但仍固定高德上游目标，不转发浏览器 Cookie/Authorization。位置状态字段见 [LOCATION_API.md](LOCATION_API.md)。

## 配对、权限与失败处理

- 必须同时提交 `pairing_id` 和 8 位数字 `code`。2026-10-19 起新生成的标识为 12 位随机大写字母/数字（排除 0/O、1/I），旧标识仍兼容，不更改设备凭证长度。code 是字符串，保留前导零；name 为 1–80 字符。网页已封装包含服务器地址的完整 JSON；安卓支持复制后自动填写及主动粘贴，暂不提供扫码 UI。
- 配对信息 10 分钟有效，最多 5 次错误码尝试；每个家庭新建配对信息会使旧信息失效。成功消费一次后无法重用，并发请求也只能一次成功。
- 设备凭证只返回一次，由客户端安全保存。若成功响应丢失，需要管理员重新生成配对信息；不会通过查询接口找回凭证。
- 设备凭证不能登录管理员接口，管理员会话也不能当成设备凭证。无管理权限的家庭和设备不能列出或撤销，访问按 404 处理。
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
| 409 | `conflict`（设备尚未撤销不可彻底删除，或其他业务冲突） |
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
make family-smoke
```

`auth-smoke` 在项目内临时库中生成临时测试账户、会话及设备，只在内存中使用测试凭证，不打印或写入文档；验证 v1→v5、初始化、登录/CSRF、一次性配对、权限隔离、重启、设备撤销和注销，结束回收子进程及临时文件，不操作正式数据库。该脚本在 macOS/Linux 运行，还会使用 PTY 检查密码输入处被 SIGINT/SIGTERM 中断后终端属性正常恢复。

位置后端、网页和安卓客户端代码已实现，实际验证边界见 [PROGRESS.md](PROGRESS.md)；安卓配置和真机验收见 [ANDROID.md](ANDROID.md)。

汪汪
