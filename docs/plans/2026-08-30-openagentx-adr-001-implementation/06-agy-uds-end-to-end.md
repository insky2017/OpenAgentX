---
doc_type: implementation_task
status: completed
owner: openagentx
updated_at: 2026-08-31
---

# 任务 06：AGY Batch + UDS 端到端闭环

## 目标

实现首个真实 Agent Runtime Adapter，并证明 Domain Agent 完成 Task A 后不退出 Worker、无需 tmux 注入即可自动领取并完成 Task B。

## 依赖与入口

- 依赖：任务 05；
- 入口：并发 Worker、M1 状态模型和 UDS Worker API；
- 退出门槛：G1。

## 实施范围

- 实现 `AgyBatchAdapter` descriptor、启动前 health probe 和 stream-json parser；
- 使用配置的 binary 和工作目录，以 direct argv 启动 `--print --input-format stream-json --output-format stream-json`，显式对齐 print timeout、preflight 权限、允许的 model/effort，不经过 shell；
- 将 conversation ID 持久化到最小 SessionBinding；
- 将 stream、usage、结果和退出分类归一化为标准 Runtime Event/TurnResult；
- 声明 `steer=queued`、已验证的 approval mode 和 `cancel=process_signal`；
- 实现 daemon、Worker 和受控 fake/real AGY 的 UDS E2E harness；
- 执行 Task A 完成、Worker idle/wait、Task B 到达/wake/完成的连续流程。

## 目标代码边界

```text
OpenAgentX/internal/runtime/agy/
OpenAgentX/internal/runtime/conformance/
OpenAgentX/internal/integration/agyuds/
OpenAgentX/docs/reports/validation/
```

## 质量与验证

- 超时、非零退出、畸形 stream、stderr 超限、进程信号和 conversation resume 测试；
- prompt、Backend 参数和环境变量不经 shell 拼接，日志执行脱敏；
- 退出码零不自动把 Task 标为 succeeded，必须经过 Worker reconcile；
- daemon 重启、Broker wakeup 丢失后 pending item 仍被领取；
- 测试期间拦截进程调用，证明没有 `tmux paste-buffer`、`send-keys` 或 `capture-pane`。

## 退出条件

- Task A 与 Task B 连续闭环通过；
- Worker 在两个 Task 之间保持在线、Backend process 不常驻占用 turn；
- 所有任务投递和结果都有状态表与 Event Journal 证据；
- G1 验收报告完成，才允许进入 M2。

## 完成记录

- 已实现 `AgyBatchAdapter`、descriptor、health probe、direct argv 启动、正式 NDJSON message envelope、嵌套 stream-json parser、stderr 脱敏诊断、超时/退出分类和 process-signal cancel；
- 已实现 AGY conversation resume、允许模型目录、`backend_default`/`effort`、turn timeout 和 workspace 配置传递，prompt 与 Backend 参数均不经 shell 拼接；
- Worker Control 已实现真正 long poll、Broker wakeup 和 fencing/lease fail-closed，空闲 daemon/Worker 不再形成 busy loop；
- 已通过真实 `agy-graft` Worker + UDS 连续 Task A/Task B 验证；同一 WorkerInstance、generation 和 fencing 在两个 AGY turn 之间保持在线并重新等待 Mailbox；
- 超时、非零退出、空流、畸形流、缺失终态和显式未知副作用均有 fail-closed 自动化覆盖；
- `go test -count=1 ./...`、`go vet ./...`、`git diff --check` 通过；
- 验收报告：[Task 06 AGY Batch 与 UDS 验证报告](../../reports/validation/2026-08-30-openagentx-task06-agy-uds.md)；
- 实机报告：[ADR-001 T04 AGY Resident Worker 连续任务验证](../../reports/validation/2026-08-31-openagentx-adr001-t04-resident-agy-e2e.md)；
- 冻结 ADR-001 未修改，AGY Adapter 不依赖 tmux connector、pane 或 Stop Hook。
