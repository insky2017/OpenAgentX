---
doc_type: implementation_plan
status: active
owner: openagentx
updated_at: 2026-09-19
---

# OpenAgentX ADR-009 实施计划

## 1. 目标

按 [ADR-009](../decisions/ADR-009-pane-zero-task-console-observability.md) 分阶段把
`OAX:<agent-id>.0` 建成 Task-centric Console：从 dispatch 创建开始，连续显示排队、领取、运行、等待、
安全过程输出、终态和最终回复，并提供 focused steer/cancel 与同 pane Diagnostic 切换。

本计划不修改 ADR-006/007，不实现 Foreground Takeover、Runtime TTY、原始日志查看或 tmux 业务控制。

## 2. 开始条件与 P0

ADR-009 Task 01 开始前必须全部满足：

1. ADR-009 经监督明确接受，状态由 `proposed` 通过独立 docs-only 记录改为 `accepted`；
2. 复核 ADR-008 Task 08、后续修复/安装文档和独立现场 PASS 的事实，解决计划状态、分支提交链与实际
   已安装 revision 之间的差异；
3. 确认 ADR-008 的权威代码基线已经处于可追溯 Git ref，不能假设尚未包含其 35 个提交的
   `origin/main` 已具备 ADR-009 前置能力；
4. 不修改、不提交 `steadyflow` 父仓库，主工作树和现有 ADR-008 worktree 的未提交现场均得到解释；
5. 从监督确认的 ADR-008 基线创建独立 branch `codex/adr009-task-console` 和 sibling worktree，不直接在
   `main` 或 ADR-008 feature worktree 实施产品代码。

P0 只做基线收口和隔离，不夹带 Task 01 契约或产品改动。出现混合范围、失败或来源不明时立即停止。

## 3. 执行协议

1. 任务严格按 `01 -> 08` 顺序执行；前一任务监督门禁为 `GO` 前不得开始下一任务。
2. 每个 Task 开始时只把 execution log 对应项标为 `active/WAIT`；主计划和 Task front matter 保持
   `pending`，直到后续 gate record。
3. 每个 Task 只允许一个主要实现批次和一次独立验证批次。实现、定向测试和 append-only execution
   log 同批提交；独立验证集中执行，失败后回到当前 Task 集中修复，不启动重复审计或下一 Task。
4. P0/P1 阻塞当前 Task；P2 只有在不破坏当前验收时才可登记 owner 和后续阶段，不得自动扩大范围。
5. 监督复核通过后，用独立 docs-only gate record 写入精确实现 SHA、`GO`、实际 clean/ahead，并同步
   主计划/Task 状态；阶段提交无法自包含自身 SHA，不 amend 已复核提交。
6. 监督发现缺陷时回到归属 Task 创建独立 review-fix；保留失败记录，不在后续 Task 顺手修复。
7. 每关提交前核对冻结 ADR hash/等价 diff、工作树路径边界、计划状态、execution log 和验证报告一致性。
8. 默认不 push feature、不 merge main、不安装二进制、不重启服务、不迁移真实 DB、不操作真实
   socket/credential/default tmux/user-systemd；任何外部状态变更需要另行明确授权。
9. 测试使用临时 HOME/DB/UDS、唯一 `tmux -L` 和隔离进程。日志、fixture、报告和命令不得包含原始
   token、password、Secret、Cookie、完整 fencing token、隐藏推理或未脱敏 Runtime 输出。
10. Runtime、PTY、网络和部署类长测试按预定分钟级检查点等待，只在完成或明确错误时取证，不高频轮询。

## 4. 任务与依赖

| ID | 阶段 | 任务 | 依赖 | 状态 |
|---|---|---|---|---|
| 01 | G0 | [基线、能力盘点与契约冻结](2026-09-19-openagentx-adr-009-implementation/01-baseline-and-contract-freeze.md) | P0、ADR-009 Accepted | completed |
| 02 | G1 | [权威任务观察投影](2026-09-19-openagentx-adr-009-implementation/02-authoritative-task-observation-projection.md) | 01 | pending |
| 03 | G2 | [Runtime 安全输出与终态结果对齐](2026-09-19-openagentx-adr-009-implementation/03-runtime-output-and-terminal-result-alignment.md) | 02 | pending |
| 04 | G3 | [Task-centric Console reducer](2026-09-19-openagentx-adr-009-implementation/04-task-centric-console-reducer.md) | 03 | pending |
| 05 | G4 | [Pane 0 任务 TUI 与控制易用性](2026-09-19-openagentx-adr-009-implementation/05-pane-zero-task-tui-and-control-ergonomics.md) | 04 | pending |
| 06 | G5 | [同 pane Diagnostic 模式](2026-09-19-openagentx-adr-009-implementation/06-in-place-diagnostic-mode.md) | 05 | pending |
| 07 | G6 | [隔离用户闭环与操作文档](2026-09-19-openagentx-adr-009-implementation/07-isolated-user-workflow-and-documentation.md) | 06 | pending |
| 08 | G7 | [集成审查与候选门禁](2026-09-19-openagentx-adr-009-implementation/08-integration-review-and-release-gate.md) | 07 | pending |

持续记录：
[EXECUTION-LOG.md](2026-09-19-openagentx-adr-009-implementation/EXECUTION-LOG.md)。

## 5. 跨阶段不变量

- `dispatch succeeded`、`queued` 或 `task.created` 只表示创建成功，不表示 Worker 已执行或用户已收到回复；
- Task、Run、Worker generation、version 和 cursor 的一致性必须可由事务和失败注入证明；
- Console 的当前状态只来自共享 reducer；View 不执行 I/O，写操作只走正式 authenticated API；
- Task outcome、Runtime reply 和业务效果证据分层呈现，`uncertain` 不伪装成功；
- Normal 不显示 Diagnostic；Diagnostic 也只显示授权、限量、脱敏的安全投影；
- 关闭 Console/tmux/SSH 不影响 resident Worker 和已持久化 Task；
- tmux 只提供 `OAX` 稳定位置，不成为 Agent/Task/Worker 身份或控制通道；
- 默认路径主流程不要求重复 path flags，覆盖参数保持兼容且 fail closed。

## 6. 分层验证

每个阶段按风险运行定向测试；Task 08 至少覆盖：

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go mod verify
go build -o <isolated-cache>/openagentx-adr009-<git-sha> ./cmd/openagentx
bash scripts/check-legacy-control-paths.sh --release
bash -n scripts/*.sh deploy/scripts/*.sh deploy/systemd/*.sh
npm run test:observation
npm run test:pwa
npm run build
git diff --check
git status --short --branch
```

另外必须有临时 UDS/DB 的 Task 全生命周期、cursor 故障恢复、Normal/Diagnostic 权限边界，以及唯一
`tmux -L` + PTY 的小 pane 用户闭环。静态测试、入队成功或模型自报不能替代这些证据。

## 7. 状态维护规则

- 本文件已在 ADR 接受和 P0 基线隔离后改为 `active`；Task 01 仍需按独立阶段门禁开始。
- Task 实现提交后，Task 仍为 `active/WAIT`；监督 `GO` 后由下一次 docs-only gate record 改为
  `completed/GO`。
- Task 08 候选通过不表示已部署。push、merge、安装、服务重启、真实 DB 迁移和现场验收必须有单独授权
  与 provenance 记录。
