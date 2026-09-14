---
doc_type: decision
status: accepted
canonical: true
owner: openagentx
updated_at: 2026-09-14
---

# ADR-005: Fleet、tmux Console Attach 与 Worker 开发者体验

## 状态

已接受 (Accepted)，尚未实施。

## 日期与决策者

- 日期：2026-09-14
- 决策者 / 所有者：OpenAgentX Team / OpenAgentX Maintainer

## 背景

ADR-001 确立了中心化控制面、长生命周期 Resident Worker 和正式 API/Mailbox 控制路径。实际接入多个 Agent 后，仍存在以下开发体验问题：

1. Worker 只能逐个初始化和启停，缺少显式的 Fleet 清单与批量入口；
2. systemd Worker 稳定但缺少统一、可交互的本地终端工作台；
3. 开发者无法在终端中持续观察正在执行的 RunAttempt，并经正式控制路径执行 steer、cancel、approval 和 dispatch；
4. tmux session 可能已经存在，且其 windows 与 Fleet 不完全对应，初始化不能破坏用户现场；
5. 为了终端互动而停止 systemd Worker 会打断工作，也错误地把“控制台”与“Worker 宿主”绑定在一起。

本 ADR 将 Fleet、systemd 与 tmux 明确分层，并选择 Console Attach 作为日常交互机制。

原草案中混入的两项独立问题已拆分为：

- [ADR-006：Task intent 与可验证终态语义](ADR-006-task-intent-and-verifiable-terminal-semantics.md)；
- [ADR-007：Worker 网络绑定的代际连续性](ADR-007-worker-network-binding-generation-continuity.md)。

两者保持 Proposed，不属于本 ADR 的已接受范围。

## 术语

- **Agent**：控制面中的逻辑身份和任务目标。
- **Worker**：代表一个 Agent 常驻运行、领取 Mailbox 工作并维护 lease/heartbeat 的进程。
- **Runtime Backend**：Worker 中配置好的执行方案，例如 Adapter、模型、网络策略和 Runtime binary；它不是正在运行的进程。
- **Runtime Process**：Backend 为某次 Turn 启动的瞬态子进程，例如 `agy-batch` 或 `codebuddy`。
- **Console**：独立终端客户端；它观察和控制逻辑 Agent，但不是 Worker，也不接管 Worker 的 stdin/stdout。

## 决策

### 1. 三层职责边界

采用以下分层：

```text
Fleet       管 Agent 清单、初始化和期望的启停操作
systemd     管真实 Worker 进程生命周期
 tmux       管本机统一观察与交互工作台
```

具体约束：

- Fleet 使用显式清单，不扫描 `agents/*` 自动推断应启动对象，避免误启 fake、测试或样例 Worker；
- Worker 启动后仍按既有协议完成配置加载、Runtime 校验、注册、generation、lease 和 heartbeat；
- systemd 是生产 Worker 的默认宿主，SSH 或 tmux 中断不得导致 Worker 下线；
- tmux 仅是本机开发者工作台，不保存权威 Worker 身份，不参与任务路由、lease、generation 或调度决策。

ADR-001 关于“禁止 tmux 成为业务控制路径”的约束继续有效。本 ADR 仅允许本地 CLI 为 Console workspace 创建、枚举和命名 tmux session/window；仍禁止以 `send-keys`、`paste-buffer`、`capture-pane` 等机制传递业务命令或判断任务状态。

### 2. Fleet 初始化与生命周期

Fleet 清单显式列出需要管理的 Agent，以及其 identity、Worker 配置和启用意图。初始化应支持：

- 一次认证后批量校验和 apply Agent identities；
- 在产生副作用前报告清单级配置错误，避免静默的部分初始化；
- 为 Fleet Agent 准备 systemd 与 Console workspace；
- 独立地将 Worker 设为 online 或 offline，而不删除其 Console window。

本 ADR 只固定职责与状态转换，不固定最终命令名、YAML 字段或 TUI 布局细节。

### 3. 固定的 tmux workspace

本地工作台固定使用专用 session：

```text
agentx
```

布局规则：

- 一个 `overview` window；
- Fleet 中每个 Agent 一个 window，包括当前 offline 的 Agent；
- window 名使用稳定且唯一的 `agent_id`；
- 每个受管 Worker window 固定使用 pane `0`；
- 用户可以重排 window，window 序号不构成身份；
- 用户入口按名称解析，例如 `agentx:quote-service.0`；
- 不向用户暴露 `%100` 一类 tmux pane ID。

Console 在 Worker 重启后按 `agent_id` 跟随当前有效 generation，而不是绑定某个 WorkerInstanceID、window 序号或 pane ID。

### 4. tmux 初始化采用非破坏性协调

Fleet 初始化必须幂等，且不能把已有 `agentx` session 当成可随意重建的临时目录。

