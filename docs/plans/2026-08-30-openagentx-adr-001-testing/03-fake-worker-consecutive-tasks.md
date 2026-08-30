---
doc_type: test_task
status: completed
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

## 验收结果

- 隔离 daemon、UDS、数据库和 Fake Worker 启动成功；稳定基线 Worker 为 generation 9、fencing 16、PID 1051417。
- Task A、Task B 均 `queued → running → succeeded`，分别创建独立 MailboxItem 和 RunAttempt；两任务之间 Worker 保持 online，PID/WorkerInstance/generation/fencing/NRestarts 不变。
- 快速提交的 20 个短 Task 全部 succeeded，Mailbox sequence 为 6—25 且全部 accepted/attempts 1。
- 20 个 Task 生成 20 个独立 succeeded RunAttempt，只使用同一 Worker；RunAttempt 区间重叠数和 Mailbox/Run 启动顺序违例数均为 0。
- 完成后 Active Run 和 pending/claimed Mailbox 均为 0；Worker 进程无子进程，配置与命令行不包含 tmux 或 pane 寻址。
- Event Journal 对每个 Task 均形成 `task.created → mailbox.claimed → task.running → run_attempt.started → mailbox.accepted → run_attempt.finished → task.settled` 的连续因果链。

证据见 [T03 验证报告](../../reports/validation/2026-08-30-openagentx-adr001-t03-fake-worker-consecutive-tasks.md)。
