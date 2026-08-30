---
doc_type: test_task
status: pending
owner: openagentx
test_id: T03
updated_at: 2026-08-30
---

# T03：Fake Worker 连续任务闭环

## 目标

隔离真实 CLI 变量，证明 Resident Worker 完成 Task A 后不退出，并由 Mailbox 自动唤醒处理 Task B。

## 步骤

1. 在隔离环境启动 daemon 和 `fake` Worker，记录 PID、WorkerInstance、generation、fencing。
2. 从 Control API/指挥台向同一 `test-fake-agent` 提交 Task A。
3. 观察 `queued -> running -> succeeded`，核对 Mailbox 和 RunAttempt。
4. 确认 Worker PID 未变化，状态回到 online/idle，重新进入 claim wait。
5. 不操作 tmux/终端，提交 Task B 并验证同样闭环。
6. 连续执行至少 20 个短 Task，验证单 Agent 最多一个 Active Run及 mailbox sequence 顺序。

## 通过条件

- Task A/Task B 均成功且 RunAttempt 独立；
- Worker 在两任务之间常驻等待；
- 不存在 tmux、pane 或永久 LLM wait；
- Event Journal 可重建完整因果顺序。
