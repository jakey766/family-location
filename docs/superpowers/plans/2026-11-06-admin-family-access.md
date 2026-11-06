# 管理页与家庭口令访问实施计划

> 执行方式：使用 superpowers:subagent-driven-development 的实现及审阅流程；互不编辑相同文件的后端、H5 任务按 dispatching-parallel-agents 分工。用户已授权直接在当前工作区实现，不创建 worktree，不执行 git add/commit，不增加人工确认节点。

**目标：** 实现 `/admin` 管理所有设备及家庭、`/family?key=...` 七天口令只读访问和已撤销设备彻底删除。

**架构：** 沿用 Go/SQLite 与嵌入式 H5。独立设备管理关系保障家庭删除后管理及上报不受影响；家庭查看授权独立于管理员身份。管理页和家庭页使用独立入口，共享已有地图和位置状态模块。

**实际验证：** 后端/竞态/HTTP smoke/DOM 联调通过，见进度；实际 Chrome 在启动阶段 SIGABRT，浏览器可选检查已尝试但未通过，未声称 Safari 验收。Android 未修改，无 APK 任务。

**技术栈：** Go、modernc SQLite、原生 ES modules、Node/DOM、Python 隔离 HTTP smoke。依据[已确认设计](../specs/2026-11-06-admin-family-access-design.md)。

## 1. 后端数据与权限（独立工作域）

文件：新增 `backend/internal/storage/access_schema.go`、`backend/internal/auth/family_access.go` 及对应测试；修改 `storage/sqlite.go`、`auth/{credentials,devices,families,settings}.go`。

- [x] 先编写迁移与权限失败用例：v4 设备/共享/位置/配置保持；删除默认/最后家庭后登录、上传、配置及所有位置仍可用；跨账号拒绝。
- [x] `make test` 确认新增行为在旧实现失败，并记录实际原因。
- [x] schema v5 增加家庭删除标记、口令哈希/版本、设备管理员关系、浏览器凭证/按家庭授权；迁移前管理家庭授权映射到设备管理员关系。旧/CLI 家庭口令为空，管理页首次设置。
- [x] 创建家庭、配对、成员变更、撤销和配置全部使用独立管理权限；所有可见家庭查询排除删除标记。家庭删除清理分享授权、成员及配对记录，保留设备、历史 ID 与账号。
- [x] 口令严格七位 ASCII 数字字符串；随机生成使用 crypto/rand；复用 Argon2 与哈希并发保护。固定 7 天授权持久化、按家庭版本失效、同浏览器多家庭独立有效期。
- [x] 撤销设备后才允许物理删除，级联清理配置/位置/授权关系；旧令牌不可重新使用。

## 2. 后端 HTTP 与位置（同一后端工作域）

文件：`backend/internal/httpapi/{families,auth,location,web}.go`、新增 `family_access.go`、`backend/internal/location/locations.go` 及对应测试。

- [x] 先补 HTTP 失败用例，覆盖口令登录、只读快照、错误口令、限流、固定七天、改口令撤销、无效 key、scope 冲突、权限隔离、删除与 purge。
- [x] 扩展现有路由：家庭 PATCH/DELETE、PUT passcode、设备 DELETE purge、位置 `scope=owned`，沿用管理员 Origin/CSRF。
- [x] 创建家庭响应仍为家庭对象，额外返回 `passcode`；家庭对象增加 `passcode_set`。PUT passcode 输入 `{}` 随机生成或 `{ "passcode": string }` 手动设置，响应 `{ "passcode": string }`。
- [x] 家庭访问前缀 `/api/v1/family/{key}`，POST session 输入 `{ "passcode": string }`，返回 `{ "expires_at": RFC3339 }`；DELETE session 退出单个家庭。GET snapshot 返回以下结构：

```js
({family: {public_id, name}, devices: [{id, name, revoked_at}], locations: [], server_time, expires_at})
```

`locations` 沿用现有位置字段，`devices` 不包含管理归属、凭证或配置；未撤销成员可展示无位置状态。会话鉴权和快照读取必须在一致事务内校验，防止已撤销授权重新读出位置。

- [x] GET web-config 返回现有地图配置结构；地图代理接受有效管理员或有效家庭授权，依然使用固定上游白名单与无凭证转发。
- [x] `/` 与 `/?family=...` 跳转 `/admin` 并兼容旧家庭选择；`/admin` 加载 index，`/family` 加载 family.html，静态白名单加入新模块。
- [x] `make test vet test-race` 验证后端。已有测试硬编码 schema 或旧页面入口的断言按新契约修改，保留原权限/回滚检查。

## 3. H5 管理与查看（独立工作域）

文件：`backend/internal/webui/assets/{index.html,app.mjs,management.mjs,styles.css}`，新增 `family.html`、`family.mjs`；`scripts/web_dom_test.mjs`、`scripts/web_smoke.py` 与浏览器测试按实际现有文件修改。

- [x] 先新增 DOM 用例并观察默认所有设备/家庭 CRUD/独立口令页面尚未提供的失败。
- [x] 管理选择器始终有“所有设备”，使用 `scope=owned`；指定家庭使用 `family`。无家庭也正常初始化/刷新，配对仍要求选择家庭。
- [x] 家庭编辑、删除确认、首次口令设置与重置、当次口令复制、链接复制；复制路径固定 `/family?key=...`。所有/指定视图一致筛选地图和设备，已撤销设备提供彻底删除确认。
- [x] 独立家庭页面输入口令、恢复 Cookie 授权、固定七天过期返回验证、退出当前家庭；只展示成员状态与地图。共享 map.mjs/state.mjs，不加载管理逻辑。
- [x] 加入请求取消/代次保护、401/404 清屏、切换/后台恢复、断网重试与地图降级；后端不可用不假报授权或保存成功。
- [x] 运行 `make web-test web-smoke`；依据新入口调整既有 smoke，真实 HTTP 联调同时覆盖无管理员的家庭访问。

## 4. 集成、审阅与交付

文件：`docs/{PROJECT_CONTEXT,PROGRESS,AUTH_API,LOCATION_API,WEB_UI,DEPLOYMENT}.md`、`README.md`、必要 smoke 脚本。

- [x] 独立需求审阅：逐条检查已确认设计，修复遗漏；随后代码质量审阅关注跨账号读取、改口令竞态、Cookie、迁移回滚、删除最后家庭及前端迟到响应。
- [x] 隔离库端到端检查：管理员登录→家庭口令→设备跨家庭→所有设备位置→删除家庭→无家庭设备位置→撤销→彻底删除。正式数据库不参与。
- [x] 最终运行 `make test vet test-race smoke auth-smoke location-smoke web-smoke`，可用时运行 `make web-browser-test`，记录真实结果；`git diff --check` 与文档链接检查。
- [x] 更新已实现 API/使用/部署文档、真实进度与本计划勾选。记录启动 `make run`、新入口、schema v5 升级及旧家庭首次口令设置，不将本地验证写成部署/iPhone 验收。
- [x] Android 协议保持兼容且未改代码时无需 APK；若改变 Android 则另行递增、构建和校验签名产物。

汪汪
