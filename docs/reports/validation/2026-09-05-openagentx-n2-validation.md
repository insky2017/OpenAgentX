---
doc_type: validation_preparation
scope: N2 network configuration workflow
status: pending_freeze
owner: independent-verification
prepared_at: 2026-09-05
model: gpt-5.6-terra high
---

# OpenAgentX N2 独立验证准备

> 本准备稿已被 [2026-09-05-openagentx-n2-validation-executed.md](2026-09-05-openagentx-n2-validation-executed.md) 的实际执行证据取代。该执行报告判定为 N2 功能范围 `partial_pass`：三种模式和当前配置页通过，完整 ADR 验收仍未覆盖。

## 当前判定

- 本文和 `verify-n2.mjs` 仅为待执行验证准备，未运行应用、daemon、Worker、真实模型、浏览器、`go test`、`go vet`、`go build` 或 race。
- 基线 HEAD 为 `0839341`，它只包含 N1/U1 GO；N2 后端与新配置页面仍在同一未冻结实现批次，不能借用 N1/U1 结论，也不能声明 N2 或 ADR-002 GO。
- 冻结 ADR-001、ADR-002、ADR-003 均不得修改；正式批次开始前重新记录三份 ADR SHA-256、最终源码 commit、可解释 diff、构建二进制 SHA-256、schema 版本、隔离配置版本与 Worker instance/generation/fencing。
- 已锁定的测试输入包括 `named_profile`，以及 `inherit`/`direct` 的 mode API：`POST /api/control/v1/network-bindings/mode/tests` 创建候选测试，`POST /api/control/v1/network-bindings/mode/publish` 才显式创建待应用 binding。当前 profile 内容 DTO 的 mode 仍是 `only_http_proxy` 或 `only_socks5`。

## 准备修订记录

- 本准备稿曾错误要求 `network_effect`、`model_call` 为 `passed` 才能通过 N2。该条件与已锁定 DTO 和 N2 边界冲突，现已更正：两层必须是 `not_verified` 且诊断为 `NOT_VERIFIED`；其诚实显示未核验与整体配置测试 `succeeded` 可以同时成立，真实出网和模型调用仍归 E1。
- 当前脚本只实际实现了 create 的同 key 同请求重放。下方十类矩阵保留为后续完整 ADR 验收计划；最新收口基线已将其余 helper 后置，不能据此宣称十类脚本已经完成。

## 验收矩阵

| 类别 | 待证明断言 | 正式证据 |
|---|---|---|
| 全流程 | 认证 UI 创建草稿、编辑、替换秘密、指定 Worker/Backend 测试、ready、发布、绑定、Worker `applied`、回退、一次性导入均经正式 API 完成；`inherit`/`direct` 先创建 mode test，再显式 publish 为 pending 并由 Worker ACK | Control receipt、Observe `{profiles,versions,tests,mode_tests,bindings,active_runs}`、Worker work/ACK、Event Journal、RunAttempt 与隔离 workspace 事实 |
| 状态不变量 | profile 内容版本不可变；head 以 `state_revision` CAS；binding 以 `binding.version` CAS；`ready` 对应同一内容/秘密/Runtime identity；`applied` 只接受当前 Worker generation/fencing/revision 的回执 | 各步骤前后 overview、Worker API 响应、持久化读模型和 Journal 关联断言 |
| CAS/幂等 | 同 key 同 body 只产生一个 receipt/版本/测试/绑定/import；双 Session 各自固定草稿 `state_revision`，后台自动刷新后旧基线只能显式重新确认或放弃，不能覆盖新草稿；旧 binding revision 不能发布 mode | Control HTTP 状态、版本计数、两个认证 Session、页面冲突提示以及显式重新确认/放弃动作 |
| 失败与安全 | 无权限、CSRF、同键异请求冲突、错误 target、离线 Worker、失败或 stale test、缺身份、错误 generation/fencing、旧 Worker ACK 必须 fail closed；失败/stale 一经 Observe 可见即停止等待；组合错误按 token/principal/generation/lease/fencing 的安全优先级返回 | Control/Worker API 失败响应、无状态变更的 overview/Journal、脱敏诊断代码 |
| 期望和实际 | UI 分开显示期望 profile+mode、target test、binding `pending/applied/failed` 与 Runtime 健康；profile/mode 测试不能改 binding 或健康，只有显式发布可创建 pending；`active_runs` 有运行时显示固定的 profile/version/revision，空态不推算历史字段，Task 按钮仅导航到既有 Task；发布或回退仅影响后续 Run | DOM、Observe、Worker ACK、两个 StartTurn 与 RunAttempt snapshot、实际 materialized policy 摘要 |
| 分层诊断 | named、inherit、direct 的 `probe_results` 必须恰含 `configuration`、`secret`、`endpoint`、`direct_rules`、`runtime_health`、`network_effect`、`model_call` 各一次；前五层按适用性为 `passed` 或 `not_applicable`，但 `inherit` 的 `secret`、`endpoint` 必须为 `not_verified/INHERITED_CONFIGURATION_UNVERIFIED`；后两层必须为 `not_verified/NOT_VERIFIED`，不可伪造 `passed`。整体配置 test `succeeded` 与后两层未核验可同时成立 | Worker ACK、Observe DTO、三视口 Chrome DOM 的 layer/state/脱敏 diagnostic 对照 |
| 版本与回退 | 安全版本历史不返回 secret/secret_ref/config 路径；回退创建待测候选，经成功 test 和显式发布/绑定后才可 applied；跨 profile 回退反例，即不属于当前 target binding 的 profile/version，必须拒绝；导入由真实 Worker ACK 一次创建版本，重放不复制内容 | versions/tests/import receipt、manifest digest、Worker source identity、重复请求结果 |
| 前端/PWA | 390x844、412x915、1440x900 可完成配置与观察；双 Session 旧草稿不覆盖；秘密提交立即清空，失败不进入表单/Store/API 缓存；main 的自动刷新在离线时停止，离线写保护不排队 | 认证 Chrome DOM、截图、网络/控制台、local/session storage 和离线恢复后的请求计数 |
| 秘密边界 | sentinel 仅由 0700 fixture 中 0600 文件读取；不得出现在 argv、报告、日志、截图、API 缓存、Observe、Journal、RunAttempt 或 Worker 外泄输出 | 脚本只输出 `sentinel_leaked` bool；受限响应扫描、受控日志检查、文件权限检查 |

