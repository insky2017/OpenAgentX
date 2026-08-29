---
doc_type: implementation_task
status: pending
owner: openagentx
updated_at: 2026-08-30
---

# 任务 15：Web Auth、HTTP API 与 SSE

## 目标

实现 OpenAgentX 指挥台后端的密码认证、Web Session、RBAC、CSRF、Observe/Control/Admin API 和基于 Event Journal 的可重连 SSE。

## 依赖与入口

- 依赖：任务 14 / G4；
- 入口：Organization/AuthorityPolicy、Command Service、Admin service 和 read model；
- 浏览器不能访问 Worker Control API 或 Remote Worker Gateway。

## 实施范围

- 实现本机交互式 owner 创建/改密流程，不提供默认密码或公开注册；
- 使用 Argon2id + 随机 salt 保存密码摘要；
- 实现高熵不透明 Web Session、摘要存储、idle/absolute timeout 和立即撤销；
- 实现 `owner/operator/viewer` RBAC，并叠加 Organization AuthorityPolicy；
- 实现 CSRF token、`Idempotency-Key`、version/sequence 前置条件和控制审计；
- 实现 Auth、Observe、Control、Admin API；
- 实现 Event Journal `after_sequence`/`Last-Event-ID` SSE replay 和 read model 更新；
- 实现 forwarded header allowlist 和应用层登录限速。

## 目标代码边界

```text
OpenAgentX/internal/auth/web/
OpenAgentX/internal/api/auth/
OpenAgentX/internal/api/observe/
OpenAgentX/internal/api/control/
OpenAgentX/internal/api/admin/
OpenAgentX/internal/transport/sse/
```

## 质量与验证

- Cookie 固定 `Secure; HttpOnly; SameSite=Strict`；
- 用户不存在和密码错误返回不可区分响应；
- viewer 写入、operator 越权、非 owner Admin、无效 CSRF/幂等键均拒绝并审计；
- 网络重试使用同一 Idempotency-Key 只创建一份业务事实；
- SSE 断线重连按 sequence 回放，客户端重复应用安全；
- API 不返回密码摘要、token、私钥、完整 fencing、环境变量或隐藏推理。

## 退出条件

- 未登录、过期、撤销 Session 无法访问页面/API/SSE；
- CLI/MCP/Web 写入共用同一 Command Service；
- Observe API 保持只读，Admin API 不能创建业务 Task；
- Web 后端安全负向测试通过。
