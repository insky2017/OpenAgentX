---
doc_type: execution_log
status: completed
owner: openagentx
updated_at: 2026-09-22
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
| 07 | 隔离用户闭环与操作文档 | completed | `a5874d7` | GO |
| 08 | 集成审查与候选门禁 | completed | `8e62f8c` | GO |

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

### Task 07：隔离用户闭环与操作文档

- 开始时间：`2026-09-20`；baseline：`4c7fa48`；branch：`codex/adr009-task-console`；worktree：
  `/home/sky/work/touzi/OneAxe/OpenAgentX-adr009-worktree`。
- 本轮用户结果：以默认 profile 从 PTY 初始化/login/Fleet/OAX Attach 到 dispatch、过程输出、focused steer、
  最终 reply、Diagnostic 往返和 `/quit`；Console 退出后同一 resident Worker 继续领取并完成第二个 Task。
- 状态/CAS/幂等：重复 init/up/workspace 不破坏；focused steer 只使用 reducer 最新完整 Task ID/version 且正式
  API 只调用一次；Task/Run/cursor、Worker instance/generation 和 terminal result 由正式 API/持久化状态核对。
- 失败/竞态：复用已有无 credential、scope、socket replacement、offline、CAS stale、Diagnostic forbidden、
  无增量/无结果测试；新增 Console 退出与第二 Task 领取竞态，失败不得把 dispatch accepted 写成执行成功。
- 资源/平台：测试仅用临时 HOME/DB/UDS/credential、fake user-systemd、唯一 `tmux -L`、长寿命 pane sentinel
  和精确 PID cleanup；密码只走 PTY stdin，token 不进入 argv/log，文件权限核对 0700/0600。
- 证据格式：真实进程 PID、Worker identity/generation、Task/Run/version/cursor、可见安全过程与 final reply、
  pane 0 exit status 和 pane 1/2 保留；静态文档和 fixture 单测不冒充该闭环。
- 范围边界：不操作真实 profile/default tmux/user-systemd/installed binary，不修改 Fleet lifecycle、ADR-006/007
  或真实 Runtime 语义。主计划和 Task 07 front matter保持 `pending`，本 log 为 `active/WAIT`。

#### 主要实现批次

- fake Runtime 的隔离 fixture 新增与 `result_status_sequence` 等长的 `output_sequence`；每个 Turn 先通过正式
  `EventSink` 持久化安全 `turn.output`，再完成 Turn，EventSink 失败时 fail closed。配置限制为 1-256 项、
  每项非空合法 UTF-8 且不超过 4096 bytes。
- Panel SSE 保留原始 Run aggregate 做 Agent ownership 查询，但把 `runtime.*` 安全 transport 归一为
  `aggregate_type=runtime`，避免同一 frame 同时携带互斥的 Run 与 output projection；Normal 继续剥离
  Diagnostic。
- Mailbox 安全 DTO 增加 `kind/lane`。reducer 仅允许 `task/work` 更新 Task 的唯一 WorkDelivery；
  `message/control` 等合法控制 Mailbox 只推进 cursor 和 Timeline，不覆盖任务领取状态。
- Task outcome 与 Runtime reply 在 Timeline 中独立成行；pending Task 的暂存 result 不标为终态 outcome；
  Timeline buffer 复用有界多行 sanitizer，保留结构化换行但剥离控制字符并维持总字节上限。
- 新增默认 profile 隔离用户闭环：临时 HOME/DB/UDS/credential、正式 PTY login、fake user-systemd、唯一
  `tmux -L`、真实 daemon/Worker 进程、正式 Network binding、Console Attach/dispatch/focused steer、
  Normal/Diagnostic 往返和 `/quit`。正式 `Client.Follow` 独立确认三段持久化 safe output；退出后同一
  Worker PID/instance/generation 继续完成第二个 Task。
- README、安装指南和 `/help` 串联默认路径、`console attach`、focused control、Task/Run 状态、
  Task outcome/Runtime reply、Timeline 滚动、Diagnostic/Normal 与 `/quit` 常用流程。

#### 失败与纠正

