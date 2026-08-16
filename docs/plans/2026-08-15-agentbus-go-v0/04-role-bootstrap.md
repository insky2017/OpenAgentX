---
doc_type: implementation_task
status: completed
completed_at: 2026-08-16
---

# Task 04：Agent 角色 Bootstrap 生命周期

## 目标

让 Orchestrator 和所有南向 Agent 不依赖一次性人工提示，而是在每个受管 session 中从 canonical manifest/ROLE 恢复身份，并在 ready 前禁止参与 Task。

## 实施内容

- 新增 `agents/<agent-id>/agent.yaml` 与 `ROLE.md`；
- 新增 Profile、Session generation 与 `bootstrapping/ready/delivery_failed` 状态；
- 实现 `agent whoami/attach/bootstrap/launch`；
- 实现 `session ready/show` 与 generation compare-and-set；
- 所有 Task 操作在 Store transaction 内检查参与者 ready；
- Bootstrap、Task 与 supplement 通知携带最小身份提醒及 ROLE 路径；
- generic launcher 只接受 direct argv，attach 未完成或投递未成功时终止子进程并返回失败。

## 审核重点

- 不以旧版兼容性作为约束，直接审核当前生命周期不变量；
- delivery failure 不得重复增加 generation，也不得伪造数据库已更新；
- ready 检查必须与 Task 写入处于同一事务，避免 re-bootstrap 竞态；
- `launch` 必须位于 tmux、地址一致、attach 成功且 disposition 精确为 `notified`；
- daemon API 不能注册不可读的 ROLE 文件；
- TmuxConnector 不注入 Task 正文，不从 pane 输出推断完成。

## 验收结果

- `gofmt`、`go vet`、普通测试、race test、build、`git diff --check` 全部通过；
- Orchestrator、AgentBus Agent、Quote Service Agent 均 attach/ready 成功；
- Quote Service Agent re-bootstrap 到 generation 2 后，未 ready submit 返回 HTTP 409 `AGENT_NOT_READY`；
- 再次 ready 后任务恢复成功；
- daemon 重启后三个 Profile/Session 和验收 Task 持久可读。

证据见 [2026-08-16-agent-role-bootstrap-e2e.md](../../reports/validation/2026-08-16-agent-role-bootstrap-e2e.md)。
