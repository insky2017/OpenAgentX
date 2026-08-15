# AgentBus (Go V0.1)

AgentBus 是一个**异构 Agent Runtime 的通信与协作控制面**。
提供单机控制面，支持 Coordinator（如 Codex CLI）与 Worker（如 AGY CLI / Quote Service Agent）通过 Unix Domain Socket 和 Tmux 注入实现受控协作。

## 架构与安全模型

- **Go 1.22 单二进制**：同一个 `agentbus` 二进制同时作为 daemon 服务端与 CLI 客户端；
- **编译与运行依赖**：基于 SQLite WAL 模式存储，使用 `github.com/mattn/go-sqlite3`（需 CGO/GCC 支持）；
- **Role Manifest 与 Agent Profile**：
  - 各 Agent 具备权威配置 `agents/<id>/agent.yaml` 与规范定义 `ROLE.md`；
  - 严格 YAML 解析，禁止未知字段，自动校验工作区与 instructions 规范文件的存在性与可读性；
- **Session Generation 与生命周期**：
  - Agent 每次 Attach 或重新 Bootstrap 分配单调递增的 `generation`；
  - 状态流转：`bootstrapping -> ready / delivery_failed`；
  - 投递结果明确区分 `notified`（实际注入）、`delivery_failed`（注入失败）与 `skipped`（免通知或非 tmux）；
- **Task Ready Gate (Fail-Closed)**：
  - 在服务层与数据库事务层强制双重拦截：任务发起方与目标方必须均处于 `ready` 会话状态；
  - 处于 `bootstrapping` 或 `delivery_failed` 状态的 Agent 无法创建任务或执行 `ack/status/send/complete/fail/cancel` 写操作，直接返回 409 `AGENT_NOT_READY`；
  - `session ready` 采用 CAS 校验请求 generation 与当前活动 generation 一致性；
- **受控状态机与数据库级并发约束**：
  - 任务状态流转：`queued -> running -> succeeded / failed`，`queued -> canceled`；
  - SQLite 部分唯一索引（`uq_tasks_target_active`）在数据库层面硬性保证每个 target 最多一个处于 `queued`/`running` 状态的 Task；
- **安全 TmuxConnector**：
  - 直接以 argv 调用 `tmux`，禁止任何 shell 字符串拼接；
  - 探针同时检测 `#{pane_id}` 与 `#{pane_dead}`，目标 pane 失效时准确记录 delivery failure；
  - 使用唯一命名 buffer 和 stdin `load-buffer`/`paste-buffer` 机制注入短通知，并在 paste 后立即清理 buffer；
  - 注入短通知包含角色身份提示，引导 Agent 在上下文不确定时读取 `ROLE.md`；
- **实时 Event Stream**：单调递增 Event Sequence，支持 `task watch` 增量同步。

## 真实 Tmux Pane 映射（当前默认部署）

```text
%50  Codex Coordinator        (agents/coordinator/)
%51  AGY AgentBus Agent       (agents/agentbus-agent/)
%52  AGY Quote Service Agent  (agents/quote-service/)
%53  AgentBus daemon/log pane
```
*注：通过 `--address` 覆盖实际 pane 时角色身份与职责不变。*

## 目录结构

```text
AgentBus/
├── agents/              # Canonical Agent 定义与规范
│   ├── coordinator/     # Coordinator agent.yaml + ROLE.md
│   ├── agentbus-agent/  # AgentBus Agent agent.yaml + ROLE.md
│   └── quote-service/   # Quote Service Agent agent.yaml + ROLE.md
├── bin/                 # 构建产物 (git ignored)
├── cmd/
│   └── agentbus/        # main 入口
├── data/                # SQLite 数据文件 (git ignored)
├── docs/                # 架构、设计与实施文档
├── internal/
│   ├── client/          # Unix socket HTTP client 与路径解析
│   ├── cli/             # CLI 子命令实现
│   ├── connector/       # TmuxConnector 通知与探针
│   ├── domain/          # 核心领域模型、Manifest 解析与校验
│   ├── server/          # Unix socket HTTP 服务器与安全探测
│   ├── service/         # 业务协调、Ready Gate 与 Event Broker
│   └── store/           # SQLite WAL 存储与原子 Ready Gate
├── run/                 # Unix socket 文件 (git ignored)
├── go.mod
├── go.sum
└── README.md
```

