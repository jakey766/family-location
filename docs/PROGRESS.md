# 项目进度

更新日期：2026-10-18。需求以 [PROJECT_CONTEXT.md](PROJECT_CONTEXT.md) 为准，实施任务见[整体计划](superpowers/plans/2026-10-10-family-location-plan.md)、[第二步计划](superpowers/plans/2026-10-12-auth-pairing-plan.md)、[第三步计划](superpowers/plans/2026-10-15-location-plan.md)、[第四步计划](superpowers/plans/2026-10-15-web-plan.md)及[第五步计划](superpowers/plans/2026-10-18-android-plan.md)。本文件记录事实，不将后续方案视为已交付能力。

## 当前状态

2026-10-18（vivo 界面反馈）：用户已在 vivo X9 / Android 7.1.2 安装打开 0.5.0，提供截图并确认正常打开/滚动也会文字残影、黑底。已生成 0.5.1（versionCode 2）正式兼容包：API 25 主界面软件绘制、滚动视图不透明背景、仅变化时更新文字；Robolectric API 25/33 六项页面测试和原有 28 项测试均通过，Release lint 零问题，签名与已有发布证书一致。便于传输的副本位于 `bin/family-location-0.5.1-release.apk`。截图症状指向绘制兼容问题，但尚未在本机复现 vivo GPU 行为；新包需要用户真机复测，不能记录为界面已验收。

2026-10-18：第五步安卓客户端代码已实现，新增 `android/`（`com.jk.family`，minSdk 25、targetSdk 35），包括配对、主动启停、隐私授权、前台服务、高德适配、30/180 秒策略、离线最新点及 Keystore 凭证。28 项 JVM 测试、Kotlin 客户端与真实隔离后端联调、Debug APK 构建及零问题 Android lint 均通过。用户确认 Android Key 与发布签名已有；发现项目内已有 `secrets/android/release.jks` 和 `debug.jks`，未重新生成或覆盖。用户已补齐两份本地配置；签名配置原文件名 `secrets/signing.properties` 已更正为构建使用的 `secrets/android-signing.properties`，没有改动其配置内容。`make android-release` 已成功生成正式签名 APK，签名校验通过且证书与配置的发布签名库一致；已确认 Key 打包、包名/minSdk/targetSdk 正确、APK 不可调试。真实高德、三台安卓、iPhone 和公网链路仍未验收。

2026-10-17：用户在服务器 `/opt/family-location` 尝试源码构建，下载依赖阶段报 GOPROXY 列表没有有效条目。已核对 Go 源码与代理规则，补充服务器终端的代理修复说明；尚未取得重试成功结果，不代表部署已完成。

2026-10-15：第四步网页代码已实现，Go 服务根路径提供管理员登录、家人位置列表、设备配对/撤销及高德地图适配。用户确认尚未申请 Web JS API Key，当前实现无地图降级和配置说明。已通过 Go、前端状态/SDK 替身、DOM + 真实隔离后端检查；真实高德、浏览器完整交互与 iPhone Safari 真机验收未完成。安卓 App 尚未创建。已有管理员无需重新初始化，包名仍为 `com.jk.family`。

第一步已完成，用户于 2026-10-12 反馈 backend 本地运行验证通过，并要求继续。第二步已实现管理员认证、设备配对、独立设备凭证及权限校验，第三步位置后端也已完成；截至 2026-10-15 交付到第四步，2026-10-18 从此继续第五步，没有重复开始已有阶段。

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

- 第三步 schema v3 增量迁移：设备共享会话与序号/时间水位、每设备唯一的 latest_locations 表和租约索引，保留既有认证数据。
- 新增设备主动开始/停止、位置 PUT、同家庭管理员 GET；启停、上传和撤销在事务内协调，防止旧请求恢复位置。
- 同序号重试不更新位置或续租，低序号/时间倒退拒绝；过期清理后保留水位，旧包不能恢复被清理的点。
- 原始定位与接收时间独立保存，GCJ-02、精度、电量可空；5 分钟 stale，7 分钟租约，时间允许前 24 小时/后 2 分钟。
- 查询强制过滤停止、撤销与租约到期设备；启动及每分钟清理过期位置，退出取消并等待清理任务。
- 新增 [位置 API](LOCATION_API.md)、第三步设计/计划、`make location-smoke` 和 `make location-demo`，使用项目内隔离库和明确标记的模拟设备。

