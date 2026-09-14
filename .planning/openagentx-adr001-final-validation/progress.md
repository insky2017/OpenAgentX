# Progress Log

## Session: 2026-08-31

### Phase 1：恢复实施与验证基线

- **Status:** complete
- 已确认 20 个实施任务文档均标记完成，Git 历史存在对应实施提交。
- 已确认 T01-T03 测试提交和 CodeBuddy Runtime Adapter 提交。
- 已确认 daemon 与 quote-service Worker 均为 `active`。
- 已确认 T04 失败集中于 AGY Adapter/CLI 契约，不是 Worker 生命周期重启。
- 已确认正式验收计划为 T01-T10，当前 T01-T03 passed、T04 pending。
- 已读取 `planning-with-files` 完整规则并建立持久执行账本。

### Phase 2：T04 AGY 实机闭环

- **Status:** complete
- `sol medium` subagent 已留下 AGY Adapter、测试和相关文档改动，但最终因 429 中止，尚未形成可采信交付。
- 下一步：审计当前 diff，并由 `terra high` 接管补全与验证。
- 用户补充：WorkBuddy/CodeBuddy 不再承担代码、测试任务或审计，只能作为 OpenAgentX Runtime 功能验证中的最小黑盒被测对象。
- 已读取主测试计划与 T04 原始步骤，记录 Task A/B、ExecutionSpec、Worker 常驻、无 tmux 和失败路径的必要证据。
- `terra high` 已接管前一补丁并完成静态验证，未真实调用模型、未重启服务、未提交、未修改 ADR-001。
- 主审查已确认关键实现方向：direct argv、NDJSON 输入、ExecutionSpec 参数接线、stderr 持久诊断、缺失终态 fail closed；下一步核实本机 CLI help 与测试覆盖。
- 主审查已核实本机 CLI help 和新增测试覆盖；尚未证明的唯一输入契约是 NDJSON message 的具体 schema，将由真实 Task A 验证。
- 已核实 AGY changelog 的输入/输出事件语义，并确认模型目录可查询；下一步检查 quote-service 的实际 ExecutionSpec 装配。
- 已确认 quote-service 当前只会解析为 backend 默认模型/推理；T04 实机任务需显式选择受支持 model/effort，或先完善安全配置入口，不能仅靠单元测试宣称参数已生效。
- 发现 `M1TurnPlanner` 当前未消费 Task 请求的 ExecutionSpec；先记录为后续 ADR 全量审计项，不在 AGY CLI 契约补丁中无边界扩张。
- 主代码审查继续标记 model allowlist、preflight approval 与 AGY 子进程取消三项风险，先核对现有控制面实现再决定是否并入 T04 修复。
- 发现 T04 直接缺口：AGY Worker 未从 YAML 装配 `WorkingDir`，真实 turn 不在 AgentProfile workspace 中；将在构建前补齐配置解析和测试。
- 已补齐 AGY `working_dir` 相对路径解析、目录存在性校验、model catalog allowlist，并将 quote-service 指向 SteadyFlow 根目录和明确模型 `gemini-3.7-flash-low`。
- 已撤销猜测性的 `IMPLEMENTATION_DISCUSSION.md` schema 变更；冻结 ADR 未触碰。
- 新增改动的定向验证通过：`go test ./internal/runtime/agy ./internal/cli/worker`、`git diff --check`。
- 已构建二进制 SHA-256 `2a2b2066...adfa` 并重启 daemon/quote-service Worker。
- daemon 立即 active；Worker 因旧 active lease 尚未过期收到 4 次 `409 logical Agent already has a valid Active Worker`，随后 lease 过期并自动注册成功。该现象属于恢复行为证据，后续 T07 需验证是否应提供更平滑的受控重启交接。
- Worker 当前稳定 online：generation `3`、fencing `5`、WorkerInstance `worker-010aaff7-0bcb-44d7-bb56-848ee2423b0f`；准备通过正式 Control API 提交 Task A。
- 尝试复用 T02 浏览器 session，但其当前为 Chrome error page；继续检查其他已存在 session，不读取或输出 Cookie/密码。
- 历史 `task17` 只连接本地 4174 开发前端；未将其状态冒充生产登录。下一步确认远程 HTTPS 网络路径。
- 已确认远程 HTTP 强制跳转 HTTPS 且 HTTPS 200；将新建独立 T04 浏览器 session 检查登录状态。
- 新生产浏览器 session 受代理路由阻断；改为在已有本地前端 session 内以同源 fetch 验证 Session，敏感 token 仅保留于页面内存。
- `task17` session 的同源 Session fetch 返回 `Failed to fetch`，表明历史 4174 dev server 已停止；不会重复同一调用。
- Vite 无 API proxy，故不重启 4174。下一步让历史 session 直接访问同 host 的 18100，检查 Secure Cookie 是否仍可用。
- 同时发现 daemon/Worker 高 CPU 风险，记录为必须闭环的运行时缺陷，不因 T04 模型链路成功而忽略。
- 已关闭 `task17` 的 offline 模式并恢复页面访问；本地 18100 显示登录页，继续尝试生产 HTTPS 历史 session。
- 生产历史 session 无 Cookie，不能复用；继续检查 `task17` 的 cookie metadata（只看名称/域，不输出值）。
- 已确认所有可复用浏览器 session 均无认证 Cookie，密码管理器也未填充密码；正式 Control API 当前缺少可用认证材料。
- 已从只读状态确认新 backend healthy 且模型目录生效；T04 模型选择前置通过。
- `sol medium` 已完成高 CPU 只读诊断，确定为 Worker Control 伪 long poll，并给出 Broker wakeup/鉴权/lease 完整修复边界。
- 已委派同一 subagent 实施修复和高强度定向测试；当前真实 AGY Task 暂停，防止资源争用污染证据。
- subagent 已完成 long poll/Broker/fencing 修复并通过 count/race/UDS/全量/vet；主审查已确认核心调用链和测试覆盖，下一步检查 guard 细节后构建部署。
- 构造调用和 UDS client wire 映射审查通过；继续核对 repository guard 的精确错误语义。
- repository guard 审查通过；已构建并重启实际持久服务，10 秒空闲 CPU 从高占用降至 daemon `0.50%`、Worker `0.00%`。
- 35 秒跨周期采样继续保持空闲，busy-loop 缺陷实机验证通过；开始 T04 Task A。
- 启动不存在的 fake/verification unit 名失败后，已通过实际 unit 列表纠正；未创建猜测性服务。
- 已打开可见本机登录窗口等待用户认证；生产 HTTPS 的 agent-browser 代理路径仍失败，后续不再重复。
- 已按用户授权尝试登录；不带句末标点的密码未显示错误，但 HTTP 页面无法维持 Secure Session，下一步只检查 cookie metadata 并改用安全 HTTPS/curl 会话路径。
- 正式 HTTPS 登录探测通过，owner/CSRF 均有效；T04 Control API 认证不再阻塞。
- 真实 Task A `task-86aec3e6-a87a-4ab6-aac4-c59174d97ae3`、Task B `task-874ebc71-6fd2-4ada-a618-cf1d867ea7a0` 均 succeeded；分别对应独立 RunAttempt 与 Mailbox sequence `16/17`。
- 两次 turn 共用 Worker `worker-c8c03350-4b43-4690-b283-ab48c435cfad`、generation `9`、fencing `17`，turn 间持续 heartbeat，未使用 tmux。
- Runtime `agy.init` 与真实 `pwd` 工具输出共同证明模型 `gemini-3.7-flash-low` 和 SteadyFlow workspace 生效。
- AGY/Worker 定向测试、`go test -count=1 ./...`、`go vet ./...`、`git diff --check` 和冻结 ADR hash/diff 检查全部通过。
- 最终部署二进制 SHA-256 `71f24dd6...b992`；daemon/Worker 重启后由 generation `10`、fencing `19` 安全接管，空闲 CPU 平均 `0.60%/0.00%`。
- T04 测试任务与总测试计划已标记 passed，正式报告已建立；下一关进入 T05。

