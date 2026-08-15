---
doc_type: architecture
status: draft
canonical: true
updated_at: 2026-08-15
---

# AgentBus 架构设想

> 状态：架构草案。本文描述基于当前讨论形成的建议基线，供实现与评审使用。选型推导、备选路线和调研证据见 [IMPLEMENTATION_DISCUSSION.md](IMPLEMENTATION_DISCUSSION.md)。

## 1. 定位

AgentBus 是一个**异构 Agent Runtime 的通信与协作控制面**。它让 Codex、AGY、Claude Code、OpenCode 等终端 Agent 互相表现为可调用的远程 sub-agent，同时保留各自 Runtime、会话、工具和权限模型。

首版采用 Coordinator/Worker 角色模型：Codex CLI 作为 Northbound Coordinator，负责需求理解、拆解与协调；一个 AGY CLI 作为 AgentBus Agent，另一个 AGY CLI 作为 Quote Service Agent。AgentBus 只负责注册、路由、状态和消息，不承担需求推理。

AgentBus 不是：

- 新的推理模型或 Coding Agent；
- 把所有 Agent 上下文拼成一个共享 prompt 的聊天室；
- 只负责把字符串塞进其他终端的 PTY 工具；
- 假定 exactly-once 执行的普通消息队列；
- 默认允许多个 Agent 同时修改同一工作区的并行执行器。

## 2. 架构目标

- **异构接入**：Runtime 协议差异由 Adapter 层吸收。
- **任意发起方**：任何已授权 Agent 都能提交、补充、观察和取消任务。
- **会话隔离**：每个任务上下文与 provider session/thread/conversation 明确绑定。
- **可靠状态**：Task、RunAttempt、Event、Artifact 和 Lease 有持久事实源。
- **受控并发**：任务并发不造成 session 串线或共享工作区破坏。
- **渐进降级**：Runtime 能力不对称时，明确返回 native、best-effort、queued 或 unsupported。
- **协议可演进**：内部模型不绑定某个 CLI；南向优先 ACP，远程边界预留 A2A。
- **默认安全**：身份、权限、工作区、委派链和凭据均 fail closed。

## 3. 总体架构

```text
                             User
                              │
                              ▼
              Codex Northbound Coordinator
                   理解 / 拆解 / 协调
                              │ AgentBus CLI
                              ▼
┌──────────────────────── AgentBus ────────────────────────┐
│ Northbound API                                           │
│ V0 Local CLI / Watch；后续 MCP                            │
├───────────────────────────────────────────────────────────┤
│ Registry & Capability Matrix                             │
│ Router / Scheduler / Admission Control                   │
│ Task Manager / Run Supervisor / Session Broker           │
│ Policy / Approval / Delegation Guard                     │
│ Workspace Coordinator / Lease Manager                    │
│ Event Store / Transactional Outbox / Artifact Store      │
├───────────────────────────────────────────────────────────┤
│ Connector / Adapter Host                                 │
│ V0 TmuxConnector；后续 ACP / Native Adapter              │
└───────────────┬──────────────────────────────┬────────────┘
                │                              │
     TmuxConnector 通知                后续协议
                │                      MCP / ACP / A2A
       ┌────────────┼────────────┐           │
       ▼            ▼            ▼           ▼
 AgentBus Agent  Quote Service  Dashboard  Remote Runtime
   AGY CLI        AGY CLI        Future      or Hub
```

### 3.1 三个协议边界

| 边界 | 首选协议 | 职责 |
|---|---|---|
| Agent/User → AgentBus | V0 为 `agentbus` CLI；后续 MCP tools | 提交、补充、查询、观察、取消和取回结果 |
| AgentBus → 现有交互 Agent | V0 为 TmuxConnector 通知 + Agent 主动 CLI 取件 | pane 注册、存活检查、短通知注入；不解析终端输出 |
| AgentBus → Coding Runtime | 后续 ACP；必要时 Native Adapter | session、prompt、事件、权限、文件/终端和取消 |
| AgentBus ↔ Remote Agent/Hub | A2A Gateway，后续实现 | 跨主机发现、长任务、状态、Artifact 与标准认证 |

内部 Task/Run/Event 模型是事实源。MCP、ACP 和 A2A 都是边界协议，不直接替代内部调度状态。

## 4. 核心组件

### 4.1 Northbound API

