---
doc_type: research_discussion
status: discussion
canonical: false
updated_at: 2026-08-15
---

# AgentBus 实现方式讨论与选型推演

> 本文保存 AgentBus 从需求、现有项目调研到架构取舍的过程信息，便于后续追溯“为什么这样设计”。它不是冻结的接口契约。当前建议基线见 [ARCHITECTURE.md](ARCHITECTURE.md)。

## 0. 2026-08-15 实施方向调整

用户进一步明确了首版运行形态：现有 tmux `AgentBus` session 中的 Codex CLI 是 Coordinator Agent；当前负责 AgentBus 代码的 AGY CLI 作为 AgentBus Agent，另一个 AGY CLI 作为 Quote Service Agent。三者都作为 AgentBus 节点接入，而不是由 Hub 启动新的 batch Runtime。

因此 V0 调整为：

```text
User
  ↓
Codex Coordinator（现有 tmux pane）
  ↓ AgentBus CLI
AgentBus daemon（Go）
  ↓ TmuxConnector 通知
AGY AgentBus Agent / AGY Quote Service Agent（现有 tmux panes）
  ↓ AgentBus CLI 显式 ACK/状态/结果
AgentBus
  ↓ watch/get
Codex Coordinator
```

本轮采用以下新决策：

- 实现语言由早期建议的 TypeScript/Node 22 改为 **Go 1.22**；
- V0 northbound 只实现 CLI，MCP 留到后续；
- V0 不实现 AgyBatch、ACP、A2A、自动路由或 Runtime 生命周期管理；
- TmuxConnector 只负责逻辑 Agent 与 pane 的映射、存活检查和短通知注入；
- 完整任务内容由 Agent 主动调用 `task get` 获取；
- 完成状态由 Agent 显式调用 `task ack/status/complete/fail` 上报，不解析 pane 输出推断；
- Coordinator/Quote 是业务角色，Codex/AGY 是 Runtime 类型，二者不混为一个字段。

早期 ACP-first、MCP 和 AgyBatch 分析仍作为后续演进依据保留，不再属于本次最小实现范围。

## 1. 问题定义

目标不是再实现一个 AI Agent，而是提供一层异构 Coding Agent 协作基础设施，使 Codex、Antigravity CLI（下文简称 AGY）、Claude Code、OpenCode 及未来 Runtime 能够：

- 任意一方发起任务或发送补充消息；
- 将其他 Agent 当作远程 sub-agent 使用；
- 保持各 Runtime 独立的 session/context，不混合上下文；
- 支持多个任务并发运行，并正确隔离路由、状态、结果和工作区；
- 在执行过程中处理补充输入、审批、取消和异常恢复；
- 以可持久取回的结果或 Artifact 返回执行成果；
- 在不改变 Hub 核心模型的前提下继续增加新的 Agent Runtime。

Hub 的职责应收敛为：

```text
Agent 注册
任务下发
消息路由
Session 绑定
任务与运行状态
事件与结果返回
并发和工作区协调
权限、审批与审计
```

## 2. 最初设想：统一 Adapter 和自定义消息

最初方案是让每个 Runtime Adapter 实现一组最小方法：

```python
class AgentAdapter:
    spawn(task) -> session_id
    send(session_id, message)
    interrupt(session_id)
    get_status(session_id)
    wait(session_id) -> Event
```

Hub 只看到统一的消息信封：

```json
{
  "id": "msg-001",
  "task_id": "task-001",
  "session_id": "codex:xxx",
  "from": "codex/main",
  "to": "agy/main",
  "type": "task",
  "content": "...",
  "reply_to": null
}
```

这个方向抓住了“协议差异应停留在 Adapter 层”的核心，但有三个不足：

1. `spawn/send/wait` 太粗，无法准确表达审批、输入等待、取消确认、断线后副作用不确定等状态。
2. `task/message/result/error/interrupt/status` 只是消息分类，不能替代 Task、RunAttempt、Session 和 Artifact 的生命周期。
3. Codex、Claude、OpenCode 已有 ACP 或更丰富的原生接口，完全自定义南向协议会重复实现 session、permission、terminal 和流式事件语义。

因此后续设计保留“统一 Adapter”，但改为“内部 canonical model + 分层标准协议”，不再用一个简单消息结构承担所有职责。

## 3. 现有方案调研

本节结论基于 2026-08-15 的仓库与本地工具快照。相关项目更新很快，正式实施前仍需运行兼容性测试。

### 3.1 hcom：最接近终端协作体验

