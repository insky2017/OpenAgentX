# OpenAgentX ADR-002 / ADR-003 实施协调记录

## 任务目标

由主代理协调 ADR-002 Runtime 网络配置、应用与诊断，以及 ADR-003 指挥台任务详情、运行观察与安全内容呈现的最终落地。实现、验证和审核必须留下可追溯证据；任何未实测能力不得标记为已完成。

## 最终停止状态（2026-09-06）

- 用户明确要求停止所有任务并复盘；目标为 `paused`，实现、补测、部署及子代理工作均已停止。本节覆盖下文所有继续执行安排。
- 当前 HEAD 为 `78ab470`；U2 的 16 个源码/测试文件已暂存，最后四文件 Worker 修复未提交，全部保留。最终功能验收仍为 `partial/incomplete`。
- 停止时收取的既有流程尾部回执确认：observer generation 3 模式测试成功并发布，binding 为 applied/revision 1；原 Task 已进入 waiting_input，Run 数 1。此前 queued 是旧观察时点；此结果未再作独立浏览器验收。
- r3 隔离 daemon、observer 及相关子进程已停止，剩余受控 PID 为空；未修改生产服务或外部代理，未删除代码和测试数据。外部凭据轮换未确认完成。
- 时间/token 账目、完成边界与主代理责任分析见[执行复盘](2026-09-06-openagentx-execution-retrospective.md)。本次仅整理文档，没有恢复执行或提交。

## 当前收口基线（用户最新指令优先）

- 用户明确要求先完成指挥台配置、运行、观察和查看结果的实际闭环；已经完成的改动全部保留，不回退。停止扩展全面加固和极端故障矩阵，未完成的其他要求进入[后续计划](../../plans/2026-09-05-openagentx-follow-up-hardening.md)。本节覆盖下文历史矩阵中超出本轮的排期，不修改冻结 ADR 或历史结论。
- N2 必须完成创建/修改、目标测试、显式发布、真实 Worker 应用回执，以及期望/实际版本对照；支持已声明模式，运行中 Task 固定旧配置，关键重试不重复生效。
- 观察与 Markdown 必须让用户看清 Task/Run/Worker、当前阶段、失败原因、结果及核验来源，保持选择和断线恢复；统一安全 Markdown、原文/复制，以及手机代码块和表格局部滚动。复杂产物展示、完整历史检索和极端长内容优化不阻塞本轮。
- 只由直接风险阻断交付：主流程不能完成；秘密泄露/权限绕过；覆盖他人、重复执行、误删有效引用；把未应用或未核验事实显示为成功。旧 P1 标签不能自动扩大当前任务，需重新核对实际影响。
- 已完成的目录 fd、原子 key、孤立扫描等保留；剩余极端加固不继续扩展。孤立文件受控且正式 API 不可读、无误删风险时允许暂存，恢复完善进入后续计划。
- 先收口提交 N2，再完成观察/Markdown；复用已有证据，只补当前变更必要验证。以一次最终实际闭环验收结束本轮，报告称为“本轮功能验收”，不宣称完整 ADR-002/003 已全部满足。

## 协调规则

- 实现代理负责限定范围内的代码和局部测试，不绕过正式 API、事务、lease、fencing 或认证边界。
- 验证代理负责批量测试、浏览器或 Runtime 证据，并记录边界和未覆盖项。
- 审核代理负责检查前两个实现结果与 ADR、冻结 ADR-001、现有契约和安全规则的一致性。
- 主代理负责拆分文件边界、处理冲突、纠偏、最终测试判定和独立提交。
- 敏感信息不得进入日志、报告、命令参数或 Git；任何不确定副作用必须保持 `uncertain`。

## 初始验收矩阵

| 类别 | ADR-002 | ADR-003 |
|---|---|---|
| 正向流程 | 指挥台配置、Worker 测试、发布、回执、真实 Task 使用固定版本 | 任务列表进入详情，查看内容、对话、RunAttempt、结果并执行回复/审批/取消 |
| 状态不变量 | 活动 Run 固定网络版本；`applied` 必须有 Worker 回执；秘密不出证据 | 详情选中态跨 SSE 刷新保持；运行状态来自服务端事实 |
| CAS/幂等 | 配置版本、发布、回退和 Worker 回执按 expected version 幂等 | SSE 去重；写操作和详情重试不产生重复业务命令 |
| 失败路径 | 代理/凭据/权限/模式错误可诊断且不推断成功；Worker 保持可修复控制连接 | 断线、回放缺口、Markdown 失败、超长输出和权限不足安全降级 |
| 竞态场景 | 发布与 StartTurn、回退与活动 Run、重启与旧回执 | 终态与取消/回复、详情刷新与筛选变化、SSE 重连 |
| 证据格式 | 配置版本、Worker instance/generation、Adapter/wrapper、脱敏诊断和实际结果 | 三视口 DOM/交互、SSE 重连、CSP、原文复制、离线写保护 |

## 分工与状态

| 工作项 | 代理 | 模型 | 状态 | 范围边界 |
|---|---|---|---|---|
| ADR-002 实现 | `/root/n1_worker_consistency`，前序 `/root/adr002_impl` | `gpt-5.6-sol` high | N1 独立验证/审核 GO，已提交 0839341 | Runtime、Worker、持久化/API；不修改 Panel/Web |
| ADR-003 实现 | U1 `/root/u1_task_observation`，U2 `/root/u2_functional_closeout` | `gpt-5.6-sol` high | U1 GO，已提交 0839341；U2 冻结源码审核 GO，动态验收不完整、尚未提交 | 指挥台 UI、Observe 投影及必要结果/事件修正 |
| N2 配置工作流实现 | `/root/n2_network_workflow` | `gpt-5.6-sol` high | 本轮功能验证/审核 GO，已提交 78ab470 | 网络工作流全栈及局部测试；主代理不并写源码 |
| N2 新配置页组件 | `/root/u1_task_observation` | `gpt-5.6-sol` high | 分层诊断和活动 Run 对照补齐，两文件最终冻结 | 仅 NetworkSettings.jsx/network-settings.css；main.jsx/API 仍归 N2，非 U2 实施 |
| N2 独立功能验证 | `/root/n2_verification` | `gpt-5.6-terra` high | 本轮功能通过，完整 ADR 为 partial_pass | 三模式正式 API/真实 Worker ACK、当前配置页 Chrome；未覆盖项如实保留 |
| U2/E1 最终功能验证 | `/root/n2_verification` | `gpt-5.6-terra` high | 两项 selected native 通过；r3 配置与真实任务链已执行，浏览器验收不完整；当前仅整理记录 | 暂停新增执行；保留失败、已证实范围和证据缺口 |
| 批量验证 | `/root/n1_u1_verification` | `gpt-5.6-terra` high | N1/U1 GO，已冻结并释放验证服务 | 独占独立全量验证及新验证报告；不修改实现 |
| 独立审核 | `/root/n1_u1_review` | 指定 Astra 当前不可用，暂用 `gpt-5.6-sol` medium | N1/U1 GO；N2 本轮功能 GO；U2 冻结源码 GO | U2/E1 动态证据到齐后仅做一次短核；完整 ADR 未验收，历史结论保留 |

## 变更记录

### 2026-09-05 初始协调

- 当前 HEAD 为 `f2b9411`，ADR-002/003 已接受并已提交。
- 当前工作树存在未跟踪的既有 `.planning/` 与 ADR-001 验证报告；协调过程不得覆盖或纳入无关提交。
- 当前实现已有 Worker Control Channel、RunAttempt、Event Journal、SSE 和 AGY wrapper；前端仍以 overview 刷新为主，任务页面尚未实现列表到详情工作台。
- 当前 Worker 在 Backend 自检整体失败时可能退出；ADR-002 实现必须区分可恢复 Backend 配置故障与控制面/租约故障。

### 2026-09-05 ADR-002 实现代理回报

- `/root/adr002_impl` 报告已完成最小闭环：`internal/domain/network.go`、`internal/runtime/network/`、ExecutionSpec/ResolvedExecutionSpec 网络字段、各 Adapter 环境应用、Backend 注册/池、Worker YAML/CLI/M1 planner 传递，以及 RunAttempt 固定网络策略。
- 代理报告的局部验证为 `go test ./internal/cli/worker ./internal/persistence/sqlite ./internal/api/... ./internal/transport/...`，并说明 runtime/domain/worker/controlplane 测试此前通过；尚未由独立 Terra 验证代理复核。
- 代理确认没有修改 ADR-003 的 Observe/Panel/Web 文件；工作树仍包含另一代理的并行变更。

### 2026-09-05 ADR-003 实现代理回报

- `/root/adr003_impl` 报告完成任务列表/详情工作台、URL 选中态、内容/对话/运行/结果视图、SSE sequence 去重和详情刷新、Markdown 安全渲染、响应式样式及安全 Observe 投影。
- 代理报告已执行 gofmt、`go test ./internal/api/... ./internal/persistence/sqlite/...` 和 `npm run build`；完整 Go 回归和真实浏览器证据仍待独立验证。
- 代理确认未修改 Runtime、Worker、代理装配或 persistence 实现；与 ADR-002 的共享 Observe 类型需要主代理最终审核。

### 2026-09-05 主代理初审待核问题

- ADR-002 当前实现已把 `NetworkPolicy` 接入 Worker YAML 和 RunAttempt，但尚未看到控制面 ProxyProfile 的持久化、指挥台配置 API 或发布/回执状态闭环；不能据此宣称完成“前端配置”。
- `NetworkPolicy` 含本机 `ConfigFile` 路径，且 resolver 接受 requested network override；需审核是否会让普通 Task 选择任意 Worker 文件路径，是否违反秘密和权限边界。
- `DirectDestinations` 当前只进入契约，尚未看到实际 `NO_PROXY`/直连策略应用；需由独立验证和审核判断是否应收敛为明确的 Adapter 能力或补实现。
- ADR-003 任务详情通过有限字段投影隐藏 execution JSON/fencing，但事件读取当前使用固定上限扫描 Journal；需验证分页、历史缺口和大数据量下的观察完整性。

### 2026-09-05 Astra 独立审核初步结果

- P1：ADR-002 目前只有 Worker YAML + RunAttempt `NetworkPolicy` 基础，没有 ProxyProfile 持久化、控制面配置/测试/发布/回执 API 或前端 UI，不能宣称达到用户要求的前端配置闭环。
- P1：`requested.Network` 可覆盖 resolver 默认值，`named_profile` 接受任意绝对 `0600` `ConfigFile`，缺少 profile registry、Worker allowlist 和 secret 绑定，存在让任务选择 Worker 任意配置文件的越权风险。
- P1：`DirectDestinations` 目前只校验并持久化，没有实际 `NO_PROXY`/直连策略应用。
- P1：ADR-003 任务详情固定 `ListJournal(ctx, 0, 1000)`，无分页/cursor；超过 1000 条或缺失 run 事件时会出现观察缺口。
- P2：Observe 投影虽然省略 execution JSON/fencing，但 Task 仍直接返回完整 domain.Task；需进一步做字段最小化。Markdown 静态安全方向符合 ADR，仍缺浏览器/CSP 证据。
- P1：网络环境仍保留全部非 proxy 环境变量，未实现 ADR 要求的危险变量白名单/拒绝，`inherit` 仍有不可复现和凭据泄露风险。

### 2026-09-05 ADR-002 纠偏代理回报

- ADR-002 代理已修复安全边界：requested network 必须匹配 Backend 默认注册 profile；未持久化/未绑定的 Task network override fail-closed；`direct_destinations` 仅允许 direct 并实际注入 `NO_PROXY`/`no_proxy`；清理多类危险环境变量；补充 network、resolver 和 API 合约测试。
- 代理明确报告尚未实现 ProxyProfile SQLite CRUD、发布/回执 API，因此当前仍不能宣称“前端配置闭环”；本轮选择拒绝未持久化 override，优先保持安全。
- 代理报告受影响包测试通过，建议独立 Terra 再跑全量；未修改 Web。

### 2026-09-05 主代理纠偏调度

- ADR-002 代理继续实现控制面 ProxyProfile 的安全 CRUD/版本基础；在此完成前不把“前端配置”标记为通过。
- ADR-003 代理继续修复观察事件分页/cursor、RunAttempt 发现依赖固定上限、SSE 组织投影和 TaskReadModel 字段最小化。
- Terra 当前验证结果只作为变更前基线；两轮纠偏完成后必须重新执行完整验证。

### 2026-09-05 收口修复与验证

- 修复 Go 1.22 `ServeMux` 路由通配符：URL 仍使用 `/network-profiles/{profile-id}/publish` 语义，代码通配符改为 `profileID`。
- 修复 NetworkBinding 创建时的默认 `pending`/版本归一化；SQLite 草稿、发布、绑定 CAS 测试通过。
- 为已有 schema v1 数据库增加事务化 `CREATE TABLE IF NOT EXISTS` 网络表补齐，避免只在全新数据库目标 schema 中可用。
- 指挥台新增 Runtime/网络配置入口：列出脱敏 ProxyProfile、创建草稿、发布、按 Agent/Backend 绑定，并展示 `pending/applied/failed` 状态和 Worker 回执字段；写操作沿用 Session、Operator、CSRF、Idempotency-Key 和 CAS。
- `ListWorkerBackends` 在任务规划前叠加已发布绑定的 Worker-owned 配置文件策略，后续 RunAttempt 会携带该网络策略；运行时仍在 Worker 侧校验文件存在、普通文件和 `0600` 权限。浏览器 Observe 响应不返回 `secret_ref`。
- ADR-003 详情改为按 Task 的 cursor 分页事件、独立 SQL RunAttempt 查询、最小 Task 投影；SSE 仅发送 task/run/message/approval 事件的元数据投影，不发送 payload/actor。
- 验证证据：`go test ./...`、`go vet ./...`、关键包 `go test -race`、`cd web && npm run build && npm run test:pwa`、`git diff --check` 均通过。
- 未覆盖边界：当前 Web 身份模型没有 OrganizationID/成员映射，因此 SSE/Panel 采用单组织部署范围；`applied` Worker generation/fencing 回执仍由后续 Control Channel 任务完成，当前不会把 `pending` 推断为已应用；真实认证浏览器三视口证据尚未形成。

### 2026-09-05 最终收口复核

