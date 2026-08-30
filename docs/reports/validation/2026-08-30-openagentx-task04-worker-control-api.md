---
doc_type: validation_report
status: passed
validated_at: 2026-08-30
---

# OpenAgentX Task 04 Worker Control API 验证报告

## 1. 结论

Worker 注册、短期 Session Token、generation、lease、fencing、双 lane Mailbox claim/accept、RunAttempt 启动/事件/结算和 HTTP over UDS 已形成一条稳定的 Worker Control API。所有执行写入同时校验 Worker 身份、逻辑 Agent 绑定和当前所有权；任务 04 验收通过。

## 2. 实现边界

- `internal/controlplane.WorkerService` 负责协议无关的注册、heartbeat、long poll、claim、BeginAttempt、Event 与 Finish 语义；
- `internal/persistence/sqlite` 在单一事务内校验 Worker guard、转移 Mailbox/Task/RunAttempt 状态并追加 Event Journal；
- `internal/api/workerapi` 提供严格 JSON、Bearer Token、稳定错误码和无 Adapter 分支的 HTTP handler；
- `internal/client/worker.UnixHTTPWorkerClient` 实现统一 `WorkerControlClient`；
- `internal/transport/unixhttp` 只负责 UDS 生命周期、HTTP 承载和 socket 所有权，不承载业务规则；
- topic Broker 只提供 best-effort wakeup，Mailbox 数据库始终是持久事实源。

## 3. 不变量与证据

| 不变量 | 验证证据 |
|---|---|
| 一个逻辑 Agent 同时只有一个有效 Active Worker | 注册事务和 `TestExpiredWorkerCannotRaceReplacementWorkerForMailbox` |
| token、principal、Agent、generation、lease、fencing 任一错误都 fail closed | `TestWorkerAuthenticationLeaseGenerationFencingAndAgentBindingFailClosed` |
| heartbeat 滑动续期 token/Worker lease，并延长活动 Run lease | `TestHeartbeatSlidesTokenAndRenewsActiveRunLease` |
| control lane 优先于 work lane，同 lane 按 sequence 升序 | `TestMailboxControlOrderingBackpressureAndAtLeastOnce` |
| Active Run 存在时仍消费 control，但新 Task 保持 pending | `TestMailboxControlOrderingBackpressureAndAtLeastOnce` |
| Mailbox claim 是 lease 保护的 at-least-once，accept 可安全幂等 | 同一测试的 lease 到期 reclaim 与重复 accept |
| 旧 Worker 与替代 Worker 竞争时，只有当前代次取得 item | `TestExpiredWorkerCannotRaceReplacementWorkerForMailbox`，普通与 race 重复验证 |
| 丢失 Broker wakeup 不丢工作 | `TestLongPollRechecksDatabaseWhenBrokerWakeupIsLost` 的超时数据库重查 |
| BeginAttempt 原子完成 Task running、RunAttempt 创建、Mailbox accept 和 Journal | `TestWorkerServiceRegisterClaimBeginEventsAndFinish` |
| 相同 Finish 重试幂等，不同终态结果冲突 | `TestWorkerServiceRegisterClaimBeginEventsAndFinish` |
| UDS handler/client 完成注册到结算的真实链路 | `TestUnixHTTPWorkerAPIEndToEnd` |
| UDS 固定 `0600`，不删除普通文件、活动 socket 或替换路径 | `TestServerRefusesToRemoveNonSocketPath`、`TestSecondServerCannotTakeActiveSocket`、`TestServerCleanupDoesNotRemoveReplacementPath` |
| HTTP 输入拒绝未知字段和缺失 Bearer Token | `TestWorkerAPIRejectsUnknownFieldsAndMissingBearerToken` |

## 4. 自动化验证

```text
go test -count=1 ./...                                      PASS
go test -race -count=1 ./...                                PASS（0 race）
go vet ./...                                                 PASS
go test -count=20 ./internal/controlplane \
  -run 'TestMailboxControlOrderingBackpressureAndAtLeastOnce|TestExpiredWorkerCannotRaceReplacementWorkerForMailbox|TestLongPollRechecksDatabaseWhenBrokerWakeupIsLost|TestHeartbeatSlidesTokenAndRenewsActiveRunLease|TestUnixHTTPWorkerAPIEndToEnd' PASS
go test -race -count=5 ./internal/controlplane \
  -run 'TestMailboxControlOrderingBackpressureAndAtLeastOnce|TestExpiredWorkerCannotRaceReplacementWorkerForMailbox|TestLongPollRechecksDatabaseWhenBrokerWakeupIsLost|TestUnixHTTPWorkerAPIEndToEnd' PASS（0 race）
git diff --check                                             PASS
```

冻结 ADR-001 无 diff。Worker API handler 未引用任何具体 Agent Runtime Adapter，UDS 路径未引入 tmux 控制语义。

## 5. 退出判定

- UDS binding 已通过 Worker API 正向链路和安全负向验证；
- Mailbox 顺序、backpressure、at-least-once 与幂等语义稳定；
- Worker 和 Active Run 的续租、代次替换及旧写入隔离已落到服务端事务；
- Task 05 可以只依赖 `WorkerControlClient` 构建并发 Resident Worker，不需要接触 HTTP、SQLite 或 UDS 实现细节。
