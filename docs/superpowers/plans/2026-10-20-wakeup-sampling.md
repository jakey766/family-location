# 休眠采样修复实施计划

> 按 writing-plans 与 test-driven-development 在当前工作区逐项执行；用户已确认方案，不执行 git add/commit，不创建 worktree。

**目标：** 修复 vivo 进程存活但休眠导致采样暂停的问题，使用逐次定位与有超时短锁，不恢复常驻锁。

**架构：** WakeupTimer 负责 Handler/AlarmManager 共用截止时间；ScheduledPositionSource 负责有界单次采样；SharingController 和 SharingService 延用原会话/网络/夜间状态机，补唤醒入口和重试闹钟。

**技术：** Kotlin、Android AlarmManager/PowerManager、AMap 6.4.9、Robolectric API 25/33。

## 任务 1：失败测试

- [x] 新增 `android/app/src/test/java/com/jk/family/ScheduledPositionSourceTest.kt`，使用可控 elapsed 时钟和假的 PositionSource：初始采样有锁、收到结果后无锁；仅推进 elapsed，调用 wake() 后再次 start；重复 wake 不重复采样；30 秒超时/停止/夜间后迟到结果丢弃；静止模式在重新启动单次 SDK 时保留。
- [x] 新增 `WakeupTimerTest.kt`：实际 AlarmManager 中存在 ELAPSED_REALTIME_WAKEUP；重复调度不推迟同一到期任务；广播只在保存共享意愿时恢复；精确权限缺失降级；取消后晚到广播不恢复。
- [x] 修改 `NetworkWorkExecutorTest.kt`，先验证新交接要求在旧代码失败：
  ```kotlin
  executor.execute { assertTrue(ShadowPowerManager.getLatestWakeLock().isHeld) }
  assertTrue(ShadowPowerManager.getLatestWakeLock().isHeld)
  queue.removeFirst().run()
  assertFalse(ShadowPowerManager.getLatestWakeLock().isHeld)
  ```
- [x] 增加 `AmapSourceTest` 的 `assertTrue(options.isOnceLocation)`，旧连续模式应失败。
- [x] 执行 `sh scripts/android.sh :app:testDebugUnitTest --tests '*NetworkWorkExecutorTest' --tests '*AmapSourceTest'`，确认真实断言失败；新类缺失可先观察编译失败。

## 任务 2：实现和定向验证

- [x] 创建 `WakeupTimer.kt`：独立 SAMPLE/RETRY PendingIntent、单调时间截止、Handler + 唤醒闹钟、精确授权降级、取消；WakeWorkReceiver 检查意愿/权限后调用活动 controller.wake() 或既有恢复入口。
- [x] 创建 `ScheduledPositionSource.kt`：30 秒窗口与 35 秒硬超时锁，结果处理前停止 SDK，回调后依据最新 interval 安排下一次；窗口 token 使旧回调失效；暂停与 stop 取消全部窗口工作。
- [x] `AmapSource.kt` 改单次定位，保留外部设置的间隔/模式，HTTP 超时 10 秒；版本升为 0.5.6/code7。
- [x] `SharingController.kt` 为 PositionSource 增加默认 wake()；controller.wake() 先更新夜间状态，再仅在白天活动状态唤醒 source。`SharingService.kt` 接入采样包装器和重试 WakeupTimer。
- [x] `NetworkWorkExecutor.kt` 增加有超时交接锁，在执行锁取得后交接、finally 清理；拒绝提交同样清理。
- [x] Manifest 注册非导出的 WakeWorkReceiver；更新精确定时说明和隐私文字。
- [x] 执行 `sh scripts/android.sh :app:testDebugUnitTest --tests '*ScheduledPositionSourceTest' --tests '*WakeupTimerTest' --tests '*NetworkWorkExecutorTest' --tests '*AmapSourceTest' --tests '*SharingServiceTest' --tests '*SharingScheduleTest'` 并修正失败。

## 任务 3：交付验证

- [x] `make android-test`；`make android-release`；`sh scripts/android.sh :app:lintRelease`。
- [x] 使用 requesting-code-review 技能安排独立只读评审，修复重要发现并复测受影响用例。
- [x] `apksigner verify --verbose --print-certs` 确认 v2 和旧版证书一致；aapt 确认版本，复制到 `bin/family-location-0.5.6-release.apk`。
- [ ] 真机安装/采样验证：vivo 安装器要求账号验证，用户明确选择先交付 APK、稍后自行安装。本轮停止安装，手机仍 0.5.5；新包定位/上传、等待无锁与断电长期验收均待用户安装后进行。
- [x] 更新 `docs/PROGRESS.md`、`docs/ANDROID.md`，记录真实执行结果、产物摘要、限制和下一步。运行 `git diff --check`。

补充：独立评审两项发现已先红后绿修复；增加 `WakefulMainExecutor.kt` 与 `ResultHandoffTest.kt`，覆盖 IO→主线程交接。最终 154 项测试通过、1 项按配置跳过，Release 构建/lint/签名通过。详见 PROGRESS.md。