| 现象 | 根因与裁决 | 纠正及证据 |
|---|---|---|
| 首轮 PTY ready 文案匹配失败 | 实际状态使用小写 `connection connected`；测试断言与产品文案不一致 | 对齐真实状态文案；未改产品连接语义 |
| 两次 dispatch 长期停在 queued | fresh DB 没有 Network binding，Worker 按正式策略拒绝执行；同因 fixture 连续两次后停止旧路径 | 通过 Web cookie/CSRF 正式 test/publish API 建立 `inherit` binding，并核对应用到当前 Worker/generation；未直接修改 SQLite |
| `runtime.turn.output` 被 reducer 拒绝 | 持久化事件使用 Run aggregate，SSE 又附加 Run projection，形成 Run+Output 互斥 frame | transport 归一为 runtime，ownership 仍按原 Run 查询；Panel/Console 回归测试通过 |
| 毫秒级 fake output 未稳定出现在 PTY recorder | tmux/ANSI 重绘不是可靠的每帧历史证据 | 按止损规则改用正式 Follow 确认精确 output/cursor/Normal 脱敏，纯 TUI model 证明同一 DTO 进入可滚动 Timeline；不降低真实 PTY 主流程 |
| focused steer 后 Event 被拒绝 | 新 `message/control` Mailbox 被误当作唯一 `task/work` delivery | 安全投影增加 kind/lane 并 fencing WorkDelivery；控制 Mailbox 回归测试证明推进 cursor 但不覆盖领取状态 |
| 非终态暂存 result 显示为 Task outcome；最终详情在窄 pane 横向截断 | Timeline 未按 outcome state 隔离结果，且多行 summary 又被单行 sanitizer 压平 | pending 不复制 result/error；终态 outcome/reply 独立行并穿过有界多行 buffer；真实 pane 0 已显示两条完整 final reply |
| standalone E2E 两次在 Diagnostic 返回后误报未切回 Normal | fixture 从完整 recorder 历史匹配旧 `> /help`，随后又等待可能滚出 viewport 的提示；实时 header 已显示 `mode normal` | 从 Esc/命令前 offset 等待新输入帧，并以顶部 `mode normal | connection connected` 为权威证据；正式五包与三轮稳定性批次通过 |

#### 独立验证批次

- `go test ./internal/consolemodel ./internal/runtime/fake ./internal/cli/worker ./internal/api/console ./internal/api/panel -count=1`
  退出码 0；最慢 package 16.859s。
- `go test ./internal/cli/console ./internal/client/console ./internal/fleet ./internal/cli/fleet ./internal/worker -count=1`
  退出码 0；Console 29.565s，Fleet 10.665s；包含完整默认路径用户闭环。
- `go test -race ./internal/cli/console ./internal/client/console ./internal/fleet ./internal/worker -count=1`
  退出码 0；Console 31.417s，Fleet 11.951s。
- `go test ./internal/cli/console ./internal/fleet -run 'TTY|Tmux|Workspace|Workflow|Task' -count=3`
  退出码 0；Console 83.693s，Fleet 31.770s。
- `go test ./... -count=1` 退出码 0；全仓 package 通过，最慢 Console 28.570s。
- `go vet ./...`、`bash -n scripts/*.sh deploy/scripts/*.sh deploy/systemd/*.sh`、
  `bash scripts/check-legacy-control-paths.sh --release` 和
  `go build -o /tmp/openagentx-adr009-task07 ./cmd/openagentx` 均退出码 0；构建 SHA-256
  `22664527a459dfd150a37227d7882964b9a4ae8ad74e9f3fe56a7ebbe0c482c9`。
- built binary 的 Console/Fleet help smoke、README/安装指南相对链接、Markdown bash fenced blocks、diff
  secret-pattern scan 和 `git diff --check` 均退出码 0。
- Web 源码未修改；Task 07 未重复运行无影响面的 Web build/test。ADR-009、主计划和 Task 07 front matter
  未修改；Task 08 未开始。

#### 外部状态与 open issues

- 所有 DB/socket/credential/HOME、daemon/Worker、HTTP listener 和 tmux server 均为测试临时资源并由 fixture
  cleanup；未操作真实 `~/.openagentx`、default tmux、user-systemd、installed binary、真实 DB/socket、
  `steadyflow` 父仓，未 push/merge/install/restart。
- 当前无 Task 07 P0/P1 open issue。阶段状态保持 `active/WAIT`，等待实现提交后的监督 gate。

#### Task 07 gate record

- 实现提交：`a5874d70c1209aed52da9b44f68336f04ea40f2c`；17 files，1411 insertions、56 deletions。
- 提交后 feature worktree clean；相对 `origin/main@c3fc1bb` ahead 51。主工作树仍为 clean
  `main@3723c77`、相对 origin ahead 1；`steadyflow` 父仓既有 dirty 现场未修改。
- ADR-009 SHA-256 仍为 `afb7473b6eb353dd06551bb3126fbe3300005e4a1d782b863a9cb1d0f2a32c69`；执行基线
  `AGENTS.md` 为 `b53264590ccb1ebace81668ae3c98f1e29ce8f61384a259f08a5e23b8b92a95b`。
- 监督按默认路径真实用户闭环、正式 Network binding、Task/Run/cursor/Worker 身份、focused control、
  Normal/Diagnostic、最终安全结果、Console 退出后 Worker 常驻与第二 Task、定向 race、三轮稳定性、全仓
  普通测试和 release/secret 检查复核为 `GO`；无 P0/P1、误报成功、secret 泄漏或 ADR-006/007 混入。
