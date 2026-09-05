---
doc_type: independent_review
scope: N1-U1
status: go
initial_status: no-go
owner: independent-review
reviewed_at: 2026-09-05
baseline: b0e98ece3335ba43b6cf2b74a9e6a1b2cd1c56e8
---

# OpenAgentX N1/U1 独立审核报告

## 审核身份与边界

- 本次实际审核模型为 `gpt-5.6-sol` medium；指定的 Astra 当前不可用，本报告不是 Astra 审核证据。
- 基准为 `b0e98ece3335ba43b6cf2b74a9e6a1b2cd1c56e8`，审核对象为其上的 N1/U1 冻结工作树实现。
- U1 范围：`internal/api/observe.go`、`internal/api/panel/*`、`internal/persistence/sqlite/observe_task_query*.go`、`web/src/*`。
- N1 范围：Worker API/Control、network domain、SQLite migration/binding/Worker/RunAttempt、resolver、BackendPool、Runner/RunManager 及对应测试。
- 本次不判定 N2 的 secret store、materializer、受控 wrapper digest、测试/ready/回退/导入审计，也不判定 U2 的完整 TurnResult、产物和全部内容安全呈现。
- 未运行全量、race、真实 Runtime 或浏览器批次；这些由 Terra 独立验证代理独占执行。本审核只运行了定向 `go test -count=1 ./internal/api/panel ./internal/persistence/sqlite`、`web` 的 `npm run build` 和 `git diff --check b0e98ec`，均通过。定向测试和构建不能替代竞态或浏览器证据。
- ADR-002、ADR-003 均与 `b0e98ec` 的 Git blob 一致；未修改冻结 ADR。

## 首次验收矩阵（修复前）

| 批次 | 正向流程与状态不变量 | CAS/幂等与竞态 | 失败路径 | 证据格式 | 判定 |
|---|---|---|---|---|---|
| N1 | Register 与初始 binding 同事务；heartbeat 与 Worker/Backend/application/Journal 同事务；只有当前 Worker generation/revision 的 applied binding 可进入新 Run | guard 优先；旧 revision 回执忽略；重复回执不重复 Journal；Begin 事务复核网络快照 | `{}` 遗留策略 unavailable；apply/Backend 故障不终止控制连接；失败回执不写新 applied 事实 | 代码路径、故障注入/状态断言；等待 Terra 批次 | 静态审核 GO，最终仍待 Terra |
| U1 | 服务端 Agent/状态/时间/文本筛选及 keyset 分页；详情 history/live 分页；URL 选中态与离线写保护 | sequence 去重；旧请求不得覆盖选择/历史/滚动；重连不得跳历史 | 大于一页、筛选变化、无匹配、403/404/409、断线恢复 | 代码路径、定向测试；浏览器证据由 Terra 提供 | **NO-GO（3 项 P1）** |

## 首次审核发现（修复前）

### P1-U1-01：live 请求可覆盖并丢失并发完成的 history 页

`catchUpTaskDetail` 在请求开始时把 `taskDetailRef.current` 捕获到局部变量 `merged`（`web/src/main.jsx:460-472`），live 响应后继续只基于该局部对象合并并直接写回 ref/state（`web/src/main.jsx:475-483`）。history 请求虽然在返回时读取最新 ref 并追加旧事件（`web/src/main.jsx:572-583`），但两个通道只分离了 request ID，没有在写回时对同一详情状态做 CAS 或基于最新 ref 合并。

可证明交错：详情为事件 `[101..200]`；live 请求 L 启动并捕获该对象；history 请求 H 启动并先返回 `[1..100]`，写成 `[1..200]` 并推进 `history_before_sequence`；随后 L 返回事件 `201`，用旧捕获对象合并成 `[101..201]` 并写回。结果是 H 已成功显示的历史页消失，历史游标也回退到 L 启动前的值。选择 generation 检查无法阻止该交错，因为 L/H 属于同一个 Task，且 `detailHistoryRequestRef` 与 `detailLiveRequestRef` 相互独立。

