---
doc_type: execution_log
status: pending
owner: openagentx
updated_at: 2026-09-19
---

# ADR-009 持续执行记录

> 本文件是 ADR-009 的 append-only 执行记录。它不预写成功结论。失败、纠正、未执行项和外部状态必须
> 保留；不得把单元测试、fixture、dispatch accepted、模型自报或静态文档检查扩大为真实 E2E。

## 1. Gate record 协议

1. ADR-009 未 Accepted、P0 未关闭时，所有 Task 保持 `pending/WAIT`。
2. Task 开始只更新本 log 为 `active/WAIT`；主计划和 Task front matter 在监督 gate 前保持 `pending`。
3. 每个 Task 一个主要实现批次和一次独立验证批次。阶段实现提交包含代码/测试/本 log，但无法包含自身
   SHA。
4. 监督 `GO` 后创建独立 docs-only gate record，记录精确实现/review-fix SHA、实际 clean/ahead，随后
   同步主计划、Task front matter 和本表为 `completed/GO`。
5. 监督 `NO-GO` 时回到归属 Task，创建独立 fix，不 amend 已复核提交；失败记录不得删除。
6. 每次提交前检查冻结 ADR hash、diff whitespace、relative links、staged paths、计划/log/report 状态一致。

允许 Task 状态：`pending`、`active`、`blocked`、`completed`。
监督门禁：`WAIT`、`GO`、`NO-GO`。

## 2. 建立时基线

记录时间：`2026-09-19`。

- 文档建立 worktree：`/home/sky/work/touzi/OneAxe/OpenAgentX-adr008-worktree`
- branch：`codex/adr008-implementation`
- 建立前 HEAD：`008b2e08923614182811023eddd652df69aa98a9`
- 建立前状态：clean，相对 `origin/main` ahead 35
- 已安装代码 revision（来自 2026-09-18 独立现场复测）：
  `f49cec4ed31a0f63e82626f1de8d6332baca205d`
- 独立复测报告：`/tmp/openagentx-agy-retest-20260918.md`，结论 PASS；该报告是既有现场证据，本轮未
  重启、重测或修改现场。
- ADR-008 validation report 已为 `passed-candidate`，但 ADR-008 主计划/Task 08 front matter 和
  execution log 仍保留 `pending`/`active-WAIT` 状态；后续又存在代码、迁移和安装文档提交。
- `origin/main` 尚未包含当前 worktree 的 35 个提交。ADR-009 不得直接从旧 `origin/main` 假定拥有
  ADR-008 前置能力。

以上差异属于 P0。未经监督确认不得在当前 ADR-008 worktree 上开始 ADR-009 产品实现，也不得自行
push/merge/安装来“修正”差异。

## 3. 总体状态

| 阶段 | 名称 | 状态 | 实现提交 | 监督门禁 |
|---|---|---|---|---|
| P0 | ADR-008 基线、状态和 Git lineage 收口 | pending | - | WAIT |
| 01 | 基线、能力盘点与契约冻结 | pending | - | WAIT |
| 02 | 权威任务观察投影 | pending | - | WAIT |
| 03 | Runtime 安全输出与终态结果对齐 | pending | - | WAIT |
| 04 | Task-centric Console reducer | pending | - | WAIT |
| 05 | Pane 0 任务 TUI 与控制易用性 | pending | - | WAIT |
| 06 | 同 pane Diagnostic 模式 | pending | - | WAIT |
| 07 | 隔离用户闭环与操作文档 | pending | - | WAIT |
| 08 | 集成审查与候选门禁 | pending | - | WAIT |

## 4. 冻结范围摘要

| 主题 | 决策边界 | 首次 owner |
|---|---|---|
| Task 观察 | Agent-scoped 窄投影、一致 snapshot/high-water、分页 active/recent Task | Task 02 |
| Runtime 输出 | Adapter 实际能力、Web/terminal 共用 safeoutput、最终 reply 与 Task outcome 分层 | Task 03 |
| Console 状态 | reducer 唯一真相、Task/Run/Worker/version/cursor fencing、显式 ack | Task 04 |
| 任务交互 | dispatch 自动 focus、完整 ID/version、focused steer/cancel、CAS 不自动重试 | Task 05 |
| Diagnostic | 同 pane 正式重 Attach/Follow、owner+scope、失败保持 Normal | Task 06 |
| 用户流程 | 默认路径、OAX pane 0、从 dispatch 到最终回复、退出不影响 Worker | Task 07 |
| 候选 | 全量 traceability、无缓存验证、隔离 E2E、独立候选 provenance | Task 08 |

明确排除：ADR-006/007 产品语义、Foreground Takeover、Runtime TTY、raw stderr/hidden reasoning、tmux
业务控制、直接 TurnHandle、真实部署和未授权外部状态变更。

## 5. Open issues

| ID | 严重度 | 问题 | owner | 当前状态 |
|---|---|---|---|---|
| A09-01 | P0 | ADR-008 计划状态、35 提交 Git lineage、现场安装 revision 和后续 docs/code 提交尚未形成单一监督确认基线 | P0 | open；阻塞 Task 01 |
| A09-02 | P1 | 各正式 Runtime/Adapter 是否提供增量输出、最终 body/error 及其解析边界尚需按实际版本冻结 | Task 01 | pending；阻塞超出实测能力的实现 |
| A09-03 | P1 | Console Task projection 的精确 DTO、容量上限和 snapshot 事务字段尚未冻结 | Task 01 | pending；阻塞 Task 02 |

## 6. 事件记录（append-only）

| 日期 | 事件 | 影响 | 处理/结论 |
|---|---|---|---|
| 2026-09-19 | 用户反馈 pane 0 只能看到 `dispatch succeeded`/`task.created`，看不到任务执行到哪里和最终回复；`/steer` 需手工 ID/version，`/diagnostic` 只提示重新 Attach | 当前 Console 符合安全控制入口，但未达到日常 Task 工作台目标 | 提出 ADR-009；不把 queued 误报为执行，不把 Diagnostic 定义成 raw Runtime TTY |
| 2026-09-19 | 创建 ADR-009 与 8 阶段计划 | 只产生决策/计划文档，不改变产品行为或外部状态 | ADR 保持 Proposed；所有 Task `pending/WAIT`，等待接受和 P0 授权 |
| 2026-09-19 | 首次文档相对链接检查调用 `ruby` 失败（目标机未安装）；首次新文件 whitespace 脚本把多行路径合成一个参数 | 两项命令未产生有效检查结果，未修改文件或外部状态 | 改用仓库现有 Node 做只读链接检查，并用显式 zsh 数组逐文件执行 `git diff --no-index --check`；12 个文档链接和全部新文件 whitespace 检查通过 |

## 7. 后续记录模板

每个 Task 开始后追加以下内容，不覆盖前文：

```text
### Task NN
- 开始时间 / 执行者 / baseline / branch / worktree
- 冻结 ADR hash 与范围边界
- 验收矩阵：正向、状态/CAS/幂等、失败、竞态、资源、证据格式
- 主要实现批次：文件、决策、迁移/兼容、安全边界
- 独立验证批次：命令、退出码、耗时、环境、结果
- 失败与纠正：现象、根因、安全影响、修复 SHA、重验
- 外部状态：未操作项或经授权操作的 provenance
- open issues：P0/P1/P2、owner、是否阻塞
- 阶段提交与 post-commit status
```
