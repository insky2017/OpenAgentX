---
doc_type: decision
status: accepted
canonical: true
owner: openagentx
updated_at: 2026-09-14
---

# ADR-008: OAX Workspace 绑定、交互式 Console TUI 与可撤销 CLI 会话

## 状态

已接受 (Accepted)，尚未实施。

## 日期与决策者

- 日期：2026-09-14
- 决策者 / 所有者：OpenAgentX Team / OpenAgentX Maintainer

## 与既有决策的关系

本 ADR 根据首轮 ADR-005 实现的真实体验，修订 [ADR-005](ADR-005-interactive-worker-console-and-developer-experience.md) 中 tmux workspace 和 Console 用户体验的具体决策：

- 固定 workspace session 从 `agentx` 改为 `OAX`；
- Agent window 从“只能有 pane `0`”改为“pane `0` 是稳定 Console pane，允许并保留其他 pane”；
- 用户显式执行 Console Attach 时，允许在严格前置检查后将当前 window 绑定并重命名为 Agent；
- 行式 REPL 和原始 JSON 刷屏改为真正的全屏 TUI；
- 增加默认本地路径和可撤销的持久化 CLI 会话；
- 删除 `attach --once` 和独立的 `console status`，将状态纳入 Attach TUI。

ADR-005 的下列原则继续有效：

- systemd 是 Worker 默认宿主，关闭 SSH、tmux 或 Console 不影响 Worker；
- tmux 不是 Agent 身份、任务路由、generation、lease 或业务状态的权威来源；
- dispatch、steer、cancel、approval 和 Worker lifecycle 操作必须经过正式 API、鉴权、CAS、持久化和审计；
- Fleet 初始化保持幂等和非破坏，不自动扫描 `agents/*`，不删除未知 window，不杀死未知进程；
- Runtime 输出只能使用结构化、限量、脱敏的安全投影；
- graceful drain-and-stop 和独立危险 force-stop 的语义不变；
- Foreground Takeover 继续只显示“规划中，暂不可用”。

ADR-006 和 ADR-007 保持 Proposed，本 ADR 不授权实现其领域语义。

## 背景

ADR-005 首轮实现验证了 Fleet、持久化 Worker lifecycle、Console API 和安全输出流的主体架构，但实际使用暴露了以下用户体验和正确性问题：

1. `console attach --once` 输出一次 JSON 后退出，用户容易误以为 Console 或 Fleet 初始化失败；
2. 未指定 `--agent` 时只有严格的 tmux window 推导，没有交互式 Agent 列表；
3. 首轮实现使用 `agentx` session，但实际本地工作台和确认后的产品入口为 `OAX`；
4. 首轮 workspace 将“稳定 pane `0`”错误收紧为“window 只能有一个 pane”，妨碍用户保留辅助 shell、日志和编辑器 pane；
5. Attach 不会把用户明确选择的 Agent 绑定到当前 window，也不会按 Fleet 规则稳定命名 window；
6. Console 每次执行都要求输入密码，缺少安全、可撤销的本地 CLI 会话；
7. 当前所谓 Console 是并发行式 REPL，事件会冲刷输入区，并且原始 JSON 不适合日常交互；
8. Follow 从 sequence `0` 开始会重放大量历史 heartbeat；旧 generation 事件还可能回退客户端当前 Worker 快照；
9. socket、数据库和 Fleet manifest 每次都要显式传入，偏离本地日常使用路径。

## 决策

### 1. 本地默认路径

用户级安装采用统一的 OpenAgentX home：

```text
~/.openagentx
```

默认路径为：

```text
socket          ~/.openagentx/run/openagentx.sock
database        ~/.openagentx/data/openagentx.db
Fleet manifest  ~/.openagentx/fleet.yaml
Worker configs  ~/.openagentx/workers/<agent-id>.yaml
CLI credentials ~/.openagentx/credentials.json
```

路径解析优先级为：

