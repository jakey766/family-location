# 省电与夜间暂停实施计划

**Goal:** 实现已获授权的三项省电措施和手机本地时间 22:00–08:00 夜间暂停。
**Architecture:** 控制器保留共享意愿和会话，暂停采集和上传；平台层负责边界定时、网络恢复及短时网络唤醒锁。
**Tech Stack:** Kotlin、Android API 25–35、高德 6.4.9、JUnit、Robolectric。

在当前会话逐项执行；不创建 worktree、不执行 git add/commit。设计见 ../specs/2026-10-19-power-saving-design.md。

- [x] core 策略：新增 QuietHours.kt/RetryBackoff.kt 及相应测试；明确本地 22:00 包含、08:00 不包含、时区变化，单调时钟退避 15/30/60/120/240/300 秒与重置。`make android-test` 先失败再实现。
- [x] SharingController：测试夜间启动、采集中暂停、旧回调和排队上传丢弃、次日新点恢复、启动跨界及停止竞态；加入 quietNow 与任务调度回调，网络可用门控和退避，正常成功清零，定位新点不越过退避。测试旧行为回归。
- [x] 平台省电：AmapSource 静止选 Battery_Saving、移动选 Hight_Accuracy、仅近似授权兼容；NetworkWorkExecutor 仅任务执行时持有 15 秒超时锁并 finally 释放。新增行为测试。
- [x] 定时与服务：SharingSchedule 安排下一 22:00/08:00 边界，精确授权不足降级；广播处理时间/时区和定时，停止取消闹钟；服务移除 15 秒轮询与常驻锁，按待重试和边界调度，注册并释放网络回调。API 25/33 生命周期回归先红后绿。
- [x] UI 与文档：展示夜间暂停、精确定时权限状态/入口和低功耗说明，更新隐私、PROJECT_CONTEXT、ANDROID、PROGRESS；versionCode 6/versionName 0.5.5。
- [x] 验证与交付：`make android-test`、`make android-release`、`sh scripts/android.sh :app:lintRelease`、APK 签名/版本/权限、`git diff --check`；复制到 bin/family-location-0.5.5-release.apk，记录测试数量、SHA256 和真机未验收边界。

验证结果：126 项通过，1 项独立后端 smoke 按配置跳过；正式包与 Release lint 通过，签名延续 0.5.4。完整证据及真机待验收项见 ../../PROGRESS.md。

汪汪