面向 Agent 和人类暴露统一操作。V0 先提供本地 CLI：

```text
agentbus agent register/list/get
agentbus task submit/get/list
agentbus task ack/status/send
agentbus task complete/fail/cancel
agentbus task watch
```

Coordinator 通过 CLI 提交任务，立即取得 `task_id`，再通过 watch/get 获取状态。目标 Agent（如 Quote Service Agent / AgentBus Agent）收到 TmuxConnector 的短通知后调用 `task get <task-id> --agent <agent-id>` 获取完整内容，并显式调用 `ack/status/complete/fail`。MCP tools 与 `delegate_and_wait` 便利操作留到后续，但必须复用同一服务层和状态机。

### 4.2 V0 TmuxConnector

TmuxConnector 是透明通知层，只负责：

- 保存逻辑 Agent 到 `session:window.pane` 的映射；
- 使用 tmux 原生命令检查 pane 是否存在、是否死亡；
- 在新任务或补充消息到达时注入不含完整任务正文的短通知；
- 返回 `notified` 或 `delivery_failed` 处置结果并记录事件。

安全要求：

- Go 进程直接以 argv 调用 `tmux`，不拼接 shell 命令；
- 使用唯一 tmux buffer + `load-buffer`/`paste-buffer` 注入，避免 shell quoting 和共享 buffer 竞争；
- 仅注入由 AgentBus 自己生成的固定模板和已校验 ID；
- 不捕获或解析 pane 输出判断任务状态；
- Agent 必须通过 CLI 主动读取内容并显式上报结果。

### 4.3 Registry & Capability Matrix

Registry 保存：

- Agent profile 与逻辑地址；
- Adapter 类型、版本和配置；
- Runtime instance 与 provider 版本；
- health、readiness、auth state；
- capabilities 与 degradation mode；
- 可接受的 workspace、模型、权限和并发范围。

进程存在只代表 `alive`，不代表 `ready`。只有通过 probe、认证、最小 session/turn 检查后，才能进入可调度状态。

### 4.4 Router / Scheduler

负责：

- 按显式 target 派发；
- 后续按 profile/capability 选择 Runtime；
- 校验身份、权限、委派深度、预算和 deadline；
- 执行 admission control、quota 和 workspace lease；
- 选择健康 Adapter instance；
- 创建 RunAttempt 并交给 Run Supervisor。

V0 只支持显式 target，不做模型自行猜测的自动路由。

### 4.5 Task Manager / Run Supervisor

Task Manager 维护稳定任务意图，Run Supervisor 管理一次具体 Runtime 尝试：

- command idempotency；
- Task 与 RunAttempt 状态转换；
- 启动、心跳、超时、取消和进程监督；
- transient failure 分类；
- retry 或 uncertain reconcile 决策；
- terminal result 与 Artifact 生成。

### 4.6 Session Broker

Session Broker 将 AgentBus 内部 `context_id` 与 provider 会话绑定：

```text
(context_id, agent_profile, runtime_instance)
    → provider_session_id / thread_id / conversation_id
```

约束：

- provider ID 不能直接当成全局 Task ID；
- 同一 context 调用不同 Agent 时，各自保持独立 binding；
- resume 前必须验证 Adapter、provider version 和 workspace 兼容性；
- fork 创建新的 binding，并记录父子关系；
- session 关闭不删除 Task、Event 和 Artifact 历史。

### 4.7 Policy / Approval / Delegation Guard

负责把各 Runtime 的 permission/elicitation 统一映射成：

- `waiting_approval`；
- `waiting_input`；
- `rejected`。

委派必须携带 `parent_task_id`、ancestor chain、depth/hop、deadline、token/cost budget。系统拒绝：

- 当前 Task 再委派给祖先形成的环；
- 超过最大深度或 hop；
- 子任务权限大于父任务授权；
- 未授权 target、workspace 或外部能力；
- 预算已耗尽后继续派生任务。

### 4.8 Workspace Coordinator

Workspace Coordinator 是 Coding Agent 场景的关键安全边界：

- 对 workspace/path scope 建立 read/write lease；
- 同一工作区默认只允许一个 writer；
- 并发 writer 必须使用隔离 git worktree；
- 记录任务开始前的 commit、dirty、staged/unstaged baseline；
- 使用 fencing token 防止过期 Worker 继续写；
- 完成或异常后执行 diff/staging reconcile；
- 不允许 Runtime 文本自行扩大写入范围。