- 第四步在 `backend/internal/webui/assets/` 新增无构建网页，通过 Go embed 内嵌，同一服务提供 `/` 和 `/assets/`；生产不需要 Node 服务。
- 登录恢复/退出、Cookie/CSRF、10 秒串行刷新、多人位置卡片、原始时间/stale/电量/精度/坐标、设备列表、确认撤销、一次性配对及 HTTP 复制降级已实现。
- 网络失败/页面隐藏时清除位置，恢复时重新验证；独立请求代次避免旧响应恢复配对或位置，失败退出可以重试；慢设备接口不延长位置租约的显示时间。
- 高德 JS API 2.0 适配支持每设备最新标记/精度圆、选择/查看全部，地图未配置或失败保留文字列表；仅 SDK 替身验证，未使用真实 Key 联调。
- 可选高德环境配置、管理员 web-config 及固定 HTTPS 目标代理；安全密钥只保留服务端，不转发家庭 Cookie/Authorization，不记录请求密钥。
- 新增 [网页说明](WEB_UI.md)、网页设计/计划、`web-test` / `web-smoke` 与可选浏览器脚本；测试依赖锁定并安装到 `.cache/`。

- 2026-10-15 补充 [服务器部署说明](DEPLOYMENT.md)：用户服务器已有 Go，提供源码上传/本机架构构建、独立正式账号、systemd、项目内配置/日志、更新与排错；此次仅编写文档，没有连接服务器或执行部署。

### 第五步代码（2026-10-18）

- 新增 Android 原生 Kotlin App + JVM core 模块，锁定 Gradle/AGP/Kotlin/高德版本；提供 Wrapper 下载校验、项目内 SDK/缓存/临时目录、debug 签名及正式签名配置入口。
- 配对表单、固定服务器地址、设备名、状态、开启/停止、系统定位设置与解除本机配对；配对默认不共享，无开机广播，服务为 START_NOT_STICKY。
- 明确隐私说明与高德政策链接，用户同意后才构造 SDK；粗略/精确权限、Android 13 通知拒绝路径及 Android 7.1/8+ 前台服务启动分支，通知提供停止动作。
- AES-GCM Android Keystore 加密设备凭证，禁用备份；HTTP origin 校验、设备 Bearer 隔离、禁用 Cookie/重定向/隐式重试、10 秒总超时及响应大小上限。
- 共享代次、最新待发点、原始定位时间与采集序号；停止立即销毁 SDK/使回调失效/清空点，只取消上传，在途 start 串行完成后清理；旧响应不能恢复共享或清除新点。
- 初始 30 秒移动策略，锚点/精度/速度与单调时钟判断静止后 180 秒；仅接收高德 GCJ-02，定位错误不伪造点，电量无法读取可空。离线约 15 秒重试最新点；401/409 停止，400 明确拒绝后允许校准时钟恢复。
- 新增 `make android-test/android-build/android-lint/android-smoke/android-release/android-signing`、[Android 使用与验收](ANDROID.md)、第五步设计/计划，保留三台真机的独立待验收清单。

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

HTTP 层覆盖匿名访问、账号/设备凭证互用拒绝、Origin/CSRF、JSON 类型/字段/大小限制及忽略伪造转发 IP，也验证 HTTP Cookie 与 TLS 请求下 Secure Cookie 的登录/注销属性。TLS 请求测试不代表已部署 HTTPS。2026-10-13 用户反馈已完成 `make init-admin` 等前述操作；完整认证和设备配对流程的手动验收尚未单独确认，不能据此认定客户端或端到端功能已完成。

### 第三步验证（2026-10-15）

