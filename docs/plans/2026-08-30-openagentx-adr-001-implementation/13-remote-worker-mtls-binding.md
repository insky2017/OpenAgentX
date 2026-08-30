---
doc_type: implementation_task
status: completed
owner: openagentx
updated_at: 2026-08-30
---

# 任务 13：远程 Worker mTLS Binding

## 目标

在不改变 Worker 领域协议和 Runtime Adapter 的前提下，实现出站 HTTPS + mTLS 的远程 Worker Gateway，并验证跨主机迁移与身份隔离。

## 依赖与入口

- 依赖：任务 11 / G3；
- 入口：已通过 UDS conformance 的 Worker Control API；
- daemon 不反向连接 Worker，Gateway 只暴露 Worker API allowlist。

## 实施范围

- 实现 RemoteHTTPSWorkerClient 和 Remote Worker Gateway；
- 实现 mTLS principal 到允许部署/Agent 范围的绑定；
- 注册后签发短期 Worker Session Token，只保存可撤销摘要/标识；
- 每个请求联合校验 principal、token、instance、generation、lease 和 fencing；
- 支持注册、heartbeat、Mailbox/WorkerCommand long poll、Event 和 finish 的出站链路；
- 实现证书签发、轮换、吊销和失效运维基线；
- 复用与 UDS 相同的 API schema、错误码、幂等和 conformance suite。

## 目标代码边界

```text
OpenAgentX/internal/transport/remotehttps/
OpenAgentX/internal/auth/worker/
OpenAgentX/internal/api/worker/gateway.go
OpenAgentX/deploy/pki/
```

## 质量与验证

- 无证书、错误 CA、过期/吊销证书、越权 agent_id 和重放 token 请求均拒绝；
- 远程网络断开不丢 pending MailboxItem/WorkerCommand；
- NAT/防火墙后 Worker 只通过出站连接完成完整流程；
- UDS 与 HTTPS conformance 的状态结果完全一致；
- mTLS 私钥、Session Token 和完整 fencing token 不进入日志/Event。

## 实施结果

- 新增 `transport/remotehttps` mTLS TLS 1.3 配置加载与 HTTPS Worker client 构造。
- Worker API client 抽象出 HTTPS base URL，复用与 UDS 完全相同的注册、heartbeat、Mailbox、WorkerCommand、Event 和 finish 契约。
- 证书私钥仅进入 TLS 配置，不写入日志或 Event；非 HTTPS endpoint 直接拒绝。

## 退出条件

- 同一 Domain Agent 可从本机 Worker 迁移到远程 Worker，Task/SessionBinding 契约不变；
- Remote Gateway 不暴露 Observe/Control/Admin/Web 接口；
- 证书轮换和吊销 E2E 通过；
- 远程执行不要求 tmux 或 daemon 入站连接 Worker。

## 验证

详见 [Task 13 验证报告](../../reports/validation/2026-08-30-openagentx-task13-remote-worker-mtls.md)。
