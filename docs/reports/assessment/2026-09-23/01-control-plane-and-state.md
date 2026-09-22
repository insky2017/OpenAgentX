# 01 控制面与持久状态评估

- 日期：2026-09-23；本模块约 00:28–00:39（Asia/Shanghai），在首次 35 分钟预算内收口。
- 固定源码：`34053c0`，只读副本 `/tmp/oax-assessment-20260923-source`；已安装 Go 基线 `6d599ac`，其后至固定 HEAD 的变化为 Web/文档，由主报告核对部署来源。
- 范围：组织/业务权限、Web/CLI auth、Control/Worker API、SQLite 事务、CAS/幂等、Mailbox、Task/Run/Session、恢复状态。
- 非目标：不修改产品代码、ADR、生产 DB、服务、网络或正式任务；不把内部测试提升为真实 Runtime E2E。

## 结论

- 用户所说的“外围设计很多、核心薄”在本模块有明确证据：组织岗位/汇报链/策略已有 schema、领域类型和“completed/passed”记录，但没有进入真实 CommandService；另一方面，最普通的排队任务取消仍失败，等待输入任务也取消不了。
- SQLite 的原子事务、幂等、防重执行和 Worker 身份保护并不薄弱。本次 13 个相关 package 的 `-race -count=1` 全部通过，状态、Mailbox、Journal、SessionBinding 的同事务与回滚已有较扎实实现，不建议重写这层。
- 测试重心偏内部合法状态和既定结论。现有恢复单测甚至明确断言“未确认取消的过期运行对应 Task=canceled”，因此全绿会维持错误终态语义；它不是缺少更多同类测试，而是缺少基于用户结果的反向验收。
- 本次新增 5 个隔离缺陷特征测试，命中 4 个根因：无活动 Run 的取消失败、取消恢复误报确定终态、Execution 覆盖被静默丢弃、业务组织权限没有贯通。前两项直接损害当前单用户主流程；组织项在当前单 owner 下不等同于已发生权限事故。
- 核心闭环应先收敛成“创建任务 → 执行 → 明确可理解的结果 → 可取消/补充/再做下一项”，再决定是否启用完整岗位治理；不应先补齐全部组织管理再修取消和结果。

## 承诺—实现—证据矩阵

| 承诺/用户结果 | 当前实现 | 本次证据及结论 |
|---|---|---|
| 提交一次只创建一份 Task、初始 Message、Mailbox、审计事件 | `CreateTask` 单事务，principal+key 唯一，载荷比较重放 | SQLite race tests 通过；HTTP 创建可持久化。**内部事务实现较好**，不是业务副作用证据 |
| 用户可取消 queued/running/waiting_input | 有活动 Run 的路径有 Control item 和结算测试；无活动 Run 的错误类型不匹配 | 本次 HTTP queued、waiting_input 均 400；CORE-01。Web 模块另有真实 Chrome queued 取消复现 |
| 取消是 desired state，未知物理结果保持 uncertain | 正常 Finish 对未知副作用保守；恢复把 cancel_requested 写 canceled | 本次隔离恢复得到 Task=canceled、Run=uncertain；CORE-02 |
| Task 执行参数不被静默替换 | API 接受 Execution，仅 network override 明确拒绝；Task 根本不存 Execution | 本次 HTTP 相同 key 修改 execution 仍 200 同 Task；源码确认未传入 planner；CORE-03 |
| operator 只能在授权组织/目标范围下达业务命令 | WebRole/CSRF 成立；AuthorityPolicy 只被领域单测调用 | 本次真实 auth+HTTP+SQLite fixture 接受 operator 跨组织不一致 direct Task；CORE-04 |
| Position/Role/ReportingLine 形成可用的责任与指挥模型 | 表与类型存在；未发现业务读写、策略装配或 CommandService 查询 | 源码确认能力未贯通；旧任务12的 completed/passed 超出证据 |
| Task 终态由可信业务结果决定 | `SideEffectsKnown=false` 的成功 Turn 导致 Task uncertain；没有显式 intent | 防误报成功的安全边界保留，但 query/分析任务缺少适配；ADR-006 在本基线仍 proposed，旁支未安装，不能记为现有能力 |
| 单 Agent 只能一个 Active Run、过期 Worker 不可写 | 唯一约束、lease/generation/fencing、guard 与事务内复查 | 本次对应 package race tests 通过；没有本次生产异常切换试验 |
| Message/finish、cancel/finish、SessionBinding CAS 竞态原子化 | 有事务、消息保留/拒绝、SessionBinding CAS 失败回滚 | 本次现有测试通过，属隔离服务/Repository 验证；需另有实际用户连续任务验收 |
| 认证可撤销且读取不泄漏 Worker 凭据 | Web session/CLI token 持久摘要、CLI audience/scope/实时 user 校验、读模型投影 | auth/api tests 通过；本模块未进行网络渗透或重测浏览器会话 |

