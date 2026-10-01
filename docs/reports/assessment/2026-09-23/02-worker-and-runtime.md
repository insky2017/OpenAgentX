# 02 — Worker 与 Runtime 评估

- **结论：常驻执行骨架成立，核心结果和继续交互能力明显弱于外围框架。** 本次先完成两项隔离集成任务，再完成 Chrome → 隔离 daemon → 真 AGY → 文件副作用 → 下一任务的完整联合链；两条链都准确写入并读取文件，同链 Worker 持续在线、事件和 RunAttempt 可追溯。但四项 Task 都是 `uncertain/business_effect_unverified`，平台没有把此次独立核验纳入 Task 结果的产品入口。
- **已有一条明确范围的真实浏览器 E2E，不能外推全部功能通过。** 首轮集成链使用身份 fixture；补充联合链通过正式 `agent apply`、已登录真实 Chrome 的网络测试/发布与任务提交、真实 AGY 工具执行、页面回复和独立文件核验，连续两项完成。它运行在隔离 daemon/DB/workspace，不是生产 E2E；取消、断线恢复和同 Task 多轮仍未获本次真实链验证。
- **已复现两项可装配 Adapter 缺陷：** CodeBuddy 空输出且退出 0 被标为 Runtime `succeeded`；执行规格设置 40ms 超时仍运行约 255ms 后 `succeeded`。现有全部相关 package 测试和定向 race 均通过，说明测试断言缺失。
- **优先修通一个真实用户闭环，再扩适配面。** 保留持久 Mailbox、lease/fencing、事务和脱敏；收敛未交付 ACP、重复注册抽象和能力声明。不能将“给每次成功都盖 uncertain”当成完整业务核验设计。
- **本批只评估。** 无产品代码、冻结 ADR、正式配置、服务或数据库修改；无修复 commit。ADR-006 的新 intent 工作未安装，不计当前已交付能力。

## 1. 基线与验收边界

源码固定为 `/tmp/oax-assessment-20260923-source`，HEAD `34053c0`；Go 代码对应已安装 `6d599ac`。本模块开始于 2026-09-23 00:26（Asia/Shanghai），首次评估预算 35 分钟。主代理于首轮报告落盘后增加浏览器联合链，单独限制最多 20 分钟或同因失败两次；实际约 00:40–00:45 完成并清理自有 Worker。

| 本轮承诺 | 证据及结果 | 边界 |
| --- | --- | --- |
| 真实请求能经 Worker 执行并产生低影响副作用 | 两条链各两项真 AGY Task：A 写文件，B 只读；内容、长度、SHA 独立验证通过 | 首轮身份 fixture；联合链正式注册且浏览器提交；两者均隔离，不是生产 E2E |
| 完成后继续领取下一项，无终端注入 | 首轮同 Worker `worker-eb00ba54…` gen 1；联合链同 Worker `worker-ebf9cff7…` gen 1；各自两个 Run/Task 生命周期齐全 | 两个不同 Task；不是同 Task 多轮/会话恢复实测 |
| 空闲持续等待，不 busy-loop | 6 秒空闲共 20 个 HTTP 请求；心跳 1s，mailbox/control wait 5s | 请求频度验证，未做 CPU/内存长时 soak；正式缺省心跳为 10s |
| 状态与物理结果区分 | 两条链均 Run succeeded、Task uncertain，文件核验成功 | 不把外部报告中的核验写成产品内成功 |
| Session/stream/failure/control 边界 | 相关 package/race 通过；AGY 本次 Journal 有 init/step_update/result、两次 session_binding.saved | 当前真实 cancel/lease丢失/断线恢复未重测；这些证据本次主要来自 fixture |
| CLI 契约与真实环境可追溯 | 正式 wrapper SHA、native/helper SHA、version/help、正式 Worker proxy 环境键名有记录 | 不输出环境值/凭据；运行环境是复用当前凭据的隔离请求 |

## 2. 核心用户路径实际表现

