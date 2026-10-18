# Linux 服务器部署：在服务器上用 Go 构建

适用于已有 Go 环境的 Linux 服务器。长期运行示例使用 systemd，命令按 Bash 编写；需要具备 sudo 权限。本文使用 `/opt/family-location` 作为服务器项目目录，端口为 `8080`。如果使用其他路径或端口，需要同步修改命令、环境配置和服务文件。

当前可部署 Go 后端和内嵌网页，不需要 Node、Nginx 或独立 SQLite 服务。安卓 App 尚未实现；没有已开启共享的设备时，正式网页的位置列表为空是正常现象。本文是部署操作说明，尚未在用户服务器执行或验证。

## 1. 检查服务器环境

以下在 **服务器终端** 执行：

```bash
go version
make --version
ps -p 1 -o comm=
```

- 当前 `backend/go.mod` 要求 **Go 1.26.0 或更高版本**。Makefile 设置了 `GOTOOLCHAIN=local`，不会自动下载更高版本 Go；版本不足时先升级服务器 Go。
- 需要 make；首次构建需要访问 Go 模块下载地址。直接 `make build` 会下载锁定依赖，不需要先运行会整理 go.mod 的 `make deps`。
- 第三个命令应输出 `systemd`，才能使用后面的 systemd 章节。若为容器或其他 init 系统，先用前台启动方式验证，再接入该环境已有的进程管理器。
- 普通构建禁用 CGO，无需 C 编译器。烟雾检查额外需要 Python 3；生产运行和 Go 构建不需要安装网页测试的 Node/npm 依赖。

## 2. 上传完整项目源码

首次部署，在服务器创建项目目录并交给当前 SSH 登录用户管理：

```bash
sudo install -d -m 0755 -o "$(id -un)" -g "$(id -gn)" /opt/family-location
```

在 **本地开发机器** 的项目根目录执行下面命令，将 `你的SSH用户名` 和 `服务器公网IP` 替换为实际值。两端需要 rsync；也可通过已有的 SFTP 工具上传相同源码。

```bash
rsync -av \
  --exclude='/.git/' \
  --exclude='/.cache/' \
  --exclude='/bin/' \
  --exclude='/data/' \
  --exclude='/logs/' \
  --exclude='/secrets/' \
  --exclude='/.env*' \
  --exclude='/deploy/' \
  --exclude='*.db' \
  --exclude='*.db-shm' \
  --exclude='*.db-wal' \
  ./ 你的SSH用户名@服务器公网IP:/opt/family-location/
```

此方式会上传当前工作区源码，包括尚未提交的网页文件；仅从远程 Git 仓库拉取，可能没有本地尚未提交的实现。不要漏掉根目录 Makefile、`backend/go.mod`、`backend/go.sum`、`backend/internal/webui/assets/`。不要加 `--delete`，避免误删服务器上的运行文件。上面的排除项保留服务器自身的数据库、日志、配置和构建缓存。

本机已初始化的管理员不会随源码上传。服务器使用新的数据库时，需在服务器单独初始化管理员；已有服务器数据库则继续使用，不要覆盖。

## 3. 构建并首次启动

回到 **服务器终端**：

```bash
cd /opt/family-location
make build && chmod 0755 bin bin/family-location
```

产物为 `bin/family-location`，自动匹配服务器 CPU 架构，网页已内嵌到二进制中。这里用 `make build`，无需交叉构建。Go 依赖、构建缓存和临时文件由 Makefile 放在项目 `.cache/` 下。

为正式服务创建专用系统账户及数据/日志目录。下面以新部署为前提；若已存在同名专用账户，先用 `id family-location` 核对并跳过创建：

```bash
sudo useradd --system --user-group --home-dir /opt/family-location \
  --no-create-home --shell /bin/false family-location
sudo install -d -m 0700 -o family-location -g family-location \
  /opt/family-location/data /opt/family-location/logs
```

**仅首次创建服务器管理员时**执行：

