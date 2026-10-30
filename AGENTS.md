# 项目工作规则

## 保留的已有规则

1. 用简体中文回复。
2. 所有任务不需要执行 git add、commit 操作；本项目明确不执行 `git add`、`git commit`。
3. 每次回复的消息最后，都要添加一个“汪汪”。
4. review 代码时，优先用 Git/GitLab 接口获取 MR 内容，尽量不用 browser-use。
5. 在普通工作区、不在 worktree 时，允许直接在当前工作区修改代码，不创建 worktree，不需要用户确认。

## 项目补充规则

6. 开始工作前阅读 [docs/PROJECT_CONTEXT.md](docs/PROJECT_CONTEXT.md) 和 [docs/PROGRESS.md](docs/PROGRESS.md)。从真实进度继续，不重新开始已完成的工作。
7. 所有生成文件放在 `family-location/` 项目内；Go 构建、测试优先使用现有 Makefile，保持缓存和临时文件也在项目内。
8. 沿用现有目录结构，不因旧建议使用 `server/` 就重命名 `backend/`。
9. 分阶段实现，每步提供启动方法、实际验证结果和下一步；及时更新 `docs/PROGRESS.md`，不得把计划、交叉构建或单台设备验证写成已完成全部功能或真机验收。
10. 修改需求时同步 `docs/PROJECT_CONTEXT.md` 及相关设计；技术细节引用已有文档，避免重复和冲突。
11. 文档中不得记录密码、设备令牌或签名私钥。
12. 每次修改 Android 代码后，必须构建 APK；默认使用已有发布签名执行 `make android-release`，校验产物并提供路径，更新进度。构建失败须报告实际原因，不能只运行测试就交付。新版本按现有规则递增版本号，保留旧版本交付包；构建不代表已安装或真机验收。

汪汪
