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
| 01 | 基线、能力盘点与契约冻结 | completed | `5717506` | GO |
| 02 | 权威任务观察投影 | completed | `84a2c15` | GO |
| 03 | Runtime 安全输出与终态结果对齐 | completed | `e82ae37` | GO |
| 04 | Task-centric Console reducer | completed | `874a0e1` | GO |
| 05 | Pane 0 任务 TUI 与控制易用性 | completed | `f1bf09a` | GO |
| 06 | 同 pane Diagnostic 模式 | completed | `de7eb32` | GO |
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

#### Task 01 gate record

- 实现提交：`5717506f4f0182b4472901fb596eb81775e6ae3a`；3 files，292 insertions、3 deletions。
- 提交后 worktree clean；相对 `origin/main@c3fc1bb` ahead 39。ADR-009 SHA-256 仍为
  `afb7473b6eb353dd06551bb3126fbe3300005e4a1d782b863a9cb1d0f2a32c69`。
- 监督结论：用户已授权连续执行；主代理按当前范围和证据复核为 `GO`。A09-02/A09-03 已关闭，
  没有产品行为、权限或外部状态变更。
- 主计划和 Task 01 front matter 在本 docs-only gate record 同步为 `completed`；Task 02 保持
  `pending/WAIT`。

### Task 02：权威任务观察投影

- 开始时间：`2026-09-19`；baseline：`0e37e45309351e39790324f092e3b685b2fcf97e`；branch：
  `codex/adr009-task-console`。
- 本轮结果：交付 Agent-scoped Task list/detail、Attach suggested Task、单事务 Task snapshot 和
  Task/Mailbox/Message/Approval SSE 安全投影。
- 状态/CAS/竞态：Task version 与 Journal high-water 来自同一只读事务；分页按
  `updated_at DESC, task_id DESC`；N/N+1 只能整体纳入快照或从 cursor 后重放。
- 失败与权限：跨 Agent、坏 cursor、无效 identity/status、repository/projection/encode/write 失败均
  fail closed，失败 frame 不推进 cursor；CLI viewer 仅通过 `console.read` 使用窄 Console route。
- 非目标：不修改 Runtime adapter、Task 终态语义、reducer/TUI、Diagnostic 切换、schema、Web cookie/CSRF
  或真实服务/DB/socket/tmux。
- 证据预算：一个主要实现批次和一次独立验证批次；若同因测试夹具失败两次，按 `AGENTS.md` 停止原
  验证路径并采用更小的等价证据，不扩大到 Task 03。

#### Task 02 主要实现批次

- Domain 增加 transport-neutral `ConsoleTaskCursor`/`ConsoleTaskSnapshot` aggregate；SQLite 新增稳定
  keyset Task list 与单一 read transaction snapshot，不修改 schema。
- Console 新增 Agent-scoped Task list/detail route，cursor 版本化、绑定 Agent/updated_at/task_id、带
  checksum 且要求 canonical 编码；Attach 在原事务中增加最新非终态 `suggested_task`。
- snapshot 同一事务读取 Task、原始 work Mailbox、latest Run 与精确 Worker generation、latest Message、
  pending Approval 和 Journal high-water；跨 Agent统一 404。
- Panel SSE 为 Task/Mailbox/Message/Approval 增加结构化安全投影；依赖对象同时携带所属 Task 投影以供
  后续 reducer 关联。所有 repository、identity/status、safe projection、encode/write 错误在当前 frame
  和 cursor 之前终止。
- Console client 增加完整分页 Task options 与 detail snapshot 方法；总量 hard cap 10,000，继续只用
  installation-bound CLI bearer。Web cookie/CSRF、CLI scope 白名单、Runtime、Task 终态和 TUI 未修改。
- 变更共 14 个路径：10 个既有代码/测试文件、3 个新增代码/测试文件和本 execution log；无父仓、生成物、
  credential 或真实运行状态变更。

#### Task 02 失败与纠正

| 日期 | 失败 | 根因/影响 | 纠正与结果 |
|---|---|---|---|
| 2026-09-19 | 首次编译型定向测试报 `internal/api/console/handler.go` 未使用 `strings` import | 纯编译错误，未产生运行或外部副作用 | 删除 import；四个受影响 package 编译通过 |
| 2026-09-19 | 首次行为定向测试中 Panel SSE 的 agent filter/cursor recovery 用例失败 | 既有 fake Task/Message/Approval 只满足旧归属查询，缺 version/principal/time 等正式投影必需字段；新代码按设计 fail closed | 将 fixture 提升为合法持久化对象，增加 Mailbox fixture；产品校验未放宽，定向测试恢复通过 |
| 2026-09-19 | 独立验证后审查发现 cursor decoder 接受等价非 canonical JSON，且 detail 尚未直接断言 latest Run/fencing 边界 | 不构成越权，但弱于冻结的 tamper fail-closed 与 Run snapshot 证据 | cursor 增加 canonical bytes/UTC 检查；detail 测试加入精确 generation 并断言 execution JSON/fencing 不泄漏；完整验证从头重跑通过 |

#### Task 02 独立验证批次

