---
doc_type: validation_report
status: passed
owner: openagentx
test_id: T03
validated_at: 2026-08-30
---

# OpenAgentX ADR-001 T03 Fake Worker 连续任务验证

## 结论

T03 通过。同一逻辑 `test-fake-agent` 的 Resident Worker 在完成 Task A 后没有退出，而是重新等待 Mailbox 并自动处理 Task B；随后连续处理 20 个快速入队任务，始终保持单 Agent 单 Active Run，没有 tmux、pane 或永久 LLM wait。

## 环境

| 检查 | 结果 |
|---|---|
| 代码基线 | `6ab3d78 test: pass OpenAgentX ADR-001 T02` |
| 验证二进制 SHA-256 | `6cf1966155269aef9654fb76aba7699d1c58448603e0506494bf7ca5486524c6` |
| 隔离数据 | 生产 SQLite 在线备份到 `/run/user/1000/openagentx-t03`；测试只经公开 Control/Worker API 写入 |
| daemon | 独立 UDS、`rtx4090:18101`，PID 1050322 |
| Worker 稳定基线 | PID 1051417、WorkerInstance `worker-d09bd7cb-6317-4734-8468-6e5b4fe58da9`、generation 9、fencing 16 |
| 启动说明 | 复制库旧 generation 8 lease 到期前 Worker 注册发生 11 次预期冲突；generation 9 online 后 NRestarts 保持 11 |

隔离测试结束后服务已停止，数据库、Session Cookie 和临时响应头副本已覆盖清空。报告不记录任何密码、Token、Cookie、CSRF 或摘要。

## Task A / Task B

| Task | Task 状态 | Mailbox | RunAttempt | Worker 连续性 |
|---|---|---|---|---|
| A `task-7c06afbe-e2ca-4d66-a607-b96f263490b2` | succeeded | sequence 4、accepted、attempts 1 | `run-cac886d2-80a2-48df-83e9-0c6a6b5938f4` succeeded | PID 1051417、generation 9、fencing 16 |
| B `task-829b0fa2-408d-420a-9584-bd1bbd339ffc` | succeeded | sequence 5、accepted、attempts 1 | `run-3a790531-ceff-4e35-a89c-949862828f43` succeeded | PID 1051417、generation 9、fencing 16 |

A 的 Event sequence 为 3441—3447，B 为 3448—3454。两者均依次记录 `task.created`、`mailbox.claimed`、`task.running`、`run_attempt.started`、`mailbox.accepted`、`run_attempt.finished`、`task.settled`。

## 20 Task 压力结果

| 断言 | 实际结果 |
|---|---|
| Task 数 / succeeded / distinct | 20 / 20 / 20 |
| RunAttempt 数 / succeeded / distinct | 20 / 20 / 20 |
| 使用的 Worker 数 | 1 |
| Mailbox sequence | 6—25，连续 20 条 |
| Mailbox accepted / attempts 1 | 20 / 20 |
| RunAttempt 时间区间重叠 | 0 |
| Mailbox sequence 与 Run 启动顺序违例 | 0 |
| 结束后的 Active Run | 0 |
| 结束后的 pending/claimed Mailbox | 0 |
| 稳定基线后的 Worker 重启 | 0 |

20 个 Task 各产生 7 类领域事件，共 140 条目标聚合事件；每类事件数量均为 20，因果链完整。

## Runtime 边界

- Worker 命令行为 `openagentx worker run --config .../agent.yaml`，没有 tmux/pane 参数。
- Fake Adapter 在 Worker 进程内执行；Worker 子进程数为 0。
- Task 的业务目标始终是逻辑 `agent_id=test-fake-agent`，没有使用 WorkerInstance 作为业务地址。
- `go test ./internal/worker -count=20` 通过，实时隔离 E2E 进一步证明进程和持久状态闭环。
