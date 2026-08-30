---
doc_type: validation_report
status: passed
validated_at: 2026-08-30
---

# OpenAgentX Task 05 并发 Resident Worker 验证报告

## 1. 结论

独立 `openagentx worker run` Worker 已具备四个并发职责：Heartbeat、Mailbox Pump、Active Run Manager 和 Worker Control Loop。活动 `TurnHandle.Wait` 不再阻塞 heartbeat、control claim 或活动控制；Worker 完成一个 turn 后保留进程与租约，继续领取后续 Task。任务 05 验收通过。

## 2. 实现边界

- `internal/worker.Runner` 使用 `errgroup` 管理四个 loop，统一处理 context cancel、lease loss 和受控 stop；
- `ActiveRunManager` 是单 actor，串行化 StartTurn、Steer、Approval、Cancel、Wait completion 和 Finish；
- `BackendPool` 只依赖 `AgentRuntimeAdapter`，按 descriptor/health 选择实际 Backend；
- `internal/runtime/fake` 提供可阻塞、可控制、可自动完成的 fake Adapter/TurnHandle；
- `openagentx worker run --config` 使用严格 YAML 配置、独立 CLI 入口和非零异常退出语义；
- M1 进程装配支持显式 fake Backend，用于验证生命周期；真实 AGY Backend 在任务 06 接入。

## 3. 不变量与证据

| 不变量 | 验证证据 |
|---|---|
| TurnHandle.Wait 阻塞时 heartbeat 仍运行 | `TestResidentWorkerWaitDoesNotBlockHeartbeatOrActiveControlAndContinuesToNextTask` |
| 活动 turn 可并发接收 native steer、approval、cancel | 同一测试及 `TestTurnCompletionRacesAllControlOperationsWithoutDeadlockOrDuplicateFinish` |
| 单 Agent 同时只有一个活动执行，第二 Task 保持 Mailbox pending | Resident Worker 并发测试检查 `WorkCapacity=0` 与第二 item 未被取走 |
| Worker 完成 Task A 后继续完成 Task B，无终端输入 | `TestRunWorkerProcessCompletesConsecutiveTasksWithoutTerminalInput`，真实 UDS + fake Backend |
| drain 停止新 work claim，但 control/heartbeat 仍能完成 | `TestDrainStopsNewWorkWhileMailboxPumpKeepsRunningAtZeroCapacity` |
| 受控 stop 先取消并 reconcile 活动 Run，再确认命令 | `TestControlledStopCancelsAndReconcilesBeforeAcknowledgement` |
| 失去 lease 以异常错误退出 | `TestLeaseLossIsFatalAndReturnsNonZeroSemantics` |
| Wait、控制操作与 late completion 无 data race、重复 Finish 或死锁 | `go test -race` 聚焦 20 轮 |
| 配置拒绝未知字段和旧 tmux 字段 | `TestLoadProcessConfigIsStrictAndGeneratesRunnerConfig` |
| CLI 正常/异常退出码符合 Worker 约定 | `TestOpenAgentXWorkerRunExitCodes` |

## 4. 自动化验证

```text
go test -count=20 ./internal/worker \
  -run 'TestResidentWorker|TestTurnCompletionRaces|TestControlledStop|TestDrain|TestLeaseLoss' PASS
go test -race -count=20 ./internal/worker \
  -run 'TestResidentWorker|TestTurnCompletionRaces|TestControlledStop|TestDrain|TestLeaseLoss' PASS（0 race）
go test -count=20 ./internal/cli/worker \
  -run TestRunWorkerProcessCompletesConsecutiveTasks PASS
go test -race -count=10 ./internal/cli/worker \
  -run TestRunWorkerProcessCompletesConsecutiveTasks PASS（0 race）
go test -count=1 ./...                                      PASS
go test -race -count=1 ./...                                PASS（0 race）
go vet ./...                                                 PASS
git diff --check                                             PASS
```

冻结 ADR-001 未修改。Worker 包不导入 tmux connector、旧 pane session 或旧控制面；`openagentx` 入口不依赖 legacy `connector/service/store/server`。

## 5. 退出判定

- Resident Worker 已从一次性 CLI 执行模型变为可连续接活的长期进程；
- 活动 turn 的等待与控制、heartbeat、Mailbox 收件已解耦；
- Task 06 可以在不改 Worker actor 模型的前提下接入真实 AGY Batch Adapter，并完成真实 Backend E2E。
