---
doc_type: implementation_contract
status: frozen
owner: openagentx
updated_at: 2026-09-19
---

# ADR-009 Task 01 契约冻结

## 1. 基线与目标

| 项目 | 冻结值 |
|---|---|
| Git baseline | `3312d95e9662eb9b064b13ff4db06e5982564e4a` |
| ADR-009 SHA-256 | `afb7473b6eb353dd06551bb3126fbe3300005e4a1d782b863a9cb1d0f2a32c69` |
| AGENTS.md SHA-256 | `b53264590ccb1ebace81668ae3c98f1e29ce8f61384a259f08a5e23b8b92a95b` |
| workspace | `OAX:<agent-id>.0`，pane `1+` 不受影响 |
| 用户结果 | dispatch 后在同一 TUI 连续看到 queued、领取、运行/等待、安全输出、终态和最终回复，并能控制 focused Task |
| 非目标 | ADR-006/007、Foreground、Runtime TTY、raw stderr/Journal、tmux 控制、真实部署 |

本冻结只规定 ADR-009 的最小完整链路。现有 Task/Run 终态语义、CLI Token、`OAX` binding、Fleet 和
user-systemd 不改变。

## 2. 当前权威链路与缺口

当前正式链路已经具备：

1. `CommandService.CreateTask` 在事务中创建 Task、首条 Message、work Mailbox 和 `task.created`；
2. Worker claim 后 `BeginAttempt` 将 work Mailbox accepted、Task running、RunAttempt starting/running，并
   写 Journal；
3. Runtime `EventSink` 通过带 WorkerInstanceID/generation/fencing/expected run version 的
   `AppendRunEvents` 持久化；
4. `FinishRun` 以 Task/Run CAS 持久化 TurnResult、Run terminal、Task terminal/waiting_input 和 Journal；
5. Observe SSE 已按 Agent 归属过滤，projection/encode/write 失败不跨 cursor；
6. Console Follow 已在 reducer Apply ack 后推进 last-applied cursor。

现有缺口是 Console projection/reducer/TUI 没有 Task 当前状态：普通 Task event 只有 event type；Attach
只含 Worker 和 active Run；最终 Task/TurnResult 不会自动补取；控制结果只显示短 ID。

## 3. Runtime 能力冻结

### 3.1 实测 CLI

| Runtime | 实测版本 | 只读 help 证据 | 本轮采用能力 |
|---|---:|---|---|
| AGY wrapper/CLI | `1.2.7` | 支持 `--input-format stream-json`、`--output-format stream-json`、`--print-timeout`、`--conversation`、`--model`、`--effort` | NDJSON 输入/输出、session resume、结构化增量和终态 |
| CodeBuddy CLI | `2.143.0` | 支持 `--print`、text/json/stream-json、`--include-partial-messages`、`--model`、`--effort`、`--max-turns` | 现有 Adapter 继续使用 text stdout；按完整行产生安全增量，最终 bounded stdout 为 reply |
| ACP package | repository contract | NDJSON status/result/event；当前 Worker config assembler 不把 ACP 暴露为普通 YAML Adapter | 保持现有代码兼容，不作为 ADR-009 E2E 必选 Runtime |

版本/help 仅证明 CLI 表面。真正产品能力以 Adapter + fixture 测试为准：

- `agy-batch`：`Streams=true`，New/Resume，Steer queued，Approval preflight，Cancel process signal；解析
  stream-json 的公开 stage/status/text/diagnostic/result/error，未知事件降级为安全 event，空/畸形/无
  terminal fail closed；stderr 只作为 bounded/redacted diagnostic 进入失败 TurnResult。
- `codebuddy-cli`：`Streams=true`，New only，Steer queued，Approval preflight，Cancel process signal；当前
  启动参数明确使用 `--output-format text`，每个完整 stdout 行产生 `turn.output`，最终 stdout 为
  TurnResult body；进程失败、event persistence 失败或 stdout/stderr 截断均为 `uncertain`。
- Adapter descriptor 的 `SteerQueued` 不表示运行中 handle 原生 steer。follow-up 仍通过正式 Message/
  Mailbox，在现有领域规则允许时形成后续 turn；UI 不声称即时注入当前模型 token stream。

### 3.2 输出上限

- 每个安全文本字段使用 `safeoutput.MaxTextBytes = 4096`，追加 `[TRUNCATED]`；
- 每个 Run 最多投影 256 个实时输出 event；Adapter 原始 stdout/stderr 继续使用自身 bounded buffer；
- CodeBuddy 最终 stdout 内部上限维持 1 MiB，但公开 body 最多 4 KiB；
- 达到实时 event 上限只停止继续发布过程片段，不能丢失最终 Run/Task 状态；原始输出本身超出 Adapter
  authoritative buffer 时继续按现有规则进入 `uncertain`；
- 不输出 raw stderr、环境、credential、hidden reasoning 或未知 JSON 字段。

## 4. Console Task API

