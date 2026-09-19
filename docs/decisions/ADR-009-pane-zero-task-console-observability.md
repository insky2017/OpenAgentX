---
doc_type: decision
status: proposed
canonical: true
owner: openagentx
updated_at: 2026-09-19
---

# ADR-009: Pane 0 任务工作台、连续观察与最终回复

## 状态

提议中 (Proposed)，待监督接受后实施。

## 日期与决策者

- 日期：2026-09-19
- 决策者 / 所有者：OpenAgentX Team / OpenAgentX Maintainer

## 用户目标

用户进入 `OAX:<agent-id>.0` 后，应当把这里当作该 Agent 的日常任务工作台，而不是只能看到
`dispatch succeeded`、`task.created` 或 Worker heartbeat 的状态窗口。一次任务至少要能连续回答：

1. 任务是否已创建、完整 Task ID 和当前 version 是什么；
2. 任务仍在排队、已经被领取、正在运行、等待输入/审批，还是已经结束；
3. 当前 RunAttempt、Worker generation 和连接状态是否仍是权威的；
4. Runtime 是否产生了经过脱敏和限量的过程输出；
5. Task 最终状态、Runtime 最终回复和错误分别是什么；
6. 如何在不手工抄写短 ID 和 version 的情况下，对当前任务执行 steer 或 cancel；
7. 如何在同一个 pane 中进入或退出经过授权的 Diagnostic 视图。

Console 仍不是 Runtime TTY。实现目标是让持久化控制面事实变得连续、可理解、可操作，而不是显示
隐藏推理、原始 stderr、原始 Journal payload，或通过 tmux 直接控制 Worker。

## 与既有决策的关系

- 本 ADR 是 [ADR-008](ADR-008-oax-workspace-console-tui-and-cli-session.md) 的后续。`OAX`、稳定
  pane `0`、CLI Token、Attach cursor、user-systemd 和非破坏 tmux 边界保持不变。
- 本 ADR 补齐 [ADR-005](ADR-005-interactive-worker-console-and-developer-experience.md) 已提出、但当前
  pane `0` 尚未完整交付的 Task、Run、Message、安全输出和最终结果统一时间线。
- Web 与终端继续复用 [ADR-003](ADR-003-command-center-task-observation-and-content.md) 的安全内容投影。
- [ADR-006](ADR-006-task-intent-and-verifiable-terminal-semantics.md) 仍为 Proposed。本 ADR 只展示现有
  Task、RunAttempt 和 TurnResult 事实，不改变 Task intent、成功判定或业务副作用证明；`uncertain`
  必须如实显示，Runtime 有回复也不自动等价于业务成功。
- [ADR-007](ADR-007-worker-network-binding-generation-continuity.md) 仍为 Proposed，本 ADR 不改变网络
  绑定或 generation 继承规则。

## 背景与当前缺口

ADR-008 已交付全屏 TUI、Attach snapshot/cursor、连接状态、受限 Timeline 和正式控制 API，但真实使用
仍存在以下断点：

- `/dispatch` 返回 `status queued` 和 `task.created` 只能证明任务已创建，不能说明 Worker 已领取、执行
  或返回结果；
- 当前普通 Task event 只显示 event type，Task ID、version、状态和最终结果没有形成可持续观察对象；
- Runtime safe output 只有在现有事件投影实际产生时才显示，Console 不会主动补取 Task/Run 的最终事实；
- `/steer` 和 `/cancel` 要求用户输入 Task ID 和 expected version，而 Timeline 又只展示截短 ID；
- `/diagnostic` 在 Normal Attach 中只提示重新使用 Diagnostic Attach，没有同 pane 的正式切换流程；
- 用户无法从 pane `0` 明确分辨“仍在排队”“Worker 正在执行”“等待最终结果”“已终止但无可展示
  结果”等状态。

## 决策

### 1. Pane 0 是 Task-centric Console