## 固定边界与前置条件

- fixture 必须新建在显式绝对路径，目录名为 `openagentx-n2-*`、权限精确为 0700；脚本不删除任何路径。口令和 sentinel 文件须为非链接的精确 0600 常规文件，父目录须为非链接的精确 0700 目录。agent 定义仅经正式 `init` 和 `agent apply` 载入，绝不直接写 SQLite。
- `OAX_N2_BASE_URL` 仅接受无 credentials/path/query/fragment 的 loopback `http(s)` origin，防止 fixture 口令被环境变量带到外部或生产端点。loopback 只是最小路由约束，不能证明该端点不是本机生产服务；正式批次还须核对最终二进制 SHA-256、启动 PID/端口、隔离 DB/config/secret fixture 和 Worker identity。
- 认证 Control API 是唯一配置写入口；Worker 通过已认证的 register/heartbeat/pull/ack API 消费真实 network work。测试 harness 不伪造成功 ACK，也不将 HTTP 200、test work 领取或 `pending` 推断为应用成功。
- Cancel 相关输入若覆盖到组合生命周期，只能先经 Worker `mailbox/claim` 与 `begin-attempt` 建立 active Run，再走正式 Control cancel；不得对 queued Task 直接宣称 Run/网络快照结论。
- `verify-n2.mjs named-workflow` 只在 N2 freeze 后，以 `OAX_N2_ALLOW_EXECUTION=1` 显式运行。它等待真实 Worker 在一分钟检查点完成 test/apply；超时是失败，不能由脚本代发成功 ACK。
- `inherit/direct` 必须经独立的 mode candidate/test，测试完成仍不得改变 binding 或 Backend health；只有 `/mode/publish` 可创建 pending，真实 Worker ACK 后才成为 applied。publish receipt 必须先证明 `state=pending` 和下一 binding revision；后续 Observe 可合法地已是 `pending` 或已完成 `applied`，但 `applied` 必须核对完整 ACK identity。不得把 named profile 成功外推到这两种模式。
- 正式网络端点必须是隔离协议 fixture 提供的真实 SOCKS5 认证端点；不得使用外部代理、生产代理或伪造 ACK。named 用固定公开合成用户名 `n2-fixture-socks-user` 与仅从 0600 文件读取的密码；fixture 必须逐字节核对 SOCKS5 用户名/密码认证帧，禁止依赖空用户名的非标准兼容行为。隔离进程、真实 Worker、隔离 DB/config/secret store 与 fixture endpoint 的 PID、端口、版本和身份都须进入证据包。

## 正式批次命令预案

1. 冻结并记录基线：`git rev-parse HEAD`、三份 ADR 的 `sha256sum`、`git diff --check`、最终源码树指纹；在独立目录执行 `node --check docs/reports/validation/verify-n2.mjs`，随后构建隔离二进制并记录 SHA-256。
2. 使用最终二进制和隔离数据库/端口/Unix socket/secret store 启动 daemon；以正式 `init`、`agent apply` 创建 fixture，并让真实已登记 wrapper/Worker 持续在线。记录 wrapper/CLI `--version`、`--help` 契约、argv/stdin/stdout/stderr/exit/timeout/model/effort/permission/session 的实测边界。
3. 在单一独立批次执行 `go test -count=1 ./...`、`go vet ./...`、`go build ./...`，再按实际受影响包运行一次关键 `go test -race -count=1`；失败交 N2 实现者集中修复，freeze 后仅重跑受影响项目。
4. 执行 `OAX_N2_FIXTURE_DIR=/tmp/openagentx-n2-<id> node docs/reports/validation/verify-n2.mjs prepare`，严格从 fixture 0600 文件供给认证和网络 sentinel；完成正式登录后运行 `named-workflow`、`mode-workflow` 与 API 权限/CAS/幂等/旧 generation/回退/import 用例。`mode-workflow` 以 `OAX_N2_MODE=inherit` 或 `direct` 分别运行；每次网络/Worker 长操作按一分钟检查点观察。每个模式在三视口均核对固定 layer/state：`network_effect`、`model_call` 必须诚实显示 `not_verified/NOT_VERIFIED`，这与 N2 配置测试成功可同时成立。
5. 执行最终 Web build/PWA；按真实认证 Chrome 在三视口进行配置、双 Session、SSE/后台刷新、离线保护和秘密清除检查。浏览器批次开始前读取 `/home/sky/.codex/skills/agent-browser/SKILL.md`，使用 `/usr/bin/google-chrome` 显式 executable path，且与前一真实浏览器批次间隔至少五分钟。

