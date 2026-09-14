---
doc_type: decision
status: proposed
canonical: true
owner: openagentx
updated_at: 2026-09-14
---

# ADR-006: Task intent 与可验证终态语义

## 状态

提议中 (Proposed)，未授权实施。

## 日期与决策者

- 日期：2026-09-14
- 决策者 / 所有者：OpenAgentX Team / OpenAgentX Maintainer

## 背景

当前零信任副作用语义要求模型自报成功之外还要有可验证的业务效果。这对文件、数据库和外部系统变更十分重要，但对问答、分析、巡检等只读任务可能产生 `uncertain / business_effect_unverified` 假阳性。

该问题曾与终端 Console 混写在 ADR-005 草案中。它会改变 Task 领域模型和终态安全语义，因此拆为独立 ADR，不能由 Console 的实现顺带修改。

## 提议方向

考虑为 Task 引入显式 intent：

- `mutation`：预期产生外部副作用，继续要求可验证的业务效果；
- `query`：预期只读，在 Runtime 成功且没有其他错误时，可以依据完整、可持久化的结果证据进入成功终态。

安全默认值应保持为 `mutation`。不能仅依赖模型自行判断任务是只读还是变更类，也不能通过提示文本关键词自动降低验证要求。

## 待决定问题

1. intent 由谁声明：调用者、策略引擎、Agent identity，还是它们的组合；
2. `query` 的成功证据是什么，以及空输出、截断输出和不可验证引用如何处理；
3. 查询过程中发生工具副作用时，是否自动升级为 `mutation` 或转入 `uncertain`；
4. API、数据库和旧 Task 的迁移及默认语义；
5. intent 是否允许在 Task 创建后改变，以及需要什么审计记录；
6. `waiting_input`、失败、取消和 Runtime `uncertain` 与 intent 的组合规则。

## 不变量

- `query` 不能成为绕过副作用审计的开关；
- mutation 的现有保守语义在本 ADR 接受前不得弱化；
- Runtime 执行成功不自动等价于业务变更成功；
- 终态必须能够从持久化事实重建，不能只依赖 Worker 内存或 UI 推断。

## 实施状态

本 ADR 仅保存问题、候选方向和安全边界。需要单独完成领域模型、迁移、状态转换矩阵和测试设计，并经接受后才能实施。

## 来源

本提议从 [ADR-005](ADR-005-interactive-worker-console-and-developer-experience.md) 的早期混合草案中拆出。
