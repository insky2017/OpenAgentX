---
doc_type: validation_report
status: passed
validated_at: 2026-08-16
---

# AgentBus V0.1 角色 Bootstrap tmux E2E 验证报告

## 1. 结论

AgentBus V0.1 已通过独立代码审核、全量 Go 验证、真实三 Agent 角色恢复、generation ready gate 以及 daemon 重启持久化验收。当前实现以 V0.1 契约为准，不承担旧数据库、旧 API 或旧 CLI 的兼容义务。

## 2. 实际拓扑

```text
%50  Codex Coordinator       agent_id=coordinator, role=coordinator
%51  AGY AgentBus Agent      agent_id=agentbus-agent, role=agentbus
%52  AGY Quote Service Agent agent_id=quote-service, role=quote
%53  AgentBus daemon/log
```

运行端点：

```text
socket: AgentBus/run/agentbus.sock (0600)
db:     AgentBus/data/agentbus.db
daemon: PID 699958，命令 ./AgentBus/bin/agentbus serve --log-level debug
```

## 3. 自动化验证

```text
gofmt -l .                               PASS（无输出）
go vet ./...                             PASS
go test -count=1 ./...                   PASS（全部包）
go test -race -count=1 ./...             PASS（全部包，0 race）
go build -o bin/agentbus ./cmd/agentbus  PASS
git diff --check                         PASS
```

覆盖重点包括 Manifest 严格解析、ROLE 文件校验、generation CAS、delivery failure、事务内 ready gate、通知身份提醒、launcher fail-closed、Unix socket API 与 daemon 重启读取。

## 4. 三 Agent Bootstrap

- Coordinator 使用 `agents/coordinator/agent.yaml` 执行 `whoami` 与 `attach --no-notify`，读取 ROLE 后以 generation 1 ready；workspace 解析到 SteadyFlow 根目录。
- `%51` 收到 Bootstrap 短通知，读取 `agents/agentbus-agent/ROLE.md` 后以 generation 1 ready。
- `%52` 收到 Bootstrap 短通知，读取 `agents/quote-service/ROLE.md` 后以 generation 1 ready。
- 三者 `session show` 均返回 canonical Profile 和 `status=ready`。

## 5. 真实 Task 闭环

AgentBus Agent：

```text
task_id: task-19c7595f-ff14-4692-8bc0-31cc9e971991
result:  agent_id=agentbus-agent role=agentbus role_bootstrap=ok
status:  succeeded
```

Quote Service Agent：

```text
task_id: task-59b1000c-c1b3-4cd4-bfb3-262e1a090ba4
result:  agent_id=quote-service role=quote role_bootstrap=ok
status:  succeeded
```

两条 Task 的状态均来自 AgentBus API/Event，不依赖 pane 输出判断。

## 6. Generation Ready Gate

Quote Service Agent 重新 bootstrap 后进入 generation 2、`status=bootstrapping`。Coordinator 在其 ready 前立即 submit，服务返回：

```text
HTTP 409
code: AGENT_NOT_READY
```

Quote Service Agent 重新读取 ROLE 并执行 generation 2 ready 后，恢复任务成功：

```text
task_id: task-b2468c53-6990-40b4-8c0a-9f07004821d8
result:  quote-ready-generation-2-ok
status:  succeeded
```

该过程证明旧 generation ready 不会误确认新 session，且 Task gate 在角色未恢复时 fail closed。

## 7. 重启持久化

最终重启 `%53` daemon 后：

- Coordinator：generation 1，ready；
- AgentBus Agent：generation 1，ready；
- Quote Service Agent：generation 2，ready；
- 上述 Task、result、Profile 与 Session 均可重新读取；
- socket 权限保持 `0600`，daemon 持续输出结构化日志。

## 8. 非阻断观察

- `agent launch` 在取消 attach context 后未显式 join attach goroutine；当前 HTTP/Tmux 调用链尊重 context，buffered result 不会阻塞，本轮未观察到泄漏。
- 已存在 Agent 的 attach response 中 `created_at` 使用本次请求对象时间，而 `session show` 返回数据库原始时间；不影响角色生命周期，后续可通过 Store reload 统一响应。

以上两项不影响 V0.1 验收，不构成兼容性问题。