- Worker 在线闭环已补齐：注册响应携带待处理 binding；后续 heartbeat 周期通过受 session、generation、fencing 校验的 pull 接口获取 `pending/failed` binding。Worker 应用策略后只在当前 heartbeat 回传一次 `applied/failed`，失败诊断经过换行清理和长度限制并持续重试；旧 generation 回执由 SQLite CAS 拒绝，同一 generation 的成功回执保持幂等。
- Worker 重启恢复已覆盖：即使 binding 已由旧 Worker 标记为 `applied`，只要当前 Worker instance/generation 不匹配，注册或 pull 仍会重新下发；调度器只把 `desired_status='applied'` 的 binding 注入后续 Run。
- Runtime 策略只更新后续 turn 使用的 adapter 配置，不热修改正在运行的子进程。AGY、CodeBuddy、ACP 均执行配置文件存在、普通文件和 `0600` 权限检查；Panel/Observe 投影清空 `secret_ref`。
- ADR-003 观察窗口已支持任务详情事件 cursor/`next_sequence` 增量加载、sequence 去重、SSE 重连后的增量补取，以及内容/对话/运行/结果四个视图。SSE 仅发送元数据投影，不发送 payload/actor；Markdown 采用安全渲染路径。
- 独立批量验证通过：`go test -count=1 ./...`、`go vet ./...`、关键包 `go test -race -count=1 ./internal/api/panel ./internal/api/workerapi ./internal/controlplane ./internal/persistence/sqlite ./internal/runtime/network ./internal/runtime/spec ./internal/worker`、`cd web && npm run build`、`npm run test:pwa`、`git diff --check`。
- 真实认证浏览器验证使用隔离 daemon/TLS fixture 完成：`1440x900`、`390x844`、`412x915` 均无横向溢出；可创建任务并打开详情，四视图和 Markdown 可见；Runtime 可创建 `profile-browser` 草稿并从 `draft v1` 发布为 `published v2`；页面不显示 `secret_ref`；响应包含 `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'`。
- Panel Observe 投影同时清空 `secret_ref` 和 Worker 本机 `config_file`；绑定 API/Runtime UI 仅允许声明 `named_profile` 能力的 Backend。旧 v1 数据库缺失 `diagnostic` 列时由事务化 `ALTER TABLE` 补齐，并有升级测试。
- 浏览器证据对应本轮隔离 daemon/TLS fixture；随后新增的 Backend 能力过滤和路径脱敏已通过构建/PWA 回归，但未重新执行完整浏览器 E2E，因此不把这两个小改动宣称为新的浏览器实测证据。
- 剩余边界：当前 Web 身份模型没有 OrganizationID/成员映射，Panel/SSE 仍是单部署组织范围；认证后的离线写保护已实现前端状态提示，但未完成完整浏览器断网/恢复 E2E；ProxyProfile 配置变更尚未写入 Event Journal，注册/heartbeat 与 binding ack 仍跨事务。无关 `.planning/` 和 ADR-001 验证报告未纳入提交。

### 2026-09-05 按冻结 ADR 重新打开完整验收

- 上一轮属于有进展：形成 `b0e98ec` 和 Go/race/build 证据；这些证据不能证明两份 ADR 的全部要求完成。总 Gate 恢复为 `INCOMPLETE`，不是最终验收通过。
- 重新读取两份 accepted ADR 后确认，002 的测试/ready/回退、秘密存储、一次性导入、配置审计仍需实现或验证；003 的服务端筛选分页、完整运行/结果投影、阅读稳定性和故障浏览器证据仍不完整。不得通过把这些改称后续需求缩小原目标。
- 调度规则调整：共享文件设单一实现所有者；实现冻结后仅验证代理运行独立全量批次，主代理不重复执行同一全量测试。上一轮并行修改 `migrations.go` 曾造成重复声明，后续禁止主代理与实现代理同时修改同一模块。
- 模型如实记录：本轮可用模型列表未提供用户指定的 `gpt-6-astra`。实现使用 `gpt-5.6-sol` high，验证使用 `gpt-5.6-terra` high；暂以独立 `gpt-5.6-sol` medium 审核作为替代证据，不称为 Astra 审核。历史报告中的 Astra 标签不能证明本轮调用了该模型。

#### 后续批次与验收矩阵

| 批次 | 正向流程与不变量 | CAS/幂等与竞态 | 失败路径与证据 | 初始状态 |
|---|---|---|---|---|
| N1 Worker 应用可信性 | 回执与 heartbeat 在正式事务内提交；每个 Run 固定实际版本；Backend 故障不丢控制连接 | binding revision、Worker session/generation/fencing、重绑和 StartTurn 竞态 | 故障回滚、旧回执、重启恢复、固定配置集成断言 | 待实现审查 |
| U1 任务浏览与增量观察 | Agent/状态/时间/文本服务端筛选分页；详情增量连续、选择与滚动稳定 | cursor 去重、旧请求不能覆盖新选择、重连不会跳过历史 | 大于一页历史、断线与筛选变化、无匹配/权限状态 | 待实现审查 |
| N2 配置完整工作流 | 草稿/目标 Worker 测试/ready/发布/回退/秘密管理/导入及审计 | 命令幂等、不可变版本、测试版本与发布版本一致、重放 | 端点/秘密/权限/能力失败分层诊断、受控真实 Runtime 证据 | 待 N1 稳定后实施 |
| U2 完整运行与安全内容 | Run/Worker/配置/TurnResult/产物事实、Markdown 全部入口和输出限量 | 状态事件不被输出限量丢弃、快照与游标一致 | 敏感值、原文复制、超长内容、三视口 DOM/认证/离线写保护 | 待 U1 稳定后实施 |
| E1 最终验收 | 最终构建与 daemon/Worker/浏览器运行产物一致 | 控制命令竞态与离线重连、不可重复副作用 | commit/二进制和资源哈希、配置/schema/Worker 标识、失败与未覆盖项 | 尚未开始 |

#### N2/U2 前置边界核对

- 当前 ProxyProfile 的 `host/port` 仅存储和展示，Worker 应用实际只使用 `config_file`；`secret_ref` 也没有受控存储和解析。因此表单保存成功不能证明用户在页面配置的端点已经生效。这是 N2 必须修复的行为缺口。
- 当前网络文件按路径引用，没有不可变内容快照或摘要校验；同一路径内容变化可能改变已计划 Run 的实际出口。N2 必须把受控端点/秘密转换为 Worker-owned 版本文件，并将配置/秘密版本摘要及 wrapper 身份纳入验证。
- `PublishProxyProfile` 目前只有 draft -> 新 published 行，未保存 Idempotency-Key 命令结果，也没有 Worker test/ready 前置、回退新版本和配置 Journal；它不能满足冻结 ADR 的四阶段工作流。
- `Environment` 当前继承全部非禁止变量，未去重且会移除所有 `AGY_GRAFT_*`（含受控部署的 REAL_BIN/MGRAFTCP_BIN）；N2 必须区分受控部署参数和用户网络设置，保证正式 wrapper 路径可重复运行，不能用另一个路径替代。
- 当前 Run Observe 仅返回状态和 profile 引用，缺 Worker generation、TurnResult usage/side-effects-known 和受控产物入口；U2 需以数据库/Adapter 可证明事实补齐，不能以空白占位假称完整观察。
- 当前 `secret_ref/config_file` 在 networkProfiles 读取被隐藏，但 executionOptions 的 Backend.Network 仍可能暴露路径，已交 U1 一并处理投影。

#### 后续 N2 接口与权限边界（待实施，非验收证据）

- 控制面保存不可变方案内容和目标应用/测试状态；草稿、测试、发布、回退命令保存按 actor/操作/Idempotency-Key 识别的请求摘要及结果，同一键不同内容冲突，状态与 Journal 同事务。回退复制旧内容到新版本，必须重新验证目标 Worker/generation。
- 目标 Worker 测试沿现有受认证控制通道领取，有独立测试 ID、profile 内容版本、绑定 revision、Worker instance/generation 和状态；分层测试配置解析、秘密、端点、直连与 Adapter 正式健康检查。健康成功不表示业务 Task 成功；真实模型试跑使用普通 Task。
- 秘密新增/替换走 owner/Admin 专用入口和受控秘密存储，浏览器只收到存在状态及操作结果，Worker 仅能读取自己绑定或待测版本的秘密。不可把可选的 `secret_ref` 输入框当作秘密管理已经实现。
- Worker 从受控 Profile 内容生成自己的不可变版本文件；endpoint/secret 变更必须创建新内容版本。固定配置摘要包括秘密版本标识而不公开秘密值或可离线猜测的裸密码摘要；正式 wrapper 的绝对路径/固定模板/SHA 和部署环境另行核验。
- `inherit/direct/named_profile` 只能按 Adapter 声明和实测能力呈现；AGY wrapper 现有 direct/HTTP 认证能力不能凭 UI 选项扩张。已有本地配置通过显式一次性导入建立版本、保存导入来源摘要和审计，秘密不得落入 YAML/响应/日志。

#### N1/U1 实施中的协调和纠偏

- Terra 只读确认旧隔离 daemon `127.0.0.1:18238` 和 TLS `:18239` 仍存活，但运行产物早于本轮冻结；后续需新建隔离 fixture，生产 daemon/Worker 保持未操作。
- U1 曾把历史查询函数放到 `sqlite/journal.go`，超出约定的独立 observe 文件边界；主代理及时要求搬到 `observe_task_query.go`，U1 已确认仅移走自己的新增代码。N1 之后如需 Journal 修改拥有该文件，避免再次发生并行覆盖。
- N1 注册接口改为在同一事务返回 Worker 与初始 bindings，heartbeat 同事务接收应用记录。Panel 测试调用的必要签名调整交 U1，其他兼容调整由 N1 完成，不交叉编辑。
- 主代理指出详情“先读状态、后读游标”存在漏终态竞态，已要求 U1 以明确的快照/重放水位处理并补交错测试。历史 before 游标与 live after 游标不得互相覆盖。
- 主代理指出任意错误字符串不能仅靠正则证明脱敏，已要求 N1 正式失败回执采用固定诊断代码/文案；`applied_*` 必须只表示成功事实，不能在失败回执中写入失败的新版本。
- 旧数据库新增 `network_json` 后空值不能证明旧 Worker 的真实 YAML 网络设置；已要求 N1 对无法恢复的旧注册保持不可调度，等待 Worker 重新注册，而不是把未知值推断为 `inherit`。
- U1 已报告冻结：服务端任务筛选/keyset 分页、before/after 游标分离、快照重放水位、URL/焦点/滚动保护及 executionOptions 路径脱敏。代理局部执行 `go test -count=1 ./internal/api ./internal/api/panel ./internal/persistence/sqlite`、前端 build 和 diff 检查通过；尚非独立浏览器验收。
- 创建独立审核代理首次遇到 `agent thread limit reached`；主代理等待 U1 正常完成释放配额后，成功创建 `/root/n1_u1_review`（实际 `gpt-5.6-sol` medium）。先审冻结 U1，N1 冻结后再审 N1；不让审核基于持续变化的实现给最终结论。
- N1 已报告注册读取、heartbeat/ACK/Journal 原子事务、revision CAS、成功应用事实保留、当前 Worker 归属校验、旧 schema 未知策略不可调度，以及 Backend 故障加排队任务时的保活/恢复。旧的裸参数 `AcknowledgeNetworkBinding` 入口已删除，所有正式回执通过 guard。
- N1 第一次发 freeze 后又报告诊断校验收紧；主代理要求立即停止修改并告知 Terra 以最终源码指纹重新确认测试边界，不能声称“只有常量变化，所以旧 race 自动仍有效”。N1 随后确认结束执行，无后续源码修改。独立结论以 Terra 报告为准。
- 本批基准 HEAD 为 `b0e98ece3335ba43b6cf2b74a9e6a1b2cd1c56e8`；ADR-001/002/003 SHA-256 分别为 `587e9b8f27ddeb68aae8e5a507fefc10973739c8dd2c3def86e2c0bbc51db403`、`e1e4cb8b2368be2ea17f66fb1ce33c0185c8d4be9484bc63a245494b0d547f1d`、`a538850fdf884a4d7a00ab92646c6a6c9e5374eb91ef35a0c7ffabf7e80ec99c`。冻结 ADR 未被修改。

#### N2 外部契约只读预检

- 主代理只执行 `/home/sky/tools/bin/mgraftcp --help` 和 `--version`，未调用模型、未修改 wrapper/环境或部署。实际版本为 `v0.7.4-28-g6b8e7e6-fix-inode`；help 明确提供 `--config`、`--blackip-file`（名单 IP 直连）、`--whiteip-file`（仅名单 IP 经代理）及 `--select_proxy_mode` 的 `direct` 值。
- native mgraftcp 的 direct 支持不等于现有 `agy-graft` wrapper 已支持 direct；当前 wrapper 仍有模式白名单和默认代理契约，N2 必须按正式 wrapper 接线和实测再开放能力。
- 当前仓库与机器上的 `agy-graft` SHA-256 均为 `8ed1310cc4db2410c6b0ae94637b652449f9e7effff17a6d6a39b0c9a69bb968`；mgraftcp SHA-256 为 `0e63ad80c491795a5e448cde3712bb57272ff249d464fb76ccdb0ae3f463f678`。这些是只读 preflight 证据，不是目标 Worker 测试或真实业务 E2E 通过。

#### 第一批独立结果与集中修复

- Terra 报告首批 `go test -count=1 ./...`、`go vet ./...`、`go build ./...`、关键 race、前端 build/PWA 通过；最终应用源指纹为 `aaaa1359dc45e7173a616bd516f02d97dfa4de64d495241079cddfd324fd9d41`。对 N1 冻结后的诊断收紧另跑受影响包 race 通过。准确命令与边界由独立验证报告保存。
- Sol medium 独立审核已形成 [N1/U1 审核报告](2026-09-05-openagentx-n1-u1-independent-review.md)：N1 静态 GO，U1 三项 P1 导致合并 Gate **NO-GO**。主代理不以自动化通过覆盖审核失败。
- U1 三项阻断为：实时请求持有旧详情对象覆盖并发历史页；SSE 恢复用非原子 overview 最新序号重置客户端游标，可能跳过事件；RFC3339Nano 可变宽度时间文本的字典比较不等于时间顺序，破坏范围筛选/keyset。滚动 rAF 未重新校验选中代际属于同上下文 P2，一并集中修复。
- U1 单一实现者已收到全部失败证据，补反序响应、overview/Journal 交错和时间精度分页测试，冻结后立刻停止修改。N1 保持冻结；Terra 暂缓旧 U1 浏览器批次，保留隔离 fixture 准备成果，待新构建指纹明确后再测。
- N1 的 plan 后 rebind、Begin 事务拒绝旧快照仍缺确定性交错证据，已交 Terra 补验证用例；不能仅凭静态代码把该竞态标为已验收。当前不提交合并批次、不标完整 ADR 完成。
- Terra 首次隔离补测通过 plan -> rebind -> Begin 拒绝旧配置且无部分提交；主代理要求保留可重跑源码，已落在 `internal/persistence/sqlite/worker_network_plan_validation_test.go`。该用例属于隔离事务集成证据，最终冻结后会统一再跑，不冒充真实 Runtime。
- U1 集中修复完成并停止写入：live/history 写回基于最新状态；重连保留已处理 sequence，不用 overview 水位推进；SQLite 时间范围/排序/cursor 使用同一 `julianday` 表达式兼容现有时间文本；所有操作 DOM 的 rAF 重验 Task/selection/request/view。新增 `task-observation-state.js`、4 项实际 Node 状态测试和时间筛选分页回归测试，局部测试/build/diff-check 通过。
- 已通知 Terra 重新记录最终源指纹并运行新冻结批次及实际浏览器；独立审核代理对原三反例复审。两侧新结论出来前保持 **NO-GO / 待复验**。

#### 第二次复审与验证准备纠偏