这直接违反协调矩阵“详情增量连续、history/live 独立游标、旧请求不能覆盖、加载旧页期间新增事件仍可见”。需让每次 live 写回基于当时最新的 `taskDetailRef.current`（或 reducer/CAS），并增加 L 先发、H 先回、L 后回的确定性交错测试。

### P1-U1-02：离线恢复会用非原子 overview 水位重置 SSE 起点，存在跳过事件窗口

重新建连时，前端先请求 overview，然后无条件把 `lastSequenceRef.current` 设置为响应的 `latest_sequence`，并从该值创建新的 EventSource（`web/src/main.jsx:250-279`）。服务端 overview 依次独立读取 agents、workers、tasks、approvals，最后才读取 Journal 最新序号（`internal/api/panel/handler.go:124-133`），这些读取没有共同事务或同一快照水位。

可证明交错：overview 已读到旧 Task 投影；随后 Task 事务提交新状态和 Journal 序号 `N`；overview 最后的 `LatestJournalSequence` 返回 `N`；前端从 `after_sequence=N` 重连。新事件不会再由 SSE 返回，而 overview 内 Task 仍是旧状态。任务列表的并行刷新不是一致性保证，它也没有与该水位绑定；若同样在提交前读取，页面会一直旧到下一条事件触发刷新。Task 详情自己的 live cursor 能补 selected Task，但不能修复全局列表/overview 的缺口。

这违反 U1“恢复后从最后游标补齐、重连不会跳过历史”。重连必须从客户端已确认处理的最后事件继续，或让快照数据与覆盖水位来自同一数据库快照并在应用后再推进 cursor；需增加“overview 投影读取与 Journal 提交交错”的重连测试。

### P1-U1-03：RFC3339Nano 文本排序破坏时间筛选和 keyset 分页

`QueryTasks` 对 SQLite `TEXT` 列 `updated_at` 直接使用 `>=`、`<`、游标比较和 `ORDER BY updated_at DESC`（`internal/persistence/sqlite/observe_task_query.go:41-61`）。该列通过 `formatTime` 按 `time.RFC3339Nano` 写入（`internal/persistence/sqlite/repository.go:18`、`:125-127`），而该格式会省略整秒值的小数部分。因此时间先后与 SQLite 字典序并不一致：`2026-09-05T12:00:00Z` 会排在实际更晚的 `2026-09-05T12:00:00.1Z` 前面。

这会同时破坏服务端时间范围筛选、降序排序和以 `(updated_at, task_id)` 为键的分页边界。跨该边界翻页时，Task 可能乱序、遗漏或重复，直接违反 U1 的筛选和稳定 keyset 分页验收条件。现有测试把全部 Task 的 `updated_at` 设置为完全相同的值，只验证 `task_id` tie-breaker（`internal/persistence/sqlite/observe_task_query_test.go:11-35`），无法发现此问题。

需改用具有固定宽度且保持时间顺序的持久化/比较表示，或在 SQL 中按可靠的时间值比较，并增加整秒、不同长度小数秒及分页游标跨界的回归测试。迁移或兼容既有时间文本也应纳入修复边界。

### P2-U1-04：延迟滚动回调未绑定 Task/请求 generation，且 live 自动滚动不区分详情视图

live 合并和 history 追加都在通过 generation 检查后安排 `requestAnimationFrame`，但回调执行时不再次核对 selection/request，并直接操作当前 `detailScrollRef`（`web/src/main.jsx:484-486`、`web/src/main.jsx:585-588`）。同一帧前切换/关闭 Task 时，旧请求回调可能作用到新的详情节点。`readingLatestRef` 还跨 content/conversation/run/result 共用，live 事件新增时会在任何视图调用滚到底部，而“跳到最新”语义实际只对应运行事件。

该问题可能破坏“旧请求不能覆盖新选择的滚动”和“SSE 更新不抢夺阅读位置”。建议在 rAF 内重验 Task ID、selection generation 与请求 ID，并只在当前 run 视图且仍位于底部时自动跟随；其余情况仅显示“跳到最新”。P2 不单独阻塞，但应与 P1-U1-01 同批修复。

