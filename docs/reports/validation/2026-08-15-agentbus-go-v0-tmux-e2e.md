---
doc_type: validation_report
status: passed
validated_at: 2026-08-15
---

# AgentBus Go V0 tmux E2E 验证报告

## 1. 结论

AgentBus Go V0 已通过代码审查、自动化验证、真实 tmux 双 Agent 协作以及 daemon 重启持久化验收。当前 daemon 留在 `%53` 运行并输出 JSON debug 日志。

## 2. 实际拓扑

```text
%50  Codex Coordinator       agent_id=coordinator
%51  AGY AgentBus Agent      agent_id=agentbus-agent, role=agentbus
%52  AGY Quote Service Agent agent_id=quote-service, role=quote
%53  AgentBus daemon/log
```

运行端点：

```text
socket: AgentBus/run/agentbus.sock (0600)
db:     AgentBus/data/agentbus.db
```

## 3. 自动化验证

```text
gofmt -l .                         PASS（无输出）
go vet ./...                       PASS
go test -count=1 ./...             PASS（全部包）
go test -race -count=1 ./...       PASS（全部包，0 race）
go build -o bin/agentbus ...       PASS
```

覆盖重点包括 participant-only 读取、状态权限、SQLite active-task partial unique index、live/stale/replaced socket、TmuxConnector dead pane 与失败路径、supplemental message 回读。

## 4. AgentBus Agent 真实任务

```text
task_id: task-b00c409e-ed11-486c-8e12-a0e9406bf1cd
target:  agentbus-agent (%51)
result:  agent_id=agentbus-agent handshake=ok
```

Event 链：

```text
task.submitted
task.notified (pane_id=%51)
task.acknowledged
task.status_updated
task.succeeded
```

## 5. Quote Service Agent 与补充消息

```text
task_id: task-5b6c60e3-0b26-4395-8a8a-5f1363f8432b
target:  quote-service (%52)
result:  agent_id=quote-service handshake=ok supplement=ok
```

Agent 先 ACK 并上报 `waiting-for-supplement`。Coordinator 通过 `task send` 写入 `supplement=ok`，AgentBus 记录 `task.message_sent`，TmuxConnector 再次向 `%52` 发送短通知；Agent 重新 `task get` 后完成任务。最终 Task Messages 包含 instruction、status update 与 supplement。

## 6. 重启与持久化

确认两条任务均为终态后，对 `%53` daemon 执行一次 graceful shutdown 并重新启动：

- 旧 socket 被 ownership-aware cleanup 正确删除；
- 新 daemon 重新创建 `0600` socket；
- 三个 Agent 注册信息仍存在；
- 两条 Task、全部 Messages、Event sequence 及结果仍可读取；
- daemon 最终继续留在 `%53` 运行。

## 7. 安全边界

- V0 是单机同 Unix 用户信任域，Unix socket 权限为 `0600`；Agent ID 是自报身份，不是强认证。
- TmuxConnector 只注入包含 Task ID 的短通知，不注入任务正文，不解析 pane 输出。
- 所有任务状态判断与验收结论均来自 AgentBus CLI/SQLite Event，不以终端画面推断成功。
- V0 使用 `mattn/go-sqlite3`，构建需要 CGO/GCC。
