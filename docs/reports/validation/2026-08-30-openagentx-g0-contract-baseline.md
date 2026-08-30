---
doc_type: validation_report
status: passed
validated_at: 2026-08-30
---

# OpenAgentX ADR-001 G0 契约基线验证报告

## 1. 结论

任务 02 已建立可编译、可迁移、可测试的 ADR-001 目标契约基线，G0 通过。该基线只定义目标领域、API、schema 和 conformance harness，未接入旧 V0 生产调度路径。

## 2. 契约范围

- `internal/domain`：逻辑 Agent、Task、Message、MailboxItem、WorkerInstance、WorkerCommand、RunAttempt、SessionBinding、Approval、Event Journal、ExecutionSpec 的状态与不变量；
- `internal/api`：Worker、Auth、Observe、Control、Admin 的版本化 DTO、严格 JSON 解码、幂等键与 CAS 前置条件；
- `internal/runtime`：`AgentRuntimeAdapter`、`TurnHandle`、descriptor、Runtime Event 和稳定 unsupported 错误；
- `internal/persistence/sqlite/migrations`：从空库创建 `schema_meta(version=1)` 和 ADR-001 全量目标表，拒绝旧 schema 与未知版本；
- `internal/testkit`：fake clock、可丢 wakeup broker、并发 barrier、fake Adapter/TurnHandle 和临时目标 SQLite。

## 3. 核心不变量证据

| 不变量 | 自动化证据 |
|---|---|
| 逻辑 Agent 不包含 pane、socket 或 Worker 地址 | `TestLogicalAgentIdentityHasNoRuntimeAddress` |
| 每个逻辑 Agent 最多一个 Active Run | `TestSingleActiveRunInvariant`、`TestTargetSchemaAllowsQueuedTasksButRejectsTwoActiveRuns` |
| Agent 可积压多个 queued Task | `TestTargetSchemaAllowsQueuedTasksButRejectsTwoActiveRuns` |
| control lane 优先、同 lane 按 sequence | `TestMailboxOrderingAndLaneConstraints` |
| `cancel_requested` 禁止新 RunAttempt | `TestCancelRequestedPreventsBeginAttempt` |
| native Approval 绑定 Run/version，preflight 不绑定活动 Run | `TestApprovalScopeContracts` |
| 目标 Task/Message 使用 principal 与逻辑 `agent_id` 寻址 | `TestTargetTaskAndMessageRequirePrincipalAddressing` |
| API 拒绝未知字段、未知协议版本和非法枚举 | `TestDecodeStrictJSON`、`TestWorkerRequestsRejectStaleOrUnknownContractValues` |
| 写契约要求幂等键及必要 CAS version | `TestCommandMetaRequiresIdempotencyAndVersion`、`TestControlRequestsRequireIdempotencyCASAndStructuredExecution` |
| 空库 migration 可重复，旧库和未知 schema version fail closed | `TestApplyCreatesTargetSchemaAndIsRepeatable`、`TestApplyRejectsLegacyAndUnsupportedSchemas` |
| fake backend 可阻塞、完成、取消、审批和崩溃 | `TestFakeAdapterSimulatesControlCompletionAndCrash` |
| Broker wakeup 丢失可被测试 | `TestFakeClockAndDroppedBrokerWakeup` |

## 4. 自动化验证

```text
go test ./...                  PASS
go test -race ./...            PASS（0 race）
go vet ./...                   PASS
git diff --check               PASS
python3 ../scripts/check_docs.py PASS
```

边界检查结果：

- `internal/api`、`internal/runtime`、`internal/persistence`、`internal/testkit` 不导入旧 `internal/service` DTO；
- `internal/domain` 不导入 SQLite、HTTP、CLI、connector 或具体 Runtime；
- `scripts/check-legacy-control-paths.sh --inventory` 正常完成，旧路径仍按计划保留至任务 18；
- 冻结 ADR-001 无 diff，验证时 SHA-256 为 `587e9b8f27ddeb68aae8e5a507fefc10973739c8dd2c3def86e2c0bbc51db403`。

## 5. G0 退出判定

- 目标领域对象、状态、错误码、API DTO、schema version 和 migration 规则具备唯一代码入口；
- Worker/Adapter conformance harness 可运行，后续实现无需依赖旧 `service` DTO；
- 任务 01 的删除映射可机器执行；
- 任务 03 可以在本契约上实现 Transactional State 与 Event Journal。
