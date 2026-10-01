# 08 Multica 固定版本参考研究

## 结论

- **可借鉴的是用户闭环和边界表达。** Multica 把连接计算机、创建 Agent、派发工作、看执行与回复串成明确旅程，并区分长期 Issue、单次 Run 和可继续的 Chat。这与 OpenAgentX 的就绪引导、终态收口和后续输入缺口直接相关。
- **角色并非只登记配置。** 源码存在 Agent instructions → 领取载荷 → Runtime brief → provider 指令文件或 inline fallback 的完整接线；OpenAgentX 应把 ROLE.md 接入已支持 Runtime，并以真实输出/工具效果验收。本文确认的是 Multica 源码链，未实测其模型是否遵守。
- **保留 OpenAgentX 现有持久执行基础。** Multica 的完成回报事务、CAS、结果 outbox 和重连恢复值得理解；无需替换 Mailbox、Journal、lease/fencing，也不宜照搬 Issue/Squad/Autopilot 全平台。
- **许可证不是纯 Apache-2.0。** 固定版本采用含附加条件的 Multica License；第三方托管（含免费公共服务）、商业嵌入、UI 品牌与非 UI 归属声明均有约束。下文“借鉴”指独立实现交互与设计思想，不包含搬代码。
- **测试须分层解释。** 本次只运行两组原样纯逻辑文件的 race 测试，均通过；未启动 Multica、未运行浏览器或真实 provider。其仓库的浏览器 routing 测试使用 SQL fixture，真实 agentintegration 被默认 CI 明确排除，不能据此宣称完整真实 E2E 已通过。
- **优先三项：角色输入、任务结果与继续入口、少量真实用户链回归。** 就绪提示、当前 Run 和首连恢复纳入这些小结果；按总报告 A/B/C 批实施，不另起状态机或平台替换项目。

## 1. 来源、验收与研究边界

| 项目 | 本次固定事实 |
|---|---|
| 官方仓库 | <https://github.com/multica-ai/multica> |
| 固定 commit | `90e0bdf830436b3981b32a7017e1c18d41c7cdea` |
| commit 时间与标题 | `2026-09-22T16:44:14+08:00`；`MUL-7586 Atomically create issues with custom properties (#8689)` |
| 研究时间与预算 | 2026-09-23 00:44 起，最多 25 分钟；达到源码/许可证/测试边界和三项建议后收口 |
| 来源形式 | 只读浅克隆；本文外部源码链接全部固定到上述 commit |
| 技术栈 | Next.js 16；Go/Chi/WebSocket；PostgreSQL 17；web/desktop 共享 core/views/ui，mobile 独立 |
| 工具限制 | 仓库要求 Go 1.26.6、Node >=22；本机 Go 1.22.4、Node 20.19.4；未安装依赖或新工具链 |
| 产出验收 | 可追溯的核心旅程/角色/状态/恢复链、许可证限制、实测与未测分层、三项有界借鉴及验收 |
| 非目标 | 不部署 Multica，不访问账号或真实 Agent CLI，不改 OpenAgentX 产品/冻结 ADR，不重启正式服务，不 commit |

固定源码与测试文件哈希见 [provenance](evidence/multica-provenance.txt)，关键片段见 [source evidence](evidence/multica-source-evidence.txt)。README 的 provider 数量和上手时间属于上游声明，未在本次验证。本文对 OpenAgentX 的判断复用本批 [总评估](00-overall-assessment.md)、[Runtime](02-worker-and-runtime.md)、[Web](03-web-and-pwa.md)、[Console](04-console-fleet-and-onboarding.md) 的证据，不重复执行相同测试。

## 2. 第一次把工作交给 Agent

[Quickstart][quickstart] 给出的路径是：登录工作区 → 连接运行机器 → 确认 online → 创建 Agent → 创建并派发 Issue → 在同一 Issue 看执行状态和回复。每步有成功判据和失败排查，而不要求用户先理解 daemon、调度和 session。

| 用户问题 | Multica 的源码/文档做法 | 对 OpenAgentX 的最小借鉴 |
|---|---|---|
| 我现在缺什么才能开始？ | 前置声明本机需要已登录的 coding tool；连接页面给命令和检测结果 | 统一展示实际 Backend readiness；缺 network binding 时给具体配置入口，不能仅显示 Worker online |
| 它还在找机器，还是失败了？ | runtime picker 空列表每 2 秒刷新，注册事件即时刷新；desktop scanning/found/empty，5 秒软超时、20 秒上限及 Refresh | 首用等候有原因、时限与恢复动作；不先做通用向导引擎 |
| Agent 配置要填多少？ | Start blank 只有名称严格必需，确认 runtime/tool，其余以后补 | 主入口优先满足可执行路径；未消费的组织/岗位配置不作为首项任务门槛 |
| 我的结果在哪里？ | 当前执行和回复回到 Issue 时间线；可展开 transcript | Task 主区始终显示当前状态、最新有效回复/产物和下一步；Run ID 等放诊断 |
| 一堆历史执行怎么读？ | active runs 前置，past runs 折叠；失败/取消显示原因和可用 retry | 明确当前 Run 归属与排序，修复旧 Run 展示；历史折叠不能代替正确的权威选择 |

