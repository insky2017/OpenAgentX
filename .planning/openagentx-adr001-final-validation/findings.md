# Findings & Decisions

## Requirements

- 以冻结 ADR-001 和 20 个实施任务文档为验收基线。
- 每项完成后必须验证并提交，随后才能进入下一项。
- 最后执行全量验证；发现缺陷继续修复，直到实际可用。
- 代码、测试任务和审计由主 Agent与 Codex 内置 subagent 执行，subagent 限定 `sol medium` 或 `terra high`。
- WorkBuddy/CodeBuddy 不参与具体代码或测试任务，只能在测试 OpenAgentX Runtime 功能本身时作为最小黑盒被测对象。

## Current Evidence

- 主实施计划的 01-20 均标记为 `completed`。
- 冻结后的实机测试计划是 T01-T10；T01-T03 已通过，T04-T10 尚需逐关完成。
- Git 历史存在 20 项实施对应的连续功能/发布提交，当前 HEAD 为 `6c103e5`。
- T01、T02、T03 已有独立测试通过提交：`3a6f3ac`、`6ab3d78`、`b07d3e1`。
- CodeBuddy Runtime Adapter 提交为 `6c103e5`，真实链路 Task `task-cf086128-2759-46e3-a0fa-8f4cc1492124` 已成功。
- daemon 与 `openagentx-quote-service-worker.service` 当前均为 `active`。
- AGY T04 两次真实任务均快速进入 `uncertain`，Worker 未重启；根因报告判定为 `CLI_CONTRACT_MISMATCH`。
- 已知 AGY Adapter 问题包括：缺少 `--input-format stream-json`、stdin 不是 NDJSON、stderr 丢失、model/effort/timeout 未接线、能力描述虚标、成功判定不够 fail-closed。
- `terra high` 已在不调用真实模型的前提下完成 AGY 修复静态验证：相关最小测试、`go test ./...`、`go vet ./...`、`git diff --check` 和 AGY help contract 均通过。
- 当前补丁把默认 binary 统一为 `agy-graft`，通过 direct argv 传入 stream-json input/output、model、effort、timeout、conversation 和非交互权限。
- 当前补丁使用“单行 JSON string + newline”作为 NDJSON prompt，终态 parser 要求明确 `result` 或 error 事件；该输入 schema 仍需真实 T04 turn 证明。
- stderr 已并发 drain、限长、脱敏，并经 `RunManager` 保留到 RunAttempt、Task 与 Event Journal；非零退出、空流、畸形流、缺失终态均保持 fail closed。
- 主 Agent 直接读取 `agy-graft --help`：确认 stream-json 输入为“one NDJSON message per line”且要求 stream-json 输出；model、effort、conversation、print-timeout 和 skip-permissions 参数均存在。
- 单元测试覆盖精确 argv、恰好一行 JSON string stdin、default 参数省略、budget/sandbox 拒绝、执行超时、stderr 脱敏、exit 0 无终态、配置解析和诊断跨层持久化。
- `--help` 没有公开 NDJSON message 的对象 schema，因此当前 JSON string 输入只能由真实 T04 turn 最终确认，不能仅靠 help 宣称完成。
- 本机 `agy-graft` 是 `/home/sky/tools/bin/agy-graft` 包装器，真实 `agy` 位于 `/home/sky/.local/bin/agy`；本机 tools/local/cache 中未检索到更具体的 stream-json 输入 schema 文档。
- `agy-graft changelog` 1.1.15 明确：stream-json 从 stdin 读取 newline-delimited JSON prompts，每条 message 运行一个 turn，并在同一 conversation 中持续；1.1.8 明确输出为 `init`、`step_update`、终态 `result`，与 parser 的终态约束一致。
- `agy-graft models` 可在不发起 turn 的情况下列出当前可用模型；T04 仍应以 AgentProfile/ExecutionSpec 的实际选择为准，不能在 Adapter 中硬编码模型。
- 当前 `quote-service/agent.yaml` 仅配置 `binary: agy-graft`，没有配置 Adapter model catalog；因此 Scheduler 会得到 `model=default`、`reasoning=backend_default`，真实 Task 默认不会携带 `--model/--effort`，不足以单独证明 T04 的参数接线要求。
- daemon unit 运行 `bin/openagentx serve`，Worker unit 通过非交互 zsh 加载 `.zshrc`、执行 `set_proxy_server` 后运行 `openagentx worker run`；两个 unit 均 `Restart=on-failure` 且当前 active。
- Worker unit 未把代理凭据直接写入 unit 文件，符合凭据不落仓库/报告要求。
- 重启后的 quote-service Worker 已注册为 generation `3`、fencing token `5`、status `online`，当前 WorkerInstance 为 `worker-010aaff7-0bcb-44d7-bb56-848ee2423b0f`；generation 2 已正确变为 offline。
- daemon 没有可用的 `/health` 或 `/api/observe/v1/health` HTTP 实现（均为 404/SPA fallback），服务可用性当前以 systemd、Worker heartbeat 和受认证 Observe API 为证；健康端点缺口记录到最终运维审计。
- `agent-browser` 中存在 T02 历史 session，但 `openagentx-t02` 当前停在 `chrome-error://chromewebdata/`，没有可复用的已登录页面证据；不能据此绕过重新认证。
- 另一个 `task17https` session 同样是 Chrome error page；`task17` session 在 `http://rtx4090:4174/` 显示已登录 UI，但它是历史本地前端开发服务器，不等价于当前生产 HTTPS Session，不能拿来作为正式 Control API 身份。
- 远程入口网络链路正常：`http://agentx.oneaxe.cn/` 返回 301 到 HTTPS，`https://agentx.oneaxe.cn/` 返回 200，连接经 Tailscale `server` 节点；此前 Chrome error 不是 Nginx/daemon 全局不可达。
- 新建 `agent-browser` 生产 session 仍返回 `ERR_EMPTY_RESPONSE`，说明该 Chromium 进程未复用 shell 的网络/代理链路。
- Control API 的 `createTask` 从已认证 Session 注入 `SenderPrincipalID`，并要求 CSRF token 与同值 `Idempotency-Key`/body meta；可在不暴露 token 的浏览器同源 `fetch` 中完整执行。
- 历史 4174 Vite server 已停止，且仓库 Vite 配置没有 API proxy；重启它不能恢复正式 Control API Session，因此不采用该路径。
- `task17` 浏览器上下文先前被 T02 离线测试留在 offline 模式；关闭 offline 后可正常打开 `http://rtx4090:18100/`，当前显示登录页。这解释了此前浏览器 `ERR_INTERNET_DISCONNECTED`，也提示测试结束必须恢复浏览器网络状态。
- `task17https` 关闭 offline 后仍因浏览器网络路径返回 `ERR_EMPTY_RESPONSE`，且该 session 没有保存任何 Cookie；无法用它复用生产登录。
- `task17` 也没有 Cookie；登录表单仅自动填充了 5 字符用户名，密码字段为空，无法在不取得用户密码的情况下通过正式 Web Auth 提交 Task。
- 已启动可见 `openagentx-t04-login` 浏览器窗口指向本机登录页，供用户在不泄露密码的情况下建立 Session。
- 用户授权的密码已仅用于浏览器输入：带句末标点的解释被服务明确拒绝；不带句末标点后页面清除密码且不显示认证错误，但仍回到登录页，符合 Secure Cookie 在本机 HTTP 上未被后续请求接受的现象。凭据未写入任何文件/报告。
- 通过正式生产 HTTPS Auth API 已确认 owner 登录成功、角色为 `owner`、CSRF token 正常返回；输出已过滤 token，且该探测未持久化 Cookie。真实 Task 可在服务修复后用临时内存/受限文件会话提交。
- 即使在启动浏览器前加载 `set_proxy_server`，Chromium 访问生产 HTTPS 仍返回 `ERR_EMPTY_RESPONSE`；不再重复这一代理方式。
- 新 generation 3 的 backend registration 已持久化为 `agy-batch/primary`、health `healthy`，descriptor model 为 `gemini-3.7-flash-low`，reasoning modes 为 `backend_default/effort`；说明新 Worker YAML 和 Adapter descriptor 已实际生效。
- 正式 Web 用户仅有 active `owner`；未读取 password hash，仍需用户通过浏览器完成登录。
- 进程检查显示 daemon 在重启后持续接近单核满载，三个 Worker 也各自累计明显 CPU；这与“Worker waits”目标冲突，需在 T04/T07 期间测量并定位是否为 long-poll/broker busy loop，而不能只看状态 online。
- `sol medium` 只读诊断已确定根因：`Runner.workerControlLoop` 持续发送 `WaitSeconds=30`，但 `WorkerService.ClaimWorkerCommand` 忽略 wait 并在空队列立即返回；三个 Worker 因此形成无退避 HTTP/UDS busy loop。
- Mailbox long poll 已正确实现；Worker Control 应复用“初查、subscribe、重查、防丢 wakeup、timeout/context、末次查库”模式。
- `WorkerControlTopic` 已定义但生产 Admin Service 未 Publish；同时 control claim 的 token digest、expiry、lease/status/revoke 校验存在 fail-closed 缺口。
- 现场 `pidstat` 证据：daemon 约 `171% CPU`，三个 Worker 约 `21%-24% CPU`，每线程每秒约 `2400-2850` 次 voluntary context switch。该缺陷属于 T04 阻断。
- busy-loop 补丁主审查确认：Admin command/revoke 均在 repository 成功返回后 Publish；daemon 将同一个 Broker 注入 WorkerService 与 WorkerAdminService；claim 在订阅前后双查并在 wake/timeout 后重查。
- Control wire request 新增必填 `fencing_token`，Runner 已从当前 Worker Session 传递；repository 在同一领取事务内调用 guarded worker 校验，并把 command lease 截断到 Worker lease。
- 新增测试直接覆盖 1s 空等待、5s 提前唤醒、subscribe race、丢 wakeup、context cancel、错误/过期 token、generation/fencing、lease expiry/revoke，以及真实 UDS wait/wake。
- 所有 `NewWorkerAdminService` 生产/测试调用点已迁移到强制 Broker 参数；nil Broker 有负向测试，避免未来退回无 wakeup 装配。
- UDS client 已将 body 的 `WaitSeconds` 同时映射到 `?wait=<n>s`，handler query 再覆盖 body；这使公开 wire 行为与 Worker Runner 的 30s 配置一致。
- 新二进制 SHA-256 为 `6d7f1087...0ac2`。受控重启后 10 秒 `pidstat`：daemon 平均 `0.50% CPU`，quote-service Worker `0.00% CPU`；此前 `171%/约23%` 的 busy loop 已消失。
- 跨越完整 30 秒 long-poll 周期的 35 秒 `pidstat -w` 显示 daemon/Worker 平均 context switch 采样为 `0.00/s`，未再出现每秒数千次空请求；空闲等待行为已实机闭环。
- 实际持久 unit 只有 daemon 与 quote-service Worker；先前运行的 fake/verification Worker 不是持久 unit，daemon 重启后已退出。错误 unit 名未被继续猜测或创建。
- 当前 `M1TurnPlanner.Plan` 从 Worker descriptor 选择首个 model，并优先使用 `reasoning=backend_default`；它没有读取 Task 中的请求 ExecutionSpec。T04 可验证 backend-default 的正确省略语义和 Adapter 映射单测，但“Task/turn 任意覆盖模型与思考难度”的 ADR 能力需在后续专门验收，当前不能视为已证明。
- 主代码审查发现两项需继续核对的安全/契约边界：AGY `Validate` 当前没有自行确认 `spec.Model` 属于 descriptor model catalog；Adapter 无条件传 `--dangerously-skip-permissions`，必须确认控制面确实已完成 preflight approval，不能把 CLI 自动批准当成审批本身。
- AGY `RequestCancel` 当前只向父进程发送 SIGTERM，没有像 CodeBuddy Adapter 那样显式管理进程组；这会作为 T06 cancel/finish 竞态与子进程清理的重点风险项。
- T04 workspace 核对发现：`agy.Config` 有 `WorkingDir`，但 Worker YAML 装配目前只接受 `binary/models`，`quote-service/agent.yaml` 也未声明工作目录；因此真实 AGY 会继承 systemd 的 OpenAgentX 子目录，而不是 AgentProfile 的 SteadyFlow workspace。这是 T04 直接阻断项，应在当前补丁中修复并测试。
- 当前 quote-service backend model catalog 是隐式 `default`，无法把实际 provider model 与 RunAttempt 的 `model` 字段一一对应。T04 应至少配置一个明确、低成本、已由 `agy-graft models` 返回的 model slug。
- T04 最终真实任务均通过：Task A `task-86aec3e6-a87a-4ab6-aac4-c59174d97ae3` / Run A `run-71e75ec3-d2ed-4cba-b8a0-4a6cd08c70bc`，Task B `task-874ebc71-6fd2-4ada-a618-cf1d867ea7a0` / Run B `run-45a8d0b6-085b-456c-99ec-8dc1bbd2e998`。
- 两次 turn 共用 Worker `worker-c8c03350-4b43-4690-b283-ab48c435cfad`、generation `9`、fencing `17`，Mailbox sequence 为 `16/17`；Task A 结束到 Task B 到达之间 Worker 心跳连续。
- `runtime.agy.init` 直接记录 model `gemini-3.7-flash-low` 与 cwd SteadyFlow 根目录；两个 turn 的真实 `pwd` 输出一致。workspace 应描述为 Runtime Context/进程工作目录配置并经实机验证，不能扩大为工具协议级 Cwd 强制。
- Task A/B 运行二进制 SHA-256 为 `68402bec...a9aa`；最终 parser fail-closed 补丁部署后二进制为 `71f24dd6...b992`，未重复真实模型任务。
- 最终部署后 Worker 以 generation `10`、fencing `19` 恢复 online，旧 generation 9 offline；daemon/Worker 空闲 CPU 平均 `0.60%/0.00%`，active run 与 pending mailbox 均为 0。
- T04 已标记 passed，下一测试关卡为 T05；T05 不得复用 T04 的成功结果替代 multi-turn/queued steer 的直接证据。

