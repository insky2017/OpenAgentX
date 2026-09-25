# FenixAgent 有界测试与测试层次审计

- 固定源码：`HuangPuStar/FenixAgent@da5eb543bf17aa0c772eebbf0961e4dc2daac0e8`；执行日期 2026-09-26（Asia/Shanghai）。
- 9 个上游原测试文件合计 **104 项通过**，但使用独立 runner 和隔离配置，不是上游完整依赖/CI 原样运行。另有 **6 项本轮自编行为探针**确认 scheduler 完成语义反例，两类数字不得混为上游覆盖。
- Chat 通道的目标会话取消、`cancelling → cancelled/interrupted`、迟到终态幂等、commandId 去重、权限决议 CAS 和快照 CAS 有局部执行证据；假 Relay/Y.Doc 测试不等于真实 Agent 已停止。
- 原 scheduler executor 对 `cancelled`、`max_tokens`、任意 truthy `error` stopReason 及无终态的空事件流均返回 `success`；40 ms 会话打开延迟超出配置 5 ms 仍成功。探针直接加载原实现，替换宿主会话依赖，未调用 Provider。
- CI 有大量单元/组件/包测试和手动开启的真实 Runtime integration 入口；默认配置中后者跳过。不能从测试数量或文件名 “e2e” 推导全产品、真实浏览器、真实 Provider、持久数据库或副作用核验通过。
- 本轮无产品修复、无服务部署、无账号读取、无正式 DB 访问；第三方 tracked 源码 `git diff --exit-code` 与 `git status --short` 干净。下一步由主报告引用证据和限制，不扩装完整平台。

## 环境与隔离方式

见 [环境清单](fenix-test-environment.json)、[依赖锁](fenix-test-dependency-lock.json)、[命令](fenix-check-commands.md)。Bun 1.3.14 与 CI 声明版本相同；原环境没有 Bun，使用 npm 官方包 `@oven/bun-linux-x64@1.3.14`，`--ignore-scripts` 安装在独立 `/tmp/oax-fenix-runner-20260926`。没有运行 Fenix installer、`bun install`、`prepare`、`precheck`、主服务或 Docker Compose。

上游 `bunfig.toml:1-2` preload `setup-globals.ts` 与 `setup-mocks.ts`，后者导入宿主模块且替换 auth、DB、config、resource permission、部分 Runtime/transport 等依赖。为了避免装全平台，本轮从独立 cwd 使用 `--config` 指定无 preload 的配置，原包测试文件与实现文件保持不变。全局 preload 差异属于实质测试条件，不能将本轮称为完整上游套件原样执行。scheduler 子批次还额外使用本轮编写的两个模块替身：`agent-chat-service` 和 `@fenix/logger`。

执行时 `env -i`，仅提供 `/usr/bin:/bin` PATH、`NODE_ENV=test` 与独立 `LOG_DIR`。未继承账号、Provider、DB 环境。外部依赖是必要子集；没有按 upstream `bun.lock` 安装完整依赖。特别是本轮预备的 `ioredis@5.10.1` 与 upstream lock 的 `5.11.1` 不同，因此不声称锁文件等价；本轮未执行真实 Redis 行为。已保存 runner 的完整 npm lock。

## 执行结果

| 上游测试文件 | 通过数 | 实际层次与边界 |
|---|---:|---|
| `packages/acp-link/src/__tests__/acp-dispatcher-cancel.test.ts` | 3 | 原 Dispatcher + fake ClientSideConnection；核对 sessionId 路由、fallback、无连接响应 |
| `packages/chat-channel/src/channel/command-id-dedup.test.ts` | 11 | 原 Coordinator + 注入执行器；进程内 commandId 去重/串行/队列上限/CAS 版本；dispose 后允许重新执行 |
| `packages/chat-channel/src/channel/permission-cas.test.ts` | 12 | 原 SessionChannel/聚合器 + Y.Doc + fake relay；重复决议、过期、deny、终态清理；不是用户 RBAC |
| `packages/chat-channel/src/channel/session-channel-action.test.ts` | 23 | 原 Action→Ack→Y.Doc + fake relay；取消确认、20 ms 缩短测试超时收敛 interrupted、迟到终态忽略、会话绑定守卫 |
| `packages/chat-channel/src/__tests__/snapshot-cas-isolation.test.ts` | 3 | 注入快照持久接口的并发控制测试；不等于真实 Postgres 事务验收 |
| `packages/workflow-engine/src/__tests__/recovery/snapshot-recovery.test.ts` | 18 | 内存 storage + MockNodeExecutor 为主，另含真实临时 `sleep 60` 子进程清理测试；不证明远程 Agent 已停或重跑副作用安全 |
| `packages/workflow-engine/src/__tests__/executor/agent-executor.test.ts` | 25 | 原 Workflow AgentExecutor + FakeTransport；重试/结果/资源清理局部验证，不同于 scheduler agent executor |
| `src/__tests__/task-timeout-instanceof-error.test.ts` | 7 | 上游复制 http-executor 内联判断的纯函数测试，未调用实际 HTTP executor |
| `src/__tests__/agent-executor.test.ts` | 2 | 原 scheduler 入口依赖注入测试，仅验证 cron/manual 映射 `scheduled`；本轮额外 preload 隔离宿主导入 |
| 本轮 `fenix-test-scheduler-probe.test.ts` | 6 | 自编行为探针，不计上游测试；对原 scheduler 做反例观测 |

主子批次：[102 pass / 0 fail / 291 expect / 8 files](fenix-tests-primary-recheck.log)，10.16 s。scheduler 子批次：[8 pass / 0 fail / 14 expect / 2 files](fenix-tests-scheduler-probe.log)，85 ms，其中只有 2 项来自上游文件、6 项是本轮探针。合计 110 项通过只是不同隔离层次的运行计数，不表示 110 项业务验收通过。

