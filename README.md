# lauren

lauren 是一套个人用的编码质量工具。它用一个不依赖大模型的命令行程序盯住一次变更：从把需求写清楚，到实现、验收、可选的发布观察，再到复盘。Codex 和 Claude Code 可以共用同一份 Skill 来引导调用这个程序。门禁在程序里，不在 Skill 里。

讨论过程见 [docs/讨论记录.md](docs/讨论记录.md)。命令、阶段、问题库和限制见 [docs/v0.1.md](docs/v0.1.md)。Skill 怎么放进两个宿主见 [docs/接入.md](docs/接入.md)。

背景演讲不是本仓库的规格：<https://ai.engineer/code/2026/talks/lauren-tan>

## 安装

从源码构建。模块声明的 Go 版本是 1.22。仓库没有发布过可下载的二进制，也没有上架插件市场。

```bash
go build -o dist/lauren ./cmd/lauren
```

构建在 Linux 上验证过。macOS 还没有做过运行实测；把得到的程序拷过去使用是预期的安装方式，不要在 Mac 上改这个仓库再推送。

程序文件和项目数据是分开的。升级时替换 `dist/lauren` 即可。项目里的 `lauren.project.json`、`.lauren/problems/` 和 `.lauren/runtime/` 不要用 `git reset` 或自动拉取 `main` 盖掉。本仓库没有这种命令。

## 接入 Skill

这份仓库已经带上 Skill 正文和两个宿主的入口，见 [docs/接入.md](docs/接入.md)。确认文件还在：

```bash
./dist/lauren skills check
```

## 看一次完整流程

```bash
bash examples/shelf/run-demo.sh
```

这是虚构的小例子，会走完六步，并演示问题库：第一次出现只记下来，第二次才登记到已有检查。登记不等于已经验证防住。
