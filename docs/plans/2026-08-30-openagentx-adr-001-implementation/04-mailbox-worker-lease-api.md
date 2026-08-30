---
doc_type: implementation_task
status: completed
owner: openagentx
updated_at: 2026-08-30
---

# 任务 04：Mailbox 与 Worker Lease API

## 目标

实现 Worker 注册、heartbeat、Agent Mailbox long poll、claim/accept、RunAttempt 事件与结算的稳定 Worker Control API，并以 generation、lease 和 fencing 保护所有执行写入。

## 依赖与入口

- 依赖：任务 03；
- 入口：任务 02 的 Worker API DTO 和 repository；
- 本阶段先实现本机 HTTP over UDS binding，领域协议不得绑定 UDS。

## 实施范围

- 实现 Worker register、短期 Session Token、generation 和 Active Worker lease；
- 实现 heartbeat 续租、过期判定和 fencing token 推进；
- 实现 Mailbox work/control lane 的事务 claim、lease、accept、重试与 supersede；
- 实现 `lane_priority ASC, sequence ASC` 的确定性领取；
- 实现 BeginAttempt、AppendRunEvents 和 FinishRun 的 CAS/幂等写入；
- 实现 topic Broker best-effort wakeup 与 long poll 超时后数据库重查；
- 抽取可供 UDS/HTTPS 共用的 handler 和 `WorkerControlClient`。

## 目标代码边界

```text
OpenAgentX/internal/controlplane/worker_service.go
OpenAgentX/internal/controlplane/mailbox_service.go
OpenAgentX/internal/api/worker/
OpenAgentX/internal/transport/unixhttp/
OpenAgentX/internal/client/worker/
```

## 质量与验证

- 过期 token、旧 generation、过期 lease、旧 fencing 和错误 Agent 绑定均拒绝；
- 两个 Worker 并发 claim 同一 item 时只有一个取得 lease；
- active turn 时仍可领取 control lane，但不提前占有新 Task；
- Broker wakeup 关闭后仍能通过 DB 重查领取；
- UDS socket 权限为 `0600`，只删除确认是 socket 且属于当前实例的旧路径。

## 退出条件

- Worker API conformance 在 UDS binding 通过；
- Mailbox at-least-once 与幂等 accept 行为稳定；
- Active Run 唯一约束由 daemon 跨 Worker 强制；
- API handler 不包含具体 Runtime Adapter 分支。

## 完成记录

- 已实现协议无关 `WorkerService`、双 lane Mailbox long poll、Worker/Run lease 续期、generation/fencing 校验和安全幂等终态写入；
- 已实现严格 Worker HTTP handler、`UnixHTTPWorkerClient` 和 socket `0600`/所有权保护；
- token、principal、Agent、generation、lease、fencing、未知字段和缺少 Bearer Token 的负向测试均通过；
- Mailbox 顺序、backpressure、at-least-once、丢失 wakeup 数据库重查和替代 Worker 并发竞争已通过重复与 race 验证；
- `go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...` 和 UDS 真实端到端链路通过；
- 验收报告：[Task 04 Worker Control API 验证报告](../../reports/validation/2026-08-30-openagentx-task04-worker-control-api.md)；
- 冻结 ADR-001 未修改，handler 不依赖具体 Runtime Adapter，UDS 不承载业务语义。