## 做得好的部分

- **权威状态与事件同提交。** `internal/persistence/sqlite/task_repository.go:99` 至提交路径保证 Task、初始 Message、Mailbox 和 Journal 同事务；消息、审批、Run 结算有故障注入和回滚覆盖。数据库采用 WAL、foreign key、immediate transaction（`repository.go:52`），而非用事件流当权威状态回放数据库。
- **身份保护有真实边界。** `internal/domain/worker_security_contract.go:61` 的 token/principal、Agent、generation、fencing、expiry、lease 检查，以及 `worker_service.go:246` 的先认证再处理 heartbeat ack，已有组合故障测试；本次没有发现需要另开安全大重构的证据。
- **防重复执行的复杂度有明确价值。** Mailbox claim/lease、单 Agent Active Run 唯一约束、SessionBinding CAS、未知副作用不自动重试，可以应对连接丢失、daemon/Worker 重启等当前真实场景，不能因为单用户就删除。
- **认证角色分层本身合理。** owner/operator/viewer 及 CLI scope 隔离，可约束管理动作和普通业务动作；问题在业务授权图未接入，而不是这些角色必须全部移除。
- **正常终态对副作用保持保守。** `worker_execution_repository.go:629` 拒绝把没有可信效果证据的成功 Turn直接当作 Task 成功，这条保护值得保留；需要为只读任务补完整成功证据，而不是整体放宽。

## 缺陷与最小处置

### CORE-01：无活动 Run 的任务无法取消

- **严重度：高 / P1，当前单用户核心操作缺陷。** 触发：Task 刚创建仍 queued，或上一 turn 已结束为 waiting_input；用户点击取消。
- **本次动态结果：** HTTP 均返回 400，正文 `find active run for cancel: resource not found`。queued 保持 queued，仍可能被 Worker 执行；waiting_input 保持 waiting_input。事务确实回滚，没有假称取消成功，但用户没有受支持的取消路径。
- **根因：** `internal/persistence/sqlite/control_input_repository.go:184` 查询 active run；`:212` 仅容忍 `sql.ErrNoRows`。而 `internal/persistence/sqlite/execution_repository.go:261` 已把该错误转换为 `domain.ErrNotFound`。因此无 Run 被误当数据库错误；现有取消测试集中于 active Run。
- **额外状态缺口：** 只把错误检查改对还不够。当前无 Run 分支仅设 `cancel_requested`，不创建 control item；`worker_execution_repository.go:232` 又禁止它开始 Run，需要在同事务定义“没有活动物理执行”的取消终态及待投递 item 收口，避免修成永久取消中。
- **最小处置：** 修复无 Run 判定；在任务/运行状态 CAS 下把确定尚未执行或已无活动执行的取消收敛为 canceled，并结束对应待执行 work item。保留 active Run 的 desired-state 路径。
- **验收：** 浏览器与公开 API 分别覆盖 queued、waiting_input、active Run、finish/cancel 竞态；等待输入/排队取消后，下一项独立任务仍可执行；不可只断言取消请求写入。
- **证据：** `evidence/core-reproductions.log` 两项 HTTP 测试；`evidence/core-http-repro_test.go.txt`；独立 Chrome 证据 `evidence/web-15-mobile-canceled.txt`、`evidence/web-15-mobile-canceled.png`（由 Web 模块提供）。