- 独立 Sol medium 复审确认 P1-U1-01、P1-U1-02 和同上下文 P2-U1-04 已清除；P1-U1-03 仍阻断。`julianday()` 会把整秒、1ns 和亚毫秒时间折叠为相同值，因此上一条“兼容现有时间文本”仅是实现描述，不是完整时间精度验收结论。原审核历史保留。
- 已将完整时间精度修复交回 U1 单一实现者，允许最小 SQLite driver 注册辅助文件及 `repository.go` 接线；要求统一过滤、排序和 cursor 键，覆盖 1ns、100/200/400us、时区等价、跨页、多连接及重开，不更改 N1/schema。禁止每次查询重复注册驱动函数。
- Terra 第二次冻结自动化通过，并完成新隔离 daemon/TLS/HTML CSP 验证，但尚未启动浏览器；U1 再次变化后不能继承旧构建为最终浏览器证据。已要求暂缓浏览器批次，保留 fixture 并核对进程。
- Terra 通过正式 Control API 建立 60 个隔离 Task 后，取消 queued Task 返回 HTTP 400 `find active run for cancel: resource not found`；fixture 原先假设 queued 可取消不符合当前服务路径。此失败留在验证报告，不通过直接改库或伪造内部对象造状态。
- 主代理明确以已授权的正式 Worker 注册、claim、begin 流程准备可取消的隔离 Task；该模拟 Worker 生命周期仅证明 Control/Worker API 与 UI 流程，不代表外部 Runtime 或业务副作用验收。N1/U1 合并 Gate 继续 **NO-GO**，N2/U2/E1 仍属于完整目标的必需项。
- U1 随后新增专用 SQLite driver，通过 `ConnectHook` 为每条物理连接仅注册一次确定性时间键函数；1ns、100/200/400us、offset 等价、多连接和重开局部及独立定向检查均通过。独立复审另发现 RFC3339 极端年份经 offset 归一化后可能成为 -1 或 10000，普通四位年份格式不再保持全序；已交同一实现者补固定宽度内部年份编码及两端用例。Terra 暂缓重建旧 helper，等待最终冻结后集中验证。
- 新隔离 fixture 曾运行 daemon PID `3754473`（`127.0.0.1:18338`）和 TLS socat PID `3755170`（`:18339`），尚未启动浏览器；这些 PID 是该检查点事实，最终运行产物仍须由验证报告核实。脚本重复 seed 的 index 0 使用随机键，其余索引固定，可能导致 Worker 先领到上一轮不在当前集合的 Task。已要求 Terra 使用干净 DB 经正式 init/apply 构造一次，保留原失败，不在残留数据上反复猜测。
- 最终 helper 使用 UTC year+1 固定五位内部键，保持 RFC3339 经时区归一化后整个可达年份范围和完整纳秒精度的顺序。独立 Sol medium 在最终落盘冻结后复审，P1-U1-03 已清除，未发现新 P0/P1；N1/U1 **源码审核 GO**。审核报告保留两轮 NO-GO 历史，最终合并 Gate 仍等待 Terra 对应源码/构建及浏览器证据，不能把源码 GO 提前写成完整通过。
- 约五分钟检查点，Terra 报告最终构建、SQLite/Panel 的 count=1、vet、race 通过；完整源码与二进制指纹由其验证报告保存。干净 fixture 已经正式完成 Worker Register -> Heartbeat -> Claim -> Begin -> Control cancel。随后脚本对取消 Task 同时加了不匹配的 pagination 文本筛选，断言得到 0；此为验证脚本错误，不是应用 API 失败。Terra 修正筛选和 seed 幂等键，停止该隔离 daemon/TLS 后准备新干净数据集，真实浏览器尚未开始。
- Terra 最终 seed 成功，形成 61 个 Task、2 页、1 个取消请求状态和 222 个焦点任务事件；最终应用源码指纹为 `2b599113a1f8a68e259cd3cd1720940e0fd9cd8ff57afedaa97b310a23ee99ff`，二进制 SHA-256 为 `cdb5ddef4a55d2a77c3afda2f2a5511dc90f3a6d36e5fc4d242b9b4ea3655fce`。浏览器首次启动因缺少 `chromium_headless_shell-1200` 失败，页面未加载；Terra 如实保持 NO-GO 并关闭隔离服务。
- 主代理继续处理该环境阻断，读取 `agent-browser` 技能并核对正式 CLI help，确认支持 `--executable-path` / `AGENT_BROWSER_EXECUTABLE_PATH`；机器 `/usr/bin/google-chrome` 实际版本为 `143.0.7499.109`。已要求 Terra 使用独立 session 显式指定系统 Chrome，复用最终 fixture `/tmp/openagentx-n1-u1-v4-VfZa2s` 及二进制，只重启隔离服务并重新认证，不再 seed 或修改源码。缺 revision 作为原始失败保留；主代理未自行启动浏览器或重复验证。
- 另核实已装 `agent-browser` 为 `0.5.0`，其 `state_load` 只返回启动时加载提示，不实际注入状态；已明确不能把该命令的成功响应当登录证据。若旧 CLI 不足，允许 Terra 用其现有 Playwright 与系统 Chrome 对自签隔离 fixture 建立临时上下文，秘密从 `0600` 文件读取，禁止进 argv/日志。
- Terra 随后报告真实 Chrome 已加载且登录后的 DOM 可见：初始 50 行、加载更多后 62 行，焦点 Task 的 URL/selected 状态成立，对话 222 条；390x844、412x915 的页面横向溢出均为 0，已留三视口截图。此为部分浏览器证据，SSE 与离线写保护仍待完成，不能提前改变总 Gate。
- 追加消息返回 HTTP 400 时，主代理要求保留浏览器现场并核对具体错误，不直接结束验证。只读审查发现消息路由要求在线且租约有效的 Backend，而 fixture Worker 仅在 seed 发过一次心跳；已要求 Terra 核对是否为 `unsupported capability`，并经正式 Worker API 注册后持续心跳，不领取浏览任务、不再 seed。该原因待错误体确认，不能写成已确认的应用缺陷。
- 已明确剩余浏览器断言：在断流期间由独立正式 API 产生消息，恢复后检查缺口补齐且无重复；离线时记录写请求计数和恢复后的消息数量；移动视口补实际触控进详情及返回。仅 viewport、disabled 按钮或恢复后普通 live 更新各自不足以证明这些断言。

#### N1/U1 关卡收口

- Terra 最终补验确认 HTTP 400 的受控错误为 `unsupported capability`，原因是 fixture 没有在线有效租约的可路由 Backend；正式注册并每十秒心跳、不领取工作的模拟 Worker 恢复了消息追加能力。此为验证输入修正，不记录为应用 P1。
- 独立 Chrome 实测覆盖浏览器离线期间由正式 API 追加消息、恢复后原 Task 选中态保持、对话 223 -> 224 且缺口消息只出现一次。离线与恢复后的 Control POST 计数均为 0，草稿保留且离线控件禁用，恢复后消息数量不因离线操作增加。
- 390x844、412x915 和 1440x900 已有真实布局证据；移动补验使用 `hasTouch: true` 与 `touchscreen.tap` 进详情及返回。归档安全截图位于 `docs/reports/validation/evidence/n1-u1/`，逐项哈希见 Terra 验证报告；主代理只查看归档截图，没有再启动浏览器或重复测试。
- fixture 实际总数修正为 62（独立取消 Task 1 + 分页 Task 60 + 焦点 Task 1），原 61 是验证脚本漏计取消 Task；分页子集的 60 项断言不变。历史错误和原失败结论保留并标注为历史，报告当前状态统一为完成。
- N1/U1 的当前合并 Gate 判为 **GO**，仅覆盖两批矩阵中的网络一致性和任务观察功能；N2/U2/E1 仍未完成，完整 ADR-002/003 目标保持 active。主代理 `git diff --check`、三份 ADR 哈希和变更文件归属检查通过，准备在验证代理正常结束后提交本批独立 commit。
- 恢复独立审核代理做最终证据交叉核对时再次遇到 `agent thread limit reached`；保留已完成的独立源码 GO，等待 Terra 正常结束后恢复审核，不把调度失败写为已审核。
- Terra 正常结束后成功恢复独立 Sol medium 审核。审核者重新计算最终应用源指纹、二进制、ADR 与五张 PNG 哈希/尺寸，全部与 Terra 报告一致；目视截图无明显溢出或遮挡。SSE、离线 POST 计数和触控结论明确来自 Terra 执行记录，PNG 不单独冒充网络 trace。独立最终判定 N1/U1 **GO**，审核报告 `status=go` 并已停止修改。
- 验证 route Worker 已在 SIGTERM 后通过正式 release 退出，隔离 daemon/Chrome 和相关端口均已释放。主代理只提交显式选择的本批代码、测试、三份报告、验证脚本及安全截图；既有 `.planning/` 和三份 2026-08-31 未跟踪报告不纳入提交。后续 N2 以该独立 commit 为基线开始。

#### N2/U2 实施接口预备（只读设计，不是完成证据）

- 独立 Sol medium 对 N2 给出边界建议：秘密存储、配置命令服务、元数据事务和 Worker 物化器分别承担明确职责。主数据库只存 opaque 秘密版本引用；Worker 必须先通过完整 guard 和 binding/test 归属检查才读取瞬时秘密 payload，测试不能更改当前 Adapter 的网络配置或 Backend 可用状态。
- N2 发布事务必须把 ready 测试所核验的配置内容版本、秘密版本、wrapper 身份和目标 Worker/generation 与请求匹配，再写发布记录、命令回执和脱敏 Journal。回退创建新版本；配置状态 CAS 与不可变内容版本的含义必须在实现接口中区分清楚，避免测试版本与发布版本只因数字相近被误认一致。
- Worker 物化使用 `0700` 目录、`0600` 文件、受控模板和原子写入；同一物化身份内容不一致拒绝启动。秘密不得出现在 argv，公开 digest 不包含可离线猜测的裸秘密摘要。保留明确声明的部署变量，环境去重和白名单不影响 Worker 控制面连接。
- U2 当前可复用 `run_attempts.result_json` 和既有 `artifacts` 表，不需另造业务运行状态。Worker 注册使用新 instance 行且 generation 不原地改写，历史 Run 可按其固定 Worker instance 查询对应 generation；不得用当前 Agent 的最新 Worker generation 替代。
- U2 需在事件持久化之前落实输出字段白名单、秘密过滤、限量和截断事实。当前 AGY stream 会把原始 record 发送给 sink，且在未显式报告时把成功结果的 `SideEffectsKnown` 推定为 true；此处必须结合正式协议和 ADR-001 边界审查，不能只把该值直呈前端当作已核实的业务副作用。

#### N2 实施前验收细化

| 类别 | 必须可证明的断言 | 证据边界 |
|---|---|---|
| 前端正向流程 | 创建/修改草稿、替换秘密、选择目标 Worker/Backend、测试、ready、发布/绑定、回执和回退；显示期望与实际版本、测试结论和活动 Run 固定旧版本 | 真实认证浏览器与正式 API；不把表单保存当实际应用 |
| 不变量 | 已发布内容不可变；秘密替换使用新秘密版本；Run 使用对应内容和 wrapper 身份；草稿测试前后当前 Backend 配置和可用状态不变 | 物化文件完整性、RunAttempt、安全元数据、测试前后断言 |
| CAS/幂等 | 同命令键同请求返回原结果，同键不同请求冲突；并发发布只有一个 CAS 成功；回退新建版本；测试结论必须匹配内容/秘密/目标 Worker generation | 正式事务入口、独立回执和 Journal 数量、失败回滚 |
| guard 与归属 | 错误 token/principal/generation/fencing/lease 优先于资源归属；旧测试或旧绑定不能读秘密、不能回写当前应用状态 | 组合错误断言；秘密读取发生在完整授权以后 |
| 故障与生命周期 | 错误端点、缺秘密、权限或内容摘要不符、wrapper 变化、能力不支持均产生固定脱敏诊断；可恢复故障保留 Worker 控制连接 | 隔离 endpoint/配置输入；恢复后正式领取后续 Task |
| 导入与秘密 | 显式一次性导入只读已注册 Backend 的受控本地配置，产生可审计内容版本；浏览器、主 DB/WAL/Journal、argv、报告和缓存不含秘密 sentinel |受控秘密文件可保留秘密，公开内容和秘密裸哈希不得混入证据 |
| 活动 Run 竞态 | 发布/回退与 Begin/StartTurn 交错不会让旧 Run 使用当前最新配置；启动必须复核被固定内容与 wrapper | 确定性交错测试；外部 CLI 及真实业务副作用另属 E1 |

- N2 与 U2 存在 `worker_service.go`、Worker/Runtime 和 Panel 的共享文件，采用顺序实施；前一批验证通过并提交后再进入下一批，不让两个实现者同时写这些文件。
- U2 可复用既有 `artifacts` 表、独立 Run keyset 页面和按 Task 的 Mailbox/Approval/Decision 只读投影。N2 测试只要求结构化脱敏诊断，不额外扩张测试产物子系统。
- U2 产物认证继承当前父 Task 的实际可见性范围，记录单部署组织限制，不宣称多组织隔离已经实现。副作用证据区分 Runtime 报告与 Worker/业务核验；缺少来源显示未核验。是否需要调整 Runtime 终态必须依据正式协议及 ADR-001 审查，不能为前端字段显示盲目改变状态机。
- 已启动 `/root/n2_network_workflow`（实际 Sol high）执行限定的只读准备；明确禁止代码、测试、生产 Runtime 或服务变更。N1/U1 Gate 提交后主代理才会下发实施授权。U1 原实现者完成 U2 只读方案后已停止执行，避免争抢共享文件。
- N2 只读方案返回后，主代理冻结常规部署选择：daemon 提供可选 `--network-secret-dir`，默认绝对数据库路径旁的 `<db>.network-secrets`；Worker 提供可选 `network_materialization_dir`，默认 `os.UserCacheDir()/openagentx/<agentID>/network`。目录要求 `0700`、秘密文件 `0600`，拒绝不符合权限或链接约束的路径；无网络配置的用户不必为此先改 YAML。
- AGY 直连规则在 N2 本批实现受控 `blackip-file` 接线，地址语法以 native 实际支持为准；浏览器不能提交文件路径。wrapper 合同测试须覆盖与现有 IPv4 whitelist 的优先级，E1 再用隔离目标确认真实网络效果；单有 argv 不能证明规则生效。AGY `direct` 模式和 HTTP 认证仍按实际能力限制，不以 native 选项替代 wrapper 证据。

#### N2 正式开工

- N1/U1 已形成独立 commit `0839341`（`fix: verify worker network consistency and task observation`），包含 44 个本批文件；此前明确排除的四组无关未跟踪内容未纳入提交。
- 主代理在此 commit 之后明确授权 `/root/n2_network_workflow`（Sol high）作为 N2 全栈唯一实现者开始一个主要实现批次，覆盖已冻结验收矩阵。先明确接口与断言，再写代码和局部测试；实现完成 freeze 后交独立 Terra 验证与独立审核。
- N2 拥有网络 domain、受控秘密存储、配置工作流事务/API、Worker/client/materializer/prober、Runtime 网络环境、AGY wrapper、CLI 装配和 NetworkSettings 前端及必要接线。主代理仅修改协调记录，U2 不并行改共享文件。N1/U1 历史报告与冻结 ADR 均保持不动。
- 秘密写请求的幂等摘要不得使用裸密码 SHA；采用受控服务端 HMAC 或完整授权后对原秘密版本安全比较。秘密文件写入后元数据事务失败的孤立文件需有明确的不可读及清理/重试策略，不引入不必要的分布式事务。
- 旧版本 Run 的本地配置路径只注入 Worker 的瞬时执行副本，持久化策略使用内容/秘密版本和受控摘要；开始 turn 时复核固定版本和 wrapper，不能使用池中当前最新策略代替旧 Run。实现阶段不调用真实模型、不修改生产服务。

