# family-location

家庭位置共享，计划采用 Kotlin Android App + 高德定位 SDK + Go + SQLite + iPhone 网页。

**当前已实现第五步安卓客户端代码：配对、主动共享、高德定位接入、前台服务和离线最新点上传。** Debug/正式签名 APK 构建、JVM 测试和隔离后端联调已通过，vivo X9 已反馈界面重影，0.5.1 兼容处理待复测，三台安卓功能验收仍未完成。网页由 Go 服务直接提供，未配置地图时可查看文字列表；真实高德地图、iPhone Safari 及公网端到端验收仍待完成。

- [整体设计](docs/superpowers/specs/2026-10-10-family-location-design.md)
- [实施计划与进度](docs/superpowers/plans/2026-10-10-family-location-plan.md)
- [已确认需求与工作约定](docs/PROJECT_CONTEXT.md)
- [真实进度与验证记录](docs/PROGRESS.md)
- [管理员初始化与认证 API](docs/AUTH_API.md)
- [位置 API 与隔离模拟工具](docs/LOCATION_API.md)
- [网页使用、高德配置与手动验收](docs/WEB_UI.md)
- [Android 构建、Key/签名配置与安装验收](docs/ANDROID.md)
- [Linux 服务器部署（已有 Go 环境）](docs/DEPLOYMENT.md)

下一步：安装正式签名 APK，分别完成三台安卓真机验收，再进行公网部署联调；网页真实地图/iPhone 验收同步补齐。自动检查只使用隔离模拟位置。

## 本地启动

需要 Go **1.26.0 或更高版本**、make；烟雾验证额外需要 Python 3（含 sqlite3 模块）。首次构建需要访问 Go 模块仓库。SQLite 驱动为纯 Go，普通构建无需 C 编译器；`test-race` 需要平台支持 CGO 和 C 编译器。

所有命令均在 `family-location/` 项目根目录执行：

```bash
make deps
make init-admin
make run
```

已有第一步服务在运行时先 Ctrl+C 停止。首次使用 `make init-admin` 创建管理员，用户名和家庭名称交互输入，密码隐藏输入并确认；后续重启无需重复初始化。没有默认密码或公开注册接口。现有 v1/v2/v3 数据库会自动迁移至 v4，保留已有数据。详细接口与验证说明见 [认证 API](docs/AUTH_API.md)。登录 H5 后可创建多个家庭、复制家庭链接，将同一设备加入多个家庭；按设备配置移动/静止间隔、高精度及每日暂停时间，使用方法见 [设备配置](docs/DEVICE_CONFIG.md)。

默认监听 `:8080`，数据库为项目内 `data/family-location.db`。浏览器打开 **http://127.0.0.1:8080/** 即可登录；运行网页不需要 Node 服务。高德尚未配置时显示地图不可用提示，其他功能正常。

在另一个终端验证：

```bash
curl -i http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:8080/readyz
```

成功响应分别为：

```json
{"status":"ok"}
```

```json
{"database":"ok","status":"ok"}
```

按 Ctrl+C 停止；服务等待在途请求，最多 10 秒，然后关闭数据库。SQLite 文件和迁移版本在重启后保留。

## 配置

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `FAMILY_HTTP_ADDR` | `:8080` | 监听地址；端口必须是 1–65535，`:8080` 面向所有网络接口 |
| `FAMILY_AMAP_JS_KEY` | 空 | 可选高德 Web JS API Key，与安全密钥成对设置 |
| `FAMILY_AMAP_SECURITY_JSCODE` | 空 | 可选安全密钥，仅服务端使用；配置方法见 [网页说明](docs/WEB_UI.md) |
| `FAMILY_DB_PATH` | `data/family-location.db` | SQLite 文件路径；Makefile 默认使用项目内绝对路径，直接运行二进制时相对进程工作目录 |

```bash
FAMILY_HTTP_ADDR=127.0.0.1:9090 make run
```

