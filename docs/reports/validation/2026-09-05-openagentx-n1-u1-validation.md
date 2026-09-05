---
doc_type: validation_report
scope: N1-U1
status: complete
owner: independent-verification
validated_at: 2026-09-05
---

# OpenAgentX N1/U1 独立验证报告

## 冻结基线

- 基线 Git commit：`b0e98ece3335ba43b6cf2b74a9e6a1b2cd1c56e8`（工作树包含本轮已冻结、尚未提交的实现）。
- 冻结实现 diff SHA-256：`c1ae3810fcab116c2702112a706e38500c6a908825f6bb2e69024b1648d7ba73`；文件清单 SHA-256：`bf5a9445c199c48fcc37bb0c1bb348ccd2143788d66867239279f549aed4d73a`。
- ADR-002 SHA-256：`e1e4cb8b2368be2ea17f66fb1ce33c0185c8d4be9484bc63a245494b0d547f1d`；ADR-003 SHA-256：`a538850fdf884a4d7a00ab92646c6a6c9e5374eb91ef35a0c7ffabf7e80ec99c`。二者在验证开始时无工作树 diff。
- 生产 daemon/Worker 与先前浏览器 fixture 未纳入本次验证，均不作为被测构建或 E2E 证据。本次浏览器批次将新建并销毁隔离 daemon、SQLite、TLS 和浏览器上下文。

## 验收矩阵

| 批次 | 正向流程与不变量 | CAS/幂等与竞态 | 失败路径与证据 |
|---|---|---|---|
| N1 | Register 与初始 binding 同一事务；Heartbeat 与回执/Journals 同一事务；仅当前 Worker generation、fencing、revision 的 `applied` binding 可用于后续 Run；Backend 配置故障期间 Worker 保持控制在线，修复后领取既有 queued Task | 重复 ack 不重复 Journal；旧 revision/generation、错误 fencing 不改变 binding；重绑与 StartTurn 不使用旧配置 | 提交故障回滚、过期回执、pending/failed binding、Backend unavailable 后恢复领取任务；记录测试名称和状态事实 |
| U1 | Agent/状态/时间/文本由服务端筛选并稳定 keyset 分页；详情先后加载与实时事件合并后 sequence 连续无重复；URL 选中态、焦点和阅读滚动稳定 | 旧详情请求不能覆盖新选择；重复/重连 SSE 去重且不跳历史；加载旧页期间新增事件仍可见 | 超过单页 Task 和事件、强制 SSE 断开重连、无匹配、权限失败、离线写操作不排队；三视口 DOM/CSP/控制台证据 |

## 执行约束

- 自动化批次只运行一次；失败即记录，不修改业务实现。
- 浏览器写操作只经隔离 fixture 的正式初始化、认证、Observe 与 Control API；不直接写 SQLite，不记录密码、Cookie、CSRF 或其他凭据。
- `agent-browser` 真实 Chromium 将在自动化批次结束后执行；旧 fixture、旧截图和 API CSP 响应不替代本轮指挥台 HTML CSP 与浏览器执行证据。

## 已完成自动化证据

### 最终源码快照

- 应用源码指纹：`aaaa1359dc45e7173a616bd516f02d97dfa4de64d495241079cddfd324fd9d41`。覆盖 `cmd/`、`internal/`、`web/src/`、`go.mod`、`go.sum`、前端 package 文件；排除验证报告、验证辅助脚本和 `web/dist/`。
- 隔离二进制：`/tmp/openagentx-n1-u1-bin`，SHA-256 为 `c3e03e1eb16623cd50fcebd42da6332af6a3579293fe375d52051ff50aaf82a5`，由该源码快照构建。
- 自动化批次开始、最终 N1 race 后及本节写入前的应用源码指纹一致；`git diff --check` 通过。

### 批量命令