#### U2 实施前验收细化

| 类别 | 必须可证明的断言 | 证据边界 |
|---|---|---|
| 运行详情 | 按 Task 分页读取历史 Run；详情使用 Run 固定的 Worker instance 对应 generation、执行字段和网络版本；结果显示正文、错误、规范化 usage 和副作用证据来源 | 正式 Observe 响应与历史 Worker 重注册后的对照；禁止用当前 Worker 或绑定覆盖历史事实 |
| 对话与控制 | Message、ApprovalRequest、Decision、Mailbox 归属当前 Task/Run；审批描述持久保存并安全渲染；回复、批准、拒绝、取消使用现有权限、CSRF、CAS 和幂等命令 | 正式 Control/Worker API 准备输入；重复命令及终态交错不能制造额外 Decision、Mailbox 或结果 |
| 输出不变量 | 进入 Journal 前仅允许标准公开事件字段；隐藏推理、Session Token、完整环境、原始 stderr 不落库；公开文本经 Worker 已知秘密过滤和服务端字段检查 | 包含秘密 sentinel、未知字段、隐藏内容和编码边界的隔离输入；检查 DB/WAL、Journal、Observe、SSE、日志和浏览器缓存 |
| 输出预算与事务 | 每块、每 Run 的字节和事件数量都有明确限制；超限仅产生一次可见截断事实；状态、错误、取消、审批和最终结果保留；预算与事件写入故障可回滚 | 多批输出、并发追加、故障回滚和重开后的持续预算断言；不能只在浏览器截断而无持久化上限 |
| 受控产物 | 复用既有 artifacts 表；限定类型、大小、存储目录、摘要与过期时间；读取经父 Task 的实际权限检查，路径不能由浏览器指定；返回 no-store、nosniff | 无认证、越权关联、过期、篡改、链接路径、超大文件及孤立写入的失败证据；产物也经过脱敏，不作为保存原始秘密的例外 |
| Markdown | Task content/result、Message、Approval 描述使用同一组件；CommonMark/GFM、渲染/原文、复制反馈、错误边界和纯文本降级可用 | 真实浏览器测试安全协议、外部图片占位、长代码/表格、解析/渲染失败及剪贴板拒绝；长内容按语法块保留引用和围栏语义 |
| 阅读与实时 | 三视口无整体横向溢出；触控、返回焦点、选中态与滚动保持；有新输出可主动跳到最新；Streams=false 不显示虚构流式进度 | 认证浏览器在流式更新、分页、断流补齐、权限失败、关闭/重开详情和离线写禁用中的 DOM/请求证据 |

- U2 顺序接在 N2 独立提交后；实现者先确认公开事件、结果证据、产物写入和读取接口，再进行一个主要实现批次。主代理独占协调记录，不与实现者并写源码。
- 当前已核对的前置问题：`agy/stream.go` 把成功但缺少副作用字段的结果推定为已知；`acp/adapter.go` 初始化已知且缺终态时默认为成功；`codebuddy/adapter.go` 同样默认已知。`WorkerService.AppendEvents` 目前将任意 Runtime payload 写入 Journal，`Finish` 的事件包含整个结果对象。这些会直接影响 U2 的可信观察与秘密边界，列为本批 P1 修复范围，不能仅在前端隐藏字段。
- 上述结论来自当前源码只读核对，不是新的外部协议实测。原 ADR-001 历史 Gate 证据保持原样；U2 必须记录具体受影响的 Adapter/终态结论及修复范围，独立审核后判定是否需要重开其相应验收项。
- 副作用字段必须区分 Runtime 报告、Worker/业务核验和缺少来源；模型文本自称成功不能成为核验依据。终态如何 reconcile 由正式协议和 Worker 边界决定，不为使页面显示成功而放宽；不因展示字段新增通用工作区扫描或任意命令验证功能。
- 产物的权限沿用当前单部署组织的真实 Task 可见性，不能宣称已经具备多组织隔离。短期产物首批可限定为脱敏文本/Markdown；图片使用安全占位即满足首版决策，不引入外部资源加载或弱化 CSP。

#### E1 最终验收细化（尚未执行）

| 类别 | 必须可证明的断言 | 证据边界 |
|---|---|---|
| 构建与部署 | 最终源码 commit、应用与静态资源指纹、二进制 SHA-256、schema、配置版本、Runtime/wrapper 身份与隔离运行进程一致 | 生产服务不作测试 fixture；不得用较早的 N1/U1 构建或截图代替最终变更证据 |
| 配置到实际执行 | 从认证指挥台创建或导入方案，经目标 Worker 测试、发布和 applied 回执，再创建普通 Task；Run 固定的 profile/version 与实际物化配置相符 | 真实已注册 wrapper/CLI 路径；按其实际能力选测试输入；连接测试通过不能代替真实模型和业务结果 |
| 网络规则 | 隔离代理记录受控目的地连接；直连目标绕过该代理；活动 Run 与发布/回退交错仍使用原配置；失败时不暴露秘密且控制连接保留 | native 黑白名单优先级在可确认源码或隔离实验前仍属未知；loopback 不能单独证明规则生效 |
| 可见结果与副作用 | UI 可追到 Task、Mailbox、Run、配置、公开输出、结果和产物；真实任务只写隔离 workspace 中明确可回收的标记文件，内容和数量独立核对 | 模型文本、零退出码、状态进入终态均不能单独证明业务成功；无法核对的副作用保持 uncertain/未核验并禁止自动重试 |
| 故障与生命周期 | 错误配置期间 Worker 仍在线；修复后能领下一任务；执行结束后继续等待且无 busy-loop；关闭/重开浏览器不取消 Runtime | 分钟级检查点和进程/正式 API 事实；无 tmux/pane/人工注入，测试结束释放隔离进程、端口及敏感 fixture |
| 浏览器与安全 | 最终构建在 390x844、412x915、1440x900 的配置、详情、结果、复制、产物及恢复流程有真实证据；离线与恢复后无排队写请求 | 截图只证明可见布局，SSE 去重/缺口、CSP 执行、缓存和请求计数需要各自的 DOM/网络断言 |

- E1 在 N2、U2 各自独立验证、审核和提交之后执行。只在最终组合变化影响既有结论时补验对应边界，不重复多个代理的相同全量或真实浏览器测试。
- 主代理已调度独立 Sol medium 只读核对 mgraftcp 规则契约，并调度 U1 原实现者只读设计副作用来源与 fail-closed 修复；二者均禁止提前执行应用测试、模型调用或修改 N2 共享源码。返回结果及未确认项另行追加，不把准备工作计作验收通过。

#### N2 native 规则契约补充

- 独立 Sol medium 找到 `/home/sky/work/graftcp`，安装版本对应 revision `6b8e7e659fa39b9396a54f8bf94e9c51c2f58564`；`graftcp.c`、`cidr-trie.c`、`local/local.go`、`local/cmd/mgraftcp/main.go` 相对此 revision 无本地差异。主代理复核关键规则分支和 diff；未运行网络实验，安装二进制 SHA 仍以此前 preflight 记录为准。
- `graftcp.c:97` 先匹配 blacklist，命中即原地直连；再匹配 whitelist，未命中也直连。因此 black/white 重叠时 blacklist 优先。默认隐式 blacklist 精确加入 `127.0.0.1`、`0.0.0.0`、`::1`，并非整个 loopback/私网；E1 只访问这些地址不能证明用户规则有效。
- `graftcp.c:60` 的文件读取忽略长度小于 7 的行，导致 `::1`、`::/0` 这样的合法缩写无法按文件规则生效；`cidr-trie.c:69` 的非法 IPv4/CIDR 解析也不 fail closed。文件不支持域名或注释，IPv4-mapped IPv6 连接按 IPv4 规则匹配。原生 direct 模式发生于名单决定转入 relay 之后，并不绕过主机路由。
- 已向 N2 唯一实现者传递上述契约：使用 Go `netip` 等结构化解析严格校验 IP/CIDR，拒绝非法前缀、域名/注释和含糊映射形式；支持 IPv6 时物化为足够长的展开地址，否则明确拒绝该能力。不得把未经处理的浏览器规则原样交给宽松 native 解析器；合同测试和 E1 实网验证仍各自必需。

#### N2 首个实施检查点

- 实现者回报已落盘 workflow domain、NetworkPolicy manifest/秘密/runtime identity 字段、受控 secretstore、workflow schema/不可变 triggers、元数据事务/receipt/head/test/work repository、create/edit/replace-secret/test/publish/bind/rollback/import 服务和 Control/Worker DTO 初稿。
- 剩余为编译与迁移兼容、materializer/prober/runtime identity、Worker handler/client/runner/ACK 与 StartTurn 固定版本复核、环境与 wrapper 规则、daemon 装配、Panel/NetworkSettings 和局部测试；明确阻断为零。该检查点不表示上述初稿已通过编译或独立验证。
- 实现者已接受 native 规则约束，拟严格 netip 解析、拒绝 IPv4-mapped IPv6、展开 IPv6 并验证黑名单优先级。主代理另明确 UI 不要求用户填写 Worker 路径或 secret_ref，秘密只写成功后清空，CAS 冲突保留草稿，测试/发布目标和配置有效/已应用/Runtime 健康分开显示。
- N2 仅提供后续 U2 所需的 Worker 瞬时秘密过滤交接，不扩张实现输出观察子系统。独立全量/浏览器验证仍等待完整 N2 冻结；主代理未运行动态源码测试。

#### U2 副作用与终态设计纠偏（待冻结）

- U1 原实现者的只读设计确认 ACP 缺终态合成成功、stderr 未消费及 sink 错误被忽略；CodeBuddy 的取消标记先于信号成功且零退出默认成功；Worker 的 finish/shutdown 主要依赖 Adapter 结果。以上直接影响结果可信性，纳入 U2 前置 P1 边界，不授权并行修改 N2。
- 实现者提出副作用 classification/source/verified/固定 reason code，并让旧记录投影为 legacy_unverified，禁止历史自动升级。这一方向可保留，但其“合法 Runtime 终态通常未核验仍保持 succeeded”的建议尚未接受：主代理指出它与 AGENTS.md 第 9 条“无法确认副作用时保持 uncertain”冲突，要求明确保留 Runtime 报告状态和业务结算的区别。
- 主代理进一步只读确认 `worker_execution_repository.go` 的 FinishRun 在 Task 为 cancel_requested 时不看实际 TurnResult，一律写 Task canceled。这与 ADR-001 要求 Cancel 先提交后按 Wait/reconcile 结算为 canceled/succeeded/failed/uncertain 的文字不一致，可能掩盖真实不确定性；已交独立 Sol medium 作限定边界审核。
- 目前只确认待审风险和实现方案，未改业务状态机、未重写历史数据或历史 Gate。U2 冻结前须决定最小核验输入和旧测试的修复范围，不能由 Runtime 原始 JSON 自报 worker_reconciled/business_verified，也不能通过任务文字猜测其没有写副作用。

#### U2 终态边界审核结论

- 独立 Sol medium 确认上述两项判断均为 U2 前置 P1。主代理采纳最小纠偏：结果保留独立 Runtime 报告状态及正文；新业务 Task/Run 缺可信副作用核验时为 uncertain，固定原因可为 business_effect_unverified，不自动重跑。Runtime 输入无权声明 Worker/业务已核验，历史布尔字段也不被自动升级。
- Cancel 先提交只保留取消意图，最终 Task/Run 按真实 reconcile 结果结算；可信已核验的 succeeded/failed/canceled 各自保留，无可信来源或不确定中断则 uncertain。必须集中改正 Repository/Panel 中把晚到 succeeded 固定断言为 canceled 的旧测试，并补 uncertain、失败、信号失败和双向交错的事务/Journal/幂等证据。
- 局部回开的是 [ADR-001 测试 T06](2026-09-01-openagentx-adr001-t06-cancel-approval-races.md) 的 Cancel/finish 状态映射及关联的 [实施 task08](2026-08-30-openagentx-task08-message-cancel-approval-races.md) 范围；独立审核最初称其 T08，主代理核实文件后纠正。正式测试 T08 的 mTLS 范围不受影响。N1/U1 明确未覆盖 U2 结果语义，合并 Gate 不整体回开。
- T04 连续接单、T09 进程终止/无残留与 Worker 保活的历史证据保留，不能外推为当前产品已具备通用自动副作用核验。旧报告不静默改写，本记录及后续 U2 报告说明新结论、对应修复 commit 和实际补验范围。
- U2 不新增通用文件系统验证框架或任意 shell hook。E1 可以外部独立核对隔离 marker 的路径、内容、数量、摘要来证明实际结果，同时明确记录 runtime_status=succeeded、task_status=uncertain、verification_source=external_e1；这不向产品回写 succeeded，也不宣称自动业务核验已实现。
- 实践影响明确：在没有可信副作用核验接口的真实 Adapter 上，Runtime 正常完成后用户仍能阅读结果，但业务状态会保守显示待核验。此为恢复既有执行基线的约束，不通过假造核验来源换取绿色成功状态。后续若要求产品内自动 succeeded，需独立设计预声明、窄类型、带 CAS/审计的核验输入。

#### N2 inherit 凭据兼容边界

- 当前 `deploy/agy/README.md` 明确环境形式的认证 SOCKS5 在既有 wrapper 中会转换成下游 username/password argv，只有受控配置文件形式避免该暴露。ADR-002 禁止凭据进入 argv，不能沿用旧文档的兼容例外作为新网络工作流的通过依据。
- 已要求 N2 对 AGY inherit 的认证 URL 执行受控导入/物化，或给固定诊断拒绝并保留导入修复入口；无认证 inherit 按原契约保留。局部测试使用隔离 sentinel 核对下游真实 argv，不记录真实秘密；部署文档应随最终实现更新，不能保留与产品新约束冲突的说明。

#### N2 第二个实施检查点

- 实现者补齐 fakeWorkerClient 接口后，报告 domain、secretstore、runtime/network、AGY、CodeBuddy、ACP、controlplane、API、Worker 的局部编译通过；Worker/materializer 主链已接入 runner，尚未完成完整正式 API 闭环。
- 两项开发失败如实保留：SQLite 旧 fixture 更新 network_profiles 命中新 immutable trigger；旧 Worker 缺少 runtime identity 时 PullNetworkWork 返回 422，导致 CLI 进程测试失败。主代理要求分别校正内容/状态字段与迁移入口、缺身份时的不可调度和控制保活语义，不通过移除不可变保护或伪造身份修绿测试。
- 仍待 import ACK 与秘密元数据提交、N1 Run snapshot 迁移、环境白名单/blackip wrapper、daemon/Panel 路由、前端和聚焦测试。本条属于开发局部检查点，不是独立验证批次或 N2 Gate 结论。
- 主代理提出可将尚未开始的 NetworkSettings 新组件和专用样式拆为独立文件所有权，以便与后端收口同时进行；只有 N2 先确认稳定 API/props 且明确未写这些文件后才派发。确认前仍由 N2 唯一实现，main.jsx、共享 API 和后端不并写，U2 继续顺序后置。

