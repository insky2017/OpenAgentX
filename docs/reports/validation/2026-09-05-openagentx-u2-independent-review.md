# OpenAgentX U2 独立审核报告

日期：2026-09-05
审核角色：独立源码审核
基线 commit：`78ab470b21081dd2bf41d0d098861c4ce423d264`
结论：**U2 冻结源码 GO；最终功能验收等待 Terra。**

## 审核边界与验收矩阵

本轮只读审核基线之后的 U2 最小冻结批次，范围为 Runtime 结果语义、Run/Task 终态映射、公开 Journal/Observe 投影和控制台 Markdown 展示。未运行测试、Runtime、网络或浏览器，未审新范围，也未修改源码。N2 `NetworkSettings`、schema 和冻结 ADR 均不在本次 diff 中。

| 验收维度 | 本轮源码判据 | 证据格式 |
|---|---|---|
| 正向流程 | Runtime 结果可落入 Run，业务效果未知时 Task 明确转为 `uncertain`；控制台可展示正文 | 文件与行号、冻结指纹 |
| 状态不变量 | Runtime 自报不得提升权威 `SideEffectsKnown`；cancel intent 不得覆盖未知物理结果 | 状态映射代码 |
| CAS/幂等 | Finish 继续受 Worker、generation、fencing、lease 和 Run version 约束；相同 approval 重试不重复推进 | 仓储事务代码 |
| 失败路径 | 畸形/失败 Runtime 结果保留诊断；Markdown 渲染异常回退原始纯文本 | 失败分支代码 |
| 竞态场景 | Finish 与 cancel/message 并发时保留 `uncertain`，terminal Task 的消息不重开工作轮次 | 事务分支代码 |
| 安全与证据 | Journal 不写 Runtime 原始正文、session 或任意 payload；Observe 不暴露 fencing/provider session | 白名单 DTO 与序列化代码 |

审核的 10 个核心文件逐文件 `sha256sum` 输出再做 SHA-256，集合指纹为：

```text
e4bd16909935a0f77d598d4991d3de09c3e79bc953e9f43b2b4b4471f5ca31df
```

核心文件为：

```text
internal/runtime/contract.go
internal/runtime/agy/stream.go
internal/runtime/acp/adapter.go
internal/runtime/codebuddy/adapter.go
internal/controlplane/worker_service.go
internal/persistence/sqlite/worker_execution_repository.go
internal/api/observe.go
internal/api/panel/handler.go
web/src/main.jsx
web/src/styles.css
```

## 四类直接风险结论

| 直接风险 | 源码证据 | 判定 |
|---|---|---|
| 主流程失败 | `TurnResult` 分离权威与 Runtime 自报字段；AGY 只写 `RuntimeSideEffectsKnown`，ACP/CodeBuddy 成功也不推断业务副作用；Run 保留 Runtime 物理终态和正文，业务效果未知时 Task 转 `uncertain`（`contract.go:229-246`；`agy/stream.go:45-65`；`acp/adapter.go:220-227`；`codebuddy/adapter.go:263-286`；`worker_execution_repository.go:612-643`） | **未发现源码阻断**；两条真实 AGY Task 待 Terra |
| 秘密泄露或权限绕过 | 普通 Runtime event 只保存类型和时间白名单；approval payload 严格解码且 Task/Run 绑定由认证事务派生；Finish Journal 只写状态、有无正文/错误、自报值和 `not_recorded`，不写正文或 provider session（`worker_service.go:507-592,595-618`） | **未发现秘密或权限直接风险** |
| 覆盖、重复或误删 | Append/Finish 先验证 guard、Run version、lease 与 fencing；重复 approval 为 no-op；Finish 与 cancel/message 的版本差异集中处理，`uncertain` 不被 cancel intent 覆盖（`worker_execution_repository.go:430-475,594-625,704-725`） | **未发现覆盖、重复执行或误删路径** |
| 虚报成功或核验 | Runtime 自报仅标为 `runtime_reported`，业务核验来源固定为 `not_recorded`；公开 DTO 不返回权威 `SideEffectsKnown`；控制台展示 `uncertain` 和 Runtime 诊断，不把自报改写为业务成功（`observe.go:88-115`；`panel/handler.go:448-476`；`main.jsx:160-174,1024-1034`） | **未发现把未核验业务效果显示为成功的路径** |

## 关键状态与安全证据