| 命令 | 退出码/耗时 | 结果 |
|---|---:|---|
| `go test ./... -count=1` | 0 / 20.91s | 全仓 Go 普通测试无缓存通过；Console 5.396s、Panel 18.413s、SQLite 12.301s |
| `go test -race ./internal/api/console ./internal/api/panel ./internal/client/console ./internal/persistence/sqlite/... -count=1` | 0 / 41.33s | Task 02 全部受影响 package race 通过；Panel 38.634s |
| `go vet ./internal/api/... ./internal/client/console ./internal/persistence/sqlite/...` | 0 / 0.51s | 无 vet 诊断 |
| `go build -o /tmp/openagentx-adr009-task02 ./cmd/openagentx` | 0 / 2.95s | 独立临时产物构建成功；未覆盖 installed binary |
| `bash scripts/check-legacy-control-paths.sh --release` | 0 / 0.15s | 全部类别 `CLEAN` |
| `git diff --check` | 0 / <0.1s | 无 whitespace error |
| `sha256sum docs/decisions/ADR-009-pane-zero-task-console-observability.md` | 0 / <0.1s | 仍为 `afb7473b6eb353dd06551bb3126fbe3300005e4a1d782b863a9cb1d0f2a32c69` |

- 新测试覆盖：stable pagination/坏 cursor/跨 Agent、Attach suggestion、真实 CLI bearer role+scope、
  snapshot N/N+1、Task/Run/Mailbox/Message/Approval 安全字段，以及 ownership/projection/encode/write 故障
  不发送失败 event 或 N+1、下一请求从 last-applied 恢复。
- 外部状态：未操作真实 HOME/DB/UDS/credential/default tmux/user-systemd/installed binary；未 push、merge
  或修改 `steadyflow` 父仓。Task 02 当前仍为 `active/WAIT`，主计划/front matter 保持 `pending`。
- Open issues：当前 Task 02 无 P0/P1；Runtime 过程输出和终态结果对齐仍按计划归属 Task 03，未提前实现。

#### Task 02 gate record

- 实现提交：`84a2c15473455cf2ae5e052e9790fd851db7abda`；14 files，1599 insertions、22 deletions。
- 提交后 worktree clean；相对 `origin/main@c3fc1bb` ahead 41。ADR-009 SHA-256 仍为
  `afb7473b6eb353dd06551bb3126fbe3300005e4a1d782b863a9cb1d0f2a32c69`。
- 主代理按冻结契约、全量普通测试、定向 race 和安全故障注入复核为 `GO`；无 P0/P1 或未归属兼容
  fallback。任务说明中“仅挂 UDS CLI mux”的歧义已按 Task 01 契约纠正为：同一 Console handler，Web
  cookie 与 UDS CLI bearer 分离，CLI 不开放广泛 Panel route。
- 主计划和 Task 02 front matter 在本 docs-only gate record 同步为 `completed`；Task 03 保持
  `pending/WAIT`。未 push、merge、安装、重启或操作真实 DB/socket/tmux/父仓。
- Gate 检查首次在 zsh 中把三个路径误作为单一标量传给 `sed/git add`，命令在暂存前以路径不存在退出；
  未产生 staging 或提交。随后改用显式路径数组并重新执行全部 whitespace、相对链接、状态一致性和
  staged-path 检查。

### Task 03：Runtime 安全输出与终态结果对齐

- 开始时间：`2026-09-20`；baseline：`be753ee1a1bb058799888199f385da900201263c`；branch：
  `codex/adr009-task-console`；worktree：`/home/sky/work/touzi/OneAxe/OpenAgentX-adr009-worktree`。
- 本轮用户结果：支持增量的 Runtime 产生有界安全输出，所有正式 Adapter 都给出可验证的终态或明确
  fail closed；Console/Web 共用投影并区分 Task outcome、Runtime reply、无结果、无效和截断。
- 状态/CAS/幂等：`FinishRun` 的 Run、Task、Journal 原子关系不变；最终回复只记录一次；terminal 后的
  迟到输出不能回退状态；每个 Run 最多持久化 256 条实时输出。
- 失败/竞态：覆盖空流、畸形 envelope、无 terminal、仅 stderr、非零退出、sink 失败、超限、cancel/exit、
  最后一条 output 与 finish 顺序、重复 finish 和事务回滚；失败不推断成功。
- 安全/资源：公开文本单字段 4 KiB，Adapter authoritative buffer 有界；不输出或持久化 Secret、raw
  stderr、hidden reasoning 和未知 JSON 字段。
- 非目标：不修改 ADR-006/007 语义，不实现 reducer/TUI/Diagnostic 切换、Runtime TTY、tmux 控制或真实
  部署；主计划和 Task 03 front matter 保持 `pending`。
- 证据预算：一个主要实现批次和一次独立验证批次；同因夹具/验证工具失败两次即停止该路径，改用更小
  等价证据或报告阻断，不扩大到 Task 04。

#### Task 03 主要实现批次

- `safeoutput` 增加幂等文本投影、截断元数据、Task outcome state 和 stored TurnResult 解码；AGY、
  CodeBuddy、ACP 与 Worker service 在返回/持久化 TurnResult 前使用同一规则，服务端保留第二道校验。
- TurnResult/API 增加 result/error truncation 标记；Task projection 增加 `pending/not_recorded/available/truncated`
  outcome，Run projection 增加 `not_recorded/invalid/empty/available/truncated` reply state。Console 与 Web
  均复用相同 projector，旧 v1 JSON 缺少新可选字段时仍可读取。
- AGY 只采信已知 init/step/result/error 字段，未知事件不进入最终 reply；空流、畸形、无 terminal、矛盾
  status、sink 失败和 1 MiB authoritative output 超限均返回 error，最终由 Worker 归类 `uncertain`。
- CodeBuddy 保持 text stdout、完整行增量、1 MiB authoritative buffer 和 256 event 上限；最终 TurnResult
  在 Adapter 返回前脱敏/限长。ACP 不再忽略 malformed/sink/stderr/process failure，也不再缺 terminal 默认
  success；stderr 有界读取后仅进入脱敏诊断。
