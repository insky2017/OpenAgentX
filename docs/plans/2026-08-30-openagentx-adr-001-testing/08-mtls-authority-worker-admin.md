---
doc_type: test_task
status: pending
owner: openagentx
test_id: T08
updated_at: 2026-08-30
---

# T08：mTLS、组织权限与 Worker Admin

## 目标

验证远程 Worker 与本机 UDS 使用相同业务契约，并验证组织寻址、AuthorityPolicy 和 Worker 运维控制边界。

## 场景

- 同一 Worker API conformance suite 分别通过 UDS 和 mTLS HTTPS；
- 无证书、错误 CA、过期证书、证书 principal 与 Agent binding 不匹配全部拒绝；
- coordinated/direct dispatch 的允许和拒绝路径均写入审计；
- 业务输入只寻址逻辑 `agent_id`，不得寻址 worker_instance 或主机；
- drain、health-check、lease revoke、stop 只作用于目标 WorkerInstance；
- `stop` 正常退出不被 `Restart=on-failure` 自动拉起；
- 浏览器不能访问 Worker Control API 或读取 Worker Session Token。

## 通过条件

UDS/mTLS 状态转换一致，身份伪造全部 fail closed，Agent 业务 Mailbox 与 WorkerCommand 运维通道保持分离。