- AGY 的 `side_effects_known` 只进入 `RuntimeSideEffectsKnown`，不会写入权威 `SideEffectsKnown`。AGY 无自报以及 ACP、CodeBuddy 的成功结果均保持权威副作用未知。
- Runtime 返回 `succeeded` 但权威副作用未知时，Run 保持物理 `succeeded` 和正文，Task 转为 `uncertain` 并记录 `business_effect_unverified`；已有 cancel intent 不覆盖该不确定终态。
- pending message 在 terminal `uncertain` Task 下不会变为新的 work lane 或触发自动重试；其 control lane 可按既有领取流程结束为 `superseded`。旧的 defer-to-work 分支只适用于非 terminal Task，不构成本轮直接阻断。
- Runtime Journal 对普通事件丢弃原始 payload；approval 只保留严格解析后的必要字段；Finish Journal 不保存正文、完整 `TurnResult` 或 provider session。
- Observe 通过 Run 的 `worker_instance_id` 查询对应 Worker generation；结果 DTO 仅返回 Runtime status/body/error、Runtime 自报及来源和业务核验来源，不暴露 execution JSON、fencing、provider session 或权威 `SideEffectsKnown`。
- Task、Message、Approval、Result 共用 `MarkdownContent`；URL 仅允许 `http`、`https`、`mailto`，图片隐藏，渲染异常回退纯文本，并提供原文查看和复制结果反馈。代码块和表格使用局部横向滚动（`main.jsx:47-116,900-918,1024-1034`；`styles.css:103-104`）。

## 未覆盖项

- 本轮未运行单元、集成、race、构建、真实 Runtime 或浏览器测试，不以源码审核代替动态证据。
- 未验证 Terra 正在执行的 U2/E1 浏览器流程和两条真实 AGY Task，也未核对其最终 Event Journal、RunAttempt、Task、workspace 副作用和截图。
- `TurnResult.Validate` 没有额外约束两个 side-effect 字段的关系；当前三个正式适配器未把 Runtime 自报写入权威字段，因此不构成本轮四类直接风险阻断，后续新增适配器仍需遵守该边界。
- 完整 ADR 条款、极端组合故障、N2 网络配置与 schema 不属于 U2 最小冻结批次，本报告不对其作 Gate 结论。

## Gate 判定

- **U2 冻结源码：GO。** 在本轮四类直接风险和限定源码范围内未发现阻断。
- **最终功能验收：等待 Terra。** Terra 动态证据到齐并完成一次短核前，不宣称 U2/E1 真实闭环通过。

## 当前动态证据短核

本节更新上文“等待 Terra”的旧时点，只读核对 [U2/E1 验证报告](2026-09-05-openagentx-u2-e1-validation.md)、两份已标为历史的准备稿及协调记录中的已保存非秘密事实；未运行测试、浏览器、API、Runtime，也未重新审核源码。动态报告已准确标为 `partial/incomplete`：r2 无监听只作为退役时的历史检查，r3 已实际执行且当前暂停新增验证。

### 已有证据匹配项

- r3 指挥台已完成 `e1-http-g4` 的正式 Test、publish 和 binding r1 apply，绑定到同一 Worker generation 4。分层结果中的 network effect 与 model call 仍为 `not_verified`，报告没有将其外推为已核验。
- selected E1 native 的 `no_blackip` SOCKS5 与 `http_config_host_port` HTTP 案例均记录 marker 匹配和一次代理连接；该事实只支持 wrapper/native 规则效果，不支持 AGY Task 或业务副作用通过。
- 实际 Task 顺序为 A → B → A2：A 只生成 31-byte 文件并明确失败；B 在 A2 前只读该 31-byte 状态，只证明同一常驻 Worker 再次接单；A2 在前置 SHA 精确匹配时追加 LF，外部 `cmp` 与 SHA 确认最终 32 bytes。A2 已纠正 workspace 最终内容，但没有抹去 A 的历史失败，也没有把产品 Task 的 `uncertain / business_effect_unverified` 提升为 succeeded。
- 有限 Markdown DOM 只证明 observer 内容中的链接、表格、“图片已隐藏”以及原文/复制按钮可见。报告没有宣称复制成功、完整安全链接行为或移动布局通过。

### 未通过与未验证项

- observer Task 在观察窗口内一直为 `queued`，未进入预期 `waiting_input`，原因尚未确定。因此断线恢复、消息恰一次和离线浏览器 Control POST=0 均没有证据，不能判通过。
- observer 输入中的 code fence 被 shell 误解析，当前 Task 不含预期代码块；这是一项输入失败，不能作为代码块 Markdown DOM 或手机代码滚动证据。
- 坏配置正式失败、关键 idempotency key 重放、三视口/触控、手机代码与表格、完整截图/网络 manifest 均未完成。
- Task B 未导出完整 Run/Result/Journal 白名单字段；现有只读结果不得扩大解释为完整 Journal 验收。
- 外部代理/API 凭据存在暴露风险，原始值未写入报告，但轮换尚未完成；这属于仍待处理的交付事项。

### 当前判定

- **U2 冻结源码：继续 GO。** 10 个核心文件集合指纹仍为 `e4bd16909935a0f77d598d4991d3de09c3e79bc953e9f43b2b4b4471f5ca31df`。
- **冻结 ADR：未变。** ADR-001/002/003 SHA-256 仍分别为 `587e9b8f27ddeb68aae8e5a507fefc10973739c8dd2c3def86e2c0bbc51db403`、`e1e4cb8b2368be2ea17f66fb1ce33c0185c8d4be9484bc63a245494b0d547f1d`、`a538850fdf884a4d7a00ab92646c6a6c9e5374eb91ef35a0c7ffabf7e80ec99c`。
- **U2/E1 动态验收：`partial/incomplete`。** 已完成事实和失败记录可信，但上述缺失证据仍属于原最小验收矩阵，当前不能判最终功能 GO。
- **阶段交付方案不等于验收基线。** 用户当前只要求收口方案；延期剩余最小验收仍是待确认提案，尚未获准改变验收边界，不能据此把未验证项视为后置完成或将当前 Gate 提升为 GO。

