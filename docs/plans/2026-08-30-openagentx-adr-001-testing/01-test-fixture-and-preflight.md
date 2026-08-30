---
doc_type: test_task
status: completed
owner: openagentx
test_id: T01
updated_at: 2026-08-30
---

# T01：测试准备、身份初始化与环境门槛

## 目标

通过受支持的 OpenAgentX 管理入口建立 owner、Organization、逻辑 Agent、AgentProfile 和测试 Worker 配置，使后续测试不依赖直接写 SQLite。

## 步骤

1. 备份并记录当前目标库 hash；破坏性测试另建隔离数据库。
2. 验证 `openagentx schema verify`、daemon health、UDS 权限和 HTTPS。
3. 通过正式 Admin CLI/API 创建测试 Organization、`test-fake-agent`、`quote-service` 和所需 Principal/Profile。
4. 重复相同幂等请求，确认不产生重复身份或 Event。
5. 使用不存在 Agent 启动 Worker，预期明确返回 404/冲突类错误而不是 500。
6. 使用合法 Agent 启动 Fake Worker，确认注册、generation、lease、fencing 和 online 状态。

## 通过条件

- 全部身份通过公开入口创建并出现在 Observe API；
- Event Journal 包含创建事实且无敏感字段；
- Worker 注册错误可解释，合法 Worker 可 online；
- 没有直接数据库写入或测试专用后门。

## 验收结果

- `openagentx init` 已通过本机交互式隐藏密码输入创建 owner、`default` Organization 和 daemon system principal；daemon 不再使用默认密码或环境变量构造内存 owner。
- `openagentx agent apply` 已创建 `quote-service` 与 `test-fake-agent` 的 Principal、AgentIdentity 和 AgentProfile；重复 apply 后 Event 数量保持不变。
- 初始化与 Agent apply 的事务回滚、owner 负向认证和 Event 敏感字段检查均有自动化测试。
- 未知 Agent 的 Worker 注册返回 `404 NOT_FOUND`；合法 Fake Worker 为 `online`，generation、lease、fencing token 和 Backend health 均有效。
- daemon、UDS `0600`、本地 HTTP、远程 HTTPS/TLS 和 owner 登录均通过。

证据见 [T01 验证报告](../../reports/validation/2026-08-30-openagentx-adr001-t01-environment-identity.md)。
