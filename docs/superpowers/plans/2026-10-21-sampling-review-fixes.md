# 采样周期评审修复计划

目标：落实用户授权的两项评审修复，保留已有 120/300 秒采样改动。

设计：后端 `StaleAfter` 与网页 `locationStatus` 默认值统一为 7 分钟，为静止 5 分钟采样留出 2 分钟余量；原有 7 分钟租约仍独立判断。按原始定位时间计算陈旧状态，同点重传不续期。App 显示 2/5 分钟，当前设计和使用文档同步，历史版本记录保留。

技术：Go、JavaScript、Kotlin。当前工作区内顺序执行，不执行 git add/commit。

- [x] 在 `backend/internal/location/service_test.go` 和 `scripts/web_state_test.mjs` 增加 300 秒、330 秒、419.999 秒仍新鲜及 420 秒陈旧的用例；延迟上传测试使用 7 分钟旧点，保持租约过期测试。
- [x] 分别执行 `make test`、`make web-test`，确认旧实现因 5 分钟判定陈旧而失败。
- [x] 修改 `backend/internal/location/service.go` 为 `StaleAfter = 7 * time.Minute`，`backend/internal/webui/assets/state.mjs` 默认参数为 `staleAfter = 420`；同步 `MainActivity.kt` 的 2/5 分钟文案和烟雾测试旧点样本。
- [x] 更新需求、相关设计、API/网页/Android 使用说明与进度；注明需更新后端和网页、新 APK 尚未构建。
- [x] 执行 `make test vet web-smoke location-smoke android-test` 及 `git diff --check`，检查结果与变更范围。

汪汪
