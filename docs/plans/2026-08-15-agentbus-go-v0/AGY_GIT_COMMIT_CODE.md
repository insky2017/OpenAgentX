# AgentBus Agent：初始化独立仓库并提交代码

你负责在 `AgentBus/` 中完成独立 Git 仓库的第一个代码提交。不要修改任何文件内容，不操作 SteadyFlow 父仓库，不提交 `docs/`，不 push，不创建远端仓库，不操作真实 tmux panes。

## 1. 初始化

从 SteadyFlow 根目录执行：

```bash
cd AgentBus
git init -b main
```

若仓库已经初始化则先停止并汇报，不要重建。

## 2. 只暂存你负责的实现文件

只允许暂存：

```text
.gitignore
README.md
go.mod
go.sum
cmd/
internal/
```

显式执行：

```bash
git add .gitignore README.md go.mod go.sum cmd internal
```

必须确认 `docs/` 没有进入 staged 区；`bin/`、`data/`、`run/` 必须继续被忽略。

## 3. 校验与提交

```bash
git diff --cached --check
git diff --cached --stat
git status --short
git commit -m "feat: implement AgentBus Go V0"
```

提交后输出：

```bash
git rev-parse HEAD
git show --stat --oneline --summary HEAD
git status --short
```

最后只汇报 commit hash、提交文件范围和剩余未跟踪路径。不要继续提交文档。
