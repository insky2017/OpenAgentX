# Buzz 有界测试与测试层次审计

- 固定源码：`block/buzz@781d39510cf23cfe224e8f521ae06a23377e06de`；本轮在 2026-09-26 10:22–10:45（Asia/Shanghai）测试预算内完成。
- 实际通过：独立 `ifc-core` 的 **5 个原单元/属性测试 + 2 个原 doctest**；桌面 **4 个原测试文件、29 项原逻辑测试**。不存在本轮自编测试或自造依赖替身，原loader已有fixture/stub沿用；**源码字符串断言 0 项**。数字只描述分层运行结果，不累计为完整业务验收。
- 直接支持：读者集合信息流限制、未知/跨 universe 输入 fail closed；取消回执的 requestId/channel/type 关联与 unconfirmed 超时；Agent+relay配对、有限reconcile重试；编辑/转发消息时保留有效接收者、丢弃旧接收者与未送达的自动地址元数据。
- 首次桌面批次 21 项通过，消息语义文件因缺 `nostr-tools` 收集失败；补上 upstream lock 的精确 `2.23.12` 后只复验该文件，8 项通过。首次失败原样保留，未重复运行已通过项。
- 上游 workspace/官方工具链未复现：Rust使用1.89.0，仓库pin为1.95.0；独立crate只展开workspace元数据并沿用原源和锁定依赖。桌面使用独立Node24.14.0与原loader，未安装/启动Tauri、React整个平台、relay或真实Agent。
- 已保存命令、完整独立manifest/锁、源码与二进制hash、源码复制一致性、原日志和进程检查；原副本tracked源码干净。Rust ACP/DB/授权接线、真实取消效果、真实Provider、浏览器/原生App及生产业务均未验证。

## 运行条件与源码完整性

见 [环境/哈希](buzz-test-environment.json)、[命令](buzz-check-commands.md)、[Rust独立manifest](buzz-test-ifc-Cargo.toml)、[Rust锁](buzz-test-ifc-Cargo.lock)、[Node锁](buzz-test-node-package-lock.json)。原副本只读，全部产物在 `/tmp/oax-buzz-runner-20260926`。

Rust主workspace锁定registry包共1027个，现有全局cache仅37个精确版本，无target；不启动主workspace/ACP冷构建。选择 `ifc-core` 是因为它运行时仅依赖std、原测试只需要proptest，并直接涉及信息流边界。本轮给此独立子批次300秒硬上限；实际构建24.83秒即完成。

复制 `crates/ifc-core/src/lib.rs` 后保持字节相同；临时Cargo.toml把workspace的version/edition/rust-version/license/repository展开，并将原锁proptest版本明确为 `=1.11.0`。复制完整upstream Cargo.lock作为起点，Cargo在临时目录将其裁剪为独立crate依赖图；结果锁中没有任何不在upstream锁里的包版本。不是重新实现IFC算法，也没有删除测试。对 `crates/` 的 `ifc_core|ifc-core` 搜索只命中该crate自身manifest/lib；根Cargo.toml只是workspace成员声明，未发现Agent/Relay生产crate消费它。因此这是独立通用原语验证，**不能证明Buzz已将该信息流模型接入生产授权链**。实际stable rustc/cargo均1.89.0，满足manifest MSRV1.88.0，但与官方pin1.95.0不同；不声称官方环境通过。Cargo使用临时 `CARGO_HOME`、`CARGO_TARGET_DIR`，未升级系统工具链或运行Hermit安装。

桌面复制 `desktop/src`、原package.json、`test-loader.mjs` 与 `test-loader-hooks.mjs`，共2491个文件逐个与原副本字节比较一致。原loader保留extensionless/`@/`解析及现有asset/emoji stub逻辑，选中的测试仅导入纯业务函数，没有本轮增加的stub。独立安装typescript6.0.3、nostr-tools2.23.12与Node24.14.0，前两者匹配upstream锁对应版本。Node24大版本相同，但仓库Windows CI声明24.14.1，本轮不冒称完整pin一致。未运行全项目pnpm install或完整桌面typecheck/build。

所有执行命令使用 `env -i`，测试不继承用户Provider、Relay、DB或Agent身份环境。依赖安装禁lifecycle。没有运行默认 `just setup/test/ci/reset`，也没有使用可能复用用户Desktop数据库的默认Docker栈。

## 实际执行矩阵

| 原测试/模块 | 通过数 | 验证到的边界 | 不等价于 |
|---|---:|---|---|
| `crates/ifc-core/src/lib.rs` unit/property | 5 | reader-set流向/组合律、吸收律、FlowState累积限制、unknown不能遗忘、跨universe拒绝 | Relay成员授权/NIP-PL生产接线、工具权限、真实数据泄漏验收 |
| 同文件doctest | 2 | 公开示例可执行；FlowState禁止clone的`compile_fail`约束 | 真实跨进程权限隔离 |
| `desktop/src/features/agents/lib/cancelTurnOutcome.test.mjs` | 8 | 回执必须同requestId/channel/type；错误/过时/缺ID/未来状态不能确认；hung send能超时unconfirmed；先到回执/迟到transport rejection安全收敛；清理订阅与timer | 远端进程已停止、业务副作用已停止；`sent`只保持上游词义 |
| `desktop/src/features/agents/managedAgentReconciliationPlan.test.mjs` | 6 | 5s/30s/120s后停止重试；跳过in-flight；整批/单relay失败分类；无row可完成计划 | 真正重启harness、持久化恢复、跨崩溃幂等 |
| `desktop/src/features/agents/managedAgentRuntimeStatus.test.mjs` | 7 | 同pubkey不同relay不折叠；pair key无边界碰撞；relay规范化；localSetup/lifecycle展示 | Agent正在执行、真实在线、已安装状态或启动成功 |
| `desktop/src/features/messages/lib/sendToChannelSemantics.test.mjs` | 8 | 原始/编辑/转发接收者语义，空snapshot去旧接收者，只保留已送达自动地址 | Relay真正送达、exactly-once、业务结果可信 |