`OAX:<agent-id>.0` 的 Console Attach 以“当前关注任务 (focused task)”为中心组织状态和时间线：

- `/dispatch` 成功后自动关注正式响应返回的 Task；
- Attach 时若存在唯一最新的非终态 Task，可将其作为建议关注项；否则由用户从受限的 active/recent
  Task 列表选择，不猜测；
- 用户可以查看当前 Agent 的 active/recent Task，并显式切换关注项；
- 完整 Task ID、version、状态和关联 RunAttempt 必须可在状态/详情 overlay 中查看，Timeline 可以使用
  短显示但不得成为控制命令的唯一输入来源。

关闭 TUI 只 detach Console，不取消 Task、不 drain/stop Worker，也不改变 workspace 绑定。

### 2. 使用窄范围、权威的 Console Task 投影

服务端提供专用于 Console 的 Agent-scoped 安全投影，不重新开放 Task 04 已禁止 CLI Token 访问的广泛
Observe/Network 页面路由。投影至少包含：

- Task：ID、version、status、创建/更新时间和安全的终态 result/error；
- Mailbox/领取摘要：pending、claimed 及其持久化时间或等价的可证明状态；
- RunAttempt：ID、version、status、WorkerInstanceID、generation、Backend、开始/结束时间和安全
  TurnResult；
- Message/Approval：只包含支持当前任务交互所需的公开 ID、version、状态和决策摘要；
- 对应 Event Journal high-water cursor。

任务详情快照必须在 repository/service 的单一事务边界内取得。Persistence 不依赖 transport DTO；HTTP
handler 不拼接多个相互竞态的查询。Task 必须精确属于所选 Agent，Run 必须精确属于 Task，Worker
identity/generation 必须经过校验。查询、归属或安全投影失败时 fail closed，不发送缺字段 frame，也不
推进 cursor。

active/recent Task 列表必须分页或有明确 continuation，不能静默截断前 100 条后声称完整。

### 3. 时间线展示真实任务生命周期

Console 只展示已经持久化并可重建的事实，按可用证据形成以下过程：

```text
created/queued
-> mailbox pending/claimed
-> run starting/running
-> waiting_input/waiting_approval（如发生）
-> finishing
-> succeeded/failed/canceled/uncertain
```

不是所有 Runtime 都提供 token 流、工具调用细节或阶段事件。缺少证据时必须显示“运行中，等待新的持久化
状态/最终结果”，不得制造 thinking、百分比或伪进度。Heartbeat 默认只更新状态栏；只有 Worker、lease、
generation、drain 或连接的有意义转换进入 Timeline。

### 4. 最终回复与 Task 终态分层呈现

Console 将以下内容分开显示：

1. **Task outcome**：Task 的终态、version、持久化 result/error；
2. **Runtime reply**：TurnResult 的 runtime status、经安全投影后的 body/error；
3. **执行证据摘要**：关联 RunAttempt、Backend、结束时间以及已有的安全副作用证据来源。

Runtime reply 不得覆盖 Task outcome。Task 为 `uncertain` 时，即使存在自然语言回复，也必须标记为
“回复已产生，但业务终态仍为 uncertain”。Task 已终止但没有可展示结果时，必须显示明确原因，例如
“任务已完成，但没有安全可展示的回复”，不能留下看似仍在运行的空白界面。

### 5. Web 与终端共用安全内容投影

Task result、TurnResult body/error 和增量 Runtime output 必须通过同一 `safeoutput` 规则：

- Secret、credential、Cookie、Token、完整 fencing material、隐藏推理和原始 stderr 不得出现；
- 文本和诊断字段分别设置单项及总量上限，并显示明确 truncation marker；
- 不渲染原始 JSON 或 Journal payload；
- Normal 模式强制移除 diagnostic 字段；
- Diagnostic 模式仍只显示脱敏、限量、结构化内容。