## 预期脚本接口

- `prepare`：新建私有 fixture、agent YAML、workspace 与仅本 fixture 可读的随机网络 sentinel；不启动任何进程。
- `unauthenticated-read-check`：freeze 后显式检查 Observe 网络概览拒绝无认证读取。
- `named-workflow`：以正式登录创建 `only_socks5` 草稿，验证 create 幂等，编辑、用固定公开合成 SOCKS5 用户名 `n2-fixture-socks-user` 和私有文件密码替换秘密、发起指定 target test；从 test receipt 取得 `test_id`，只等待该测试对应的真实 ready，遇到 `failed`/`stale` 立即停止。显式 publish/bind 后，必须核对 desired profile/version、target test/runtime evidence 和 Worker/generation/mode/profile/version/binding revision 全部匹配的 `applied`。控制台只输出布尔/状态摘要，不输出秘密、cookie、CSRF、receipt、诊断文本或完整响应。
- `mode-workflow`：以 `inherit` 或 `direct` 创建 mode test，确认测试前后 binding 与 target Backend health 不变；只在真实 Worker test 成功后显式 publish。publish receipt 必须为 `pending` 且 binding revision 为测试时 revision + 1；随后的 Observe 允许 `pending` 或合法 `applied`，再核对身份、generation、mode、policy version、binding revision 全部匹配的真实 applied ACK。出现 `failed` 或 `stale` 即停止等待。

## 幂等与冲突执行矩阵

正式批次对下列十个网络命令逐一固定原请求和 idempotency key。每一项先记录首次 receipt 与相关 overview 状态/版本，再以相同 body 重放并要求 receipt 语义一致、HTTP 成功、版本和 work/test/import 数量不增加；随后以同 key 改变一个非秘密字段并要求 HTTP 409、原状态和版本不变。秘密替换的原值只从 0600 文件读取，不出现在命令行、报告或控制台；冲突只改变 `expected_version` 等非秘密字段。除 create 外，这些仍是待补的可执行 helper，不是本脚本已经覆盖的结果。

| 命令 | 同 key 同请求成功重放 | 同 key 异请求冲突与不变断言 |
|---|---|---|
| create | profile receipt、content v1 仅一份 | `host` 变化；profile/version 不增加 |
| edit | 新 content/version 仅一份 | `direct_ips` 变化；head/content 不变 |
| replace secret | receipt/secret version 仅一份 | `expected_version` 变化；head/secret 引用不变 |
| profile test | `test_id`/test work 仅一份 | `backend_id` 变化；test/head 不变 |
| profile publish | publication 仅一份 | `expected_version` 变化；published head 不变 |
| bind | binding revision/apply work 仅一份 | `profile_version` 变化；binding 不变 |
| rollback | candidate/test work 仅一份 | `target_content_version` 变化；head/binding 不变 |
| mode test | `test_id`/mode work 仅一份 | `mode` 变化；mode test/binding 不变 |
| mode publish | binding revision/apply work 仅一份 | `test_id` 变化；binding 不变 |
| import | import receipt/work 与导入内容仅一份 | `profile_id` 变化；import/profile 内容不变 |

## 未覆盖与阻断

- `inherit/direct` API 已锁定，但本轮未执行其候选、测试、显式发布或 ACK；它们仍不能标绿。
- 本轮未启动 daemon/Worker，不存在真实 Runtime、native 黑白名单、wrapper argv、连接测试、Task 副作用、Worker 常驻或下一任务领取证据。
- 本轮未执行浏览器；当前源码已将 NetworkSettings 接到 main 的 Observe 数据与 Task 导航，但尚无三视口、离线、SSE/刷新、认证或秘密清除的真实浏览器证据，不能声明 UI 通过。
- 旧 Worker generation 的安全优先级、双 Session 冲突、回退、一次性导入、权限矩阵和密码泄露扫描均只有待执行断言，尚无结果。
- 十类幂等/冲突矩阵中，当前仅 create 有脚本内同 key 同请求重放；其余九类的可执行 helper 尚未准备，因此不能将矩阵视为已执行或已自动化覆盖。
