# OpenAgentX ADR-002 / ADR-003 实施协调记录

## 任务目标

由主代理协调 ADR-002 Runtime 网络配置、应用与诊断，以及 ADR-003 指挥台任务详情、运行观察与安全内容呈现的最终落地。实现、验证和审核必须留下可追溯证据；任何未实测能力不得标记为已完成。

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
| ADR-002 实现 | `/root/n1_worker_consistency`，前序 `/root/adr002_impl` | `gpt-5.6-sol` high | N1 已冻结，独立源码与已覆盖验证 GO | Runtime、Worker、持久化/API；不修改 Panel/Web |
| ADR-003 实现 | `/root/u1_task_observation`，前序 `/root/adr003_impl` | `gpt-5.6-sol` high | U1 已冻结，独立源码和浏览器验证 GO | 指挥台 UI、Observe 投影、查询；不修改 Runtime/Worker/migrations |
| N2 配置工作流准备 | `/root/n2_network_workflow` | `gpt-5.6-sol` high | 只读准备，尚未授权代码修改 | N1/U1 提交前不得改变冻结源码；准备网络工作流全栈的接口与文件边界 |
| 批量验证 | `/root/n1_u1_verification` | `gpt-5.6-terra` high | N1/U1 GO，已冻结并释放验证服务 | 独占独立全量验证及新验证报告；不修改实现 |
| 独立审核 | `/root/n1_u1_review` | 指定 Astra 当前不可用，暂用 `gpt-5.6-sol` medium | N1/U1 源码及 Terra 证据交叉核对 GO | 独立审核代码及 Terra 证据，替代模型如实记录 |

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
