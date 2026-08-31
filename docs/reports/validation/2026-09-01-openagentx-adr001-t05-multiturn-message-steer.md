---
doc_type: validation_report
status: passed
owner: openagentx
test_id: T05
updated_at: 2026-09-01
---

# OpenAgentX ADR-001 T05 验证报告

## 结论

T05 通过。Task 可跨多个 turn；queued Message 在当前 turn 活动时持久化，不会被投递给其他 Task，当前 turn 结束后由下一 RunAttempt 消费；SessionBinding 能从 new 转为 resume。消息幂等、defer/reclaim 和 Worker 鉴权边界均通过自动化矩阵与独立审计。

## 构建与环境

| 项目 | 证据 |
|---|---|
| 源码基线 | `02a876b` 加 T05 未提交实现批次 |
| 二进制 SHA-256 | `7959f6596d86ae74c1fe5a1859d90c82558e95fa87c9381ce9ed5d4e28ca651b` |
| daemon / Worker | `openagentx.service`、`openagentx-quote-service-worker.service` 均为 `active` |
| Worker | generation `11`，fencing token `21`，UDS `0600` |
| Runtime | `agy-graft`，model `gemini-3.7-flash-low`，reasoning `backend_default` |
| ADR-001 SHA-256 | `587e9b8f27ddeb68aae8e5a507fefc10973739c8dd2c3def86e2c0bbc51db403` |

测试经正式 HTTPS Control API 提交，Worker 通过 OpenAgentX UDS 领取；凭据、Cookie、CSRF、代理认证信息和完整 fencing token 未进入报告。

## 自动化矩阵（L1/L2）

- 聚焦测试覆盖 multi-turn、SessionBinding new/resume、native/queued/unsupported 路由、跨 Task 隔离、并发幂等 replay、defer/reclaim ACK、payload 与 WorkerCommand ACK guard；
- 隔离 UDS 覆盖注册、heartbeat、Task claim、native Message claim、错误 fencing payload 拒绝、payload resolve、accept、finish 和 control long poll wakeup；
- `go test -race` 相关包通过；相关包 `go vet` 通过；`go test -count=1 ./...` 通过；
- 独立 `gpt-5.6-terra high` 审计未发现 P0/P1；`git diff --check` 通过。

## 真实 queued steer（L3）

1. 通过正式 Control API 创建 Task `task-d27444c9-72da-43d2-bd3b-2c4984caf4b5`，要求执行无文件副作用的延时命令。
2. Task version `2`、状态 `running` 时创建补充 Message `message-c215848f-6fc6-57b0-97fe-366395181b51`，Event/Mailbox sequence 为 `20`。
3. 首个 RunAttempt `run-6ebb9191-6d00-4b43-8a5e-c112a7e8c1aa` 完成后，Task 保持开放并立即形成第二个 RunAttempt `run-be0f9ca9-814b-4e06-b691-5525171fd51a`。
4. 两个 run 使用同一 WorkerInstance、backend `primary` 和 model `gemini-3.7-flash-low`；SessionBinding provider session 保持一致，version 更新为 `2`。
5. 第二 turn 消费补充 Message，最终返回 `QUEUED_FOLLOWUP_ACK`；Task version `6`、状态 `succeeded`。
6. Task 与 Message MailboxItem 均为 work lane、最终 `accepted`，每项 attempts 为 `1`。

## 失败与边界

- 首次 Message 请求省略 `sender_principal_id`，Control API 以 `INVALID_INPUT` 拒绝；补齐契约字段后成功。该失败未写入 Message 或 Mailbox。
- AGY Adapter 的 `steer=queued` 已由真实 Runtime 验证；native steer 由 Fake/UDS conformance 覆盖，AGY 不虚报 native 能力。
- T06 将继续验证 Cancel 与 Approval 的 finish 竞态，不纳入 T05 结论。
