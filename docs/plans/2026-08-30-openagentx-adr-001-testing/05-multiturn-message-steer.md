---
doc_type: test_task
status: pending
owner: openagentx
test_id: T05
updated_at: 2026-08-30
---

# T05：Multi-turn、Message 与 queued steer

## 目标

验证 Task 可以跨多个 turn，补充 Message 在 native/queued/unsupported capability 下按 ADR 处理。

## 场景

1. `running -> waiting_input -> message -> running -> succeeded`。
2. active turn 中发送 Message；native steer 立即进入当前 handle。
3. queued steer 将 Message 持久化，当前 turn 结束后创建下一 RunAttempt。
4. 当前 run CAS miss 的 Message 保留为合法下一 turn 输入。
5. Backend 连 deferred follow-up 都不支持时，在入队前明确拒绝。
6. SessionBinding resume/new 模式与 Runtime descriptor 一致。

## 通过条件

Task 不因一次 turn 结束而错误 complete；Message 无丢失、无越权、无重复，run/version/session 归属可从事件和状态表审计。
