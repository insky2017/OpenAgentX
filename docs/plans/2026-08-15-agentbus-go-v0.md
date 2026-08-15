---
doc_type: implementation_plan
status: completed
updated_at: 2026-08-16
---

# AgentBus Go V0 实施计划

## 1. 目标

在不启动新 Runtime 的前提下，把 tmux `AgentBus` session 中现有的 Codex CLI 和两个 AGY CLI 接入一个最小、可持久恢复的 AgentBus：

```text
User
  ↓
Codex Coordinator
  ↓ agentbus task submit/watch
Go AgentBus daemon + SQLite
  ↓ TmuxConnector 短通知
AGY AgentBus Agent / AGY Quote Service Agent
  ↓ agentbus task get/ack/status/complete
Go AgentBus daemon
```

## 2. V0 / V0.1 范围

- Go 1.22 单二进制；
- Unix socket 本机 API；
- SQLite WAL 持久化；
- Agent Registry；
- Task、Message、Event 最小状态机；
- CLI 提交、读取、ACK、状态、补充消息、完成、失败、取消和观察；
- TmuxConnector pane 映射、存活检查、短通知注入；
- 每个 Worker 最多一个 `queued/running` Task；
- 结构化日志输出到 daemon stdout。
- canonical Agent manifest 与 ROLE；
- attach/bootstrap/session generation/ready 生命周期；
- 未 ready Agent 的事务内 Task gate；
- generic `agent launch` 生命周期封装。

明确不做：MCP、ACP、A2A、AgyBatch、专用 Runtime Adapter、Web UI、自动角色路由、远程网络、多租户和 pane 输出解析。V0.1 的 generic launcher 只包装现有 Runtime 命令，不负责 Runtime 私有协议。

## 3. 技术基线

```text
Language:       Go 1.22
Database:       SQLite WAL
Transport:      HTTP over Unix domain socket
CLI:            同一个 agentbus 二进制
Logging:        log/slog JSON to stdout
Tmux calls:     os/exec direct argv，禁止 shell 拼接
```

推荐运行路径：

```text
AgentBus/bin/agentbus
AgentBus/data/agentbus.db
AgentBus/run/agentbus.sock
```

运行时目录和数据库不进入 Git。

## 4. CLI 契约

```bash
agentbus serve --db data/agentbus.db --socket run/agentbus.sock

agentbus agent register --id coordinator --role coordinator \
  --connector tmux --address %50
agentbus agent register --id agentbus-agent --role agentbus \
  --connector tmux --address %51
agentbus agent register --id quote-service --role quote \
  --connector tmux --address %52
agentbus agent list

agentbus agent whoami --config agents/coordinator/agent.yaml
agentbus agent attach --config agents/agentbus-agent/agent.yaml --address %51
agentbus agent bootstrap --id agentbus-agent
agentbus session ready --agent agentbus-agent --generation 1
agentbus session show --agent agentbus-agent

agentbus task submit --from coordinator --to agentbus-agent \
  --idempotency-key demo-001 --content "执行 AgentBus 自检"
agentbus task get <task-id> --agent agentbus-agent
agentbus task ack <task-id> --agent agentbus-agent
agentbus task status <task-id> --agent agentbus-agent --message "正在自检"
agentbus task send <task-id> --from coordinator --content "补充检查缓存"
agentbus task complete <task-id> --agent agentbus-agent --result "检查完成"
agentbus task fail <task-id> --agent agentbus-agent --error "失败原因"
agentbus task cancel <task-id> --agent coordinator
agentbus task watch <task-id> --agent coordinator --after 0 --timeout 30s
```

所有 CLI 支持 `--socket`，也读取 `AGENTBUS_SOCKET`。输出默认 JSON，便于 Agent 稳定解析。

## 5. 最小数据模型

### agents

```text
id, role, connector, address, status, created_at, updated_at
```

### tasks

```text
id, sender_agent_id, target_agent_id, idempotency_key,
content, status, result, error, created_at, updated_at
```

约束：`(sender_agent_id, idempotency_key)` 唯一；同一 target 最多一个 `queued/running` Task。

### messages

```text
id, task_id, sender_agent_id, kind, content, created_at
```

### events

```text
sequence, id, task_id, actor_agent_id, type, payload, created_at
```

`sequence` 由 SQLite 自增，`watch --after` 用它增量读取。

## 6. 状态转换

```text
queued --ack--> running --complete--> succeeded
                     └--fail-------> failed
queued --cancel--------------------> canceled
```

- V0 不支持强制停止正在执行的交互式 Agent；`running` Task 的 cancel 返回明确错误。
- 只有目标 Agent可以 ACK、完成或失败。
- 只有任务发起方可以补充消息或取消 queued Task。
- 终态不可再次转换。

## 7. TmuxConnector 契约

新任务通知固定为：

```text
[AgentBus] New task <task-id>. Use AgentBus CLI: task get <task-id> --agent <agent-id>
```

补充消息通知固定为：