## N1 审核结果

N1 未发现 P0/P1。以下路径满足本批冻结矩阵：

- `RegisterWorker` 在一个 SQLite 事务内写 Worker、Backend、Journal 并读取初始 binding，任一失败整体回滚（`internal/persistence/sqlite/worker_control_repository.go:16-144`；故障测试 `internal/persistence/sqlite/worker_network_consistency_test.go:78-101`）。
- Heartbeat API 先认证 principal/token，再按 generation/fencing/lease 授权，之后才验证 ack ownership；数据库在同一事务更新 Worker、Run lease、Backend health、binding application 和 Journal（`internal/controlplane/worker_service.go:179-236`；`internal/persistence/sqlite/worker_control_repository.go:213-374`）。
- binding ack 携带 `BindingRevision`；rebind 提升 version 并清空 applied 字段；旧 revision ack 只被忽略，不回滚 heartbeat；重复 applied/failed ack 不重复写 application Journal（`internal/persistence/sqlite/network_profile_repository.go:149-196`；`internal/persistence/sqlite/worker_control_repository.go:318-356`）。
- failed ack 只更新 desired status/固定脱敏诊断，不写入新的 applied worker/generation/profile/revision；已有 applied 事实不会被失败回执伪造成新成功事实（`internal/persistence/sqlite/worker_control_repository.go:336-347`）。
- `ListWorkerBackends` 将遗留 `{}` 策略、pending/failed binding、非当前 worker generation/revision 的 applied binding 标记为 unavailable；新注册时把零值显式归一为 `inherit`（`internal/persistence/sqlite/worker_execution_repository.go:17-95`；`internal/persistence/sqlite/worker_control_repository.go:106-124`）。
- Begin 在状态事务内重新比对持久 Backend policy、当前 binding revision、Worker generation 及待落库 ResolvedExecutionSpec；rebind 发生在 plan 后、Begin 前会 fail closed，不会以旧配置创建 Run（`internal/persistence/sqlite/worker_execution_repository.go:157-182`、`:323-382`）。
- Backend 自检/配置应用失败进入 unavailable/degraded，`WorkCapacity` 为零而控制、heartbeat 循环继续；修复后恢复领取 queued Task（`internal/worker/backend_pool.go:114-170`；`internal/worker/run_manager.go:65-68`；`internal/worker/runner.go:45-85`、`:134-209`）。

N1 的剩余证据边界：冻结代码中没有看到专门制造“plan 完成后 rebind、Begin 事务随后拒绝旧快照”的交错测试；当前结论来自事务代码审查，最终 Gate 应要求 Terra 的 N1 边界用例覆盖该交错。N2 的 secret/materializer/wrapper digest 不计为 N1 缺陷，也不能据此宣称完整 ADR-002 已通过。

## 首次 Gate 判定（修复前）

- **N1：静态独立审核 GO，等待 Terra 批量证据后才能形成 N1 最终验收。**
- **U1：NO-GO。** P1-U1-01、P1-U1-02 与 P1-U1-03 均直接破坏冻结 U1 验收条件。
- **N1/U1 合并 Gate：NO-GO。** 修复三项 U1 P1、补相应竞态与分页边界测试并由 Terra 对冻结后的新 diff 重跑其独立批次后，方可重新审核。

## 首次下一步（修复前）

1. U1 单一所有者集中修复 live/history 合并、reconnect 水位和时间比较/分页，不扩展到 U2。
2. 增加三组确定性测试：live/history 反序完成；overview 快照与 Journal 提交交错后重连补齐；整秒/小数秒排序与跨界分页。
3. 修复冻结后由 Terra 重新运行一次独立批量与真实浏览器证据；主代理再据新 diff 判定 Gate。

## 冻结修复复审

本节保留上述首次 NO-GO 及其反例作为历史，复审其后的冻结修复。N1 实现未变；Terra 新增的 plan/rebind/Begin 验证测试不改变本报告对 N1 源码的既有结论，独立批量证据仍由 Terra 判定。