### 4.9 Event Store & Transactional Outbox

所有状态变化先在同一事务中写入 authoritative store，再写 outbox。Dispatcher 将事件投递到 MCP watcher、SSE/TUI 或可选终端 Connector。

事件约束：

- 不可变；
- 每个 Task 有单调递增 `sequence`；
- 至少一次投递；
- 消费者用 `event_id` 幂等；
- Watcher 断线后按 sequence replay；
- Runtime 原始事件可保存在 namespaced payload，标准字段由 normalizer 提取。

### 4.10 Artifact Store

关键结果不能只存在一次性 stream message 中。Artifact 至少支持：

- 文本总结；
- 结构化 JSON；
- patch/diff；
- 文件或目录 URI；
- 测试与验证报告；
- Runtime 原始输出索引。

Artifact 带内容类型、大小、checksum、producer run、workspace revision 和访问策略。

## 5. Canonical Domain Model

### 5.1 核心对象

| 对象 | 作用 | 关键字段 |
|---|---|---|
| `AgentProfile` | 可路由的逻辑 Agent | `agent_id`, `adapter_type`, `capabilities`, `policy_scope` |
| `Task` | 稳定工作意图 | `task_id`, `context_id`, `target`, `status`, `parent_task_id`, `budget`, `deadline` |
| `RunAttempt` | 一次实际派发 | `run_id`, `task_id`, `attempt_no`, `adapter_instance`, `status`, `failure_class` |
| `SessionBinding` | 内部上下文到 provider session 的映射 | `binding_id`, `context_id`, `agent_id`, `provider_session_id`, `workspace_id` |
| `Message` | 指令、补充、澄清与回复 | `message_id`, `task_id`, `role`, `reply_to`, `causation_id`, `idempotency_key` |
| `Event` | 不可变生命周期事实 | `event_id`, `task_id`, `run_id`, `sequence`, `type`, `payload` |
| `Artifact` | 可持久取回的任务产物 | `artifact_id`, `task_id`, `run_id`, `media_type`, `uri`, `checksum` |
| `Lease` | Worker、终端输入或工作区所有权 | `lease_id`, `resource`, `owner`, `expires_at`, `fencing_token` |
| `Approval` | 权限或输入决策 | `approval_id`, `task_id`, `request`, `status`, `decided_by` |

### 5.2 标识与关联规则

- 所有 creator-generated command 都携带 `idempotency_key`。
- `task_id` 在 retry 中不变；每次派发新建 `run_id`。
- `context_id` 表示协作上下文，不等于某个 provider session。
- `message_id` 用于去重；`reply_to` 表示回复关系；`causation_id` 表示因果来源；`correlation_id` 可关联一次跨 Task 操作。
- `parent_task_id` 与 ancestor chain 用于防循环和预算继承。
- Event sequence 只要求 Task 内单调，不要求全局严格排序。

### 5.3 建议存储表

```text
agent_profiles
adapter_instances
tasks
run_attempts
session_bindings
messages
events
artifacts
leases
approvals
outbox
```

V0 使用单进程 SQLite WAL。Task 状态、Event 和 outbox 必须在事务内一致更新。迁移到 PostgreSQL 后保持同一领域约束，不把消息中间件当 authoritative store。

## 6. 状态机

### 6.1 Task 主状态

```text
queued
  │
  ▼
dispatching ───────→ rejected
  │
  ▼
running ───────────→ waiting_input ──┐
  │                 waiting_approval ├──→ running
  │                                  │
  ├───────────────→ cancel_requested ┴──→ canceled
  │
  ├───────────────→ succeeded
  ├───────────────→ failed
  └───────────────→ uncertain
```

### 6.2 状态语义

- `queued`：已持久化，尚未建立 RunAttempt。
- `dispatching`：正在获取 lease、session 和 Runtime 资源。
- `running`：Runtime 已确认进入本次 turn/工作单元。
- `waiting_input`：需要发起方补充信息。
- `waiting_approval`：需要有权限的主体批准工具、路径或权限升级。
- `cancel_requested`：已请求取消，尚未获得可靠终态。
- `succeeded`：Task 达到完成条件，关键结果已形成 Artifact。
- `failed`：已知失败，且副作用边界可诊断。
- `canceled`：Runtime/进程监督已确认停止并完成必要收尾。
- `rejected`：调度前因权限、能力、预算、环路或输入无效而拒绝。
- `uncertain`：Runtime 失联，且可能已经产生文件或外部副作用。

