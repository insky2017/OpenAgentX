---
doc_type: implementation_task
status: completed
owner: openagentx
updated_at: 2026-08-30
---

# 任务 07：RunAttempt、SessionBinding 与 Multi-turn

## 目标

在 M1 最小执行实体上补全 Task 跨 turn 的状态、provider session 连续性、queued follow-up 和 RunAttempt 审计边界。

## 依赖与入口

- 依赖：任务 06 / G1；
- 入口：已运行的最小 RunAttempt、SessionBinding 和 AGY Adapter；
- 本任务不改变 V1 单 Agent 单 Active Run 限制。

## 实施范围

- 补全 Task `dispatching/running/waiting_input/waiting_approval/terminal` 转移；
- 明确 V1 一个 RunAttempt 至多对应一个 turn，Task 可以包含多个 RunAttempt；
- 持久化 `context_id + agent_id + backend_id` 到 provider session 的 SessionBinding；
- 实现 `new/resume`、失效检测和切换 Backend 时创建新 binding；
- 实现 AGY `steer=queued`：活动 turn 不能接收补充时，Message 可靠留到下一 turn；
- 实现 waiting_input 收到 Message 后重新 dispatch；
- 记录每个 RunAttempt 的输入引用、SessionBinding、事件、结果和核对信息。

## 目标代码边界

```text
OpenAgentX/internal/domain/task.go
OpenAgentX/internal/domain/run_attempt.go
OpenAgentX/internal/domain/session_binding.go
OpenAgentX/internal/controlplane/scheduler/
OpenAgentX/internal/worker/run_manager.go
```

## 质量与验证

- Task A 在同一 SessionBinding 完成 `turn 1 -> waiting_input -> turn 2`；
- Worker 更换后可以在 daemon 授权下恢复同一 provider session；
- Backend 切换创建新 SessionBinding，不能伪造跨 provider resume；
- 多个 queued Message 按 sequence 被下一 turn 消费且幂等；
- Task 终态后“继续工作”只能创建引用原 Task 的新 Task。

## 退出条件

- multi-turn 状态和 session 行为均有 repository、service 和 Worker 集成测试；
- Task 与 turn/RunAttempt 的成功边界不混用；
- 指挥台未来所需的 RunAttempt/SessionBinding read model 字段已稳定；
- 后续竞态任务可以用 version/CAS 识别当前活动执行。

## 完成记录

- 已增加 `waiting_input` TurnResult，并将本次 Run 结算为终止 Run、Task 保持可继续派发的 `waiting_input`；
- 已实现 SessionBinding 的 create/update CAS 保存，保持 `context_id + agent_id + backend_id` 唯一，并拒绝 stale writer；
- 已验证 waiting_input Task 收到补充 Message 后重新进入 work lane，Task 终态与单个 turn 的成功边界不混用；
- AGY Adapter 已支持同一 SessionBinding 的 conversation resume 参数，Backend 切换通过不同 binding key 隔离；
- `go test -count=20` 与 `go test -race -count=10` 聚焦 controlplane/persistence/runtime 通过；
- 验收报告：[Task 07 Multi-turn 与 SessionBinding 验证报告](../../reports/validation/2026-08-30-openagentx-task07-multi-turn.md)；
- 冻结 ADR-001 未修改。