### 复审验收矩阵

| 首次问题 | 冻结修复 | 反例复审 | 判定 |
|---|---|---|---|
| P1-U1-01 live 覆盖 history | live 响应写回前读取最新 `taskDetailRef.current`，合并时保留最新 history 游标及历史事件 | L 先发、H 先回、L 后回时，最终事件为 `[1..201]`，history 游标保持 H 的推进结果 | **已清除** |
| P1-U1-02 overview 水位跳过 SSE | overview 的 `latest_sequence` 不再推进客户端确认游标；EventSource 只从 `lastSequenceRef.current` 恢复 | overview 读旧投影、事件提交、overview 后读新水位时，仍从已确认序号继续回放 | **已清除** |
| P1-U1-03 RFC3339Nano 时间比较 | 查询改用 SQLite `julianday()` 做范围、排序和 cursor 比较 | 原整秒/0.1 秒反例已通过；合法亚毫秒时间仍折叠为同一值，范围与真实顺序仍错误 | **未清除，P1** |
| P2-U1-04 延迟滚动与跨视图抢位 | rAF 回调重验 Task、selection、request、view；仅 run 视图且原本位于底部时自动跟随 | 切换 Task/请求/视图后旧回调均被拒绝，其他视图仅提示“跳到最新” | **已清除** |

### 已清除问题的代码证据

- P1-U1-01：`catchUpTaskDetail` 在每次 live 页返回时读取最新 ref，再调用 `mergeLiveTaskDetail`（`web/src/main.jsx:477-487`）；合并函数保留当前 history 游标和已有事件（`web/src/task-observation-state.js:1-15`）。确定性单测覆盖 L 先发、H 先回、L 后回（`web/src/task-observation-state.test.js:16-42`）。
- P1-U1-02：overview 只更新展示数据，EventSource 起点通过 `sseResumeAfter(lastSequenceRef.current, overview.latest_sequence)` 取得，helper 明确忽略非事务 overview 水位（`web/src/main.jsx:266-289`；`web/src/task-observation-state.js:27-32`）。对应反例单测位于 `web/src/task-observation-state.test.js:44-49`。
- P2-U1-04：live 和 history 的 rAF 均在执行时调用 `isCurrentObservation` 复核 Task、selection、request 和 run 视图（`web/src/main.jsx:489-496`、`:598-605`）；非 run 视图不会自动滚动。helper 的拒绝条件由 `web/src/task-observation-state.test.js:51-64` 覆盖。

### 剩余 P1：`julianday()` 丢失亚毫秒精度

冻结修复把 `updated_at` 的范围、cursor 和排序全部改为 `julianday(updated_at)`（`internal/persistence/sqlite/observe_task_query.go:12`、`:43-63`）。这消除了整秒与 0.1 秒文本的字典序反转，也能归一化时区表示，但 SQLite 的日期函数不会保留当前领域值允许的 RFC3339Nano 全部精度。Task 创建与状态迁移仍由 Go `time.Now()` 生成并按 RFC3339Nano 写入，因此亚毫秒差异是合法生产输入，不是仅测试可构造的异常格式。

本次直接 SQLite 反例：

```text
task-z-earlier|2026-09-05T12:00:00.0001Z|2461289.000000000000000
task-a-later|2026-09-05T12:00:00.0002Z|2461289.000000000000000
```

两者 `julianday()` 相等，`ORDER BY julianday(updated_at) DESC, task_id DESC` 会把较早的 `task-z-earlier` 排在较晚 Task 前；`updated_after=...0002Z` 也会错误包含 `...0001Z`，而 `updated_before=...0002Z` 会错误排除它。新增 Go 测试使用整秒、10 ms、50 ms、100 ms（`internal/persistence/sqlite/observe_task_query_test.go:37-85`），没有覆盖亚毫秒折叠，测试名中的 `Precision` 不能证明 RFC3339Nano 精度已保持。