Adapter 没有提供可安全投影的增量输出时，Console 只能显示状态转换和最终回复，不得从进程 stdout/stderr
旁路抓取。

### 6. Reducer 是 Console 当前状态的唯一来源

共享 reducer 在现有 Worker fencing 之外增加 Task-centric 状态，并满足：

- Task ID 固定后，version 与状态只能按合法转换前进；重复事件幂等，倒退或冲突 fail closed；
- Run 必须同时绑定 focused Task 和当前 WorkerInstanceID/generation，旧 generation 或同代不同 instance
  只能作为安全历史，不能覆盖当前状态；
- Task replacement、Worker replacement、offline 和 terminal transition 必须清除无法继续证明归属的
  run-scoped/worker-scoped 状态；
- snapshot/event 只有在 TUI Update 成功应用并 ack 后才推进客户端 last-applied cursor；
- 合法但与当前 Agent/Task 无关的事件可安全忽略，并按明确规则推进 cursor；
- 普通重连从 last-applied 继续，只有 retention gap 才重新 Attach/获取一致快照，绝不回到 sequence 0。

Normal 与 Diagnostic 使用同一个 reducer、同一身份和 cursor 规则。

### 7. 当前任务提供安全的快捷控制

显式 CAS 仍是服务端写入前提。TUI 可以从 reducer 的最新权威 focused Task 中取得完整 ID/version，提供：

```text
/steer <content>
/cancel
```

这些快捷命令只在 focused Task 存在、状态允许且连接有效时启用。每次只调用正式 API 一次，不本地排队、
不假成功、不自动重试写操作。

显式控制形式继续保留，但使用无歧义语法并在帮助中展示，例如：

```text
/steer --task <task-id> --version <n> <content>
/cancel --task <task-id> --version <n>
```

兼容既有位置参数时也必须严格解析。CAS stale 时重新读取权威任务状态并提示新 version，由用户确认下一次
写入；不得静默使用新 version 重放旧指令。`/approve`、`/reject` 继续使用正式 Approval ID/version。

### 8. Diagnostic 在同一 pane 内安全切换

Normal Attach 中执行 `/diagnostic` 时，TUI 使用现有 authenticated official client 申请 Diagnostic
Attach/Follow：

- 服务端继续要求 owner role 与 `console.diagnostic` scope；
- 成功后取消并等待旧 Follow 安全结束，再应用新的 snapshot/cursor，模式标识更新为 diagnostic；
- 授权、网络或投影失败时保留原 Normal 状态和输入 draft，不泄漏 Diagnostic 内容；
- `/normal` 以同一流程切回 Normal；
- 切换不重启 pane、不修改 tmux marker、不 stop/respawn Worker，也不建立 Worker 私有协议。

Diagnostic 至少可展示结构化 stage、Backend、等待原因、退出类别、heartbeat/lease/drain 聚合和脱敏
diagnostic。它不是 raw log viewer，也不承诺 Runtime TTY。

### 9. 有界资源与紧凑终端继续成立

- Timeline 对条目数和总字节数同时设上限，Task 详情/result/output 也有独立上限；
- 快照、分页和重连不得造成无界历史加载；
- 用户向上滚动时后台事件不强制拉到底；仅原本位于底部时 auto-follow；
- `80x5` 及更小 pane 继续严格按实际尺寸渲染，后台事件、overlay 和最终回复不得冲刷输入；
- 断线、token 过期或 reducer 拒绝事件时写操作立即禁用，但输入 draft 保留。

### 10. 默认用户工作流必须简短且完整

默认 profile 已建立后，日常路径不要求重复提供 `--db`、`--socket`、`--file` 或 credentials 路径：

```bash
openagentx console login
openagentx fleet up
tmux attach-session -t OAX
openagentx console attach --agent <agent-id>
```

Fleet 创建的 managed pane `0` 可以直接运行最后一条等价命令。安装指南必须从 dispatch 一直演示到过程
状态、最终回复、focused steer/cancel、Normal/Diagnostic 切换和 `/quit`，并说明 `/quit` 不停止 Worker。