- Worker API 单 batch 最大 256 event；SQLite 在同一受 fencing/lease 保护的事务内按 Run 持久化最多 256
  条公开 Runtime event，超额片段安全 no-op，approval 与最终 Run/Task Journal 不受该上限影响。
- `FinishRun` 原有 Run、Task、SessionBinding、Journal 单事务和重复 finish 幂等保持不变；新增测试证明最后
  output sequence 早于 terminal event，最终安全 result 与 truncation metadata 同时提交。

#### Task 03 失败与纠正

| 日期 | 失败 | 根因/影响 | 纠正与结果 |
|---|---|---|---|
| 2026-09-20 | 首次定向测试拒绝未知 AGY event 携带的伪 status | 严格状态校验误把未知字段当正式能力；无外部副作用 | 未知 event 整体降级为空安全 event，不采信 status/text；terminal result 继续严格校验 |
| 2026-09-20 | AGY event-cap fixture 的 `step_update.status=running` 被拒绝 | 过程状态与 terminal enum 未分开 | 明确接受 starting/running/pending/processing 为非终态且不写入最终 status；其余未知状态仍 fail closed |
| 2026-09-20 | 既有 Worker UDS E2E 在 `turn.output` 缺 JSON payload 时返回 500 | 旧 fixture 依赖缺字段 event 被静默持久化 | fixture 改为合法空对象；产品保持非对象/缺失 projection fail closed |
| 2026-09-20 | CodeBuddy 安全结果断言假定 `[REDACTED]` 外保留 JSON 引号 | 测试绑定了非协议格式，秘密已正确移除 | 改为断言原值均消失且存在 redaction marker；未放宽 projector |
- 自审进一步修正：截断 marker 二次投影改为幂等；AGY terminal status 不再继承先前 step 状态；ACP
  重复 terminal fail closed；三个正式 Adapter 均在返回 TurnResult 前执行共享安全投影。
- 安全自审发现新增 `diagnostic_truncated` 若不随内容清空，会向 Normal 模式暴露诊断存在性的侧信号；
  `stripDiagnostic` 已同时清除内容和 truncation metadata，并增加直接回归测试。

#### Task 03 独立验证批次

| 命令 | 退出码/耗时 | 结果 |
|---|---:|---|
| `go test ./internal/safeoutput ./internal/runtime/agy ./internal/runtime/codebuddy ./internal/runtime/acp ./internal/controlplane ./internal/api/console ./internal/api/panel ./internal/persistence/sqlite/... -count=1` | 0 / 约 18s | Task 03 Adapter、service、API、SQLite 定向普通测试通过 |
| `go test -race ./internal/worker/... ./internal/safeoutput/... ./internal/runtime/agy ./internal/runtime/codebuddy ./internal/runtime/acp ./internal/controlplane ./internal/api/panel ./internal/api/console ./internal/persistence/sqlite/... -count=1` | 0 / 43.55s | 全部受影响链路 race 通过 |
| `go test ./... -count=1` | 0 / 21.60s | 全仓无缓存普通测试通过 |
| `go vet ./internal/worker/... ./internal/safeoutput/... ./internal/runtime/agy ./internal/runtime/codebuddy ./internal/runtime/acp ./internal/controlplane ./internal/api/... ./internal/persistence/sqlite/...` | 0 / 1.15s | 无 vet 诊断 |
| `go build -o /tmp/openagentx-adr009-task03 ./cmd/openagentx` | 0 / 3.00s | 独立临时产物构建成功；未覆盖 installed binary |
| `npm run test:observation` | 0 / 0.47s | Web observation 4/4 通过 |
| `bash scripts/check-legacy-control-paths.sh --release` | 0 / 0.15s | 全部类别 `CLEAN` |
| `git diff --check` | 0 / <0.1s | 无 whitespace error |
| `sha256sum docs/decisions/ADR-009-pane-zero-task-console-observability.md` | 0 / <0.1s | 仍为 `afb7473b6eb353dd06551bb3126fbe3300005e4a1d782b863a9cb1d0f2a32c69` |

- 最后只修改了 Panel Normal projection 的 Diagnostic truncation metadata 清理；影响范围复核后复用前述
  全仓证据，并额外执行 `go test ./internal/api/panel -count=1`（0 / 18.60s）和
  `go test -race ./internal/api/panel -count=1`（0 / 39.09s）。
- 首次并行执行末次 Panel race 时未回传后台 session ID，结果通道不可恢复；确认原进程自然结束后，
  单独重跑取得上述明确退出码。该编排失败未修改代码、fixture 或外部状态。
- 新增证据覆盖：AGY/CodeBuddy/ACP 增量/仅终态能力、256 event cap、空/畸形/无 terminal/sink/stderr/
  截断、Task/Runtime 结构化结果状态、Web/Console 同源脱敏、Normal Diagnostic metadata 清除，以及
  output->terminal Journal 顺序和原子持久化。
- 外部状态：未操作真实 HOME/DB/UDS/credential/default tmux/user-systemd/installed binary；未 push、merge
  或修改 `steadyflow` 父仓。Task 03 保持 `active/WAIT`，主计划/front matter 继续 `pending`。
- Open issues：当前 Task 03 无 P0/P1；focused Task reducer、TUI 最终回复渲染和控制易用性按冻结计划分别
  归属 Task 04/05，未提前实现。

#### Task 03 gate record

- 实现提交：`e82ae373fd9a3bbadb7a4bee16ef3c5a8fc52f00`；21 files，808 insertions、130 deletions。
- 提交后 worktree clean；相对 `origin/main@c3fc1bb` ahead 43。ADR-009 SHA-256 仍为
  `afb7473b6eb353dd06551bb3126fbe3300005e4a1d782b863a9cb1d0f2a32c69`。
