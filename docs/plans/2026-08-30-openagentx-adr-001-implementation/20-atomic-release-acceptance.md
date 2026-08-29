---
doc_type: implementation_task
status: pending
owner: openagentx
updated_at: 2026-08-30
---

# 任务 20：原子发布验收与旧库归档

## 目标

执行 OpenAgentX 目标架构的正式原子发布，完成全链路验收、旧数据库只读归档和发布证据收敛，不启用任何旧控制路径。

## 依赖与入口

- 依赖：任务 19；
- 入口：已冻结的发布/恢复运行手册和 go/no-go 清单；
- 退出门槛：GR。

## 发布步骤

1. 冻结旧业务入口并确认没有进行中的旧 Task；
2. 停止旧 daemon/agent session 控制服务；
3. 备份旧 SQLite，记录 hash，设置只读归档并验证可读；
4. 部署唯一 `openagentx` 构建、目标 schema、daemon、Worker、Nginx 和证书；
5. 创建/验证 owner，确认 Web Session、RBAC、CSRF 和审计；
6. 等待全部必要 Worker 注册、lease、Adapter/Backend health 和 Mailbox claim ready；
7. 开放 CLI/MCP/Web 的新 Task/Message 入口；
8. 执行本机与远程 Agent、AGY 与 ACP、手机与 PC 的验收矩阵；
9. 保存测试结果、Event sequence、服务状态和发布报告。

## 必须验收

- 同一 Agent 连续完成 Task A/Task B，无 tmux 控制调用；
- multi-turn Message、Cancel、native/preflight Approval 和 `uncertain` 场景正确；
- 两 Worker 竞争时只有一个 Active Run，旧 fencing 写入拒绝；
- UDS 与 mTLS Worker API conformance 一致；
- coordinated/direct dispatch、组织权限和 Admin 边界正确；
- 手机可登录、发指令、处理待办、跟踪结果，PWA 离线 fail closed；
- daemon/Worker 重启和 SSE 重连不丢持久事实；
- 当前运行环境不存在 `agentbus`、TmuxConnector、pane/Stop Gate 可执行路径。

## 发布失败处理

- 任一安全、状态一致性、旧路径扫描或核心 E2E 失败均判定 no-go；
- 关闭新业务入口，停止目标服务，保存新库和日志用于分析；
- 按运行手册恢复发布前版本，不把新旧数据库双写或开启 tmux fallback；
- 修复后重新从任务 19 完整演练，不能跳过失败检查点。

## 退出条件

- GR 全部验收通过并形成正式验证报告；
- 旧库有 hash、位置、权限和只读验证记录；
- 当前文档、服务和操作入口只描述 OpenAgentX；
- 主计划更新为 completed，ADR-001 保持 Accepted/冻结。
