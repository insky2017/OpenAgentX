---
doc_type: design_and_implementation_spec
status: implemented_and_validated
updated_at: 2026-08-16
---

# AgentBus Agent 角色持久化与自动 Bootstrap

## 1. 背景与目标

当前 Agent Registry 只让 Hub 知道 `agent_id/role/address`，不会自动把身份与职责写入 Agent 的上下文。现有 `%51/%52` 依赖人工发送“读取运行说明”的一次性提示；当前 `%50` Codex 也只因本次对话知道自己是 Orchestrator。Runtime 重启、新 pane、上下文清空或未来新增南向 Agent 后，这种认知不会自动恢复。

本设计将角色认知变成 AgentBus 的正式生命周期协议：

```text
Role Manifest + ROLE.md
          ↓
attach / launch
          ↓
Agent Registry + Agent Session(generation)
          ↓
TmuxConnector 注入短 Bootstrap
          ↓
Agent 读取 ROLE.md
          ↓
session ready
          ↓
允许参与 Task
```

目标：

1. Orchestrator 与南向 Agent 使用同一种角色定义和握手协议。
2. 每次启动、重新 attach 或主动 bootstrap 都生成新 generation，旧 ready 不能误确认新 session。
3. Agent 未完成 `session ready` 时不能作为 Task sender/target 执行任务协议。
4. 每条 Task 通知都携带简短身份提醒和 ROLE 路径，降低上下文压缩后的角色遗忘风险。
5. TmuxConnector 仍只注入短控制消息，不注入 Task 正文，不解析 pane 输出。
6. 新 Runtime 通过 `agent launch` 或启动后立即 `agent attach` 接入；禁止把“裸启动且未 attach”视为受管 Agent。

## 2. 保证边界

AgentBus 无法修改已经运行的 Agent 进程环境，也无法从语言模型内部证明“它永久记住了角色”。V0.1 的保证方式是：

- canonical role 文件持久存在；
- 每个受管 session 必须显式 bootstrap/ready；
- 未 ready 的身份不能参与 Task；
- 每次 Task 通知重复最小身份信息；
- Runtime 重启后必须重新 attach，或由 `agent launch` 自动完成。

这是一种可验证的生命周期保证，不是依赖一次 prompt 的心理假设。

V0.1 仍属于单机同 Unix 用户信任域。`agent_id` 与 `generation` 用于防误操作和状态一致性，不构成强认证。

V0.1 不承诺兼容旧版 V0 的数据库、API 或命令行为。验收只以本文件定义的新生命周期契约为准；旧运行数据若能被现有 schema 初始化自然保留属于实现结果，不是必须维持的兼容保证。

## 3. 文件布局

新增 canonical 配置：

```text
AgentBus/
├── agents/
│   ├── orchestrator/
│   │   ├── agent.yaml
│   │   └── ROLE.md
│   ├── agentbus-agent/
│   │   ├── agent.yaml
│   │   └── ROLE.md
│   └── quote-service/
│       ├── agent.yaml
│       └── ROLE.md
└── docs/design/AGENT_ROLE_BOOTSTRAP.md
```

`agent.yaml` 是机器事实源；`ROLE.md` 是 Agent 每个 session 必须读取的职责说明。现有 `docs/runtime/*_INSTRUCTIONS.md` 改为导航入口，链接到 canonical ROLE 文件，不再维护重复正文。

## 4. Manifest 契约

采用 YAML，版本固定为 `1`：

```yaml
version: 1
id: orchestrator
role: orchestrator
runtime: codex
connector: tmux
address: "%50"
workspace: ".."
instructions: "ROLE.md"
capabilities:
  - understand
  - decompose
  - delegate
  - review
```

字段规则：

- `version`：必须为 `1`。
- `id/role/runtime`：使用现有安全 identifier 字符集，最长 64。
- `connector`：V0.1 为 `tmux` 或 `none`。
- `address`：tmux 地址；`attach --address` 或 `TMUX_PANE` 可以覆盖。
- `workspace`：相对 AgentBus repo root 解析；`..` 表示当前 SteadyFlow 父工作区。允许绝对路径，但必须 clean 后存储。
- `instructions`：相对 manifest 文件解析，必须存在、为普通文件且可读；解析后存储绝对路径。
- `capabilities`：安全 identifier 数组，去重、稳定排序后以 JSON 存储。
- 拒绝未知 YAML 字段、控制字符、空 ROLE 文件、目录穿越后不存在路径和超过 1 MiB 的 manifest/ROLE。

