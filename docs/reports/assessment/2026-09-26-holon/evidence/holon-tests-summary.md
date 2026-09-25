# Holon 有界测试与测试层次审计

- 固定源码 `holon-run/holon@7c0ebbdfbf2f38adf41968051ed3440bc5ddb774`，执行日期 2026-09-26（Asia/Shanghai）。
- 本轮实际执行 Conversation SDK 的 4 个原有测试文件：**53 pass、0 fail、0 skip**，约 0.81 s；TypeScript 构建通过。源文件逐一 SHA-256 对照原仓库，无内容改动、无自编行为探针。
- 证据支持客户端会话投影的待处理顺序/来源、旧代响应拒绝、终态防复活、完整批次检查点、断线恢复、撤权缓存清理；**不支持推导 Rust 持久 Agent 身份、消息持久投递、真正停止或业务结果已经验收**。
- Rust 冷构建不在本轮预算内：470 个锁定 registry 包只有 33 个精确版本缓存、437 个缺失，没有 target 工件；不为局部研究下载/构建整个原生依赖链。现有工具链为 rustc/cargo 1.89。没有启动 cargo test，故这不是 Rust 测试失败，也不是 Rust 测试通过。
- Holon 确实有真实 Rust HTTP/SSE、真实 daemon/browser、Docker scheduler 和 live Provider 验证入口；本轮只核查这些入口源码及触发条件，未运行、未查询历史 CI 通过状态，不能借用其存在作为本次 E2E 证据。
- 原仓库 tracked 文件干净；安装和编译仅落独立临时目录，禁 lifecycle，未运行默认 make ci、HTTP skill integration、已有 Holon 服务或真实 Provider；结束后无匹配临时 Node 进程。

## 环境、复制与范围

见 [环境/源码哈希](holon-test-environment.json)、[依赖锁](holon-test-dependency-lock.json)、[命令](holon-check-commands.md)。原副本 `/tmp/oax-holon-assessment-20260926` 只读；将 `packages/conversation-sdk` 原样复制到 `/tmp/oax-holon-runner-20260926/sdk`，全部 tracked SDK 文件逐个比较一致，编译只在复制树生成 `dist`。

使用独立 Node **24.14.0**（满足 SDK `engines.node >=24` 和 CI Node 24 大版本）、TypeScript **5.9.3**（与 SDK package.json 精确版本一致）；npm **10.8.2** 以 `--ignore-scripts --no-audit --no-fund --save-exact` 仅安装两个包。无系统 Node 替换、无全 Web 平台安装、无项目源码/manifest 修改。保存的是本轮独立 runner 的 npm lock；不是声称执行了仓库全部 npm ci。

运行编译与测试使用 `env -i PATH=/usr/bin:/bin`，无继承 Provider/账号/DB 环境、无 HOME 重定义。原测试通过假客户端、注入 fetch、人工 summary/batch/detail 与内存 SSE 片段驱动原 SDK，实现与原测试未替换。执行 `node --test --test-timeout=60000` 比原 npm test 多一个有界超时上限；没有筛选测试名、跳过测试或重试。

## 实际结果

| 原测试文件 | 数量 | 支持的局部判断 | 不覆盖 |
|---|---:|---|---|
| `packages/conversation-sdk/test/controller.test.mjs` | 27 | bootstrap、串行重连/退避、checkpoint resume、reset重新取快照、single-flight历史/brief、暂停释放stream、缓存撤权与迟到结果丢弃 | 真实服务重启、网络设备、持久 DB、服务端授权与任务停止 |
| `packages/conversation-sdk/test/state.test.mjs` | 11 | generation/旧revision拒绝、终态不可被迟到detail复活、batch原子应用、边界超限不推进checkpoint、pending按到达顺序 | Rust事务原子性、消息恰好一次、执行副作用 |
| `packages/conversation-sdk/test/decode-client.test.mjs` | 11 | 响应边界/unsafe u64拒绝、capability fail closed、显式pending来源/时间/发送时名称、interrupted无brief解码 | Agent canonical identity、权限授予、Provider终态真实性 |
| `packages/conversation-sdk/test/sse.test.mjs` | 4 | chunked/multiline SSE、EOF残缺事件丢弃、完整batch才给checkpoint、checkpoint错配拒绝 | 真实 HTTP/SSE 服务器、代理断链、浏览器 |

[测试原输出](holon-tests-sdk.log)：53 tests、53 pass、fail/cancelled/skipped/todo 均 0，`duration_ms 807.845964`。[构建日志](holon-tests-sdk-build.log) 为空且实际退出码 0；类型检查/emit 由原 tsconfig 驱动。依赖安装与两个执行步骤均首次成功，本轮没有失败后重试。

这些测试中的 `identity` 是会话协议的作用域/代次标识，不应混同为长期 Agent 的持久身份和权限。`dispose/pause` 停止的是客户端 stream/定时器，不证明服务端 Agent 或外部进程被停止。`terminal outcome` 是解码/投影对象，不是独立核实过的业务完成事实。

## Rust 主路径为何不执行

主包 `Cargo.toml` 引入 SQLite、Tokio、wreq/btls 原生链以及 Linux jemalloc；`Cargo.lock` 含 btls-sys→bindgen/cmake。只筛选一个 Rust 测试仍需编译主 lib，并不天然轻量。预检发现锁定 registry 包 470 个、精确缓存 33 个、缺失 437 个，无 target，故按主代理预算选择直接相关的 SDK 投影测试，不启动完整 Rust 冷构建或安装系统工具链。

