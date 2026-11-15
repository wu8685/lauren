# lauren

跨 Codex 与 Claude Code 的个人 coding 质量 CLI，外加一份只负责引导的 Skill。阶段门禁在程序里，不在 Skill 里。

讨论记录仍在 [docs/讨论记录.md](docs/讨论记录.md)。v0.1 的实现范围和限制在 [docs/v0.1.md](docs/v0.1.md)。接入路径在 [docs/接入.md](docs/接入.md)。

背景演讲（不是本仓库规格）：<https://ai.engineer/code/2026/talks/lauren-tan>

## 安装

在 Linux 上构建：

```bash
go build -o dist/lauren ./cmd/lauren
```

`dist/lauren` 是安装下来的程序。目标项目的配置是 `lauren.project.json`，运行状态和证据在该项目的 `.lauren/runtime/`。两边分开。本仓库没有自动拉取 `main` 或 `git reset` 的命令。

macOS 上的使用方式是拿走这个程序来运行，并在目标项目里执行检查。这里的开发和测试在 Linux 上完成，没有在 Mac 上跑过。

## 完成一次变更

示例是虚构的记事程序 `examples/shelf`，不代表任何内部项目。

```bash
bash examples/shelf/run-demo.sh
```

脚本会把示例复制到临时 git 仓库，然后走完：明确需求、制定方案、实现与自验、验收与交付、发布与观察、复盘与改进。

手工做同样的事时，先构建，再在一个**自己的 git 根**里执行：

```bash
cd examples/shelf   # 演示请先复制到临时目录并 git init，见 run-demo.sh
../../dist/lauren change create --goal "空选择时走明确失败路径" --release true
../../dist/lauren change submit --artifact artifacts/demand.json
../../dist/lauren change advance
../../dist/lauren status
../../dist/lauren next
../../dist/lauren --json status
```

`status` 把阶段、整体状态、检查结果、交付接受、发布声明和工具读回分开显示。没有完成百分比，也没有「已通知」。

新项目可以先 `lauren init`。检查命令写进 `lauren.project.json` 的 `argv`，由 `lauren check` 实际执行。这个配置是显式信任的，程序不会把变更目标拼成 shell。

## 信任边界

本地状态文件可以被改掉。CLI 不认证谁在操作，也不能防止恶意篡改。检查是否可信，看仓库 CI 是否重跑；交付是否接受，看单独记下的接受记录。换一个 agent 再跑，不等于独立验收。

## 开发

```bash
go test ./...
go vet ./...
go build -o dist/lauren ./cmd/lauren
bash examples/shelf/run-demo.sh
```