源码依据：[runtime picker][runtime-picker]、[runtime connect][runtime-connect]、[execution log][execution-log]。这些是本次源码确认，未操作 Multica 的在线页面，也不证明其所有首用组合无缺陷。OpenAgentX 的 `OAX:overview` 空窗和隐藏网络前置已有本批动态证据；这里支持补一个轻量任务导航/就绪入口，不支持建设第二套调度器。

## 3. 对象边界及角色指令

### 3.1 不把每个执行都等同长期工作

[概念文档][concepts] 区分：Agent 是可复用身份/指令/模型/权限/runtime 配置，不是常驻进程；Runtime 是运行机器与工具；Issue 是长期工作及讨论；Run 是单次执行；Chat 每条消息触发一个 Run。

这能解释“Agent 完成一次回复后为什么还能继续问”。OpenAgentX 已有 Task/Run/SessionBinding，无需复制同名 Issue/Chat 实体；可在终态 Task 旁提供关联的新 Task 入口，明确是否继承会话和上下文。不能以修改 terminal Task 的状态绕过现有不变量。

### 3.2 指令从保存到执行的接线可追溯

| 环节 | 固定源码证据 | 已确认/未确认 |
|---|---|---|
| Control 下发 | [handler/daemon.go:2520][role-payload] 的 TaskAgentData 含 Agent instructions | 下发字段存在；不代表实测领取 |
| Runtime 输入构造 | [daemon.go:7738][role-context] 从 `task.Agent.Instructions` 填入 `TaskContextForEnv.AgentInstructions` | 明确消费路径 |
| 生成 brief | [runtime_config_sections.go:108][role-identity] 写 Agent 名称、ID、instructions | 指令进入实际 brief 构造 |
| provider 文件 | [runtime_config.go:159][role-files] 映射 Claude 的 CLAUDE.md、Codex/AGY 的 AGENTS.md 等 | 有 provider 差异；不是一套 argv 覆盖所有工具 |
| 无可靠文件读取路径 | [daemon.go:8665][role-inline] 对需要的 provider 使用 `execOpts.SystemPrompt = runtimeBrief` | 有限定的 inline fallback，不是所有 provider 都支持 |
| 文件保护 | `runtime_config_test.go:949/986/1028` 有保留原内容、更新 managed block、幂等测试 | 本次只阅读，未运行该 package |

OpenAgentX 当前 ROLE.md 注册校验没有形成确定性执行输入，应优先补这条接线。最小验收需在正式支持的 wrapper/model/cwd 下设置受控角色 marker 或约束，经真实执行确认，再关联 Run/Journal 中的输入版本证据；仅检查文件存在、模型自述“已读取”均不够。应保留用户已有 workspace 指令，不照搬上游全部 brief/workflow 文本。

## 4. 完成、继续和取消

### 4.1 `completed` 的语义比“工作完成”窄

[Runs 文档][tasks] 明确 `completed` 仅表示该次执行正常结束，不能确认 Issue 的目标实现。[Issue 文档][issues] 则说明 `in_progress/in_review` 通常由 Agent 通过 CLI 显式更新，server 不随 Run start/complete 自动切换 Issue；`done` 通常来自人的确认或 PR integration。这个分层值得借鉴，但 Agent 主动更新业务状态本身也不是独立效果核验。

源码 [CompleteTaskWithTransition][complete-tx] 将 running→completed CAS、chat session/runtime pointer 和 assistant outcome 放在一个事务中，成功后再广播；幂等重放不能重复写回复。这是“执行收口与结果可见”保持一致的具体实现。

[daemon 的结果分支][result-switch] 仍允许 `completed` 且无文本的执行正常完成，理由是工具可能已经完成工作；known poisoned output 才转 blocked。它没有普遍核验工作区副作用。因此，不能据此把 OpenAgentX 的 Run succeeded 自动映射 Task succeeded，也不能借“减少 uncertain”取消诚实的未知状态。应对接正在实施但未安装的 ADR-006 `dcd8fd6`，先完成一个只读结果规则和一个低副作用文件核验路径。