| 用户动作/预期 | 已确认的现状 | 判定 |
| --- | --- | --- |
| 发送一个工作请求 | CommandService 创建 Task 和 Mailbox；Worker 自动领取，经正式装配启动真 AGY | 首轮集成链及补充浏览器联合链成立 |
| 看到执行过程 | 首轮 46 个、联合链 51 个 `runtime.agy.step_update`，各有 init/result；每项 started/finished/settled 齐全 | Runtime→Journal 与补充真实浏览器页面成立；截图见 03 报告 |
| 获得可信结果 | 首轮文件 `OAX-ASSESSMENT-20260923\n` 为 24 bytes；联合链文件 `OAX-WEB-E2E-20260923\n` 为 21 bytes，B 回复与最终文件匹配 | 人可核验，平台 Task 仍 uncertain；闭环不完整 |
| 继续下一项新 Task | B 被同一在线 Worker 自动执行 | 成立 |
| 对完成的 Task 继续对话 | uncertain 是终态，常规 supplement 不能当成 session continuation | 当前默认真 Adapter 路径基本不支持成功后的自然追问 |
| 执行中补充一句，期待下一轮处理 | AGY/CodeBuddy 声明 queued steer；但普通成功因未知副作用变 uncertain，已排队消息会 superseded | 安全规则有测试，却削弱真实用户主路径，见 RUN-01 |
| 更换 Runtime/保留同样能力 | AGY 支持 new/resume；CodeBuddy 仅 new；ACP 不在 Worker CLI 装配中 | 统一接口不代表等价产品能力 |

独立文件核验 SHA-256：`ec6c45e643137647f577f8d6a62cb1e94768bf6db9809f77725c8525961153ff`。两项分别约 19.018s、19.019s；真实测试总体 48.935s。

## 3. 实现较好的部分

- **Worker 与模型进程生命周期真正分离。** `internal/worker/runner.go:85` 将 heartbeat、health、network work、Mailbox、RunManager、control 分开运行；长 turn 不阻塞心跳/控制。RunManager 单 active turn 与 drain admission 锁（`internal/worker/run_manager.go:78`、`:89`）符合单 Agent 工作区顺序执行的需要，不是应删掉的复杂度。
- **持久状态承担真相，内存 broker 只唤醒。** `internal/controlplane/worker_service.go:326` 的 claim 先查持久队列、订阅后再查，避免查询与订阅间丢唤醒；本次真实连续任务证明不需要 tmux/pane 输入。
- **AGY 适配是实际接线而非接口壳。** stdin stream-json、明确 `--print ""`、model/effort/conversation、进程级 timeout 均有实现（`internal/runtime/agy/adapter.go:218`）；真实 wrapper 1.2.8 本次跑通。解析器拒绝空流、坏 JSON、缺终态、多终态和成功同时带错误（`internal/runtime/agy/stream.go:93`、`:111`、`:131`）。
- **真实效果未知时不自动重试，防重复副作用。** 外部 Runtime 的自报只写 `RuntimeSideEffectsKnown`，不自动变权威 `SideEffectsKnown`（`internal/runtime/agy/stream.go:67`）。CAS/lease/fencing 和 safeoutput 仍应保留；问题在缺少可用的收口规则，而非应该信任模型说“完成”。
- **进程清理有专门处理。** AGY 取消兼顾 mgraftcp tracer/tracee 与脱离进程组的后代，CodeBuddy 使用独立进程组；本次定向 race 和已有子进程取消测试通过。但不把 fixture 成绩说成本次真实取消 E2E。

## 4. 缺陷与最小处置

### RUN-01 — 真实成功缺少 Task 收口路径，连带吞掉 queued follow-up（P1，当前核心流程）

**触发：** AGY/CodeBuddy 正常完成、没有独立业务核验来源。即使请求只是只读问答，`FinishRun` 仍按同一规则处理。

**用户后果：** 能看到回复却总是“不确定”；Task 已终结，不能自然追问；运行中已接受的 queued supplement 也不会续执行。这既让用户怀疑任务是否完成，也迫使新建 Task，弱化已持久化的 SessionBinding 价值。

