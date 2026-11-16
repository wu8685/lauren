# lauren

lauren 是一套个人用的编码质量工具。它用一个不依赖大模型的命令行程序盯住一次变更：从把需求写清楚，到实现、验收、可选的发布观察，再到复盘。Codex 和 Claude Code 可以共用同一份 Skill 来引导调用这个程序。门禁在程序里，不在 Skill 里。

讨论过程见 [docs/讨论记录.md](docs/讨论记录.md)。命令、阶段、问题库和限制见 [docs/v0.1.md](docs/v0.1.md)。把 Skill 放进另一个项目，以及为 macOS 交叉编译，见 [docs/接入.md](docs/接入.md)。

背景演讲不是本仓库的规格：<https://ai.engineer/code/2026/talks/lauren-tan>

## 安装

仓库没有发布过可下载的二进制，也没有上架插件市场。从源码安装。`go.mod` 要求 Go 1.22 或更新。命令在这份检出的模块根目录执行。

```bash
git clone https://github.com/wu8685/lauren.git
cd lauren
go build -o dist/lauren ./cmd/lauren
mkdir -p "$HOME/.local/bin"
install -m 755 dist/lauren "$HOME/.local/bin/lauren"
```

把 `$HOME/.local/bin` 放进当前 shell 的 `PATH`，仍在该仓库根目录确认能找到命令，并检查仓库自带的 Skill 文件：

```bash
export PATH="$HOME/.local/bin:$PATH"
command -v lauren
lauren skills check
```

`skills check` 只检查当前目录或 `--root` 里的 Skill 文件，不安装 Skill。另一个项目怎么放，见 [docs/接入.md](docs/接入.md)。

上面的获取、构建、安装和检查在 Linux 上验证过。macOS 不要直接使用这里产出的 `dist/lauren`；按目标架构交叉编译的步骤在 [docs/接入.md](docs/接入.md)。交叉编译通过不等于在 macOS 上运行过。

升级时替换 `PATH` 里的 `lauren`。目标项目里的 `lauren.project.json`、`.lauren/problems/` 和 `.lauren/runtime/` 留在那个项目里，细节见 [docs/v0.1.md](docs/v0.1.md)。

## 看一次完整流程

在仓库根目录：

```bash
bash examples/shelf/run-demo.sh
```

这是虚构的小例子，会走完六步，并演示问题库：第一次出现只记下来，第二次才登记到已有检查。登记不等于已经验证防住。