### 6.3 Retry 规则

只有以下情况可自动 retry：

- 可证明 prompt 尚未被 Runtime 接受；
- Adapter probe/启动在创建 session 前失败；
- 读任务明确无外部副作用，且 policy 允许；
- failure class 被显式列入安全重试表。

写任务进入 `uncertain` 后必须先 reconcile workspace、provider session 和 Artifact；不得像普通队列任务一样盲目重投。

## 7. Adapter Contract

下面是逻辑契约，不是冻结的语言级 API：

```typescript
interface AgentAdapter {
  probe(): Promise<AdapterProbe>;
  startRuntime(request: RuntimeStartRequest): Promise<RuntimeHandle>;
  openOrResumeSession(request: SessionRequest): Promise<SessionHandle>;
  runTurn(request: TurnRequest): AsyncIterable<NormalizedEvent>;
  steer(request: SteerRequest): Promise<SteerDisposition>;
  cancel(request: CancelRequest): Promise<CancelDisposition>;
  snapshot(request: SnapshotRequest): Promise<RuntimeSnapshot>;
  close(request: CloseRequest): Promise<void>;
}
```

`steer()` 必须返回真实处置结果：

```text
accepted      Runtime 原生接受当前 turn 补充输入
best_effort   已尝试终端/非正式注入，不能保证 Runtime 接受
queued        已持久化，将在下一 turn 发送
unsupported   当前 Adapter 不支持
```

`cancel()` 同样只返回请求处置，不直接决定 Task 终态。

### 7.1 Capability Matrix

基础能力：

```text
new_session
resume_session
run_turn
stream_events
cancel
health
```

可选能力：

```text
mid_turn_steer = native | best_effort | queued | none
fork_session
structured_output
permission_bridge
subagent_events
file_events
usage
background
```

Router 必须根据 capability 和 policy 做准入，不能因统一接口存在就假装所有 Runtime 能力相同。

## 8. Runtime 接入基线

以下 Runtime Adapter 是 V1/V2 演进基线，不属于 Go V0。V0 中 Codex 与 AGY 都是已经运行的交互式进程，通过 AgentBus CLI 和 TmuxConnector 协作。

### 8.1 Codex Adapter（后续）

```text
GenericAcpAdapter
    ↓ ACP stdio
codex-acp
    ↓
Codex App Server
```

优先复用 `codex-acp`。只有 ACP 映射缺失且业务确有需要时，才增加 Direct Codex App Server Adapter，并保持同一 normalized event contract。

### 8.2 Claude Adapter（后续）

```text
GenericAcpAdapter
    ↓ ACP stdio
claude-agent-acp
    ↓
Claude Agent SDK
```

保留 namespaced extension metadata，以支持嵌套 subagent、TODO、usage 等尚未标准化信息。运行环境基线为 Node 22 或更高兼容版本。

### 8.3 OpenCode Adapter（后续）

```text
GenericAcpAdapter
    ↓ ACP stdio
opencode acp
```

V0/V2 首选原生 ACP。只有远端独立部署等明确场景才增加 OpenCode HTTP Adapter，且必须开启认证，不允许裸露 Server。

### 8.4 AGY Adapter（后续）

默认：

```text
AgyBatchAdapter
    ↓ process + stream-json
agy --print --conversation ...
```

语义：

- provider conversation ID 持久绑定；
- active turn 的 follow-up 持久排队；
- `mid_turn_steer=queued`；
- 进程输出、退出状态、实际工作树和独立验证共同决定结果；
- 顶层 `ERROR` 不直接覆盖已产生的 Artifact/副作用事实。

可选 `HcomTerminalConnector` 提供 best-effort 注入和唤醒；实验性社区 ACP Adapter 必须 feature flag、锁版本、通过 conformance test 与人工政策 gate。

## 9. 端到端任务流程

### 9.1 提交与执行

