# AGY 实施指令：AgentBus Go V0

你是本任务的实施 Agent。请直接在当前仓库完成 AgentBus Go V0 代码，事实源按优先级为：

1. `AgentBus/docs/plans/2026-08-15-agentbus-go-v0.md`
2. `AgentBus/docs/plans/2026-08-15-agentbus-go-v0/01-core-daemon-store.md`
3. `AgentBus/docs/plans/2026-08-15-agentbus-go-v0/02-cli-tmux-connector.md`
4. `AgentBus/docs/ARCHITECTURE.md`

工作范围和安全约束：

- 只修改 `AgentBus/`，不得修改其他目录。
- 保留已有 `AgentBus/docs` 内容；除实现所需 README/运行文档外，不重写方案文档。
- 不运行 `git add`、`git commit`，不改变已有 staged/unstaged 状态。
- 搜索使用 `rg`/`rg --files`，不要无边界扫描仓库。
- Go 版本为 1.22；实现 daemon + CLI + SQLite WAL + HTTP over Unix socket + TmuxConnector。
- V0 不实现 MCP、ACP、A2A、AgyBatch、Runtime spawn、Web UI 或 pane 输出解析。
- TmuxConnector 仅注入固定短通知，不注入任务正文；使用 direct argv 和唯一 tmux buffer，禁止 shell 拼接。
- 任务状态和 Event 必须事务一致；actor 权限、幂等和单 Worker active Task 必须由服务端校验。
- pane 地址要支持稳定 pane ID（例如 `%51`）及 `session:window.pane`。
- CLI 默认 JSON stdout，错误写 stderr 并返回非零状态。

请自行完成合理的包结构、migration、单元/集成测试和 README。至少运行：

```bash
cd AgentBus
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
go build -o bin/agentbus ./cmd/agentbus
```

不要在真实 `%50/%51/%52` pane 上做 Connector 测试；使用独立临时 tmux session。不要启动或替换 `%52` 的进程，真实运行由 Codex 审核后执行。

完成后只需简要报告：修改文件、关键设计、测试结果、已知限制。Codex 会独立审核实际 diff。