| 检查 | 实际结果 |
| --- | --- |
| `make test` | 全包通过；真实 v2→v3 升级/回滚/保留认证数据、位置唯一行、GCJ-02/精度/电量、数值与时间边界、缺失/null 字段、跨家庭及身份分离、序号/时间倒退、旧会话、同序号不续租、租约/stale 边界、过期清理后重放均覆盖 |
| `make test-race` | 全包通过；含 20 路上传与停止/撤销并发、后台清理启动/触发/取消与错误回调 |
| `make vet` | 通过 |
| `make smoke` | 真实进程 HTTP、schema v3 重启持久化、SIGTERM、无效路径和端口占用通过 |
| `make auth-smoke` | 原有认证流程在 v3 上回归通过；v1→v3 数据保留、密码输入终端中断恢复、重启凭证持续有效、撤销/注销通过 |
| `make location-smoke` | 真实 HTTP 验证配对默认不共享、覆盖唯一行、乱序/重试、原始时间/stale、跨家庭、重启保留位置/序号、停止/旧会话、撤销、启动清理和清理后重放；检查日志不含模拟坐标或测试凭证 |
| `location-demo` PTY 验证 | 独立隔离服务每 30 秒更新，已观察第二次上传；SIGINT 和 SIGTERM 均退出、关闭端口、删除临时库 |
| Linux amd64 / arm64 构建 | 两种 `make build-linux` 均成功，`file` 确认为对应架构的静态 ELF；未在服务器运行 |
| 独立评审 | 需求符合性与代码质量两次审核均通过，没有阻塞项 |

测试和模拟仅使用项目内临时数据库，没有写入正式家庭位置。过期边界通过可控时钟/临时库验证，后台周期通过可控 tick 验证；这不代表真实网络离线、移动/静止策略或长时间后台定位已经验收。当前仍未完成安卓、iPhone 或公网联调。

### 第四步验证（2026-10-15）

| 检查 | 实际结果与边界 |
| --- | --- |
| `make test` / `make test-race` / `make vet` | 全包通过，包含静态资源 MIME/HEAD/404、可选配置与非法组合、管理员配置、代理鉴权/固定目标/密钥及请求头隔离/路径穿越/重定向/超时/响应大小 |
| `make web-test` | 7 项 Node 测试通过：服务端时间估算、过期/撤销过滤、原始时间 stale、配对倒计时、地图未配置、SDK 替身多点/精度圆/删除与选择、异步加载失效 |
| `make web-smoke` | jsdom + 真实 HTTP 临时库通过：登录/CSRF、多人/空电量/过期、无地图降级、配对/手动复制、离页迟到响应丢弃、断网恢复/租约到期、恶意名称文字渲染、撤销确认与取消、退出失败/后台切换/重试/服务端会话失效；慢 devices 响应不延长租约 |
| 既有 `smoke` / `auth-smoke` / `location-smoke` | 三类真实进程烟雾回归通过，正式 `data/` 未被测试写入 |
| Linux amd64 / arm64 构建 | 两架构构建通过，包含嵌入式网页；没有服务器运行验证 |
| 浏览器只读检查 | 可用 Chrome 工具成功打开桌面登录页，1200×780 下无横向溢出，标签/表单可见；没有完成登录后的实际浏览器操作 |
| 浏览器自动化限制 | 命令行 Chrome 与 WebKit 在页面导航前 SIGABRT 退出；工具的表单交互、resize、截图要求审批，而当前策略 never 不允许审批，因此未执行。提供可选 `web-browser-test`，未计为通过 |
| 独立评审 | 需求审核发现配对离页/注销恢复两项竞态，补失败用例、修复并复核通过；质量审核发现慢设备响应的租约时间基准问题，补回归并修复，独立复现复核通过 |

DOM 测试不包含真实渲染、触控或 Safari 平台行为。没有截图验收结果，没有真实高德 Key、瓦片、白名单验证；尚未完成第四步的 iPhone 真机验收，不能仅凭代码/自动化测试标记“第四步全部验收”。

### 部署文档核对（2026-10-15）

核对了 Go 版本、Makefile 构建/缓存、配置变量和服务退出行为，检查文档链接与 shell 代码块语法。systemd 示例参考官方文档；当前 macOS 环境没有 systemd，未运行 Linux unit 校验或远程部署。没有因此将第六步部署联调标记为完成。

### 第五步验证（2026-10-18）