**证据：** `internal/persistence/sqlite/worker_execution_repository.go:628` 将 succeeded+unknown effects 改为 uncertain；`:637` 仅给 succeeded/failed 的 pending message 留 `waiting_input`。`internal/persistence/sqlite/control_input_repository_test.go:303` 的测试名即 `TestUnknownSuccessfulEffectsSupersedePendingMessageWithoutReplay`，证明这是明确的安全策略而非本次猜测。此次两个真实 Task 均复现 uncertain，见 `evidence/runtime-live-test.txt:7`。

**最小处置：** 先定义并交付只读 query 的完成标准、action 的核验/人工确认入口以及 queued follow-up 的明确处置。不能将 Runtime 自报升级为业务核验，不能不加分辨自动重跑。ADR-006 正在处理相关意图语义，只能记为后续依赖，不能抵消当前缺口。

### RUN-02 — CodeBuddy 不执行已解析的 turn 超时（P1，启用 CodeBuddy 时）

**触发：** 用 CodeBuddy 执行时间超过 `Execution.Spec.Timeout` 的任务；Worker 本身仍正常运行。

**用户后果：** 预期有上限的任务可继续占住唯一 active slot，后续 Task 等待；预算/超时记录与真实控制不一致。

**证据：** `internal/runtime/codebuddy/adapter.go:203` 用 Worker 的 `ctx`，没有 `context.WithTimeout`；`internal/worker/run_manager.go:189`、`:195` 也直接传 Worker ctx。定向 fixture 40ms timeout → 255ms 后 succeeded，`evidence/runtime-defect-probes.txt:7`。AGY 在 `adapter.go:237` 正确创建 turn timeout；ACP `adapter.go:151` 有相同源码缺口，但尚未 CLI 装配。

**最小处置：** 对 CodeBuddy 使用 turn deadline 并沿用进程组清理，覆盖超时后新任务可执行。无需先重构整个 Worker。

### RUN-03 — CodeBuddy 空输出/退出 0 被误报为 Runtime 成功（P1，启用 CodeBuddy 时）

**触发：** CLI 退出 0，但没有任何 stdout 结果；或者仅 stderr 带诊断而 stdout 为空。

**用户后果：** Run 显示 succeeded，实际没有可用答复；Task 最后仍被 unknown effects 保护为 uncertain，但过程状态与诊断误导排查，不能据此宣称 fail-closed 兑现。

**证据：** `internal/runtime/codebuddy/adapter.go:279` 的 default 直接设 succeeded，`:303` 接受空正文，stderr 仅在非零退出分支被使用；`runtime/contract.go:243` 也不拒绝空成功。空进程 fixture 实测 succeeded、result_bytes=0、error_nil=true，`evidence/runtime-defect-probes.txt:4`。

**最小处置：** 明确采用 CodeBuddy 结构化终态协议，或先在当前 text 协议拒绝空成功并保留有界脱敏诊断；追加最小失败断言。不要用 Task 最终 uncertain 掩盖 Adapter 层假成功。

### RUN-04 — 注册的 Agent 角色文件没有进入统一 Runtime 输入（P1，源码确认）

**触发：** 用户注册 Agent 的 `instructions_path`/ROLE.md，期待其长期职责决定执行行为。

**用户后果：** 平台维护并校验角色文件，但实际 turn 不确定性地缺少该角色内容；不同 Agent 可能只是不同标签/工作目录。原生 CLI 自己发现 AGENTS.md 或用户配置，不等价于加载平台注册的 ROLE.md。

**证据：** `internal/cli/admin/command.go:304` 校验 instructions 文件；`internal/runtime/contract.go:209` 的 TurnRequest 无 profile/instructions；`internal/controlplane/worker_service.go:501` 仅填 Task/Run/Messages/Execution/Session；AGY `adapter.go:308`、CodeBuddy `adapter.go:244` 的 prompt 构造无角色加载。全仓 `InstructionsPath` 引用只出现于注册、验证、持久化和测试，未发现 Worker 消费入口。

**最小处置：** 选一个真实 Adapter，将固定版本/哈希的 Agent instructions 与 workspace 来源接入执行快照；用只存在于 ROLE.md 的无害 marker 验证注入。本次未做模型是否偶然读取角色文件的黑盒断言，因此结论严格为“平台没有确定性注入链路”。

