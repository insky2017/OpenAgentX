---
doc_type: implementation_task
status: pending
owner: openagentx
updated_at: 2026-08-30
---

# 任务 02：目标契约、Schema 与测试骨架

## 目标

在实现运行时前冻结目标领域、API、schema version、稳定错误和 conformance 接口，使后续 daemon、Worker、Adapter、远程 binding 和 Web 共用同一契约。

## 依赖与入口

- 依赖：任务 01；
- 入口：ADR-001 的 Persistent Model、Worker Contract 和 API Boundaries；
- 退出门槛：G0。

## 实施范围

- 定义 Agent、Task、Message、MailboxItem、WorkerInstance、WorkerCommand、RunAttempt、SessionBinding、Approval 和 Event 的 ID、状态与 version；
- 定义 Worker/Auth/Observe/Control/Admin API 的版本化 DTO、错误码、幂等键和 CAS 前置条件；
- 定义 schema version 表、全新数据库 migration 顺序和旧库拒绝策略；
- 定义 `WorkerControlClient`、Runtime Adapter、TurnHandle 和 descriptor conformance interface；
- 建立 fake clock、fake broker、fake backend、并发 barrier 和临时 SQLite testkit；
- 写下 `max_active_runs_per_agent=1`、lane ordering、Cancel desired state 和 Approval scope 等核心不变量测试。

## 目标代码边界

```text
OpenAgentX/internal/domain/
OpenAgentX/internal/api/
OpenAgentX/internal/runtime/contract.go
OpenAgentX/internal/persistence/sqlite/migrations/
OpenAgentX/internal/testkit/
```

M0 可以新增只承载契约和测试骨架的代码，但不能形成绕过旧路径的半成品生产调度。

## 质量与验证

- DTO 不导入 application service 的内部类型；
- domain 不导入 SQLite、HTTP、CLI 或具体 Backend；
- 所有枚举拒绝未知值，JSON 严格校验；
- schema migration 在空数据库上可重复验证，旧 schema 明确返回不兼容错误；
- fake 实现必须能模拟 turn 阻塞、完成、取消、审批、崩溃和丢 wakeup。

## 退出条件

- 领域、API、schema 和 conformance 契约可编译并有契约测试；
- 后续任务不需要直接依赖旧 `service` DTO；
- 不变量清单与 ADR-001 一致；
- G0 验收记录完成。
