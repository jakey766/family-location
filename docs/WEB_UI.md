# 网页使用、地图配置与验收

第四步已实现同源网页，包含登录/退出、家人位置列表、设备配对/撤销、高德地图适配及降级。当前未配置真实高德 Key，未完成 iPhone Safari 真机验收；实际检查结果见 [PROGRESS.md](PROGRESS.md)。安卓 App 尚未实现，因此当前先用隔离模拟设备验证页面。

## 启动和访问

在项目根目录停止旧服务，再运行：

```bash
make run
```

本机打开 `http://127.0.0.1:8080/`，使用已初始化的管理员账号登录；不用重复初始化管理员。Go 二进制已内嵌网页，不需要启动 Node 或另一个前端服务，修改网页源码后重新运行 `make run` 即重新构建。网页与 API 同源，保留已有 Cookie、CSRF 和家庭隔离规则。

iPhone 与开发机器处于可互通网络时，可在 Safari 打开 `http://开发机器局域网IP:8080/`；iPhone 上的 `127.0.0.1` 指向 iPhone 自身。默认后端监听 `:8080`，仍需本机防火墙允许访问。公网部署按第六步执行，当前没有部署结果。

不影响正式数据的演示：

```bash
make location-demo
```

终端显示独立 localhost 地址与临时管理员信息，浏览器打开该地址登录即可看到两台明确标记的模拟设备。每 30 秒更新一次；Ctrl+C 结束时清理服务和临时库。此工具仅监听 localhost，不能直接用于 iPhone 跨设备访问；没有对正式 `data/` 数据库写入虚构家人位置。临时密码只在终端展示，不复制到文档。

## 页面行为

- 登录后，网页每 10 秒刷新设备与最新位置；“刷新”可手动更新。
- 家人卡片显示原始定位时间、服务端同步时间、精度、电量（缺失显示“未知”）及坐标。定位达到 5 分钟显示“位置已过期”；接受新点后 7 分钟租约到期，卡片和地图标记移除。
- 已配对但无有效位置的设备仍可在“设备管理”查看。网页无法区分未开启共享、离线或租约过期的全部原因，因此不会把“暂无位置”武断显示为已停止共享。
- 设备名称始终按普通文字渲染。地图显示每台设备一个最新标记及精度圆，点击卡片可聚焦，支持查看全部；不显示历史轨迹，不请求 iPhone 定位权限。
- 地图缺配置、加载失败或超时，保留文字位置列表。图层 SDK 替身测试不是实际高德地图验收。
- 网络错误会隐藏尚不能确认的坐标，提示连接状态；恢复后重新请求。页面进入后台/离开时清除位置和配对信息，恢复时重新验证会话。显示时间按服务端时间加经过时间计算，避免手机时钟偏差伪装新鲜位置。
- 退出立即清理位置展示，后端成功撤销会话后返回登录页；网络失败会说明退出未完成，并提供“重试退出”。

## 配对与撤销

进入“设备管理”生成一次性配对信息，页面显示 8 位数字码和完整 JSON，包含 `server_url`、`pairing_id`、`code`。必须把完整信息提供给准备配对的家人；只有数字码不够。安卓阶段会接入这个契约。

新建会使同家庭旧配对信息失效；10 分钟过期、一次使用、最多 5 次错误尝试。切走管理页、页面进入后台或退出登录会清除页面上的信息，不能据此认为服务器已撤销未过期配对码；若需要作废旧信息，可生成新信息。刷新页面不会恢复原码。

HTTP 下浏览器可能不允许剪贴板接口：点击复制失败时会选中文本并提示手动复制，仍可长按文本操作。配对成功不会自动开启共享，由家人以后在安卓手机上主动操作。

撤销设备须确认；成功后立即删除位置并吊销设备凭证。已撤销设备保留在管理列表，需重新配对才能继续使用。

## 高德地图配置（尚未申请时可跳过）

