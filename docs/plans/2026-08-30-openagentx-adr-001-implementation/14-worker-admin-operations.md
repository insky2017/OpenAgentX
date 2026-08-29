---
doc_type: implementation_task
status: pending
owner: openagentx
updated_at: 2026-08-30
---

# 任务 14：Worker Admin 与运维控制

## 目标

实现持久 Worker Control Channel 和实例级 drain、health-check、stop、lease revoke，使本机与远程 Worker 在断线、重连和受控退出时保持一致运维语义。

## 依赖与入口

- 依赖：任务 12、任务 13；
- 入口：WorkerCommand 表、Worker Control Loop、Admin application service 和组织审计 principal；
- 退出门槛：G4。

## 实施范围

- 实现 WorkerCommand create/claim/ack/retry 和 generation 绑定；
- 实现 `drain`：停止新 work claim，允许 control/reconcile 完成；
- 实现 `health_check`：返回 Adapter/Backend/lease 的结构化状态；
- 实现 `stop`：只停止当前 instance，收尾后以退出码 `0` 结束；
- 实现 daemon 直接 lease revoke 和 fencing 推进，不等待 Worker 在线；
- 提供 Admin API 的 application service，暂不由浏览器直连 Worker API；
- 提供 `Restart=on-failure` 的 Worker service 行为测试与运维说明。

## 质量与验证

- 命令按 `worker_instance_id + generation` 幂等，旧实例不能领取新实例命令；
- 断线时命令保持 pending，重连后领取并确认；
- stop 返回 `202 + worker_command_id` 只表示持久化，不提前显示 offline；
- 正常 stop 不被 systemd 自动拉起，手动启动产生新 instance/generation；
- lease revoke 后旧 Worker 的 heartbeat、Event 和 finish 立即失败。

## 退出条件

- 本机与远程 Worker 的 drain/health/stop/revoke E2E 通过；
- WorkerCommand 与 Agent Mailbox 物理和领域边界清楚；
- Admin 操作有完整 Event Journal 审计；
- G4 验收报告完成。
