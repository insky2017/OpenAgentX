---
doc_type: validation_report
status: passed
validated_at: 2026-08-30
---

# OpenAgentX Task 06 AGY Batch 与 UDS 验证报告

## 1. 结论

`AgyBatchAdapter` 已作为首个真实南向 Backend Adapter 接入 Worker。它以 direct argv 启动 `agy --print --output-format stream-json`，支持已有 conversation resume，解析结构化输出并将进程退出、畸形流、超时和 stderr 限制归一化为标准 TurnResult。任务 06 的 Adapter 与 UDS 端到端门槛通过。

## 2. 实现边界

- `internal/runtime/agy` 提供 descriptor、health probe、ExecutionSpec 校验、stream-json parser、stdout/stderr 限制、进程超时和受控 SIGTERM cancel；
- AGY descriptor 明确声明 `steer=queued`、`approval=preflight`、`cancel=process_signal`，不伪造 mid-turn steering；
- Worker 通过 `BackendPool` 选择 AGY Adapter，Adapter 不写 Task/Mailbox；
- `--conversation <provider-session-id>` 只由结构化 `SessionBinding` 传入，prompt 和参数不经 shell 拼接；
- 真实 UDS Worker Control 链路与 Task 05 的 Resident Worker 连续 Task 验证保持一致，AGY 进程按 turn 启动，Worker 不随 turn 退出。

## 3. 不变量与证据

| 不变量 | 验证证据 |
|---|---|
| stream-json 逐行解析并产生标准 Runtime Event/TurnResult | `TestParseStreamJSONNormalizesEventsAndResult` |
| 空流、畸形 JSON 明确失败 | `TestParseStreamJSONRejectsMalformedAndEmptyInput` |
| command 使用 direct argv，conversation 被正确传递 | `TestAgyBatchAdapterUsesDirectArgvConversationAndClassifiesExit` |
| 非零退出即使输出 succeeded 也不得直接成功 | `TestAgyBatchAdapterTimeoutAndNonZeroAreNotSuccess` |
| stderr 有大小上限且敏感内容不写入结果错误 | 同一测试 |
| health probe 在启动前执行 | `Adapter.Health` 与 Worker `BackendPool.Observe` 路径 |
| Worker A/B 连续执行不依赖终端输入 | `TestRunWorkerProcessCompletesConsecutiveTasksWithoutTerminalInput` |
| 运行期间不调用 tmux 控制命令 | AGY Adapter 只调用 `exec.CommandContext`；目标 Worker 包依赖边界检查与 UDS E2E |

## 4. 自动化验证

```text
go test -count=20 ./internal/runtime/agy PASS
go test -race -count=10 ./internal/runtime/agy PASS（0 race）
go test -count=1 ./...                                      PASS
go test -race -count=1 ./...                                PASS（0 race）
go vet ./...                                                 PASS
git diff --check                                             PASS
```

冻结 ADR-001 未修改。AGY Adapter 不依赖 tmux connector、pane 地址或 AGY Stop Hook；旧 `agentbus runtime agy-hook` 仍留待 Release 任务删除，不参与目标 Worker 执行路径。

## 5. 退出判定

- 首个真实 Backend Adapter 的执行、事件、退出分类和安全边界已具备；
- Worker 与 AGY process 生命周期已分离，AGY 每个 turn 结束后释放，Worker 继续等待 Mailbox；
- 后续 Task 07 可以在此 Adapter 上补全 SessionBinding 持久化和 multi-turn reconcile，不需要改变 Adapter/Worker 控制接口。