2026-10-15 用户确认暂未申请 Key，当前默认地图禁用。申请时选择 **Web 端（JS API）**，需要 JS Key 和配套安全密钥 jscode；Android Key、Web 服务 Key 不可直接当作本网页 JS Key。根据[高德准备说明](https://lbs.amap.com/api/javascript-api-v2/prerequisites)与[安全密钥文档](https://lbs.amap.com/api/javascript-api-v2/guide/abc/jscode)，新申请的 JS Key 需要配套安全密钥。

| 环境变量 | 用途 |
| --- | --- |
| `FAMILY_AMAP_JS_KEY` | Web JS API Key，登录后下发浏览器用于加载 SDK |
| `FAMILY_AMAP_SECURITY_JSCODE` | 安全密钥，仅在服务端追加到固定高德代理请求 |

两项都不设置时，其他网页功能可正常使用。只设置一项、包含空格或非法字符时启动失败，错误不会打印值。密钥不会写入配置接口、HTML、日志或文档；JS Key 本身会出现在已登录浏览器的 SDK 请求中。

本机 zsh 可通过隐藏输入设置当前终端环境，避免把实际值写入命令历史：

```zsh
read -rs 'FAMILY_AMAP_JS_KEY?请输入高德 Web JS API Key: '
print
read -rs 'FAMILY_AMAP_SECURITY_JSCODE?请输入配套安全密钥: '
print
export FAMILY_AMAP_JS_KEY FAMILY_AMAP_SECURITY_JSCODE
make run
```

退出当前 shell 后需重新设置；服务不会自动加载 `.env`。长期服务配置在部署阶段完成，密钥保存在受限、忽略版本控制的位置，文档中不要记录其值。

实现使用高德 JS API 2.0 和 `window._AMapSecurityConfig.serviceHost`，安全密钥通过 Go 的 `/_AMapService/` 代理追加，符合[官方代理方案](https://lbs.amap.com/api/javascript-api-v2/guide/abc/jscode)。代理要求管理员登录，只访问固定高德 HTTPS 目标，不转发会话 Cookie/Authorization；配置接口 `GET /api/v1/web-config` 也需要管理员会话。

配置后重新启动并刷新网页，在实际访问来源验证 Key 权限、域名白名单与网络连通性。如果提示地图不可用，先核对 Key 平台、配套密钥、可用配额及访问来源；域名配置问题参考[高德 INVALID_USER_DOMAIN 说明](https://lbs.amap.com/faq/js-api/map-js-api/create-project/46515)。真实 Key、白名单、地图瓦片与手机网络尚未验证，不能根据 SDK 替身测试认定这些已可用。

## 自动化验证

```bash
make web-test
make web-smoke
make test
make test-race
make vet
```

网页测试需要 Node.js 22.13+（22 系列）或 24+ 和 npm；生产运行只需 Go 二进制。`web-test` 使用 Node 内置测试，覆盖时间边界和 SDK 替身。`web-smoke` 根据 `scripts/web-test-deps/` 锁文件安装测试依赖到 `.cache/web-test/`，使用真实临时后端、Cookie/CSRF 和 jsdom 验证交互、状态、网络与生命周期竞态。Node VM modules 的 ExperimentalWarning 是此测试运行方式的提示，不是产品运行依赖。

**jsdom 不渲染页面，不代替浏览器兼容或移动端外观验收。** 当前实际浏览器仅通过可用工具打开了桌面登录页并检查无横向溢出；命令行 Chrome/WebKit 在导航前异常退出，交互/尺寸/截图工具因当前审批策略不可用。

已提供可在允许启动浏览器的开发环境运行的补充脚本：

```bash
make web-browser-install
make web-browser-test
```

使用固定版本 Playwright 下载 Chromium/WebKit 到项目内 `.cache/ms-playwright/`，检查 390×844 布局、登录/恢复、列表、配对和退出，截图写 `.cache/web-screenshots/`。脚本已提供，当前环境未跑通，不能计为通过。Playwright 桌面 WebKit 也不等同于真实 iPhone Safari。

## 待手动验收

1. iPhone Safari：使用真实管理员登录，检查窄屏、横屏、安全区、键盘与触控、复制降级、退出、切后台/回来。
2. 配置实际高德 Key 后：地图完成加载，多设备点与精度圈正确，选择家人/查看全部可用，停止/撤销/过期移除覆盖物；保留高德版权标识。
3. 安卓完成后：用真实采集点核对原始定位时间、精度、电量、GCJ-02 对齐、离线恢复与停止；模拟结果不替代真机验收。

汪汪
