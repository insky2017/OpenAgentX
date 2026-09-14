# OpenAgentX

OpenAgentX 是面向异构 Agent Runtime 的组织协作控制面。daemon 持久化组织、任务、Mailbox、RunAttempt、Worker lease 和 Event Journal；Worker 作为独立进程通过 Unix Socket 或 mTLS HTTPS 长轮询工作；Runtime Adapter 按一次 turn 启动具体 Agent CLI/ACP 会话。

## 架构

```text
OpenAgentX daemon
  ├─ Transactional SQLite + Event Journal
  ├─ Mailbox / Worker lease / fencing
  ├─ Observe / Control / Admin API + SSE
  └─ Command Center PWA
       │ Unix Socket 或 mTLS HTTPS
       ▼
Resident Worker
  └─ Runtime Adapter (AGY Batch / ACP / fake)
       └─ Agent CLI 或 Runtime 进程
```

业务寻址只使用逻辑 `agent_id`。终端、pane、键盘注入和生命周期 Hook 不属于 OpenAgentX 控制面。

## 构建

```bash
go test ./...
go test -race ./...
go vet ./...
go build -o bin/openagentx ./cmd/openagentx
cd web && npm install --no-audit --no-fund && npm run build
```

将二进制安装到 `~/.local/bin/openagentx`、运行配置和状态放到
`~/.openagentx`，并通过用户级 systemd 启动的步骤见
[用户级安装与启动指南](docs/operations/openagentx-user-install-guide.md)。

## 首次初始化

daemon 启动前，必须通过本机交互式 CLI 创建首个 owner 和默认 Organization。密码使用隐藏输入，不支持默认密码、命令行密码参数或 Web 自助注册：

```bash
./bin/openagentx init --db data/openagentx.db
```

随后由 owner 应用逻辑 Agent 身份。`identity.yaml` 只描述稳定的 Agent Principal、AgentIdentity 和 AgentProfile；Worker 进程配置仍单独保存在 `agent.yaml`：

```bash
./bin/openagentx agent apply \
  --db data/openagentx.db \
  --file agents/quote-service/identity.yaml
```

相同定义重复 apply 是无事件的幂等操作；定义与现有身份不一致时明确失败。

## Worker

为每个 Domain Agent 准备严格校验的 Worker 配置：

```yaml
version: 1
agent_id: quote-service
transport: unix
unix_socket: run/openagentx.sock
capabilities:
  - quote-service-development
runtime_backends:
  - backend_id: primary
    adapter_id: agy-batch
    options:
      binary: agy
```

启动常驻 Worker：

```bash
./bin/openagentx worker run --config agents/quote-service/agent.yaml
```

Worker 完成一个 Task 后释放当前 RunAttempt，继续等待 Mailbox 中的下一项工作。取消、审批和补充消息通过控制面持久化，不依赖 Worker 所在主机的终端布局。

## Fleet 与终端 Console

Fleet 清单显式列出受管 Agent，不扫描目录推断启动对象：

```yaml
version: 1
session: agentx
agents:
  - agent_id: quote-service
    identity_file: agents/quote-service/identity.yaml
    worker_config: agents/quote-service/agent.yaml
    enabled: true
```

`fleet init` 一次认证后校验并应用 identity，协调固定的 `agentx` tmux workspace；`fleet up` 通过 systemd 模板启动已启用 Worker。新建 Agent window 只有 pane `0`，默认运行经 UDS 正式 API 连接的 Console 客户端：

```bash
./bin/openagentx fleet init --file fleet.yaml --db data/openagentx.db --socket run/openagentx.sock
./bin/openagentx fleet up --file fleet.yaml --socket run/openagentx.sock
./bin/openagentx fleet status --file fleet.yaml
```

systemd 模板实际读取 `/etc/openagentx/workers/%i.yaml`。`fleet up` 会在任何 workspace 或 `systemctl start` 副作用前，验证每个启用 Agent 的 `worker_config` 与该 canonical 文件是同一文件或内容 SHA-256 完全一致；缺失或不一致时 fail closed。部署配置后再启动，例如：

```bash
sudo install -D -o root -g "$(id -gn quote-service)" -m 0640 agents/quote-service/agent.yaml /etc/openagentx/workers/quote-service.yaml
./bin/openagentx fleet up --file fleet.yaml --socket run/openagentx.sock
```

协调器只创建缺失的具名 window 并保留额外或已移除的 window；重名、非 pane `0`、多 pane 或未知程序占用会在变更前失败。它不删除、重排或覆盖现场，也不使用 `send-keys`、`paste-buffer` 或 `capture-pane`。tmux 不是 Worker 宿主或权威身份，关闭 Console、window、session 或 SSH 不影响 systemd Worker。

Console attach 按逻辑 `agent_id` 跟随当前 generation，展示安全投影后的状态和实时事件，并通过正式 Control/Admin API 执行 dispatch、steer、cancel、approval 与停止操作：

```bash
./bin/openagentx console attach --socket run/openagentx.sock --agent quote-service
./bin/openagentx console attach --socket run/openagentx.sock --agent quote-service --diagnostic
```

交互 attach 同时订阅事件并读取 TTY 命令栏，支持 `/dispatch`、`/steer`、`/cancel`、`/approve`、`/reject`、`/down`、`/foreground` 和 `/quit`；所有写操作仍调用 authenticated Control/Admin API。`/foreground` 只返回规划中提示。省略 `--agent` 时仅从当前 `agentx` session 的受管具名 window、pane `0` 推导；`overview`、unmanaged、重名或错误 pane 均要求显式 `--agent`。非交互调用必须使用 `--once`，该模式只输出一次 attach 快照且不读取 stdin。

普通停止是持久化 graceful drain-and-stop：先为全部在线目标提交停止意图，再持续显示 Agent、当前 RunAttempt、draining、elapsed、最近状态和“不会领取新任务”，直到全部 offline；终端中断只结束观察，不撤销意图。Worker 停止领取新工作，等待活动 RunAttempt 自然完成，idle 后释放 lease 并正常退出。强制停止是独立危险路径，必须显式确认且不显示为 graceful：

```bash
./bin/openagentx fleet down --file fleet.yaml --socket run/openagentx.sock
./bin/openagentx fleet force-stop --file fleet.yaml --socket run/openagentx.sock --confirm-force-stop
```

`Foreground Takeover（规划中，暂不可用）` 仅作为禁用提示；当前实现不会把 Worker 切换到 Runtime TTY。

## 指挥台

使用 Nginx/Tailscale 提供 HTTPS 入口，静态资源来自 `web/dist`。指挥台支持密码登录、RBAC、CSRF、SSE 状态观察、任务创建/回复/审批/取消和 PWA 安装。离线状态下所有写操作 fail closed，不排队、不自动重放。

## 数据与发布

目标 schema 版本由 daemon 严格校验；旧数据库不会在线迁移或兼容运行。正式发布只提供 `openagentx` 命令、OpenAgentX socket、数据库和环境变量命名。发布前执行：

```bash
./scripts/check-legacy-control-paths.sh --release
python3 scripts/check_docs.py
```

ADR-001 的分阶段实机验收入口见 [ADR-001 测试计划](docs/plans/2026-08-30-openagentx-adr-001-test-plan.md)。测试按 T01-T10 顺序执行，失败关卡修复并重测后才能继续。