- 主代理按冻结契约、Adapter/事务/投影故障注入、定向 race、全仓普通测试、Web observation 和 release
  scanner 复核为 `GO`；没有 P0/P1、权限 fallback 或 ADR-006/007 语义变化。
- 主计划和 Task 03 front matter 在本 docs-only gate record 同步为 `completed`；Task 04 保持
  `pending/WAIT`。未 push、merge、安装、重启或操作真实 DB/socket/tmux/父仓。

### Task 04：Task-centric Console reducer

- 开始时间：`2026-09-20`；baseline：`2de51b9354326635e659208411ab6f6756df8c37`；branch：
  `codex/adr009-task-console`；worktree：`/home/sky/work/touzi/OneAxe/OpenAgentX-adr009-worktree`。
- 本轮用户结果：reducer 成为 focused Task、Task version/status、Mailbox/Run、最终 outcome/reply、
  Message/Approval、Worker identity、mode/connection 和 Event cursor 的唯一状态来源；Task 05 只负责呈现
  和命令体验。
- 状态/CAS/幂等：focus 来源严格按 dispatch、手工选择、Attach suggestion；Task higher version 更新、same
  version 完全一致幂等、lower version 合法忽略；terminal 冲突拒绝；Run 同时受 Task 与当前 Worker
  instance/generation fencing。
- 失败/竞态：覆盖跨 Agent/Task、无效安全投影、dispatch/event 乱序、旧 Worker/Run、snapshot/event ack、
  mode epoch 迟到输入和 terminal 后冲突；拒绝路径不推进 cursor，合法忽略路径明确推进。
- 资源/安全：active/recent Task、Timeline 和文本均同时受 count/byte cap；只保存 Task 02/03 安全 read
  model，不接收 raw payload，不新增 I/O、schema、API、TUI 命令或 Diagnostic 切换。
- 证据预算：一个主要实现批次和一次独立验证批次；同因 fixture/验证工具连续失败两次即停止原路径。
  主计划和 Task 04 front matter 保持 `pending`，本 log 为 `active/WAIT`。

#### Task 04 主要实现批次

- reducer 新增 focused/active/recent Task、Mailbox、Run、Message、Approval、Task outcome、Runtime reply、
  connection/mode epoch 和 bounded Timeline；dispatch 正式结果、手工选择与 Attach suggestion 是仅有 focus
  来源，普通 SSE Task event 不抢占 focus。
- Task version、terminal、Run `(started_at, run_id)`、Worker instance/generation 和 stream epoch 均 fail
  closed；合法 lower-version/旧 Worker 历史事件推进 cursor，无效或冲突投影不 ack。Worker replacement/offline
  清理无法证明的 active Run、backend、diagnostic 和 native pending Approval，但保留 terminal outcome/reply。
- snapshot/event 通过 copy-on-write 提交 Task 状态；Task snapshot 中旧 Worker 的 active Run 与关联 native
  Approval 被过滤，旧 Worker 已持久化 terminal reply 保留。Normal 模式拒绝 Diagnostic，安全文本及 truncation
  metadata 在进入状态前校验。
- active/recent Task 各最多 128 条、Task 总计 256 KiB；Timeline 最多 256 条、64 KiB、单条 2 KiB，长文本
  再限为 512 bytes。TUI 本阶段只接入 connection、dispatch/steer/cancel outcome 和 reducer rejection，未修改
  布局、公开命令语法或实现 Task 05/06 行为。
- 变更限于 6 个 reducer/TUI 代码与测试文件、本 execution log，共 7 个路径；无 API/schema/persistence/
  Runtime/tmux/systemd 变更。

#### Task 04 失败与纠正

| 日期 | 失败/发现 | 根因与影响 | 纠正与结果 |
|---|---|---|---|
| 2026-09-20 | 首次编译缺少本包 `cloneTime` helper | 新 clone 路径未包含时间指针复制；仅编译失败 | 增加本包私有 helper，未改变 transport/domain contract |
| 2026-09-20 | 既有 Run/TUI fixture 被严格投影校验拒绝 | fixture 缺 Task 03 已冻结的 version/time/reply state 或 Task projection | 提升 fixture 为正式安全 DTO；未放宽产品校验 |
| 2026-09-20 | 新测试曾假定普通 SSE Task 自动抢占 focus | 测试违背冻结 focus 优先级 | 先模拟 dispatch 正式结果；普通 SSE 继续只更新列表/Timeline |
| 2026-09-20 | Task options 顺序修复时出现局部变量声明顺序编译错误 | 实现期编辑错误，无运行副作用 | 调整声明后定向测试通过 |
| 2026-09-20 | 自审发现全量 Task 深拷贝、高频旧 Run/terminal Run、option 顺序和 Timeline truncation 边界 | 可造成额外复制、旧状态覆盖、排序反转或 metadata 不一致 | event 改为按相关 Task copy-on-write；补 Run fencing/terminal 清理、权威排序和 truncation 一致性测试 |
| 2026-09-20 | 自审发现 Task outcome/reply/output truncation 与 Task snapshot 旧 Worker active Run 边界不足 | 无效安全投影可能污染状态，旧 active Run/native Approval 可能留存 | 入 reducer 前统一 fail closed；active Run 再按当前 Worker fencing，terminal reply 保留 |
| 2026-09-20 | 独立验证首次使用 zsh `time -p`，7 条命令均以 127 退出且测试未启动 | zsh 将 `time` 解析为保留字并尝试执行 `-p`；无代码或外部副作用 | 不重复该路径，改用 `/usr/bin/time -p`；同一验证批次随后全部通过 |