需修正一个容易误判的前提：`src/http/mod.rs:184-190` 有 `#[allow_missing = true]`，缺 `web-gui/app/dist` 会嵌入空资源，**不是编译硬阻断**。不跑 Rust 的原因是冷依赖/原生构建成本及范围，而不是缺少前端目录。

独立 decision-core crate 主要是决策 Provider 类型契约，不覆盖本轮长期身份、投递与结果责任；没有为了增加通过数再执行它。本轮也没有用抄写 Rust 状态机或替身实现伪装核心测试。

## 与研究重点相关但本轮未执行的原测试

- `src/runtime_db/agent_message_delivery.rs:843-908` 的 `accepted_delivery_is_idempotent_and_survives_restart` 使用临时 RuntimeDb，检查重复 admission 复用 delivery_id、queue只有一项、保留principal/caller/turn/task/work上下文，并关闭后重开 SQLite 检查 receipt。这是有意义的持久化组件测试源码；不等于真实 daemon 重启或收件 Agent 完成业务。
- 同文件 `:1028-1044` 的 `idempotency_conflict_is_typed_and_does_not_admit_twice` 检查同键异体的typed error和queue数量；`:1081` 起检查投递/删除 fence 线性化。仅源码核对，未执行。
- `src/runtime_db/agent_relations/tests.rs` 使用人工 identity/task record 与 tempdir，覆盖 canonical关系/持久性/独立与派生投影；`tests/runtime_subagents.rs`、`tests/runtime_waiting_and_reactivation.rs` 通过 `tests/support/runtime_subagents.rs`、`runtime_waiting.rs` 的 StubProvider、DelayedTextProvider、ToolUsingProvider等专用fixture驱动 RuntimeHost。它们比纯UI投影更接近真实宿主链，但本轮没有编译或运行。
- complete_work_item/任务结果 settlement 的实际业务边界由主源码报告核对；SDK绿灯不能替代这些路径。未运行 issue #2903 指向的 HTTP skill integration，也未访问用户 `~/.agents` 内容。

## CI 与真实链路分层

| 仓库入口 | 实际组织与触发 | 证据边界 |
|---|---|---|
| `.github/workflows/ci.yml` 的 Rust shard/concurrent | 按路径变化/full sweep；lib/control/cli/misc shard、并发生命周期及重复；有缓存、长超时 | 仓库有系统性 Rust 门禁，本轮未运行或验证托管CI状态 |
| `Makefile:55-64` / `.github/workflows/ci.yml:236-264` Conversation SDK | Node 24 npm test 后明确执行 `cargo test --test conversation_sdk_e2e ... -- --ignored` | 不能把CI说成只有fake SDK测试；本轮只执行其Node测试部分 |
| `tests/conversation_sdk_e2e.rs:45-115` | `#[ignore]` 默认cargo跳过，专用make/CI显式启用；真实 RuntimeHost + axum 临时HTTP/SSE + Node runner | Provider 是 `StubProvider("unused")`、会话人工seed，真实协议链≠真实模型业务 |
| `.github/workflows/web-e2e.yml:29-79` / `playwright.config.ts:30-44` | Chromium P0 在非定时触发下运行，启动 `e2e/fixture-server.mjs` | 真实浏览器+fixture后端，不是Holon服务端全链 |
| `.github/workflows/web-e2e.yml:81-126` / `e2e/real-daemon/daemon-fixture.ts:56-82` | real-daemon定时或手动运行；临时daemon/provider fixture | 真实daemon与browser；默认本地stub Provider快速返回500，部分spec可另行覆盖，不自动等于真实Provider |
| `.github/workflows/ci.yml` scheduler-e2e-required | 相关Rust/Docker或full sweep时 Docker内 deterministic scheduler profile，并检查三份report状态 | 有真实进程/容器链，仍需区分fixture provider与live场景；本轮未运行 |
| CI optional live canary / nightly / release-e2e | PR live需 `e2e-scheduler` label且 `continue-on-error`；nightly/release有受控模型凭据、模型与证据报告配置 | 有真实Provider验证入口，但依赖凭据/触发条件；本轮未运行，不能引用为此次通过 |

`tests/conversation_sdk_e2e.rs` 的 ignore 文案提及 `make conversation-sdk-e2e`，当前 Makefile 实际入口是 `conversation-sdk-ci`；报告以现行 Makefile 接线为准。

## 收口与后续条件

- 无失败的执行测试被隐去；Rust/浏览器/Provider未执行明确保留。没有产品修复，修复commit不适用。
- 未覆盖完整构建/lint/fmt/主Rust测试、跨domain持久投递、并发安全授权、任务结果 settlement/业务效果、真实取消/进程清理、daemon重启恢复、真实浏览器、真实Provider、正式数据与长时间空闲资源行为。
- [结束后进程检查](holon-test-process-check.json) 未发现临时Node可执行文件的剩余进程；测试未启动服务监听、Holon daemon、浏览器或Docker。本次范围不修改用户HOME或既有服务。
- 下一步：主报告引用53项客户端证据，同时引用但不冒领Rust/CI源码覆盖；若以后要检验持久domain Agent闭环，应单独准备可复现Rust构建和隔离daemon/provider/DB预算，本批不扩展。
