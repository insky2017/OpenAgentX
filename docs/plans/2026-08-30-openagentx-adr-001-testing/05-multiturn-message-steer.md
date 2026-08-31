---
doc_type: test_task
status: pending
owner: openagentx
test_id: T05
updated_at: 2026-09-01
---

# T05：Multi-turn、Message 与 queued steer

## 目标

验证 Task 可以跨多个 turn，补充 Message 在 native/queued/unsupported capability 下按 ADR 处理，并覆盖实际暴露过的路由、幂等、重试和鉴权缺口。

## 场景

1. `running -> waiting_input -> message -> running -> succeeded`。
2. active turn 中发送 Message；native steer 立即进入当前 handle。
3. queued steer 将 Message 持久化，当前 turn 结束后创建下一 RunAttempt。
4. 当前 run CAS miss 的 Message 保留为合法下一 turn 输入。
5. Backend 连 deferred follow-up 都不支持时，在入队前明确拒绝。
6. SessionBinding resume/new 模式与 Runtime descriptor 一致。
7. 同一 Agent 的 Task A active 时提交 Task B Message，确认消息不会投递到 Task A 的 active turn；目标 Task 和目标 RunAttempt 可审计。
8. 相同幂等键并发提交返回稳定 replay，不暴露 SQLite 唯一键错误或重复 MailboxItem。
9. Message defer 后发生 reclaim/重试，ACK 保持幂等且消息不丢失、不重复执行。
10. Worker payload 读取和 WorkerCommand ACK 同时使用 token、principal、generation、lease、fencing 校验；组合错误返回安全优先级最高的拒绝。

## 通过条件

Task 不因一次 turn 结束而错误 complete；Message 无丢失、无越权、无重复，跨 Task 路由、幂等 replay、defer/reclaim、run/version/session 归属和鉴权结果可从事件和状态表审计。完整矩阵、race、vet 和隔离 UDS E2E 均通过后，才可将 T05 标记 PASS。
