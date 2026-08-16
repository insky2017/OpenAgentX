# Task 03：独立审核、E2E 与 tmux 运行

状态：已完成（2026-08-15）。

## 目标

由 Codex 对 AGY 实际修改进行独立审核，修复问题后在真实 `AgentBus` session 中完成闭环验收，并将 daemon 留在用户指定 pane 运行。

## 审核清单

- 只审核实际 diff，不接受自然语言总结作为完成证据；
- 确认 AGY 未修改 `AgentBus/` 之外的文件，也未改变原 staged 状态；
- 检查 SQL 事务、状态权限、并发 active task、Unix socket 清理和 tmux 注入安全；
- 检查错误是否返回非零退出码，JSON stdout 是否稳定；
- 运行 `gofmt`、`go vet ./...`、`go test ./...`、`go test -race ./...`；
- 使用隔离临时 tmux session 测 Connector，不先污染真实 Agent pane。

## 真实运行验收

1. 构建 `AgentBus/bin/agentbus`。
2. 在确认后的 daemon pane 启动 `serve`，保留 stdout/stderr 日志。
3. 注册 `orchestrator`、`agentbus-agent` 和 `quote-service`。
4. Orchestrator 提交一个无代码副作用的演示 Task。
5. AgentBus Agent 收到短通知，主动 `get`、`ack`、`status`、`complete`；Quote Service Agent 完成注册与无副作用连通检查。
6. Orchestrator watch/get 看到顺序事件与最终结果。
7. 重启 daemon，确认数据仍可读取。

## 验收结果

- Codex 已独立完成 `gofmt`、`go vet`、普通测试、race test 与构建，全部通过。
- `%51` AgentBus Agent 完成 `get → ack → status → complete` 真实闭环。
- `%52` Quote Service Agent 完成 `get → ack → status → 等待 supplement → 再次 get → complete` 真实闭环。
- Orchestrator 通过 `watch/get` 读取完整 Event sequence、Messages 与最终结果。
- `%53` daemon 受控重启后，Agent、Task、Message、Event 均持久可读，并继续留驻运行。
- 详细证据见 [验证报告](../../reports/validation/2026-08-15-agentbus-go-v0-tmux-e2e.md)。

## 交付

- 更新总体实施计划状态；
- 记录实际 pane 映射、启动命令和验证输出；
- daemon 留在指定 pane 运行；
- 不提交 Git commit，除非用户另行要求。
