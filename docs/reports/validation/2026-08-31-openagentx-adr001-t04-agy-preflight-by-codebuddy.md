# OpenAgentX ADR-001 T04 前置检查报告：真实 AGY Worker 连续任务（CodeBuddy verification-runner）

- 日期：2026-08-31
- 执行角色：OpenAgentX `verification-runner` Domain Agent（CodeBuddy Code，模型 `hy4-preview`）
- **OpenAgentX Task ID：`task-cf086128-2759-46e3-a0fa-8f4cc1492124`**
  - 该 Task ID 取自本任务 prompt 顶部，是本任务经 OpenAgentX Mailbox 正式派发的直接证据（非临时 CLI 会话）。
- 性质：**只读前置检查**（preflight），不启动/停止服务、不创建 Task、不写数据库、不提交 Git、不读取任何凭据
- 结论：**T04_PRECHECK_PASS**

## 1. 检查范围与边界

| 项 | 说明 |
|---|---|
| 检查对象 | T04「AGY Worker 真实连续任务闭环」的可执行前置条件 |
| 依赖链 | T04 依赖 T03；测试计划中 T03 状态 `passed`，T04 状态 `pending`（`docs/plans/2026-08-30-openagentx-adr-001-test-plan.md`） |
| 允许产物 | 仅本报告文件 |
| 禁止动作 | 服务启停/重启、建 Task、写库、Git 提交、读取或输出密码/Cookie/Session Token/Worker Token/私钥、修改冻结 ADR |
| 委派模型口径 | 只使用免费模型 `hy4-preview` |

## 2. 执行环境与五条命令证据

> 执行目录：`/home/sky/work/touzi/OneAxe/steadyflow`（仓库根目录）。每条命令后紧跟 `echo "EXIT=$?"` 记录退出码。

### 命令 1：`systemctl --user is-active openagentx.service`

- 退出码：**0**
- 输出：`active`
- 判定：OpenAgentX 控制面服务处于 active 状态，T04 所需的控制面在线。

### 命令 2：`stat -c '%a %F' OpenAgentX/run/openagentx.sock`

- 退出码：**0**
- 输出：`600 套接字`
- 判定：UDS 端点 `OpenAgentX/run/openagentx.sock` 存在，类型为套接字（socket），权限 `600`（仅属主可读写，符合最小暴露面）。与 `agents/quote-service/agent.yaml` 中 `transport: unix` + `unix_socket: run/openagentx.sock` 一致。

### 命令 3：`NO_PROXY=rtx4090 curl -sS -o /dev/null -w '%{http_code}' http://rtx4090:18100/`

- 退出码：**0**
- 输出：`200`
- 判定：OpenAgentX HTTP 端点（`rtx4090:18100`）可达并返回 200，控制面板/API 面可访问。

### 命令 4：`command -v agy`

- 退出码：**0**
- 输出：`/home/sky/.local/bin/agy`
- 判定：AGY CLI 在 PATH 中可达。与 `agents/quote-service/agent.yaml` 的 `runtime_backends[primary].options.binary: agy` 一致，且满足 `internal/runtime/agy.NewAdapter` 中的 `exec.LookPath(config.Binary)` 前置（`OpenAgentX/internal/runtime/agy/adapter.go:42`）。

### 命令 5：`agy --version`

- 退出码：**0**
- 输出：`1.1.22`
- 判定：CLI 支持 `--version` 参数，无需退避到其它探测方式。该参数正是 `Adapter.Health` 的探测命令（`OpenAgentX/internal/runtime/agy/adapter.go:86`：`exec.CommandContext(..., a.config.Binary, "--version")`），因此「Backend health online」的探测路径在本环境中可执行且已验证成功。

## 3. quote-service 身份、workspace、Runtime Adapter 核对

### 3.1 身份（`agents/quote-service/identity.yaml`）

| 字段 | 值 | 核对 |
|---|---|---|
| `version` | 1 | 具备 |
| `agent_id` | `quote-service` | 具备，与 `agent.yaml` 一致 |
| `principal_id` | `agent-quote-service` | 具备 |
| `organization_id` | `default` | 具备 |
| `display_name` | `Quote Service` | 具备 |
| `profile.instructions_path` | `ROLE.md` | 具备：`agents/quote-service/` 下存在 `ROLE.md`（同目录实际文件：`agent.yaml`、`identity.yaml`、`ROLE.md`） |
| `profile.workspace_root` | `../../..` | 具备：相对 `OpenAgentX/agents/quote-service` 解析为 `/home/sky/work/touzi/OneAxe/steadyflow`（已验证目录存在） |
| `profile.capabilities` | `quote-service-development`、`quote-service-testing`、`quote-market-integration` | 具备，与 `agent.yaml.capabilities` 三项完全一致 |

### 3.2 Runtime Adapter（`agents/quote-service/agent.yaml`）

