---
doc_type: validation_report
status: passed
owner: openagentx
test_id: T07
updated_at: 2026-09-01
---

# OpenAgentX ADR-001 T07 验证报告

## 结论

T07 通过。`ReconcileExpired` 现在对 Mailbox、Worker、RunAttempt 和 Task 的实际恢复状态转换逐条 CAS，并在同一事务追加结构化 Event Journal；恢复幂等和故障回滚均通过。lease/generation/fencing 拒绝矩阵、丢唤醒重查、replacement Worker 竞态、SSE 断点 replay 和真实 Worker 进程 UDS 场景均通过。

## 源码与冻结边界

| 项目 | 证据 |
|---|---|
| 源码提交 | T07 独立提交（本提交） |
| ADR-001 SHA-256 | `587e9b8f27ddeb68aae8e5a507fefc10973739c8dd2c3def86e2c0bbc51db403` |
| 测试数据 | 临时 SQLite、固定时钟和进程内 Broker；未使用生产库或真实凭据 |

## 已执行矩阵

| 断言 | 证据等级 | 结果 |
|---|---:|---|
| 过期 Mailbox claim reclaim、过期 RunAttempt -> `uncertain`、关联 Task settle | L1 | 通过；`TestReconcileExpiredRequeuesClaimsAndMarksRunUncertain`，并验证 `mailbox.reclaimed`、`worker.offline`、`run_attempt.uncertain`、`task.uncertain/canceled` sequence |
| daemon 重启后 SQLite 当前状态可读取 | L1 | 通过；`TestOpenConfiguresTargetSQLiteAndReopensCurrentState` |
| 事务故障回滚与迁移回滚 | L1 | 通过；既有 repository、control input、multi-turn 和 migration rollback 测试 |
| Worker token、principal、Agent、generation、lease、fencing 拒绝 | L1 | 通过；Worker mailbox/control claim 负向矩阵 |
| 丢失 Broker wakeup 后数据库重查 | L1 | 通过；long-poll lost-wakeup 与 subscribe-race 测试 |
| 过期 Worker 与 replacement Worker 领取竞态 | L1 | 通过；`TestExpiredWorkerCannotRaceReplacementWorkerForMailbox` |
| 恢复事务状态与 Journal 同成同败 | L1 | 通过；`TestReconcileExpiredRollsBackStateAndJournalTogether` |
| 真实 Worker 进程经 UDS 连续/多轮恢复链路 | L2 | 通过；`TestRunWorkerProcessCompletesConsecutiveTasksWithoutTerminalInput`、`TestRunWorkerProcessCompletesMultiTurnTaskWithSessionResume`，各重复 10 次 |
| SSE 断点 replay | L2 | 通过；`TestSSEReplaysAfterHighestClientSequence`，重复 20 次，按最高 `Last-Event-ID` 只回放后续 sequence |
| 关键恢复/授权竞态重复稳定性 | L1 | 通过；`go test -race -count=20`，SQLite 约 5 秒、controlplane 约 62 秒，无失败 |
| 全仓库竞态回归 | L1 | 通过；`go test -race -count=1 ./...` |

## 验证命令

```text
go test -race -count=1 ./...
go test -race -count=20 ./internal/persistence/sqlite ./internal/controlplane -run 'Test(ReconcileExpiredRequeuesClaimsAndMarksRunUncertain|OpenConfiguresTargetSQLiteAndReopensCurrentState|CreateTaskRollbackAndIdempotency|TargetSchemaTransactionRollsBackOnMigrationFailure|WorkerAuthenticationLeaseGenerationFencingAndAgentBindingFailClosed|WorkerControlClaimAuthorityFailsClosed|WorkerControlClaimFailsClosedAfterLeaseRevoke|LongPollRechecksDatabaseWhenBrokerWakeupIsLost|ExpiredWorkerCannotRaceReplacementWorkerForMailbox|WorkerControlLongPollRechecksAfterSubscribeRace|WorkerControlLongPollRechecksDatabaseWhenWakeupIsLost)$'
go test -race -count=10 ./internal/cli/worker -run 'TestRunWorkerProcessCompletes(ConsecutiveTasksWithoutTerminalInput|MultiTurnTaskWithSessionResume)$'
go test -race -count=10 ./internal/controlplane -run 'TestUnixHTTPWorker(APIEndToEnd|ControlLongPollWaitsAndWakesThroughSharedBroker)$'
go test -race -count=20 ./internal/api/panel -run 'TestSSEReplaysAfterHighestClientSequence'
go vet ./...
go test -count=1 ./...
git diff --check
```

以上命令均通过；聚焦 T07 矩阵的 `-race -count=20` 及 UDS/SSE 的重复测试也全部通过。输出和报告未记录密码、Cookie、Session Token、代理凭据或完整 fencing token。

## 审计结论

前一轮发现的恢复状态缺少 Journal 问题已修复并由上述测试覆盖；当前未发现阻塞缺陷。恢复事件使用 daemon system principal 作为 actor，sequence 单调递增，重复执行不会追加重复恢复事件。

## 未覆盖与边界

- 尚未独立启动真实 `openagentx serve` daemon 二进制并执行 kill/restart；当前 L2 证据由 repository reopen、真实 Worker 进程逻辑、Unix Socket HTTP 和恢复持久状态测试组成。该部署级场景留在 T10 回归，不影响本关状态机结论。
- 未发现重复 Active Run 或旧 Worker 写入成功；相关矩阵已通过。

## 独立提交要求

T07 修复与验证通过后，必须将生产代码、回归测试、报告和测试计划作为一个独立提交；提交前再次确认 ADR-001 未修改，且运行中的二进制来自该提交。