| 现场状态 | 默认处理 |
|---|---|
| `agentx` session 不存在 | 创建 session、`overview` 和 Fleet windows |
| Agent 对应的受管 window 已存在 | 原位复用 |
| Fleet 有 Agent，但缺少 window | 创建缺失 window |
| 存在非 Fleet window | 保留并视为 unmanaged |
| Agent 已从 Fleet 移除 | 保留对应 window 并标记 orphaned，显式确认后才能删除 |
| window 名重复 | 报告冲突，不按序号猜测 |
| 同名 window 被未知程序或不兼容布局占用 | 停止该 window 的自动协调，要求用户选择接管、重建或跳过 |

自动协调不得杀死未知进程、删除 window、重命名用户 window 或重排现有布局。未来可以增加显式清理命令，但不能改变此默认行为。

在 Worker window 中发起操作时：

- 若当前 window 名精确匹配 Fleet `agent_id`，默认选中该 Agent；
- 在 `overview`、unmanaged 或无法唯一匹配的 window 中，显示 Agent 列表供选择；
- 所有识别均基于稳定 window 名，不依赖 session/window 排列顺序。

### 5. Normal Console Attach 是默认日常模式

pane `0` 默认运行 Console 客户端，而不是 Worker 进程。Console 经正式 API 连接控制面和逻辑 Agent：

```text
Runtime Process
  -> Adapter 归一化
  -> Worker 上报
  -> Control Plane / Event Stream
  -> Console 展示

Console 输入
  -> 正式 UDS/HTTP API
  -> 持久化、鉴权、CAS 与审计
  -> Mailbox / Worker command
  -> 当前 Worker
```

Normal Console 应提供：

- Worker online/offline、generation、lease、Backend 健康和当前 RunAttempt 状态；
- 正在执行内容的安全、结构化实时输出；
- Task、Message、工具步骤、审批、完成结果和错误的统一时间线；
- steer、cancel、approval 和 dispatch 操作；
- 断线重连与历史补放；
- 多个 Console 或 Web 客户端同时观察同一 Agent。

`steer`、`cancel` 和 `dispatch` 不得直接调用 Worker 内存中的 TurnHandle，也不得通过 tmux 注入按键。Runtime 不支持某项能力时，Console 必须展示正式 API 返回的 unsupported 结果。

关闭 Console、window、tmux session 或 SSH 连接不得改变 Worker 生命周期。

### 6. Diagnostic Attach 纳入实现范围

Diagnostic Attach 与 Normal Console 使用完全相同的 attach、身份和控制机制，不切换 Worker，也不接管 Runtime TTY。它只是为授权本地操作者增加诊断视图，例如：

- Worker 注册、heartbeat、lease 和 drain 状态；
- Backend 健康检查与 Runtime 启动阶段；
- 经脱敏的 Adapter、网络策略和进程退出诊断；
- 最近活动、卡住阶段和等待原因。

Diagnostic Attach 不能绕过正式 API，不能泄漏凭据、原始环境变量、隐藏推理或未经安全投影的 Runtime payload。诊断信息应有边界、限流和脱敏规则；关闭诊断视图不影响 Worker。

### 7. 补齐统一的安全实时输出流

Console Attach 能否展示“正在执行中的内容”，取决于 Adapter 到控制面的输出流，而不是 Worker 是否运行在前台。

当前实现存在已知缺口：

- AGY 已解析 `stream-json`，但公开事件主要保留阶段、状态和 `has_output`/`has_error`，没有正文；
- CodeBuddy 当前主要缓冲 stdout/stderr，并在进程结束后形成 TurnResult；
- Panel SSE 有意只发送安全投影，不发送 Runtime 原始 payload。

因此实现 Console 时必须建立 Web 与终端共用的安全输出模型：

- Adapter 归一化允许公开的文本、工具和状态事件；
- 控制面执行鉴权、脱敏、大小限制和安全投影；
- Console 支持实时订阅、断线恢复和最终结果对齐；
- 不盲目持久化逐 Token 原始流，不公开秘密、内部推理或任意 stderr。

不得为终端另建绕过控制面的 Worker 私有控制协议。

### 8. 普通停止采用持久化 graceful drain-and-stop

Console Attach 不要求停止 Worker。只有 Fleet `down`、维护或未来的宿主切换才进入停止流程。

普通停止语义为：

```text
持久化 graceful-stop 意图
-> Worker 进入 draining，不再领取新工作
-> 当前 RunAttempt 自然完成
-> 确认 Worker idle
-> 释放 lease
-> Worker 正常退出
```

约束：

- graceful-stop 意图必须由控制面持久化并由 Worker 执行，不依赖发起 CLI 或页面持续在线；
- 默认无限等待当前 RunAttempt 自然结束，不主动 cancel，不制造不必要的 `uncertain`；
- 终端和页面持续展示 Agent、RunAttempt、已等待时长、最近活动和“不会领取新任务”的提示；
- 如果 Runtime 永久卡住，允许授权用户选择单独、显著标警的强制停止；强制停止可能浪费 Token、造成副作用不确定，不能成为默认路径；
- 直接的 systemd/SIGTERM 运维操作不能被包装成“安全排空”，其风险必须明确。