API 只挂现有 Console handler，并分别使用 Web cookie authorizer 或 UDS CLI bearer authorizer。CLI 读取
要求 viewer + `console.read`；Diagnostic 字段另需 owner + `console.diagnostic`。不扩大 Panel CLI scope。

### 4.1 路由

```text
GET /api/console/v1/agents/{agent-id}/tasks?cursor=<opaque>&limit=<1..100>
GET /api/console/v1/agents/{agent-id}/tasks/{task-id}
GET /api/console/v1/attach?agent_id=<agent-id>&mode=normal|diagnostic
GET /api/observe/v1/events/stream?...&mode=normal|diagnostic
```

- Task list 默认 `limit=50`、最大 100，排序为 normalized `updated_at DESC, task_id DESC`；
- cursor 为服务端生成、版本化、URL-safe opaque 值，必须同时绑定上一项 updated_at 与 task_id；空、篡改、
  不推进或重复 cursor 返回 400；client 总量 hard cap 10,000；
- Task detail 在目标 Agent 不存在、Task 不存在或 Task 属于其他 Agent 时统一返回 404，避免归属枚举；
- Attach 增加可空 `suggested_task`，取该 Agent 按同一排序的最新非终态 Task；没有非终态 Task 时为空；
- list/detail/suggested task 都只返回安全 DTO，不返回 sender principal、idempotency key、fencing token、
  execution JSON、scope digest、raw payload 或内部错误。

### 4.2 DTO

```text
ConsoleTaskOption
  task_id, version, status, summary, updated_at

ConsoleTaskSnapshot
  task                 ConsoleTaskReadModel
  work_delivery        *ConsoleMailboxReadModel
  latest_run           *RunAttemptReadModel
  latest_message       *ConsoleMessageReadModel
  pending_approval     *ConsoleApprovalReadModel
  snapshot_sequence    int64

ConsoleTaskReadModel
  task_id, version, agent_id, status
  content, result, error
  created_at, updated_at

ConsoleMailboxReadModel
  mailbox_item_id, state, attempts
  worker_instance_id, lease_until, created_at, accepted_at

ConsoleMessageReadModel
  message_id, version, sequence, kind, content, created_at

ConsoleApprovalReadModel
  approval_request_id, mode, state
  target_run_id, expected_run_version, expires_at, created_at
```

- `content/summary/result/error/message.content` 均 `RedactText`，单字段 4 KiB；summary 再限制为 1 KiB；
- `work_delivery` 只选择 `kind=task,lane=work` 的原始交付项；控制 mailbox 不混入领取状态；
- `latest_run` 按 `started_at DESC, run_id DESC`，active 或 terminal 都可返回；Worker generation 必须通过
  该 Run 精确 WorkerInstanceID 查询；
- `latest_message` 按 sequence/version/ID 确定性选择；
- `pending_approval` 只返回 state=pending 的最新一项；scope digest 不公开；
- 所有子对象必须精确归属 Task/Agent，任一存储值无效或查询失败，整个 snapshot fail closed。

### 4.3 事务边界

`ConsoleTaskSnapshot` 在 repository 的单一 SQLite read transaction 中依次读取 Task、work delivery、latest
Run+精确 Worker generation、latest Message、pending Approval，最后采样 Journal high-water。空 Journal
为 0。读取期间提交 N+1 时，只允许：

1. snapshot 与 high-water 都包含 N+1；或
2. snapshot/high-water 都停在 N，Follow 从 N 后交付 N+1。

不得出现状态包含 N+1、cursor 却越过/落后导致无法重放的混合快照。

## 5. SSE 安全投影

`JournalEventReadModel` 增加可空字段：

```text
Task      *ConsoleTaskReadModel
Mailbox   *ConsoleMailboxReadModel
Message   *ConsoleMessageReadModel
Approval  *ConsoleApprovalReadModel
```

- task event 查询并投影当前 Task；
- mailbox event 查询 item，再通过 Task 精确确认 Agent；
- message/approval event 查询对象及所属 Task；
- run/runtime/worker 延续 ADR-008 的精确归属与 generation fencing；
- repository NotFound、查询错误、归属不一致、无效 enum/identity/time、safe projection、JSON encode 或 frame
  write 失败，都在该 frame 和 `after` 推进前终止 stream；
- 只有可证明属于其他 Agent 或不属于公开 aggregate 的合法事件可以跳过并推进 cursor；
- Normal 强制清空 Diagnostic；缺省 mode 兼容为 Normal，显式空、重复或未知 mode 为 400。

## 6. Focus、状态、CAS 与幂等

### 6.1 Focus 来源

固定优先级：

1. `/dispatch` 正式响应返回的 Task 精确成为 focus；
2. 用户从 active/recent overlay 显式选择；
3. 首次 Attach 的 `suggested_task`；
4. 没有 Task 时保持 no-focus，不猜测。

SSE 中其他 Task 的事件只进入 Agent Timeline/更新列表，不自动抢占用户 focus。Task terminal 后仍保持 focus，
直到用户选择或新 dispatch。