三个首发 manifest：

```text
orchestrator     role=orchestrator runtime=codex address=%50
agentbus-agent   role=agentbus     runtime=agy   address=%51
quote-service    role=quote        runtime=agy   address=%52
```

## 5. ROLE.md 最小内容

每个 ROLE 文件必须明确：

- `agent_id`、角色、Runtime 与当前职责；
- 与 Orchestrator/其他 Agent 的协作关系；
- 可执行能力和禁止边界；
- AgentBus CLI 路径发现方式；
- `get → ack → status → complete/fail` 任务循环；
- 收到 supplement 后重新 `task get`；
- 不直接调用 TmuxConnector，不捕获 pane 输出判断完成；
- 启动后使用通知给出的 generation 调用 `session ready`；
- context 重置或身份不确定时先 `agent whoami`，必要时请求重新 bootstrap，不能猜测角色。

Orchestrator ROLE 还必须规定：理解用户需求、拆解与路由、通过 AgentBus 委派、审查结果；除非任务明确要求，不替南向 Agent执行其实现职责。

## 6. 数据模型

Registry、Profile 与 Session 分表保存，保持身份注册、角色配置与进程生命周期三个边界独立。新增：

```sql
CREATE TABLE IF NOT EXISTS agent_profiles (
    agent_id TEXT PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    manifest_version INTEGER NOT NULL,
    runtime TEXT NOT NULL,
    workspace TEXT NOT NULL,
    config_path TEXT NOT NULL,
    instructions_path TEXT NOT NULL,
    capabilities_json TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS agent_sessions (
    agent_id TEXT PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    generation INTEGER NOT NULL,
    status TEXT NOT NULL,
    resolved_pane_id TEXT NOT NULL,
    delivery_error TEXT,
    started_at TEXT NOT NULL,
    ready_at TEXT,
    updated_at TEXT NOT NULL
);
```

Session 状态：

```text
bootstrapping
ready
delivery_failed
```

不保存 ROLE 正文，只保存 canonical 绝对路径。每次 attach/bootstrap 都在事务内执行：

1. generation = previous + 1，首次为 1；
2. 写入 `bootstrapping`；
3. 完成 pane probe 和 Bootstrap delivery；
4. 成功保持 `bootstrapping`，等待 ready；失败改为 `delivery_failed` 并保存错误。

`session ready` 只有 generation 与当前 generation 精确匹配时才能把状态改为 `ready`；旧 generation 返回冲突错误。

## 7. CLI 契约

新增：

```bash
# 读取本地 canonical 身份，不需要 daemon
agentbus agent whoami --config agents/orchestrator/agent.yaml

# 把已经运行的交互 Agent 绑定到 AgentBus，并注入 Bootstrap
agentbus agent attach --config agents/orchestrator/agent.yaml
agentbus agent attach --config agents/quote-service/agent.yaml --address %52

# 重新注入角色（context reset / 手工恢复）；generation 自动 +1
agentbus agent bootstrap --id quote-service

# Agent 读完 ROLE 后显式确认
agentbus session ready --agent quote-service --generation 2

# 查询当前 profile/session
agentbus session show --agent quote-service

# 从当前 tmux pane 启动新的 Runtime；`--` 后直接 argv 执行，不经 shell
agentbus agent launch --config agents/quote-service/agent.yaml -- <runtime-command> [args...]
```

所有 daemon client 命令继续支持 `--socket` 和 `AGENTBUS_SOCKET`。

### 7.1 `agent whoami`

- 加载并严格校验 manifest/ROLE；
- 输出 JSON：resolved manifest、绝对 workspace/config/instructions 路径、推荐环境变量；
- 不注册、不通知、不修改 daemon 状态；
- 用于 Agent 对身份不确定时自检。

### 7.2 `agent attach`

