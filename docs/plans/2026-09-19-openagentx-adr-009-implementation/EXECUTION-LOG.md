---
doc_type: execution_log
status: active
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
| P0 | ADR-008 基线、状态和 Git lineage 收口 | completed | `dad6c40`, `484db18` | GO |
| 01 | 基线、能力盘点与契约冻结 | active | - | WAIT |
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
| A09-01 | P0 | ADR-008 计划状态、35 提交 Git lineage、现场安装 revision 和后续 docs/code 提交尚未形成单一监督确认基线 | P0 | closed；选择 `008b2e0` 作为已复测 ADR-008 tip，以 `484db18` 引入本地 main 的最新治理规则；不改写历史计划状态 |
| A09-02 | P1 | 各正式 Runtime/Adapter 是否提供增量输出、最终 body/error 及其解析边界尚需按实际版本冻结 | Task 01 | closed；AGY 1.2.7、CodeBuddy 2.143.0、ACP repository contract 与 Adapter fixture 已记录，未实测能力不进入契约 |
| A09-03 | P1 | Console Task projection 的精确 DTO、容量上限和 snapshot 事务字段尚未冻结 | Task 01 | closed；Console Task list/detail/suggested snapshot、SSE DTO、单事务、4 KiB/256 event/Timeline 上限已冻结 |

## 6. 事件记录（append-only）

| 日期 | 事件 | 影响 | 处理/结论 |
|---|---|---|---|
| 2026-09-19 | 用户反馈 pane 0 只能看到 `dispatch succeeded`/`task.created`，看不到任务执行到哪里和最终回复；`/steer` 需手工 ID/version，`/diagnostic` 只提示重新 Attach | 当前 Console 符合安全控制入口，但未达到日常 Task 工作台目标 | 提出 ADR-009；不把 queued 误报为执行，不把 Diagnostic 定义成 raw Runtime TTY |
| 2026-09-19 | 创建 ADR-009 与 8 阶段计划 | 只产生决策/计划文档，不改变产品行为或外部状态 | ADR 保持 Proposed；所有 Task `pending/WAIT`，等待接受和 P0 授权 |
| 2026-09-19 | 首次文档相对链接检查调用 `ruby` 失败（目标机未安装）；首次新文件 whitespace 脚本把多行路径合成一个参数 | 两项命令未产生有效检查结果，未修改文件或外部状态 | 改用仓库现有 Node 做只读链接检查，并用显式 zsh 数组逐文件执行 `git diff --no-index --check`；12 个文档链接和全部新文件 whitespace 检查通过 |
| 2026-09-19 | 用户明确要求执行 ADR-009 直到完成，并要求中间证据和实际 E2E | ADR-009 获得接受与连续执行授权；仍保留逐阶段实现/验证/gate 提交 | ADR 状态改为 Accepted；不把全局授权扩大为 push、部署或真实状态变更 |
| 2026-09-19 | P0 核验 ADR-008/主工作树/远端 lineage | ADR-008 worktree 为 clean `008b2e0`、相对 `origin/main@c3fc1bb` ahead 35；本地 main 为 clean `3723c77`、ahead 1，提交只修改 `AGENTS.md`；独立现场报告证明安装代码 `f49cec4` 和 `008b2e0` docs tip PASS | 在 `008b2e0` 创建 `codex/adr009-task-console`；`dad6c40` 保存 ADR/计划，`484db18` 引入最新治理；新 sibling worktree `/home/sky/work/touzi/OneAxe/OpenAgentX-adr009-worktree`，原 ADR-008/main/远端/运行现场未修改 |
| 2026-09-19 | Task 01 首轮代码定位读取了不存在的 `internal/domain/event.go` | 仅只读命令返回错误；Journal 模型是否存在尚未据此判断 | 用 `rg 'type JournalEvent'` 定位到 `internal/domain/journal_contract.go`，确认结构化 Journal contract 存在；停止使用猜测文件名 |

#### Task 01 能力盘点与决策

- 当前正式链路已覆盖 CreateTask 事务、work Mailbox claim/accept、BeginAttempt、Runtime EventSink、
  FinishRun 和 Journal；缺口集中在 Console Task projection/reducer/TUI，不需要新 Worker 私有协议。
- 已安装 `agy-graft --version/--help` 只读返回 `1.2.7`，支持 stream-json、conversation、model、effort 和
  print timeout；`codebuddy --version/--help` 返回 `2.143.0`，支持 print、text/json/stream-json、partial
  messages、model/effort/max-turns。命令未执行 turn、未读取 credential 或修改 Runtime 状态。
- AGY Adapter 已用 stream-json，CodeBuddy Adapter 当前故意用 text stdout 并按完整行产生安全 event；
  两者 `Streams=true`，但运行中 native steer/approval 均不承诺，UI 必须按 queued/preflight 事实表达。
- 冻结 Console Task API、Task snapshot 单事务、SSE Task/Mailbox/Message/Approval 安全投影、Task version
  reducer、focused CAS、Diagnostic mode-switch 和容量上限；详见
  [TASK-01-CONTRACT-FREEZE.md](TASK-01-CONTRACT-FREEZE.md)。
- Task 01 文档冻结没有修改 API/schema/reducer/TUI/Adapter 产品行为，也没有操作真实服务、DB/socket、
  credential、tmux 或 installed binary。

#### Task 01 验证批次

| 命令 | 退出码/耗时 | 结果 |
|---|---:|---|
| `go test ./internal/domain ./internal/api/... ./internal/client/console ./internal/consolemodel ./internal/cli/console -count=1` | 0 / 19.17s | domain/API/admin/auth/console/panel/client/reducer/TUI 全部通过；Panel 15.887s |
| `go test ./internal/worker/... ./internal/runtime/agy ./internal/runtime/codebuddy ./internal/runtime/acp -count=1` | 0 / 9.06s | Worker 1.533s、AGY 6.882s、CodeBuddy 2.117s、ACP 0.007s |
| `bash deploy/agy/agy-graft_test.sh deploy/agy/agy-graft` | 0 / 0.54s | 7 组 proxy/config/permission/symlink fixture 全部通过；无真实 turn |
| `bash scripts/check-legacy-control-paths.sh --release` | 0 / 0.15s | 全部类别 `CLEAN` |

- 本批只验证冻结契约与既有行为一致，不是 ADR-009 产品 E2E；真实 dispatch->reply 闭环归属 Task 07/08。
- Task 01 变更仅为任务文档、冻结契约和本 log；未运行 Web/部署测试，因为本阶段无产品/Web 行为变化。
- 阶段提交前继续检查 frozen ADR hash、relative links、whitespace、staged path 和 clean external state。

## 7. 后续记录模板

### Task 01：基线、能力盘点与契约冻结

- 开始时间：`2026-09-19`
- baseline：`3312d95` (`codex/adr009-task-console`)
- worktree：`/home/sky/work/touzi/OneAxe/OpenAgentX-adr009-worktree`
- 状态：`active/WAIT`
- 本轮结果：冻结 Task projection、Runtime 输出、focus/CAS、cursor、Diagnostic mode 和容量契约；只允许
  文档、characterization tests 与无行为变化 test helper。
- 非目标：不新增 API/schema，不改 reducer/TUI/Adapter 行为，不操作真实服务/DB/socket/default tmux。
- 证据要求：代码入口、实际 Runtime/wrapper help 或 fixture、现有测试与新增 characterization test
  逐项对应；未知能力标为不支持或待验证，不按设计猜测。

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