### RUN-05 — 失败有状态，没有足够可操作的原因（P2，当前排障流程）

**触发：** wrapper 环境、工作目录、identity 或启动前 Validate 失败；或者 Health 失败。

**用户后果：** 用户看到 unavailable 或 `Runtime Backend could not establish a controlled turn`，无法区分应修凭据、模型、路径还是网络，容易反复重新发布/重启。

**证据：** `internal/worker/backend_pool.go:398` 将任意 Health error 丢弃为 unavailable；`internal/worker/run_manager.go:201` 在 Finish 成功时吞掉原 cause，仅持久化固定文字。AGY Health 本身只是 wrapper `--version`（`adapter.go:185`），不验证认证与真实模型请求，所以 healthy 也不等于可执行。历史现场有同样固定错误，`docs/reports/validation/2026-09-22-openagentx-adr009-live-installation.md:218`。

**最小处置：** 保留稳定失败阶段/原因码和有限脱敏原因，区分“程序可启动”和“最近实际 turn 成功”；界面直接提示下一项可操作检查。不要把原始 stderr、环境值或凭据写入公开 Journal，也不要求每次心跳都发真实模型请求。

### RUN-06 — ACP 的通用声明超出交付能力，且矛盾终态未拒绝（P2，未交付支路）

**触发：** 按架构说明尝试在 Worker 中配置 ACP，或未来直接启用现有 Adapter。

**用户后果：** 配置后得到 `not assembled in the M1 Worker build`；抽象上的 native steer/approval/resume 不应被理解为可用能力。未来启用时，矛盾结果可能报告成功。

**证据：** `internal/cli/worker/command.go:160` 只装配 AGY、CodeBuddy、fake；`internal/runtime/descriptors/catalog.go:18` 声明 native controls，而 ACP `adapter.go:300` 实际 Steer/DecideApproval 返回 unsupported。ACP 仅发送一次自定义 `session/prompt` JSON 后关闭 stdin（`:170`），本次未证明它符合任何真实 ACP server 的初始化/会话协议。矛盾 `succeeded` + `error` 实测 Wait 无错误（`evidence/runtime-defect-probes.txt:15`），源码在 `adapter.go:241` 与`:277` 无交叉检查。

**最小处置：** 明确标为实验/不可选，descriptor 只声明实际已验证能力；没有用户需求前不继续扩 ACP。以后接入时先验证一个真实版本的一条完整协议链，再开放界面，不宜先通用化。

## 5. 过度设计与必要边界的划分

| 应保留 | 可收敛/后置 | 原因 |
| --- | --- | --- |
| Task 与 RunAttempt 分层、Mailbox 事务、lease/fencing/CAS | ACP 多产品 descriptor 与未装配入口 | 前者解决重复执行/丢工作；后者没有当前真路径收益 |
| 独立 heartbeat/control、drain admission | 没有消费方的 Runtime registry | `internal/runtime/registry/registry.go` 与 BackendPool 有注册/健康/解析职责重复，源码搜索未见实际消费者 |
| Runtime/业务证据分层、脱敏及有界输出 | 一套覆盖所有行为的“大核验框架” | 先交付 query 完成与一个 action 核验场景，避免再添层但仍不可用 |
| AGY tracer/子进程取消处理 | 在多个 Adapter 中各自放任 timeout/终态不一致 | 可抽取很小的 deadline/终态校验公约；不需要统一所有 argv/协议 |
| 声明实际 session/control 能力 | 将 descriptor 字段存在当成特性完成 | CodeBuddy 仅 new，AGY queued steer，均无 native approval/steer；真实能力矩阵要驱动入口 |

Runner 的六条并发循环各有生命周期职责，不能仅因数量多就判为过度设计。更真实的失衡是：外围有角色配置、session、执行规格、通用 ACP、审批/控制框架，而当前主用户流程仍不能确认完成、自然追问，甚至某个 Adapter 的 timeout 都未执行。

## 6. 测试层次与本次命令