## Test Results

| Test | Evidence | Expected | Actual | Status |
|---|---|---|---|---|
| T01 | commit `3a6f3ac` | 通过 | 通过 | passed |
| T02 | commit `6ab3d78` | 通过 | 通过 | passed |
| T03 | commit `b07d3e1` | 通过 | 通过 | passed |
| CodeBuddy Runtime real path | task `task-cf086128-2759-46e3-a0fa-8f4cc1492124` | succeeded | succeeded | passed |
| T04 AGY Task A (legacy command) | task `task-5e492668-458c-4dcd-a8c7-d3a6aa7648eb` | succeeded | uncertain | failed |
| T04 AGY Task A (`agy-graft`) | task `task-31981d1f-c42a-449f-a4c6-73d4d3e566b2` | succeeded | uncertain | failed |
| T04 AGY Task A（最终） | task `task-86aec3e6-a87a-4ab6-aac4-c59174d97ae3` / run `run-71e75ec3-d2ed-4cba-b8a0-4a6cd08c70bc` | succeeded | succeeded | passed |
| T04 AGY Task B（最终） | task `task-874ebc71-6fd2-4ada-a618-cf1d867ea7a0` / run `run-45a8d0b6-085b-456c-99ec-8dc1bbd2e998` | succeeded | succeeded | passed |
| T09 production command center API core | task `task-ec41aea8-2a04-43f8-932b-711bec5b1fd4`；证据见 T09 报告 | 生产 HTTPS、Task/Message、SSE、取消 API、Worker 常驻 | 已通过抽检 | passed with blocked subtest |
| T09 long-running AGY cancel | task `task-89ff5a4b-0a8c-4d0b-ba3b-9b635a0a0b94` | `sleep 30` 期间被取消 | eligibility DNS failure before sleep | blocked |