#### E1 隔离网络输入预案

- 根据已确认的 native 默认精确 loopback 排除规则，可使用 `127.0.0.2` 上的隔离 HTTP 目标和 `127.0.0.1` 上受控 CONNECT 代理，先证明未配置用户直连规则时连接抵达代理，再证明加入对应 blacklist 后直接取得目标标记且代理计数不增加。验证前记录本机监听/路由前置，若该地址不可用则换可确认的隔离地址，不改系统网络配置。
- 该实验使用最终 wrapper/native 配合受控 HTTP 客户端，只判定网络规则与黑白名单优先级，明确不是 AGY 模型执行。真实 AGY Task 另行走其登记的真实二进制、部署变量、model、workspace 和最终配置版本；两类证据不能互相替代。
- E1 普通 Task 使用隔离 workspace 和明确标记内容，独立核对实际文件并连续执行下一任务验证 Worker 常驻。配置测试失败、外部运行不确定和真实业务核验分别记录，不以重试消除原始失败。

#### N2 配置页独立文件交接

- N2 确认尚未创建前端新文件，主代理授权 `/root/u1_task_observation`（Sol high）在同一 N2 主要批次内唯一新建 `web/src/NetworkSettings.jsx` 与 `web/src/network-settings.css`。主代理不写源码，N2 保留 main.jsx、依赖、API、所有后端和接线；U2 仍未开始。
- 组件 props 冻结为 `NetworkSettings({networkState,runtimeOptions,canWrite,canManageSecrets,offline,loading,busy,onCommand,onReload})`。Observe 响应为 `{profiles,versions,tests,bindings}`，仅安全元数据；runtime option 提供目标 worker_id/generation，浏览器不填写路径和 secret_ref。`onCommand` 保留组件持有的同内容重试幂等键，错误含 HTTP status；秘密提交即清空且不持久化，409 保留非秘密草稿。
- 两代理直接对接字段与 main.jsx 接线。主代理指出 versions 的 published 不能只根据 head 当前指针代表历史发布：旧已发布版本仍需由实际发布记录证明，不能从测试状态猜测；若只表达当前发布必须明确命名，不能误导回退选择。
- 前端实现者已确认验收：真实 DTO 的创建/编辑/替密/测试/发布/绑定/回退/导入、内容版本与状态 revision/Worker generation 分开、权限与离线门禁、三视口布局/可访问性、无凭据/路径暴露。仅在协调后做组件局部构建，不提前运行独立全量、浏览器或真实 Runtime。

#### N2 验证准备调度限制

- 主代理尝试创建 Terra high 的 `/root/n2_verification`，限定为只写待执行报告/验证 helper，不启动应用测试；调度返回 `agent thread limit reached`。随后尝试恢复原 Terra `/root/n1_u1_verification`，同样被线程上限拒绝。
- 两次均未形成新的验证执行或文件，不把已发任务描述写成准备完成。N2 后端和独立前端继续当前工作；等待实现者正常结束释放槽后，再恢复 Terra 并进行准备/冻结后的独立批次。主代理不通过自行重复全量测试替代角色隔离。

#### N2 第三个实施检查点

- 实现者报告旧 fixture/immutable trigger 和缺 runtime identity 的 422 已消除，SQLite、CLI Worker 回归包通过；环境白名单去重、保留声明的 PATH/REAL_BIN/MGRAFTCP_BIN、危险键过滤及 wrapper 受控 blackip 文件接线已完成局部验证。argv 合同只证明接线，真实黑白名单网络效果仍属 E1。
- 导入已形成独立 Adapter 解析、完整 guard 下读取 claimed work、确定性不可变 secret 写入、第二次完整 guard 下 profile/head/import/work/Journal 原子提交骨架；仍待聚焦事务/重放/孤立秘密文件验证。Run snapshot 的 manifest/秘密/runtime/materialization 元数据已接入，相关局部包通过。
- 正式闭环仍缺 daemon/Panel 装配、安全版本历史 DTO、main.jsx 接线、异步 network work loop、部署 README/inherit 凭据约束及集中聚焦测试，尚未 freeze。
- 主代理要求慢 prober/Health 不占用唯一 heartbeat 或控制循环，必须在超时期间仍可保活/接控制；网络测试不覆盖当前 Backend 配置/健康。前端异步刷新由 main.jsx 单一持有，组件保留本地草稿，不另建重复轮询器。
- 主代理要求明确 legacy 分支的升级边界：历史观察与已有运行可保留，升级后新 Run 不得通过旧 ConfigFile 绕过固定内容、测试/发布和 runtime identity；缺可信元数据时导入重测或不可调度但控制在线。此要求需旧库/旧注册的新 Begin 反例证明，不能只测全新 workflow。

#### N2 表单并发边界纠偏

- 主代理在新组件接线检查中发现：editDirty 会保留旧草稿，但 submitEdit 使用 props 中最新 state_revision，后台刷新可让旧草稿绕过预期 CAS 冲突覆盖他人新内容。已交前端唯一实现者在本批中把编辑基线 revision 与草稿绑定，刷新不得自动重基；冲突后保留非秘密输入并要求明确重新确认基线或放弃。
- 替密输入随方案切换必须清空，避免旧方案凭据误写新方案；传输失败/5xx 不能显示“命令未提交”的确定判断，应显示无法确认结果并先刷新核对。非秘密同内容重试复用幂等键，秘密不为重试而缓存原文。
- Worker 已应用的文案必须标明实际绑定的 profile/version/generation，不能只根据另一个已绑定方案的 desired_status=applied，暗示当前选中但未绑定的新方案已生效。上述为实现中的边界纠偏，独立浏览器/并发验收尚未开始。

#### N2 配置页阶段冻结与模式闭环补齐

- 前端实现者只新增两份授权文件并报告阶段冻结，已补草稿固定基线、显式重新确认/放弃、失败后的保守提示、实际应用身份校验和全部 named profile 命令。局部 Oxc JSX transform、Vite CSS preprocess、SSR import/空态 render、diff-check 通过；没有完整 build、认证浏览器或模式切换的通过证据，N2 总批次仍未冻结。
- 主代理发现已绑定代理后缺少从指挥台切回 inherit/direct 的正式 Control 入口；N2 实现者确认当前仅完成 named profile，不能宣称 ADR-002 全模式闭环。此为原决策中每 Backend NetworkPolicy 的日常配置范围，本批必须补齐。
- 主代理采用独立 Backend 模式候选/测试/发布路径，不将 inherit/direct 伪造为 ProxyProfile。模式候选和不可变 policy 快照与当前 binding 分离；测试仅使用独立 Adapter，不改当前配置/健康/可用性。ready 后由明确发布命令核对测试、Worker generation、runtime identity 与 binding CAS，再创建 apply work，Worker ACK 才标 applied。只有发布改变 desired，活动 Run 始终固定原快照。
- 计划接口为 `/api/control/v1/network-bindings/mode/tests` 与 `/mode/publish` 或等价明确 DTO，最终契约由 N2 先锁定。AGY 未声明的 direct 不开放；其他 Adapter 只开放声明和实测支持的模式。模式与直连规则也需可追溯版本/manifest，不能只保存一个 mode 字符串。
- schema/domain/work/Panel 修改仍由 N2 唯一负责，模式接口稳定后恢复同一前端作者在本批追加接线。此期间不启动独立验收，避免对未完成目标提前给 GO。
- 新组件同时发现跨 profile 重绑缺 applied_profile_id。主代理要求集中保存实际 mode/profile/version/worker/generation/revision，与 desired 分开；pending 时保留最近成功事实，旧库仅在 applied revision 确实匹配当前 binding 时安全回填，其余保持未知，不能猜测来源或删除可确认事实。
- 前端作者正常结束后，主代理再次创建 `/root/n2_verification`（Terra high）成功。本次仅授权新待执行报告与正式 API helper 准备，不启动 daemon/Worker/浏览器/全量测试，模式 API 未锁定前不编造请求。早先线程上限失败保留为历史，本次未因此改变任何 Gate。

#### 范围内缺陷与后续台账

| 编号 | 级别 | 问题与处置 | 归属 |
|---|---|---|---|
| N2-MODE-01 | P1 | 指挥台缺 inherit/direct 切换，阻断 ADR-002 明确模式闭环；本批集中补候选、测试、发布、回执与旧 Run 不变量 | N2 当前批次 |
| N2-UX-01 | P2 | 原方案不能直接移除凭据；当前可新建无认证方案并测试/发布/重绑完成业务，不阻断既定矩阵。后续可增加 owner 专用显式 clear 命令生成新内容版本，旧秘密保留给旧 Run | 后续凭据管理易用性任务 |

- 主代理曾询问移除凭据的最小方案，随后按任务范围规则明确为 P2，仅记录、不授权为此扩大当前源码。不得将该项与明确要求的模式切换混为同一阻断。

#### N2 验证准备回报与工具纠偏

- Terra 已新增 `2026-09-05-openagentx-n2-validation.md` 和 `verify-n2.mjs`，报告状态 pending_freeze；仅 Node 语法/diff 检查通过。没有启动应用、daemon、Worker、浏览器、模型或 Go/Web 构建，准备成果不等价于测试通过。
- 主代理发现 helper 在状态检查前强制解析 JSON，现有未认证响应为 text/plain，会把预期 401/403 误记失败；已交同一验证者集中修正。另要求私有文件使用 lstat 拒绝链接、准确 0600/父目录 0700、验证 base URL 限隔离 loopback，避免误用环境连接生产或外部服务。
- 待测矩阵不假定幂等键有过期机制，采用实际契约的同键异请求冲突。新模式 API 尚未锁定，脚本只准备确定的 named profile 路径，等待最终契约后补齐；正式独立批次仍未授权。
- Terra 已完成上述 helper 纠偏，Node 语法与新文件 diff 检查通过并正常结束；没有执行 fixture bootstrap 或任何应用/浏览器/模型。后续等待完整实现冻结，再以最终构建开始独立验证。

#### N2 模式接口冻结与前端补接

- N2 已冻结并落盘 `/api/control/v1/network-bindings/mode/tests`：请求含 agent_id/backend_id、inherit 或 direct、目标 worker_instance_id/generation 和当前 binding revision（无绑定为 0）；receipt/Observe mode_tests 保存 test_id、policy_version、manifest_digest、binding_revision 和目标身份。
- `/api/control/v1/network-bindings/mode/publish` 使用 test_id、相同 Worker/generation 与测试时 binding_revision，显式发布后才进入 pending。Observe 新增 mode_tests；binding 增 mode/policy_version/test_id/manifest_digest，以及 applied_mode/applied_profile_id/applied_profile_version/applied_policy_version。main.jsx 的异步刷新需涵盖 mode_tests pending/claimed。
- 主代理已恢复同一前端作者，只在原两文件补接；作者确认目标列表包含所有声明 inherit/direct/named_profile 的 Backend，故障健康不移除修复入口。模式测试 succeeded 且目标/mode/Worker/generation/revision 匹配后才允许用户显式发布，测试不自动发布，未声明能力不开放。
- 此时 N2 仍在改 schema/repository/Worker，未到完整可测状态；“命名方案局部闭环全绿”为实现者回报，不等价于独立认证浏览器或总 Gate。前端补接后仍需全批冻结。
- 主代理尝试恢复 Terra 只补 mode helper 时再次遇到 thread limit，未启动该任务、未增加应用验证。待前端正常结束释放槽后再调度；现有 helper 只覆盖已准备的 named 路径，mode 验证不被提前记为已准备或通过。
- 主代理在补接检查发现跨方案回退对象不一致：版本表来自 selectedProfile，但回退 API 从当前 binding 推导 profile，仅提交 target_content_version；若目标绑定 A、界面选 B，则 B 的版本按钮可能回退 A 的同号版本。已交同一前端作者在按钮和 handler 两层校验 version.profile_id、selectedProfile.profile_id、selectedBinding.profile_id 相等且目标/mode 匹配，继续使用 binding CAS。此反例必须进入后续浏览器验收，尚未有独立通过证据。

#### N2 配置组件最终交接与验证准备恢复

- 主代理恢复上下文后核对 HEAD 为 `0839341`，N2 工作树仍未提交，完整 ADR-002/003 goal 保持 active；冻结 ADR、既有未跟踪文件和源码所有权边界不变。
- 前端作者已正常结束，明确冻结 `NetworkSettings.jsx` 与 `network-settings.css`：包含 named profile、inherit/direct 测试与显式发布、模式历史诊断、严格目标身份/CAS，以及跨方案回退反例修复；实际应用统一读取 `applied_*`。局部 Oxc JSX、CSS 预处理、SSR 空态及 diff 检查通过，没有完整 build 或浏览器通过证据。
- 已通知 N2 唯一后端及 main.jsx 实现者完成 `mode_tests` 初始化、pending/claimed 刷新和离线/卸载停止，并继续收口异步 work loop、legacy 新 Run 边界、部署说明与局部测试；尚未收到全批 freeze。
- 前端正常结束后恢复 `/root/n2_verification` 成功，仅授权扩充 mode-workflow helper 和待执行矩阵，不启动应用/daemon/Worker/Go/Web/浏览器/模型。模式测试不得改变 binding 或健康，显式发布才 pending，实际 ACK 必须匹配目标身份；failed/stale 立即结束等待。准备完成后正常结束，正式验收另待完整冻结。

#### N2 物化摘要与测试证据边界复核

- 主代理只读发现 `Materialize` 用包含代理秘密的配置文件与 blackip 内容计算裸 SHA-256，并经去路径后的 ACK、注册和 Run 快照持久保存；`ImportConfig.SourceIdentity` 同样直接哈希原始含秘密文件。已知配置模板和端点时，这些值可被用于离线验证密码猜测，违反本批已锁定的秘密摘要约束，列为当前 P1。
- 已交唯一 N2 实现者在同批集中改为受控 Worker 密钥认证摘要或等价不可猜测方案，保留旧 Run 文件篡改校验；导入来源可使用不含秘密的规范化内容与 opaque 秘密身份。不能仅删除摘要而放弃固定内容核验。尚未收到修复或独立测试证据。
- 当前 network work 已有独立循环，但 heartbeatLoop 仍同步调用 BackendPool.Observe/Adapter.Health；慢当前 Backend 检查可能阻塞保活。已要求实现者补独立健康检查与慢 Health 的生命周期用例，不能只用慢候选测试证明所有健康检查均不阻塞。
- 当前候选测试仅 clone Adapter 后调用 Health，AGY 的 Health 实际为正式 wrapper `--version`。此证据只能支持本地启动检查，不能证明代理端点或认证可用；坏端点不能据此 ready。已要求最小有界分层诊断，分别记录配置、秘密、端点、直连规则和 Runtime 本地健康；未执行的直连网络效果与模型调用保持未核验。最终 DTO 稳定后再由原 UI 作者补接，不以“分层测试”标题替代实际证据。

#### N2 分层诊断契约与运行身份收口