### 4.2 后续输入有明确入口，session 丢失不伪装延续

[Chat 文档][chat] 把每条消息作为新 Run，并尽量复用原 coding-tool session；session 不可用时保留聊天记录但从新 session 开始。源码还对 Codex rollout 缺失抑制无效 resume pointer，并记录 continuity gap（[daemon.go:8844][result-switch]）。

可借鉴的是用户知道“这是接着上次做，还是带着历史开始一次新执行”。不宜把聊天历史存在等同 provider 上下文确实恢复。OpenAgentX 的终态禁止 `/steer` 与安装指南追问示例不一致，应选择明确的新 Task/续上下文契约并同时更新两端入口。

### 4.3 排队取消与运行中中断分别处理

[取消 SQL][cancel-sql] 覆盖 queued/dispatched/running/waiting_local_directory/deferred；另有 [queued-only CAS][cancel-queued]，避免用户点击后恰好被 claim 的竞态。[Task service][cancel-service] 用事务更新取消与 chat 指针，配合 actor、幂等返回和后续通知；daemon 的 cancel watcher 则轮询状态，并在重连信号后重新检查。

这些源码支持“取消不只是一颗 UI 按钮”的参考结论，但本次未验证其外部进程是否可靠中断，也不证明已发生副作用会回滚。OpenAgentX 的 queued/waiting_input 取消已归 CORE-01；此处不新增一个重复缺陷编号，更不以数据库 terminal 标记替代实际进程与结果证据。

## 5. 断线恢复与结果回报

| 机制 | Multica 固定源码 | 可借鉴边界 |
|---|---|---|
| 首次连接失败也重连 | [ws-client.ts:159][ws-client] 的 onclose 调度指数退避；1 秒到 30 秒，含 jitter | OpenAgentX 首连 503 也应进入恢复循环，不能只处理曾连接后的断线 |
| 首次失败后恢复也刷新 | `recoveredConnection = hasConnectedBefore || reconnectAttempt > 0`；[use-realtime-sync][realtime] 恢复时 invalidation/refetch | 事件恢复后重新读取权威 snapshot；沿用现有 SSE，不必换成 WebSocket |
| 恢复广播 | `reconcile.go` close-and-replace、多订阅者、迟到订阅 replay、reconnect debounce | 单个恢复信号让等待者核对权威状态；本次该纯逻辑测试通过 |
| 终态回报持久化 | [terminal_report_queue.go:84][terminal-outbox] 按 task 存文件，按 server/profile/daemon 隔离 | 网络失败时重送已得到的结果，避免崩溃丢失回报 |
| 保持回报语义 | [daemon.go:6230][report-result] 只有明确 completed 才调用 complete；回报未 ACK 保留原结果重试 | **重送结果不等于重新执行任务**；不能用 synthetic failure 覆盖已完成结果 |
| 有限自动重试 | `service/task.go:5172` 按 runtime/network/timeout 等原因 allowlist 与次数安排 retry | 不直接迁入；OpenAgentX 对未知副作用不得自动重跑的约束更重要 |

上游也有未完成处：`ws-client.ts` 注释明确 UI 尚无明显 disconnected 状态或手动 retry，因此无限重连只改善传输恢复，不等于用户知道当前信息是否过期。OpenAgentX 应同时显示连接/陈旧状态及恢复入口，不能把该文件当作完整交互范本。

## 6. 权限边界与许可证

### 6.1 不把交付 review 当作危险工具调用审批

[Security model][security] 明确默认以 daemon 所在 OS 用户完整权限执行，不提供普遍 filesystem sandbox 保证。固定源码中 Codex 对 exec/file change/permissions 自动批准（[codex.go:2867][codex-approval]），Claude 使用 bypassPermissions，AGY 使用 `--dangerously-skip-permissions`。

因此，上游所说的 review gates 应结合 Issue/交付语义理解，不能解读成每次危险工具调用都要人确认。不能为追求“顺畅”将这些默认值迁入 OpenAgentX，或放松现有服务端授权、秘密投影、lease/fencing 和未知结果处理。

### 6.2 不是无附加限制的 Apache-2.0

固定 commit 的 [LICENSE][license] 开头明确：Part I 附加条件与 Part II Apache 文本共同构成 Multica License，任何一部分不单独授权；冲突时 Part I 优先。