## Error Log

## T09：生产指挥台端到端（2026-09-01）

- 生产 HTTPS、Nginx、SSE、PWA 基础资源、认证/CSRF、生产 Task/Message、跨 turn Message、Worker 常驻和取消 API 均通过抽检。
- Panel 回复 Message 的 `sender_principal_id` 缺省问题已由 Sol 修复并补回归测试；相关改动尚待独立提交。
- 取消实战 `task-89ff5a4b-0a8c-4d0b-ba3b-9b635a0a0b94` 已验证 `running → cancel_requested → canceled`、control mailbox 领取/接受和 Worker 持续在线。
- 认证后手机/PC 完整页面链路、独立双 Session 对同一持久事实的成对记录、认证后敏感信息隔离和生产浏览器离线禁写均尚无充分证据。
- 真实长运行取消被 AGY eligibility DNS 故障阻断，发生于预定 `sleep 30` 之前；不将其记为真实长运行进程取消通过。T09 保持 `BLOCKED`，不能进入 T10 的 GO 判定。

| Timestamp | Error | Attempt | Resolution |
|---|---|---:|---|
| 2026-08-31 | `sol medium` subagent: 429 Too Many Requests, exceeded retry limit | 1 | 改用 `terra high` 接管现有落盘改动，避免从头重复 |
| 2026-08-31 | daemon 与 Worker 紧邻重启后新 Worker 注册收到 409 | 1 | 未绕过 lease；由 systemd on-failure 重试，旧 lease 到期后成功注册 |
| 2026-08-31 | 历史 `task17` 页面同源 API fetch 失败 | 1 | 检查 4174 dev server 配置与监听状态，再决定是否安全恢复该正式入口代理 |
| 2026-08-31 | 首个临时 Cookie 清理脚本被安全策略拒绝 `rm -f` | 1 | 改为无文件登录探测，响应经 `jq` 仅输出 principal 与 CSRF 存在性 |
| 2026-08-31 | 猜测的 fake/verification Worker unit 名不存在 | 1 | 读取 systemd 实际 unit 列表；只重启已配置的 daemon/quote-service Worker |

## 5-Question Reboot Check

| Question | Answer |
|---|---|
| Where am I? | Phase 3，T09 生产 API 核心抽检通过；浏览器全旅程与真实长运行取消仍阻塞 |
| Where am I going? | 完成 T09 修复提交与独立审计；补齐生产浏览器证据、恢复 AGY eligibility 网络后解除阻塞，再进入 T10 |
| What's the goal? | 20 项实施真实满足冻结 ADR-001，并完成逐项验证与缺陷闭环 |
| What have I learned? | 见 `findings.md` |
| What have I done? | 见本文件上述记录 |
