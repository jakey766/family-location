# Android 使用、构建与验收

第五步客户端使用 Kotlin + 高德定位 SDK，包名 `com.jk.family`，最低 Android 7.1（API 25），compile/targetSdk 35。最新实际构建、签名与测试结果以 [PROGRESS.md](PROGRESS.md) 为准；构建成功不代表真机定位和后台表现已验收。

## 构建环境与命令

需要 JDK 17 或 21、Android SDK Platform 35、Build Tools 35.0.0。Gradle 8.11.1 Wrapper、AGP 8.9.2、Kotlin 2.1.20、高德 6.4.9 已固定版本，Wrapper 校验下载 SHA-256。生产后端仍使用 Go，不需要 Android 工具。

在项目根目录执行：

```bash
make android-test
make android-build
make android-lint
make android-smoke
```

- `android-test`：核心状态/调度/真实本地 HTTP、服务协调层 JVM 测试，以及 Robolectric 4.14.1 的 API 25/33 页面框架测试；隔离后端用例无环境时跳过。首次页面测试会下载框架依赖，缓存仍在项目内。
- `android-build`：生成 `android/app/build/outputs/apk/debug/app-debug.apk`。
- `android-lint`：检查 Android 权限、API 兼容、资源和组件等，报告在 `android/app/build/reports/`。
- `android-smoke`：启动真实 Go 二进制和临时 SQLite，调用实际 Kotlin `DeviceApi` 配对、启停、上传、重试及乱序拒绝；结束清理临时库。只使用明确标记的模拟设备，不向正式家庭写入模拟位置。

Makefile 通过 `scripts/android.sh` 使用项目内 `.cache/gradle`、`.cache/android-sdk`、`.cache/android-user`、`.cache/tmp`；保留已有 `JAVA_HOME`，未设置时可使用本机 IntelliJ 自带 JDK。更换机器先设置可用 `JAVA_HOME`。不要直接使用默认会写用户主目录的 Gradle/SDK 命令；命令行优先使用 Makefile。

