---
doc_type: test_task
status: pending
owner: openagentx
test_id: T07
updated_at: 2026-08-30
---

# T07：恢复、lease、generation 与 fencing

## 目标

证明 daemon、Worker 或 Runtime 崩溃及网络中断时，持久事实不丢、旧执行所有者不能继续写入，副作用不确定时不会盲目重试。

## 故障矩阵

- Mailbox commit 后、Broker publish 前故障；long poll 重查仍领取工作；
- Worker claim 后崩溃；lease 到期后可安全重领；
- active run 中 kill Worker；恢复后状态为可解释的 failed/uncertain/retryable；
- daemon 重启后 pending Mailbox、Task 和 Event sequence 保留；
- 新 Worker generation/fencing 生效，旧 token/generation/fencing 的 heartbeat、event、finish 全部拒绝；
- Backend 有潜在外部副作用但结果未知时进入 `uncertain`；
- SSE 断线重连从 last sequence 回放。

## 通过条件

没有双 Active Run、重复副作用或失联工作；恢复决策由持久状态和 lease/fencing 驱动，不依赖内存 Broker。