#### Task 04 独立验证批次

| 命令 | 退出码/耗时 | 结果 |
|---|---:|---|
| `go test ./internal/consolemodel ./internal/client/console ./internal/cli/console -count=1` | 0 / 3.55s | reducer、Follow ack client 与最小 TUI 接线普通测试通过 |
| `go test -race ./internal/consolemodel ./internal/client/console ./internal/cli/console -count=1` | 0 / 8.44s | Task 04 全部受影响 package race 通过 |
| `go test ./internal/consolemodel -run 'Fuzz|Property' -count=1` | 0 / 1.42s | `FuzzTaskReducerMalformedProjectionDoesNotAdvanceCursor` seeds 实际执行通过 |
| `go test ./... -count=1` | 0 / 21.30s | 全仓 Go 普通测试无缓存通过；Panel 19.098s、Fleet 11.973s、SQLite 12.383s |
| `go vet ./internal/consolemodel ./internal/client/console ./internal/cli/console` | 0 / 0.69s | 无 vet 诊断 |
| `go build -o /tmp/openagentx-adr009-task04 ./cmd/openagentx` | 0 / 4.73s | 独立临时产物构建成功，未覆盖 installed binary |
| `bash scripts/check-legacy-control-paths.sh --release` | 0 / 0.16s | 全部 legacy/tmux control 类别 `CLEAN` |
| `git diff --check` | 0 / <0.1s | 无 whitespace error |
| `sha256sum docs/decisions/ADR-009-pane-zero-task-console-observability.md AGENTS.md` | 0 / <0.1s | ADR 仍为 `afb7473...32c69`；执行基线 `AGENTS.md` 为 `b532645...a95b` |

- 覆盖证据包括 focus 三来源、dispatch/event 乱序、Task version/terminal matrix、Mailbox/Message/Approval/
  Runtime reply、旧 Worker/旧 Run、replacement/offline、Task snapshot fencing、stream epoch/Normal Diagnostic、
  atomic rejection、cursor 不跨越、bounded collection/Timeline 和 malformed projection fuzz seeds。
- 外部状态：未操作真实 HOME/DB/UDS/credential/default tmux/user-systemd/installed binary 或父仓；未 push、
  merge、部署或重启。Task 04 无新增 P0/P1；Task 05 TUI 呈现/快捷命令与 Task 06 Diagnostic 切换保持后置。

#### Task 04 gate record

- 实现提交：`874a0e12cbc2cb73d0ed403c8e562a6427a102e6`；7 files，1923 insertions、88 deletions。
- 提交后 feature worktree clean；相对 `origin/main@c3fc1bb` ahead 45。主工作树仍为 clean
  `main@3723c77`、相对 origin ahead 1；ADR-009 SHA-256 仍为
  `afb7473b6eb353dd06551bb3126fbe3300005e4a1d782b863a9cb1d0f2a32c69`。
- 主代理按新版 `AGENTS.md` 的范围裁决、Task/Run/Worker/cursor 状态矩阵、定向 race、malformed projection
  fuzz seeds、全仓普通测试和 release scanner 复核为 `GO`；没有 P0/P1、权限 fallback、无界集合或
  ADR-006/007 语义变化。
- 主计划和 Task 04 front matter 在本 docs-only gate record 同步为 `completed`；Task 05 保持
  `pending/WAIT`。未 push、merge、安装、重启或操作真实 DB/socket/tmux/父仓。

### Task 05：Pane 0 任务 TUI 与控制易用性

- 开始时间：`2026-09-20`；baseline：`4f88c29e56eda644fecc3638771984ae4837d880`；branch：
  `codex/adr009-task-console`；worktree：`/home/sky/work/touzi/OneAxe/OpenAgentX-adr009-worktree`。
- 本轮用户结果：pane 0 连续呈现 Task 排队、领取、Run、等待、终态、安全输出和最终回复；用户无需抄写
  Task ID/version 即可 steer/cancel focused Task，并可从 bounded active/recent 列表显式切换 focus。
- 状态/CAS/幂等：View 只读 Task 04 reducer；快捷控制只使用最新 focused Task 的完整 ID/version；显式
  flag 与经批准旧位置语法无歧义，每次用户确认只调用一次 authenticated official API。
- 失败/竞态：无 focus、terminal、断线、token expiry、CAS stale、API 失败均不假成功或自动重试；事件、
  resize、heartbeat、terminal 与 control response 不改变输入 draft/cursor/focus 或用户上滚位置。
- 资源/平台：复用 bounded Task/Timeline/result；`80x5`、`20x3`、`8x1` 严格按实际高度渲染；退出只
  detach Console，不停止 Task/Worker。
- 范围边界：不实现 Task 06 同 pane Diagnostic 切换，不修改 API/schema/Runtime/Fleet/workspace/
  user-systemd/default paths，不引入 TurnHandle、tmux 控制或 raw output。
- 证据预算：一个主要实现批次和一次独立验证批次；同因 fixture/验证工具连续失败两次即停止原路径。
  主计划和 Task 05 front matter 保持 `pending`，本 log 为 `active/WAIT`。

#### Task 05 主要实现批次

- Console application 复用既有 authenticated official client 的 `ListTaskOptions` 和 `TaskSnapshot`；没有新增
  transport、控制协议、认证 fallback 或 tmux 业务控制。Attach suggested Task 自动加载权威详情，`/tasks`
  展示 reducer 中 bounded active/recent 集合并支持显式 focus。