```text
[AgentBus] Task <task-id> has a new message. Use AgentBus CLI: task get <task-id> --agent <agent-id>
```

Connector 不注入用户正文，不解析 pane 输出。实现使用唯一 tmux buffer，经 `load-buffer`/`paste-buffer` 后发送 Enter；所有 ID 和地址先校验，所有 tmux 调用都使用直接 argv。

当前验收映射使用 tmux 稳定 pane ID：Coordinator `%50`、AgentBus Agent `%51`、Quote Service Agent `%52`、daemon 日志 pane `%53`。Connector 同时接受标准 `session:window.pane` 地址，但运行记录优先保存稳定 pane ID，避免分屏重排导致 `pane_index` 变化。

## 8. 分任务文档

- [01-core-daemon-store.md](2026-08-15-agentbus-go-v0/01-core-daemon-store.md)：Go 工程、领域模型、SQLite 和 Unix socket daemon。
- [02-cli-tmux-connector.md](2026-08-15-agentbus-go-v0/02-cli-tmux-connector.md)：CLI、权限/状态校验与 TmuxConnector。
- [03-review-e2e-runtime.md](2026-08-15-agentbus-go-v0/03-review-e2e-runtime.md)：独立审核、测试和真实 tmux 运行验收。
- [04-role-bootstrap.md](2026-08-15-agentbus-go-v0/04-role-bootstrap.md)：canonical 角色、attach/bootstrap/ready、Task gate 与三 Agent 实机验收。

过程指令与审核记录：

- [AGY_IMPLEMENTATION_PROMPT.md](2026-08-15-agentbus-go-v0/AGY_IMPLEMENTATION_PROMPT.md)、[AGY_REVIEW_ROUND1.md](2026-08-15-agentbus-go-v0/AGY_REVIEW_ROUND1.md)、[AGY_REVIEW_ROUND2.md](2026-08-15-agentbus-go-v0/AGY_REVIEW_ROUND2.md)、[AGY_REVIEW_ROUND3.md](2026-08-15-agentbus-go-v0/AGY_REVIEW_ROUND3.md)：V0 实现和三轮审核记录；
- [AGY_GIT_COMMIT_CODE.md](2026-08-15-agentbus-go-v0/AGY_GIT_COMMIT_CODE.md)：V0 实现提交边界；
- [AGY_ROLE_BOOTSTRAP_REVIEW_ROUND1.md](2026-08-15-agentbus-go-v0/AGY_ROLE_BOOTSTRAP_REVIEW_ROUND1.md)、[AGY_ROLE_BOOTSTRAP_REVIEW_ROUND2.md](2026-08-15-agentbus-go-v0/AGY_ROLE_BOOTSTRAP_REVIEW_ROUND2.md)：V0.1 生命周期审核记录；
- [AGY_ROLE_BOOTSTRAP_COMMIT.md](2026-08-15-agentbus-go-v0/AGY_ROLE_BOOTSTRAP_COMMIT.md)：V0.1 实现提交边界。

## 9. 验收条件

1. 重复提交相同 idempotency key 返回同一 Task，不重复注入通知。
2. 未注册 Agent、错误 actor、非法状态转换全部拒绝并返回非零退出码。
3. 同一 Worker Agent 已有 active Task 时，第二个提交被拒绝。
4. pane 不存在时 Task 仍可诊断，记录 `delivery_failed`，不能伪造已通知。
5. TmuxConnector 不把用户正文直接注入终端，也不通过 shell 执行拼接字符串。
6. AgentBus Agent 与 Quote Service Agent 都能接入；AgentBus Agent 可通过 CLI 完成 `get → ack → status → complete`。
7. Coordinator 的 watch 能按 sequence 看到完整事件和最终结果。
8. daemon 重启后 Agent、Task、Message 和 Event 仍存在。
9. `go test ./...`、`go test -race ./...`、`go vet ./...` 通过。
10. daemon 最终在用户指定 tmux pane 中运行并持续输出结构化日志。
11. Coordinator 与两个南向 Agent 均从 canonical manifest/ROLE 恢复身份，并用 generation 精确确认 ready。
12. re-bootstrap 后旧 ready 立即失效，Task 在同一数据库事务内因 `AGENT_NOT_READY` 被拒绝。
13. daemon 重启后 Profile、Session generation、ready 状态和既有 Task 仍可读取。

## 10. 完成记录

V0 已于 2026-08-15 完成实现、独立审核、全量测试、真实 tmux 双 Agent 闭环与 daemon 重启持久化验收。详情见 [Go V0 tmux E2E 验证报告](../reports/validation/2026-08-15-agentbus-go-v0-tmux-e2e.md)。

V0.1 角色生命周期已于 2026-08-16 完成实现、两轮代码审核、全量测试与真实 `%50/%51/%52` 角色恢复验收。该版本以当前契约正确性为准，不承诺兼容旧数据库、旧 API 或旧 CLI。详情见 [角色 Bootstrap E2E 验证报告](../reports/validation/2026-08-16-agent-role-bootstrap-e2e.md)。
