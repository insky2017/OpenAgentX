---
doc_type: implementation_task
status: completed
owner: openagentx
updated_at: 2026-08-30
---

# 任务 10：ExecutionSpec 与 Adapter Registry

## 目标

实现类型安全的 Adapter Registry、capability descriptor 和分层 ExecutionSpec 解析，使 Backend、模型、reasoning、权限和 session 选择可授权、可验证、可审计。

## 依赖与入口

- 依赖：任务 09 / G2；
- 入口：稳定 RunAttempt、SessionBinding 和 Runtime Adapter conformance；
- 不使用 Go 动态 plugin，不接受 raw argv。

## 实施范围

- 定义 Adapter descriptor、Backend registration、模型目录和 reasoning schema；
- 定义 `steer/approval/cancel/session` 的固定能力语义；
- 实现 Adapter defaults、Agent Profile、Task/turn override 和 policy hard limit 的解析顺序；
- 使用版本化 JSON Schema 校验 namespaced `backend_options`；
- 把 requested/resolved spec、来源、解析版本和最终字段完整写入 RunAttempt；
- Scheduler 只从健康 Worker 实际上报且授权允许的组合中选择；
- 为 CLI/API/Web 提供结构化 execution options read model。

## 目标代码边界

```text
OpenAgentX/internal/runtime/registry/
OpenAgentX/internal/runtime/spec/
OpenAgentX/internal/domain/execution_profile.go
OpenAgentX/internal/controlplane/execution_resolver.go
```

## 质量与验证

- 不存在的 adapter/model/reasoning、未知 option、超权限 sandbox/network/budget 明确失败；
- 不静默切换 Backend、模型或 reasoning；
- `steer=unsupported` 只表示 native 和 deferred follow-up 都不成立；
- ResolvedExecutionSpec 可从持久记录解释每个字段来源；
- 用户输入不能形成 shell fragment、任意环境变量或未声明参数。

## 实施结果

- 新增类型安全的 `runtime/registry`，以 Adapter descriptor + Backend registration 组成可验证目录，拒绝重复或不匹配注册。
- 新增 `runtime/spec` 分层解析器，按请求覆盖 profile/default，并记录字段来源；对 Adapter、Backend、model、reasoning、session、timeout、budget 执行 capability 与 policy hard limit 校验。
- Resolver 输出 `ResolvedExecutionSpec`，可直接写入 RunAttempt 的 requested/resolved JSON，不接受 raw argv 或任意未声明组合。

## 退出条件

- AGY descriptor 迁移到 Registry 并通过 resolver/conformance；
- 同一 Task 的不同 turn 可在授权范围内解析不同 spec；
- Backend 切换强制新 SessionBinding；
- API 可稳定列出目标 Agent 的合法 execution options。

## 验证

详见 [Task 10 验证报告](../../reports/validation/2026-08-30-openagentx-task10-execution-spec-registry.md)。