- Attach header/status 显示 focused Task、version、status、stage、Worker/generation、connection/cursor；
  `/status` 显示完整 Task/Run/Message/Approval ID/version、Task outcome、Runtime reply 和证据来源。Timeline
  从 Task 04 reducer 的安全 `TimelineItem` 渲染 mailbox、Run、Task terminal 和 Runtime reply，不读取 raw
  Journal、stderr 或 Runtime payload。
- 新增 focused `/steer <content>`、`/cancel`，以及无歧义的 `--task/--version` 显式形式；保留冻结契约批准的
  旧位置语法。快捷形式只使用 reducer 最新完整 Task ID/version；无 focus、terminal、断线、session expiry、
  Task detail loading 和 pending control 均在 API 调用前拒绝，每次用户确认最多调用一次正式 API。
- stale CAS 只恢复提交前 draft、读取一次权威 Task snapshot 并显示新 version，不自动重试写操作。API 失败、
  reducer 拒绝和 pending 期间 token 绝对到期也恢复 draft；suggested Task 自动详情不会抢走用户当前 overlay。
- Task 列表、状态、Timeline 和结果均在终端渲染前移除控制字符并保持既有限量；后台 event/control/resize
  仅在用户原本位于底部时自动跟随，输入 draft/cursor/focus 不变。`80x5`、`20x3`、`8x1` 继续严格按
  实际终端高度裁剪。
- 变更限于 Console application/TUI 代码与测试、本 execution log，共 6 个路径；没有 API/schema/
  persistence/Runtime/Fleet/workspace/user-systemd/default-path 或 Task 06 Diagnostic mode-switch 变更。

#### Task 05 失败与纠正

| 日期 | 失败/发现 | 根因与影响 | 纠正与结果 |
|---|---|---|---|
| 2026-09-20 | 无 focus/terminal 命令错误最初只显示 `operation failed` | user-visible error 未进入安全摘要分支，用户无法理解本地拒绝原因 | 增加有界 user-visible error，保持敏感错误不透传；回归测试通过 |
| 2026-09-20 | 新 header 测试最初要求显示完整 Task ID | 紧凑 header 与详情职责混淆；长 ID 会破坏小 pane 可读性 | header 使用短 ID，`/status` 保留完整 ID/version；尺寸与详情测试通过 |
| 2026-09-20 | 新测试一次编译失败：同一作用域误用 `:=` | 测试编辑错误，无产品或外部副作用 | 改为赋值后定向测试通过 |
| 2026-09-20 | 只读审查尝试读取不存在的 `internal/domain/mailbox.go` | Mailbox contract 实际位于 `mailbox_contract.go` | 使用 `rg` 定位正式定义；未修改代码或 fixture |
| 2026-09-20 | 提交前审查发现 Task summary 可把 ANSI 控制字符交给 Bubbles，Task detail loading 可与第二次写操作并发 | 安全 DTO 的长度/脱敏不等价于终端控制字符安全；stale refresh 期间可再次使用旧 CAS | list title/description/filter 在渲染前统一 `boundedSafeText`；权威 detail loading 期间禁用写并保留 draft；控制字符、expiry、overlay、终态原因和 loading 回归均通过 |

#### Task 05 独立验证批次

| 命令 | 退出码/耗时 | 结果 |
|---|---:|---|
| `go test ./internal/cli/console ./internal/client/console ./internal/consolemodel -count=1` | 0 / 3.3s | Task TUI、正式 client、reducer 接线普通测试通过 |
| `go test -race ./internal/cli/console ./internal/client/console ./internal/consolemodel -count=1` | 0 / 6.88s | 受影响三包 race 通过 |
| `go test ./internal/cli/console -run 'TTY\|Compact\|Task\|Control' -count=10` | 0 / 12.88s | 状态流、compact、控制和 TTY 相关测试连续十次通过 |
| `go test ./internal/cli/console -run '^TestIsolatedTTYSmokeUsesAltScreenBindsAndPreservesExtraPanes$' -count=1` | 0 / 1.80s | 唯一 `tmux -L` + PTY：alt-screen、正式临时 UDS/auth、OAX bind、正常退出和 pane 1/2 保留通过 |
| `go test ./... -count=1` | 0 / 20.81s | 全仓 Go 无缓存普通测试通过 |
| `go vet ./internal/cli/console ./internal/client/console ./internal/consolemodel` | 0 / 0.46s | 无 vet 诊断 |
| `go build -o /tmp/openagentx-adr009-task05 ./cmd/openagentx` | 0 / 4.38s | 独立临时产物构建成功，未覆盖 installed binary |
| `bash scripts/check-legacy-control-paths.sh --release` | 0 / 0.14s | 全部 legacy/tmux control 类别 `CLEAN` |
| `git diff --check` | 0 / <0.1s | 无 whitespace error |
| `sha256sum docs/decisions/ADR-009-pane-zero-task-console-observability.md AGENTS.md` | 0 / <0.1s | ADR 为 `afb7473...32c69`；新版执行基线为 `b532645...a95b` |

- 新增证据覆盖：Task focus/list/detail、完整 ID/version、queued->mailbox->Run->terminal/reply、focused 与显式
  control exactly once、CAS stale 无重试、token expiry/API/reducer 失败 draft 恢复、ANSI 清理、overlay 保持、
  heartbeat/后台事件输入与上滚稳定、bounded Timeline 和 compact layout。
- 外部状态：未操作真实 HOME/DB/UDS/credential/default tmux/user-systemd/installed binary 或父仓；未 push、
  merge、部署或重启。Task 05 保持 `active/WAIT`，主计划/front matter 继续 `pending`。