| 命令 | 结果 |
|---|---|
| `go test -count=1 ./...` | 通过 |
| `go vet ./...` | 通过 |
| `go build ./...` | 通过 |
| `go test -race -count=1 ./internal/api/panel ./internal/api/workerapi ./internal/controlplane ./internal/persistence/sqlite ./internal/runtime/network ./internal/runtime/spec ./internal/worker` | 通过 |
| 最终 N1 快照：`go test -race -count=1 ./internal/domain ./internal/api/workerapi ./internal/controlplane ./internal/persistence/sqlite ./internal/runtime/spec ./internal/worker` | 通过 |
| `npm --prefix web run build` | 通过 |
| `npm --prefix web run test:pwa` | 通过，限源码安装断言，不等同浏览器 E2E |

### N1 定向边界

- `TestRegisterWorkerRollsBackWhenInitialBindingLoadFails` 覆盖 Register 与初始 binding 装载同事务的回滚。
- `TestHeartbeatNetworkAckIsAtomicRevisionedAndJournaledOnce` 覆盖 heartbeat/回执/Journal 的提交回滚、同 revision 重放幂等、过期 revision 回执不覆盖重绑后的 pending 事实。
- `TestListWorkerBackendsRequiresCurrentGenerationAckAndPersistsExplicitPolicy` 覆盖 pending、未知策略和非当前 generation/revision 的 binding 不可调度；当前已应用版本才注入运行策略。
- `TestUnavailableBackendStillRegistersAndKeepsControlConnection` 覆盖 Backend unavailable 时 Worker 注册为 degraded、心跳继续、既有 queued Task 不被领取；健康恢复后同一 Task 被领取并完成。
- 独立测试 `TestN1ValidationRebindAfterPlanRejectsClaimedBegin` 已在 `go test -race -count=1 ./internal/persistence/sqlite -run '^TestN1ValidationRebindAfterPlanRejectsClaimedBegin$'` 下通过。它以正式 Repository 事务先取得当前已应用 binding 的计划策略，再经 `BindNetworkProfile` CAS 重绑，随后断言 `BeginClaimedRunAttempt` 以 `ErrUnsupportedCapability` 拒绝旧快照，Task 和已领取 Mailbox 均未被部分更新，且没有 RunAttempt 落库。首次临时运行后已删除；按主代理后续指令，等价回归用例现保留于独占文件 `internal/persistence/sqlite/worker_network_plan_validation_test.go`，将在下一冻结批次重跑。

### U1 暂停与隔离 fixture

- 独立审核报告 `2026-09-05-openagentx-n1-u1-independent-review.md` 判定 U1 为 NO-GO：history/live 写回可相互覆盖、重连水位可能跳过事件、RFC3339Nano 文本排序破坏时间筛选与 keyset 分页。
- 因上述 P1，本次未启动旧 U1 的 daemon、TLS 转发或 `agent-browser`；不把已构建结果或旧 browser fixture 当作 E2E 通过。
- 已创建受限隔离目录，仅完成正式 `openagentx init` 和两次 `openagentx agent apply`。其中不含生产数据；未运行 daemon 或浏览器进程。它将在 U1 修复冻结、重建对应二进制后重新初始化或作为可验证输入模板使用。
- 已创建不纳入应用实现的验证辅助脚本 `docs/reports/validation/verify-n1-u1.mjs`，用于后续经正式认证、Control/Observe API 构造多页 Task 与大历史；脚本不输出或持久化到报告任何认证材料。

### 冻结后状态变化

- 在上述 N1 补测完成后，U1 修复开始写入 `web/` 及 U1 相关源文件，应用源码指纹变为 `3a9758d322b910fb224fba19789b8aca95e64148934d88407b3271bcd83299d2`。这不是本报告所记录的先前冻结快照。
- 因此先前的 U1 自动化结果与未执行的浏览器计划均不继承到后续 U1 修复；待新的实现冻结后，必须重新记录源码/二进制指纹，重跑受影响 Go、前端和浏览器验证。

## 历史阶段结果