- 主计划和 Task 07 front matter 在本 docs-only gate record 同步为 `completed`；Task 08 保持
  `pending/WAIT`。未 push、merge、安装、重启或操作真实 DB/socket/default tmux/user-systemd/父仓。

### Task 08：集成审查与候选门禁

- 开始时间：`2026-09-20`；baseline：`5bebf14`；branch：`codex/adr009-task-console`；worktree：
  `/home/sky/work/touzi/OneAxe/OpenAgentX-adr009-worktree`。
- 正向结果：从默认 profile login/Fleet/OAX Attach 到 dispatch、领取、运行、安全过程、focused control、
  terminal outcome/reply、Diagnostic/Normal 和 `/quit` 的真实隔离闭环；Console 退出后 resident Worker 继续
  完成第二项 Task。
- 状态/CAS/幂等：逐条追踪 Task/Run/Worker/version/cursor、snapshot N/N+1、重复/重连、Follow ack/cancel、
  mode-switch epoch、控制 exactly once 和 Worker replacement/offline fencing。
- 失败/竞态：重跑 projection/ownership/encode/write、auth/scope/token、CAS stale、retention、socket replacement、
  empty/malformed/truncated output、旧 stream 和 compact terminal 故障注入；失败不得跨 cursor 或误报成功。
- 资源/平台：全量普通/race/vet/module/Web/Shell/systemd/release/secret/legacy 检查；所有动态验证只用临时
  HOME/DB/UDS、唯一 `tmux -L`、隔离 daemon/Worker 和 fake user-systemd，不操作真实状态。
- 证据格式：ADR 条款到代码/测试/文档 traceability、命令/退出码/耗时、clean detached 候选 SHA-256、
  `go version -m` 的精确 Git revision 与 `vcs.modified=false`、失败历史和未执行的发布步骤。
- 范围边界：Task 08 只审查、验证并生成 report/log；产品缺陷必须回开所属 Task 单独修复。主计划与
  Task 08 front matter 保持 `pending`，本 log 为 `active/WAIT`；不 push/merge/install/restart/migrate/deploy。

#### 完整审查与 traceability

- 审查范围为 ADR-008 权威基线 `008b2e08923614182811023eddd652df69aa98a9` 到 Task 07 gate
  `5bebf148c414c2ca8bd8d2e6367f3e828b2ec2af`：17 commits、59 files、9100 insertions、353 deletions。
  逐提交 `git show --check` 与完整 `git diff --check` 均退出码 0。
- ADR-009 §1-10、验收矩阵和明确排除已逐项映射到 Task projection/transaction、Runtime/safeoutput、
  reducer、TUI/focused CAS、Diagnostic mode-switch、default-profile E2E、测试和操作文档；矩阵写入
  [Task 08 validation report](../../reports/validation/2026-09-20-openagentx-adr009-release-candidate.md)。
- ADR-006/007 无 diff；`go.mod`/`go.sum` 无 ADR-009 修改；无生成物、credential/token 文件、父仓文件或
  未归属 compatibility fallback。secret-pattern、Console `TurnHandle`、production 禁用 tmux 命令和
  release scanner 均通过；`/foreground` 仍仅为禁用提示。
- ADR-009 SHA-256 为 `afb7473b6eb353dd06551bb3126fbe3300005e4a1d782b863a9cb1d0f2a32c69`；
  AGENTS SHA-256 为 `b53264590ccb1ebace81668ae3c98f1e29ce8f61384a259f08a5e23b8b92a95b`。

#### 无缓存与隔离验证

| 命令/检查 | 退出码/耗时 | 结果 |
|---|---:|---|
| `go test ./... -count=1` | 0 / 31.55s | 全仓通过；Console 30.131s、Panel 19.923s、Fleet 12.191s、SQLite 12.766s |
| `go test -race ./... -count=1` | 0 / 46.00s | 全仓 race 通过；Panel 41.590s、Console 32.148s、SQLite 17.554s |
| `go vet ./...` | 0 / 0.72s | 全仓通过 |
| `go mod verify` | 0 | `all modules verified` |
| 受影响 Panel/Console/Fleet/Auth/Runtime/safeoutput 定向 race | 0 / 42.68s | 全部通过 |
| `npm run test:observation` | 0 / 0.39s | 4/4 通过 |
| `npm run test:pwa` | 0 / 0.29s | 通过 |
| `npm run build` | 0 / 3.49s | 266 modules；production build 通过 |
| Shell syntax + Worker template static test | 0 / <0.1s | system/user unit 静态约束通过 |
| `systemd-analyze --user verify <temp>/openagentx.service <temp>/openagentx-worker@.service` | 0 / 0.08s | 按实际安装名通过；临时目录已清理 |
| `bash scripts/check-legacy-control-paths.sh --release` | 0 / 0.14s | 全部类别 `CLEAN` |
| changed Markdown relative links + README/install-guide bash fences | 0 | 通过 |

