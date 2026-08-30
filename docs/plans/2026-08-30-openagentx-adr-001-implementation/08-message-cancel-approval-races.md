---
doc_type: implementation_task
status: completed
owner: openagentx
updated_at: 2026-08-30
---

# 任务 08：Message、Cancel 与 Approval 竞态

## 目标

实现活动 turn 控制输入的线性化语义，确保 Message 可合法进入下一 turn，而 Task Cancel 和 native Approval 不跨 RunAttempt 泄漏。

## 依赖与入口

- 依赖：任务 07；
- 入口：Task version、RunAttempt version、双 lane Mailbox 和 Active Run Manager；
- 并发测试先于最终状态机实现。

## 实施范围

- Message 与 turn finish 使用 Task/Run version CAS 判定进入当前 turn、下一 turn或拒绝；
- Cancel API 在同一事务写 `cancel_requested`、可选 control item 和 Event Journal；
- BeginAttempt 在 Task `cancel_requested` 后无条件拒绝；
- stale Cancel control item 结束为 `superseded/no-op`，不能迁移到下一 turn；
- native Approval 绑定 request/run/version/scope，CAS miss 后 `stale/superseded`；
- preflight Approval 绑定 scope digest、有效期和一次性消费，满足后才创建新 RunAttempt；
- Active Run Manager 串行调用 TurnHandle 的 Steer、DecideApproval、RequestCancel。

## 竞态矩阵

| 输入 | 当前 Run 已结束 | 目标行为 |
|---|---|---|
| Message | 是 | 非终态 Task 可保留为下一 turn；终态 Task 拒绝 |
| native Approval | 是 | `stale/superseded`，不得继承 |
| Cancel control item | 是 | `superseded/no-op`，Task 级 `cancel_requested` 仍有效 |
| preflight Approval | 无活动 Run | scope 匹配时作为下一 turn 前置条件一次性消费 |

## 质量与验证

- 对 Message/finish、Cancel/finish、Approval/finish 分别测试两个提交顺序；
- Cancel 请求幂等，重复请求不产生多份有效 control item；
- native Approval 不能作用于新的 run/version 或不同 tool scope；
- RequestCancel 成功只表示中断请求被接受，不直接产生 `canceled`；
- race 测试覆盖 control lane 顺序、late callback 和 context cancellation。

## 实施结果

- `RequestTaskCancel` 在单事务内写入 `cancel_requested`、审计事件和当前 Active Run 的 control-lane Cancel；重复请求幂等且不会生成第二个控制项。
- `FinishRun` 支持 Message/Finish 与 Cancel/Finish 的双向版本竞态：允许控制输入先推进 Task version，取消意图不会被迟到的运行结果覆盖。
- native Approval 校验 `target_run_id`、run version 和 scope 绑定；目标 Run 失效时请求转为 `stale`，批准不得跨 RunAttempt 继承。
- preflight Approval 支持按 Task/scope/有效期一次性消费。
- Active Run Manager 已在单 actor loop 中串行调用 TurnHandle 控制方法；失配的 Cancel/Approval 仅接受为 `superseded`，不会迁移到下一 turn。

## 退出条件

- 竞态矩阵的每个分支都有确定事务结果和 Event Journal 记录；
- 任何 Cancel 后续都不能创建新 RunAttempt；
- native Approval 不跨 RunAttempt，preflight Approval 不重复消费；
- `go test -race` 通过。

## 验证

详见 [Task 08 验证报告](../../reports/validation/2026-08-30-openagentx-task08-message-cancel-approval-races.md)。