[hcom](https://github.com/aannoo/hcom) 已覆盖 Codex、AGY、Claude Code、OpenCode 等终端 Agent，主要通过 hooks、PTY、SQLite 事件库和进程管理实现：

- 跨终端发送消息、watch 和订阅；
- 启动、fork、resume、kill Agent；
- 向活跃 turn 注入输入，或唤醒 idle Agent；
- `request|inform|ack` intent、reply、thread、bundle 和 read receipt；
- 本机消息游标、ack 与重试；
- 可选跨设备 relay。

它非常适合验证 Codex 与 AGY 的真实终端体验，也适合以后作为 AgentBus 的 `TerminalConnector` 或 launcher。

但 hcom 不宜直接成为 AgentBus 的唯一事实源：

- “消息已注入终端”不等于 Runtime 已接受任务，更不等于任务成功；
- 核心存储更接近事件与实例绑定，不是完整的 Task/RunAttempt 状态机；
- PTY 注入依赖输入框、hook 时机和终端状态，只能给出 best-effort 语义；
- relay 使用共享信任域，缺少多租户所需的细粒度角色、撤销和项目级授权。

结论：**P0 优先试用，长期作为可选终端连接层，不作为 authoritative task store。**

### 3.2 cliagents：最接近服务化控制面

[cliagents](https://github.com/suyashb734/cliagents) 提供 HTTP、WebSocket、MCP、OpenAI-compatible API，以及 session、task、root 和 orchestration 等概念。值得借鉴的设计包括：

- Adapter capability/readiness；
- root/session lineage；
- room/task/run 和 task-session binding；
- idempotency key、dispatch request；
- terminal input lease、heartbeat；
- operator action replay、failure taxonomy 和 inbox retry。

它比 hcom 更接近 AgentBus 的控制面，但当前不适合无条件 fork 为长期基线：

- 项目仍明确处于 alpha；
- 普通 REST 路径与 orchestration 路径存在两套不互通的 session 系统；
- 一部分能力依赖 tmux、CLI 输出模式识别和长驻交互进程；
- 统一 API 的覆盖面很大，也带来较高的兼容和维护成本。

结论：**把数据模型、故障分类和 lease 思路作为参考；PoC 可对比验证，不整体继承其语义债务。**

### 3.3 ACP：适合 Hub 到 Coding Runtime

[Agent Client Protocol（ACP）](https://agentclientprotocol.com/) 定位于“Client/编辑器 ↔ Coding Agent”。研究快照中稳定 wire protocol 为 v1，v2 尚处于 draft。

ACP 已覆盖：

- initialize 与 capability negotiation；
- session new/load/resume/close；
- prompt turn 与流式 session update；
- permission request、elicitation；
- Client filesystem/terminal 能力；
- cancel request。

这些能力正好适合作为 AgentBus 驱动 Runtime 的南向协议。它能保留 Coding Agent 特有的文件、终端、权限和会话语义，避免 Hub 为每个 Runtime 重复发明一套协议。

但 ACP 不是完整的 AgentBus：

- 不负责 Agent Registry、durable task queue 或跨服务授权；
- 不定义 workspace writer lease、RunAttempt、Artifact retention；
- cancel 是 best-effort，Hub 不能在发出 cancel 后立即伪造 `canceled`；
- 标准 stdio 模式主要假设 Client 启动并信任本地 Agent 进程。

结论：**ACP-first，但不是 ACP-only；作为南向接入层，而不是内部全部领域模型。**

### 3.4 A2A：适合跨 Hub 和远程 Agent 服务

[Agent2Agent（A2A）](https://github.com/a2aproject/A2A) 面向不同服务器上的 opaque agent application。研究快照中的已发布规范为 1.0.0，核心对象包括：

- Agent Card 和 skill discovery；
- Message、Task、TaskStatus/TaskState、Artifact；
- `contextId` 与 `taskId`；
- polling、SSE、webhook；
- JSON-RPC、gRPC、HTTP+JSON 等 binding；
- TLS、API key、OAuth/OIDC、mTLS 等跨信任域安全机制。

A2A 对长任务、跨组织发现、状态、产物和订阅很有价值，但不会把本地 CLI 自动变成可靠 Worker，也不定义内部调度、workspace 锁或 attempt 重试。

结论：**作为后续跨 Hub/远程服务 Gateway；V0 不用 A2A 反向绑死内部 Scheduler。**

## 4. 各 Runtime 的可编程入口

| Runtime | 首选接入 | 备选接入 | 讨论结论 |
|---|---|---|---|
| Codex | [`codex-acp`](https://github.com/agentclientprotocol/codex-acp) | Codex App Server | `codex-acp` 直接包装 App Server，优先复用；需要未映射的富能力时再直连 App Server |
| Claude Code | [`claude-agent-acp`](https://github.com/agentclientprotocol/claude-agent-acp) | Agent SDK / 双向 stream-json CLI | ACP Adapter 基于官方 SDK，优先统一接入；实现环境需满足 Node 版本要求 |
| OpenCode | 原生 `opencode acp` | `opencode serve` HTTP/SSE + SDK | 原生 ACP 已覆盖主要会话、权限、工具和事件语义；HTTP 适合特殊远端部署，但必须配置鉴权 |
| AGY | 官方 batch CLI | hcom PTY；实验性社区 ACP | 官方 CLI 路径最保守；运行中 follow-up 先按 queued 处理；社区 Adapter 必须隔离、锁版本并通过政策 gate |

### 4.1 Codex

Codex App Server 提供双向 JSON-RPC 风格接口，以 Thread → Turn → Item 建模，支持 thread start/resume/fork、turn start/steer/interrupt、审批和流式事件。标准本地集成优先 stdio；研究时 WebSocket 仍标记为实验能力，不应作为 V0 生产依赖。

`codex-acp` 已将认证、model/effort、approval/sandbox、文件/终端、plan/usage、review、steering 和部分 subagent metadata 映射到 ACP，因此 AgentBus 首选复用它。

### 4.2 Claude Code

`claude-agent-acp` 基于官方 Claude Agent SDK，已覆盖权限、edit review、TODO、嵌套 subagent transcript、terminal、MCP 和扩展 metadata。ACP 尚未统一嵌套 subagent 关系时，Adapter 使用 namespaced metadata；Hub 应保留扩展字段，不把它们强行压平丢失。

研究环境中的 `claude-agent-acp` 要求 Node 22，而本机当时是 Node 20.19.4，因此 Node 22 是 PoC 的显式前置条件。

### 4.3 OpenCode

OpenCode 已原生提供 `opencode acp`，并覆盖初始化、认证、session lifecycle、prompt、model/effort、permission、tool/event/usage 等主要路径。它也提供 `opencode serve` HTTP/SSE 和 SDK，但未设置 Server password 时不应暴露到网络。

因此 OpenCode 的首选已经从“自写 HTTP Adapter”调整为“原生 ACP”，HTTP 只作为独立运维或远程部署备选。

### 4.4 AGY

AGY 是当前四类 Runtime 中不确定性最高的一条路径。本地 AGY 1.1.13 的公开 CLI 能力包括：

- `--print`；
- `--output-format text|json|stream-json`；
- `--conversation`；
- `--json-schema`；
- mode、effort 和 permission 控制。

但公开帮助没有显示与输出对称的持续 stdin 输入，也没有官方 ACP/server 命令。因此 V0 不应宣称原生 mid-turn steering。

已有项目实践还观察到：

- 顶层状态为 `ERROR` 时，工作树可能已经产生有效修改；
- CLI 可能改变调用前的 staged/unstaged 状态；
- 自然语言总结可能与实际 diff 不一致；
- JSON print 模式可能长时间没有中间输出；
- 必须复用 conversation、独立检查工作树并执行真实验证。

社区 [`antigravity-acp`](https://github.com/shubzkothekar/antigravity-acp) 提供较完整的 session 和 history 适配，但依赖 AGY 内部 SQLite 与 append-only 假设，不是官方稳定接口。其项目也明确提示第三方方式驱动个人账号可能存在政策与账号风险；本文没有取得可直接引用的官方授权结论，所以这只能作为上线前人工核验 gate，不能表述为官方认可。

## 5. 三条实施路线比较

| 路线 | 优点 | 主要问题 | 结论 |
|---|---|---|---|
| 直接使用 hcom | 最快获得跨终端 message/spawn/watch 体验；AGY 覆盖直接 | 缺完整任务事实模型；PTY delivery 不是执行确认；远端授权粒度不足 | 用于 P0 和可选 Connector |
| fork/扩展 cliagents | API、Session、Task、编排概念丰富 | alpha；双 Session 系统；继承较重的 tmux/CLI 和兼容成本 | 参考或局部复用，不整体 fork |
| 自建薄 AgentBus，复用 ACP/现成 Adapter | 模型边界可控；能统一可靠性、安全和工作区；未来可接 A2A | 需要实现最小控制面和兼容测试 | 当前推荐 |

推荐路线不是“全部从零写”，而是：

```text
自建最小可靠控制面
    + 复用 Codex/Claude/OpenCode 的 ACP Adapter
    + 为 AGY 提供可替换的官方 CLI Adapter
    + 可选复用 hcom 的终端注入和 launcher 能力
    + 后续增加 A2A Gateway
```

## 6. 协议分层的最终取舍

```text
交互式 Agent / 人类
        │
        │ MCP tools / agentbus CLI
        ▼
     AgentBus Hub
        │
        ├── ACP ───────────────→ Coding Runtime
        ├── Native Adapter ────→ ACP 暂不可用的 Runtime
        ├── TerminalConnector ─→ 可选 hcom/PTY 体验层
        └── A2A Gateway ───────→ 后续远程 Agent/其他 Hub
```

边界定义：

- **MCP/CLI**：当前 Agent 如何把其他 Agent 当成远程工具或 sub-agent。
- **ACP**：Hub 如何启动、恢复和驱动 Coding Runtime。
- **Native Adapter**：填补 ACP 缺失或成熟度不足的 Runtime 能力。
- **A2A**：跨进程、跨主机、跨组织的 Agent 服务互操作。
- **内部 canonical model**：可靠任务、attempt、lease、审批、Artifact 和审计的唯一事实源。

## 7. 从“消息总线”演进为“任务控制面”

单一 Message 不能表达一次真实 Coding 任务的完整生命周期。当前建议拆成：

- `Task`：稳定的工作意图；
- `RunAttempt`：某一次实际派发；
- `SessionBinding`：内部上下文到 provider session/thread/conversation 的绑定；
- `Message`：输入、补充、澄清和回复关系；
- `Event`：不可变的状态事实；
- `Artifact`：可重取的最终输出；
- `Lease`：worker、终端输入和 workspace writer 所有权；
- `Approval`：待人工或父 Agent 决策的权限提升。

原始 `type=task|message|result|error|interrupt|status` 可以保留为外部简化视图，但内部必须映射到这些独立对象，不能直接作为数据库状态机。

## 8. 可靠性与并发讨论

### 8.1 不承诺 exactly-once Agent 执行

当 Runtime 已收到 prompt、修改了文件，但 Hub 在确认前断线时，无法通过普通消息队列原子判断副作用。AgentBus 应承诺：

- command 通过 idempotency key 去重；
- event 使用 at-least-once delivery，消费者幂等；
- 每次派发建立新的 RunAttempt；
- 已可能产生写副作用的 lost run 进入 `uncertain`；
- 未完成 reconcile 前不自动重投写任务。

### 8.2 Task 与 RunAttempt 分离

Task ID 表示“用户要完成什么”，Run ID 表示“哪一次 Runtime 尝试”。这样才能保留 retry、取消、版本、错误分类和审计历史，而不是每次重试都丢失原任务身份。

### 8.3 取消是请求，不是终态

`cancel_requested` 只表示 Hub 已请求停止。只有 Runtime 返回终态，或进程监督器确认停止且完成必要 reconcile 后，Task 才能进入 `canceled`。取消超时必须保持可诊断，不能伪造成功。

### 8.4 工作区比消息更危险

多个 Agent 同时写同一个 dirty worktree，可能造成文件覆盖、暂存状态变化和命令副作用。V0 应把 workspace writer lease 视为必需能力：

- 同一工作区默认一个 writer；
- 读任务可并发；
- 多 writer 必须分配独立 git worktree；
- 任务前记录 dirty/staged 基线；
- 任务后对 diff、staging 和 Artifact 做独立核验。

## 9. AGY 的渐进接入策略

### 9.1 Conservative：官方 batch Adapter

```text
agy --print --output-format stream-json [--conversation ...]
```

- 首个 turn 建立 conversation binding；
- 后续 turn 使用同一 conversation；
- follow-up 在 active turn 期间进入 durable queue；
- 当前 turn 完成后，再作为下一 turn 发送；
- capability 标记 `mid_turn_steer=queued`；
- 用 JSON Schema 约束最终结构化结果，但仍独立检查实际工作树。

这是 V0 默认方案，因为它只依赖官方公开 CLI。

### 9.2 Interactive：hcom/PTY Connector

通过 hcom 向 active turn 注入、唤醒 idle Agent，改善终端协作体验。能力标记为 `best_effort`，并且：

- terminal delivery ack 不等于 Runtime accept；
- Runtime 输出仍需由 Adapter/Event normalizer 形成任务事实；
- Connector 故障不应破坏 Task/Artifact 的持久状态。

### 9.3 Experimental：社区 ACP Adapter

只在以下门禁都满足时启用：

- 人工确认账号与使用政策；
- 锁定 AGY/Adapter 兼容版本；
- session、history、cancel、crash/restart 的 contract test 通过；
- 内部 SQLite schema 变化可被启动检查识别；
- 失败时熔断并回退到官方 batch Adapter。

## 10. 分阶段开发建议

### P0：兼容性 Spike

- 在一次性测试仓库验证 hcom 的 Codex ↔ AGY message、active injection、idle wake、resume 和 kill；
- 验证 dirty/staged 基线是否被保留；
- 建立统一 Adapter conformance harness；
- 固定 CLI/Adapter 版本并补齐 Node 22；
- 完成 AGY 社区 Adapter 的人工政策 gate。

### V0：Codex → Hub → AGY → Hub → Codex

- 独立 AgentBus daemon；
- SQLite WAL、Event Log、transactional outbox；
- MCP/CLI 北向入口；
- AGY 官方 batch Adapter；
- 明确 target、单机、单 writer、delegation depth 1；
- 不做 Web UI、A2A、自动角色路由和分布式 Queue。

### V1：双向通信与可靠恢复

- Codex target 通过 `codex-acp` 接入；
- AGY 通过 MCP 或 `agentbus` CLI 反向提交任务；
- 增加 waiting input/approval、crash recovery、uncertain reconcile；
- 增加 Artifact、event replay、deadline、budget 和 idempotency；
- hcom TerminalConnector 作为可选增强。

### V2：四类 Runtime 与受控并发

- 接入 Claude ACP 和 OpenCode 原生 ACP；
- 抽象 `GenericAcpAdapter`，保留 namespaced metadata；
- capability/profile routing；
- isolated worktree、read concurrency；
- health/readiness、version gate、circuit breaker；
- SSE/TUI 可观测界面。

### V3：跨主机和跨信任域

- A2A 1.0 Gateway 和 Agent Cards；
- 需要多 Worker 时迁移 PostgreSQL；
- 确有跨进程事件吞吐需求时再引入 NATS JetStream；
- OAuth/mTLS/RBAC、tenant/project scope、audit、retention 和 quota。

## 11. PoC 验收门禁

1. 相同 idempotency key 重放不会创建第二个 Task/Run，也不会重复发送 prompt。
2. 两个并发 Task 的 session、event sequence、follow-up 和结果不串线。
3. AGY active turn 收到 follow-up 时明确返回 `queued`，在同一 conversation 的下一 turn 消费。
4. Hub 或 Runtime 被强杀后，可能已写文件的 Task 进入 `uncertain`，不会盲目自动重跑。
5. cancel 先进入 `cancel_requested`，确认后才进入 `canceled`。
6. 同一 dirty workspace 的第二个 writer 被阻止，独立 worktree 可并行，staged/unstaged 基线不被静默改变。
7. 权限提升、循环委派、超 depth/budget、未授权 target 全部 fail closed，并产生审计事件。
8. 最终结果以 Artifact 可重新获取，Watcher 重连后可以 replay 关键状态和结果。

## 12. 尚未冻结的问题

- V0 已确定使用 Go 1.22；后续 ACP/MCP 层可以通过 Go SDK、子进程桥接或独立 Adapter host 接入，不要求改写核心状态机。
- hcom 是外部进程级 Connector，还是只借鉴其机制后实现更窄的 PTY helper。
- Artifact 首版只存本地文件 URI，还是同时支持内容寻址对象存储。
- Runtime extensions 的 namespaced metadata 如何版本化并向上层暴露。
- AGY 是否会推出官方 ACP、SDK 或 Server；一旦出现，应优先替换内部数据库型社区 Adapter。
- 何时值得增加自动 capability routing；V0 应坚持显式 target，避免不可解释的路由。

## 13. 参考资料

- [hcom](https://github.com/aannoo/hcom)
- [cliagents](https://github.com/suyashb734/cliagents)
- [Agent Client Protocol](https://agentclientprotocol.com/)
- [Agent2Agent Protocol](https://github.com/a2aproject/A2A)
- [Codex App Server](https://learn.chatgpt.com/docs/app-server)
- [codex-acp](https://github.com/agentclientprotocol/codex-acp)
- [claude-agent-acp](https://github.com/agentclientprotocol/claude-agent-acp)
- [OpenCode](https://github.com/anomalyco/opencode)
- [Antigravity CLI](https://github.com/google-antigravity/antigravity-cli)
- [社区 antigravity-acp](https://github.com/shubzkothekar/antigravity-acp)
