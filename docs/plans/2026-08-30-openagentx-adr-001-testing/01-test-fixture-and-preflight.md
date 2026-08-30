---
doc_type: test_task
status: active
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

## 当前基线

公开 bootstrap 入口尚未发现；当前 Worker 对不存在 Agent 注册返回 `500 INTERNAL_ERROR`。本任务在修复前为 BLOCKED。