### CORE-02：取消后的过期恢复把未知结果误报为已取消

- **严重度：高 / P1，条件性但属于误报确定终态。** 触发：已执行的 Run 收到 Task 取消请求，尚未确认物理结果时 Worker/连接消失，lease 过期后执行恢复。
- **本次动态结果：** 使用隔离 Repository fixture 的公开方法创建 Run、提交取消，再执行 ReconcileExpired；没有任何 Runtime 取消回执或实际效果确认，最终 `Task=canceled`，`RunAttempt=uncertain`。
- **根因：** `internal/persistence/sqlite/recovery_repository.go:73` 正确将过期 Run 置 uncertain，但 `:100` 单凭 Task 为 cancel_requested 将 Task 置 canceled。正常 Finish 的 `worker_execution_repository.go:635` 却明确保留 uncertain，两条结算路径不一致。
- **为什么现有测试没挡住：** `internal/persistence/sqlite/recovery_repository_test.go:83` 的注释与 `:109` 的断言恰好要求 canceled，并且该段直接改 Task 状态建立 fixture；测试保证了实现一致，却没有证明取消实际发生。
- **后果：** 用户可能理解为工作已停止/没有未知副作用，继而再次执行相同操作；原物理进程可能已完成或留下部分副作用。本次未制造真实生产崩溃，不声称已经发生重复执行。
- **最小处置：** 取消意图保留为审计事实，未知物理执行恢复为 uncertain；正常 finish 和 recovery 复用一致的有限结算规则。不要因此新建通用工作流引擎。
- **验收：** 隔离真实 Worker 的取消后失联/过期恢复，检查 Task/Run/Journal 与真实 workspace 结果一致；物理效果未知时禁止自动重新执行。
- **证据：** `evidence/core-reproductions.log`；`evidence/core-recovery-repro_test.go.txt`。本次是隔离持久状态复现，不是 Runtime 崩溃 E2E。

### CORE-03：Execution 请求被接受但丢弃，幂等也忽略其差异

- **严重度：中 / P2；若调用者依赖权限/预算/timeout 覆盖则需要提升优先级。** 触发：通过公开 CreateTask API 提交合法的非 network `execution`，例如模型、backend、reasoning 或 timeout；当前 UI 是否发送这些字段由 Web/Console 模块补充。
- **本次动态结果：** 首次无 Execution 创建成功；相同 key 改成指定 backend/model/timeout 后仍 200，返回同一 Task，而不是载荷冲突。
- **根因：** `internal/api/control.go:316` 校验 Execution 形状，仅 network 明确 fail closed；`internal/controlplane/command_service.go:61` 构造 Task 不带它；`domain.Task`/tasks 表没有 requested execution；`task_repository.go:159` 的幂等比较也不含它。`worker_service.go:447` 后续从 Task/messages/backends 重新 Plan，已无用户覆盖信息。
- **后果：** API 使调用者以为参数已被接受；实际执行按默认 planner，违反“不静默替换 Backend/模型/权限”的承诺。此次没有运行真实外部 Runtime 来证明特定模型被替换，结论是 API 接受后持久契约确定丢失。
- **最小处置：** 在完整持久化和授权尚未落地前，明确拒绝整个 Execution override，避免假支持；若它属于当前核心用例，再一次性贯通请求持久化、授权、幂等、planner、Run resolved spec 和结果回显。
- **验收：** 两个不同覆盖同 key 必须冲突；支持时真实执行及 Run 记录与请求一致，不支持时创建前明确失败。
- **证据：** `evidence/core-http-repro_test.go.txt`、`evidence/core-reproductions.log`；与 Runtime 模块交叉引用，不重复计算为两项独立缺陷。

