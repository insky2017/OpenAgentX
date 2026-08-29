---
doc_type: implementation_task
status: pending
owner: openagentx
updated_at: 2026-08-30
---

# 任务 19：部署与原子切换演练

## 目标

在隔离环境完整演练 daemon、本机/远程 Worker、Nginx/Tailscale、证书、PWA 和数据库归档的单次切换，形成可重复发布与恢复运行手册。

## 依赖与入口

- 依赖：任务 18；
- 入口：唯一 OpenAgentX 构建、目标 schema 和完整部署资产；
- 演练不修改正式旧数据库，使用可验证副本或新库。

## 实施范围

- 创建 `openagentx.service` 和 `openagentx-worker@.service`，Worker 使用 `Restart=on-failure`；
- 配置 daemon 私有 Tailscale listener，例如 `http://rtx4090:18100`；
- 配置一个或多个 Nginx tailnet 入口、HTTPS、SSE buffering/timeout 和安全头；
- 验证 `tailscale cert` 或 Let's Encrypt DNS-01 的 ACME 证书签发/续期；
- 演练 owner 初始化、Worker identity/mTLS、Adapter health 和 mailbox ready gate；
- 演练停止旧入口、备份/只读归档旧库、创建目标库、启动服务、开放新入口；
- 演练失败时关闭新入口、停止新服务、恢复发布前版本的流程；
- 记录时序、负责人、命令、检查点和不可逆边界。

## 质量与验证

- daemon listener 不直接暴露公网，只允许已登记 Nginx Tailscale 节点；
- 多入口共享 Session，SSE 经代理重连正常；
- 受控 stop 退出码 `0` 不触发 Worker 自动重启；
- 远程 Worker 仅出站连接，证书吊销后不能重连；
- 切换过程中 Task 入口在 Worker ready 前保持关闭；
- 旧数据库归档校验 hash、权限和只读打开，目标 daemon 不加载它。

## 退出条件

- 至少完成一次成功切换演练和一次故障恢复演练；
- 运行手册可由未参与实现的操作者按步骤复现；
- 证书、服务、数据库和移动访问检查点齐全；
- 正式发布清单和 go/no-go 条件冻结。