- N1：自动化与已覆盖边界通过，包括 plan 后 rebind 与 Begin 事务拒绝旧网络快照的独立证据；不扩大为 N2 或最终 ADR-002 验收。
- U1：当时因 3 项 P1 暂停浏览器 E2E，等待集中修复并重新冻结。
- 合并 Gate：`NO-GO`。此处为关闭补验前的历史结论；最终结论见“最终关卡”。

## 关卡边界

- 本报告只判定 N1/U1，不判定 N2 的测试/ready/回退/秘密管理/导入审计，也不判定 U2 的完整运行/结果/产物和全部安全内容边界。
- E1 仍需以最终提交、运行二进制和完整部署事实另行验收；本轮不会声称真实模型 Runtime E2E。

## 最终时间键冻结复验

- 最终应用源码指纹为 `2b599113a1f8a68e259cd3cd1720940e0fd9cd8ff57afedaa97b310a23ee99ff`；ADR-002/003 哈希仍分别为 `e1e4cb8b2368be2ea17f66fb1ce33c0185c8d4be9484bc63a245494b0d547f1d` 与 `a538850fdf884a4d7a00ab92646c6a6c9e5374eb91ef35a0c7ffabf7e80ec99c`。
- `go test -count=1 ./internal/persistence/sqlite ./internal/api/panel`、`go vet ./internal/persistence/sqlite ./internal/api/panel`、`go test -race -count=1 ./internal/persistence/sqlite ./internal/api/panel` 均通过。SQLite 批次包含 RFC3339Nano 纳秒、极端年份/时区、keyset 分页、每物理连接注册和 reopen 边界，也包含保留的 N1 plan/rebind/Begin 回归。
- 最终隔离二进制为 `/tmp/openagentx-n1-u1-bin-final`，SHA-256 `cdb5ddef4a55d2a77c3afda2f2a5511dc90f3a6d36e5fc4d242b9b4ea3655fce`。它在新建的 `0700` fixture、独立 SQLite、Unix Worker socket 和 SAN TLS 转发上运行；旧 daemon/TLS 已先停止。
- 仅通过正式 `init`、`agent apply`、认证 Control/Observe API 和 Unix Worker API，成功构造 62 个 Task、2 个 keyset 页面、1 个 `cancel_requested` Task，以及焦点 Task 的 222 个事件（初始 snapshot sequence 298）。取消状态由 Worker Register -> Heartbeat -> Claim -> Begin 后的 Control cancel 产生；没有直接改 SQLite，也没有把该模拟 Worker 表述为外部 Runtime E2E。
- 浏览器 Gate 仍为阻断：`agent-browser` 实例能启动 session，但其内置 Playwright 要求缺失的 `chromium_headless_shell-1200`，安装命令未提供该 revision，故 Chromium 从未加载指挥台页面。没有 DOM、三视口、SSE 重连、离线写保护或浏览器 CSP 执行证据；这些不得由 API fixture 替代。

## 历史冻结复验 Gate

- N1：受影响 SQLite/Panel 与保留 rebind 回归通过，保持已覆盖范围内的 `GO`；不扩大为 N2 或 ADR-002 总验收。
- U1：服务端时间键及 API fixture 通过，但真实浏览器验收未执行，故为 `NO-GO`。
- 合并 Gate：`NO-GO`。这是可用 Chromium 前的历史冻结复验结论，已由后续真实浏览器批次关闭。

## 历史浏览器阻断记录