- Open issues：本阶段无 P0/P1；同 pane Normal/Diagnostic mode switch 仍严格归属 Task 06，未提前实现。

#### Task 05 gate record

- 实现提交：`f1bf09ac455ea56cad71c940a8ae37bd565996cb`；6 files，1075 insertions、39 deletions。
- 提交后 feature worktree clean；相对 `origin/main@c3fc1bb` ahead 47。主工作树仍为 clean
  `main@3723c77`、相对 origin ahead 1；ADR-009 SHA-256 仍为
  `afb7473b6eb353dd06551bb3126fbe3300005e4a1d782b863a9cb1d0f2a32c69`，新版 `AGENTS.md` 为
  `b53264590ccb1ebace81668ae3c98f1e29ce8f61384a259f08a5e23b8b92a95b`。
- 主代理按 focused Task/CAS、终态结果、输入/滚动不变量、控制字符边界、compact layout、定向 race、
  唯一 `tmux -L` + PTY smoke、全仓普通测试和 release scanner 复核为 `GO`；没有 P0/P1、认证 fallback、
  raw output、tmux 控制或 ADR-006/007 语义变化。
- 主计划和 Task 05 front matter 在本 docs-only gate record 同步为 `completed`；Task 06 保持
  `pending/WAIT`。未 push、merge、安装、重启或操作真实 DB/socket/tmux/父仓。

### Task 06：同 pane Diagnostic 模式

- 开始时间：`2026-09-20`；baseline：`016a13f`；branch：`codex/adr009-task-console`；worktree：
  `/home/sky/work/touzi/OneAxe/OpenAgentX-adr009-worktree`。
- 本轮用户结果：在当前 Attach TUI 内用 `/diagnostic` 和 `/normal` 原地往返，显示授权、脱敏、限量的
  Diagnostic 状态；不退出/重启 pane，不影响 Worker、focused Task、draft 或 Normal 安全历史。
- 状态/幂等：每个 Follow 使用独立 context、opaque ID 和 reducer stream epoch；先 cancel 并等待旧流从
  active registry 移除，再启动目标 mode；同 mode 请求幂等，切换中重复请求明确拒绝。
- 失败/竞态：403、网络、snapshot/reducer 拒绝、token expiry、旧 event/connection/followDone 迟到均不得
  污染新状态或推进 cursor；Diagnostic 失败只允许一次 Normal 回退，回退失败保持 Normal/disconnected。
- 安全/资源：切换开始即清除 Diagnostic 并进入 Normal-safe projection；Timeline 同一安全事件保存 Normal/
  Diagnostic 两种 bounded render，Normal 永不回显历史 Diagnostic；不新增 API、协议、tmux 或 Worker 控制。
- 证据格式：application Follow registry/cancel 完成、TUI before/msg/after、fake clock、API role+scope matrix、
  race，以及唯一 `tmux -L` + PTY 的 Normal->Diagnostic->Normal->quit 小 pane 往返。
- 范围边界：不修改 Fleet/workspace/user-systemd/default paths，不开始 Task 07 隔离 daemon/Worker E2E 或文档。
  主计划和 Task 06 front matter 保持 `pending`，本 log 为 `active/WAIT`。

#### Task 06 主要实现批次

- Console application 为每个 Follow 建立独立 context、opaque Follow ID 和 active registry；snapshot/event callback
  必须等待 TUI reducer ack，旧 Follow 从 registry 删除后才发送 terminal message，新 mode 只在旧流 terminal
  message 被消费后启动，不存在同时 active 的双 Follow。
- reducer 增加 stream epoch、`BeginStream`/`AbortStream` 和 `switching` connection；切换开始即删除 Diagnostic、
  使用 Normal-safe projection。旧 epoch 的 event/snapshot 只以 `context.Canceled` ack，不 Apply、不推进 cursor；
  focused Task、Task 列表和 bounded Timeline 状态保留。
- TUI 新增 `/diagnostic` 与 `/normal` 原地切换、typed follow/cancel/connection Msg；Diagnostic 成功后显示结构化
  Worker/generation/status、Backend health、Run/stage/wait/heartbeat/lease/drain 和脱敏 diagnostic。Normal Timeline
  永不把 Diagnostic-only 文本作为 fallback，回切或 Diagnostic stream 终止立即清除 privileged state。
- Diagnostic 403、网络或 snapshot/reducer 拒绝只允许一次 authenticated Normal Follow 回退；回退失败保持
  Normal/disconnected，不循环。token 到期立即切断 Follow、删除 Diagnostic、禁用写操作，同时保留 draft/cursor。
- 真实 PTY smoke 使用临时 HOME/DB/UDS/auth、唯一 `tmux -L` 和三个小 pane，完成 Normal Attach、
  `/diagnostic`、Diagnostic snapshot/SSE/overlay、`/normal`、Normal snapshot/SSE 和 `/quit`，并断言 alt-screen、
  OAX binding、pane 0 exit status 0 与 pane 1/2 保留。
- 变更限于 Console application/TUI、共享 reducer 的代码与测试以及本 execution log，共 8 个路径；没有新增
  API/schema/auth/Runtime/Fleet/workspace/systemd/default-path 行为，也没有修改 ADR-006/007。

#### Task 06 失败与纠正