### 9. Foreground Takeover 仅保留未来设计，不实施

真正的 Foreground Takeover 暂不进入实现范围。菜单可以保留禁用入口，并明确显示：

```text
Foreground Takeover（规划中，暂不可用）
```

它只面向以下未来需求：

- 调试 Worker 启动、注册和初始化失败；
- 使用 delve、strace 等进程级工具；
- 排查环境、信号、Adapter 管道或 Runtime 退出行为。

若未来决定实现，安全流程必须是：

```text
graceful drain-and-stop
-> 等待当前 RunAttempt 自然结束并确认 idle
-> 停止 systemd Worker、释放 lease
-> 在对应具名 window 的 pane 0 启动 foreground Worker
-> 调试结束后正常退出
-> 恢复 systemd Worker 与 Console
```

它必须与 systemd Worker 严格互斥。关闭 pane 可能终止 foreground Worker，因此不能作为日常运行方式。并且 foreground 本身不保证看到 Runtime 原始输出：现有 Adapter 使用 pipe 消费子进程 stdout/stderr，仍需统一输出流或明确的调试 instrumentation。

此说明只是未来约束，不表示当前产品支持 Foreground Takeover。

## 状态持久化边界

Worker 或 Console 重启时，下列控制面状态必须保留：

- Task、Message 和 Mailbox；
- Event Journal；
- 已完成 RunAttempt；
- 已提交的 SessionBinding；
- 持久化的 lifecycle intent，例如 draining/graceful-stop。

下列进程内状态不能宣称可无损迁移：

- 活动 TurnHandle；
- Runtime 子进程；
- 尚未上报或持久化的流片段；
- 进程内临时诊断状态。

因此任何宿主切换都必须先等待活动 RunAttempt 结束，除非操作者明确接受强制终止风险。

## 被拒绝或延后的方案

1. **让 tmux 成为控制面或 Agent 地址**：拒绝。window 只用于本机工作台映射。
2. **Console 直接调用本地 TurnHandle**：拒绝。会绕过持久化、CAS、权限和审计。
3. **为了日常终端互动而停止 systemd Worker**：拒绝。Console Attach 已能在不中断 Worker 的前提下观察和控制。
4. **把 systemd 进程重新绑定到现有 TTY**：拒绝。不可移植且不可靠。
5. **自动扫描所有 Agent 目录并启动**：拒绝。必须由显式 Fleet 清单授权。
6. **自动重建不匹配的 `agentx` session**：拒绝。默认协调必须非破坏。
7. **首期实现 Foreground Takeover**：延期。保留设计约束和禁用菜单入口。
8. **将 `/reload` 直接改写活动 Worker 身份**：拒绝作为 Console 快捷路径。Runtime 身份或配置变更必须走正式生命周期和重新注册流程。

## 影响与后果

### 正向收益

- 开发者能立即观察和干预当前 RunAttempt，无需等待或切换 Worker；
- systemd 继续提供稳定生命周期，tmux 中断不影响任务；
- Web 与终端共用同一状态、输出和控制语义；
- Fleet 与 tmux 初始化可重复执行且不破坏已有现场；
- foreground 调试需求被记录，但不会提前引入复杂的进程交接风险。

### 成本与限制

- 需要实现 Fleet 清单、workspace 协调和终端 Console；
- 需要扩展安全的 Runtime 输出投影，尤其是 CodeBuddy 的实时输出；
- Diagnostic Attach 增加了脱敏、限流和授权要求；
- tmux window 名必须唯一且稳定，同名冲突必须人工处理；
- Runtime 本身不支持 steer/approval 时，Console 不能制造不存在的能力。

## 实施顺序

1. 显式 Fleet 清单、批量初始化与 systemd 生命周期；
2. `agentx` workspace 的非破坏性协调；
3. Normal Console Attach 与安全实时输出流；
4. Diagnostic Attach；
5. graceful drain-and-stop 与持续等待反馈；
6. Foreground Takeover 保持未实现，待真实需求验证后另行接受实施决策。

## 验收不变量

- 关闭 tmux 不会停止任何 systemd Worker；
- window 重排不影响 Agent 选择；
- Console 控制全部经过正式 API；
- busy Worker 的普通停止不会取消当前 RunAttempt；
- 初始化不会删除未知 window 或杀死未知进程；
- offline Agent 仍可保留稳定 Console window；
- Foreground Takeover 在实现前始终明确标注为不可用；
- tmux 地址永远不能成为任务、Worker 或 lease 的权威身份。

## 关联模块（计划）

- Fleet manifest 与 CLI 生命周期编排；
- systemd Worker unit 与 graceful-stop 协议；
- tmux workspace 协调器；
- Terminal Console/TUI；
- Panel/Console 共用的安全 Runtime 输出投影；
- Worker control、Mailbox 和 Event Journal API。
