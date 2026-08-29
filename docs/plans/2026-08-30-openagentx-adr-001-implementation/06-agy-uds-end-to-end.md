---
doc_type: implementation_task
status: pending
owner: openagentx
updated_at: 2026-08-30
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
- 使用 direct argv 启动 `agy --print --output-format stream-json`，不经过 shell；
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
