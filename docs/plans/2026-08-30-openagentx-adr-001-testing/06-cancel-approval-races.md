---
doc_type: test_task
status: pending
owner: openagentx
test_id: T06
updated_at: 2026-08-30
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

同时验证 control lane 优先于 work lane、lane 内按 sequence 升序、同一用户重复点击审批/取消保持幂等。

## 通过条件

双向竞态至少各重复 100 次且结果稳定；Cancel 权威事实先落 Task，native Approval 不发生 authority leakage，页面审批与 API/Event 状态一致。
