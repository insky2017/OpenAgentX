---
doc_type: implementation_task
status: pending
owner: openagentx
updated_at: 2026-08-30
---

# 任务 05：并发 Resident Worker

## 目标

实现独立 `openagentx worker run` 进程及 Heartbeat Loop、Mailbox Pump、Active Run Manager、Worker Control Loop，使活动 turn 的等待不会阻塞收件与控制。

## 依赖与入口

- 依赖：任务 04；
- 入口：`WorkerControlClient`、Runtime Adapter 和 TurnHandle 契约；
- 首先使用 fake backend 验证并发模型。

## 实施范围

- 实现 Worker 配置加载、身份注册、自检、lease 获取和优雅关闭；
- 以 `errgroup` 管理四个常驻 loop，并统一传播致命错误；
- Mailbox Pump 持续领取 control item，把 work item 交给 Run Manager；
- Active Run Manager 采用单 actor 串行化 StartTurn、Steer、DecideApproval 和 RequestCancel；
- 独立 Turn Waiter 调用 `TurnHandle.Wait`，并把完成结果交回 actor；
- V1 内存和 daemon 双重约束每 Agent 一个 Active Run；
- 失去 lease 或收到停止信号后停止领取新工作并受控 reconcile。

## 目标代码边界

```text
OpenAgentX/internal/worker/
OpenAgentX/internal/runtime/fake/
OpenAgentX/internal/cli/worker.go
OpenAgentX/cmd/openagentx/
```

## 质量与验证

- fake turn 阻塞时，Heartbeat、control claim、Steer、Approval 和 Cancel 仍可前进；
- `Wait` 与三个控制操作的 race 测试不出现 data race、重复完成或死锁；
- work capacity 为零时不 claim 新 Task，但 control lane 正常；
- Worker 正常 stop 返回退出码 `0`，异常退出返回非零；
- `go test -race` 覆盖 actor、channel 关闭、context cancel 和 late result。

## 退出条件

- Worker 完成一个 fake turn 后保持在线并继续 long poll；
- 活动 turn 不阻塞合法控制输入；
- 旧 Worker 失去 lease 后不能继续写入；
- Worker 代码不导入 tmux connector 或旧 pane session 类型。