- `TestIsolatedDefaultProfileWorkflowShowsTaskProgressAndKeepsWorkerResident` 退出码 0、26.68s：临时
  HOME/DB/UDS/credential、正式 PTY login、fake user-systemd、唯一 `tmux -L` 和真实隔离 daemon/Worker
  完成 login/Fleet/OAX Attach/dispatch/领取/运行/safe output/focused steer/终态 reply/Diagnostic/Normal/
  `/quit`；同一 Worker PID/instance/generation 随后完成第二项 Task。
- `TestIsolatedTTYSmokeUsesAltScreenBindsAndPreservesExtraPanes` 退出码 0、2.52s：alt-screen、真实输入、
  绑定、pane 0 exit 0 和 pane 1/2 保留均成立。
- Task snapshot N/N+1 两项事务测试退出码 0；SSE ownership/projection/encode/write 故障组退出码 0、
  8.916s，证明 Task/Run/runtime/Worker/Mailbox/Message/Approval 和 backend 查询失败不发送失败 frame、
  不跨 cursor，并可从 last-applied 恢复。
- CLI Token/schema/credential 五包退出码 0、4.26s；Runtime/safeoutput/Worker 包退出码 0、7.59s；
  reducer/TUI focused CAS/Diagnostic/compact 定向退出码 0；Fleet/tmux/user-systemd/graceful 两包完整测试
  退出码 0、11.11s。
- candidate help smoke：Console/Fleet/Attach help 均 code 0；已删除 `console status`、`attach --once`、
  `console dispatch` 均 code 2。所有测试资源均为隔离 fixture，未操作 default tmux 或真实服务。

#### 失败与纠正

| 现象 | 根因与裁决 | 纠正及证据 |
|---|---|---|
| 首次 Web build 为 `vite: not found` | worktree 未安装 lock 对应依赖，不是产品测试失败 | `npm ci --no-audit --no-fund` 安装 123 packages；只重跑 build 后 exit 0，保留首次失败 |
| 首次 systemd 临时命令被执行安全策略拒绝 | cleanup 使用递归删除形式，unit 尚未复制或验证 | 改用唯一 `mktemp -d` 与精确 `unlink`/`rmdir`；verify exit 0 |
| ad-hoc 禁用路径 scan 首次命中 `TurnHandle` | 错把 Worker/Runtime 正式接口纳入 Console 边界 | 收紧到 Console client/model 与 Fleet/Console tmux 协调；正式 release scanner 未放宽且 exit 0 |
| submodule linked worktree 候选没有 `vcs.*` | 共享 Git 配置保留原 `core.worktree`，Go 1.22 未写 stamping | 删除两个无 provenance 产物；clean standalone clone 使用 `-buildvcs=true` 重建并通过 provenance |
| `/tmp` 不支持 `gio trash` | internal mount 无 Trash | 核验 clone 精确路径、clean HEAD 后限定路径清理；无临时 candidate worktree/clone 残留 |

#### 候选与外部状态

- clean detached standalone clone at `5bebf148c414c2ca8bd8d2e6367f3e828b2ec2af` 使用以下命令构建，
  退出码 0、4.41s：

  ```bash
  go build -buildvcs=true -o /home/sky/.cache/openagentx-builds/openagentx-adr009-5bebf14 ./cmd/openagentx
  ```
- 最终候选为 19523320 bytes，SHA-256
  `e482b113311ba618ac45512bf3098d431e739ab61bfc681ec3d2f90f483300c2`；cache directory/file 均为
  `0700 sky:sky`。`go version -m` 为 Go 1.22.4、`vcs=git`、精确 revision `5bebf148...`、
  `vcs.modified=false`。临时 linked worktree、standalone clone 和 probe binary 已清理。
- feature 提交前为 `5bebf14`，相对 `origin/main@c3fc1bb` ahead 52，无 remote feature ref；OpenAgentX main
  clean `3723c77`、ahead 1；ADR-008 worktree clean `008b2e0`。`steadyflow` 父仓 `d3fd774` 的既有大量
  dirty/ahead 现场只读核验，未修改、暂存或提交。
- 本轮未获 Task 08 所要求的单独真实主机只读核验授权，因此未读取或操作真实 service、installed binary、
  DB、socket、credential、Linger、Worker unit 或 default `OAX`。此项不冒充已通过的现场验收。
- validation report 结论为 `passed-candidate`；当前无 P0/P1 open issue。Task 08 继续 `active/WAIT`，
  主计划与 front matter 继续 `pending`。本轮只提交 report + execution log；不 push/merge/install/restart/
  migrate/deploy，等待最终监督 gate。

#### Task 08 最终门禁复核批次（2026-09-22）

- 前一目标轮次分类为 progress：提交了 `8e62f8c390743b660db2d0a9f07dce37a75da2f4`，交付候选验证报告。
  本次从当前 clean feature HEAD 继续；用户已明确授权 ADR-009 连续执行到完成，新版 AGENTS 第 4/5 条
  要求复用未受影响证据并由主代理裁定收口。门禁复核据实际证据作出，不冒称外部监督者另发 GO。