### 6.2 Task reducer

- identity 至少为 AgentID+TaskID；Task ID 一旦 focus 不可被 event 改写；
- higher version 才能更新当前 Task；same version 必须字段一致，完全重复幂等；lower version 安全忽略当前
  状态但按合法 event 规则推进 cursor；
- 不使用简单状态排名，因为 `waiting_input/approval -> running` 是合法新 version；
- 当前 Task 已 terminal 后，任何不同状态/result/error 的事件均 fail closed；完全相同重复可忽略；
- Run 还必须匹配 TaskID 和当前 WorkerInstanceID/generation；旧代/异 instance/旧 run 只能作为安全历史；
- Worker replacement/offline 清除无法证明属于当前 Worker 的 active run/backend/diagnostic，但不删除已经
  持久化的 Task outcome/runtime reply。

### 6.3 控制语法

```text
/steer <content>
/cancel
/steer --task <task-id> --version <n> <content>
/cancel --task <task-id> --version <n>
```

- 快捷形式只在 focused Task 非 terminal、Task version>0、连接有效且 CLI session 未过期时启用；
- focused 命令的 Task ID/version 只能来自 reducer 最新 snapshot/event/control outcome；
- 兼容既有 `/steer <task-id> <version> <content>`、`/cancel <task-id> <version>`，但帮助以 flag 形式为
  显式推荐；解析必须无歧义；
- 每次用户确认只调用正式 API 一次。CAS stale 后只读刷新并提示，不自动重试写操作；
- `/dispatch`、steer、cancel、approval 延续现有 idempotency/CAS/API audit，TUI 不本地排队。

## 7. Timeline 与最终回复

- Timeline 保持最多 256 条、总计 64 KiB、单条 2 KiB；用户离开底部时后台更新保持 scroll；
- 有意义项：Task version/status、work delivery pending/claimed/accepted、Run start/status/terminal、safe output、
  control outcome、Task outcome、Runtime reply；heartbeat burst 不逐条显示；
- 详情/status overlay 显示完整 Task/Run/Message/Approval ID 和 version；普通 Timeline 可短 ID；
- Task outcome、Runtime reply、side-effect evidence 分区显示；`uncertain` 明确标记；
- Task terminal 但 TurnResult `not_recorded/invalid`、body 为空或被截断时显示明确原因，不保持“运行中”；
- Adapter 不支持/没有产生增量时显示“运行中，等待新的持久化状态或最终结果”，不伪造进度。

## 8. Diagnostic 模式切换

`/diagnostic` 和 `/normal` 共用一个 mode-switch application service：

1. 标记 switching，禁用写操作但保留 draft/focus；
2. cancel 旧 Follow 并等待 followDone；旧 mode 迟到消息不再 Apply/ack；
3. 使用同一 credential 和 Agent 重新 Attach 目标 mode，取得新 snapshot/cursor；
4. reducer 成功 Apply 后启动新 Follow，原子更新 mode/connection；
5. 授权/网络/投影失败时保持或恢复 Normal 安全状态，不显示 Diagnostic。

任一时刻最多一个 active Follow。切换不调用 tmux、不 respawn pane、不影响 Worker/Task。

## 9. 测试地图

| Task | 必须证明的最小证据 |
|---|---|
| 02 | Task list/detail/Attach suggestion；单事务 N/N+1；归属/分页/scope；所有 projection 错误不跨 cursor |
| 03 | AGY/CodeBuddy/ACP contract fixture；safeoutput 共享；event cap；空/畸形/stderr/截断/terminal no-result |
| 04 | Task version/terminal/focus；Run/Worker fencing；dispatch/event 乱序；ack/cursor；bounded collections |
| 05 | 完整 ID overlay；queued->terminal UI；focused/explicit controls exactly once；input/scroll/compact/cleanup |
| 06 | Normal<->Diagnostic 单 Follow；403/expiry/network/gap；旧流迟到；无 Diagnostic 泄漏或 goroutine leak |
| 07 | 临时 HOME/DB/UDS + real daemon/Worker process + unique tmux/PTY，从 dispatch 到最终 reply，退出后第二 Task 可领取 |
| 08 | baseline..HEAD traceability；无缓存全量/race/vet/Web/release；独立候选 provenance；真实 E2E 重跑 |

## 10. 裁决与后置项

- **本轮修复**：Task 观察、最终回复、focused control、同 pane Diagnostic，以及直接支撑它们的投影/
  reducer/安全输出。
- **有界核实**：Adapter event 数量上限、terminal result 与 Journal 的事务顺序、existing Web projection 回归。
- **后置**：完整任务 transcript 搜索、任意历史 messages/approvals 浏览、跨 Agent 统一任务中心、Runtime
  token-level progress。只有用户目标或容量/审计事故证明需要时重新评估。
- **不采纳**：Runtime TTY、hidden reasoning/raw stderr、tmux capture/send 控制、Console TurnHandle、
  为让只读问答显示 succeeded 而顺带接受 ADR-006。