```text
显式命令参数
-> OPENAGENTX_HOME 或对应显式环境配置
-> ~/.openagentx 默认值
```

程序必须通过用户 home 目录解析路径，不能把未展开的 `~` 直接交给文件 API。系统级部署可以显式选择 `/etc`、`/var/lib`、`/run` 和 `/opt` 路径，但不得通过模糊探测在用户级与系统级配置之间猜测。

因此日常入口允许无路径参数运行：

```bash
openagentx console
openagentx console login
openagentx console attach
openagentx fleet init
openagentx fleet status
openagentx fleet up
openagentx fleet down
```

现有 `--socket`、`--db` 和 `--file` 保留为覆盖参数。Fleet 在任何 tmux 或 systemd 副作用前，仍必须验证 manifest Worker 配置与所选宿主实际读取的 canonical Worker 配置一致。

### 2. 固定使用 `OAX` workspace

本地 tmux 工作台固定使用大小写敏感的 session 名：

```text
OAX
```

稳定地址为：

```text
OAX:overview.0
OAX:<agent-id>.0
```

pane `0` 是每个 Agent window 的稳定 Console pane。一个 Agent window 可以有 pane `1`、pane `2` 等用户自建辅助 pane；Fleet 和 Console 必须保留它们，不关闭、不重排、不读取其内容，也不根据其进程推断业务状态。

window index 和 `%pane_id` 仍不构成身份。产品不得使用 `send-keys`、`paste-buffer` 或 `capture-pane` 传递业务命令、读取任务结果或控制 Worker。

### 3. Console Attach 的 workspace 前置检查

交互式 Attach 必须在变更 window 前检查当前 tmux 上下文：

- 当前进程必须位于 tmux 中；
- session 必须精确为 `OAX`；
- 当前 pane index 必须为 `0`；
- 当前 window 和目标 Agent window 的名称、managed 标记与冲突状态必须可唯一判断。

window 有多个 pane 不是错误；只要调用发生在 pane `0` 即可。调用发生在其他 pane 时必须拒绝 Attach，并明确显示当前位置和切换到 pane `0` 的操作提示。调用发生在 tmux 外或其他 session 时同样拒绝，不自动创建、切换或接管用户 tmux 现场。

这一检查只决定 Console workspace 是否允许绑定，不把 tmux 状态用于 API 鉴权、Agent 身份验证、任务路由或 Worker lifecycle。

### 4. Agent 解析和选择顺序

Attach 按以下固定优先级确定逻辑 Agent：

1. 命令显式提供 `--agent <agent-id>` 时直接选择该 Agent，不显示列表；
2. 未显式提供时，若当前 `OAX` pane `0` 所在 window 已由 OpenAgentX 绑定，且名称精确、唯一地匹配授权可见 Agent，则直接选择；
3. 仍无法确定时，在 TUI 中显示经认证控制面返回的 Agent 列表，由用户选择；
4. 非交互环境无法选择时 fail closed，要求显式 `--agent`。

Agent 列表来自正式、鉴权后的控制面安全投影，可以显示 online/offline/draining、当前 generation 和简要活动状态。不得扫描 `agents/*` 或根据 tmux 中的任意名称生成权威 Agent 列表。

### 5. 显式 Attach 可以绑定并重命名当前 window

用户在符合前置条件的 `OAX` pane `0` 中显式执行 Attach，构成将当前 window 绑定到所选 Agent 的明确操作。选择完成后：

1. 再次验证目标 Agent 和 window 冲突；
2. 将当前 window 重命名为精确的 `agent_id`；
3. 写入 OpenAgentX managed 标记和逻辑 Agent 标记；
4. 在当前 pane `0` 启动该 Agent 的 Console TUI；
5. 退出 TUI 后保留 window 名称、managed 标记和其他 pane。

这是 ADR-005“自动协调不得重命名用户 window”的窄例外：只有用户当前主动执行 Attach 才允许绑定当前 window。`fleet init` 的后台或批量协调仍不得静默重命名未知 window。

