# AgentBus (Go V0)

AgentBus 是一个**异构 Agent Runtime 的通信与协作控制面**。
首版（Go V0）提供最小单机控制面，支持 Coordinator（如 Codex CLI）与 Worker（如 AGY CLI / Quote Service Agent）通过 Unix Domain Socket 和 Tmux 注入实现受控协作。

## 架构与安全模型

- **Go 1.22 单二进制**：同一个 `agentbus` 二进制同时作为 daemon 服务端与 CLI 客户端；
- **编译与运行依赖**：基于 SQLite WAL 模式存储，使用 `github.com/mattn/go-sqlite3`（需 CGO/GCC 支持）；
- **Socket 权限与信任边界**：
  - 本地 Unix Domain Socket（默认权限 `0600`）提供同 Unix 用户隔离；
  - 属于单机同用户信任域，Agent ID 为自报身份；
  - 服务端强制执行任务防误读与操作权限边界：`task get`、`task watch`、`task list` 强制校验 `--agent`，仅任务关联的 `sender` 或 `target` 可读写任务及消息；
  - `Server` 启动时执行 live socket 探针探测，拒绝双 daemon 冲突，并支持安全回收 stale socket；
- **受控状态机与数据库级并发约束**：
  - 任务状态流转：`queued -> running -> succeeded / failed`，`queued -> canceled`；
  - SQLite 部分唯一索引（`uq_tasks_target_active`）在数据库层面硬性保证每个 target 最多一个处于 `queued`/`running` 状态的 Task；
  - V0 交互式 Agent 不支持对 `running` 状态的任务执行强制取消；
- **安全 TmuxConnector**：
  - 直接以 argv 调用 `tmux`，禁止任何 shell 字符串拼接；
  - 探针同时检测 `#{pane_id}` 与 `#{pane_dead}`，目标 pane 失效时准确记录 delivery failure；
  - 使用唯一命名 buffer 和 stdin `load-buffer`/`paste-buffer` 机制注入短通知，并在 paste 后立即清理 buffer；
  - 绝不注入任务正文、结果或错误信息，不捕获或解析 pane 输出；
- **实时 Event Stream**：单调递增 Event Sequence，支持 `task watch` 增量同步。

## 真实 Tmux Pane 映射

```text
%50  Codex Coordinator
%51  AGY AgentBus Agent
%52  AGY Quote Service Agent
%53  AgentBus daemon/log pane
```

## 目录结构

```text
AgentBus/
├── bin/                 # 构建产物 (git ignored)
├── cmd/
│   └── agentbus/        # main 入口
├── data/                # SQLite 数据文件 (git ignored)
├── docs/                # 架构与实施设计文档
├── internal/
│   ├── client/          # Unix socket HTTP client 与路径解析
│   ├── cli/             # CLI 子命令实现
│   ├── connector/       # TmuxConnector 通知与探针
│   ├── domain/          # 核心领域模型与严格校验
│   ├── server/          # Unix socket HTTP 服务器与安全探测
│   ├── service/         # 业务协调、授权边界与 Event Broker
│   └── store/           # SQLite WAL 存储与数据库级约束
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

## CLI 使用与启动方式

CLI 默认通过可执行文件自动推导 AgentBus 根目录下的绝对路径（如 `<AgentBus-Root>/run/agentbus.sock` 及 `<AgentBus-Root>/data/agentbus.db`），因此从 SteadyFlow 项目根目录或任何子目录均可直接调用。也可以通过环境变量 `AGENTBUS_SOCKET` 或 `--socket` 显式指定。

### 1. 启动 Daemon（在 %53 pane 中执行）

```bash
# 从 SteadyFlow 根目录：
./AgentBus/bin/agentbus serve

# 或在 AgentBus 目录下：
./bin/agentbus serve
```

### 2. 注册 Agent

```bash
# 注册 Coordinator
./bin/agentbus agent register --id coordinator --role coordinator --connector tmux --address %50

# 注册 AgentBus Agent
./bin/agentbus agent register --id agentbus-agent --role agentbus --connector tmux --address %51

# 注册 Quote Service Agent
./bin/agentbus agent register --id quote-service --role quote --connector tmux --address %52

# 查看已注册 Agent 列表
./bin/agentbus agent list
```

### 3. 提交与执行任务

```bash
# 1. 发起任务 (Coordinator)
./bin/agentbus task submit \
  --from coordinator \
  --to quote-service \
  --idempotency-key task-001 \
  --content "检查 Quote Service 行情服务健康状态"

# 2. 观察任务事件 (Coordinator)
./bin/agentbus task watch <task-id> --agent coordinator --after 0 --timeout 30s

# 3. 接收通知与确认 (Quote Service Agent)
# Tmux 注入提示：[AgentBus] New task <task-id>. Use AgentBus CLI: task get <task-id> --agent quote-service
./bin/agentbus task get <task-id> --agent quote-service
./bin/agentbus task ack <task-id> --agent quote-service

# 4. 上报执行进度 (Quote Service Agent)
./bin/agentbus task status <task-id> --agent quote-service --message "正在验证行情路由与日线缓存"

# 5. 发起方补充消息 (Coordinator)
./bin/agentbus task send <task-id> --from coordinator --content "补充检查 /portfolio 端点返回值"

# 6. 完成任务并提交结果 (Quote Service Agent)
./bin/agentbus task complete <task-id> --agent quote-service --result "Quote Service 接口与端点验证全部通过"
```