| 命令/动作 | 退出码与时间 | 证明范围 |
| --- | --- | --- |
| `go test -count=1 ./internal/worker/... ./internal/runtime/... ./internal/client/worker/... ./internal/cli/worker/...` | 0，9.325s | 已有确定性测试；包含 fake Worker UDS/HTTPS 连续任务、控制竞态、lease loss、取消后代、session fixture |
| `go test -race -count=1 ./internal/worker ./internal/cli/worker ./internal/runtime/agy ./internal/runtime/codebuddy` | 0，16.312s | 上述重点并发/进程 fixture 无本次可见 race；不等于无所有竞态 |
| 正式 `agy-graft --version/--help` | 均 0，0.326/0.320s；1.2.8 | argv/input/output/conversation/effort/print-timeout/权限 flags 仍在 help；help 实际写 stderr |
| `codebuddy --version/--help` | 均 0，0.057/3.571s；2.143.0 | 当前参数/模型枚举；没有真实 CodeBuddy 模型调用 |
| `python3 …/evidence/runtime-run-live.py` | 内部 `go test -overlay=… -run '^TestAssessmentRealAGYConsecutiveTurns$'` 为 0，48.935s | 正式 Runtime 装配、隔离服务/DB/UDS、两个真 AGY 任务、独立文件结果、Worker 连续性、Journal |
| `go test -overlay=…/runtime-probes-overlay.json -count=1 -v ./internal/runtime/codebuddy ./internal/runtime/acp -run '^TestAssessment'` | 1，0.836s，预期失败 | 三项缺陷反例：CodeBuddy 空成功、超时不执行；ACP 矛盾终态 |

命令、返回值、duration 与安全结果分别在 `evidence/runtime-package-tests.txt`、`runtime-race-tests.txt`、`runtime-contract.txt`、`runtime-live-test.txt`、`runtime-defect-probes.txt`。overlay 生成器/Go 源也归档，未修改固定源码树。首次取消 probe 因误用 API struct 字段仅编译失败，已移除；取消问题由 01 控制面报告负责，未将夹具错误计为产品缺陷，也未重复其 HTTP 审计。

测试最明显的缺口：`internal/runtime/conformance/suite.go:18` 只检查 descriptor 与 Validate，不会 StartTurn/Wait，更不会核验副作用。已有 process integration 使用 fake 自动结果，能准确验证传输与持久状态，但不能发现真实 Adapter 永远 unknown effects 后的交互退化；本次发现的 CodeBuddy 缺陷也完全绕过这些绿色测试。应补的是少量主线跨层验证，不是继续堆同层 mock 数量。

## 7. 跨模块衔接与未覆盖

- **控制面：** CreateTask `execution` 接受但不持久化、取消无 active run 的错误等由 01 报告审计。本模块不重复计数；前者意味着即使 Adapter 实现 model/timeout，用户入口也未必能控制。Runtime 修复必须与 API→resolved spec 快照核对。
- **Web/Console：** Reply、Run status、Task outcome 应明确区分，但文案只能帮助理解，不能替代 RUN-01 的结果/继续入口。03/04 报告须核对“补充成功”提示是否能解释消息被 superseded。
- **网络：** 真 Runtime 前需要网络测试→发布→Worker applied→固定执行版本，本次全部经现有 service/API 路径。正式 wrapper 健康与网络配置健康不能代替真实请求；05 报告审整体工作流。
- **部署：** 实测 AGY 已是 1.2.8，比 9月22日历史报告中的 1.2.7 更新；wrapper SHA 仍为 `31935c961ea76532962068c0e1a8d0a258c80d2f74940dcff3532705ca38b8de`。native SHA 与 helper SHA 在证据内，不能用旧版本报告证明今天的 Runtime。
- **未覆盖：** 本次没有真实 CodeBuddy/ACP 请求、真实 AGY 同 Task resume、真实调用期间 kill Worker/lease loss、实际上游断网与恢复、长时资源 soak、真实 cancel 子进程残局、模型对 ROLE.md 的黑盒遵循。补充联合链已覆盖已登录真实 Chrome → 网络发布/任务提交 → 隔离 daemon → 真 AGY → 文件核验 → 页面回复 → 下一任务；首次登录由 03 首轮验证，不能说此次又重新验证了生产认证/HTTPS。其余必要证据不能用 package/race 或两次简单任务成绩替换。