- 本批预算：20 分钟、一次有界复核。只核对 Task 08 验收覆盖、独立验证代理的 E2E 断言审阅、候选
  provenance、文档/状态/链接/路径及必要补验；不重跑未受影响的全量测试、不扩大产品范围。
- 验收：`5bebf14..8e62f8c` 仅 report/log，既有普通/race/E2E 证据可复用；候选 hash/revision/权限与报告
  相符；Task 01-07 均已 gate；复核通过后以 docs-only Task 08 gate 记录精确 validation SHA 和实际状态。
- 用户询问为何没有在日常 `OAX:quote-service.0` 看见测试。已说明：隔离测试使用独立 `tmux -L` server，
  其中也有 `OAX:quote-service.0`，但不属于默认 tmux；daemon/Worker 为真实隔离进程，Runtime 使用受控
  fake Adapter。该证据不覆盖生产 `quote-service`/外部 AGY 的现场回复，也不表示候选已安装。
- 现场部署/验收仍是单独的外部操作阶段。保留用户原始任务工作台目标，完成候选门禁时明确列出这一
  未执行项，不将其写成真实现场 PASS。

#### Task 08 gate record（2026-09-22）

- validation 提交精确 SHA：`8e62f8c390743b660db2d0a9f07dce37a75da2f4`，2 files、262 insertions、
  1 deletion；复核前 feature clean、相对本地 `origin/main@c3fc1bb` ahead 53。该提交只有 report/log，
  不改变已验证候选 `5bebf148c414c2ca8bd8d2e6367f3e828b2ec2af` 的任何产品、测试或依赖文件。
- 唯一独立验证代理完成只读证据复核：真实隔离 daemon/Worker/Unix control plane/tmux/PTY 与 fake
  Runtime/systemctl 的边界准确；dispatch、waiting_input、同 Worker Run、安全输出、focused steer、
  最终回复、Diagnostic/Normal、quit 与第二 Task 断言成立；snapshot/reducer/CAS/Diagnostic 映射成立。
  未复跑测试或启动真实资源。该结论只用于候选裁定，不代表生产或外部 Runtime PASS。
- 主代理依据用户连续执行授权、新 AGENTS 第 4/5 条及以上证据裁定 `GO`。复用 2026-09-20 全仓普通/race、
  vet/module、Web、systemd、release 和隔离 E2E 结果；补充逐文件 `bash -n` 全部 exit 0。
  原 `bash -n scripts/*.sh ...` 只解析第一个参数所指脚本，本次以循环逐文件补齐语法证据。
- 候选实测 hash 仍为 `e482b113311ba618ac45512bf3098d431e739ab61bfc681ec3d2f90f483300c2`；
  `go version -m` 确认 `vcs.revision=5bebf148c414c2ca8bd8d2e6367f3e828b2ec2af`、
  `vcs.modified=false`；大小 19523320 bytes，目录/file 权限 `0700 sky:sky`。ADR/AGENTS hash 未变。
- 为回答用户为何没有在日常 pane 看到测试，新增一次只读现场版本核验：
  `systemctl --user show ... --property=Id,ActiveState,SubState,MainPID,NRestarts`、安装文件和三进程
  `/proc/<pid>/exe` 的 SHA-256/Go metadata、对精确 `OAX:quote-service.0` 的只读 `tmux display-message`。
  所有命令 exit 0，未读取 pane 内容、凭据或真实 DB，也未注入输入/改变服务。
- 实际 daemon PID `486159` active/running、NRestarts `0`；Worker PID `486788` active/running、
  NRestarts `5`；Console pane 0 PID `313504`、`pane_dead=0`。安装文件与这三个运行进程均为
  `f49cec4ed31a0f63e82626f1de8d6332baca205d`、`vcs.modified=false`，hash 均为
  `984bb0df415b18f49959eda474076f0c807d90e6b5392342083249e605771114`。这证明现场尚未运行 ADR-009，
  不证明生产任务流程通过。本条是新一次只读核验，不追改前轮“未进行现场检查”的历史记录。
- 非阻断覆盖边界：CAS conflict、Diagnostic forbidden/旧流主要以模型/handler 测试注入，没有把它们
  扩大为本轮生产故障注入；当后续改动跨越这些边界时重新评估。外部 Runtime 与默认 OAX 的现场验收
  是下一次发布授权后的验收内容，不能用 fake Runtime 代替。
- 主计划与 Task 08 front matter、本 log 总体状态同步为 `completed/GO`，含义限定为开发、隔离 E2E
  与候选验证完成。完整用户现场交付仍未验证；保持未安装/未部署，不将整体目标谎报为现场可用。
  本 gate 只改主计划、Task 08、report 和 log 四个 Markdown 文件；保留全部历史失败，无源码变更。

