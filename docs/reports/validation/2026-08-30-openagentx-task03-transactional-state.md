---
doc_type: validation_report
status: passed
validated_at: 2026-08-30
---

# OpenAgentX Task 03 Transactional State 验证报告

## 1. 结论

目标 SQLite Repository 已成为 M1 当前权威状态入口。Agent/Profile、Task、Message、WorkerInstance、MailboxItem、RunAttempt、SessionBinding 与 Event Journal 可直接从状态表读写，不依赖 Event replay；所有复合写入均在单一事务内提交，任务 03 验收通过。

## 2. 实现边界

- `internal/persistence/sqlite` 负责目标数据库打开、migration 校验、WAL、foreign keys、busy timeout、immediate transaction、CAS、幂等和 Repository；
- `internal/controlplane.TransactionalState` 固定 application service 到权威状态层的依赖边界；
- `internal/domain` 增加目标 Principal、Organization、AgentProfileRecord 和事务结果契约；
- Event Journal 只提供 append 与 sequence 分页，schema trigger 拒绝 UPDATE/DELETE；
- 旧 `internal/store` 未被目标 persistence/controlplane 引用，旧 schema 不参与目标 Repository 初始化。

## 3. 不变量与证据

| 不变量 | 验证证据 |
|---|---|
| 空库 migration 原子执行，失败不留下半个 schema | `TestTargetSchemaTransactionRollsBackOnMigrationFailure` |
| 当前 schema 缺表或缺不可变 trigger 时 fail closed | `TestApplyRejectsPartialCurrentSchema`、`ValidateCurrent` |
| WAL、foreign keys、busy timeout 和 schema version 生效 | `TestOpenConfiguresTargetSQLiteAndReopensCurrentState` |
| 重开数据库后直接读取状态表，不 replay Event | `TestOpenConfiguresTargetSQLiteAndReopensCurrentState` |
| Task、initial Message、MailboxItem、Journal 同成同败 | `TestCreateTaskCommitsStateMessageMailboxAndJournalAtomically`、故障注入 rollback |
| 相同幂等键并发提交只创建一次 | `TestConcurrentTaskIdempotencyReturnsOneCreationAndOneReplay`，重复 20 轮与 race 通过 |
| Task version/CAS 拒绝 stale writer | `TestTaskCASMessageAndForeignKeyEnforcement` |
| 外键拒绝未知逻辑 Agent，数据库无悬空引用 | `TestTaskCASMessageAndForeignKeyEnforcement` |
| 两个 Task 可同时 queued，同 Agent 只能一个 Active Run | `TestConcurrentBeginRunAllowsOneActiveRunAndRollsBackLoserTask`，重复 20 轮与 race 通过 |
| BeginRun 的 Task running、RunAttempt 与两条 Event 同一事务 | 并发失败方 Task 保持 queued，Run/Event 数量均为 1 |
| Event sequence 单调且不可 UPDATE/DELETE | `TestWorkerRunSessionAndJournalAreReadableAndJournalIsImmutable` |
| Worker、RunAttempt、SessionBinding 可持久化并读取 | `TestWorkerRunSessionAndJournalAreReadableAndJournalIsImmutable` |

## 4. 自动化验证

```text
go test -count=1 ./...                                      PASS
go test -race -count=1 ./...                                PASS（0 race）
go vet ./...                                                 PASS
go test -count=20 ./internal/persistence/sqlite \
  -run 'TestConcurrentTaskIdempotency|TestConcurrentBeginRun' PASS
go test -race -count=5 ./internal/persistence/sqlite \
  -run 'TestConcurrentTaskIdempotency|TestConcurrentBeginRun' PASS（0 race）
git diff --check                                             PASS
```

冻结 ADR-001 无 diff。目标 persistence/controlplane 未导入旧 `internal/store`、`internal/service` 或 tmux connector。

## 5. 退出判定

- M1 当前状态可由统一 Repository 完整读取；
- 复合状态写入具有事务故障注入证据；
- queued Task 积压与 Agent 级 Active Run 唯一约束已落在目标 schema/transaction；
- Task 04 可以在该边界上实现 Worker register、lease、Mailbox claim/accept 和 Worker Control API。
