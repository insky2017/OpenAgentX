---
doc_type: decision
status: proposed
canonical: true
owner: openagentx
updated_at: 2026-09-14
---

# ADR-007: Worker 网络绑定的代际连续性

## 状态

提议中 (Proposed)，未授权实施。

## 日期与决策者

- 日期：2026-09-14
- 决策者 / 所有者：OpenAgentX Team / OpenAgentX Maintainer

## 背景

网络策略当前与 WorkerInstanceID 和 generation 绑定，以防止 Runtime 网络能力在 Worker 重启、配置变化或二进制变化后被静默继承。常规维护造成 Worker generation 变化时，旧绑定可能令 Backend 暂时不可用并要求重新验证。

早期 ADR-005 草案提议让 `inherit` 网络模式自动跨 generation 对齐。该行为涉及授权边界和网络安全，不属于 Console/Fleet 开发者体验本身，因此拆为独立 ADR。

## 提议方向

探索按网络策略风险分级的连续性规则：

- `named_profile` 或包含 Secret、专属代理、隧道的模式继续要求显式验证和应用；
- `inherit` 仅在能够证明是同一受信宿主、同一 Agent、兼容 Runtime 身份且策略内容未变化时，才考虑自动延续；
- 自动延续必须在控制面事务中记录来源 generation、目标 generation、判定依据和审计事件；
- 无法证明连续性时保持 fail closed，由操作者重新测试并应用。

## 待决定问题

1. 当前是否存在足够稳定且可认证的 host identity；
2. Runtime binary、Adapter 版本、网络环境或 Worker 配置变化时，哪些变化必须使继承失效；
3. `inherit` 是否真的低风险，以及透明代理、环境变量和宿主路由变化如何检测；
4. 正常重启、崩溃恢复、主机迁移和 Fleet 配置变更如何区分；
5. 自动继承失败时的 Backend 状态、告警和恢复流程；
6. 回滚到旧 generation 时是否允许复用历史绑定。

## 不变量

- 不能仅凭相同 `agent_id` 自动继承网络授权；
- 不能把 tmux window、systemd unit 名或本地路径当作受信 host identity；
- 涉及 Secret 或命名代理的绑定不得静默滚动；
- 所有自动对齐必须可审计、可解释并保持 fail closed；
- 在本 ADR 接受前，现有 generation fencing 不得弱化。

## 实施状态

本 ADR 仅记录候选方向。必须先补足 host identity、威胁模型、状态转换和故障恢复设计，再决定是否接受和实施。

## 来源

本提议从 [ADR-005](ADR-005-interactive-worker-console-and-developer-experience.md) 的早期混合草案中拆出。
