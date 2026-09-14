# OpenAgentX ADR-001 最终验证执行账本

## Goal

以当前仓库与运行环境为事实源，复核 ADR-001 的 20 个实施任务及其提交证据，完成冻结后的分项实机测试、缺陷修复、逐项提交和最终全量验收，直至系统满足 ADR-001；ADR-001 本身不得修改。

## Current Phase

Phase 3：按测试计划逐项完成剩余实战测试（下一关 T05）

## Phases

### Phase 1：恢复实施与验证基线

- [x] 确认 20 个实施任务文档状态和对应提交链
- [x] 确认 ADR-001 保持冻结
- [x] 确认当前服务、工作区和 T04 已知失败证据
- **Status:** complete

### Phase 2：完成 T04 AGY 实机闭环

- [x] 修复 Worker Control 伪 long poll、高 CPU、Broker wakeup 与鉴权/lease fail-closed
- [x] 审计并完成 `agy-graft` Adapter 契约修复
- [x] 运行相关最小测试、全量 Go 测试和静态检查
- [x] 构建并重启 daemon/Worker，执行 Task A/Task B 实机验证
- [x] 形成经验证的 T04 修复与证据提交边界
- **Status:** complete

### Phase 3：按测试计划逐项完成剩余实战测试

- [ ] 每项先设计一次性验证任务与证据要求
- [ ] 由主 Agent 与 Codex subagent 执行代码、测试和审计工作
- [ ] 仅在验证 OpenAgentX 外部 Runtime 调用能力时，将 WorkBuddy/CodeBuddy 作为最小黑盒被测对象
- [ ] Codex subagent 限定使用 `sol medium` 或 `terra high`
- [ ] 每项审计通过后单独提交，再进入下一项
- **Status:** in_progress

### Phase 4：全量验收与缺陷闭环

- [ ] 对 ADR-001、20 个任务文档、测试计划逐项建立证据矩阵
- [ ] 执行全量测试、安全检查、真实浏览器验证和部署链路验证
- [ ] 修复所有阻断缺陷并分别验证、提交
- **Status:** pending

### Phase 5：完成审计与交付

- [ ] 确认工作区仅保留用户或明确说明的并行改动
- [ ] 确认 20 项要求均有直接、充分、可复查证据
- [ ] 汇总提交、验证结果和剩余风险
- **Status:** pending

## Decisions Made

| Decision | Rationale |
|---|---|
| 不重做已经有提交证据的 20 个实施任务 | 当前主计划和提交历史显示 20 项已落地；工作重点是验证真实性和修复缺陷 |
| ADR-001 只读冻结 | 用户明确要求，新增架构决策另建 ADR |
| T04 从 AGY CLI 契约修复继续 | 两次实机任务均快速进入 `uncertain`，且已有直接契约诊断 |
| 代码、测试和审计不再委派给 WorkBuddy/CodeBuddy | 用户明确要求；其只可作为 OpenAgentX Runtime 黑盒测试对象 |

## Errors Encountered

| Error | Attempt | Resolution |
|---|---:|---|
| `sol medium` subagent 在 AGY 修复过程中收到 429 并超过重试限制 | 1 | 保留其工作区改动，先审计现状，再由 `terra high` 接管剩余工作 |
| Worker Control `wait=30s` 被服务端忽略，daemon/Worker 空闲高 CPU | 1 | 已完成只读根因定位，交由 `sol medium` 实施真正 long poll 和 Broker wakeup |

## Constraints

- WorkBuddy/CodeBuddy 仅可用于 OpenAgentX Runtime 功能本身的最小黑盒验证；若使用，只能选择免费模型 `hy4-preview`。
- AGY 命令必须使用 `agy-graft`，参数保持对应契约。
- AGY 实机运行使用 `set_proxy_server` 环境；不得把代理凭据写入仓库、日志或提交。
- 不回滚或覆盖工作区中的用户并行改动。
- 涉及前端时必须使用真实浏览器完成 DOM、渲染与交互验证。