- N2 与主代理锁定 `probe_results=[{layer,state,diagnostic_code,duration_ms}]`，持久化在两类 test 并由安全 Observe 返回。layer 只允许 configuration、secret、endpoint、direct_rules、runtime_health、network_effect、model_call；state 只允许 passed、failed、not_applicable、not_verified。适用的配置/秘密/端点/规则/本地健康层失败不得整体 succeeded，网络效果和模型调用本批不推断通过。
- 端点计划采用有界 SOCKS5 协商及可选 RFC1929 认证、HTTP 结构化协议响应；HTTP 响应不代表 CONNECT 或模型出网成功。direct_rules 只证明语法、物化和 Adapter 装配，不声称实际绕流。inherit 存在有效代理或 wrapper 默认端点时不得无条件写 endpoint/secret 不适用；无法确定的内容要显式未核验。
- 主代理只读发现 `VerifyRuntimeIdentity` 尚无调用，PrepareRunNetwork 仅比较启动时缓存 Descriptor；AGY 的 executable 与 wrapper 摘要当前指向同一 wrapper，遗漏真实 REAL_BIN。已交 N2 在正式 probe/apply/StartTurn 前核对部署声明的实际绝对 wrapper/native/REAL_BIN，限制 helper 版本输出；本条为待修 P1，不是通过证据。
- 原 UI 作者已恢复，仅在两文件追加分层事实展示；未知诊断使用固定降级文案，不裸显示任意服务端字符串。当前“活动任务配置”恒为未知，已要求两作者对接最小安全活动 Run 快照及期望版本对照，无活动执行时明确为空，不由 UI 猜测。API/main 仍归 N2，U2 深度运行观察仍后置。
- inherit 白名单不能静默丢弃现有 AGY_GRAFT 配置后切回 wrapper 默认端点；已要求受控兼容或固定不可调度/导入诊断。这与已锁定的 legacy 新 Run 边界一起集中核对。
- Terra 已完成 mode helper 并正常结束：准备断言 test 前后 binding/健康不变、显式 publish 才 pending、真实 ACK 身份与 revision 匹配，failed/stale 立即停止。只有 Node 语法/diff 检查，无应用或浏览器执行，N2 仍待完整冻结。
- 主代理尝试在等待期恢复 Terra，只准备 E1 隔离 native 黑白名单实验脚本及待执行报告，调度返回 `agent thread limit reached`；该准备任务没有开始，没有新增 E1 文件或执行网络实验。保留现有 E1 矩阵，待实现者正常结束后再调度，不重复轮询线程上限。

#### N2 第四个实施检查点

- 实现者报告 HMAC 物化与导入身份已经完成，重启稳定、文件篡改和裸 SHA oracle 反例的局部测试通过；7 层/4 状态 DTO、唯一性/白名单/整体状态一致性校验，以及两类 test 的 SQLite JSON 已落盘。
- SOCKS5 协商、RFC1929 认证及有界 HTTP OPTIONS 协议探测已实现，坏端点先于 Runtime Health 失败，相关聚焦测试通过。这些均为实现者局部证据，尚未独立审核、正式 API/浏览器或真实 E1。
- active_runs 的安全字段及 `onOpenTask(taskID)` 纯导航回调已交原 UI 作者；后端 active_runs/main/README/Panel 测试仍待收口。其余剩余项为旧 workflow fixture 的分层 ACK、实际 wrapper/native/REAL_BIN 逐次复核、health 与 heartbeat 解耦、inherit 默认端点处理及 legacy Begin 反例；实现者报告明确阻断为无，尚未 freeze。
- 主代理补充既有生命周期竞态边界：仅配置工作因同 generation 内新 revision 变 stale，不应终止仍有效的 Worker 控制连接；Session/lease/generation/fencing 失效仍按安全规则停止。要求分别提供确定性交错证据，不能把任意 ACK 错误一律当 Worker 身份失效。

#### N2 配置组件分层与活动 Run 最终冻结

- 原 UI 作者确认两文件再次最终冻结：已接入 `probe_results` 白名单和固定诊断，旧记录显示无分层记录，未知 layer/state/diagnostic 不回显原文；整体测试 succeeded 只表示流程完成，不外推真实网络效果或模型调用已经通过。
- `active_runs` 最终字段为 run_id/task_id/agent_id/status/backend_id/worker_instance_id/worker_generation/network_mode/network_profile_id/network_profile_version/network_policy_version/network_binding_revision。组件按 Agent/Backend 显示 Run 固定快照与当前 desired 的同版/旧版/未记录对照；缺字段不推算，Task 跳转只调用 `onOpenTask(taskID)`。作者报告 main 已由 N2 接线，后端完整性仍待 N2 收口及独立验证。
- 局部 Oxc JSX、Vite CSS、SSR import/空态、两文件 diff-check 通过；没有全量 build、真实浏览器或 Runtime 通过证据。主代理要求作者正常结束，N2 全批未冻结前不启动独立验收。
- UI 作者随后正常结束。主代理再次恢复 Terra 成功，仅授权 E1 native 规则实验脚本与待执行报告的准备及 Node 语法/diff 自检；没有启动端口、网络、CLI、应用或浏览器。这是先前线程上限后的新成功调度，不覆盖原失败记录，N2 仍由原唯一实现者收口。
- 主代理在分层接线复核发现 domain 已允许 named_profile 携带直连目标，但 Environment 仍只允许 direct 模式，导致物化成功后正式 AGY Health/StartTurn 拒绝非空 blackip 规则。已交 N2 修正，并补命名方案/IPv4/展开 IPv6 到 Environment 与 wrapper argv 的联动用例；真实绕流仍由 E1 证明。
- 同次只读复核指出导入端口使用 `fmt.Sscanf` 会接受带尾部垃圾的数字，重复配置键会静默覆盖。已要求严格端口解析及重复键拒绝，保留最小已支持 native 配置子集；这属于既定 fail-closed 导入边界，尚无修复通过证据。

#### E1 隔离 native 验证脚本准备完成

- Terra 新增 `verify-e1-network.mjs` 与 `2026-09-05-openagentx-e1-validation.md` 并正常结束；仅 Node 语法及新文件 diff-check 通过，没有监听端口、发起网络、运行 CLI/应用/Worker/浏览器或模型。
- 脚本须显式执行开关及最终 wrapper/helper/client 绝对路径，准备使用 127.0.0.2 marker 目标和 127.0.0.1 固定目标 allowlist 的 CONNECT/SOCKS5 代理，覆盖五项无 blackip、目标 blackip、黑白重叠及两种不重叠反例。按连接计数和相同目标结果判定，失败不重试，退出清理子进程/连接/监听。
- 该准备仅用于 wrapper/native 规则效果，不是 AGY 模型 E2E；真实 Task marker、工作区副作用独立核对、Worker 常驻及连续接单仍按 E1 顺序后置。主代理已向 N2 请求对完成的秘密存储/物化/prober 子集给出稳定边界，确认后才安排独立静态预审，总批次仍不提前 GO。
- N2 随后明确冻结 `internal/network/secretstore/**`、materializer.go/test 和 prober.go/test；报告严格 `strconv.Atoi`、重复键拒绝、HMAC 来源归一化及该子集聚焦测试通过。environment 的 named+direct_ips 已补 IPv4/展开 IPv6 局部链路测试，但仍可能被 identity/inherit 收口修改，故不纳入此次稳定子集。
- 主代理已恢复独立 Sol medium 只读预审，仅可新写 `2026-09-05-openagentx-n2-independent-review.md` 并记录子集源指纹；不运行全量、应用、网络、浏览器或模型。其余 domain/Runner/API/identity/environment/main 不提前给结论；后续全批审查只对新增范围和发生变化的子集补审，避免重复消耗。当前没有新的 N2 Gate 结论。

#### N2 独立子集预审 NO-GO 与集中修复

- 独立 Sol medium 返回五项 P1，无 P0：HTTP 物化把 `http://host:port` 写入 native 要求的 `host:port` 字段；秘密/物化文件 Lstat 后按路径 ReadFile 有替换竞态；HMAC 最终文件先创建后写入可暴露空/部分 key；metadata/blackip 失败后的孤立敏感文件尚无完整并发安全处置；Dial 后 stalled read 不响应父 context 即时取消。
- 主代理接受其作为本批集中失败证据，要求 fd-based O_NOFOLLOW/fstat/有界读取、完整 key 原子发布及坏 key 拒绝、引用感知孤立文件清理、HTTP 正式契约和 accept 后 cancel/SOCKS 认证帧断言。0700/0600 不被解释为同 uid 恶意 Runtime 的完整 OS 隔离，不因此扩张新增 sandbox。
- 孤立文件结论限定当前源码未见满足既定清理/重试策略，相关 metadata service 仍未冻结，不冒充已完成全服务审查。HTTP 407 只能证明收到协议响应，不能声称认证或出网可用，必须使用明确诊断/未核验语义。
- N2 已确认暂缓剩余 API/Binding 收口，转入同一批次集中修复；拟采用目录级锁串行、引用感知清理和原子发布。修复子集暂时回开，局部验证后重新冻结；仍不提交、不启动独立全量或浏览器，总目标保持 active。

#### E1 准备脚本只读纠偏待办

- 主代理只读发现当前 E1 helper 只终止 wrapper/native 的直接 PID，未证明清理 client 子孙；目标 HTTP socket 未跟踪，finally 的 server.close 可能无界等待；CONNECT 握手完成后未切换 phase，后续数据可能重建上游。已向 Terra 发送下次恢复时的集中修复清单，本次没有启动新的执行任务或网络。
- 下次修复要求独立进程组、有界终止和等待、目标连接跟踪/有界关闭、握手缓冲限量及转 relay 后处理剩余数据。受控 client 需固定契约和禁用用户配置/外部代理，不能隐式读取 curlrc。
- 当前五案例均走 SOCKS，需在最终 E1 增 HTTP 实际路径以确认本次 native 配置格式修复；该项仍为待准备/未执行，不能用 SOCKS 结果外推 HTTP 能力。

#### N2 子集修复回报与幂等服务边界

- N2 回报五项已修复并重新冻结：HTTP host:port、fd-based 有界读取、两个 key 原子 no-replace 发布、blackip-first/config-last 避免后置失败留下敏感 config、每秘密版本 flock 包住写文件/metadata/引用核对/失败清理、accept 后取消及 SOCKS 精确帧/HTTP407 固定诊断。局部并发、SQLite fault retry 与 prober 测试通过，独立结论仍待复审。
- N2 的受影响包回归发现 legacy 零 inherit fixture 被 trustedNetworkPolicy 拒绝，controlplane/sqlite/cli 有开发测试失败；实现者选择补完整受信 fixture，不放宽生产校验。这不是已通过回归的证据，原失败保留，完整 N2 尚未 freeze。
- 主代理对未冻结服务只读发现 EditDraft/ReplaceSecret/StartTest/Publish 等先检查当前 head 的 expected revision，再进入 execute 查幂等回执；首次成功推进 revision 后，原请求原键重试会提前 409，违反返回原回执的约定。已要求当前授权及结构验证后先核对 actor/operation/目标和完整 body 摘要限定的既有回执，不存在再做可变状态/CAS；事务内仍保留最终并发检查，秘密仍用 HMAC，不缓存原文。
- 新幂等问题归属尚未冻结服务，不改写原六文件子集审核历史。主代理已恢复同一独立 Sol medium 对五项做定向只读复审，记录新指纹，仅必要读取 CommitImmutable/引用检查调用背景，不给其余模块总 GO。
- Terra 已集中修复 E1 helper 并正常结束：独立进程组有界 TERM/KILL/等待、target/proxy 连接跟踪、有界监听关闭、4 KiB/3 秒握手及 relay 剩余数据；curl 固定 argv 禁用用户配置/外部代理并设超时；新增 HTTP host:port 案例。仅 Node 语法/diff 通过，没有网络/CLI/应用执行。
- 已给 Terra 留下 N2 后续补验清单：named failed/stale 立即停止、完整 applied 身份、probe_results、全命令幂等、active_runs；mode 发布的 pending 提交事实从正式 receipt 验证，随后 Observe 可已 applied，不强求竞态瞬态。不把 loopback URL 本身当作已证明隔离进程。

#### N2 子集第二轮结果与清理设计判定

- 独立定向静态复审清除 S01 HTTP 格式、S03 原子 key、S05 probe 取消；六文件指纹为 `388ea84385f178dd3446e0561db5ba4b04f3bbe7b0a683d50422303ecc6262a5`。S02/S04 仍 NO-GO，未执行测试/网络/浏览器，完整 N2 仍未审核。
- S02 剩余为 trim 后空 root 未被明确拒绝，以及根目录未用 directory fd 锚定。主代理要求内部叶文件通过已验证目录 fd 的 openat/renameat 等操作；不扩张 OS sandbox 或任意外部路径权限系统。
- S04 中 per-version 锁和引用检查已解决协作入口并发误删，blackip-first/config-last 也已消除该后置敏感 config 孤立路径；仍须处理崩溃/引用查询失败后的恢复。主代理将“孤立不可读”明确限定为正式授权 API 不得读取未提交版本，不能将 0600 对同 uid owner 的可读性误称对外泄露。
- 主代理提出最小可重建清理策略：在 per-version 锁下扫描私有目录并核对已提交 DB 引用，有任一历史/活动引用则保留，无引用可清理，查询失败保留并持久可重试；不强制额外大型两阶段提交。已请独立审核者判断正式入口现有引用保护及该策略是否充分，原报告保留，澄清另行追加，尚未把 S04 标为清除。
- N2 同时报告十类命令 pre-CAS 回执处理已修复，profile path 纳入摘要，替密使用 HMAC，事务内查重仍保留；十类成功原键重放及同键异请求冲突优先于 stale 的聚焦测试通过。此为未冻结服务的局部证据，独立总审与正式 API 验证仍待后续。
- 独立审核随后确认正式路径仅为认证 Worker pull -> Guarded ClaimNetworkWork -> 已提交 work.SecretVersion -> secrets.Get，不能由请求提交任意 version，所以未提交孤立文件无正式读取路径。其确认目录枚举+完整 DB 引用+同 per-version 锁+失败保留重核足够，额外持久 orphan 标记没有独立必要性；S04 仅因扫描/重试未实现继续阻塞。
- 设计澄清期间六文件指纹已变为 `51d099ca1a18123748e810c503623c583dae11021d44a0a3b12b17983d18a9f1`，属于 N2 获准修复 S02/S04 的修改；本次澄清不是对新指纹的代码复审，前轮指纹与结论保留。主代理将已确认最小策略交回 N2，待下次明确 freeze 再复审。
- 审核者正常结束后，主代理恢复 Terra，仅修改 N2 helper 和待执行报告，补分层结果、named failed/stale、完整 ACK 身份、十类幂等、active_runs，以及发布 receipt 与后续实际状态的正确断言。仅准 Node 语法/diff 检查，没有启动 fixture、应用、网络、Worker 或浏览器。

#### N2 恢复扫描进度与验证准备条件纠偏