```bash
sudo -u family-location env \
  FAMILY_DB_PATH=/opt/family-location/data/family-location.db \
  /opt/family-location/bin/family-location init-admin
```

按提示输入管理员用户名、家庭名称和密码，密码输入不回显。已有管理员时跳过；重复初始化不会重置密码。密码要求见 [认证说明](AUTH_API.md)。请保持此命令的数据库路径与后续服务完全一致。

先在前台验证运行：

```bash
sudo -u family-location env \
  FAMILY_HTTP_ADDR=:8080 \
  FAMILY_DB_PATH=/opt/family-location/data/family-location.db \
  /opt/family-location/bin/family-location
```

另开一个服务器终端检查：

```bash
curl -fsS http://127.0.0.1:8080/healthz
curl -fsS http://127.0.0.1:8080/readyz
curl -I http://127.0.0.1:8080/
```

预期健康接口返回 `{"status":"ok"}`，就绪接口返回包含 `"database":"ok"` 的 JSON，首页返回 HTTP 200。首次启动会创建数据库，已有 v1/v2 数据库自动迁移至当前 v3。

## 4. 放行端口，访问网页

在云服务器控制台的安全组/防火墙中允许所需来源访问 **TCP 8080 入站**；如果系统还启用了 ufw、firewalld 或其他防火墙，也放行同一端口。无需关闭整个防火墙，也无需暴露 SQLite 文件或额外数据库端口。

在你的电脑或 iPhone 浏览器打开：

```text
http://服务器公网IP:8080/
```

登录时使用第 3 节在 **服务器数据库** 中创建的管理员。`127.0.0.1` 只表示当前设备自己，手机上不能用它访问服务器。若更换服务端口，安全组和浏览器地址也要一起修改。

| 运行方式 | 地址 | 登录凭证 |
| --- | --- | --- |
| 正式服务器服务 | `http://服务器公网IP:8080/` | 服务器初始化的管理员 |
| 本地 `make run` | 默认 `http://127.0.0.1:8080/` | 本地正式库的管理员 |
| `make location-demo` | demo 终端打印的随机 localhost 端口 | 本次 demo 打印的临时账号/密码 |

**部署正式服务不运行 `make location-demo`。** demo 是独立临时库，只监听 localhost；其账号不能登录正式服务，关闭后数据删除，重启后地址和密码重新生成。也不需要先 `make run` 再运行 demo。

首版沿用已确认的 HTTP + 公网 IP，未配置 HTTPS，传输不加密。当前服务按直接访问设计；若以后加反向代理/HTTPS，需要同步认证 Origin、Cookie 和可信代理处理，不能直接视为已支持该部署路径。

## 5. 用 systemd 长期运行

前台检查通过后，先在前台运行的服务终端按 **Ctrl+C**，再配置 systemd。不要让两个进程共用同一个数据库文件，也不要同时用 `make run` 和 systemd 启动正式服务。

### 环境文件

下面命令只在首次配置时执行，后续修改用编辑器，不要覆盖已有高德配置：

```bash
sudo install -d -m 0700 /opt/family-location/secrets
sudo sh -c 'umask 077; cat > /opt/family-location/secrets/server.env' <<'ENV'
FAMILY_HTTP_ADDR=:8080
FAMILY_DB_PATH=/opt/family-location/data/family-location.db
ENV
```

该文件只包含环境变量赋值，不写 `export`，数据库路径必须写绝对路径；systemd 不会像 shell 一样展开 `$PWD`。文件由 root 读取后将环境传给服务，无需允许服务账户直接读取此文件。`secrets/` 已被项目 `.gitignore` 忽略，实际密钥不应上传到源码仓库。