- 本地加载 manifest并解析路径；
- `--address` 优先，其次 manifest address；若为 `auto` 或空且存在 `TMUX_PANE`，使用 `TMUX_PANE`；
- 调用 daemon attach API，原子 upsert Agent/Profile/Session；
- daemon 用 TmuxConnector probe 并注入 Bootstrap；
- JSON 输出 profile、session generation 和 delivery disposition；
- 支持 `--no-notify`，只用于当前 Orchestrator 自绑定或自动化测试：仍创建 bootstrapping session，但不注入 pane；调用者随后必须手工读取 ROLE 并 `session ready`。

### 7.3 `agent bootstrap`

- 从数据库读取现有 Agent/Profile；
- generation +1，session 回到 bootstrapping；
- 重新 probe 地址并注入 Bootstrap；
- profile 不存在时 fail closed，不能退回旧人工说明。

### 7.4 `session ready/show`

- `ready` 请求包含 agent ID 与 generation；
- generation 不匹配返回 `SESSION_GENERATION_CONFLICT`；
- `show` 输出 Agent、Profile、Session；
- 未 attach 返回 `AGENT_SESSION_NOT_FOUND`。

### 7.5 `agent launch`

- 必须在 tmux 中执行；地址默认使用 `TMUX_PANE`，可由 `--address` 覆盖；
- `--` 后命令以 `exec.Command` direct argv 启动，禁止 shell 拼接；
- 子进程继承 stdio，并增加：

  ```text
  AGENTBUS_AGENT_ID
  AGENTBUS_ROLE
  AGENTBUS_CONFIG
  AGENTBUS_INSTRUCTIONS
  AGENTBUS_SOCKET
  ```

- 子进程启动后由 wrapper 调用 attach/bootstrap；Bootstrap 通过 tmux 注入当前 pane；
- 支持 `--bootstrap-delay`，默认 2 秒，范围 0–30 秒；
- wrapper 等待并返回子进程退出码；收到 context cancel 时终止子进程；
- 命令缺失、非 tmux 环境、pane 与 manifest/override 冲突时拒绝启动。

该 generic launcher 只负责生命周期和环境，不猜测 Codex/AGY/Claude/OpenCode 专用参数；未来 Adapter 可以调用同一 attach/session API。

## 8. HTTP/Service 契约

新增本机 Unix socket API：

```text
POST /api/v1/agents/attach
POST /api/v1/agents/{id}/bootstrap
GET  /api/v1/agents/{id}/session
POST /api/v1/sessions/{agent_id}/ready
```

API body 上限继续为 1 MiB。路径与标识符必须服务端重复校验，不能只信 CLI。

建议 Domain：

```text
AgentProfile
AgentSession
AttachAgentRequest/Response
SessionStatus
```

Store 操作必须保证 Agent/Profile/Session 一致；ready generation compare-and-set 在 SQL transaction 中完成。

## 9. Bootstrap 与 Task 通知

Bootstrap 固定短消息：

```text
[AgentBus Bootstrap] agent_id=<id> role=<role> generation=<n>. Read <absolute ROLE.md path>, then use AgentBus CLI: session ready --agent <id> --generation <n>
```

Task 通知调整为：

```text
[AgentBus][agent=<id> role=<role>] New task <task-id>. If role context is uncertain, read <ROLE.md>. Use AgentBus CLI: task get <task-id> --agent <id>
```

Supplement 通知同样携带 `agent/role/ROLE.md`。所有字段拒绝 CR/LF/TAB；仍通过唯一 tmux buffer 注入，不经过 shell。

## 10. Task Ready Gate

新增 `AGENT_NOT_READY` 错误（HTTP 409）：

- submit 前要求 sender 与 target 都有 `ready` session；
- `get/list/watch/send/cancel` 要求 caller/sender ready；
- `ack/status/complete/fail` 要求 target actor ready；
- delivery 失败或重新 bootstrap 后，在 session 再次 ready 前拒绝新操作；
- 系统内部写 Event 不受 gate 影响。

启动策略：任何缺少 Profile/Session 的 Agent 都不会被默认为 ready。必须对 `%50/%51/%52` 依次 attach/ready；不为旧注册状态提供兼容旁路。

## 11. 首发 ROLE 定义

### Orchestrator `%50`

