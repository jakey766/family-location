# 第五步 Android 实施计划

> 执行：使用 superpowers:subagent-driven-development 按任务实现与审核；用户已授权继续现有设计，不创建 worktree，不执行 git add/commit。

**目标：** 交付可构建的 Android 客户端与自动验证，记录外部 Key、正式签名和三台真机尚未验收的边界。

**架构：** `android/core` 封装纯 Kotlin 共享会话、最新点和移动策略；`android/app` 提供原生界面、Keystore 凭证、HTTP、高德适配及前台服务。后端沿用既有 API。

**技术栈：** Kotlin 2.1.20、AGP 8.9.2、Gradle 8.11.1、SDK 35/minSdk 25、高德 6.4.9、JUnit 4。

## 1. 构建基础

- [x] 创建 `android/settings.gradle.kts`、根与模块 build 文件、Gradle wrapper；固定仓库与版本。
- [x] `scripts/android.sh` 与 Makefile 统一项目内缓存、临时目录和 SDK 路径；debug 签名位于 `.cache/`，release 配置位于忽略的 `secrets/`。
- [x] 验证 `make android-test` 能运行 JVM 测试，`make android-build` 生成 debug APK；没有配置 release 时构建不得静默使用 debug 签名。

## 2. 核心行为（测试先行）

文件：`android/core/src/main/kotlin/com/jk/family/core/` 与对应 `src/test/`。

- [x] 先测试初始不共享、开始代次、停止后迟到响应失效、最新点替换、相同序号重试、旧确认不清新点；运行测试确认缺少实现失败。
- [x] 实现 `SharingState`、`Fix`、`Upload`，开启/停止/采集/确认均由调用方单线程调用，上传携带固定会话、序号和采集时间。
- [x] 测试 30/180 秒切换、速度/位移恢复移动、时间倒退/无效坐标拒绝，再实现 `MotionPolicy`。
- [x] 测试并实现 origin 校验、无凭证重定向、真实 HTTP JSON 契约与错误码映射。

## 3. Android 接入

文件：`android/app/src/main/AndroidManifest.xml`、`MainActivity.kt`、`SharingService.kt`、`AmapSource.kt`、`CredentialStore.kt` 及资源。

- [x] UI 提供地址/配对/状态/隐私说明、授权、主动开启、停止、解除本机配对；首次配对和重建不启动共享。
- [x] AES-GCM Keystore 保存凭证，禁用备份；不记录令牌或坐标日志。
- [x] 前台服务先通知再异步 start，串行 HTTP；停止立即销毁定位并使回调失效，旧 start 完成只清理。
- [x] 高德隐私同意在 SDK 初始化前，GCJ-02 与原始定位时间/电量映射、位置权限失败停止。
- [x] 构建与 Android lint 检查 API 25 路径、权限、组件导出、资源及 SDK 调用。

## 4. 联调及文档

- [x] 新增隔离后端的 Android HTTP 契约烟雾检查，模拟位置只能写临时数据库。
- [x] 执行 `make android-test android-build android-lint` 与相关后端回归；对结果进行需求和代码质量独立审核，修复阻塞项。
- [x] 新增 `docs/ANDROID.md`，更新 README、上下文、设计与 PROGRESS，记录启动、Key/签名配置及三个独立设备验收表。
- [x] 保存实际输出与 APK 路径；没有真机/真实 Key/正式签名的部分保留为未验证。

执行结果：代码、Debug APK、测试、lint 和隔离后端联调已完成，详见 [PROGRESS.md](../../PROGRESS.md)。用户已补齐 Android Key 与已有发布签名配置，正式 APK 构建与证书比对通过；三台真机验收未完成。用户随后报告 vivo X9 界面残影；0.5.1 兼容包及新增 6 项页面框架测试已完成，实际效果待用户复测，记录见 PROGRESS。

汪汪