- N2 回报 directory fd 锚定与空白 root 拒绝已实现，内部 key/config/blackip/secret 使用 openat/renameat/unlinkat；初始化后替换根路径仍作用于原目录的局部测试通过。孤立扫描、查询失败保留并下次清理、引用保留和未提交版本读取拒绝已有局部证据，仍需 SQLite 历史/活动引用和 Runner 生命周期核对，尚未再次完整 freeze。
- 主代理要求 GC/配置工作错误不能导致有效 Worker 的 networkWorkLoop 直接退出；目标秘密引用不能确认时该工作 fail closed，清理失败保留并重试。控制身份/租约/generation/fencing 错误仍按原安全规则停止。
- 主代理另要求审清 inherit bootstrap：不能仅通过 YAML 手填正版本、非空 manifest/identity 标记就绕过控制面事实；合法初始 inherit 需明确来源与快照语义，不能冒充已测试发布。隔离单元 fixture 与正式 CLI/API 前置证据分开，后者不得用任意摘要修绿。该边界尚待 N2 明确实现方案。
- Terra 本次准备误把“未核验不能标作该层通过”写成 network_effect/model_call 必须 passed 才能通过 N2。主代理在任何动态执行前发现并交原作者纠正；该错误不归应用缺陷，不要求连接测试新增模型调用。
- Terra 已修正并正常结束：前五层按模式检查；inherit 的 secret/endpoint 按当前 DTO 显示 not_verified/INHERITED_CONFIGURATION_UNVERIFIED；后两层强制 not_verified/NOT_VERIFIED，整体配置 test succeeded 可与此并存。报告保留准备错误历史，只有 Node 语法/diff 通过。
- named SOCKS5 fixture 现约定公开合成用户名 `n2-fixture-socks-user` 与私有文件密码，并需核对精确认证帧。十类幂等仍只是完整矩阵，当前可执行 helper 仅实现 create 同键重放；其余九类 helper 尚待补，不能记作准备完成或已验收。

#### 用户收口指令与立即调度

- 用户明确表示“已经做的，暂时不会退，快速收口”，要求先交付可用功能，其余发现后续计划。主代理已立即通知 N2 实现、独立验证及审核：保留现有改动，停止新增全面加固、极端矩阵和通用 bootstrap/恢复框架；只按本文件顶部四类直接风险阻断。
- N2 已接受，最短剩余为：真实 CLI 初次 inherit 的 test/probe/publish/apply/Run 链路（mode test 当前未到终态，正在定位）；现有 main/API/Worker 编译及 Go 回归失败收口；已有分层/active_run 必要断言；main 缩进与 Web build；diff-check 后明确全批 freeze。已做 S02/S04 保留，不继续扩展。
- 未完成十类独立 helper 不再作为独立准备任务阻塞；在正式聚焦批次检查关键重试和状态不变量，其他扩展矩阵如实列未覆盖。尚未通过的功能与证据仍不能标绿，goal 保持 active。

#### N2 最终冻结与聚焦验收启动

- 实现者确认全批 freeze 并正常结束。首次 inherit mode test 停滞定位为内置 fake Runtime 缺少 NetworkProbeCloner/NetworkPolicyApplier，补标准接口后，三条 CLI 真实进程用例完成正式 test、七层 probe ACK、publish、apply ACK 及普通/多轮 Run。此证据使用 fake Runtime，不冒充外部模型或业务副作用验收。
- 实现者报告 Go 回归、未绑定 metadata 不可调度反例、S02/S04 定向测试、Web build（266 modules）及 diff-check 通过。主代理尚未将局部结果等同独立验收，N2 源码保持冻结。
- 已恢复 Terra 执行一次聚焦独立功能验证，授权隔离正式 API、真实 Worker 和 Chrome 配置页检查；不补其余九类幂等 helper、不扩张网络实验。验证报告需记录最终源码/构建及证据边界。
- 已恢复独立 Sol medium 对最终冻结批次做短功能风险审核，按顶部四类直接风险判定；已有 fd、原子 key、扫描和取消修复全部保留。剩余事项进入后续计划，审核不再自动沿用旧 P1 标签扩大范围。
- 独立源码短审核已返回 **GO**，S01-S05 在本轮功能边界内清除；最终功能验收仍等 Terra 动态证据。六文件聚合指纹为 `eb72cd80572b2a5f23b644a5542c53741b96584bb6fcf6f15068d944856f254e`，功能审阅集合指纹为 `326c4cf85febb50b6d1c568a4a19a557363bf96cec394e79bb192bffcc5298fa`，具体边界见独立报告。审核者已正常结束，证据到齐后只做一次短核。
- Terra 报告冻结二进制 SHA-256 为 `552e5891074356b196a90e377fa4af2379166b464c0130b8c90132ec9a6ff765`，关键 Go 包与 Web build 通过。首套尚未启动的隔离 fixture 在终端回显关闭前暴露了临时凭据，已废弃，不能用作证据；重建必须使用新凭据并预先关闭 TTY ECHO，禁止复用或抄录秘密。此为验证脚本事故，不改产品认证实现。
- 创建 `/root/u2_functional_closeout`（Sol high）仅做收窄后的只读准备，不编辑源码、不跑测试。主代理要求优先按 Run 固定 instance 精确读取历史 Worker generation、复用现有投影和 Markdown；不得为准备任务扩张 schema 或新增可自报为已核验的通用接口。N2 提交前不启动 U2 实施。

#### U2 本轮最小验收矩阵（实施前冻结）

- 只读准备已确认历史 Worker instance 与 generation 可精确关联，不新增 schema。保留 N2 两个配置组件；源码范围限于 Runtime 结果/事件必要修正、Finish 映射、Observe/Panel 安全投影及 main/styles 的展示补齐，相关局部测试随修正更新。
- 不新增通用业务核验入口或可由 Runtime 自报为已核验的字段。缺少可信副作用事实时 Task 保持 `uncertain`，Run 的 Runtime 状态和结果正文仍保留；外部 E1 的独立核验只写入验收报告，不伪造成产品内已核验事实。
- 复杂产物、审批描述新增持久化、ACP/CodeBuddy 的全面协议整治及通用核验框架后置；本轮仅修直接误报或泄露路径。U2 尚未获源码实施授权，先完成 N2 提交。

| 类别 | 本轮断言 | 证据 |
|---|---|---|
| 正向流程 | Run 显示固定 Worker/generation、网络版本、运行阶段、Runtime 结果与错误及核验来源 | 安全 Observe DTO 与浏览器 DOM |
| 状态不变量 | 当前 Worker/绑定变化不改写历史 Run；Runtime 自报不提升为业务已核验 | 历史身份对照与投影测试 |
| CAS/幂等 | 保留 Begin/Finish 的身份、版本和同结果重复提交规则 | 受影响服务/事务测试 |
| 失败路径 | 历史身份缺失显示未知；未知副作用不报成功；Markdown 错误回退纯文，复制失败有反馈 | 结果/投影用例及浏览器 |
| 竞态 | 未知结果与取消交错不虚报已取消；保留 Runtime 结果及取消意图 | 定向事务用例 |
| 安全 | 新 Runtime Journal 不持久化 raw map、provider session、隐藏字段或完整 TurnResult | 写入前白名单及 Journal payload 断言 |
| 前端 | Task/Message/现有 Approval 文本/result 共用 Markdown，手机表格和代码局部滚动，选择及重连保持 | 三视口、原文/复制、SSE 与离线写保护 |

#### N2 本轮功能验收收口

- Terra 完成 named profile、inherit、direct 的正式 Control API -> 真实 Worker work/ACK -> Observe applied；named SOCKS5 精确认证成功 1、失败 0，七层诊断及完整 applied 身份匹配。Chrome 实际登录、凭据提交清空、配置观察、active_runs 空态及三视口已执行。离线仅验证控件禁用，不冒充写请求计数或 SSE 断线证据。
- 执行报告为 [N2 功能验收报告](2026-09-05-openagentx-n2-validation-executed.md)，准备稿已链接该报告并把扩展 helper 后置；三张安全截图及 manifest 已归档到 `evidence/n2/`。报告保留旧 fixture 回显事故，结论只绑定新凭据的 r2 环境。
- 当前应用树指纹为 `bd8515ec3583f387a29e52f26d5fb0d1c0dc42f19f2c35bafa906c3b7a793657`，Web 构建资源指纹为 `4d7207cccff7c289640a2eaa6dbceca79ea982eb55d3fffe9048a7b829db2311`；二进制及 schema/Worker 身份见执行报告。三份冻结 ADR 的 SHA-256 仍与初始值一致。
- 独立 Sol medium 已短核实际证据、截图 SHA/尺寸和边界，结论为 **N2 本轮功能 GO；完整 ADR-002 未验收**。主代理采纳，准备提交本批；九类扩展幂等、回退/导入和极端故障等未覆盖项不阻断本轮，不能被写作已通过。
- 隔离 r2 daemon/Worker/SOCKS5 保留供 U2/E1 复用；最终实际闭环还须补错误配置失败、关键重试不重复、断线恢复及结果独立核对。U2 按上表最小矩阵继续，不回退既有加固、不修改冻结 ADR。

#### N2 提交与 U2 最小实施启动

- 主代理完成暂存归属、diff-check 与冻结 ADR 校验，提交 `78ab470 feat: complete console network configuration workflow`。本批共 66 文件；既有 `.planning/`、三份 2026-08-31 未跟踪报告及尚待执行的 E1 准备文件均未纳入该提交。
- 已恢复 U2 唯一实现者，授权最小矩阵源码与必要局部测试；不修改 N2 两个配置组件、冻结 ADR 或独立报告。AGY/ACP/CodeBuddy 未证实的 SideEffectsKnown 默认 true 属于同一直接误报修正，全面协议整治仍后置；fake 确定性 fixture 语义保留。
- Terra 仅获 U2/E1 短准备授权，复用 r2 与既有 helper，允许只读核对 Runtime 契约与受控私有输入。不得在 U2 未冻结时启动新的模型、网络、浏览器验证或重启服务；最终聚焦批次同时取得观察/Markdown 和实际闭环的必要证据，避免重复浏览器批次。

#### E1 最小输入与证据冻结

- Terra 已完成短准备并正常结束，新增 [U2/E1 收口准备](2026-09-05-openagentx-u2-e1-final-closure-preparation.md)，没有执行新的浏览器/模型/网络测试。native helper 本轮显式选择 `no_blackip` 与 `http_config_host_port`，其余已准备案例保留后置。
- Task A 必须由真实指挥台明确选择 AGY Backend 创建，仅在 r2 的隔离 workspace 生成唯一 `e1-marker-<公开随机 nonce>.txt`，内容严格为 `oax-e1-<nonce>` 加一个换行；要求调用工具读回，在 Markdown 结果返回文件名与 SHA-256。Task B 通过正式入口让同一常驻 Worker 只读该文件并返回内容与 SHA-256，不修改文件。
- 验证程序预先固定期望字节并独立读取文件核对 A/B 的内容、哈希、Task/Run/事件与 Worker 身份。Runtime 的自报不构成核验；Task 可为 `uncertain`，外部核对以 `external_e1` 记录在报告。清理仅限已确认唯一且身份/内容仍匹配的该测试文件；不确定时保留私有 fixture。
- 断线输入使用同一最终启动配置内的 fake Backend（正式支持 `result_status=waiting_input` 与测试 provider session），经正式 API 建立一个 Markdown 观察 Task。Chrome 保持选中并离线，独立认证 API 写入一条唯一消息；恢复后选择不变、缺口消息恰一次。浏览器离线尝试写入及恢复后的 Control POST 计数均应为 0，独立 API 那次单独计数。
- 最终实测基线为 `78ab470` 加最终应用 diff/源码指纹，U2/E1 GO 后提交并关联同一源码，避免把尚未提交的 commit 当测试前置条件。主代理只读确认本机 `:7897` 有监听，这仅是实际 AGY 代理候选的可用前置事实；N2 的 `:18081` 协议 fixture 不作为模型出网代理。实际出口仍须通过指挥台正式配置、测试、发布及 Worker applied 核对。

#### U2 最终冻结与独立验收启动

- U2 实现者完成并正常结束：RuntimeSideEffectsKnown 只表示 Runtime 自报，权威 SideEffectsKnown 不再由外部 Adapter 默认置 true；成功但未知的 Run 状态/正文保留，Task 为 `uncertain` 并记录 `business_effect_unverified`。新 Runtime 与 Finish Journal 采用固定事件白名单和最小元数据。
- Observe 补固定 Worker 历史 generation、网络 policy/binding 版本、TurnResult 安全正文/错误与来源；business_verification_source 为 `not_recorded`，无通用核验入口、无 schema 变化。Markdown 已补原文/复制反馈、错误纯文回退、已有入口统一与手机局部滚动；N2 配置组件保持冻结。
- 未知结果不会悄悄继续消费已有后续消息：实现者局部测试确认 Task uncertain、不可 BeginAttempt；原 Message 可查询，控制消息最终 superseded，不转 work lane，不自动重试。这是本轮保守结果边界，后续核验/继续执行能力仍归后续计划。
- 实现者报告受影响 Go 六包、Web build、observation 4/4、PWA 和 diff-check 通过。AGY 在当前 shell 含凭据代理环境下按设计拒绝 inherit；测试清除该环境后通过，此环境前提保留，不把不匹配部署输入写作产品已通过证据。
- 已恢复 Terra 执行一次最终 U2/E1 聚焦批次，授权最终构建、r2 隔离服务重启、真实指挥台与两项真实 AGY Task、selected native 网络案例及最小错误/重试/断线补验。已恢复 Sol medium 做短源码功能审核，独立报告另建，不重跑长测试或扩大范围。当前只有局部通过，最终功能 Gate 尚未判定。
- 独立 Sol medium 已完成 [U2 源码审核](2026-09-05-openagentx-u2-independent-review.md)，四类直接风险未发现阻断，**U2 冻结源码 GO**；10 个核心文件聚合指纹为 `e4bd16909935a0f77d598d4991d3de09c3e79bc953e9f43b2b4b4471f5ca31df`。其未运行测试/Runtime/浏览器，已正常结束；最终功能结论继续等待 Terra，证据到齐后仅做一次短核。

#### E1 冻结构建进展与夹具路由纠正

- Terra 报告受影响六包与 Web build 通过，r2 已换为最终构建并注册 generation 2。Chrome 已完成实际 HTTP `127.0.0.1:7897` named profile 创建、测试、发布、绑定，页面显示当前 generation 的 applied 与健康事实；尚未创建 Task A，没有真实任务通过结论。
- 验证者随后发现同 Agent 的 `fixture` 和 `named` 均可用，现有 M1 按 Backend ID 自动选择首个，UI 创建 Task 无显式 Backend 选择，故原夹具会执行 fake。主代理将其判为验收输入与现有调度契约不匹配，不因验证者初始 P1 标签扩张本轮产品功能。
- 原“明确选择 AGY Backend”验收措辞纠正为“在 UI 选择只注册 AGY 的目标 Agent”。授权仅调整私有 fixture：真实任务 Agent 只保留 AGY Backend，经正式 Worker 生命周期与配置测试/发布重建 applied；观察用独立 fake Agent/Worker 经正式 apply/register 建立。保留原前置失败，最终二进制、Web、通过的 Go 结果与 Chrome 批次复用，不回开源码。
- 多 Backend 的显式执行选择属于后续易用性能力，本轮不宣称具备；不会通过忽略 Execution 字段或手改数据库冒充路由成功。

#### r2 退役与终端输入工具修正

