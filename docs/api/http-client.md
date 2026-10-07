# OpenAgentX Web HTTP 客户端契约

本页供远程客户端实现者使用。实际路由和字段以 `internal/api/auth`、`internal/api/panel`、`internal/api/control.go`、`internal/api/observe.go` 为准。`clients/oaxctl` 是此契约的单文件标准库实现。

## 入口与认证

当前外网入口为 `https://agentx.oneaxe.cn`。客户端应使用 HTTPS、固定自身的 `User-Agent`，并在写请求中发送与入口一致的 `Origin`。Cloudflare 对某些默认 Python User-Agent 可能返回拦截响应；具体策略由边缘配置决定。

| 请求 | 用途 | 响应要点 |
| --- | --- | --- |
| `POST /api/auth/v1/login` | JSON `username`、`password` 登录 | `Set-Cookie: openagentx_session=...`，JSON `csrf_token`、`principal`、到期时间 |
| `GET /api/auth/v1/session` | 检查当前会话 | 返回新的 `csrf_token`，旧值随轮换失效 |
| `POST /api/auth/v1/logout` | 注销当前会话 | 需 Cookie 和 `X-CSRF-Token`，成功返回 204 |

Cookie 用于认证，CSRF 令牌用于防跨站请求伪造；它们不是双因子认证。客户端不得将 Cookie、CSRF、密码写入日志、任务内容或 Git。`oaxctl` 使用私有 XDG 状态目录和 0600 文件保存 Cookie 与 CSRF。
客户端查询会话时会保存轮换后的 CSRF；终端输出只包含主体及到期时间。HTTP 重定向会被拒绝，避免认证或写入头随请求跳转。

## 观察接口

这些接口需要有效 Cookie，不需要 CSRF。

| 请求 | 主要响应 |
| --- | --- |
| `GET /api/observe/v1/agents` | Agent 对象数组；含 `agent_id`、`organization_id`、`display_name` |
| `GET /api/observe/v1/overview` | `agents`、`workers`、`tasks` 等总览投影 |
| `GET /api/observe/v1/tasks?limit=20&agent_id=...` | `tasks` 列表；每项含 `id`、`status`、`target_agent_id`、`summary` |
| `GET /api/observe/v1/tasks/{task_id}?after_sequence=0&limit=100` | `task`、`events`、`live_after_sequence`、`has_more_live_events` |

详情 `task.version` 是追加消息的并发版本。详情查询采用 `after_sequence` 时按事件序号增量读取；如 `has_more_live_events=true`，继续使用已收到的最后序号读取。等待中的 `waiting_input` 和审批状态不是任务成功。任务终态为 `succeeded`、`failed`、`canceled`、`uncertain`；只有 `succeeded` 可称执行成功，业务效果还要另行核实。

## 控制接口

写入时带会话 Cookie、`X-CSRF-Token`、`Origin` 和 `Idempotency-Key`。Header `Idempotency-Key` 必须与请求 JSON 的 `meta.idempotency_key` 完全一致。`GET /api/auth/v1/session` 会轮换 CSRF，写入前应保存最新响应。

创建任务：

```http
POST /api/control/v1/tasks
Content-Type: application/json
Origin: https://agentx.oneaxe.cn
X-CSRF-Token: <当前值>
Idempotency-Key: <稳定操作键>

{
  "meta": {"idempotency_key": "<稳定操作键>"},
  "target_agent_id": "pay-service",
  "organization_id": "default",
  "dispatch_mode": "direct",
  "intent": "query",
  "content": "<任务内容>"
}
```

响应包含 `task_id`、`task_version`、`task_status`、`sequence`。`intent` 可为 `query` 或 `mutation`，应与真实任务权限一致。

追加普通消息：

```http
POST /api/control/v1/tasks/{task_id}/messages
Content-Type: application/json
Origin: https://agentx.oneaxe.cn
X-CSRF-Token: <当前值>
Idempotency-Key: <稳定操作键>

{
  "meta": {
    "idempotency_key": "<稳定操作键>",
    "expected_version": 3
  },
  "content": "<补充内容>"
}
```

`expected_version` 必须为正数，并应来自刚读取的 `task.version`。追加普通消息不等于审批决定；审批有独立的 `/api/control/v1/approvals/{approvalID}/decisions` 接口。取消也有独立的 `/api/control/v1/tasks/{taskID}/cancel` 接口，必须发送相应请求体及幂等元数据。`oaxctl` 当前仅实现任务派发和普通消息追加，不暴露审批、取消命令。

## 重试与等待

弱网下写请求可能已经提交，但响应未送达。重试同一个逻辑操作时必须沿用**同一幂等键和完全相同的请求体**；不能更换键创建第二项任务。追加消息重试还必须沿用原 `expected_version`。`oaxctl` 不自动重试写请求；`--key` 可供人工安全重试，`say --key` 同时要求 `--expected-version`。调用方应保存首次使用的内容、键和版本。

`oaxctl dispatch --wait`、`oaxctl say --wait` 和 `oaxctl wait` 的 `--timeout` 只限制客户端等待时间，不会取消服务端任务，也不会重新派发。等待到时可用任务 ID 继续读取。`uncertain` 是明确终态，不能自动解释为成功。HTTP 错误、网络中断和无效 JSON 必须显式报错，不能据此推断写入失败或成功。
