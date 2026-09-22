---
doc_type: contract_review
status: decision-required
owner: openagentx
updated_at: 2026-09-23
---

# Task01：ADR-006 契约审查与待决安全语义

## 审查结果

用户已授权实施ADR-006，但现有[ADR-006](../../decisions/ADR-006-task-intent-and-verifiable-terminal-semantics.md)
同时要求query基于结果证据成功、且query不能绕过副作用审计。当前AGY路径不能证明工具执行是只读。
不能把用户的实施授权解释成已接受降低后一条边界，因此query终态实施等待此项决定；不依赖该选择的
显式intent/schema/API/入口基础接线已按用户继续指令开始，仍保留原结算行为。

## 可以按原边界实施的确定合同

| 项目 | 决策 |
|---|---|
| 声明人 | 已通过现有角色/scope/CSRF鉴权的正式任务创建调用者；不信任模型或自然语言关键词自动选择 |
| API | `POST /api/control/v1/tasks`增加显式`intent`，仅`query`或`mutation`；省略为mutation，非法/显式空值拒绝 |
| 持久化 | Task保存intent；创建后不可改；同幂等key而intent不同须冲突；Journal保留创建时意图 |
| 旧数据 | 现有Task均为mutation，不改状态/result/error，不重试；CurrentVersion=1兼容ensure和空库schema一起更新 |
| 升级证据 | 事务化添加列/约束，完整旧v1 reopen、重复Apply、部分对象拒绝及故障回滚；只用临时DB验证 |
| 入口 | Web显式任务类型选择；Console `/dispatch --intent query <content>`；普通`/dispatch <content>`仍mutation |
| 后续控制 | steer/补充指令/审批/取消不能改变原Task的intent；同一Task的所有Run继续受原合同约束 |
| 显示 | 正式Task投影提供intent和服务器产生的完成依据；UI不能根据Runtime body自行改Task状态 |
| mutation | 保留现有SideEffectsKnown保守结算、fencing/CAS/审批及终态规则，Runtime自报不变成独立核验 |
| query基本结果证据 | 必须有唯一有效终态、非空且未截断的最终安全结果，无解析/进程/传输/Journal错误；不能用增量文本代替缺失终态body |
| 竞态 | 同事务结算Task/Run/Journal；重复finish幂等；cancel、pending Message、waiting_input与uncertain优先级单独测试 |

上述合同仍不足以决定query进入succeeded的完整条件；下一节是唯一阻断性的语义决定。

## A06-01：query的成功究竟证明什么

| 合同 | 成功含义 | 当前AGY能否直接满足 | 必须做的工作 |
|---|---|---|---|
| A：保留原副作用边界 | 完整查询结果，且执行受可验证的只读限制，不能用query绕过变更验收 | 不能；现有工具权限与证据不足 | 先建立并验证Runtime/工具执行限制或等价可信副作用证据；未受支持的Backend拒绝query成功 |
| B：明确改为结果交付合同 | 有权限调用者显式选择query，succeeded仅表示完整回复已交付；副作用仍未核验 | 完整结果解析可以支持，尚需实现/测试 | 明确修订ADR安全语义，持久化query_result_delivered并在API/UI标明验收范围；不能称只读保证或业务变更已验证 |

B不能悄悄解释成A：一个实际执行写操作的任务可能被调用者标为query，完整返回后进入succeeded；
仅同时显示not_recorded不能阻止这个绕过。已知写操作转uncertain也不能补足当前未知工具遥测的缺口。
因此B涉及原不变量的实质调整，需要用户明确接受；A则需要新增经过实测的Runtime限制能力，不能只
把`--mode plan`或`--sandbox`写进argv后宣布已证明。

不得用强制SideEffectsKnown=true、读取提示词“只读”、把前端徽标改绿或直接改历史Task规避该决定。
主代理推荐B供用户决定，尚未按B修改ADR或实现query成功捷径。基础接线可先独立验证，query终态
只围绕用户确定的合同继续，不重做已完成盘点。

### 主代理推荐的可审查合同（尚未接受）

- `query`的交付物是回复。仅当Runtime有唯一有效成功终态、最终body非空且未截断、解析/进程/事件
  持久化无错误时，才可记录`query_result_delivered`；增量文本不替代最终body，模型自报不替代证据。
- `mutation`的交付物是可核验的业务效果，继续原保守规则。默认mutation，任务创建后不能换类型，
  补充指令/重连/审批不改变该合同，旧Task不迁移为query或重算成功。
- query成功只表示完整回复交付，不保证答案真实、执行全程只读或业务变更已核验。呈现必须明确
  区分任务结果、Runtime结果和副作用核验；不能将未知副作用标记为无副作用，也不能省略原审计记录。
- 类型由有权创建任务的调用者显式选择；不提高Runtime权限、不取消审批，不根据提示词或模型输出
  自动选择query。要求强制只读的使用场景必须另外证明执行限制，当前AGY不作此承诺。

推荐原因是用户当前要解决的“查询有明确回复但Task仍uncertain”属于回复交付验收。把它与任意工具
副作用证明混为一个成功条件，会在当前AGY能力下持续无法闭环；反过来，也不能以回复交付的成功
徽标声称执行安全已证明。该取舍涉及原ADR不变量，等待用户明确接受后才实施终态变化。

## 当前代码和实际合同证据

| 证据 | 事实与影响 |
|---|---|
| `internal/persistence/sqlite/worker_execution_repository.go` / `FinishRun` | Runtime succeeded且SideEffectsKnown=false时Task uncertain/business_effect_unverified；当前部署与此代码一致 |
| `internal/runtime/agy/adapter.go` / `Validate`、`StartTurn` | 拒绝非空ExecutionSpec.Sandbox；实际argv总带dangerously-skip-permissions |
| `internal/runtime/agy/stream.go` / `parseStreamJSON`、`decodeRecord` | 提取status/body/usage及Runtime自报side_effects_known；未建立完整可信tool/argv/operation合同 |
| `internal/api/console/task_projection.go` | Runtime声明仅为runtime_reported，业务核验not_recorded；不应升级为独立证明 |
| `agy-graft --version` | 本机1.2.8，退出0，约0.24秒；未启动模型任务 |
| `agy-graft --help` | 有mode accept-edits/plan与sandbox（terminal restrictions），不承诺完整只读或date argv白名单；退出0，约0.22秒 |
| 独立边界审查 | 当前step_update/Journal不足以证明未执行写工具；未知工具证据不能按“没有副作用”处理 |

未执行真实Runtime权限探测，未读取隐藏推理、原始会话日志、token/password或代理凭据。不存在
“sandbox已验证”或“完整工具日志已具备”的通过结论。

## 实现后最低验证

1. 正式API/CLI/Web显式query和默认mutation；非法输入、无权限、幂等差异与不可变intent。
2. query完整结果成功；空/截断/缺终态/进程非零/解析或Journal失败不伪成功；采用A时还必须包含真实
   只读限制及写操作拒绝测试，采用B时必须验证完整的验收范围投影且不冒充副作用已核验。
3. mutation原矩阵、finish/cancel/message优先级、事务回滚、重复结算、旧库升级及重开。
4. 正式Adapter隔离fixture、PC/手机真实浏览器、受影响定向/race/全量Go/Web/release，最后才构建安装。
5. 新低副作用Task通过正式入口到达符合所选合同的Task succeeded及页面回复，同一Worker连续领取；
   不以本次docs审查或旧Run succeeded代替该证据。