- 验证者在正式 `agent apply` 的 PTY 自动输入中再次发生临时 owner 口令回显。r2 口令、会话与环境立即视为不再可信，禁止继续用于最终 U2/E1；没有创建 Task A，不能宣称真实 AGY 闭环通过。此事故及首轮失败均保留，不记录任何口令值。
- 现有隔离验证授权已覆盖撤销和新建可逆 fixture，主代理未再次请求用户确认。Terra 已按精确归属终止 r2 Worker、daemon、SOCKS5、两个专用 Chrome、未完成的 agent-apply 和 wrapper 子进程；确认相关进程不存在，18180/18081/18240/18241 无监听。r2 永久退役。此前已提交 N2 的历史证据与 U2 源码/构建证据保留，不冒充 r3 最终执行证据。
- 主代理只读确认本机 `script` 为 util-linux 2.39.3，help 明确提供 `--echo never`。仅靠延迟输入或等待提示不足以作为保护，禁止继续 `/dev/tty` 临时注入。
- 已恢复 Sol 实现者仅编写小型 `run-private-admin.mjs`：PTY 创建时禁止回显，父进程有界捕获子进程输出，外部只见固定分类/退出码/回显布尔；秘密仅从私有文件经 stdin 加载，不进入 argv、工具参数或日志。Sol 使用公开合成输入验证两次读取；Terra 不重复测试该 helper，只准备 r3 非秘密输入和两 Agent 夹具，收到工具后再加载新凭据。应用源码继续冻结，不重跑已通过 Go/Web。
- Sol 已交付并冻结 [私有 Admin CLI 输入工具](run-private-admin.mjs)，公开合成自检覆盖 init 两次读取、agent apply、含单引号路径、错误权限与故意回显检测，结果为 `self_test_passed`。主代理读取完整实现，确认任何分支均不透传原始子进程输出，捕获上限 64 KiB、超时 120 秒；授权 Terra 用它完成 r3 正式初始化，其他输入方式不再使用。
- 等待输入工具期间，Terra 已在独立新 fixture 完成两个 selected native E1 案例：SOCKS5 `no_blackip` 与 HTTP `http_config_host_port` 均为 marker_match=true、proxy_connections=1，并已完成有界清理。这是独立网络效果证据，不含 r2/r3 认证输入、不外推为 AGY 模型 Task。

#### r3 接续与验收夹具启动纠正

- 主代理接续后确认 HEAD 仍为 `78ab470`，U2 改动完整保留；三份冻结 ADR 哈希与初始基线一致，工作树 `git diff --check` 通过。最终 U2/E1 动态证据尚未到齐，不将源码审核 GO 升格为功能验收通过。
- Terra 回报 r3 已进入服务启动：AGY Worker 首次因相对 `working_dir=workspace` 不存在而退出，已仅创建私有目录；随后前台实例稳定超过 50 秒，后台退出定位为启动 session 生命周期，正在使用 `setsid` 修正。此处是验收夹具调整，未改应用源码，Task A 尚未提交。
- 已要求验证者记录最终 AGY cwd 的绝对路径，使外部文件核验与实际运行目录一致；尽快从真实指挥台提交 Task A，并在模型运行期间完成同一 Chrome 批次的观察、Markdown 和断线验证。不重跑已通过的 native、Go 或 Web 批次。
- 验证准备稿保留历史步骤，但最终需在顶部链接执行报告并明确 r2 已退役，禁止继续给出旧凭据/会话的复用指引。最终只在最小动态证据到齐后短核与提交，不回开完整 ADR 验收。

#### r3 进度回报纠正与环境诊断事故

- Terra 最初回报 HTTP profile 已创建并提交测试，随后纠正为浏览器导航定位超时，实际没有执行写入；当时正式 UI 为 0 个方案、0 个绑定，Task A 尚未创建。原“等待异步探测”判断无事实支持，不能作为验收证据。
- r3 AGY Worker 已独立会话常驻并持续心跳，但 Backend 为 `unavailable`。主代理只读源码确认 `mgraftcp --version` 属于 Runtime identity 检查；AGY Health 实际调用 `agy-graft --version`，此前先执行网络环境过滤，`inherit` 模式遇到含凭据代理值会直接拒绝。默认 shell 命令成功不能替代正式 Worker 环境的健康证据。
- Terra 尝试读取进程环境并重现 Health 时，将未拆分的 NUL 环境字符串传给 Node 子进程接口，默认异常输出包含外部代理与 API 凭据片段；未启动 Health 子进程。本次诊断证据废弃，不记录原始输出或任何秘密值。主代理已明确告知用户相关外部凭据需要轮换。
- 已停止完整进程环境读取和该诊断路径；后续仅根据既有启动代码确认 r3 身份是否曾进入环境，使用显式最小环境重启同一冻结 AGY Worker，不继承 proxy/API 环境，不借用交互 shell 结果替代。未确认受影响的身份不得继续使用；没有证据时不得仅凭截断输出声称排除影响。
- 新增但未执行的配置摘要工具不作为验证证据。已要求停止扩展验证工具，继续当前最小 UI/Task/浏览器批次，任何 JS 顶层错误只输出固定分类，禁止默认异常回显输入。
- Terra 根据启动代码确认 owner 为 stdin-only、浏览器 session 在私有 state、Worker token 在配置/注册链，未被启动命令注入环境；据此继续 r3。AGY Worker 已用最小环境重启为 generation 4，仍报 `unavailable`；外部等价 wrapper 版本命令成功只构成前置事实，不能据此确定内部故障位置或宣称根因已修复。
- 验证者因 `unavailable` 再次停在未创建 profile 的状态。主代理只读确认 `ProcessNetworkWork` 不依赖 Backend 普通 Task 的可用标记，前端“测试当前内容”也不以 Health 为前置，已纠正为直接使用正式 UI 创建/测试/发布/绑定以验证配置修复流程。只有实际 Test 的分层失败才能作为本步失败证据；普通 Task 仍须等待真实 applied 与可领取状态。

#### r3 正式配置修复与 Task A 启动

- Terra 已从真实 UI 创建 `e1-http-g4`，目标为 `e1-agy-agent / named / worker-93b6c5ba-cab0-476d-b44f-ed5b34c7f7d0 / generation 4`；这次有实际方案条目和后续正式 Test 回执，不再使用前述失败自动化作为证据。
- 正式 Test 已完成：content v1，总耗时 886ms，无诊断；configuration、endpoint、runtime_health 通过，secret/direct 为不适用，network_effect/model_call 保持 `not_verified`。发布完成，binding r1 已由同一 Worker generation 应用。
- Task A 已由真实指挥台创建，ID 为 `task-450a7f16-2554-441d-bc93-cafeb7dcdde2`，当前进入 `running`。尚待终态、workspace 内容与摘要、RunAttempt/Journal 和同一 Worker 接取 Task B 的核验；此时不能宣称业务闭环通过。

#### Task A 外部核验失败与一次明确纠正

- Task A 在 UI 终态为 `uncertain`。独立检查发现唯一 marker 文件已创建，但为 31 bytes，缺少原要求的末尾 LF；实际 SHA-256 为 `5e52eea3b68246156cc6544b51e29da2c65016d7ed5c0f9a086c403b8b2e1446`，原期望为 `efaca48d9c60940b6dd83d7af0c23a1bc0ca7d1b5773933cee0287d1cb260eae`。A 的外部业务核验失败保留，不因 Runtime 返回内容或文件存在而标作通过。
- 主代理授权一次新的明确纠正任务 A2：仅当已确认唯一文件的 SHA 仍匹配上述实际值时追加一个 LF，再读回并输出 SHA；前置不匹配立即停止。不回开应用源码，不改变 A 的原状态，也不自动重新执行副作用未知任务。
- 调度消息交错期间，Terra 已按既定矩阵创建只读 Task B `task-3b578134-5cd1-4a4b-b31c-f1715c5149c9`，发生在 A2 之前。实际顺序保留为 A → B → A2；B 只能按当时 31-byte 文件核对其只读结果与同一常驻 Worker 身份，不能宣称满足原 32-byte 断言。
- 为快速收口，不再追加 B2 或制造无失败的 A/B 历史。A2 后由独立程序核对 32 bytes 和原期望 SHA；真实模型批次以原失败、只读常驻证据及一次明确纠正的实际结果结束。A2 若再次失败则停止重试，继续完成独立观察/Markdown 项并如实报告部分验收。

#### 已执行结果与用户要求交付方案

- A2 `task-1da1fff7-a03d-4301-b2f5-0b90ca5b46a7` 的结果页显示 32 bytes 和原预期 SHA，独立 `cmp` 与 SHA 核对也通过。已回报其 Run 为 `succeeded`，Task 仍为 `uncertain / business_effect_unverified`；Worker 为 generation 4、named profile v1、binding r1。外部纠正证据只写入报告，不提升产品内核验状态。
- observer Task `task-5f9abc00-e2f3-4143-9ba2-48fbf79c6e44` 在约 55 秒观察窗口后仍为 `queued`，未进入预期 `waiting_input`，原因未确认。其输入中的 code fence 被验证 shell 误解析，实际 Task 不含预期代码块；该输入不能证明代码块渲染。已观察到的 DOM 仅为链接存在、图片未加载、表格和原文/复制入口可见，不外推为复制成功或完整安全/移动端验收。
- 断线恢复、浏览器 Control POST 计数、关键幂等重放、坏配置正式失败和剩余浏览器交互尚未完成。最小验收矩阵仍不完整；既有测试和源码审核 GO 不能替代这些证据。
- 用户随后明确询问为何长期未收敛，并要求提供收敛与交付方案。主代理提出冻结现有代码和证据、汇总已验证/未验证/失败、一次交付短核、独立提交及外部凭据事故单独善后的阶段性交付方案；其中延期剩余最小验收属于提案，尚未获得用户确认，不能据此改变当前 Gate 或宣称目标完成。
- 当前源码保持原冻结指纹，16 个源码/测试文件已在此前整理时暂存，U2 尚未提交。全部执行代理已结束；本次只恢复验证者整理三份既有报告和已有非秘密证据，禁止新测试、浏览器/API、服务或源码修改。主代理仅更新协调记录，继续保留未完成目标。
- Terra 已完成三份报告事实校正并结束，执行报告现为 `partial_incomplete`；两个准备稿标明历史性质并撤销 r2 复用指引。独立审核者随后完成一次交付材料短核：核心源码与三份 ADR 指纹未变，源码 GO 保持，动态验收 `partial/incomplete`，阶段交付提案不能自行改变原最小验收矩阵。
- 用户进一步询问具体未通过项。当前应分开说明：隔离 observer 最近观察仍 queued、原因未定；断线/消息/离线写保护及其余浏览器、坏配置、幂等与完整 B 证据未完成；A 的历史缺 LF 失败已由独立 A2 精确纠正；外部凭据事故的轮换仍待处理。本次没有新增测试、源码修改或提交。

#### observer 调度前提的只读定位

- 目标接续后，主代理仅通过既有私有 browser state 加载认证，对正式 Observe API 作白名单只读检查：observer Task 为 `queued`、Run 数为 0；observer 的 binding 数为 0、mode test 数为 0；generation 1 的 fixture Backend 为 `unavailable`，模式为 inherit，但没有 policy version 和 binding revision。未读取进程环境、未输出任何 Cookie/Token/原始异常。
- 源码边界与上述事实对应：`ListWorkerBackends` 在 Backend 没有正式 binding 时直接标记 unavailable，`M1TurnPlanner` 不选择 unavailable Backend。因此已定位一个足以阻断 observer 的明确原因：夹具漏做模式测试、发布和 applied 前置。无需以重建环境或修改调度源码处理。
- 已恢复 Terra，仅通过既有指挥台为 observer 的 inherit 模式完成正式测试/发布/applied，再核对原 Task 是否进入 waiting_input；继续原最小矩阵缺失的浏览器交互、断线、错误配置和幂等，不扩张新范围，不重跑模型/native/Go/Web。此为原授权验收的继续，不采用尚未确认的延期提案。
- 验证脚本只允许结构化参数或由 apply_patch 创建的脚本数据，禁止将含反引号的代码或 Markdown 内插进 shell 命令；所有顶层错误必须输出固定分类。源码继续冻结，后续结论只随真实新证据更新。
- Terra 已通过真实 UI 为 observer generation 1 提交 inherit/policy v1 测试，20 秒观察仍等待 Worker。其脚本曾误读页面中 AGY 的完成文案，已纠正；这个短观察窗口不构成正式失败或进程已停止的证据。
- 主代理随后核对正式 mode test 对象仍为 `pending`、generation 1、无探测结果；并按既有 PID 文件对 observer PID `1177481` 作存活检查，确认进程及 `/proc/<pid>/exe` 均不存在。此为真实停止证据，与观察超时不同。已授权仅恢复这个停止的 observer Worker，采用已验证的独立 session/最小环境；新 generation 必须重新测试/发布，旧 generation 测试保留历史，不复用过期回执。

#### 2026-09-06 HTTP 能力错误映射的最小回开

- 恢复 observer 后取得正式失败：Worker 注册后领取已有 queued Task，BeginAttempt 返回 HTTP `422 UNSUPPORTED_CAPABILITY`，Worker 随即退出，无法继续处理模式测试。该现象已从“前置未配置”推进为四类直接风险中的可复现主流程阻断，不再只按夹具状态不明处理。
- 主代理定位 `RunManager.startWork` 已处理 `errors.Is(err, domain.ErrUnsupportedCapability)`，但 `WorkerClient.APIError.Is` 缺少该 HTTP 错误码映射；领域内测试无法证明跨 HTTP 行为。这是前置客户端边界的缺陷，N2 历史 GO 保留，并明确此前没有覆盖“任务先于绑定且真实 HTTP 拒绝”这一组合。
- 已授权唯一 Sol 实现者最小回开客户端错误映射和必要回归。另读 `TryClaimMailbox` 确认已有未过期 claimed item 会立即重返；仅映射错误会产生快速重试，因此允许在同一未开始 Run 的能力不足分支补最小可取消等待，禁止扩展通用重试框架、数据库或 UI 改造。
- 本次验收矩阵限定为：实际 HTTP422 经正式 client 能识别领域错误；等待时 Worker/控制连接存活且请求次数有界；配置恢复后只开始一次；认证、generation、lease、fencing 错误仍按原安全规则失败；不伪造 Run 或成功终态。只运行 client/worker 与受影响 worker 包，后续以原 r3 的 queued Task 和正式模式测试补动态证据。
- Terra 暂停重启旧二进制，等待修复冻结后仅构建一次并恢复 observer。已完成 AGY/native/Go 六包/Web 证据保留，不重跑；新旧二进制及实际证据身份必须分别记录，不把旧 AGY 执行伪称新产物的实测。
- Sol 已完成最小修复并冻结：`internal/client/worker/client.go`、新增 `client_test.go`、`internal/worker/run_manager.go`、`runner_test.go` 四文件。实际 HTTP 测试覆盖能力/认证/租约/fencing/stale 错误映射与互不混淆；同 claimed item 重返用例覆盖一秒可取消退避、期间 heartbeat 推进和恢复后一次 Run/Finish。作者 `go test ./internal/client/worker ./internal/worker` 与定向 diff-check 通过。
- 已分别恢复独立审核者只审这四文件，以及 Terra 只跑两包、构建一次并恢复原 observer；其余源码仍冻结。最终需从原 queued Task 证明 Worker 等待存活、模式测试/发布/applied 可继续、只启动一次，再完成原剩余浏览器与关键失败/幂等证据。