冲突处理必须非破坏：

- 当前 window 已绑定同一 Agent 时原位复用；
- 当前 window 已绑定另一 Agent 时不得静默覆盖，必须要求明确确认重新绑定；
- 目标名称已被另一个合法受管 window 占用时，不创建重复名称，可提示用户导航到已有 window；
- 目标名称被 unmanaged 或不兼容 window 占用时拒绝，不杀进程、不猜测、不覆盖；
- window 的其他 pane 始终保留。

允许使用 tmux 的 window 枚举、标记、`rename-window` 和显式 workspace 导航。它们只能服务本地界面布局，不能成为业务控制路径。

### 6. `openagentx console` 是 TUI 主入口

单独执行：

```bash
openagentx console
```

必须进入交互式 TUI 菜单，而不是打印 usage。菜单至少包含：

```text
Normal Console Attach
Diagnostic Attach
Login / Replace Login
Logout
Foreground Takeover（规划中，暂不可用）
Exit
```

菜单顶部显示本地 CLI 会话状态，例如未登录，或当前用户名和会话剩余有效期。Foreground Takeover 始终禁用，不能通过菜单、隐藏参数或 tmux 操作绕过。

直接命令仍支持：

```bash
openagentx console login
openagentx console logout
openagentx console attach [--agent <agent-id>] [--diagnostic]
```

### 7. 删除 `attach --once` 和独立 Console Status

Console Attach 的语义固定为进入持续更新的交互式 TUI。因此删除公开的：

```text
console attach --once
console status
```

自动化观察应调用正式 Observe API，而不是复用交互式 Console 命令。

Attach TUI 始终显示精简状态栏，并提供内部命令：

```text
/status
```

`/status` 打开或聚焦详细状态面板，不向时间线打印原始 JSON。状态面板至少包含 Agent、Worker 状态、generation、Backend 健康、当前 RunAttempt、drain 状态、最近 heartbeat age、lease、连接状态、Event Journal cursor 和当前 CLI 会话身份。

### 8. Console 必须是真正的全屏 TUI

行式 `bufio.Scanner` REPL 和连续 JSON 输出不再作为产品 Console。交互 Attach 使用真正的终端 UI，至少划分为：

- 顶部 Agent、模式、Worker、generation 和连接摘要；
- 可滚动的安全时间线；
- 状态栏；
- 固定、独立、可编辑的底部输入区域；
- 帮助、状态或审批等覆盖面板。

后台事件更新不得冲刷、截断或重排用户正在编辑的输入。TUI 必须处理终端 resize、断线重连、退出和取消。默认视图不得输出原始 JSON；诊断信息也必须经过相同的安全投影、授权、大小限制和限流。

TUI 至少支持：

```text
/status
/dispatch <content>
/steer <task-id> <expected-version> <content>
/cancel <task-id> <expected-version>
/approve <approval-id> <expected-version>
/reject <approval-id> <expected-version>
/diagnostic
/help
/quit
```

所有写操作继续走正式 API。`/quit` 只退出 Console，不停止 Worker，不撤销已持久化命令。

### 9. Attach 使用一致快照和 Event Journal high-water cursor

Attach 响应必须把安全状态快照与该快照对应的 Event Journal high-water cursor 一并返回。首次实时订阅从该 cursor 之后开始，不能默认从 sequence `0` 重放完整历史。

最近时间线如果需要补放，应通过单独的、数量和大小受限的查询加载，并默认排除重复 heartbeat。断线重连从客户端最后确认的 sequence 恢复；出现 retention gap 时重新获取一致快照和新 cursor，而不是猜测缺失状态。

客户端状态 reducer 必须满足：

- sequence 不倒退；
- 旧 generation 的 Worker 事件不能覆盖当前 generation；
- 不匹配当前 WorkerInstanceID 的同代事件不能替换当前快照；
- attach、重连和 lifecycle 命令使用的 generation 必须来自最新权威快照；
- 历史事件只能进入受限时间线，不能回写当前状态。