### CORE-04：组织业务授权仅有领域模型，未进入命令入口

- **严重度：当前单 owner 为中 / P2 的能力与承诺缺口；启用多主体/多组织前是权限隔离阻断。** 不把它等同于当前安装已遭越权或要求现在实现完整组织系统。
- **本次动态结果：** 隔离真实 auth+HTTP+SQLite 场景新增普通 operator；未提供组织/岗位/direct 授权。向 org-main 的 `quote` Agent 创建标记 org-other 的 direct Task，HTTP 200，持久化 Task.organization=org-other、Agent.organization=org-main。
- **根因：** `internal/api/panel/handler.go:149` 仅 role/scope/CSRF；`command_service.go:52` 没有 AuthorityPolicy 调用；`task_repository.go:116` 只依赖两个分别存在的外键，没有同组织约束。全仓 `AuthorityPolicy.Authorize` 的业务调用不存在，仅 `organization_contract_test.go` 使用。OrgUnit/Position/Role/ReportingLine/authority_policies 只有 schema/类型，没有完成工作流。
- **承诺差距：** ADR-001 `:205`、`:237` 要求针对组织、目标和动作授权；任务12文档标 completed，验证报告标 passed，但正文主要证明领域纯函数，而非 HTTP 入口强制执行。
- **现实后果：** 当前单 owner 的岗位树增加概念和文档成本，却没有提供实际指挥治理；如果据此开放 operator、多组织或委派，读写权限不会自动获得隔离。coordinated/direct 目前更接近标签，不能推断已完成主指挥者解析和层级指挥。
- **最小处置：** 先明确当前支持单组织 owner 流程，保证 target Agent 与 Task organization 一致，校正文档完成度；保留轻量 RBAC。在真正开放多主体前，统一 CommandService 的 principal/action/resource/context 业务授权，覆盖创建、补充、取消、审批，不只接创建。
- **验收：** HTTP 认证 operator 的允许/拒绝矩阵与记录落库、审计一起验证；不以纯 policy 单测替代入口授权。暂不扩建岗位编辑 UI、策略 DSL 或多租户系统。
- **证据：** `evidence/core-reproductions.log`；`evidence/core-source-excerpts.txt`；任务12历史报告为源码中的历史记录，不是本次验证结果。

## 核心用户闭环与模块衔接

| 用户阶段 | 控制面已有保障 | 薄弱衔接/证据限制 |
|---|---|---|
| 我交代一项工作 | authenticated principal、幂等、原子入队 | Execution 形状看似支持但丢失；组织/dispatch 未成为真实授权 |
| 系统开始做 | 持久 Mailbox、Worker guard、单 Agent Active Run | 排队取消失败，使用户在执行前缺乏有效撤回；Worker 可用性由 Runtime 模块核实 |
| 我看懂结果 | Task/Run/Journal 持久化，副作用不明保持 uncertain | query 和 mutation 没有领域区分，普通输出可能保守变 uncertain；取消恢复却反向过于乐观 |
| 我补充、取消或做下一项 | Message 幂等、follow-up、SessionBinding 与终态不能续写的边界 | waiting_input 取消失败；状态机有很多细节，但基本退出动作缺完整用户验收 |
| 失败后恢复 | lease/fencing、恢复审计、事务回滚 | 状态恢复正确不等于业务结果正确；CORE-02 正是跨边界错误 |

后端包测试能证明“输入这一状态，得到那个状态”；它们不能单独证明用户能在页面创建工作、看到确实有用的结果、按取消撤回，然后连续执行下一项。已有 HTTP integration 测试也常在 setup 用 Repository 建立活跃 Run，结尾直接提交 TurnResult；例如 `internal/api/panel/integration_http_test.go:179` 的取消测试跨了 HTTP 和 SQLite，却没有真实 Runtime 物理取消。此类测试有价值，但应标为集成契约测试。

