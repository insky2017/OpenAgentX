---
doc_type: test_task
status: passed
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

## 验证结果

- L1 完整矩阵通过：聚焦测试、相关包 race/vet、`go test -count=1 ./...`、`git diff --check`；独立审计未发现 P0/P1；
- L2 隔离 UDS 覆盖 native Message、payload fencing 拒绝、Mailbox accept/finish、control long poll wakeup；仓储和服务矩阵覆盖跨 Task 隔离、并发幂等、defer/reclaim 及 token/principal/generation/lease/fencing 负向；
- L3 使用新构建和正式 `agy-graft` 验证 queued steer：Task `task-d27444c9-72da-43d2-bd3b-2c4984caf4b5` 在首个 run 活动时接收 Message sequence `20`，随后形成第二个 RunAttempt；
- 两个 RunAttempt 均由同一 WorkerInstance 执行，SessionBinding `binding-3ec80c23-4780-4abf-847b-47b0e7c8865b` 从 version `1` 更新为 `2`，provider session 保持一致；
- 第二 turn 最终返回指定标记 `QUEUED_FOLLOWUP_ACK`，Task version `6`、状态 `succeeded`；Message 对应 work-lane MailboxItem 最终为 `accepted`，未使用 tmux 或人工终端注入；
- 冻结 ADR SHA-256 保持 `587e9b8f...1db403`。详细证据见 [T05 验证报告](../../reports/validation/2026-09-01-openagentx-adr001-t05-multiturn-message-steer.md)。