没有高德 Key 可以先部署，文字列表和管理功能正常。申请到 **Web 端（JS API）Key 与配套安全密钥**后，通过编辑器在此文件中同时增加 `FAMILY_AMAP_JS_KEY` 和 `FAMILY_AMAP_SECURITY_JSCODE` 两项；实际值只保留在服务器配置中，不写文档。两项必须成对设置，详见 [网页地图配置](WEB_UI.md#高德地图配置尚未申请时可跳过)。

### 服务文件

在服务器项目内创建 unit 文件，systemd 通过链接引用：

```bash
sudo install -d -m 0755 /opt/family-location/deploy
sudo tee /opt/family-location/deploy/family-location.service >/dev/null <<'UNIT'
[Unit]
Description=Family Location API and Web
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=family-location
Group=family-location
WorkingDirectory=/opt/family-location
EnvironmentFile=/opt/family-location/secrets/server.env
ExecStart=/bin/sh -c 'exec /opt/family-location/bin/family-location >> /opt/family-location/logs/server.log 2>&1'
Restart=on-failure
RestartSec=5s
KillSignal=SIGTERM
TimeoutStopSec=20s
UMask=0077
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
UNIT
sudo chmod 0644 /opt/family-location/deploy/family-location.service
sudo systemd-analyze verify /opt/family-location/deploy/family-location.service
sudo systemctl link /opt/family-location/deploy/family-location.service
sudo systemctl daemon-reload
sudo systemctl enable --now family-location.service
```

`ExecStart` 使用 `exec` 保证服务进程接收 SIGTERM，应用自身有 10 秒优雅退出上限，systemd 留出 20 秒。应用日志追加到项目 `logs/server.log`；systemd 自身的启动失败信息仍可通过 journal 查询。`systemctl link/enable` 会创建系统管理所需的链接，源码、实际 unit 文件、配置、数据库和应用日志仍在项目目录。不能把项目放到开机时尚未挂载的临时目录。

这里直接启动构建后的二进制，而不是让 systemd 执行 `make run`。这避免服务启动依赖 Go PATH、网络下载和编译。配置项语义与链接方式依据 [systemd.service](https://github.com/systemd/systemd/blob/main/man/systemd.service.xml)、[systemd.exec](https://github.com/systemd/systemd/blob/main/man/systemd.exec.xml) 和 [systemctl](https://github.com/systemd/systemd/blob/main/man/systemctl.xml) 官方文档。

检查状态、就绪和日志：

```bash
sudo systemctl status family-location.service --no-pager
curl -fsS http://127.0.0.1:8080/readyz
sudo tail -n 100 /opt/family-location/logs/server.log
sudo journalctl -u family-location.service -n 50 --no-pager
```

`active (running)` 只说明进程正在运行，还需要 readyz 成功和浏览器登录验证。断开 SSH 后服务继续运行，开机由 systemd 拉起。常用操作：

```bash
sudo systemctl restart family-location.service
sudo systemctl stop family-location.service
sudo systemctl start family-location.service
```

只改 `server.env`：`restart` 即可生效。改 `.service` 文件：先 `daemon-reload`，再 `restart`。改网页或 Go 源码：重新构建，再启动新二进制。

## 6. 更新版本和保留数据

再次上传源码时，沿用第 2 节排除规则，保留服务器 `data/`、`secrets/`、`logs/`、`deploy/`。然后在服务器执行：

```bash
cd /opt/family-location
sudo systemctl stop family-location.service
make build && chmod 0755 bin bin/family-location
```

**确认构建成功**后再启动并检查：

```bash
sudo systemctl start family-location.service
curl -fsS http://127.0.0.1:8080/readyz
```

不用再次 `init-admin`，也不用删除数据库。停止后更新会有短暂不可用；构建失败时先查看错误，不要把“命令执行过”当成已经发布新版本。大版本迁移前应先制定数据备份/恢复步骤；当前尚未交付自动备份工具，也不提供自动降级。数据库升级后，旧程序可能因为 schema 过新拒绝启动，不能只替换旧二进制就承诺完成回滚。

项目只保存设备最新位置。不要对包含位置的整个 SQLite 文件做长期周期快照，让备份积累成历史轨迹；后续正式备份方案需要排除位置数据。也不要在运行期间只复制 `.db` 主文件当作一致性备份，因为应用使用 WAL。

日志当前追加到单个文件，尚未配置自动轮转。需要手动归档时可停止服务，将 `logs/server.log` 移动为项目 `logs/` 内带时间戳的文件，再启动；不要在运行时移动文件后假设进程会自动重新打开新日志。定期检查磁盘余量。

## 7. 常见问题

| 现象 | 检查与处理 |
| --- | --- |
| `go.mod requires go >= 1.26.0` | 用 `go version` 核对当前 PATH 中的 Go；项目不自动升级工具链 |
| 模块下载失败 | 检查服务器 DNS、外网、所配置的 Go 模块代理和校验服务；无需更换 SQLite 数据库 |
| 本机 readyz 成功，公网打不开 | 核对监听地址是否为 `:8080`、公网 IP、安全组、系统防火墙；不要用 `127.0.0.1:8080` 作为公网监听配置 |
| `address already in use` | 用 `sudo ss -ltnp 'sport = :8080'` 找到占用者；停止你自己之前的前台服务，避免随意终止其他进程 |
| `permission denied` / SQLite 无法创建 WAL | 检查项目路径可遍历、二进制可执行，`data/` 和其中已有文件由 `family-location` 用户可读写；数据库目录也必须可写，不只是 `.db` 文件 |
| systemd `203/EXEC` / `200/CHDIR` | 检查 `ExecStart`、`WorkingDirectory` 的绝对路径、权限及文件存在；不要依赖 shell 的 `cd` 或 Go PATH |
| 密码错误 | 确认当前是服务器正式页面，并使用服务器数据库中的管理员；本机管理员/demo 凭证不会自动出现在服务器 |
| 登录提示操作频繁 | 当前登录有 10 分钟窗口限流，等待后再试；不要反复初始化账户或删除正式数据库来绕过 |
| 登录成功但没有家人位置 | 当前尚无安卓真实上报；配对不自动共享，停止/过期的设备也不会显示坐标 |
| 地图尚未配置 | 可正常使用管理和文字列表；在服务器环境文件中补齐两项高德配置并重启 |
| 修改源码后页面没变化 | 网页在编译时嵌入，须 `make build` 并重启；确认浏览器访问的端口确实属于更新后的实例 |

### GOPROXY 非空但没有有效条目

若构建报 `GOPROXY list is not the empty string, but contains no entries`，表示服务器实际生效的 GOPROXY 配置被解析为空列表，例如值只有逗号或空白。项目 Makefile 没有设置 GOPROXY，会继承当前服务器环境及 Go 用户配置；具体来源尚需在服务器核对。

在**出现错误的服务器终端**执行，覆盖当前 shell 的配置后重试：

```bash
cd /opt/family-location
export GOPROXY=https://proxy.golang.org,direct
make build && chmod 0755 bin bin/family-location
```

如果官方代理连接超时，可按服务器网络条件选择 [Goproxy.cn](https://goproxy.cn/) 提供的代理，然后重新构建：

```bash
export GOPROXY=https://goproxy.cn,direct
make build && chmod 0755 bin bin/family-location
```

以上 export 仅对当前终端及其子进程生效。确认可下载后，如需新终端也生效，可在服务器修正 shell 启动文件中原有的错误 GOPROXY 设置，或清除该环境变量后使用 `go env -w` 保存选定代理。shell 环境变量优先于 Go 用户配置，仅执行 `go env -w` 不一定能覆盖现有错误的 export。代理格式和配置规则见 [Go 模块参考](https://go.dev/ref/mod#environment-variables)。该错误不需要删除数据库、修改依赖版本或关闭校验和验证。

## 部署验收记录

部署后应分别确认：服务器 Go 构建成功、systemd 状态正常、本机 readyz、外网网页登录、重启后管理员/设备保留、停止/重启服务与日志。高德真实地图、iPhone Safari 和安卓真机链路仍按 [项目进度](PROGRESS.md) 单独验收。

本文编写时仅核对现有代码/Makefile、shell 示例语法及文档链接。当前开发环境是 macOS，没有执行服务器命令，也没有完成 Linux systemd 实机验证。

汪汪