不自动读取 `.env`，通过 shell 或进程管理器设置环境变量。数据库新建父目录权限 0700，数据库文件权限 0600。使用 WAL、外键约束和 5 秒 busy timeout，单连接池服务家庭级负载。当前 schema v4 包含迁移记录、家庭及公开标识、管理员、家庭授权、家庭设备成员关系、设备配置、会话、配对信息、设备、持久化限流及最新位置表；设备表记录共享会话与序号水位；高版本数据库拒绝由旧程序启动。

## 健康检查

| 接口 | 成功 | 失败 | 含义 |
| --- | --- | --- | --- |
| `GET /healthz` | 200 | — | 进程可以响应请求，不依赖数据库 |
| `GET /readyz` | 200 | 503 | 1 秒内成功查询 SQLite 迁移版本且等于当前版本 |

两个端点也支持 HEAD，其他方法 405，未知路径 404。均禁止缓存，健康状态无需登录，不暴露文件路径和底层错误。就绪失败返回 `{"status":"unavailable","database":"unavailable"}`。就绪检查不是磁盘容量或完整写能力检测。

## 测试与构建

安卓需要 JDK 17/21 与项目内 Android SDK 35；本机已准备，其他环境见 [Android 文档](docs/ANDROID.md)。在项目根目录执行 `make android-test android-build android-lint android-smoke`；Debug APK 在 `android/app/build/outputs/apk/debug/`，正式签名配置后运行 `make android-release`。

```bash
make test
make test-race
make vet
make smoke
make auth-smoke
make location-smoke
make web-test
make web-smoke
make build
make build-linux GOARCH=amd64
make build-linux GOARCH=arm64
```

`make smoke` 构建并在临时端口启动真实服务，验证 HTTP、SIGTERM、重启持久化、数据库路径错误及端口占用；退出时清理子进程和临时数据，不使用正式 `data/`。

`make auth-smoke` 额外验证管理员 CLI 初始化、v1 升级、登录/CSRF、设备配对及撤销、重启后凭证持续有效；只使用项目内临时数据库，凭证不打印、不保存到文档。

`make location-smoke` 验证真实进程的共享启停、模拟上传、乱序与重试、超时后保留最后点、重启持久化及停止/撤销清除。`make location-demo` 启动隔离的本地模拟服务，供接口/后续网页调试，Ctrl+C 清理；不会向正式数据库写入模拟位置。详见 [位置 API](docs/LOCATION_API.md)。

`make web-test` 检查前端时间逻辑和地图 SDK 替身；`make web-smoke` 使用 jsdom + 真实隔离后端验证网页交互，需要 Node.js 22.13+（22 系列）或 24+。依赖和 npm 缓存均在 `.cache/`；它不代替真实浏览器/iPhone 验收。可选浏览器检查和当前限制见 [网页说明](docs/WEB_UI.md)。

构建产物在 `bin/`，Go 依赖/构建缓存、Go 工作目录和测试临时目录在 `.cache/`，均忽略版本控制。不建议绕过 Makefile 执行 Go 命令，以免写入默认的项目外缓存。运行日志输出到标准输出，需要保存时重定向到项目内 `logs/`。可在没有编译或服务运行时删除 `.cache/` 释放空间；下次构建重新下载依赖。

## 公网服务器运行

服务器已有 Go 时，按照 [服务器部署说明](docs/DEPLOYMENT.md) 上传完整源码，在服务器项目根目录执行 `make build`，首次初始化服务器管理员，再使用 systemd 长期运行。文档包含权限、环境配置、端口放行、日志、更新与排错。

网页已经内嵌到 Go 二进制，服务器不需要 Node 或单独前端服务。已有服务器数据库不要重新初始化；本机正式账户和 demo 账户不会随源码自动迁移到服务器。部署正式服务不运行 `make location-demo`。

如果服务器不安装 Go，也可使用前面的 `make build-linux GOARCH=amd64` 或 `GOARCH=arm64` 在开发机交叉构建，上传与服务器架构对应的二进制，命名为服务器项目内的 `bin/family-location`，再按部署说明启动。当前仅提供文档和构建产物，没有执行远程部署或 systemd 实机验收。

汪汪
