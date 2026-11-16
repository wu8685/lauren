---
name: lauren-workflow
description: 用 lauren CLI 推进一次有明确目标的变更。适用于明确需求、制定方案、实现与自验、验收与交付、发布与观察、复盘与改进。不要自行判断门禁或把检查记成通过。
---

# lauren 工作流

门禁只在 `lauren` 命令的退出码和 `status` / `next` 里。本 Skill 不复制通过条件，也不根据对话判断需求是否正确。

1. 在含有 `lauren.project.json` 的项目根目录工作。该目录必须是 git 根。
2. 创建变更：`lauren change create --goal "<目标>"`。需要覆盖项目默认值时，用 `--environment`、`--release-stage included|excluded`、`--artifact-level standard|abbreviated`、`--integration-branch` 或 `--integration-intent not_in_current_iteration`。
3. 用 `lauren next` 看下一合法动作，用 `lauren status` 看阶段、policy、检查环境和几件分开的事实。
4. 当前阶段产物用 `lauren change submit --artifact <项目内相对路径>` 提交。精简记录也不取消必过检查。
5. 检查用 `lauren check`，环境默认是这次流程的 `policy.environment`，也可以 `--environment`。命令来自项目配置，不要把任务文本拼成 shell，也不要另造一个通过结果。
6. 可以推进时执行 `lauren change advance`。被拒绝就按 stderr 或 JSON 的 `error` 处理。验收失败会回到实现与自验。
7. 回退、阻塞、恢复、取消都要带理由：`rollback`、`block`、`resume`、`cancel`。
8. 交付接受和发布声明用 `lauren change accept`。来源只能是 `human`、`other-session` 或 `external`。`gate` 为 `self_verification` 的检查不能充当独立验收。
9. 手填版本用 `declare-version`，来源是 declaration。机器读回只来自 `release_readback`，并且要先 `change integration record` 记下合入的分支和 SHA。不要自动 merge、push 或部署。
10. 复盘用 `change problem add` 记下问题。有去向再用 `dispose`。没有同一检查的旧失败和新通过时，不要把问题说成已验证修复。
11. 没有投递回执。不要说已经通知用户。
12. 本地状态文件可以被改写。CLI 不认证操作者，也不能防止篡改。

人读输出和 `--json` 都可以。不要发明完成百分比。