## 2026-09-06 HTTP 422 能力不足恢复定向短核

本节只审以下四个新增变更，不重审其余 U2 源码，不运行测试、浏览器、API 或 Runtime：

```text
internal/client/worker/client.go
internal/client/worker/client_test.go
internal/worker/run_manager.go
internal/worker/runner_test.go
```

四文件逐文件 `sha256sum` 输出再做 SHA-256，集合指纹为：

```text
b086692323ac991b728a34d9a790fddafec5291af45143b951907a80865b5643
```

逐文件指纹：

```text
eb7d73c975049b1eeec88c406c5adb549c840a36fc2d13537d78f43f5b7efad1  internal/client/worker/client.go
4639fc9fe697aa0a9d3a8c49d1d830d88654d3ecf17b2d11d5be57a39bad308c  internal/client/worker/client_test.go
163c621a1c6de49439c3aa2fe3b88ad1d51e7019149b626f51f37f97a439118e  internal/worker/run_manager.go
4690cb1c084ac1491ed48e1bea0fa0b5fbbbb337274dd83430669645abdaf1b5  internal/worker/runner_test.go
```

### 定向验收矩阵

| 验收维度 | 源码判据 | 判定 |
|---|---|---|
| 只恢复未开始 Run 的能力不足 | HTTP `UNSUPPORTED_CAPABILITY` 映射为 `ErrUnsupportedCapability`；恢复分支只位于 `BeginAttempt` 返回错误且尚未取得 `BeginAttemptResponse` 的路径。Begin 成功后的网络、Adapter 校验或启动失败仍走 `failStartedRun`（`client.go:31-45`；`run_manager.go:128-178`） | **符合** |
| 不吞安全与所有权错误 | unauthorized、lease、fencing、stale/generation 保持各自领域错误；`startWork` 只对 `ErrUnsupportedCapability` 返回 recoverable，其余错误继续返回并终止 Resident Worker（`client.go:31-45`；`client_test.go:15-77`；`run_manager.go:140-153`；`runner_test.go:500-530`） | **符合** |
| 不虚报成功或重复执行 | 首次能力不足在 Run 创建和 Runtime 启动前释放容量，不调用 Finish 或伪造终态；同一 claimed item 重返时先等待 1 秒，恢复后测试断言两次 Begin 仅创建一个 Run、启动一次 Runtime、写一次 Finish（`run_manager.go:140-185`；`runner_test.go:458-498`） | **符合** |
| 退避可取消且生命周期独立 | 退避 timer 同时等待 `ctx.Done()`，取消时可立即退出；定向测试证明退避期间 heartbeat 继续发生。network work 处理代码未在四文件实现变更中修改，既有 runner 测试仍覆盖慢 network probe 不阻塞 heartbeat/control（`run_manager.go:143-150`；`runner_test.go:458-474,564-597`） | **源码未见阻断**；本次新测试未直接覆盖退避窗口内 network work |

### 测试证据边界

- 作者报告 `internal/client/worker` 与 `internal/worker` 两包已通过；本次独立审核未重跑，不能把作者结果写成独立执行证据。
- 新 client 测试通过 `httptest.Server` 实际执行 HTTP POST、Bearer header、422/401/409 响应解码和 `errors.Is` 映射，覆盖 `UNSUPPORTED_CAPABILITY` 及 unauthorized、lease、fencing、stale/generation 的区分；它不是完整 daemon/Unix socket/真实 Worker E2E。
- 新 runner 测试使用 fake Worker client，覆盖同一 claimed item 立即重返、250 ms 内不热重试、heartbeat 存活、恢复后单 Run/单 Finish，以及四类安全错误终止 Worker且不启动 Runtime；未直接覆盖退避期间取消、network work ACK 或真实 HTTP 422 后的完整 Worker 生命周期。
- Terra 的独立 HTTP 与 Resident Worker 生命周期验证仍是动态 Gate 证据来源；结果到齐前不得把本节源码 GO 扩大为最终功能验收通过。

### 定向判定

- **四文件定向源码审核：GO。** 未发现恢复范围越界、吞掉安全错误、虚报成功或重复 Runtime 执行的直接风险；1 秒退避有界且可取消。
- **U2 冻结源码：继续 GO；此前 U2/E1 动态验收 `partial/incomplete` 历史不变。** 本节只清除预配置 queued Task 因 HTTP 422 错误未映射而杀死 Worker 的源码阻断，实际 HTTP/生命周期结论等待 Terra 独立证据。
