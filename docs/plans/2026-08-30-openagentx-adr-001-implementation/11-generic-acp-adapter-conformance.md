---
doc_type: implementation_task
status: completed
owner: openagentx
updated_at: 2026-08-30
---

# 任务 11：Generic ACP Adapter 与 Conformance

## 目标

实现 Generic ACP Adapter，并通过 descriptor 将 Codex、Claude Code 和 OpenCode 的协议差异映射到统一 StartTurn/TurnHandle/Event/TurnResult 契约。

## 依赖与入口

- 依赖：任务 10；
- 入口：Adapter Registry、ExecutionSpec 和 AGY conformance 基线；
- 退出门槛：G3。

## 实施范围

- 实现 ACP 初始化、capability handshake、session new/resume、prompt 和 stream 生命周期；
- 映射 permission/tool/usage/cancel 和 provider 扩展 metadata；
- 仅在握手与 descriptor 同时确认时声明 native steer/approval/cancel；
- 为 Codex、Claude Code、OpenCode 提供版本化 descriptor，不复制核心 Adapter 状态机；
- 实现 fake ACP server，覆盖乱序事件、断流、重复响应和协议错误；
- 运行 AGY 与 ACP 共用的 Adapter conformance suite；
- 对可用真实 Backend 执行受控 smoke/E2E。

## 目标代码边界

```text
OpenAgentX/internal/runtime/acp/
OpenAgentX/internal/runtime/conformance/
OpenAgentX/internal/runtime/descriptors/
```

## 质量与验证

- `StartTurn` 返回后 `Wait` 与控制操作可并发；
- 不支持能力返回稳定错误，不能伪造成功；
- provider session 失效、协议断流和取消超时具有标准退出分类；
- provider metadata namespaced 保存，不污染核心领域枚举；
- conformance 同时覆盖 AGY、fake ACP 和已启用真实 ACP Backend。

## 实施结果

- 新增 `runtime/acp` Generic ACP Adapter，统一处理结构化 session prompt、JSON 事件流、provider session id、断流/非零退出和取消信号。
- 新增 Codex、Claude Code、OpenCode 版本化 descriptor catalog；核心 Worker 只消费统一 `AgentRuntimeAdapter` 能力描述。
- 新增 `runtime/conformance` 共享 descriptor/ExecutionSpec 校验入口，AGY、ACP 和后续 Backend 可复用同一契约。
- ACP 不支持的 steer/approval 能力返回稳定错误，不伪造 native 成功。

## 退出条件

- 至少一个 ACP Backend 完成真实 turn 闭环；
- AGY/ACP 在核心 Worker 看来只有 descriptor 能力差异，没有分叉调度逻辑；
- capability 与 ExecutionSpec 验证一致；
- G3 验收报告完成。

## 验证

详见 [Task 11 验证报告](../../reports/validation/2026-08-30-openagentx-task11-acp-conformance.md)。