## T04 Required Evidence

- 必须验证真实 `agy-batch` Runtime 的 binary、模型凭据、workspace、AgentProfile 和 Backend health。
- Task A/Task B 必须指向同一逻辑 `agent_id`，但各自拥有独立 RunAttempt。
- 两个 turn 之间 Worker PID、WorkerInstance、generation 和 fencing 保持可解释，Worker 返回 long poll。
- Task B 只能由持久 Mailbox + wakeup 驱动，不得使用 tmux、pane 地址或人工终端输入。
- `ResolvedExecutionSpec` 中 model/reasoning/timeout 必须真实传入 Runtime，而不只是配置存在。
- 超时、非零退出和输出解析失败必须得到确定且 fail-closed 的 Task/Run 状态。
- 每项报告需记录 commit、binary hash、schema、脱敏配置、Task/Mailbox/Run/Event、服务日志和明确 PASS/FAIL。

## Worktree Boundaries

- `agents/quote-service/agent.yaml` 已改为 `agy-graft`，属于 T04 必要改动。
- ADR-002、decisions 索引和现有验证报告可能来自并行工作，不得删除或回滚。
- 冻结文件 `docs/decisions/ADR-001-resident-agent-worker-runtime-observability.md` 不得修改。

## Technical Decisions

| Decision | Rationale |
|---|---|
| 当前修复先针对 AGY CLI 1.1.22 的明确 schema | 禁止通过猜测 stream-json 协议修复生产链路 |
| control-plane 终态继续 fail closed | 无法确认成功时必须保留 `uncertain`，不能把非空 stdout 当成功 |
| 不重复执行已有可信验证 | 将 token 用于测试设计、缺陷定位和证据审计 |
| 分离 20 个实施任务与 T01-T10 验收关卡 | 前者证明代码交付过程，后者证明冻结架构在真实环境中可用 |

