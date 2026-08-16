# AGY 审核反馈 Round 1

请在当前同一 AGY 会话继续修正 AgentBus Go V0。只修改 `AgentBus/`，不暂存、不提交，不启动或操作真实 `%50/%51/%52/%53` pane。保留现有方案文档；代码、README 和测试按下面证据修正。

## 必须修正

### 1. 标识与 Tmux 通知边界

- `Agent.Validate` 目前只校验非空，而 Agent ID 会进入 tmux 通知。对 Agent ID/Role 增加严格长度和字符集校验，至少拒绝换行、控制字符、空白和 shell-like 内容；建议 ID 使用 `^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`。
- Task/Message/Event 使用完整 UUID，不再截取前 8 位。
- `ProbePane` 同时读取 `#{pane_id}` 和 `#{pane_dead}`，dead pane 必须返回 delivery failure。
- 补充 ID 注入、dead pane、异常 buffer/paste/send 路径测试。

### 2. Socket 单 daemon 安全

- `Server.Start` 不能看到 socket 就直接 unlink。先尝试连接：仍有 daemon 监听时拒绝启动；只有能明确判断为 stale socket 时才删除。permission/未知错误必须 fail closed。
- 清理 socket 时确认路径仍是本 Server 创建的 socket，不能删除启动后被替换的路径。
- 增加 live socket 拒绝、stale socket 恢复、普通文件拒绝测试。

### 3. 数据库级 active Task 约束

- 在 SQLite 增加 partial unique index，数据库级保证每个 target 最多一个 `status IN ('queued','running')` Task。
- insert 命中该约束时稳定映射为 `ErrWorkerBusy`。
- 使用两个 Store/连接或并发测试证明不能绕过；不要只依赖单进程 `COUNT(*)`。

### 4. Task 读取和观察边界

- `task get` 强制要求 `--agent`，Service 必须校验 caller 是 sender 或 target；其他已注册 Agent 也不能读取 Task/Messages。
- `task watch` 增加并强制 `--agent`，GetEvents 同样只允许 sender/target。
- `task list --agent` 至少验证 Agent 存在，并只返回该 Agent 参与的 Task；V0 不提供匿名列出全部任务的 CLI。
- 保留 Unix socket 同用户信任模型，但落实上述防误读边界。测试非法 reader、空 caller 和合法 sender/target。
- E2E 在 `task send` 后再次 `task get`，断言补充消息可读取。

### 5. 真实 CLI 发现与默认路径

- 当前默认 `run/agentbus.sock` 依赖 cwd，不适合三个现有 pane。把默认 socket 解析为与 cwd 无关的绝对路径；建议基于真实 `os.Executable()`（处理 symlink）定位 AgentBus 根目录的 `run/agentbus.sock`，也保留显式 `--socket` 和 `AGENTBUS_SOCKET` 优先级。
- daemon 默认 DB 同样应与构建后的 AgentBus 根目录一致，或提供等价的稳定绝对默认。
- Tmux 通知不要声称裸 `agentbus` 一定在 PATH。改为“Use the AgentBus CLI: task get ...”之类的协议提示；README 给出现有工作区可直接执行的完整路径/启动方式。

### 6. 事件和输入健壮性

- Connector delivery 后 `AddEvent` 失败不能静默忽略；至少 error log，并且不能打印“已持久化”的误导信息。
- HTTP request body 设置合理上限（例如 1 MiB）。非法 `after`/`timeout` 返回 `INVALID_INPUT`，不能静默变 0。
- 运行 `go mod tidy`，让 go.mod 的 direct/indirect 标记正确。

### 7. README 与最新 pane 映射

更新 README：

```text
%50  Codex Orchestrator
%51  AGY AgentBus Agent
%52  AGY Quote Service Agent
%53  AgentBus daemon/log pane
```

同时明确：

- `go-sqlite3` 需要 CGO/GCC；
- socket `0600` 只提供同 Unix user 隔离，actor ID 仍是本机信任域内自报身份；
- V0 不解析 pane 输出，不支持 running Task 强制取消；
- 从 SteadyFlow 根目录和 AgentBus 目录分别如何调用同一二进制/默认 socket。

## 验证

至少运行：

```bash
cd AgentBus
gofmt -w .
go mod tidy
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./...
go build -o bin/agentbus ./cmd/agentbus
```

用隔离临时 tmux session 验证 Connector，不使用真实 panes。最终简要报告修改和测试；Codex 会再次逐文件复审。
