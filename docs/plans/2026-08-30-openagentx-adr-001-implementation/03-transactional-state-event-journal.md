---
doc_type: implementation_task
status: completed
owner: openagentx
updated_at: 2026-08-30
---

# 任务 03：Transactional State 与 Event Journal

## 目标

实现 ADR-001 的当前权威状态表与 append-only Event Journal，为 Mailbox、Worker、RunAttempt 和后续组织/Web 状态提供统一事务基础。

## 依赖与入口

- 依赖：任务 02；
- 入口：目标 schema 与 repository 契约；
- 本任务只升级全新目标数据库，不在线迁移旧 AgentBus 数据库。

## 实施范围

- 实现版本化 SQLite migration runner、WAL、foreign keys、busy timeout 和 schema version 校验；
- 实现 M1 所需的 Agent/Profile、Task、Message、WorkerInstance、MailboxItem、RunAttempt、SessionBinding 和 Event Journal 表；
- 实现事务 repository、version/CAS、幂等键和单调 journal sequence；
- 在同一事务中提交领域状态、必要 MailboxItem 与 Event Journal；
- 将 Agent 级单 Active Run 约束落在 RunAttempt/lease，而不是 queued Task；
- Event Journal 提供 sequence 分页读取，不承担 aggregate replay。

## 目标代码边界

```text
OpenAgentX/internal/persistence/sqlite/
OpenAgentX/internal/domain/
OpenAgentX/internal/controlplane/transaction.go
```

## 质量与验证

- migration、rollback、重开数据库、CAS 冲突、幂等和 foreign key 测试；
- 事务故障注入证明状态表、MailboxItem 和 Event Journal 同成同败；
- 两事务竞争创建 Active Run 时只能一个成功；
- journal sequence 单调且不可更新/删除；
- 现有旧 schema 输入被明确拒绝，不被隐式 `CREATE TABLE IF NOT EXISTS` 升级。

## 退出条件

- M1 领域状态可通过 repository 完整读写；
- 当前状态读取不依赖 Event replay；
- Task 可以积压多个 queued 工作，但同 Agent 不能有两个有效 Active Run；
- SQLite 并发和重启测试通过。

## 完成记录

- 已实现目标 SQLite Repository、`TransactionalState` 边界、schema 完整性校验和 append-only Event Journal trigger；
- Agent/Profile、Task、Message、WorkerInstance、MailboxItem、RunAttempt 与 SessionBinding 可直接从权威状态表读写；
- Task/Message/RunAttempt 的复合写入、CAS、幂等、外键和故障回滚测试通过；
- 并发幂等提交与单 Agent Active Run 竞争重复 20 轮并通过 race；
- `go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...` 通过；
- 验收报告：[Task 03 Transactional State 验证报告](../../reports/validation/2026-08-30-openagentx-task03-transactional-state.md)；
- 冻结 ADR-001 未修改，目标 persistence/controlplane 不依赖旧 store/service/tmux 路径。