| 检查 | 实际结果与边界 |
| --- | --- |
| `make android-test` | 20 项 core + 8 项服务协调层 JVM 测试通过；普通运行时独立后端用例跳过，由下列 smoke 实际执行。覆盖启停代次、迟到 start 清理、旧上传确认、最新点重试、400 时间校准、401 撤销、离线停止提示、移动/静止、真实本地 HTTP/超时/取消/响应大小/重定向及 Cookie 隔离 |
| `make android-build` | Debug APK 构建通过，产物 `android/app/build/outputs/apk/debug/app-debug.apk`；`aapt` 确认包名 `com.jk.family`、versionCode 1、minSdk 25、targetSdk 35 |
| `apksigner verify --verbose` | Debug APK v2 签名校验通过；使用项目内自动创建的调试签名，不等同于已有发布签名 |
| `make android-release`（缺配置检查，历史） | 补齐配置前如预期在 preReleaseBuild 拒绝，明确要求本地发布签名配置 |
| `make android-release`（补齐配置后） | 构建通过；正式包 `android/app/build/outputs/apk/release/app-release.apk`，约 1.4 MiB，versionName 0.5.0、versionCode 1；签名验证及配置证书比对通过，Key 已打包、debuggable 未开启，minSdk 25 / targetSdk 35 |
| `:app:lintRelease` | 正式变体通过，0 errors / 0 warnings；不代表高德或真机已验收 |
| `make android-lint` | 通过，报告 0 errors / 0 warnings；不代表真实系统权限、UI、Keystore 或 SDK 已实测 |
| `make android-smoke` | 实际 Kotlin DeviceApi + Go 临时进程/SQLite：一次性配对、开启、重复上传 false、更新、旧序号 409、停止后 409；管理员查询和数据库确认停止后无位置，正式数据未写入 |
| `make test vet web-test` | 全部 Go 包测试、vet 及 7 项网页状态/地图替身测试通过 |
| 独立需求与质量审核 | core 与 app 均通过；发现的未来采集时间被 400 拒绝后阻断正常点问题已先补失败用例、修复并复核，停止时取消 start 的清理竞态也已补红绿回归 |

测试先观察缺实现失败；停止 start 取消与未来时间恢复两个集成回归均先失败后修复。所有测试为 JVM/本地 HTTP/构建静态检查，未执行 Android 模拟器或三台真实设备。没有高德真实鉴权/定位、锁屏间隔/耗电、真实通知/触控与端到端验收结果；Keystore 与系统生命周期行为也仍需真机验证。正式签名包已使用已有签名库构建并验证，不能据 APK 构建宣称第五步已全部验收。

### vivo X9 界面兼容包验证（2026-10-18，0.5.1）

- 用户报告：0.5.0 已在 vivo X9 安装打开；截图包含文字残影与黑底，进一步确认正常打开/滚动也异常。未记录为安装后功能验收通过。
- 自动复现边界：新增 Robolectric API 25/33 页面测试，原先两个平台在 3 秒内各发生 12 次未变化文字写入；两平台的滚动布局均未发现控件重叠。API 25 使用硬件绘制的原路径也由兼容模式测试捕获。这些结果不能证明 vivo 驱动根因或复现截图像素。
- 修正：MainActivity 在 API 25 改软件绘制，API 26+ 保持硬件绘制；滚动区域有不透明背景；四处状态文字仅变化时写入。versionCode 从 1 升为 2，versionName 从 0.5.0 升为 0.5.1，包名/签名/凭证格式保持一致，可覆盖安装。
- `make android-test`：20 项 core、8 项共享协调、6 项页面测试均通过；普通测试的独立后端 smoke 用例按设计跳过。本次没有修改 HTTP/后端，没有重复进行端到端模拟。
- `make android-release`、`:app:lintRelease`：均通过，Release lint 0 errors / 0 warnings。APK 签名验证和与配置证书比对通过，确认 Key 打包且不可调试，`aapt` 确认 versionCode 2 / versionName 0.5.1 / minSdk 25 / targetSdk 35。
- 正式产物 `android/app/build/outputs/apk/release/app-release.apk`；传输副本 `bin/family-location-0.5.1-release.apk`，SHA-256 为 `9de164bcbf500f940e489d2ebdd855cf8d4acbb5f5d040a53743b6ee7d8d3cc3`。
- 独立定向审核通过，无阻塞项；仍需用户在 vivo X9 复测普通打开、上下滚动、输入框/键盘与隐私弹窗。Robolectric 不等于 vivo GPU 或 Android 13 设备验收，软件绘制下的流畅度也需观察。