## 必要复杂度与过度设计判断

- **保留：** Agent/Worker/Run 分离，持久 Mailbox，lease/fencing，CAS，幂等，事务内 Journal，SessionBinding。它们对应真实重启、防重执行、连续任务和模型会话风险，已有验证收益。
- **当前不宜继续投入：** 尚未消费的岗位/汇报链/authority schema 与完整组织概念。它们并未帮助单用户“发任务—看结果”，反而让读者误以为治理能力已经交付。先明确不支持并隐藏未贯通入口，比继续堆模型更有效。
- **优先局部修复：** 取消错误归一化、取消状态收敛、恢复终态一致、拒绝假支持字段。这些不要求重写 Repository、换数据库或建立统一事件框架。
- **仅做有边界重构：** 若正常 finish 与 recovery 无法共享终态不变量，可抽取小型结算决策函数并各自保留事务执行；若以后开放组织授权，给 CommandService 一个统一授权入口。无需现在把所有 API/DB 方法泛化成通用 Command Bus。

## 测试、证据与未覆盖

| 命令 | 时间（北京时间）/耗时/退出码 | 边界 |
|---|---|---|
| `go test -race -count=1 ./internal/domain/... ./internal/auth/... ./internal/api/... ./internal/controlplane/... ./internal/persistence/sqlite/...` | 00:28:58–00:29:44；46.239s；0 | 13 packages 通过，使用固定副本；含 auth、API、事务、恢复、安全优先级及网络持久化现有测试 |
| `go test -race -count=1 -run '^TestAssessment' -v ./internal/api/panel ./internal/persistence/sqlite` | 00:34:05–00:34:15；10.246s；0 | `/tmp/oax-assessment-core-repro` 仅添加评估测试；5项 PASS 表示缺陷行为被确认，并非验收通过 |

- 原始日志：`evidence/core-package-tests.log`、`evidence/core-reproductions.log`；精简机器结果 `evidence/core-test-results.json`；现有测试名清单 `evidence/core-test-inventory.txt`。
- 可复现源码：`evidence/core-http-repro_test.go.txt`、`evidence/core-recovery-repro_test.go.txt`；源码摘录 `evidence/core-source-excerpts.txt`。复现夹具首轮调整、错误与证据限制记录于 `evidence/core-repro-attempt-notes.md`。
- **本次动态实测：** 所列 Go 包、隔离 HTTP/SQLite 场景。**源码确认：** Execution 丢失、Authority 未接线、错误映射与恢复分支。**历史记录：** 任务12 completed/passed 等。**推断：** 未修复 queued 取消可能导致用户不希望的任务执行、多主体开放后的隔离后果。
- **未覆盖：** 本模块没有直接操作生产 DB，没有真实外部 Runtime 取消/进程崩溃，没有独立 browser 登录/CSRF/离线重测，没有在生产新增 operator/组织，没有穷举所有 API 或渗透审计。真实 Chrome queued 取消由 Web 模块提供；连续执行/物理效果由 Runtime 模块提供。
- **冻结边界：** 没有修改产品代码或冻结 ADR。没有修复 commit、没有部署；仅交付评估报告和证据。ADR-006 新旁支不算当前能力。

## 下一步

1. 优先局部修复 CORE-01/02，并用同一用户验收脚本覆盖“创建排队任务—取消”“等待输入—取消”“执行中取消后失联—未知结果”“随后新任务仍可执行”。
2. CORE-03 先 fail closed，避免继续扩大假支持；与独立 Task intent 工作一起对齐可理解的终态，不顺带扩大组织实现。
3. 明确当前单组织单 owner 支持范围并校正文档，暂缓岗位治理；需要开放多主体时再以 HTTP 授权矩阵为关卡处理 CORE-04。