## 2026-09-22 授权安装与真实现场验收

- 用户在候选门禁后明确回复“授权安装”，覆盖已提出的候选安装、当前任务自然结束后更新 daemon/Worker/Console、
  真实 `OAX:quote-service.0` 验收。无需再次请求同一安装授权；仍不 push/merge 或改父仓。
- 本批预算：60 分钟，一次安装批次和一次现场验证批次；异常先有界诊断，不自动重试业务任务或扩展 ADR-006/007。
- 正向验收：已验证 `5bebf14` binary 原子安装；三个运行进程 hash/provenance 一致；正式 API 与真实 pane
  共同证明低副作用 Task 的领取/运行/终态/最终回复，第二项任务证明 resident Worker 连续执行。
- 不变量与失败边界：普通 stop 使用持久化 graceful intent；不强停活动 Run，不用 tmux send/paste/capture；
  保留其他 window/pane/未知进程。先备份旧 binary 与一致 SQLite 备份，失败保留现场并使用已确认的回滚边界。
- 资源证据：schema/quick_check、canonical 配置与 unit hash、Worker identity/generation（不记录 fencing）、
  监听地址、权限及 Console 退出不影响 Worker。没有 Runtime 增量时只报告等待最终结果，不虚构过程。
- 首轮预检：candidate SHA-256 与既有证据相符；CLI credential/session 有效；quote-service online generation 50，
  systemd daemon/Worker active。SQLite quick_check=ok/schema_meta=1。
- 辅助查询纠正：对可选 `quote-service.env` 执行 hash/stat 返回 1；unit 明确 `ignore_errors=yes`，该文件
  不存在是现有合法配置，未创建。两次源码查询误把 Go import alias 当目录（`internal/api/openapi`、
  `internal/console`），rg 返回 2；仅查询错误，无产品或现场修改，已按真实路径继续。
- 发现既有网络 binding 的 applied generation=47，而当前 Worker generation=50；作为真实执行必要条件
  有界核验，不能把 online/queued 写成执行成功，不直接改数据库或实现代际继承。

### 安装结果与现场证据（2026-09-22 16:04–16:12 CST）

- 已验证候选原子安装至 `/home/sky/.local/bin/openagentx`：revision `5bebf148c414c2ca8bd8d2e6367f3e828b2ec2af`，
  SHA-256 `e482b113311ba618ac45512bf3098d431e739ab61bfc681ec3d2f90f483300c2`，`vcs.modified=false`。
  旧 binary、SQLite 一致备份、unit/config/manifest/release evidence 保留在私有目录
  `/home/sky/.openagentx/backups/adr009-20260922-DTkpFj`，backup quick_check=ok/schema=1。
- graceful `fleet down` exit 0，约 1s 从 stop pending 到 offline，Worker exit status=0。精确确认旧 Console
  PID313504/installed hash 后 SIGTERM，只使该 pane 自身正常退出；不 kill pane/window/session。
- daemon stop → 同目录临时 binary+hash/fsync+atomic rename → start → schema verify 全部 exit 0。
  daemon 16:04:14 启动 PID830503；`fleet workspace --respawn-dead`、`fleet up` exit 0；Worker PID835790，
  instance `worker-cf83eb66-4b84-4453-84b9-20fc1024d3eb`、generation 51。三个运行进程 hash 与候选一致。
- 默认 tmux server 的真实 `OAX:quote-service.0` 已通过 PTY tmux client 观察与输入：焦点原 Task、queued/
  Mailbox 状态、`/status`、`/diagnostic` 原地切换及 `/normal` 成立。`/quit` 后 pane dead/status=0，Worker
  同 PID/gen 继续在线；正式 workspace 命令恢复 Console PID870030。本次 client 以自身 C-b d 正常 detach。
  未使用 tmux send/paste/capture，未修改其他 window/aux pane。
- 真实原 Task `task-3abf6b7c-9b41-4238-b5b2-044f6627d70e` 仍 queued/version1，Attach primary=unavailable。
  已有 applied generation47 与 current51 不符；升级前为 current50，说明这是既有运行条件。CLI Token
  没有网络管理 scope，检查用 Web 未登录；向用户请求通过 owner Web 会话对现有 inherit 模式先测试再应用。
  安装已完成，但尚不宣称真实 Runtime 回复/连续执行通过；不改 DB、不扩 CLI 权限、不实现 ADR-007。
- health=ok、监听仍仅127.0.0.1:18100、SQLite quick_check=ok/schema1；Worker YAML/manifest/Wrapper hash
  未变，unit/Web assets 未替换，release.txt 已更新且0600。未 push/merge、未改 main/父仓。
- 临时 Go 验证器复用正式 credentialstore、installation probe、CLI Session 和 Console client，仅输出安全
  白名单；源文件已从 feature 删除，私有 backup 保存可复现 source/binary。无密码/token/fencing进入证据。
