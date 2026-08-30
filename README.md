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
./bin/openagentx worker run --config agents/quote-service/worker.yaml
```

Worker 完成一个 Task 后释放当前 RunAttempt，继续等待 Mailbox 中的下一项工作。取消、审批和补充消息通过控制面持久化，不依赖 Worker 所在主机的终端布局。

## 指挥台

使用 Nginx/Tailscale 提供 HTTPS 入口，静态资源来自 `web/dist`。指挥台支持密码登录、RBAC、CSRF、SSE 状态观察、任务创建/回复/审批/取消和 PWA 安装。离线状态下所有写操作 fail closed，不排队、不自动重放。

## 数据与发布

目标 schema 版本由 daemon 严格校验；旧数据库不会在线迁移或兼容运行。正式发布只提供 `openagentx` 命令、OpenAgentX socket、数据库和环境变量命名。发布前执行：

```bash
./scripts/check-legacy-control-paths.sh --release
python3 scripts/check_docs.py
```
