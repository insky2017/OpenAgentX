---
doc_type: implementation_task
status: completed
owner: openagentx
updated_at: 2026-09-19
---

# 任务 02：权威任务观察投影

## 目标

建立窄范围、Agent-scoped、CLI Token 授权的 Console Task snapshot/list/event projection，使 Task、Mailbox、
RunAttempt、Message/Approval 和 Journal cursor 在一致事务与严格归属检查下可被 Console 消费。

## 范围边界

- 不开放 Task 04 已拒绝的广泛 Panel Observe/Network route；
- persistence 不依赖 HTTP/API DTO，handler 不拼接多个非事务查询；
- 不修改 Task intent、终态判定、Runtime adapter 或 TUI；
- Web 既有 cookie/CSRF 和 CLI UDS mux/scope 边界保持不变。

## 验收矩阵

| 类别 | 验收内容 |
|---|---|
| 正向 | 分页 active/recent Task；按 Task ID 取得一致 snapshot；Task/Run/Message/Approval 安全投影；Journal task/run frame |
| 状态/CAS/幂等 | snapshot 唯一 high-water；空 Journal=0；Task version 来自事务读取；重复 list/snapshot 无副作用 |
| 失败 | 任一查询、归属、identity/status 校验、safe projection、JSON encode/write 失败时 frame 不发送、cursor 不推进 |
| 竞态 | snapshot N 期间提交 N+1；dispatch response/event 乱序；Task version 与 Run terminal 同时提交；retention gap |
| 权限 | viewer `console.read` 仅安全 list/snapshot/events；Diagnostic 字段剥离；跨 Agent、跨 installation、错误 scope fail closed |
| 证据格式 | repository transaction test、handler/mux 集成、故障注入恢复请求、API schema snapshot，均记录 sequence/version |

## 实施步骤

1. 在 domain/service 定义 transport-neutral Console Task contract。
2. 在单一 SQLite 只读事务中读取 Task、Mailbox/claim 摘要、关联 Runs、必要 Message/Approval、安全
   TurnResult 和 Journal high-water；建立确定性排序和容量上限。
3. 新增专用分页 Task option/detail API，挂现有 Console handler；Web 仍只用 cookie authorizer，CLI route
   只通过 UDS mux 使用 bearer。复用既有 role/scope 和 installation audience，不增加 bearer fallback。
4. 扩展 SSE 安全 projection，使 Task/Run/Message/Approval 的必要公开状态可被 reducer 消费；Normal
   始终移除 Diagnostic。
5. 所有 repository/projection/encode/write 错误在当前 event 之前结束 stream；下一请求从 last-applied
   恢复。合法其他 Agent 事件才可明确跳过。

## 验证

```bash
go test ./internal/api/console ./internal/api/panel ./internal/client/console ./internal/persistence/sqlite/... -count=1
go test -race ./internal/api/console ./internal/api/panel ./internal/client/console ./internal/persistence/sqlite/... -count=1
go vet ./internal/api/... ./internal/client/console ./internal/persistence/sqlite/...
go build -o /tmp/openagentx-adr009-task02 ./cmd/openagentx
bash scripts/check-legacy-control-paths.sh --release
git diff --check
```

## 退出条件

- snapshot/cursor/归属/分页/权限可由事务和故障恢复测试证明；
- API 不泄露 Task content 之外的 principal、secret、raw payload 或隐藏字段；
- Web 既有 route/auth 回归通过；
- 创建一个 Task 02 实现提交后停止等待监督 gate。

## 完成记录

- 实现提交：`84a2c15473455cf2ae5e052e9790fd851db7abda`。
- 产物：transport-neutral Task aggregate、SQLite 单事务 snapshot/stable keyset list、Console Task list/detail、
  Attach `suggested_task`、client 分页/detail，以及 Task/Mailbox/Message/Approval SSE 安全投影。
- snapshot N/N+1、坏 cursor/跨 Agent、真实 CLI bearer role+scope、projection/encode/write 故障恢复和安全
  字段测试通过；全仓普通测试、Task 02 race、vet、build、release scanner 与 diff check 均通过。
- Task 01 冻结契约要求同一 Console handler 分别使用 Web cookie 与 UDS CLI authorizer；本 gate 修正步骤 3
  的歧义。CLI 未获得广泛 Panel Observe/Network route，Web cookie/CSRF 行为未改变。
- 未修改 schema、Runtime/Task 终态、reducer/TUI 或真实运行状态；Task 03 及以后未提前实现。
