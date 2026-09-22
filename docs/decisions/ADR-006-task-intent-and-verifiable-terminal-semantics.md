---
doc_type: decision
status: accepted
canonical: true
owner: openagentx
updated_at: 2026-09-23
---

# ADR-006: Task intent 与可验证终态语义

## 状态

已接受 (Accepted)。2026-09-23 用户接受主代理推荐的结果交付合同，并授权按目标架构实现、补充文档。
实施中，尚未完成候选安装与真实验收。

## 日期与决策者

- 日期：2026-09-14
- 决策者 / 所有者：OpenAgentX Team / OpenAgentX Maintainer

## 背景

当前零信任副作用语义要求模型自报成功之外还要有可验证的业务效果。这对文件、数据库和外部系统变更十分重要，但对问答、分析、巡检等只读任务可能产生 `uncertain / business_effect_unverified` 假阳性。

该问题曾与终端 Console 混写在 ADR-005 草案中。它会改变 Task 领域模型和终态安全语义，因此拆为独立 ADR，不能由 Console 的实现顺带修改。

## 决策

Task 使用创建者显式声明且创建后不可变的 intent：

- `mutation`：预期产生外部副作用，继续要求可验证的业务效果；
- `query`：交付物为回复。唯一有效成功终态的最终正文非空、未截断，且解析、进程、事件持久化均无错误时，
  Task 可进入 succeeded，持久化 `completion_basis=query_result_delivered`。

安全默认值应保持为 `mutation`。不能仅依赖模型自行判断任务是只读还是变更类，也不能通过提示文本关键词自动降低验证要求。

## 成功依据与安全边界

1. 只有原有鉴权通过的任务创建者可声明类型；权限、scope、审批和审计不随类型改变。
2. query succeeded 只证明完整回复交付，不证明答案真实、引用可靠、全程只读或业务副作用已核验。
   调用者即使将写操作标为 query，也不能据此声称其业务效果已验证。要求只读隔离时须另有可信执行限制。
3. Adapter 的 `final_reply` 表示协议层完整最终回复证据。增量文本不替代最终正文，模型自报不生成证据。
   当前实现支持 AGY stream-json；未提供同等证据的 Adapter 不能让 query 成功。
4. mutation 保留既有 SideEffectsKnown 保守规则；满足时记录 `mutation_effects_known`，否则保持
   uncertain/business_effect_unverified。该依据不冒充独立业务核验，不强制 SideEffectsKnown=true。
5. 空/截断/矛盾/缺少最终回复的 query 保持 uncertain/query_result_unverified；失败、取消、等待和
   Runtime uncertain 不因 intent 提升为成功。cancel/pending message 优先级、CAS/幂等与事务不变。
6. Task/Run/Journal 在同一事务结算，Task 持久化成功依据，非成功状态不保留成功依据。
   API、Console、Web只呈现该事实，不从回复正文或 Run succeeded 推测 Task 成功。

## 存储和协议

采用 schema v2。空库直接建立目标结构，当前完整 v1 单事务前向升级；历史 Task 默认 mutation、
成功依据为空，原状态、结果、错误不重算、不自动重试。保留历史数据不等于维护旧协议 fallback。
新增查询任务必须通过当前协议显式声明，省略类型仍是产品安全默认 mutation；非法类型拒绝。

## 不变量

- `query` 不关闭权限、审批或副作用审计；回复交付成功不得描述为副作用已核验或没有副作用；
- mutation 的现有保守语义不得弱化；
- Runtime 执行成功不自动等价于业务变更成功；
- 终态必须能够从持久化事实重建，不能只依赖 Worker 内存或 UI 推断。

## 实施状态

类型/API/存储和入口基础接线已独立提交；终态证据与呈现按本合同继续实施。
阶段和验收记录见[实施计划](../plans/2026-09-22-openagentx-adr-006-implementation-plan.md)。

## 来源

本提议从 [ADR-005](ADR-005-interactive-worker-console-and-developer-experience.md) 的早期混合草案中拆出。