| 日期 | 失败/发现 | 根因与影响 | 纠正与结果 |
|---|---|---|---|
| 2026-09-20 | 首轮四包定向测试中 4 个既有 TUI fixture 把合法 event 当作 stale，往返测试因 Task detail pending 未启动 `/normal` | fixture 未绑定新增 stream epoch；只读详情加载被过度当作 mode-switch 阻断 | fixture 显式使用 reducer epoch；只允许只读详情与切换并行，写操作仍禁用；四包重验通过 |
| 2026-09-20 | 首次真实 PTY 往返已进入 Diagnostic，但第二次 Normal SSE 未建立 | 单独 `Esc` 后固定等待 150ms，随后 `/normal` 字节被 PTY 解析为同一转义序列；产品状态机未收到命令 | recorder 按新输出 offset 等待 overlay 关闭后的输入提示，再逐键发送；相同 smoke `-count=3` 通过 |
| 2026-09-20 | 首次 Web observation 从仓库根运行返回 ENOENT | `package.json` 位于 `web/`，测试没有启动；无产品或外部副作用 | 在 `web/` 执行正式脚本，4/4 通过；不再重复错误路径 |
| 2026-09-20 | 提交前审查发现 Diagnostic snapshot 被 reducer 拒绝后只 cancel、不继续读取旧流 terminal message | ack 已阻止 cursor 前进，但 Normal 回退可能因收不到旧 `followDone` 永远不启动 | cancel 与 `waitFollow` 同批执行，旧流结束后才启动唯一 Normal Follow；拒绝 snapshot 回归和 race 通过 |
| 2026-09-20 | 提交前审查发现 timeline 容器在 Normal 文本为空时会复制 Diagnostic 文本 | 当前主调用通常有普通摘要，但容器安全不变量允许未来 Diagnostic-only 内容越界 | 删除 Diagnostic->Normal fallback，保留 Normal->Diagnostic 安全 fallback；专门回归证明 Normal 为空且 Diagnostic 可见 |

#### Task 06 独立验证批次

| 命令 | 退出码/耗时 | 结果 |
|---|---:|---|
| `go test ./internal/cli/console ./internal/client/console ./internal/api/console ./internal/api/panel ./internal/consolemodel -count=1` | 0 / max package 21.93s | Follow/TUI/client/API/reducer 五包普通测试通过 |
| `go test -race ./internal/cli/console ./internal/client/console ./internal/api/console ./internal/api/panel ./internal/consolemodel -count=1` | 0 / max package 42.51s | 五包 race 通过 |
| `go test ./internal/cli/console -run 'Diagnostic\|Mode\|Follow\|TTY' -count=3` | 0 / 9.04s | mode/Follow/TTY 状态测试三次通过 |
| `go test ./internal/cli/console -run '^TestIsolatedTTYSmokeUsesAltScreenBindsAndPreservesExtraPanes$' -count=3` | 0 / 8.45s | 唯一隔离 tmux/PTY 完整往返三次通过；pane 1/2 保留 |
| `go test ./... -count=1` | 0 / max package 21.56s | 全仓 Go 无缓存普通测试通过 |
| `go vet ./internal/cli/console ./internal/client/console ./internal/api/... ./internal/consolemodel` | 0 / <1s | 无 vet 诊断 |
| `go build -o /tmp/openagentx-adr009-task06 ./cmd/openagentx` | 0 / <5s | 独立临时产物构建成功，未覆盖 installed binary |
| `npm run test:observation`（`web/`） | 0 / 0.53s | Web observation 4/4 通过 |
| `bash scripts/check-legacy-control-paths.sh --release` | 0 / <1s | 全部 legacy/tmux control 类别 `CLEAN` |
| `git diff --check` | 0 / <0.1s | 无 whitespace error |
| `sha256sum docs/decisions/ADR-009-pane-zero-task-console-observability.md AGENTS.md` | 0 / <0.1s | ADR 为 `afb7473...32c69`；执行基线为 `b532645...a95b` |

- 证据覆盖单一 active Follow、cancel/ack/registry 清理、旧 epoch 迟到输入、Diagnostic 403/网络/snapshot
  拒绝与单次 Normal 回退、token expiry、Normal projection、bounded 双视图 Timeline、输入/滚动/focus 保持及真实
  三 pane PTY 往返。
- 外部状态：所有 UDS/DB/HOME/credential/tmux 均为测试临时资源；未操作真实服务、默认 tmux、user-systemd、
  installed binary、父仓或运行数据库，未 push、merge、部署或重启。
- Open issues：本阶段无 P0/P1；真实 daemon/Worker 的 dispatch->执行过程->final reply 与 Console 退出后继续领取
  第二项 Task 归属 Task 07，未用本阶段 smoke 冒充。

#### Task 06 gate record

- 实现提交：`de7eb3204132259e58f155c6ced9419d60c8f591`；8 files，924 insertions、95 deletions。
- 提交后 feature worktree clean；相对 `origin/main@c3fc1bb` ahead 49。主工作树仍为 clean
  `main@3723c77`、相对 origin ahead 1；ADR-009 SHA-256 仍为
  `afb7473b6eb353dd06551bb3126fbe3300005e4a1d782b863a9cb1d0f2a32c69`，执行基线 `AGENTS.md` 为
  `b53264590ccb1ebace81668ae3c98f1e29ce8f61384a259f08a5e23b8b92a95b`。
- 主代理按单一 Follow/ack/cursor/epoch 状态机、Normal 安全投影、一次回退、token expiry、定向 race、三次
  唯一 `tmux -L` + PTY 往返、全仓普通测试和 release scanner 复核为 `GO`；没有 P0/P1、双 Follow、
  Diagnostic 泄漏、权限 fallback 或 ADR-006/007 语义变化。
- 主计划和 Task 06 front matter 在本 docs-only gate record 同步为 `completed`；Task 07 保持
  `pending/WAIT`。未 push、merge、安装、重启或操作真实 DB/socket/tmux/父仓。

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