```text
1. Caller 通过 MCP/CLI 提交 task.submit(idempotency_key, target, workspace, policy)
2. Northbound API 认证 Caller，持久化 Task + queued Event
3. Router 校验 target、capability、delegation、budget 和 deadline
4. Workspace Coordinator 获取 read/write lease
5. Scheduler 创建 RunAttempt，状态变为 dispatching
6. Session Broker 新建或恢复 provider session binding
7. Adapter 启动 turn，确认后状态变为 running
8. Runtime update 规范化并写入 Event Store + outbox
9. Runtime 结束后生成 Artifact，执行 workspace/result validation
10. Task 进入 succeeded/failed/uncertain，释放或转交 lease
11. Caller watch/poll/replay 获取终态和 Artifact
```

### 9.2 执行中补充消息

```text
task.send
  ├─ native steer     → 当前 turn 接受
  ├─ best_effort      → 终端注入，仅报告 delivery disposition
  ├─ queued           → 下一 turn 使用同一 SessionBinding
  └─ unsupported      → 返回明确错误，不丢弃消息
```

### 9.3 父子任务

父 Agent 提交子 Task 时，Hub 继承并收紧 policy、budget、deadline 和 workspace scope。子任务 Event 可向父 Task 投影摘要，但原始 session 和 event sequence 保持独立。子 Task 完成后以 Artifact 返回，不把全部子上下文无条件复制进父会话。

## 10. 消息、事件与投递语义

- Command：至少一次到达 API，通过 idempotency key 实现幂等。
- Runtime prompt：不承诺 exactly-once；断线时依靠 RunAttempt 与 reconcile。
- Event：持久化后至少一次投递，消费者幂等。
- Watch：支持按 Task sequence 恢复，不能只依赖在线 push。
- Result：必须落 Artifact；transient message 只用于提示和增量体验。
- Outbox：只有持久化事务提交后才允许对外发布事件。
- Backpressure：Scheduler 有最大运行数、每 Adapter 并发数、每 workspace writer 数和 watcher buffer 上限。

## 11. 并发和隔离

### 11.1 并发维度

AgentBus 分别限制：

- 全局 active Run 数；
- 每个 Adapter/Runtime instance 的 session/turn 数；
- 每个 Agent profile 的 quota；
- 每个 workspace 的 reader/writer 数；
- 每个 delegation tree 的并发子任务数；
- 每个 Caller 的 token/cost/deadline budget。

### 11.2 工作区规则

```text
同一 workspace:
  read + read       允许（受限于 Runtime 行为）
  read + write      默认拒绝或使用 snapshot/worktree
  write + write     拒绝

独立 git worktree:
  write + write     可允许，每个 worktree 独立 lease
```

任何 Adapter 都不得绕过 Workspace Coordinator 直接声明自己拥有写权限。

## 12. 身份、权限与安全

### 12.1 身份与地址分离

消息中的 `from`/`to` 只是逻辑路由地址。授权依据必须是经过认证的 principal、project/tenant scope 和 task policy，不能信任 Agent 自报身份。

### 12.2 本机与远程传输

- 本机优先 stdio 或 Unix domain socket。
- 若在本项目环境提供本机 HTTP，使用 `rtx4090` 作为本地地址并强制 token，不使用 `127.0.0.1`/`localhost` 示例。
- 跨主机必须使用 TLS；跨信任域采用 OAuth/OIDC、mTLS 或等价标准机制。
- 不把 hcom 的共享全信任 relay 直接当成多租户授权模型。

### 12.3 凭据与日志

- 凭据通过环境注入或 credential broker 提供，并使用 allowlist；
- 子任务只能继承父任务授权的凭据子集；
- Message、Event、Artifact 和日志统一执行敏感字段脱敏；
- 原始 Runtime 输出设定保留期和访问控制；
- 所有审批、权限提升、取消和人工操作写审计事件。

### 12.4 Fail Closed

以下情况默认拒绝执行或进入等待态：

- principal、target、workspace scope 不明确；
- Adapter capability/版本未知；
- writer lease 状态不确定；
- 子任务请求扩大权限；
- 任务形成委派环；
- 预算、deadline 或深度超过限制；
- 可能已有副作用但无法确认 Runtime 状态。

## 13. 部署与技术基线

### 13.1 V0 单机部署

```text
agentbus daemon          Go 1.22 单二进制
agentbus.sqlite          SQLite WAL
artifact directory       本地受控目录
tmux CLI                 V0 通知传输
agentbus CLI             V0 北向与 Agent 回传入口
```