## Issues Encountered

| Issue | Resolution |
|---|---|
| 首个 Codex subagent 因 429 中止 | 审计其落盘改动后，用不同允许模型接管，避免重复同一失败路径 |

## Resources

- `docs/decisions/ADR-001-resident-agent-worker-runtime-observability.md`
- `docs/design/OPENAGENTX_ADR_001_IMPLEMENTATION_BASELINE.md`
- `docs/plans/2026-08-30-openagentx-adr-001-implementation-plan.md`
- `docs/reports/validation/2026-08-31-openagentx-t04-agy-graft-cli-contract.md`

## T09 Findings（2026-09-01）

- T09 已确认生产 HTTPS/Nginx、SSE、PWA 基础资源、认证/CSRF、Task/Message、取消 API、跨 turn Message 和 Worker 常驻；但没有认证后手机/PC 完整交互、双 Session、敏感信息隔离和离线 fail-closed 的充分 L4 证据，不能扩大为完整手机指挥台通过。
- Panel 回复 Message 未携带 `sender_principal_id` 的缺陷已由 Sol 最小修复并补回归测试；当前代码与测试改动尚未提交。
- 取消实战在 `running` Task 上验证了 Task `cancel_requested`、control mailbox 创建/领取/接受、最终 `canceled` 以及 Worker 持续在线。
- 真实长运行 AGY 取消子项阻塞于 `daily-cloudcode-pa.googleapis.com` eligibility DNS 失败，发生在预定 `sleep 30` 执行前；不应解释为真实长运行进程取消通过。
- T09 整体保持 `BLOCKED`，T10 不得据此判定 GO；解除条件见 T09 验证报告。
