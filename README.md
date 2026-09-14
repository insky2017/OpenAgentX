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
session: OAX
agents:
  - agent_id: quote-service
    worker_config: /home/<user>/.openagentx/workers/quote-service.yaml
    enabled: true
```

先使用 `openagentx console login` 建立 installation-bound CLI Token 会话。Fleet 不读取 Owner 密码，也不直接修改数据库；可选 `identity_file` 只用于核对控制面中已经存在的 Agent。缺少 manifest 时，TTY 会从完整分页的正式 Agent 列表明确多选；非 TTY 必须重复提供 `--agent`。每个 Agent 必须已有 canonical Worker 配置，或用 `--worker-config agent-id=/absolute/source.yaml` 明确导入，Fleet 不生成样例 Runtime 配置：

```bash
openagentx console login
openagentx fleet init --agent quote-service \
  --worker-config quote-service=/absolute/source/quote-service.yaml
openagentx fleet workspace
openagentx fleet up
openagentx fleet status
```

默认 Worker 宿主是用户级 systemd。将 `deploy/systemd/openagentx-worker-user@.service` 安装为 `~/.config/systemd/user/openagentx-worker@.service`；它实际执行 `%h/.local/bin/openagentx worker run --config %h/.openagentx/workers/%i.yaml`。Worker unit 只 `Wants` daemon 并自行重连，daemon stop/restart 不会通过 systemd 依赖关系强停 Worker。`fleet up` 在任何 tmux 或 start 副作用前验证 canonical 二进制，并读取已加载 unit 的实际 `ExecStart`、`WorkingDirectory` 和 `EnvironmentFiles`，要求它们精确指向用户 home 与已验证的 canonical 配置；所有 systemctl 调用都使用 `--user`。系统级 `deploy/systemd/openagentx-worker@.service` 仍保留给明确的系统部署，不是 Fleet 默认值。

```bash
install -m 0644 deploy/systemd/openagentx-worker-user@.service \
  ~/.config/systemd/user/openagentx-worker@.service
systemctl --user daemon-reload
openagentx fleet up
```

协调器只创建缺失的具名 window 并保留额外或已移除的 window；受管目标缺少 pane `0`、名称/marker 冲突或未知程序占用目标名会在变更前失败。额外 pane `1+` 和无关 unmanaged window 会原样保留。compatible managed pane `0` 退出后会保留为 dead，只有显式执行 `openagentx fleet workspace --respawn-dead` 才能恢复；live pane、pane `1+`、unmanaged/orphaned window 绝不 respawn。协调器不删除、重排或覆盖现场，也不使用 `send-keys`、`paste-buffer` 或 `capture-pane`。tmux 不是 Worker 宿主或权威身份，关闭 Console、window、session 或 SSH 不影响 user-systemd Worker。

`openagentx console` 在 TTY 中启动全屏主菜单，可登录/替换登录、退出登录、选择 Normal 或 Diagnostic Attach。主菜单可在 tmux 外运行；Attach 按逻辑 `agent_id` 跟随当前 generation，展示安全投影后的状态和实时事件，并通过正式 Control API 执行 dispatch、steer、cancel 与 approval：

```bash
./bin/openagentx console attach --socket run/openagentx.sock --agent quote-service
./bin/openagentx console attach --socket run/openagentx.sock --agent quote-service --diagnostic
```

全屏 Attach 包含状态栏、可滚动且有界的 Timeline、固定输入区以及 `/status`、`/help` overlay；命令支持 `/dispatch`、`/steer`、`/cancel`、`/approve`、`/reject`、`/diagnostic`、`/foreground` 和 `/quit`。所有写操作只调用 authenticated Control API，断线时禁用且不排队；`/foreground` 只返回规划中提示。Attach 无论是否显式提供 `--agent`，都必须从 `OAX` session 的 pane `0` 执行严格 preflight。Agent 依次来自显式 `--agent`、当前 compatible managed window、经认证控制面 selector，并在任何绑定前由完整分页 Agent list 验证。非 TTY Attach 固定返回 code `2` 且无副作用；自动化读取状态应使用 Observe API，不存在 `--once` 或 JSON fallback。

普通停止是持久化 graceful drain-and-stop：先为全部在线目标提交停止意图，再持续显示 Agent、当前 RunAttempt、draining、elapsed、最近状态和“不会领取新任务”，直到全部 offline；终端中断只结束观察，不撤销意图。Worker 停止领取新工作，等待活动 RunAttempt 自然完成，idle 后释放 lease 并正常退出。强制停止是独立危险路径，必须显式确认且不显示为 graceful：

```bash
openagentx fleet down
openagentx fleet force-stop --confirm-force-stop --confirm-active-run-uncertain
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