Go V0 通过 Unix socket 暴露本机 API，同一个 `agentbus` 二进制同时提供 daemon 和 CLI。选择 Go 是为了得到易部署的单二进制、明确的并发模型和较小运行依赖；SQLite 使用 WAL 模式。后续 ACP/MCP 可以作为 Go 内部模块或独立 Adapter host 接入，不改变 Task/Event 事实模型。

### 13.2 后续分布式部署

只有在多主机、多 Worker 或跨信任域成为真实需求后才升级：

- PostgreSQL 作为 authoritative store；
- transactional outbox 继续存在；
- 确有事件吞吐需求时增加 NATS JetStream；
- A2A Gateway 对外暴露 Agent Card、Task、Message 和 Artifact；
- OAuth/mTLS/RBAC、tenant/project scope、quota 和 retention。

不为 V0 提前引入 Redis、Kafka 或分布式一致性复杂度。

## 14. 可观测性

最低指标：

- Task 各状态数量与停留时长；
- dispatch、queue 和 runtime turn 延迟；
- RunAttempt 成功率、failure class、uncertain 数；
- Adapter health/readiness/version mismatch；
- workspace lease 冲突、过期和 reconcile 结果；
- event/outbox backlog 与 watcher replay；
- permission/approval 等待时长；
- token/usage/cost（Runtime 支持时）；
- Artifact 生成、校验和取回错误。

日志以 `task_id`、`run_id`、`context_id`、`adapter_instance_id` 和 `correlation_id` 关联，不记录未脱敏的 prompt 或凭据。

## 15. 演进路线

### P0：兼容性验证

- hcom Codex ↔ AGY 终端互通；
- ACP Adapter conformance harness；
- AGY staging/workspace 风险验证；
- CLI/Adapter 版本锁定；
- Go 1.22、SQLite driver 与 tmux 可用性门禁。

### V0：现有交互 Agent 最小闭环

```text
Codex Coordinator CLI (%50) ───┐
AGY AgentBus Agent CLI (%51) ──┼──> AgentBus Hub ──> TmuxConnector ──> 目标 Agent Pane
AGY Quote Service CLI (%52) ───┘
目标 Agent CLI (ack/status/complete) ──> AgentBus Hub ──> watch/get (Coordinator)
```

实现 Go daemon/CLI、SQLite WAL、Agent Registry、Task/Message/Event 最小状态机和 TmuxConnector。V0 不启动 Runtime，不实现 AgyBatch/ACP/MCP，也不解析 pane 输出。

### V1：双向与恢复

增加 MCP northbound、Artifact、workspace lease、审批/输入等待、取消确认、crash recovery、uncertain reconcile 和 event replay；根据需要加入 Codex ACP Adapter。

### V2：四类 Runtime

加入 Claude 和 OpenCode ACP，统一 Generic ACP Adapter、capability routing、隔离 worktree、circuit breaker 和 SSE/TUI。

### V3：远程 Agent 网络

加入 A2A Gateway、PostgreSQL、多 Worker 与跨信任域认证授权。

## 16. 架构不变量与验收基线

1. Hub 核心不依赖某个 Runtime 的私有 session ID 或事件结构。
2. 相同 idempotency key 不会重复创建 Task/Run 或发送 prompt。
3. Task retry 新建 RunAttempt，不改变 Task identity。
4. `cancel_requested` 不等于 `canceled`。
5. 可能已产生写副作用的 lost run 进入 `uncertain`，不自动重投。
6. 同一 workspace 默认只有一个 writer；并行 writer 使用独立 worktree。
7. 子任务权限和预算只能继承后收紧，不能自行扩大。
8. AGY follow-up 的 queued/best-effort/native 语义必须如实暴露。
9. 关键结果以 Artifact 持久化，Watcher 重连可 replay。
10. Adapter readiness 来自实际 probe 和兼容性门禁，不来自“进程存在”。

## 17. 当前未决项

- AgentBus 目前先位于本工作区的 `AgentBus/`，稳定后是否拆为独立仓库。
- MCP Server 后续与 daemon 同进程还是独立进程。
- Artifact 的本地目录布局、内容寻址和保留策略。
- SessionBinding 在 Runtime 大版本升级后的自动迁移策略。
- namespaced Runtime extension 的版本化规则。
- hcom Connector 是直接调用 hcom，还是实现更窄的桥接进程。
- AGY 官方程序化协议出现后的迁移与兼容周期。

这些问题不影响先完成 P0/V0，但必须在对应实施计划中明确决策和验收条件。
