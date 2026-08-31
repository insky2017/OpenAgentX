---
doc_type: test_task
status: pending
owner: openagentx
test_id: T06
updated_at: 2026-09-01
---

# T06：Cancel、Approval 与 finish 竞态

## 目标

验证运行中的 Control Lane 不被 TurnHandle.Wait 阻塞，并钉死 Message、Cancel、native Approval 和 preflight Approval 的不同竞态语义。

## 场景

| 输入 | 与 finish 正向顺序 | 与 finish 反向顺序 | CAS miss 后预期 |
|---|---|---|---|
| Message | 当前 turn steer | 下一 turn 消费 | 可保留到下一 turn |
| Cancel | 中断当前 run | Task 已 cancel_requested | control item superseded/no-op，禁止新 run |
| native Approval | 应用于目标 run | 目标 run 已结束 | stale/superseded，不得继承 |
| preflight Approval | 满足下一执行条件 | 保持持久前置条件 | 可由下一 turn 消费 |

竞态语义固定如下：

- `Cancel` 的权威效果是事务性地将 Task 置为 `cancel_requested` 并阻止新的 RunAttempt；control item 只负责尽快中断当前 RunAttempt。若 CAS 未命中，control item 结束为 `superseded/no-op`，不得迁移到下一 turn。
- `native Approval` 绑定具体 `RunAttempt`/版本；目标 run 已结束时只能为 `stale/superseded`，不得继承到下一 turn，避免 authority leakage。
- 只有 `preflight Approval` 才能作为下一 turn 的持久执行前置条件。
- V1 每个逻辑 Agent 同时最多一个 active RunAttempt；lane 内按 `sequence ASC`，control lane 只优先于 work lane，不改变同 lane 的因果顺序。

同时验证 control lane 优先于 work lane、lane 内按 sequence 升序、同一用户重复点击审批/取消保持幂等。

## 通过条件

双向竞态至少各重复 100 次且结果稳定；Cancel 权威事实先落 Task，native Approval 不发生 authority leakage，页面审批与 API/Event 状态一致。测试还必须确认同一 Agent 不会并发运行两个 active RunAttempt。