该缺陷继续直接破坏 U1 的服务端时间范围筛选和按更新时间排序，故 P1-U1-03 仍阻断。修复需使用能完整保持 RFC3339Nano 时序的比较键，例如固定宽度 UTC 纳秒文本或整数时间值，并覆盖相差 1 ns/100 us、整秒边界、时区等价值和跨边界 cursor。

### 复审验证边界

- `node --test web/src/task-observation-state.test.js`：4 项通过。
- `go test -count=1 ./internal/persistence/sqlite -run 'TestQueryTasks(NormalizesRFC3339PrecisionAndTimezoneForRangeAndCursor|UsesStableKeysetAndServerFilters)$'`：通过。
- 直接 SQLite 亚毫秒反例：确认 100 us 与 200 us 映射到相同 `julianday()`，且范围过滤结果错误。
- 按分工未运行批量、build、race、真实浏览器或 Runtime；Terra 独立验证仍在进行。

### 复审 Gate 判定

- **N1：源码静态审核维持 GO，最终仍待 Terra 独立批量证据。**
- **U1：NO-GO。** P1-U1-01、P1-U1-02 已清除，P2-U1-04 已清除；P1-U1-03 因亚毫秒精度丢失仍直接违反冻结 U1 验收条件。
- **N1/U1 合并 Gate：NO-GO。** 修复剩余时间比较 P1、增加真实精度边界测试，并由 Terra 对新冻结 diff 完成独立验证后再判定。

## 第二次冻结精确时间复审

本节复审 P1-U1-03 的第二次冻结修复及新 SQLite driver 接线，不改变前两轮审核历史。实际审核模型仍为 `gpt-5.6-sol` medium；Astra 不可用。

### 已通过边界

- `openagentx_rfc3339nano_key` 使用 Go `time.Parse(time.RFC3339Nano)` 解析，再输出九位小数 UTC 文本；1 ns、100 us、200 us 和 400 us 均保留真实次序（`internal/persistence/sqlite/time_key_driver.go:11-34`）。
- 时间范围、降序排序和 keyset cursor 统一使用同一函数，没有混用原始文本或 `julianday()`（`internal/persistence/sqlite/observe_task_query.go:12`、`:43-63`）。
- 普通 offset 等价时间归一为相同 UTC key，再以 `task_id` 稳定打破平局；逐条分页覆盖整个序列（`internal/persistence/sqlite/observe_task_query_test.go:39-100`）。
- Repository 生产入口改用带 `ConnectHook` 的专用 driver；函数在每个物理连接创建时注册。三个同时占用的连接及关闭后重开均有验证（`internal/persistence/sqlite/repository.go:39-76`；`internal/persistence/sqlite/observe_task_query_test.go:102-146`）。仓库内没有其他生产 Repository 构造路径绕过该 driver；默认 `sqlite3` 只用于 migration/testkit 的裸数据库工具。
- 畸形持久时间使 SQL 函数返回错误，`QueryTasks` 拒绝结果而不退回文本比较（`internal/persistence/sqlite/observe_task_query_test.go:148-158`）。

### P1-U1-03 仍未完全清除：offset 可破坏四位年份宽度

helper 假定 `sqliteTimeKeyLayout = "2006-01-02T15:04:05.000000000Z"` 总能产生固定宽度 key，但 Go 的 RFC3339 parser 接受 `0000` 至 `9999` 的四位输入年份，offset 换算 UTC 后可越出该范围：

```text
0000-01-01T00:00:00+14:00 => -0001-12-31T10:00:00.000000000Z
9999-12-31T23:59:59-14:00 => 10000-01-01T13:59:59.000000000Z
```

第二个 key 以字符 `1` 开头，按 SQLite 文本顺序反而小于以 `2` 开头的 `2026`。API 使用 `time.Parse(time.RFC3339, ...)` 接受该 `updated_before`，且没有年份范围约束（`internal/api/panel/handler.go:338-353`）；查询随后会错误排除本应早于 year 10000 上界的当前 Task。第一个值也证明 helper 的输出已不满足其固定宽度前提。这里应 fail closed，而不是生成不能保证时序的 key。