- [安装报告](../../reports/validation/2026-09-22-openagentx-adr009-live-installation.md) 记录命令、失败/纠正、
  provenance、验收边界与待完成项。只增加本日志与报告，不重跑未受影响的全仓测试。
- 一次独立只读验证确认 installed hash/provenance、三个进程相同 inode、daemon/Worker active/running 与
  NRestarts=0、恢复后的 pane0/name/markers 正确；源码复核确认 generation fencing 与 CLI/Web 网络授权
  边界。验证代理未读 DB 或调用 Runtime，未把部署版本证据扩大为业务闭环。5 个相对链接、diff check、
  两文件边界与冻结 ADR/AGENTS hash 检查通过。
- 外部 HTTPS 补验：首个 curl 继承 shell 代理且未限定超时，持续无返回；精确终止本次 PID893114。
  显式 no-proxy/connect-timeout10/max-time20 的同 endpoint 请求约0.66s exit0、HTTP200/health ok；
  真实浏览器同请求也HTTP200。记录探针环境限制，不改全局代理/Worker策略，不写代理值或凭据。

### 2026-09-22 16:48 恢复现场验收：保存提示不等于应用回执

- 用户报告“保存成功”后恢复必要核验，产品代码/安装 binary 未变；正式 Attach 仍显示 gen51 online、
  primary unavailable、旧 Task queued。只读 binding 仍为 version5/applied gen47。
- 新 inherit 测试于16:48:09成功（gen51/binding revision5），但正式命令记录和 Event Journal 中只有
  mode_test，无本次 mode_publish / binding pending / apply。没有将用户看到的提示写成后台应用成功。
- 有界源码审查定位 `NetworkSettings.handleSaveAndApply` 的旧 fallback：当最新匹配测试尚未成功时，
  仅检查 mode 和 desired_status=applied 就提示“当前 Runtime 已经生效”，缺少 applied Worker/generation
  对齐。这是误导性成功提示；现有严格服务端 fencing 没有被绕过。
- 当前最小处置为：核实最新测试已成功后，请用户在其现有 owner Web 会话刷新数据，仅重发一次保存/应用；
  主代理以正式 API/Worker 回执判定，保留原绑定，不扩 CLI scope、不改 DB、不自动实现 ADR-007。
  如果同一路径仍失败，停止让用户重复尝试，记录实际阻断并另行收敛页面修正。
- 已恢复真实 pane0 观察和一次60s正式 Follow，等待网络绑定恢复后再创建低影响验收 Task，避免再堆积 queued。
- 16:55预定检查点仍为 binding version5/applied gen47，最近 mode_publish 仍在9月11日；正式 Follow
  只观察到旧 Task 的 Mailbox claim，未观察到 Run 创建，不标记 E2E 通过。已向用户说明实际未应用并给出
  一次刷新后保存的纠正步骤；当前验证器没有可用 Web owner session，不能通过 CLI 越权执行网络发布。

### 2026-09-22 17:04 保存误报的最小页面修复

- 用户再次确认刷新并保存后，只读检查仍是 binding version5/applied gen47、current gen51，最近成功
  inherit test 为16:57:09；没有本轮 mode_publish。正式 Attach 仍 primary unavailable，原 Task 未运行。
- 按 AGENTS 第3条将此裁定为本轮主流程阻断/误报成功的最小修复：只修 Web 的目标身份、应用回执显示与
  测试/发布判断，不改 Go 授权、Worker fencing、网络继承或 ADR-006/007。不再要求用户无依据重复点击。
- 验收矩阵：正向为当前目标成功测试产生一次正式 publish/CAS；不变量为 applied 必须匹配
  agent/backend/Worker/generation/binding revision；失败覆盖旧代回执、pending/failed/stale 测试、
  离线/无写权限和版本冲突；浏览器覆盖实际组件、刷新与桌面/窄屏。隔离 UI 证据不冒充真实 Runtime E2E。
- 本批预算45分钟，一次主要实施与一次独立验证；真实 Web owner 会话仍由用户持有，不提取 Cookie，
  不扩大 CLI scope。部署仅使用既有安装授权范围内、验证后的 Web 静态产物；若仍需用户确认发布，
  在已修复页面提供可核对证据后再说明，不能绕过正式 API。
- 查询纠正：首次只读 SQLite 使用错误路径 `.openagentx/openagentx.db` 返回1，未创建/修改数据库；
  已从真实 ExecStart 确认 `.openagentx/data/openagentx.db` 后以 readonly/query_only 查询。源码定位中
  错误的 ADR 文件名和 `internal/service` 路径返回非零，已按 rg 文件清单纠正，无产品副作用。

#### 页面修复与独立验证（2026-09-22 17:04 起）