## 快速构建与验证

```bash
cd AgentBus

# 格式化与依赖整理
gofmt -w .
go mod tidy

# 静态检查
go vet ./...

# 单元测试与竞态检查
go test -count=1 ./...
go test -race -count=1 ./...

# 构建二进制
go build -o bin/agentbus ./cmd/agentbus
```

## CLI 使用与完整协作流程

CLI 默认自动推导 AgentBus 根目录下的绝对路径（`<AgentBus-Root>/run/agentbus.sock` 及 `<AgentBus-Root>/data/agentbus.db`），从 SteadyFlow 项目根目录或任何子目录均可直接调用。也可以通过环境变量 `AGENTBUS_SOCKET` 或 `--socket` 显式指定。

### 1. 启动 Daemon（在 %53 pane 中执行）

```bash
./AgentBus/bin/agentbus serve
```

### 2. Agent 角色识别与 Attach

```bash
# 1. 验证 Manifest 配置与环境变量 (任何 Agent 可本地执行)
./AgentBus/bin/agentbus agent whoami --config AgentBus/agents/coordinator/agent.yaml

# 2. Attach Coordinator (北向交互方使用 --no-notify 免注入)
./AgentBus/bin/agentbus agent attach --config AgentBus/agents/coordinator/agent.yaml --no-notify

# 3. Attach 南向 Worker (自动向目标 pane 注入 Bootstrap 短通知)
./AgentBus/bin/agentbus agent attach --config AgentBus/agents/quote-service/agent.yaml --address %52
./AgentBus/bin/agentbus agent attach --config AgentBus/agents/agentbus-agent/agent.yaml --address %51
```

### 3. Session Ready 握手 (解封 Task Ready Gate)

```bash
# Coordinator 确认就绪 (手工执行)
./AgentBus/bin/agentbus session ready --agent coordinator --generation 1

# Quote Service Agent 收到注入通知后确认就绪
# 提示: [AgentBus Bootstrap] agent_id=quote-service role=quote generation=1. Read .../ROLE.md, then use AgentBus CLI: session ready --agent quote-service --generation 1
./AgentBus/bin/agentbus session ready --agent quote-service --generation 1

# 查询 Session 状态
./AgentBus/bin/agentbus session show --agent quote-service
```

### 4. 任务分发与协作

双方均处于 `ready` 状态后，任务即可正常提交与执行：

```bash
# 1. 发起任务 (Coordinator)
./AgentBus/bin/agentbus task submit \
  --from coordinator \
  --to quote-service \
  --idempotency-key task-001 \
  --content "检查 Quote Service 行情服务健康状态"

# 2. 观察任务事件 (Coordinator)
./AgentBus/bin/agentbus task watch <task-id> --agent coordinator --after 0 --timeout 30s

# 3. 接收通知与接单 (Quote Service Agent)
./AgentBus/bin/agentbus task get <task-id> --agent quote-service
./AgentBus/bin/agentbus task ack <task-id> --agent quote-service

# 4. 上报执行进度 (Quote Service Agent)
./AgentBus/bin/agentbus task status <task-id> --agent quote-service --message "正在验证行情路由与日线缓存"

# 5. 发起方补充消息 (Coordinator)
./AgentBus/bin/agentbus task send <task-id> --from coordinator --content "补充检查 /portfolio 端点返回值"

# 6. 完成任务并提交结果 (Quote Service Agent)
./AgentBus/bin/agentbus task complete <task-id> --agent quote-service --result "Quote Service 接口与端点验证全部通过"
```

### 5. 辅助命令

```bash
# 重新触发 Bootstrap (使 Session 回到 bootstrapping 并向目标 pane 重新注入通知)
./AgentBus/bin/agentbus agent bootstrap --id quote-service

# 受管启动新 Agent 进程 (需在 tmux pane 环境中执行，自动注入环境变量并在指定延迟后发起 attach)
./AgentBus/bin/agentbus agent launch --config AgentBus/agents/quote-service/agent.yaml --bootstrap-delay 2s -- <command> [args...]
```