该问题仍属于原 P1-U1-03，不新增独立 P1。修复应在 UTC 归一化后检查允许年份范围，再格式化 key；若业务域只接受公历正常年份，建议明确限制为 `1..9999`。至少增加 year 0000 配正 offset、year 9999 配负 offset 两个拒绝用例，并同时覆盖 API filter 与 cursor 解析。

### 本轮定向证据与 Gate

- `go test -count=1 ./internal/persistence/sqlite -run 'Test(QueryTasksNormalizesRFC3339PrecisionAndTimezoneForRangeAndCursor|RepositoryRegistersRFC3339NanoKeyOnEveryConnectionAndReopen|QueryTasksRejectsMalformedStoredTimestamp)$'`：通过。
- 同一定向集合 `-count=5`：通过。
- 未运行全量、build、race、真实浏览器或 Runtime；等待 Terra 对最终冻结产物交叉验证。
- **U1：NO-GO。** 纳秒、普通 offset、keyset、畸形存储值和 driver 生命周期均通过，但 P1-U1-03 的 offset 年份溢出尚未 fail closed。
- **N1/U1 合并 Gate：NO-GO。** N1 静态 GO 不变；待上述边界修复并重新冻结后复审。

## 第三次冻结极端年份复审

本节复审第二次冻结发现的最后一个 P1-U1-03 边界。前述两次 NO-GO 均作为对应源码快照的审核历史保留。

### 边界结论

- 精确时间 helper 将 UTC year 加一并编码为五位前缀，随后拼接不含年份的固定宽度月日至纳秒尾部（`internal/persistence/sqlite/time_key_driver.go:11-35`）。
- 四位 RFC3339 输入经最大时区 offset 换算后，UTC year 的完整可达范围是 `-1..10000`；加一后恰为 `0..10001`，`%05d` 在整个范围均为五位且保持数值顺序。
- 下溢值 `0000-01-01T00:00:00+14:00` 编码为 `00000-12-31T10:00:00.000000000Z`；上溢值 `9999-12-31T23:59:59-14:00` 编码为 `10001-01-01T13:59:59.000000000Z`。两者与普通年份的文本顺序等于时间顺序（`internal/persistence/sqlite/observe_task_query_test.go:104-129`）。
- 集成用例把上下溢出值加入真实 `QueryTasks` 数据集，并以单条 page 逐页遍历；offset 等价、1 ns、100/200/400 us、极端年份和 keyset 顺序均连续且无重复遗漏（`internal/persistence/sqlite/observe_task_query_test.go:39-102`）。
- 畸形时间仍由 `time.Parse` 返回错误并使查询 fail closed；自定义 driver、每连接注册和重开接线未变化，沿用上一轮已通过结论。

### 最终源码判定

- 定向命令 `go test -count=1 ./internal/persistence/sqlite -run 'Test(QueryTasksNormalizesRFC3339PrecisionAndTimezoneForRangeAndCursor|SQLiteRFC3339NanoKeyKeepsUTCOverflowYearsOrdered)$'`：通过。
- 未发现由本次 helper 或 driver 接线引入的新 P0/P1。
- **P1-U1-03：已清除。** 时间筛选、排序和 keyset 使用同一精确、固定宽度、时区归一化的内部键；合法输入的纳秒与极端 offset 年份均保持顺序，畸形输入 fail closed。
- **U1：源码独立审核 GO，等待 Terra 独立批量和真实浏览器证据。** P1-U1-01、P1-U1-02、P1-U1-03 及同上下文 P2-U1-04 均已清除。
- **N1/U1：源码独立审核 GO，最终 Gate 待 Terra。** N1 冻结源码未变；本轮未运行全量、build、race、真实浏览器或 Runtime。

## Terra 最终证据交叉核对

本节只读核对 `2026-09-05-openagentx-n1-u1-validation.md` 及 `docs/reports/validation/evidence/n1-u1/`，未重跑测试或浏览器。实际审核模型仍为 `gpt-5.6-sol` medium；Astra 不可用，本结论不冒称 Astra 证据。