- 真实 Chrome、原版本组件（`087aeb3`）与临时 localhost fixture 复现：gen51 + gen47 applied + pending
  test 时点击保存，出现“当前 Runtime 已经生效”，请求计数0。切换为同目标成功测试并刷新后，原版本
  本身能够发送一次 `network-bindings/mode/publish`；因此未取得用户浏览器请求记录前，不声称完全还原
  用户再次保存的具体操作/数据时序，不把 fixture 当生产发布。
- 实现新增纯 `network-binding-state.js`：只有精确 agent/backend/Worker/generation/binding revision、
  mode 与 policy/profile 回执才视为当前应用。旧回执以历史 Worker/gen 明示并用警示色；提交成功只说明
  等待回执。进行中测试提示等待完成，当前成功测试明确可保存；不自动重测/发布，不修改服务端规则。
- 同一浏览器验证修复后：pending 保存0请求且不再误报；刷新取得成功测试后一次正式路径请求，携带
  test/Worker/gen51/expected_version5；应用前保持警示，精确当前回执后才为绿色；再次保存可幂等提示。
  离线与无写权限禁用写按钮；注入409提示刷新、不报成功，显式重试复用原 Idempotency-Key。
- 窄屏390px发现原目标 select 的 min-content 导致页面521px横向溢出；裁定为本次回执/目标可读性的
  最小伴随修正，仅加 select 收缩与状态换行5行CSS，修复后 bodyWidth=390，真实截图检查可读。桌面
  与窄屏使用同一组件，没有用静态源码检查代替浏览器。SSE/auth/Go/Console不受本次Web修复影响。
- 独立验证代理：`npm run test:network` exit0/0.43s（7/7），`npm run test:observation` exit0/0.45s
  （4/4），`npm run test:pwa` exit0/0.29s，`npm run build` exit0/1.59s，`git diff --check` exit0/0.01s；
  正式 Observe/Publish/Worker receipt JSON字段与helper匹配，未发现本轮阻断。release scanner exit0。
- 工具失败保留：临时Vite首次因系统watch资源不足EMFILE退出，改为仅该fixture禁用watch；虚拟JSX的
  两次导入/解析失败后停止该路径，改用apply_patch复制原组件到唯一临时目录成功。watch关闭后需要重启
  本次fixture才能加载新源码，已执行；没有更改系统watch限制或真实Web。若干rg不存在路径/Makefile仅
  查询失败，已按文件清单纠正。测试数据无密码/token，临时服务只监听127.0.0.1。
- 真实现场尚未发布/应用gen51；仍保持安装通过、Runtime闭环blocked。后续仅按授权安装本次可追溯
  Web产物，正式发布继续由已有owner Web会话执行；CLI Token没有网络权限，不绕过该边界。
- 证据措辞澄清：真实DB中没有本轮已接受的mode_publish记录，只能证明发布未落地；未取得用户浏览器
  network trace，不能排除请求曾被服务端拒绝。“0请求”严格指上述隔离浏览器复现，不外推到用户点击。

#### Web修复提交与安装（2026-09-22 17:15–17:17）

- 实现提交 `77ae6785eb33e30b29155dcc580a083fd79e5cf2`；精确6文件：Web组件/helper/test/CSS/package与
  本日志。提交后feature clean；无Go/schema/auth/网络代际规则修改，不push/merge。
- 最后将“测试成功，可保存”文案限定到精确当前inherit/direct测试，未扩大named_profile成功判定。
  仅重做最终Web build；dist晚于最后源码修改。JS SHA-256
  `56d5dedce662f81a0c5197b1d4cf70b199fbac7e1886df98d99bf6126feae767`，CSS SHA-256
  `76f34cdd88c2df1f079bc49d97ab2b68eea1e955ecaaa117396a6c9556227e8f`。
- 按现有安装授权备份原Web到私有`web-77ae678-9I5u5B/web`，只以同目录唯一临时文件、0644、cmp、
  fsync+rename更新两静态资源。正式staticHandler逐请求读取文件，未修改服务配置/二进制，不需要重启；
  daemon/Worker PID及NRestarts=0、gen51保持原值。release.txt保留Go provenance，另记Web revision/hash。
- 本地HTTP、公网HTTPS curl及真实Chrome分别取得200资源并核对上述两hash一致；无缓存旧包冒充更新。
  页面内刷新只更新API数据，用户需整页刷新加载新JS。检查用真实Web仍未登录，不能越权代发发布。
- 17:16正式Attach primary仍unavailable，原Task仍queued，保持Runtime验收blocked。完整失败/证据与
  回滚路径见[安装报告](../../reports/validation/2026-09-22-openagentx-adr009-live-installation.md)。
- 收尾：独立验证确认最终dist两hash与其build产物完全一致；6个相对链接、whitespace和两文档路径边界
  检查通过。已关闭本次fixture浏览器页和localhost服务，截图/fixture保留在唯一临时目录供复核。
  main仍clean/ahead1于3723c773，未操作父仓；源码修复已提交，安装记录另作docs提交，不push。

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