本机本次准备的 SDK 位于 `.cache/android-sdk/`，命令行工具为 `cmdline-tools/19.0/bin/sdkmanager`。新环境可从 [Android 官方工具下载页](https://developer.android.com/studio#command-line-tools-only) 下载对应系统命令行工具，校验官方摘要后解压到项目内 SDK 的 `cmdline-tools/<版本>/`，配置项目内 `ANDROID_USER_HOME` 和 `TMPDIR`，运行 sdkmanager 安装 `platforms;android-35`、`build-tools;35.0.0`、`platform-tools` 并阅读/接受许可证。不要把本机 macOS 工具直接复制到 Linux 使用。

## 高德 Android Key

用户已于 2026-10-18 补齐 Android Key 和发布签名的本地配置，正式 APK 已构建并通过签名/证书一致性检查；真实高德 SDK 鉴权仍待真机验证。构建配置从本地文件读取，不在聊天或文档中记录实际值。

将 `android/app.local.properties.example` 复制为 `android/app.local.properties`，在本地编辑 `AMAP_API_KEY`。该文件被 Git 忽略。Key 类型必须是 **Android 平台 SDK**，注册包名为 `com.jk.family`，SHA-1 必须对应实际安装 APK 的签名证书；不能使用网页 Web JS API Key。详见[高德 Key 配置](https://developer.amap.com/api/android-location-sdk/guide/create-project/get-key/)。

```bash
make android-signing
```

该命令显示证书指纹，不显示签名密码或私钥。debug 默认使用 `.cache/android-user/debug.keystore`，与已有 `secrets/android/debug.jks` 不自动等同。若高德控制台仅绑定已有发布签名，请使用下节正式签名 APK，或为当前 debug 指纹配置调试授权。删除 debug keystore 会更换调试签名，可能需要卸载旧 debug 包并重新配对；正式签名不能如此更换。

缺少 Key 的 debug 包仍可构建和配对，但开启共享会提示配置缺失，不初始化定位 SDK。真实 Key 不在本次自动测试中使用，SDK 鉴权、真实定位和坐标转换需真机检查。

## 固定发布签名

沿用已有 `secrets/android/release.jks`，不重新生成或覆盖。将 `android/signing.properties.example` 复制为 `secrets/android-signing.properties`，在本地配置以下四项：

| 配置项 | 用途 |
| --- | --- |
| `storeFile` | 相对 `android/` 的签名库路径，例如 `../secrets/android/release.jks` |
| `storePassword` | 签名库密码，仅在本地配置 |
| `keyAlias` | 已有发布密钥别名 |
| `keyPassword` | 对应私钥密码，仅在本地配置 |

Properties 文件中的反斜杠按 Java Properties 语法转义；不要给值添加多余引号。目录权限设为 0700，配置文件和签名库为 0600；备份已有签名库及密码，升级始终保持包名和签名一致。

```bash
chmod 700 secrets secrets/android
chmod 600 secrets/android-signing.properties secrets/android/release.jks
make android-release
```

产物为 `android/app/build/outputs/apk/release/app-release.apk`。release 缺少四项签名配置或 Key 时明确失败，不会静默使用 debug 签名。Key 会随 APK 打包，这是高德 SDK 的正常配置方式；签名私钥和签名密码不进入 APK。

Android Studio 如需直接构建，请把 Gradle user home、SDK、Android 用户目录和临时目录也配置到项目内，避免默认写入项目外缓存。命令行当前已统一处理这些路径。

## 安装、配对与共享

1. 后端已初始化管理员时只需 `make run`；手机填写可访问的服务器 HTTP/IP 地址（同一局域网电脑 IP 或公网 IP），不能填写指向手机自身的 `127.0.0.1`。只支持 origin，不能附带路径、查询或 URL 用户密码。
2. 将 APK 传到安卓手机并安装；也可用项目内 `adb install -r <APK路径>` 安装。debug 和 release 的包名相同、签名不同，不能相互覆盖安装；切换签名需先卸载原包，会清除本机配对。日常正式升级保持发布签名。
3. iPhone/电脑打开后端网页，管理员登录后生成一次性配对标识和 8 位码。新标识为 12 位字母/数字。在待配对的安卓手机上复制网页完整 JSON，再打开 App；三个配对字段全空时会自动填入地址、标识、码。也可点击“粘贴配对信息”主动替换，或手工填写；设备名称仍需填写。自动填写不提交配对，请检查地址后点击“配对此手机”。配对后共享关闭；配对失败不自动反复消费配对码。
4. 点击开启，阅读隐私说明并同意，授予定位权限；Android 12+ 可只给近似位置，App 显示精度限制。Android 13 通知权限单独申请，拒绝时仍可在 App 内停止。系统定位总开关也需要打开。
5. 共享通过常驻前台服务运行，网页显示最新原始定位时间、精度和电量。初始移动目标 60 秒；持续静止约 3 分钟后切 180 秒，恢复移动最迟可能等待一个采样周期。
6. App 和通知都提供停止。先停止本地采集、取消上传并清空待传点，再通知后端；联网清理成功会明确提示，失败会说明最后位置仍保留，需网页撤销设备，或联网重新开启后停止。关闭页面不等于停止；已主动开启的共享在服务被系统回收、重新打开 App、手机重启及覆盖升级时尝试恢复。主动停止后不恢复；强行停止后需手动打开 App，但不必重复点击开启。开机恢复受后台定位权限和厂商自启动限制，详见下节。

设备凭证用 Android Keystore AES-GCM 加密后保存在私有目录，禁用云备份和设备迁移备份。解除本机配对先停止，再删本机凭证；还需管理员在网页撤销原设备。401 会停止并提示重新配对，409 会停止并提示重新开启；400 非法时间点被丢弃，校准手机时间后可继续采集。网络失败只保留最新一个点，同点重试不修改采集时间和序号。

## 一分钟上报与便捷配对（0.5.4）

覆盖安装 `bin/family-location-0.5.4-release.apk`（签名与 0.5.3 一致）。新版本默认移动间隔为 60 秒，静止仍为 180 秒；无需解除已有配对。要生成 12 位新标识，需同时更新并重新构建、重启后端（本地 `make run`，服务器遵循 [DEPLOYMENT.md](DEPLOYMENT.md)）。旧标识在原有效期内仍可使用。

自动填写只在 App 配对页面获得焦点、地址/标识/数字码均为空时尝试读取剪贴板，不会覆盖手工输入。主动粘贴可替换这三个字段；无关文本、缺字段和无效地址不会填写。请复制完整 JSON，不能只复制数字码。iPhone 上复制不会自动传到安卓，需先把完整文本发送到待配对手机并在那台手机上复制。系统限制导致无法自动读取时，可点击粘贴按钮或手工输入。

读取时机参考 [Android 剪贴板限制](https://developer.android.com/about/versions/10/privacy/changes#clipboard-data)，系统可能显示读取剪贴板提示。真实厂商系统的自动填写、1 分钟采样与锁屏间隔仍需分别验收。

## 一次开启与重启恢复（0.5.3）

使用 `bin/family-location-0.5.3-release.apk` 覆盖安装，不要先卸载，保留原配对和共享选择。此次只修改 Android；如果此前已更新 0.5.2 对应的后端，不需再次更新后端。

- 曾经开启且未停止：打开 App 自动恢复，无需再次点击“开启共享”；服务尚未运行时显示正在恢复或系统限制提示，可以停止或重试。开机及覆盖升级广播也会尝试恢复。
- 尚未开启、主动停止、撤回隐私/定位权限、解除配对及明确鉴权失效：不会因为打开 App 或重启自动开启。若旧版已经把共享选择清掉，安装新版后需要开启一次。
- vivo X9 / Android 7.1.2：无需额外“始终允许”权限；仍需允许后台运行、后台高耗电和厂商自启动。重启恢复指系统完成开机、用户解锁后投递普通开机广播，不承诺首次解锁前定位。
- Android 10+（含两台 Android 13）：点击“允许重启后恢复定位”，按提示在系统“权限 → 位置信息”选择“始终允许”。先授予普通定位权限，再单独设置后台权限。未授予时不从开机广播启动定位，保留共享选择，打开 App 自动恢复。[Android 官方权限说明](https://developer.android.com/develop/sensors-and-location/location/permissions/background)。

### i 管家自启动列表没有“家庭位置”

0.5.2 没有声明开机广播及开机权限，0.5.3 已补上。但没有取得 vivo X9 系统日志，也未找到能确定该版本列表筛选规则的官方依据，不能认定这是列表缺失的唯一原因，更不能保证补上后必定出现。

覆盖安装新版并打开一次，再关闭并重进 i 管家自启动列表，查找应用显示名“家庭位置”。如果列表仍没有，先在“设置 → 电池”允许后台高耗电，并在最近任务中锁定应用；不要把找不到该条目理解成你操作错误。厂商权限必须在手机上设置，App 的电池优化豁免不会替你开启它。[vivo 官方后台运行说明](https://kefu.vivo.com.cn/robot/imgmsgData/2616a9cd7cd64a5083a264d16e5767da/index_1.html)区分自启动、后台高耗电和任务锁定；具体入口随 Funtouch OS 版本变化。

### 本轮复测

1. 覆盖安装后打开 App，确认已配对、服务恢复、网页原始定位时间更新；若旧版共享意愿已关闭，先开启一次。
2. 允许后台运行及上述 vivo 设置，锁屏至少 30 分钟，在另一台设备观察定位时间是否持续更新；单独记录移动/静止间隔与电量。
3. 若锁屏仍中断，打开 App 应自动恢复；记录中断时通知是否仍存在、重开后的状态原文和系统版本。这能区分服务未运行与服务在但定位/网络失败。
4. 保持共享开启，重启手机并完成系统解锁，不打开 App，查看通知与网页是否继续更新。然后停止共享再重启，确认不会恢复。
5. App 内停止、通知停止、撤回授权、网页撤销分别复测。在线停止应清除服务器位置；离线停止的清理规则保持不变。

本机无 ADB 已连接设备；Robolectric 不能模拟 vivo 杀进程策略或 i 管家列表。当前交付为恢复缺口修复和可安装包，不能称锁屏/开机稳定性已通过真机验收。

## vivo X9 锁屏后台与最后位置（0.5.2）

此节记录 0.5.2 引入的后端配套行为；安装请用上节最新 0.5.3 正式包，使用相同签名可覆盖安装。还需更新并重启后端（本地 `make run`；服务器按 [部署文档](DEPLOYMENT.md) 执行 `make build` 和 systemd 重启），刷新网页。旧服务删除过的点不能恢复，升级后需手机至少成功上报一次。

1. 打开 App，点击“允许后台运行”，允许忽略电池优化。App 会显示当前省电豁免状态。持续共享期间使用有超时保护的局部唤醒锁，可能增加耗电。
2. vivo 的“设置 → 电池”中允许家庭位置后台高耗电，“i 管家”中允许自启动，在最近任务列表锁定应用；入口名称随 Funtouch OS 版本不同。系统豁免不会自动修改厂商开关。
3. 点击“开启共享”，确认出现定位通知和网页位置后锁屏 15–30 分钟。观察原始定位时间是否继续变化；目标移动约 60 秒、静止约 3 分钟，记录实际间隔和电量。
4. 暂时断网/定位失败时，网页保留最后位置并显示延迟；重连后检查新点替换旧点。开启请求暂时失败也会每约 15 秒重试。
5. 测试 App/通知停止、撤回授权、网页撤销。在线停止成功应清除位置；离线停止立即结束本地采集，但远程最后位置需要网页撤销或联网重新开启后停止。

[START_STICKY](https://developer.android.com/reference/android/app/Service) 用于系统回收后的恢复，不保证厂商清理或强行停止后恢复；0.5.3 新增的开机恢复条件见上节。[Android 电池优化说明](https://developer.android.com/training/monitoring-device-state/doze-standby)及[高德后台定位接口](https://lbs.amap.com/api/android-location-sdk/guide/utilities/permision_8)是实现依据；JVM 生命周期测试不能替代 vivo 真机锁屏测试。

## vivo X9 界面兼容处理（0.5.1）

用户反馈 0.5.0 在 vivo X9 / Android 7.1.2 上正常打开、滚动时出现文字残影和黑底，不仅是长截图异常。0.5.1（versionCode 2）对 API 25 主界面使用软件绘制，API 26+ 保留硬件绘制；滚动区域显式绘制不透明背景，状态不变时不再每秒重写文字。Activity 绘制控制依据 [Android 官方说明](https://developer.android.com/develop/ui/views/graphics/hardware-accel)。

页面测试确认旧版在 3 秒内重复写入 12 次未变化文字，修正后为 0；API 25/33 的绘制模式选择、状态更新和滚动布局检查通过。这些测试不能复现 vivo GPU/系统合成器，不代表已证明厂商驱动是根因，也不代表真机已修复。

用最新正式包覆盖安装 0.5.0，无需卸载；包名与发布签名保持一致。请先复测首次打开、上下滚动、输入框/键盘及隐私弹窗，确认不再出现残影，再继续配对和定位验证。

## 三台设备分别验收

下表按设备记录验收状态；vivo 已收到安装后的界面异常反馈，其他功能尚未通过真机验收：

| 场景 | Mi 11 Lite / Android 13 | Redmi Note 13 Pro / Android 13 | vivo X9 / Android 7.1.2 |
| --- | --- | --- | --- |
| 安装、同签名升级、配对、配对后默认不共享 | 待验收 | 待验收 | 0.5.0 已安装打开但界面重影；0.5.1 待复测，配对仍待验收 |
| 拒绝隐私/定位、撤回权限、近似定位 | 待验收 | 待验收 | 待验收（无独立近似授权） |
| 通知拒绝与 App/通知停止 | 待验收 | 待验收 | 待验收（无运行时通知授权） |
| 高德真实鉴权、GCJ-02、原始时间、精度、电量 | 待验收 | 待验收 | 待验收 |
| 移动/静止间隔、锁屏后台与耗电 | 待验收 | 待验收 | 已反馈锁屏后仍断传、重开需手动开启；0.5.3 待复测 |
| 断网最新点、恢复上传、离线停止提示及最后位置保留 | 待验收 | 待验收 | 待验收 |
| 系统回收恢复、主动停止不恢复、重启与网页撤销 | 待验收 | 待验收 | 待验收 |

验证锁屏与省电设置时记录系统版本、测试时长、实际更新间隔和电量变化；只有 vivo 调试通过不能代替两台 Android 13 验收。JVM/Robolectric 测试不等同于设备上的 Android 生命周期、Keystore、GPU 和定位 SDK；lint/构建也不替代权限、触控、通知、后台和硬件测试。网页真实高德与 iPhone Safari 验收、服务器部署状态继续见 [PROGRESS.md](PROGRESS.md)。

汪汪