## 8. 补充：真实浏览器联合链与清理

- **环境和入口：** 03 Web 代理在 `http://127.0.0.1:19173` 的真实 Chrome 会话操作；本模块通过正式 `openagentx agent apply` 在隔离数据库新增 `web-real-agy`，启动专用 Worker。Web 通过实际 UI 完成 inherit 网络测试、发布、生效，再从任务输入框提交 A/B。没有调用数据库写语句、内部对象或 fake Runtime 代替用户动作。
- **版本来源：** 专用 Worker PID `3225460` 的 `/proc/<pid>/exe` 与磁盘二进制 SHA 同为 `a26ebf4d84ede2fa60bd4c10aaee704056732dde9dc532cd424d85de76703e89`；build metadata 为 `6d599aca8ce7c32fe11f23244478b495a0a78e68`、`vcs.modified=false`。配置 SHA `0caf18b9a6c8281f8a92470c12758c196c6f328ced58290d1c59ddd097ce8201`；AGY wrapper/model/受控环境沿用已验证路径。
- **关联证据：** A Task `task-bb2dbeeb-f297-40b8-97ed-0e4f97c08f16`、Run `run-47294afb-2cb0-4a1f-a540-9a3124bca575`；B Task `task-9434387d-3d34-4b0b-beb8-54769a773747`、Run `run-ebc767ac-0a97-4f63-956d-93569fcb2771`。均由 Worker `worker-ebf9cff7-2ee5-4fa1-9670-4b3ca003db7d`、generation 1 执行。
- **真实结果：** 两项 Run succeeded、Task uncertain/business_effect_unverified；每项各一个 started/finished、init/result、task.settled，A 29/B 22 个 step_update，各 Mailbox accepted、attempts=1。Chrome DOM 展示正文及业务未核验提示；独立只读数据库与文件检查对应同一 Task/Run。最终 `artifact.txt` 精确 21 bytes（含 LF），SHA `7459efc1414b18e032060b711f8fd712bb6ffa6321c36bed2f1fe38656a06e06`。独立文件检查发生在 B 之后，未声称两轮间另外冻结过文件哈希。
- **证据和退出：** 浏览器脚本退出 0，约 43.6s；`evidence/web-real-e2e.json`、`web-real-e2e-run.txt` 与 web21–25 截图由 03 归档。本模块归档 `runtime-web-setup.json`、`runtime-web-final.json`、`runtime-web-cleanup.json`。核对专用 PID 的完整 cmdline 后仅向该 PID 发 SIGTERM；约 0.5s 退出、DB Worker offline，正式服务未收到信号。03 代理负责其隔离 daemon/fake Worker 清理。

overlay 可重建：在冻结源码目录存在的前提下执行 `python3 docs/reports/assessment/2026-09-23/evidence/runtime-build-overlay.py` 与 `runtime-build-probes.py`，映射由脚本按当前 evidence 绝对路径生成；Go backing 文件使用 `.go.txt` 避免污染仓库 package 扫描。改名后做过 `go test -overlay=…/runtime-overlay.json -run '^$' ./internal/cli/worker` 编译检查，退出 0；没有重复真实调用。`runtime-web-setup.py` 只从受控本地 password 文件读取登录材料，经 PTY 传入、输出时脱敏；`runtime-run-live.py` 只复制允许的正式进程环境键，未归档值。所有 session/token 原文件均留在受限临时目录，不纳入证据。

## 9. 下一步

1. 以一个真实 AGY 场景完成“发送→过程→可解释终态→追问/下一项”的产品验收；优先交付 query 结果语义与 action 核验最小入口，角色输入要随执行可追溯。
2. 局部修复 CodeBuddy timeout/空终态与可操作诊断，各保留一个进程反例；取消/RBAC/事务等问题由相应模块集中裁决，避免全仓重写。
3. 将 ACP 标为未交付、后置无消费者抽象；下一次上线门禁加入少量真实 Runtime 与浏览器/Console 联合 E2E，包括执行中补充与结果后的下一步，而非仅 fixture 连续任务。
