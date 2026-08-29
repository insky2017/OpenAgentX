---
doc_type: implementation_task
status: pending
owner: openagentx
updated_at: 2026-08-30
---

# 任务 09：恢复、Fencing 与故障注入

## 目标

验证 daemon、Worker、Backend、网络和 Broker 故障下的恢复边界，保证 stale writer 不能提交结果，无法确认副作用时 fail closed 为 `uncertain`。

## 依赖与入口

- 依赖：任务 08；
- 入口：完整 Task/RunAttempt/Mailbox 并发语义；
- 退出门槛：G2。

## 实施范围

- 实现 daemon 启动 reconcile：过期 Worker、claim、RunAttempt 和 workspace lease；
- 实现 Worker 重连、重新注册、generation 递增和新 fencing token；
- 实现 Backend process crash、Worker crash、daemon restart 和 transport timeout 分类；
- 区分可安全重试、需要人工确认和 `uncertain`；
- 对 EventBroker 丢 wakeup、重复投递、late finish、stale heartbeat 和 DB busy 注入故障；
- 建立可重复的 fake clock/barrier/process fault harness；
- 形成 M2 恢复运行手册和验证报告。

## 质量与验证

- 旧 Worker 在新 lease 授予后提交 Event/finish/artifact 全部被 fencing 拒绝；
- pending MailboxItem 在 daemon restart 后继续可领取；
- 已可能产生 workspace 副作用的 RunAttempt 不自动重跑；
- claim lease 过期与 Worker lease 过期分别处理，不互相替代；
- 同一故障重复执行得到确定状态，不依赖真实时间随机性。

## 退出条件

- 故障矩阵覆盖 daemon、Worker、Backend、network、Broker 和 SQLite；
- 所有恢复分支明确为 resume、retry、terminal 或 `uncertain`；
- stale writer 负向测试通过；
- G2 验收报告完成。
