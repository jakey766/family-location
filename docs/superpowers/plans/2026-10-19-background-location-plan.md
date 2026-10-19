# 锁屏共享与最后位置实施计划

**目标：** vivo 锁屏后台共享增强；服务器及网页保留最后位置。
**架构：** 沿用 Go/SQLite 最新点和 Kotlin 前台服务，修改过期语义及服务恢复，不增加轨迹、开机广播或新数据库表。
**执行：** 当前工作区顺序实现，遵守项目约定，不执行 git add/commit。

- [x] 后端：修改 `backend/internal/location/service_test.go` 的边界用例为保留最后点，清理循环用例改为撤销残留点；执行 `make test` 观察失败。`locations.go` 移除租约过滤，`cleanup.go` 只清理停用/撤销点，再执行测试及 `make location-smoke`（更新重启过期保留断言）。
- [x] 网页：`scripts/web_state_test.mjs`、`scripts/web_dom_test.mjs` 断言过期仍显示并标记陈旧；断网仍有快照，重连重新确认。`state.mjs` 将超时视为 stale，`app.mjs` 更新显示与网络失败行为，运行 `make web-smoke`。
- [x] 安卓：`SharingControllerTest.kt` 先覆盖启动断网重试、离线停止文案、销毁本地释放；服务测试覆盖恢复意愿、空 Intent、停止、权限失效；实现 SharedPreferences 意愿、START_STICKY、本地释放、高德后台通知及设置入口。运行 `make android-test`。
- [x] 交付：更新隐私说明与相关使用/设计文档，升版 0.5.2，运行 `make android-release`、`:app:lintRelease`、`make test-race vet android-smoke`，验证签名与版本，复制 APK 到 `bin/`；记录真实结果和真机待验收项。

汪汪