### 产物与基线

- 按 Terra 报告的应用文件集合，对 `cmd/`、`internal/`、`web/src/`、`go.mod`、`go.sum`、`web/package.json` 和 `web/package-lock.json` 独立重算 SHA-256，结果为 `2b599113a1f8a68e259cd3cd1720940e0fd9cd8ff57afedaa97b310a23ee99ff`，与最终验证源码指纹一致。
- `/tmp/openagentx-n1-u1-bin-final` 仍可读取，独立计算 SHA-256 为 `cdb5ddef4a55d2a77c3afda2f2a5511dc90f3a6d36e5fc4d242b9b4ea3655fce`，与 Terra 报告的隔离运行二进制一致。
- ADR-002/003 SHA-256 分别保持 `e1e4cb8b2368be2ea17f66fb1ce33c0185c8d4be9484bc63a245494b0d547f1d`、`a538850fdf884a4d7a00ab92646c6a6c9e5374eb91ef35a0c7ffabf7e80ec99c`；冻结决策未变。
- 最终验证报告记录受影响 Go 测试、vet、race、前端 build/PWA 均通过；本审核不重复执行。N1 的 Register/Heartbeat 原子性、guard/lease、Backend 故障保活恢复及 plan/rebind/Begin 拒绝旧快照证据，与此前源码静态 GO 结论相互一致。

### U1 浏览器关闭证据

- Terra 使用最终二进制、新建隔离 SQLite/Unix Worker socket/SAN TLS 和真实 Chrome `143.0.7499.109`。测试数据只经正式 init、agent apply、认证 Control/Observe API 与 Unix Worker API 建立，没有直接改 SQLite，也没有把模拟 Worker 扩大表述为真实 Runtime E2E。
- 断流补验记录：浏览器离线期间由正式 API 追加一条消息，恢复后保持原 Task URL，消息计数从 223 变为 224，缺口消息在 DOM 中恰好一次。这与已审核的确认 sequence 恢复及 history/live 合并实现一致。
- 离线写保护记录：在线保留草稿后进入离线，textarea 和发送按钮禁用；离线期间及恢复后 Control `POST` 计数均为 0，消息仍为 224 条，没有离线排队后补发。
- 三视口与触控记录：1440x900、390x844、412x915 均完成真实渲染且无横向溢出；`hasTouch: true`、`navigator.maxTouchPoints > 0` 的 Chrome 上下文通过 `touchscreen.tap` 完成 390x844 列表进入详情、精确返回列表，并切换到 412x915。
- 归档包含且只包含五张脱敏 PNG。独立计算的 SHA-256 与最终验证报告逐项一致，图像尺寸分别为 1440x900、390x844 和 412x915；目视可见桌面详情以及两个移动视口的列表/详情状态，无明显横向溢出、控件遮挡或文本越界。归档未包含密码、Cookie、CSRF、私钥或 browser state。
- 223→224 与 Control `POST=0` 是 Terra 最终报告中的浏览器执行记录，静态 PNG 本身不能单独证明网络计数；本次交叉核对确认其源码、运行二进制、场景边界和归档哈希相互一致，不将截图扩大为网络 trace。

### 最终 Gate 判定

- **N1：GO。** 仅限 Register/Heartbeat/binding/Journal 事务、guard/lease、Backend 保活恢复、实际配置固定及 plan/rebind/Begin 竞态等已执行范围；不扩展为 N2 或完整 ADR-002。
- **U1：GO。** 仅限服务端筛选/keyset、详情 history/live 合并、SSE 断流恢复、离线写保护、URL/焦点/滚动稳定及三视口真实触控等已执行范围；不扩展为 U2。
- **N1/U1 合并 Gate：GO。** 独立源码审核与 Terra 最终自动化、隔离 API 和真实浏览器证据一致，首次及中间 NO-GO 均已由后续冻结修复和关闭补验消除。
- 未覆盖项仍为 N2、U2、真实模型 Runtime 和 E1 最终提交/部署验收，不能引用本 Gate 宣称这些范围通过。