- 上述缺 revision 结论是历史环境失败。后续使用 `/usr/bin/google-chrome`（`Google Chrome 143.0.7499.109`）和独立 `agent-browser` session 实际加载了隔离指挥台，并在登录后 DOM 确认为指挥台而非认证页。旧 CLI 的 `state load` 不被当作认证证据；它未注入 storage state，实际登录使用隔离 `0600` 密码文件经 Browser daemon socket 写入密码控件，密码未进入命令行、截图或本报告。
- 浏览器正向证据：桌面初始渲染 50 个 Task，点击“加载更多任务”后为 62 个；焦点 Task URL 保持选中态；焦点对话渲染 222 条并可滚动。390x844 和 412x915 视口的 DOM 均未发现横向溢出，截图保存在隔离 fixture 中。HTML CSP 已由实际 HTML 响应确认。
- 浏览器未闭合项：长对话滚动后离线输入控件 ref 失效，虽得到发送按钮不可用状态，未能证明离线写不排队；为制造 SSE 实时追加而调用正式 Control message 返回 `400`，页面仍为 222 条。因此 SSE 强断开重连、实时合并及离线写无请求排队均没有通过证据。
- 最终 Gate 修订：N1 维持 `GO`（限定已覆盖范围）；U1 因真实浏览器的 SSE/离线验收失败而为 `NO-GO`；合并 Gate 维持 `NO-GO`。该历史阻断已由“U1 关闭补验”定位 `400` 原因并关闭。

## U1 关闭补验

- fixture 实际可见 Task 总数为 62：1 个 cancel fixture、60 个分页 fixture 和 1 个 focus Task。此前脚本的 `createdTaskCount` 与输出漏计 cancel fixture，已修正为 62；分页断言仍只针对 60 个带查询条件的分页 fixture。
- `400` 失败路径已通过正式认证 Control API 在 Agent B fixture 上复核：响应为 `400 unsupported capability`。该 Agent 没有在线且租约有效、声明 deferred steer 的 Backend。focus Task 的 Agent A 在独立 route Worker 经 Unix Worker API Register 后每 10 秒 Heartbeat，未 Claim 任何工作；此时同一正式 Control append 成功。它是验证 fixture 的租约前置条件，不是应用 P1，也不被表述为真实 Runtime。
- 真实 Chrome（`143.0.7499.109`）批次中，focus Task 在浏览器离线后由独立正式 API 客户端追加缺口消息；恢复连接后原 Task URL 选中态仍在，消息数从 223 到 224，缺口消息 DOM 恰好一次。此顺序覆盖断流后的 replay/merge，而非只覆盖在线实时追加。
- 离线写保护：在线填写短草稿后清空请求追踪；离线时草稿保留、textarea 与发送按钮均禁用，Control `POST` 计数为 0；恢复后仍未产生 Control `POST`，focus 对话保持 224 条。控件因离线被禁用，不能在离线状态再填写新文本；保留草稿后禁用是用户可操作的等价保护路径。
- 移动触控：Chrome CDP 的 `hasTouch: true` 上下文（`navigator.maxTouchPoints > 0`）以 `touchscreen.tap` 在 390x844 完成列表进入 focus Task 详情和精确“返回”按钮回到列表；同一上下文切换至 412x915。此前的三视口截图仅代表布局，本项为独立真实触控证据。
- 脱敏证据已归档：`evidence/n1-u1/desktop-sse-offline.png` SHA-256 `3bf6f42db5956421531b6fc75c0a42da6ebb46c842e733d30e857b42202859bf`；`mobile-390.png` 为 `78bd06318e304d4a06897956dd3e1059b2fbc4cd8fdaf06ef4a6baa8a3b06ef4`；`mobile-412.png` 为 `8731e64c84505ef7b27f2efbb29540cde6f8dd938f1270b2caca6575126fabda`；touch 截图 `mobile-390-touch.png` 为 `7e2c1597dd53bec5131f287e262adc01f1b8f1505b923003b92b7b1686293c23`，`mobile-412-touch.png` 为 `33d79391af86293214f0de2e5bc93fdfa5d76134bbf783492d8031079e517d37`。目录不包含密码、Cookie、CSRF、私钥或 browser state。

## 最终关卡

- N1：`GO`，只限已执行的 SQLite/Panel 与时间键边界、Worker Register/Heartbeat 事务与 guard/lease 保活边界，以及 plan/rebind/Begin 回归范围。
- U1：`GO`，只限本报告的服务端筛选/分页、浏览器详情合并、断流恢复、离线写保护及三视口触控范围。
- N1/U1 合并 Gate：`GO`。这不扩展为 N2、U2、真实模型 Runtime 或完整部署验收通过。