### 10. Heartbeat 只更新状态，不默认进入时间线

Worker heartbeat 仍由服务端持久化和用于 lease/reconciliation，但 Console 默认不逐条显示。它只更新 TUI 内部状态和状态栏中的：

- last heartbeat age；
- lease remaining/expired；
- online、draining、offline；
- generation 和连接健康。

只有有意义的转换进入普通时间线，例如 generation 变化、online → draining、draining → offline、lease 异常或 heartbeat 丢失。Diagnostic Attach 可以显示聚合后的 heartbeat 计数、延迟和最近时间，但仍不得逐条刷屏。

### 11. Console Login 使用可撤销 CLI Token

密码只在显式登录时输入：

```bash
openagentx console login
```

登录流程为：

1. 连接解析后的控制面 UDS；
2. TUI 或安全终端表单读取用户名和不回显的密码；
3. 服务端验证密码；
4. 服务端生成高熵、不可预测、可撤销并有到期时间的 opaque CLI Token；
5. 服务端只持久化 Token 哈希、token ID、主体、scope、installation audience、创建/到期/最近使用和撤销状态；
6. 客户端仅保存原始 Token 和必要元数据，不保存明文密码；
7. 后续 Console 和需要同一控制面权限的本地 CLI 调用自动携带 Token。

默认凭据文件为：

```text
~/.openagentx/credentials.json
```

其父目录权限必须不宽于 `0700`，文件权限必须不宽于 `0600`；权限过宽时客户端拒绝使用并给出修复提示。凭据按 canonical socket、服务端 installation ID 和 username 隔离，不能把一个控制面的 Token 静默发送到另一个控制面。

Token 默认最长有效期为 30 天，可由服务端策略缩短。Token 过期、撤销、audience 不匹配或服务端返回未认证时，Attach 进入登录流程或给出明确登录提示，不能降级为未鉴权访问。修改 owner 密码时应撤销既有 CLI Token。

不新增独立的 `openagentx auth status`。CLI 会话状态显示在 `openagentx console` 主菜单和 Attach 的 `/status` 面板中。未来若 Token 成为跨产品的通用认证层，再用独立 ADR 决定是否提升为全局 `auth` 命令组。

### 12. Logout 撤销服务端 Token 并清除本地凭据

```bash
openagentx console logout
```

正常流程先请求服务端撤销当前 Token，再删除本地副本。服务端不可达时仍删除本地 Token，并明确提示远端撤销未确认。Logout 不清除浏览器会话，不停止 Worker，不取消 Task，也不改变 Fleet lifecycle intent。

### 13. Fleet 默认入口与缺失 manifest

`openagentx fleet init` 默认使用本 ADR 的 database、socket 和 manifest 路径。manifest 不存在时不得扫描 Agent 目录或静默生成启动清单；交互终端可以提示创建，并从经认证控制面列出的 Agent 中让用户显式选择。非交互环境必须明确报错并要求提供 manifest。

Fleet 创建或协调 `OAX` workspace 时复用本 ADR 的命名和 pane `0` 规则：

- 创建缺失的 `overview` 和 Agent window；
- 复用兼容受管 window；
- 允许并保留额外 pane；
- 不重命名、删除或杀死未知现场；
- 冲突时在任何副作用前 fail closed。

Fleet 创建的 pane `0` 退出 Console 后应保留稳定 window，并允许用户重新进入 Attach；不得因为 Console 退出而停止 Worker。

## 被拒绝或替代的方案