[Rust原日志](buzz-tests-ifc-core.log)：5 unit/property通过，2 doctest通过，退出码0；原属性测试的生成case不另计为测试数量。日志中的 `no method named clone` 是 `compile_fail` doctest的**预期诊断**，对应doctest通过，不能当作未解释的构建失败。

[桌面首次日志](buzz-tests-desktop.log)：21 pass、1 failed file（缺依赖导致收集失败），退出码1，809.523982 ms；[唯一复验](buzz-tests-desktop-recheck.log)：消息语义8 pass、0 fail/skip，退出码0，881.88681 ms。缺依赖问题只发生一次，随后安装精确真实依赖并原样复验，没有替身、删测、改断言或只报绿色尾部。

本轮没有自编行为探针。所有桌面测试通过实际import原生产函数调用，而非读源码字符串检查是否包含某词；没有计入菜单/截图/快照总数。选择的runtime status测试仍然只是展示/配对逻辑，因此其7项单独列层次，不升格为核心执行验收。

## CI与现有测试的层次核查

- `TESTING.md:3-18` 区分无需基础设施的unit、会自动启动/复用Postgres+Redis的integration与默认 `#[ignore]` 的 `buzz-test-client` E2E；同文档提示默认开发服务可能与用户Desktop共用数据库。因此本轮不运行默认integration脚本，也未连接现有服务。
- `.github/workflows/_ci-rust.yml:63-68` 安装cargo-nextest并跑 `just test-unit`；完整Rustworkspace测试的通过状态不是本轮证据。`desktop/package.json` 使用原Node loader跑 `.test.mjs`，另用jsdom setup跑组件测试；本轮只选4个文件，不替代完整门禁。
- `.github/workflows/_ci-desktop.yml:168-170` 构建E2E后跑4分片Chromium smoke；`AGENTS.md` 与 `desktop/playwright.config.ts` 明确区分mock bridge smoke和relay-backed integration。真实浏览器使用mock bridge仍不能代表原生Tauri功能或真实Agent执行。
- `.github/workflows/_ci-relay.yml:134-207` 有Postgres16专用测试与隔离DB wrapper；`:209-363` 启动Postgres/Redis/MinIO、应用schema、启动relay、seed数据后跑2分片desktop integration；`:404-598` 另有真实relay/backend门禁，显式启用部分ignored E2E。不能笼统说“所有E2E默认跳过所以CI没测”，也不能把CI入口存在当本轮通过。
- `crates/buzz-acp/TESTING.md` 的真实Pi adapter测试需要 `BUZZ_TEST_PI_ACP` 且 `--ignored`；git runtime测试也显式ignored，需要built binaries。Buzz Agent分支用确定性本地OpenAI-compatible响应驱动真实MCP shell，Goose分支需已配置真实Provider；本轮均未执行。
- `crates/buzz-acp/src/relay/recovery_tests.rs:1` 明说synthetic WebSocket fixture、无relay/proxy/真实agent；涵盖overflow/capacity/recovery watermark与bounded write。`recovery_wake_tests.rs` 使用本地socket fixture核查headroom、unsubscribe/shutdown/transport loss取消等待。这些有价值的生产逻辑测试源码本轮只读，不计pass。
- `crates/buzz-acp/src/queue.rs` 有dedup、requeue/dead-letter、retry throttle、cancel carryover、恢复withheld等原测试；本轮不编译ACP，不对内存队列跨崩溃恢复或副作用安全重试作通过结论。

## 限制与收口

- 源码/CI入口检查不等于执行证据；未查询托管CI历史结果，未运行TLA+/Tamarin模型检查，不将愿景文档中的proof字样认作本轮已证明。
- 未覆盖完整workspace构建/lint/typecheck、Nostr签名/多租户权限完整链、Postgres事务与Redis投递、真实取消/进程树、Provider终态和工具效果、队列崩溃恢复、harness生命周期、真实桌面/浏览器/手机及长期资源行为。
- 无产品修改与修复commit；研究runner缺依赖已经补齐，不能写成Buzz产品缺陷修复。首次失败证据保留。
- [结束后进程检查](buzz-test-process-check.json) 没有临时runner可执行进程残留；没有启动relay、daemon、浏览器或Docker服务。原源码 `git status --short` 与 `git diff --exit-code` 均干净。
- 下一步：主报告以29项桌面局部逻辑、5项IFC单元/属性、2项doctest分层引用；可靠取消可借鉴回执关联/unconfirmed边界，权限只借鉴信息流原理，真实业务闭环另行隔离验收。本批不扩测。