首次准备时独立配置 `[test] preload = []` 被 Bun 1.3.14 拒绝，错误为 `Expected preload to be an array`；[失败日志](fenix-tests-primary.log) 已保留。将配置改为仅含注释、没有 preload 字段后只复验一次成功；此失败属于研究 runner 配置，不是 Fenix 产品测试失败。无其它失败测试被隐藏。

## 本轮行为反例

探针源码：[测试](fenix-test-scheduler-probe.test.ts)、[隔离 preload](fenix-test-scheduler-preload.ts)。输入全部 synthetic，logger 是无副作用替身，实际 executor 从固定源码绝对路径直接加载，未抄写实现。

| 合成事件/条件 | 原实现观察值 | 能得出的结论 |
|---|---|---|
| `stopReason=end_turn` | `success`, 空摘要 | 正向控制输入 |
| `stopReason=cancelled` | `success`, 空摘要 | scheduler 没有把已取消区分为非成功 |
| `stopReason=max_tokens` | `success`, 空摘要 | 达到生成上限同样被当作成功 |
| `stopReason=error` | `success`, 空摘要 | 任意 truthy stopReason 都触发 break；该字符串是防御性合成输入，不声称 ACP 定义此枚举 |
| 事件流为空且自然结束 | `success`, 空摘要 | 未要求存在明确完成终态，存在误报成功入口 |
| 配置 timeout 5 ms，会话打开延迟 40 ms | `success`, `duration=42` ms | timeout 从 openAgentSession/prompt 后才设置，不覆盖打开阶段 |

直接实现依据：`src/services/scheduler/agent-executor.ts:71-103`；关闭资源 `finally:117-123` 被调用不等于 Agent 副作用停止已验证。报告应将问题限于 scheduler 链，不能据此抹掉 Chat 通道更谨慎的取消状态机。

恢复边界：`packages/workflow-engine/src/recovery/snapshot-recovery.ts:241-245` 明确无法可靠检测远程 Agent，直接记 `node.cancelled`；`:271-275` 在有重试配置时置为 `PENDING`。现有测试证明状态策略，未证明旧远程执行已经消失、旧副作用可忽略或安全重放。Shell 真实子进程测试在 `snapshot-recovery.test.ts:488-565`；它允许进程启动后已退出直接 `return`，并吞掉 `proc.exited` 拒绝，还在末尾兜底手动 kill，因而不能根据标题宣称对 SIGKILL 路径或独立进程身份做了严格验收。

## CI 与端到端层次

- `.github/workflows/ci.yml:20-61` 声明 Bun 1.3.14、frozen install、格式/lint/backend+frontend typecheck，并分三个命令运行 backend/package/frontend tests；本轮未运行这些完整门禁，也未查询托管 CI 的实际运行结果。`scripts/ci.ts` 的 precheck 会格式/import 自动写文件，测试命令只列 `src/__tests__/`，不能与 GitHub 三类测试混同；本轮未执行 precheck。
- `bunfig.toml:1-2` 与 `src/test-utils/setup-mocks.ts:142-151,188-220` 表明默认测试替换 Better Auth、DB 和资源权限仓储。它们支持路由策略/组件行为测试，不证明真实认证协议、数据库事务回滚或外部权限边界。
- `packages/core/integration/core-runtime.integration.test.ts:31-62,215-220`：寻找 `core-runtime.local.json` 或 `.conf.json`，必须 `enabled === true` 才启用；`packages/plugin-opencode/integration/opencode-runtime.integration.test.ts:31-59,231-235` 同类配置缺失/关闭时 `test.skip`。有真实 launch/relay/stop 手工入口不等于默认 CI 在验证真实 Provider；本轮未启用也未读取这些本地配置。
- `packages/opensandbox-cluster/src/__tests__/e2e-flow.test.ts:24-58` 是临时 SQLite + `app.handle` + 本地 `Bun.serve` mock sandbox server，能检验 cluster 路由与 HTTP 转发，不会启动真实 sandbox。`packages/sandbox-provider/src/__tests__/integration-flow.test.ts:24-50` 注入 fake fetch。两个套件本轮均未执行。
- `packages/workflow-engine/src/__tests__/engine/inputs-e2e.test.ts:10-22` 使用内存 storage；后续确实执行本机 shell/python 节点，属于工作流引擎的局部执行链，不包括真实 Provider/前端/持久恢复。本轮未执行。
- `web/src/__tests__/acp-main-session-recovery.test.tsx:1-22,24-91` 是 happy-dom + React render + 人工快照与回调，只验证 effect 发出 load_session，不是浏览器刷新后的真实会话重连。源码清单没有发现 Playwright/Cypress/Puppeteer/WebDriver 配置；不能由此断言项目没有仓库外人工 UI 验证。本轮没有真实浏览器证据。

固定副本清单共 632 个 `.test.ts/.test.tsx` 文件（src 308、web 169、packages 155），仅用于描述测试组织规模，不能当测试用例数、通过数、质量或端到端覆盖率。

## 未覆盖与收口

- 未验证完整构建/lint/typecheck/全套 tests、真实 Provider/CLI 版本契约、真实浏览器与多设备、首次部署/升级、真实 Postgres/Redis、账号与组织权限完整链、远程执行停止/后代进程/副作用核验、幂等记录跨服务重启、恢复后的不重复业务效果。
- 没有给第三方代码打补丁；本轮观察到的 scheduler 反例仍存在于固定 commit。修复 commit 不适用，不能把探针通过写成漏洞已修复。
- 下一步：主报告引用当前局部证据，把可靠取消/当前结果、可信完成、恢复策略分别映射；更强的真实执行验收留待另行授权和隔离部署任务，不在本批扩展。
