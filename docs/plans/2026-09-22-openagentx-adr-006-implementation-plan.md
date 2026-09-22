---
doc_type: implementation_plan
status: active
owner: openagentx
updated_at: 2026-09-23
---

# ADR-006：显式任务意图与成功终态实施计划

## 用户结果、授权和边界

用户在真实网页收到时间回复后指出Task仍为uncertain，并于2026-09-22明确授权“好，执行006”。
本轮结果是：用户能显式创建预期只读的query任务，系统依据完整、持久化且符合该任务契约的结果证据判定
成功；mutation保持既有业务效果核验要求。既有任务不自动改为query，不重算终态，不自动重试。

默认intent为mutation；不得从自然语言、模型自报、Agent名称或页面筛选猜测query。query不能提高
权限、绕过审批或把实际工具变更冒充无副作用；证据不完整、截断、未知或矛盾必须保守结算。query
所需的最小可证明证据在Task01冻结，不能仅凭CLI的plan/sandbox选项名称宣称已实现只读隔离。

基线来自已验证ADR-009 tip `34053c02042ca7448587ff8c20bdd82c11ed870c`。独立分支
`codex/adr006-task-intent`，worktree `/home/sky/work/touzi/OneAxe/OpenAgentX-adr006-worktree`。
开始时main的README和未跟踪架构文档属于用户现场；本轮不修改main、其他worktree或steadyflow父仓。
既有安装授权和低副作用真实验收范围继续有效；不push/merge，不扩大网络暴露，不改ADR-007/009。

## 执行与停止条件

本批从2026-09-22 22:50 CST起，预算120分钟；一次主要实现、一次独立验证，失败集中修复并定向复验。
先完成契约和边界核对，再写产品代码；阶段提交独立，不amend既有提交。用户已授权推进本ADR，不
人为增加重复确认；安全证据或部署前提无法证明时，停止依赖工作并报告具体阻断，不降低验收。
真实Runtime按分钟等待，不高频轮询、不自动重复业务请求；预算不因代理、夹具或会话变化重置。

## 阶段表

| Task | 结果 | 状态 | 关键证据 |
|---|---|---|---|
| 01 | 冻结intent/API/schema/终态/Runtime证据合同，记录授权与受支持范围 | blocked | [契约审查](2026-09-22-openagentx-adr-006-implementation/TASK-01-CONTRACT-REVIEW.md)已完成；A06-01需要确定成功与副作用边界 |
| 02 | 正式API到DB/Worker结算再到Console/Web显示的完整实现 | active | 类型/持久化/API/Console-Web基础接线独立验证通过；query终态待A06-01决定 |
| 03 | 独立验证、浏览器和隔离Runtime闭环、候选构建 | pending | 基础接线已有全仓/race/vet/build/release/Web/浏览器证据；完整query Runtime闭环及候选未开始 |
| 04 | 已授权范围内安装和低副作用真实验收、文档收口 | pending | 新query Task succeeded且有回复、正式Task/Run/Journal、连续领取、进程与产物一致 |

阶段状态必须与[执行日志](2026-09-22-openagentx-adr-006-implementation/EXECUTION-LOG.md)一致。
实现提交不能自包含自身SHA；后续证据提交记录精确SHA。候选、已安装、真实用户确认和未完成项分开报告。

当前范围：用户再次要求继续实施后，先贯通不依赖A06-01选择的显式intent、存储、API、Console和Web。
query成功的安全语义仍待确定；现有AGY没有可验证的只读执行边界，Worker结算保持现状。本轮这部分
可独立验证并提交，但不是完整ADR交付，不能安装为query成功已实现。不能将普通实施授权当作对安全
语义调整的默认同意。

本批基础接线的最小验收：正式创建请求保留显式intent；省略为mutation；非法值、不同intent幂等重放
及修改intent均拒绝；v1迁移不改旧Task结果；Console/Web传递用户选择并展示持久化值；原结算未改。
基础接线通过后独立提交，后续query终态实施继续使用同一预算，不重置为新一批。

## 本轮验收矩阵

| 边界 | 正向及必要负向证据 |
|---|---|
| 声明与权限 | 正式创建API显式query/mutation；缺省mutation；无效值拒绝；viewer禁写；查询不提升现有role/scope |
| 持久化 | 空库与完整v1升级、重复Apply、损坏对象拒绝、故障回滚；历史Task默认mutation且状态/result不改 |
| 意图不变量 | 创建后不可变；幂等比较包含intent；相同key不同intent冲突；消息、审批与重连不改变intent |
| query结算 | 完整成功结果按冻结证据进入succeeded；空/截断/错误/不完整证据/工具变更不伪成功 |
| mutation结算 | 已有SideEffectsKnown与保守终态不弱化；Runtime自报和query声明不得代替业务核验 |
| 竞态 | Task/Run版本与fencing；finish/cancel、finish/message、重复finish、事务故障及等待状态 |
| 呈现 | Console与PC/手机Web可显式选intent；详情显示持久化intent/判定依据/结果；旧Task仍如实uncertain |
| 资源 | 一次正式提交、连续两任务、退出观察不控制Worker；无token/password/raw stderr进入证据 |
| 真实效果 | 用户入口→领取→Run终态→Task正确终态→页面回复；不以fixture、入队或Run成功替代Task成功 |

## 验证范围

代码改动后运行受影响domain/api/controlplane/sqlite/migrations/runtime/worker/console的定向及race；
集成后运行`go test ./... -count=1`、`go vet ./...`、`go mod verify`、独立`go build`、release scanner、
Web observation/network/PWA测试及production build。浏览器使用隔离控制面和真实渲染验证PC/窄屏与
认证/离线写保护；Runtime合同先使用正式Adapter临时fixture，真实验收只使用低副作用、可核对输入。

本轮非目标：实现任意业务变更的通用验证器、自动识别任务意图、迁移旧uncertain任务、网络代际继承、
Diagnostic跨Task归属、Foreground或tmux业务控制。已知LIVE-02继续独立记录，不用本ADR扩大范围。