## 当前启动与检查

需要 Go 1.26.0 或更高版本及 make；烟雾脚本需要 Python 3。以下命令在本机执行：

```bash
cd /Users/jiangkunpeng/dev/jk_space/family-location
make run
```

先停止仍在运行的旧服务；已有管理员只需 `make run`，启动会自动迁移至 schema v3。首次使用才运行 `make init-admin`，已有用户名初始化会报错且不覆盖账户。`make init-admin` 交互询问用户名、家庭和密码，密码不回显。浏览器打开 `http://127.0.0.1:8080/` 即可登录。网页和高德配置见 [WEB_UI.md](WEB_UI.md)，认证见 [AUTH_API.md](AUTH_API.md)，共享接口见 [LOCATION_API.md](LOCATION_API.md)。

在另一终端检查：

```bash
curl -i http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:8080/readyz
```

正常均返回 200。默认监听 `:8080`，使用项目内 `data/family-location.db`；Ctrl+C 停止。更多环境配置及服务器运行方法见 [README](../README.md)。

## 未完成与未验证

- 位置后端已完成自动验证；用户正式数据库上的手动共享流程尚未确认，没有真实安卓采集位置。
- Kotlin App、高德适配、共享启停和后台策略代码已实现，Debug 和正式签名 APK 均已构建，已有 Android Key/发布签名已配置；真实 SDK/硬件/权限/后台/耗电尚未验收。
- 网页上述功能代码已实现，但高德真实 Key、iPhone Safari 窄屏/触控/后台恢复和完整浏览器交互尚未验收；用户当前未申请 Web JS API Key。
- vivo X9（Android 7.1.2）已收到安装/打开后的界面异常反馈，0.5.1 兼容处理待用户复测；Mi 11 Lite、Redmi Note 13 Pro（均 Android 13）尚未进行本项目真机验证。三台的 30 秒／3 分钟效果、耗电和后台稳定性均尚无实测结论。
- 公网服务器部署、HTTP/IP 端到端联调未执行；HTTPS 按已确认范围留待后续。

## 下一步

第五步安卓代码和正式签名 APK 已交付，先在 vivo X9 覆盖安装 0.5.1 复测界面，再按计划验收其他功能。安装 `android/app/build/outputs/apk/release/app-release.apk`，按 [ANDROID.md](ANDROID.md) 分别验证三台设备，随后进行第六步公网部署联调。网页同时保留待验收事项：用户申请 Web JS API Key 并配置后检查真实地图，使用 iPhone Safari 验证布局、触控、复制与生命周期；不能把这些待验收事项默认为已通过。

后续阶段交付与验收：

| 阶段 | 交付内容 | 验收重点 |
| --- | --- | --- |
| 第三步：位置后端 | 共享启停、最新位置上传/查询、精度/电量、原始定位时间、乱序保护、租约及清理；提供项目内测试数据工具 | 每设备仅一条位置、旧数据不覆盖新数据、停止后不再显示、跨家庭拒绝；无需高德 Key 即可验证接口 |
| 第四步：iPhone 网页 | 登录/注销、生成配对信息、设备列表与撤销、高德多人地图、精度/电量/更新时间/过期状态 | Safari 可实际操作；使用独立测试库或明确标记的测试设备和模拟位置，验证地图及接口，不写入虚构家人位置 |
| 第五步：安卓 App | Kotlin + 高德定位 SDK、配对、主动开启/停止共享、30/180 秒策略、断网仅保留最新点、签名 APK | 三台目标设备分别验收；真实手机位置显示在已完成的网页，vivo 调试不替代 Android 13 验证 |
| 第六步：部署联调 | 公网 HTTP/IP 部署、进程守护、数据权限、完整使用说明 | 安卓到 iPhone 的真实链路、锁屏/断网/重启、停止与撤销；HTTPS 仍留后续 |

本次代码交付到第五步安卓；第四步浏览器/真实地图/iPhone 和第五步三台安卓真机验收仍未完成，第六步尚未执行。构建产物、JVM/DOM 测试与模拟工具不替代真机和公网验收。

汪汪
