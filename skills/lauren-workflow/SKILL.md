---
name: lauren-workflow
description: 用 lauren CLI 推进一次有明确目标的变更。适用于明确需求、制定方案、实现与自验、验收与交付、发布与观察、复盘与改进。不要自行判断门禁或把检查记成通过。
---

# lauren 工作流

门禁只在 `lauren` 命令的退出码和 `status` / `next` 里。本 Skill 不复制通过条件，也不根据对话判断需求是否正确。

开始工作时先执行 `lauren session-context`。它只读，不创建变更、不跑检查、不改状态。没有本地 JSON 入口时，按它给出的共享来源执行 `lauren init-worktree`，然后再读一次；已经有入口就不要再初始化。软链接入口还要有 Skill 和插件清单。`.lauren/runtime/` 还不存在不等于未初始化。没有变更时按需求创建，不要补造历史。有变更时阶段和下一步与 `status` / `next` 相同。`阻塞`、`取消`、`修订 policy`、恢复、接受、合入和发布要人决定。Red 缺失要真实执行，不要默认跳过。阻塞时先看原因，不要自动 resume。`host_injected` 为 false 表示没有观察到宿主会话。送达要用 `lauren host install` 先看范围，再 `--apply`；`skills check` 不会改宿主配置。钩子跑的是 `lauren host run`，失败或不是这个仓库时不要卡住会话。这里不是 lauren 项目时，结果是 not_applicable。

1. 在当前 git worktree 里工作。配置可以是工作区根的 `lauren.project.json`，也可以是主检出 `.lauren/project/lauren.project.json` 的一份共享正文。本地入口只能是指向它的软链接，或只含 `shared_config` 的引用，不能在里面覆盖 checks，也不要做配置深合并。检查、候选、锁和 runtime 都在当前 Worktree，不要把主工作区当成这次候选。共享脚本用 `resource:` 引用 `.lauren/project/` 里的文件，检查的 cwd 仍是当前 Worktree。不要用 `..`，也不要自己展开环境变量。环境和集成分支的差别用这次变更的 policy。新 Worktree 在 `git worktree add` 之后执行 `lauren init-worktree`。它只建软链接入口和本地排除，不复制 runtime，不创建变更，入口里不写 checks。重复执行是安全的。根上已有普通配置文件时不要删，按提示人工迁移。
2. 创建变更：`lauren change create --goal "<目标>"`。需要覆盖项目默认值时，用 `--environment`、`--release-stage included|excluded`、`--artifact-level standard|abbreviated`、`--integration-branch` 或 `--integration-intent not_in_current_iteration`。可选检查可以 `--skip 名称=理由`。必过检查不能跳过。创建时会把检查计划冻住。过了明确需求就不能直接改计划。先 `change plan propose`，然后请用户自己执行 `change plan confirm --owner-confirmed`。不要自己加上 `--owner-confirmed`。
3. 用 `lauren session-context` 看启动摘要，用 `lauren next` 看下一合法动作，用 `lauren status` 看阶段、policy、检查环境和几件分开的事实。`workspace` 是当前工作区和配置来源。`plan_view` 是这次冻结的计划；它和当前共享配置不同时，没有自动升级。
4. 当前阶段产物用 `lauren change submit --artifact <项目内相对路径>` 提交。精简记录也不取消必过检查。
5. 检查用 `lauren check`，环境默认是这次流程的 `policy.environment`，也可以 `--environment`。命令来自项目配置，不要把任务文本拼成 shell，也不要另造一个通过结果。单项检查默认 600 秒，由 `defaults.check_timeout_seconds` 和检查上的 `timeout_seconds` 决定。超时是失败。没有临时改超时的命令。候选指纹会看见带引号、空格或换行的路径变化；读不了的路径不能当成通过。失败分类只能由检查脚本写 `lauren-class classification=...`，不要自己编一个分类。`defaults.red_gate` 缺省是 `required`，字段省略也一样：离开实现与自验前要有先失败再通过的记录。跳过必须 `change red skip --reason`，理由会留在状态里。只有项目把 `defaults.red_gate` 写成 `off` 才关闭这个门禁。检查若打开了 `require_executed`，输出里要有 `lauren-stats executed=<非零>`。全跳过或没有统计都是失败，而且这次失败会盖过同一环境更早的通过。
6. 可以推进时执行 `lauren change advance`。被拒绝就按 stderr 或 JSON 的 `error` 处理。同一环境的最新结果才算数，更早的通过不会盖过后来的失败。验收失败会回到实现与自验。
7. 回退、阻塞、恢复、取消都要带理由：`rollback`、`block`、`resume`、`cancel`。
8. 交付接受和发布声明用 `lauren change accept`。来源只能是 `human`、`other-session` 或 `external`，都只是声明，`source_verified` 不会因此变成 true。`product_verified` 只表示已配置要求有通过记录，不是独立验收。`gate` 为 `self_verification` 的检查不能充当独立验收。`requires_report` 的要求要先 `change report submit --requirement --file --source self|declared|external`。报告摘要由程序计算。来源只是声明，不会被标成已验证。
9. 手填版本用 `declare-version`，来源是 declaration。启用发布阶段时，离开发布与观察要有必过的 `release_readback` 通过，并且先 `change integration record` 记下合入的分支和 SHA。发布声明不是机器读回。不要自动 merge、push 或部署。
10. 复盘用 `change problem add` 写入 `.lauren/problems/`。先按 title、layer、symptom 检索；同类就追加一次出现。不要手填 verified_fixed。次数达到 `defaults.promote_threshold`（默认 2）才晋升成检查；只有一次就留在问题库里。有去向再用 `dispose`。没有同一检查的旧失败和新通过时，不要把问题说成已验证修复。
11. 没有投递回执。不要说已经通知用户。
12. 本地状态文件可以被改写。CLI 不认证操作者，也不能防止篡改。

人读输出和 `--json` 都可以。不要发明完成百分比。