| 条款 | 原文约束的实际影响 |
|---|---|
| Part I 1(a) | 未取得 commercial license，不可基于源码向第三方提供 hosted service，或商业嵌入分发；免费向组织外用户开放也受限；单组织内部使用例外 |
| Part I 1(b) | 没有书面 branding waiver 不可删改 Multica UI 的 LOGO、名称、版权/归属；涵盖修改、移动或抽取 UI 代码 |
| Part I 1(c) | 只使用 backend/daemon/CLI 也需保留声明，并在面向用户文档注明 built on Multica 及仓库链接 |
| Part I 1(d)、3 | 商业许可和品牌豁免独立；再分发必须提供整个许可证，不能仅保留 Apache 部分 |

本次没有复制 Multica 实现到 OpenAgentX。可独立借鉴概念、交互和验收选例；若以后计划搬代码或运营派生服务，应先按具体分发/托管用途核对完整条款及所需授权，不能靠 README badge 或 GitHub 分类推断许可。

## 7. 测试证据：源码存在、实际运行、真实 E2E 分开

| 测试层 | 仓库内容 | 本次执行与结论 |
|---|---|---|
| 默认 Agent 单测 | [AGENTS.md:119][agent-tests] 要求 fake/missing executable；不调用用户安装的 CLI | 未运行完整默认测试；不能据其存在证明真 provider |
| Onboarding 浏览器 | `e2e/onboarding-smoke.spec.ts` 检查 welcome/questionnaire/workspace/runtime 页面和截图；`onboarding-shell.spec.ts` 断言真实 DOM/geometry | 未执行；测试边界主要是页面与布局，没有完整模型执行 |
| Comment routing 浏览器 | [comment-agent-routing.spec.ts][comment-e2e] 直接 SQL 插入 runtime/agents/终态 runs，真实 UI 提交 comment 后验证 queued 路由与历史展示 | 未执行；这是 UI/API routing regression，不是 daemon→provider 的真实执行验收 |
| 真实 Agent smoke | [AGY integration][agy-integration] 两轮 marker/session/usage；Cursor 请求写再读 ping.txt；Codex 有中断延迟测试 | 全未执行；Cursor 示例主要检查 tool/result 事件，不能视为独立磁盘副作用核验 |
| 真实测试门禁 | [real-agent gate][real-gate] 先检查 `MULTICA_RUN_REAL_AGENT_SMOKE=1`，再 lookup CLI；需 `agentintegration` | 本次不设置开关、不访问账号 |
| CI | [.github/workflows/ci.yml:387][ci] 明确不运行 real agentintegration，仅带 tag `go vet` 防编译腐化；UI performance 为手动 workflow | 没有证据支持“真 Agent E2E 持续门禁已覆盖”；未审计所有历史 CI 结果 |
| 真实 CLI 配置 smoke 例外 | [openclaw-config-smoke.yml](https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/.github/workflows/openclaw-config-smoke.yml#L47) 安装真实 OpenClaw，执行配置 loader 测试并明确拒绝 skip | 源码确认；这是真 CLI 配置检查，不是模型任务或完整用户链 E2E，本次未运行 |
| 本次低成本验证 1 | 原样复制 `reconcile.go` 与其测试；命名文件 `go test -race -count=1 -v` | **通过**；证明 broadcaster/replay/debounce 的这些测试，非 daemon/package 整体 |
| 本次低成本验证 2 | 原样复制 `issue_state_instructions.go` 与其测试；同样命名文件测试 | **通过**；证明提示分支逻辑，非完整角色注入/模型遵守 |

两次实际测试均 `GOTOOLCHAIN=local GOWORK=off`，使用当前 Go 1.22.4；输入文件与固定 clone SHA-256 一致。完整输出：[reconcile](evidence/multica-reconcile-tests.txt)、[issue state hint](evidence/multica-issue-state-hint-tests.txt)。没有用命名文件测试冒充 Go 1.26.6 模块通过。临时测试副本已删除，固定 clone 保持 clean 并保留供复核，无研究启动的服务。

OpenAgentX 本批已有真实 Chrome → 正式 Worker → AGY → 文件独立核验 → 同 Worker 下一项的证据，两 Run succeeded、两 Task uncertain；不能将其写成“完全没有真实 E2E”。正确缺口是缺少稳定覆盖关键失败路径的回归，以及平台结果契约尚未收口。Multica 的分层测试组织可参考，但其 fixture SQL 不能迁入 OpenAgentX 并冒充正式 Control/Worker API 验收。

## 8. 借鉴裁决与前三项小结果

| 裁决 | 内容 | 原因/触发条件 |
|---|---|---|
| 可立即借鉴思想 | 首项任务明确成功判据、readiness 原因与下一步、当前执行与历史分层、首连恢复后 refetch | 对应本批已复现的用户阻断；用现有入口局部实现 |
| 只借鉴机制、按现有边界实现 | 指令 managed block/版本追踪、结果事务、session continuity gap、终态回报 outbox | 对应已有状态/协议，不复制平台；outbox 仅在确认当前存在丢回报窗口时有界补充 |
| 不直接迁入 | 全量 Issue/Project/Squad/Autopilot/多 provider 层、换库、WebSocket 替换 SSE | 当前闭环不需要，新增维护和状态边界无本批验收收益 |
| 不迁入其默认安全取舍 | 自动批准危险工具调用、因 network/timeout 自动重新执行未知副作用任务 | 与当前授权与结果诚实边界不等价 |
| 代码移植前置条件 | 完整许可证与用途核对 | 只有实际计划引入代码时触发，本批只研究独立实现思想 |

前三项应与总报告批次协调，不能作为另一份并行重构队列：

1. **角色在真实执行中生效（总报告 B）。** 最小范围是 ROLE.md 已有数据进入支持的 Adapter 输入，并保存输入版本证据。验收：正式 wrapper/model/cwd 下，受控角色 marker 和具体约束对两次连续任务生效；默认无角色路径仍成立；不覆盖用户已有指令。
2. **从一个入口理解结果并继续工作（总报告 A/B/C）。** 优先可靠 queued 取消、最新 Run/回复、只读与 action 的有限结果规则、终态后的关联新 Task；readiness 和重连状态作为入口必要反馈。验收：未配置时说明原因；可撤回且不会以后执行；真 AGY 的结果/文件/Journal 一致；追问去向明确；首次 503 恢复不重复提交。结果规则对接 ADR-006，不新增通用核验平台。
3. **固定一组短小真实用户链回归。** 保留 fake 状态竞态测试，另列少量真 provider 旅程：首次进入、正常回复、低副作用文件、终态追问、queued 取消、首连失败恢复。逐项记录浏览器操作、Task/Run/Journal、实际文件与后续任务；fake、真实、未测必须分列，不用“进终态”一个断言代替效果。

## 下一步

1. 主代理将前三项归入总报告既有 A/B/C 批；本研究不启动实现、部署或额外长测试。
2. 后续每个小结果按正式入口与真实支持的 AGY 路径验收，保留未知副作用不重跑的边界；多 provider 和平台迁移继续后置。

[quickstart]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/apps/docs/content/docs/cloud-quickstart.mdx#L1
[runtime-picker]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/packages/views/onboarding/components/use-runtime-picker.ts#L12
[runtime-connect]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/packages/views/onboarding/steps/step-runtime-connect.tsx#L91
[execution-log]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/packages/views/issues/components/execution-log-section.tsx#L34
[concepts]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/apps/docs/content/docs/concepts.mdx#L24
[role-payload]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/server/internal/handler/daemon.go#L2520
[role-context]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/server/internal/daemon/daemon.go#L7738
[role-identity]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/server/internal/daemon/execenv/runtime_config_sections.go#L108
[role-files]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/server/internal/daemon/execenv/runtime_config.go#L159
[role-inline]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/server/internal/daemon/daemon.go#L8665
[tasks]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/apps/docs/content/docs/tasks.mdx#L138
[issues]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/apps/docs/content/docs/issues.mdx#L66
[complete-tx]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/server/internal/service/task.go#L4325
[result-switch]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/server/internal/daemon/daemon.go#L8844
[chat]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/apps/docs/content/docs/chat.mdx#L43
[cancel-sql]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/server/pkg/db/queries/agent.sql#L1564
[cancel-queued]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/server/pkg/db/queries/agent.sql#L1696
[cancel-service]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/server/internal/service/task.go#L2857
[ws-client]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/packages/core/api/ws-client.ts#L159
[realtime]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/packages/core/realtime/use-realtime-sync.ts#L1793
[terminal-outbox]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/server/internal/daemon/terminal_report_queue.go#L84
[report-result]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/server/internal/daemon/daemon.go#L6230
[security]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/apps/docs/content/docs/security-model.mdx#L8
[codex-approval]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/server/pkg/agent/codex.go#L2867
[license]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/LICENSE#L1
[agent-tests]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/AGENTS.md#L119
[comment-e2e]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/e2e/comment-agent-routing.spec.ts#L1
[agy-integration]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/server/pkg/agent/antigravity_integration_test.go#L16
[real-gate]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/server/pkg/agent/real_agent_smoke_integration_test.go#L12
[ci]: https://github.com/multica-ai/multica/blob/90e0bdf830436b3981b32a7017e1c18d41c7cdea/.github/workflows/ci.yml#L387