| 字段 | 值 | 核对 |
|---|---|---|
| `transport` | `unix` | 与命令 2 的 socket 存在性一致 |
| `unix_socket` | `run/openagentx.sock` | 与命令 2 路径一致（相对 `OpenAgentX/`） |
| `runtime_backends[0].backend_id` | `primary` | 具备 |
| `runtime_backends[0].adapter_id` | `agy-batch` | 具备；`internal/runtime/agy/adapter.go:60` 的 `Descriptor().AdapterID` 恰为 `agy-batch`，可装配 |
| `runtime_backends[0].options.binary` | `agy` | 命令 4 验证可达 |

Adapter 契约侧（`internal/runtime/agy/adapter.go`）与 T04 相关的能力声明：

- SessionMode：支持 `new` 与 `resume`（`adapter.go:63`），覆盖 T04「按 turn 启动、Worker 长期在线」所需的 new-turn 语义。
- `StartTurn` 以 argv 启动 `agy --print --output-format stream-json`，prompt 仅经 stdin 传入（`adapter.go:99-107`），streams=true，与「按 turn 启动 CLI 进程」的 T04 断言一致。
- `Steer` 返回 `ErrSteerUnsupported`、`DecideApproval` 返回 `ErrApprovalUnsupported`（`adapter.go:194-198`），`Cancel` 为 `CancelProcessSignal`（SIGTERM）。T04 步骤 3—6 的连续 Task、超时、非零退出、输出解析失败等断言均由进程退出码 + 输出解析归类（uncertain / `side_effects_known=false`，`adapter.go:164-178`）支撑，退出路径确定。

### 3.3 T04 验收断言是否具备

T04 文档（`docs/plans/2026-08-30-openagentx-adr-001-testing/04-agy-worker-consecutive-tasks.md`）已含完整的「步骤（6 步）」与「通过条件」，断言具备：

1. 步骤 1「验证 AGY binary、模型凭据、workspace 和 AgentProfile」——binary 已验证（命令 4/5）、workspace 已解析并存在、AgentProfile（identity.yaml + ROLE.md + agent.yaml）齐全；**模型凭据一项本次未验证**，见第 4 节。
2. 步骤 2「启动 quote-service Worker，确认 Backend health online」——依赖启动动作，本次按边界不执行；但 health 探测命令（`agy --version`）已证明可执行且成功（命令 5）。
3. 步骤 3—5（Task A → turn 结束子进程释放且 Worker 保持 online → 不经 tmux 自动唤醒执行 Task B 并形成独立 RunAttempt）——断言明确、可检查，Adapter 的按 turn 启动 + streams 解析 + 退出码归类提供实现支撑。
4. 步骤 6「超时、AGY 非零退出、输出解析失败均形成确定 Task/Run 状态」——`adapter.go:159-183` 的 wait/parse/stderr-limit 归类逻辑提供实现支撑。
5. 通过条件「真实 AGY 连续完成两次工作、模型/推理参数按 ExecutionSpec 生效、Worker 生命周期长于两个 turn、日志中不存在 tmux 控制路径」——断言完整，需在 T04 执行阶段取证。

## 4. 未覆盖项（不构成前置阻塞，需在 T04 执行阶段关闭）

| # | 未覆盖项 | 原因 | 关闭方式 |
|---|---|---|---|
| 1 | AGY 模型凭据有效性 | 本任务边界禁止读取/输出密码、Cookie、Session Token、Worker Token、私钥；`agy --version` 仅证明 CLI 可执行，不证明模型鉴权可用 | T04 步骤 2 启动 Worker 后，以 Backend health online + 首个真实 turn 成功作为凭据有效性的实测证据 |
| 2 | Worker 实际启动与 Backend health online | 本任务禁止启动/停止/重启服务 | T04 执行阶段启动 `quote-service` Worker 后取证 |
| 3 | 真实 turn 的端到端往返（Task A/B） | 本任务只读前置检查，禁止创建 Task、写数据库 | T04 步骤 3—6 取证 |

## 5. 结论

**T04_PRECHECK_PASS**

- 五条命令退出码全为 0：`systemctl --user is-active openagentx.service`=0（`active`）、`stat` socket=0（`600 套接字`）、`curl http://rtx4090:18100/`=0（`200`）、`command -v agy`=0（`/home/sky/.local/bin/agy`）、`agy --version`=0（`1.1.22`）。
- quote-service 身份（`agent_id`/`principal_id`/`organization_id`/`display_name`/`instructions_path=ROLE.md`/三项 capabilities）齐全且与 `agent.yaml` 一致。
- workspace `../../..` → `/home/sky/work/touzi/OneAxe/steadyflow` 存在。
- Runtime Adapter `agy-batch` 在 `agent.yaml` 中声明为 primary，且 `internal/runtime/agy` 的 `Descriptor().AdapterID` 为 `agy-batch`，装配路径成立；`binary: agy` 可达且 health 探测参数 `--version` 与 Adapter 实现一致。
- T04 步骤与通过条件具备，可进入执行阶段。
- 阻塞点：**无**。第 4 节三项为 T04 执行阶段取证项，不是前置阻塞。
- 下一条最小动作：在 T04 执行阶段启动 `quote-service` Worker（Backend `agy-batch`），确认 health online 后提交第一个明确、可逆、可检查的 Task A。