- `id=orchestrator role=orchestrator runtime=codex`；
- 用户北向入口；负责理解、拆解、路由、补充消息、watch 和审核；
- 通过 AgentBus 委派，不把所有工作上下文混入同一 session；
- 当前活跃会话可用 `attach --no-notify`，由操作者读取 ROLE 后手工 ready，避免向正在执行的 Codex turn 注入文本。

### AgentBus Agent `%51`

- `id=agentbus-agent role=agentbus runtime=agy`；
- 负责 AgentBus Go 代码、测试和明确授权的提交；
- 不修改 SteadyFlow 其他模块，不操作真实 panes，除非 Task 明确是运行验收。

### Quote Service Agent `%52`

- `id=quote-service role=quote runtime=agy`；
- 负责 Quote Service 范围；遵守 SteadyFlow 行情唯一入口规则；
- 不访问外部行情源绕过 Quote Service，不修改无关模块。

## 12. 测试要求

至少覆盖：

1. YAML unknown fields、恶意 ID、控制字符、空/超大 ROLE、路径解析。
2. profile/session schema 在现有 V0 数据库上无损升级。
3. attach generation 单调递增与 profile upsert。
4. ready 精确 generation compare-and-set，旧 generation 拒绝。
5. bootstrap live/dead pane、delivery failure 与 no-notify。
6. Task ready gate 覆盖 sender、target、caller 和重新 bootstrap 后拒绝。
7. Task/Supplement 通知含 identity reminder，不含 Task 正文。
8. `whoami/attach/bootstrap/session ready/show` CLI 流程。
9. `launch` direct argv、环境变量、退出码与缺少 tmux 环境拒绝；测试不得启动真实 Codex/AGY。
10. daemon 重启后 profile/session/generation 持久可读。

执行：

```bash
cd AgentBus
gofmt -w .
go mod tidy
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./...
go build -o bin/agentbus ./cmd/agentbus
```

## 13. 真实 tmux 验收

代码审核通过后由 Codex 执行，不由实现 Agent 抢占真实 pane：

1. 重启 `%53` daemon。
2. `%50` 使用 Orchestrator manifest `attach --no-notify`，Codex 读取 ROLE 后执行 ready。
3. `%51/%52` 执行 attach，确认收到 Bootstrap、读取各自 ROLE 并 ready。
4. `session show` 三者均为 ready，generation 一致。
5. 对 `%51/%52` 各提交无副作用 Task，验证通知带 identity reminder、完整任务状态成功。
6. 对 `%52` 重新 bootstrap，确认 ready gate 暂时拒绝任务；再次 ready 后恢复。
7. daemon 重启后 profile/session 与 ready 状态可读取。

上述验收已于 2026-08-16 完成。三个首发 Agent 均通过 canonical manifest 恢复角色；Quote Service Agent 重新 bootstrap 到 generation 2 后，ready gate 正确拒绝任务并在 ready 后恢复。完整证据见 [角色 Bootstrap 真实 tmux E2E 验证报告](../reports/validation/2026-08-16-agent-role-bootstrap-e2e.md)。

## 14. 实施边界

- 实现 Agent 只修改 AgentBus submodule，不修改 SteadyFlow 父仓库 gitlink、`.gitmodules` 或 `AGENTS.md`。
- 实现阶段不提交、不 push；Codex 审核和真实验收后再安排 commit。
- 本轮以 V0.1 契约正确性为准，不承担旧数据库、旧 API 或旧 CLI 的兼容义务；实现可以直接采用更清晰的 schema 和命令行为。
- 不创建 GitHub 远端，不操作其他 submodule。
- 不把 Adapter、ACP、A2A、远程鉴权或分布式 session 扩入本轮。

## 15. 实施结果

- canonical manifest 与 ROLE 已落在 `agents/`，runtime 文档仅保留导航入口；
- `agent whoami/attach/bootstrap/launch` 与 `session ready/show` 已实现；
- Profile、Session generation 和 ready 状态由 SQLite 持久化；
- 所有 Task 读写在事务内执行 ready gate，重新 bootstrap 后 fail closed；
- TmuxConnector 只投递 Bootstrap/Task 短通知，Task 正文仍由 Agent 主动获取；
- 自动化检查、真实三 Agent 角色恢复、generation gate 与 daemon 重启持久化均通过。
