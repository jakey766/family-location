# family-location

家庭位置共享，计划采用 Kotlin Android App + 高德定位 SDK + Go + SQLite + iPhone 网页。

**当前已完成第二步：Go HTTP 服务、SQLite、健康检查，以及管理员认证与设备配对。** 已提供登录/注销、一次性配对、独立设备凭证、家庭权限隔离及设备撤销。位置上传、安卓 App 和 iPhone 网页尚未实现，目前不具备共享位置能力。

- [整体设计](docs/superpowers/specs/2026-10-10-family-location-design.md)
- [实施计划与进度](docs/superpowers/plans/2026-10-10-family-location-plan.md)
- [已确认需求与工作约定](docs/PROJECT_CONTEXT.md)
- [真实进度与验证记录](docs/PROGRESS.md)
- [管理员初始化与认证 API](docs/AUTH_API.md)

## 本地启动

需要 Go **1.26.0 或更高版本**、make；烟雾验证额外需要 Python 3（含 sqlite3 模块）。首次构建需要访问 Go 模块仓库。SQLite 驱动为纯 Go，普通构建无需 C 编译器；`test-race` 需要平台支持 CGO 和 C 编译器。

所有命令均在 `family-location/` 项目根目录执行：

```bash
make deps
make init-admin
make run
```

已有第一步服务在运行时先 Ctrl+C 停止。首次使用 `make init-admin` 创建管理员，用户名和家庭名称交互输入，密码隐藏输入并确认；后续重启无需重复初始化。没有默认密码或公开注册接口。现有 v1 数据库会自动迁移至 v2，保留已有数据。详细接口与验证说明见 [认证 API](docs/AUTH_API.md)。

默认监听 `:8080`，数据库为项目内 `data/family-location.db`。在另一个终端验证：

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
| `FAMILY_DB_PATH` | `data/family-location.db` | SQLite 文件路径；Makefile 默认使用项目内绝对路径，直接运行二进制时相对进程工作目录 |

```bash
FAMILY_HTTP_ADDR=127.0.0.1:9090 make run
```

不自动读取 `.env`，通过 shell 或进程管理器设置环境变量。数据库新建父目录权限 0700，数据库文件权限 0600。使用 WAL、外键约束和 5 秒 busy timeout，单连接池服务家庭级负载。当前 schema v2 包含迁移记录、家庭、管理员、会话、配对信息、设备及持久化限流表；高版本数据库拒绝由旧程序启动。

## 健康检查

| 接口 | 成功 | 失败 | 含义 |
| --- | --- | --- | --- |
| `GET /healthz` | 200 | — | 进程可以响应请求，不依赖数据库 |
| `GET /readyz` | 200 | 503 | 1 秒内成功查询 SQLite 迁移版本且等于当前版本 |

两个端点也支持 HEAD，其他方法 405，未知路径 404。均禁止缓存，健康状态无需登录，不暴露文件路径和底层错误。就绪失败返回 `{"status":"unavailable","database":"unavailable"}`。就绪检查不是磁盘容量或完整写能力检测。

## 测试与构建

```bash
make test
make test-race
make vet
make smoke
make auth-smoke
make build
make build-linux GOARCH=amd64
make build-linux GOARCH=arm64
```

`make smoke` 构建并在临时端口启动真实服务，验证 HTTP、SIGTERM、重启持久化、数据库路径错误及端口占用；退出时清理子进程和临时数据，不使用正式 `data/`。

`make auth-smoke` 额外验证管理员 CLI 初始化、v1 升级、登录/CSRF、设备配对及撤销、重启后凭证持续有效；只使用项目内临时数据库，凭证不打印、不保存到文档。

构建产物在 `bin/`，Go 依赖/构建缓存、Go 工作目录和测试临时目录在 `.cache/`，均忽略版本控制。不建议绕过 Makefile 执行 Go 命令，以免写入默认的项目外缓存。运行日志输出到标准输出，需要保存时重定向到项目内 `logs/`。可在没有编译或服务运行时删除 `.cache/` 释放空间；下次构建重新下载依赖。

## 公网服务器运行

先在服务器执行 `uname -m`：`x86_64` 选 amd64，`aarch64` 选 arm64。把对应的 `bin/family-location-linux-*` 上传到服务器的 `family-location/bin/`。预编译二进制在服务器上不需要 Go 或独立 SQLite 服务。

以下在服务器的 `family-location/` 根目录运行，以 amd64 为例：

```bash
chmod +x bin/family-location-linux-amd64
FAMILY_DB_PATH="$PWD/data/family-location.db" ./bin/family-location-linux-amd64 init-admin
FAMILY_HTTP_ADDR=:8080 FAMILY_DB_PATH="$PWD/data/family-location.db" ./bin/family-location-linux-amd64
```

在云安全组和系统防火墙放行所选 TCP 端口，然后访问 `http://服务器IP:8080/readyz`。SQLite 必须放在本机可写目录；首版一个进程使用一个数据库文件。当前没有执行远程部署，systemd 等长期进程管理配置在部署阶段提供。

按需求首版通过 HTTP/IP 访问。HTTP 不加密密码、令牌和坐标，当前已实现的鉴权仍不能消除这个传输限制。Android 明文网络配置及高德 key 在客户端阶段处理，细节见设计文档。服务器上已有管理员时跳过初始化命令；当前未执行远程部署。

汪汪