## 明确排除

- 不接受或实现 ADR-006 的 Task intent/终态新语义；
- 不接受或实现 ADR-007 的网络代际继承；
- 不实现 Foreground Takeover，不声称 systemd Worker 可直接获得当前 Runtime TTY；
- 不用 `send-keys`、`paste-buffer`、`capture-pane`、window index 或 `%pane_id` 承载业务控制/身份；
- 不让 Console 直接调用 TurnHandle，不建立绕过正式 API 的 Worker 私有控制协议；
- 不暴露 raw stdout/stderr、hidden reasoning、Secret、原始 Journal payload 或未脱敏异常；
- 不把“dispatch accepted / queued / task.created”写成“Worker 已执行”或“用户已收到回复”。

## 验收矩阵

### 正向流程

- dispatch 后自动 focus，新 Task 的完整 ID/version 可查看；
- queued、claimed、running、waiting、terminal 和最终回复按持久化事实连续更新；
- 有增量输出的 Adapter 显示安全增量，无增量能力的 Adapter 明确等待最终结果；
- focused `/steer <content>`、`/cancel` 使用最新 CAS 且正式 API 只调用一次；
- `/diagnostic` 和 `/normal` 在同一 pane 切换，授权内容只在 Diagnostic 显示；
- Console 断开/退出/重启后从 last-applied 或新 snapshot 恢复，Worker 与 Task 不受影响。

### 状态转换与不变量

- Task/Run/Worker identity、generation、version 和 cursor 全链路一致；
- 旧 Worker、旧 Run、重复/乱序事件不能回退 focused Task 或覆盖最终回复；
- Task terminal、Runtime reply 和业务效果证据分层显示；`uncertain` 不伪装成功；
- Normal/Diagnostic 共用 reducer，模式切换不会混入旧流事件或丢失输入。

### 失败注入

- Task/Run/Worker/Message/Approval 查询、归属、投影、encode/write 任一失败都不跨 cursor；
- CAS conflict、token expiry、Diagnostic forbidden、retention gap、socket replacement 和连接中断均
  fail closed；
- 终态无安全结果、输出被截断、Adapter 无增量能力时有明确 UI，不无限等待或显示空白成功；
- Diagnostic 切换失败保持 Normal，Follow cancel/ack 无 goroutine 泄漏。

### 资源与平台边界

- Timeline、result、diagnostic、分页和内存均有确定上限；
- 紧凑 pane、resize、滚动、overlay、长行和 masked input 不互相覆盖；
- 真实集成只使用临时 HOME/DB/UDS、唯一 `tmux -L` 和 fake/isolated Worker；
- 测试不操作默认 tmux、真实 user-systemd、正式 DB/socket/credential 或 installed binary。

## 后果

### 正向收益

- pane `0` 从“状态和命令入口”变成能看懂单次任务全过程的日常工作台；
- 用户不必从短 ID 手工拼装 steer/cancel 命令；
- 最终回复、Task 终态和业务证据不再混为一谈；
- Diagnostic 能在同一 UI 内授权切换，同时保持安全投影和 Worker 独立生命周期。

### 成本与限制

- 需要新的窄范围任务投影、事务快照、事件投影和 reducer 状态；
- Adapter 能显示的过程粒度取决于其实际持久化能力，不能由 UI 补造；
- 长任务、多任务和历史列表需要分页、容量上限和明确 focus 规则；
- ADR-006 未接受前，某些有自然语言回复的只读任务仍可能合法终止为 `uncertain`。

## 实施状态

本 ADR 当前只记录决策提案和验收边界。实施计划见
[ADR-009 实施计划](../plans/2026-09-19-openagentx-adr-009-implementation-plan.md)。ADR 状态改为
Accepted 且 P0 基线通过前，不得开始 Task 01 或修改产品行为。
