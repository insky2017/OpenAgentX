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
| ADR-002 实现 | `/root/adr002_impl` | `gpt-5.6-sol` high | implementation-complete, awaiting independent verification | Runtime、Worker、持久化/API；未修改 `web/src/main.jsx` |
| ADR-003 实现 | `/root/adr003_impl` | `gpt-5.6-sol` high | implementation-complete, awaiting independent verification | 指挥台 UI、Observe 投影和 Markdown；未修改 ADR-002 Runtime 装配 |
| 批量验证 | 待派发 | `gpt-5.6-terra` high | pending | Go、前端构建、浏览器/契约证据；不得改实现 |
| 独立审核 | 待派发 | `gpt-6-astra` medium | pending | 审核前两个实现结果、测试证据和剩余风险 |

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