1. **保留 `attach --once`**：拒绝。与持续 Attach 心智冲突；自动化使用 Observe API。
2. **增加独立 `console status`**：拒绝。状态是 Attach TUI 的持续视图和 `/status` 面板。
3. **继续输出所有 Journal JSON**：拒绝。破坏输入体验，也会重放无意义 heartbeat。
4. **保存明文密码**：拒绝。只保存可撤销、有期限的 CLI Token。
5. **只依赖 UDS 文件权限而取消应用鉴权**：拒绝。UDS 权限是附加边界，不替代主体、权限和审计。
6. **Attach 在 tmux 外自动创建或进入 `OAX`**：拒绝。必须提示用户显式进入正确 workspace。
7. **因为 window 有额外 pane 而拒绝 pane `0` Attach**：拒绝。额外 pane 是允许且必须保留的用户现场。
8. **让 tmux window 决定权威 Agent 身份**：拒绝。window 只提供默认选择，最终 Agent 必须由控制面验证。
9. **借 TUI 实现 Foreground Takeover**：拒绝。该能力继续延期。

## 影响与后果

### 正向收益

- `openagentx console` 成为可发现、低参数的统一入口；
- 用户登录一次后可以安全复用可撤销会话，不再每次输入密码；
- Agent 选择、当前 window 推导和稳定命名形成一致工作流；
- 用户可以在 Agent window 中保留辅助 pane；
- 输入区不再被 Runtime 或 heartbeat 事件冲刷；
- high-water cursor 消除全量历史重放和旧 generation 回退；
- 默认本地路径显著减少日常参数。

### 成本与限制

- 需要引入并维护终端 TUI 状态机、resize 和输入编辑；
- 需要新增 CLI Token 数据模型、迁移、撤销和本地安全存储；
- Attach API 需要提供一致 cursor，并实现受限历史时间线；
- workspace 协调器需要支持 pane `0` 与额外 pane 共存；
- `OAX` 与旧 `agentx` workspace 不自动合并，迁移必须显式、非破坏；
- Attach 依赖正确的 tmux 上下文，tmux 外调用会被拒绝。

## 实施顺序

1. 默认路径解析和 `OAX` workspace/pane `0` 模型；
2. Attach 一致快照、high-water cursor、generation reducer 和 heartbeat 合并；
3. 可撤销 CLI Token、`console login/logout` 和安全本地存储；
4. `openagentx console` 主菜单、Agent 选择器和真正的 Attach TUI；
5. 显式 window 绑定、重命名、冲突提示和多 pane 协调；
6. Fleet 默认参数、缺失 manifest 引导和 user-systemd canonical 配置一致性；
7. 文档、定向测试和真实 tmux/UDS 体验验证。

## 验收不变量

- `openagentx console attach --agent quote-service` 在 `OAX` pane `0` 中直接绑定并进入 TUI；
- `openagentx console attach` 在已绑定 Agent window 中直接选择该 Agent，否则显示授权 Agent 列表；
- Attach 在非 `OAX` session、tmux 外或非 pane `0` 时拒绝且不修改 tmux；
- window 有额外 pane 时 pane `0` 仍可 Attach，其他 pane 完全保留；
- window 名冲突不会杀进程、覆盖现场或创建歧义映射；
- 默认命令无需重复传入 socket、database 或 Fleet manifest；
- `console login` 后不再重复要求密码，本地没有明文密码；
- `console logout` 清除本地 Token，并在可达时撤销服务端 Token；
- Console 是全屏 TUI，输入区不被事件刷新破坏；
- `/status` 显示最新安全状态，不向时间线倾倒 JSON；
- 首次 Attach 不从 sequence `0` 重放完整 Journal；
- heartbeat 不逐条进入普通时间线，旧 generation 不覆盖新 generation；
- 所有控制写入仍经过正式 API、权限、CAS、持久化和审计；
- 关闭 TUI、window、tmux 或 SSH 不停止 systemd Worker；
- Foreground Takeover 始终显示“规划中，暂不可用”。

## 关联模块（计划）

- CLI 默认路径和本地 profile；
- Console 主菜单、Agent selector、TUI 和输入编辑器；
- Console Attach API、Event Journal cursor 和客户端 reducer；
- CLI Token repository、认证 handler 和 credential store；
- Fleet manifest、tmux workspace 与 user-systemd 配置；
- Normal/Diagnostic 共用的安全输出投影。
