# 一分钟上报与便捷配对实施计划

依据[设计](../specs/2026-10-19-pairing-convenience-design.md)，在当前工作区顺序实施，不执行 git add/commit。

- [x] 在 backend/internal/auth/service_test.go 增加短标识字符/长度、旧标识兼容与设备令牌长度回归；修改 MotionPolicyTest 的移动期预期为 60,000 ms；新增 Android 配对信息导入页面测试。运行 `make test android-test`，确认新增功能尚未实现的失败。
- [x] backend/internal/auth/devices.go 增加独立短 ID 生成及唯一冲突重试，不修改 randomToken；MotionPolicy 与 AmapSource 统一间隔；新增 PairingInfo 解析器并接入 MainActivity 窗口焦点/主动粘贴入口。
- [x] 执行 `make test vet android-test`，验证配对错误/消费原语义、解析输入边界、页面填写行为及移动/静止切换。
- [x] 更新 PROJECT_CONTEXT、ANDROID、AUTH_API、WEB_UI 及当前设计；PROGRESS 保留历史验证，增加本次实际结果和待验证项。
- [x] 版本更新为 0.5.4/versionCode 5；执行 `make android-smoke android-release` 和 `sh scripts/android.sh :app:lintRelease`；验证签名与旧包一致，复制正式包至 bin，记录实际产物与摘要。
- [x] 检查最终 diff 与需求覆盖，说明后端需更新重启、APK 可覆盖安装、真实设备的下一步验证。

汪汪
