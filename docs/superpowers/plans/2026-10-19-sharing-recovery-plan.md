# 共享恢复修复计划

依据：[恢复设计](../specs/2026-10-19-sharing-recovery-design.md)。在当前工作区执行，不执行 git add/commit。

- [x] 在 `MainActivityTest.kt` 增加已有意愿重开和停止后不恢复测试；在 `SharingServiceTest.kt` 验证恢复命令需意愿及重复恢复幂等；新增 `SharingRecoveryReceiverTest.kt` 覆盖真实 Manifest 的开机/升级接收器与 API 25/33 权限分支。运行 `make android-test` 确认旧代码失败。
- [x] `SharingService.kt` 增加 RESUME 命令和共享恢复启动入口，执行前重查意愿；`MainActivity.kt` onResume 恢复、状态说明及后台定位权限设置；新增接收器，只处理开机/升级，在支持条件下恢复。
- [x] Manifest 增加开机及后台定位权限和非导出接收器，更新隐私文案；运行 `make android-test` 检查停止/授权撤回和既有生命周期回归。
- [x] 版本升至 0.5.3 / 4，执行 `make android-release` 与 `sh scripts/android.sh :app:lintRelease`；校验新旧 APK 证书一致、包名、版本、不可调试，复制到 `bin/family-location-0.5.3-release.apk`。
- [x] 同步上下文、设计、使用说明与真实进度。记录实际测试数、签名校验及真机未验证边界；执行 `git diff --check` 并复核变更。

汪汪
