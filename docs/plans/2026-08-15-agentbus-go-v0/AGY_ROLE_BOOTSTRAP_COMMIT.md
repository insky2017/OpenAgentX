# AgentBus Agent：提交 V0.1 实现

当前实现和真实 tmux 验收已经由 Codex 通过。请只提交你负责的代码、角色资产与运行入口，不要提交设计、计划、审核记录或验证报告。

## 操作

在 `AgentBus/` 独立仓库执行：

```bash
gofmt -l .
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./...
go build -o bin/agentbus ./cmd/agentbus

git add -- README.md go.mod go.sum agents docs/runtime internal
git diff --cached --check
git diff --cached --name-only
git commit -m "feat: add agent role bootstrap lifecycle"
git status --short
git rev-parse --short HEAD
```

## 边界

- staged 文件只能来自 `README.md`、`go.mod`、`go.sum`、`agents/`、`docs/runtime/`、`internal/`；
- 不要 stage `docs/design/`、`docs/plans/`、`docs/reports/`；
- 不要修改或提交 SteadyFlow 父仓库；
- 不要 push；
- 不需要为旧数据库、旧 API 或旧 CLI 保留兼容逻辑；
- 若检查失败，停止提交并说明错误，不要绕过。

完成后报告 commit hash、验证结果和剩余未提交文件。
