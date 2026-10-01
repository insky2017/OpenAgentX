# AGY 工作流实施与证据记录

## 用户结果与授权

2026-10-02 用户授权更新项目规则、提交计划后直接实施修复、使用现有开发凭据与真实端到端验收。范围见[修复计划](../../../plans/2026-10-02-agy-workflow-repair.md)与[22组用例](../../../plans/2026-10-02-agy-workflow-e2e.md)。目标是AGY日常工作流，CodeBuddy保留而不扩专项覆盖。

## 基线

- 新实施工作树：`OpenAgentX-workflow-worktree`，分支 `codex/agy-workflow`。
- 代码承接 `d0fd561`；合入 `main@2ddd14f` 的既有评估与架构文档，无冲突。
- 已产生修改前真实基线（7400806）及修复定向D证据；最终修复的独立R/I证据已在下文归档，不复用基线冒充通过。

## 进度

| 工作 | 状态 | 证据 |
|---|---|---|
| 规则与计划、强证据标准 | 已提交 `7400806` | AGENTS.md与上述两计划 |
| 无活动Run取消/取消后失联 | 已提交 `a32965d`，D/race通过 | [日志](evidence/cancel-deterministic/README.md) |
| AGY角色快照/时限 | 已提交 `76b72a9`，D/race通过 | [日志](evidence/agy-inputs-deterministic/README.md) |
| 人工验收/关联继续/权威就绪 | 已提交 `3a5edd7`，定向D/race通过 | [首次失败及复验](evidence/review-deterministic/README.md) |
| Agent入口/Console | 已安装 `17d5cd2`；I/R入口、角色更新、pause/resume、Console返回通过 | [入口I](evidence/installed-17d5cd2-onboarding01-resume02/README.md) · [Console I](evidence/installed-17d5cd2-console01/README.md) |
| R真实AGY基线 | 7400806连续两个query成功 | [API及Runtime证据](evidence/live-7400806-1002f/) |
| 浏览器基线 | CUA真实登录、发query、看到精确回复；隔离测试服务已清理 | [DOM/截图/API互证](evidence/browser-7400806-1002f/) |
| 真实AGY与安装 | `9434479` 已安装；R主链/恢复及I入口、Console、Web日用通过子项 | [安装](evidence/installation/installed.json) · [逐项覆盖](COVERAGE.md) |

## 执行记录

记录保留首次失败及修正，不覆盖旧结果。分批检查点用于收敛范围和报告进度，不因历史时间预算重新请求已授权工作。


### 实施中的发现与处置

1. 无Run取消曾把领域NotFound当数据库异常；已原子收口Task/mailbox。取消后失联保留uncertain，不宣称进程停止。
2. 人工验收首测发现活动Run空result_json先触发数据库扫描错误；先判活动Run后读取结果，复验通过。首次失败日志保留。
3. 新agent open与网络准备被旧CLI范围规则拒绝：仅增加overview/network overview读取及两条mode test/publish的owner+fleet.lifecycle写权限；其余网络管理仍维持原规则。
4. Web首次session/overview暂时失败支持重试；选择其它Task清除旧继续对象，避免带错父工作；运行结果按执行时间选择最新Run。
5. Console既有终态不可变校验会拒绝人工验收的metadata版本推进；已增加仅同终态、同原结果的兼容，不放开终态改写。
6. `--model`默认生成曾使用错误的单数字段，运行环境遗漏wrapper支持的NATIVE_PROXY；已修复并增加真实配置解析验证。

D：隔离确定性/集成；R：实际AGY执行；I：真实安装/交互。最终已安装 `9434479`，历史库Web首连阻断已按后文真实I复验修复；各批保持自己的版本与边界。


### 真实 AGY 暴露的问题与已提交修复

- `live-14d7350-main01`：角色输入、精确文件写入、验收幂等、关联继续和运行中补充均通过真实 AGY/API/文件互证。活动取消失败；父子进程虽已停止，Task/Run 仍 uncertain。完整首次失败见[主链证据](evidence/live-14d7350-main01/)。
- `live-14d7350-directcancel01`：去掉采集包装器、直接运行正式 `agy-graft`，仍复现同一取消问题。目标进程约 2 秒消失，超过原延迟仍无文件，任务状态未正确收口。[直接复现](evidence/live-14d7350-directcancel01/)。`980a534` 仅在显式取消且实际进程树已确认停止时记 canceled，超时/清理失败保留 uncertain；[D 首次失败及 race 复验](evidence/active-cancel-deterministic/README.md)。最终直接 AGY 复验见下文f3批次，原失败保留。
- `live-14d7350-timeout01`：正式 Worker 配置 35 秒时限，真实 60 秒工具被清理，超过原延迟无产物，同 Worker 下一问答成功。[超时证据](evidence/live-14d7350-timeout01/)。
- `recovery-14d7350-02`：真实长任务中断 daemon 后，旧任务正确 uncertain、物理进程停止，但新 Worker 仍只有旧代网络应用回执，无法 ready。`f927f6f` 修复入口，保留已发布 inherit/direct 模式并通过正式 test/publish 和版本 CAS 应用至当前代；命名网络保留原绑定并提供操作说明。[首次失败及后续恢复](evidence/recovery-14d7350-02/README.md)、[入口回归](evidence/network-generation-deterministic/README.md)。CLI 安装路径另验。
- 浏览器首次登录恰逢隔离 daemon 清理，网络连接拒绝却显示密码错误。`1b91722` 区分连接故障与 401/403；`ccd6a27` 区分取消请求、已停止和未确认。首次浏览器失败见[证据](evidence/browser-14d7350-attempt01/)。
- `E20` 正式 API/Worker 接本地 AGY 协议进程，7 类异常输出均保持 uncertain，之后同 Worker 正常任务均成功。该证据明确为 D，14 个真实 Task/Run 及原始 stdout/stderr 见[报告](evidence/e20-14d7350-r1/README.md)。
- 完整 Go race 初次运行唯一失败为旧 contract 测试仍期待静默接受 execution override，`8655051` 改正并限定复验 API 通过；其余包及 vet、模块校验、Web 检查通过。后续仅复验实际改动影响范围，不称全量重新运行。[完整首次日志及复验边界](evidence/final-deterministic/README.md)。


### 已安装验证及进一步收口

- `f3cd7a3` 正式 `agy-graft` 完整主链通过：角色、精确文件、验收幂等、关联继续、运行中补充、排队撤回、活动取消及同Worker下一query。[报告](evidence/live-f3cd7a3-main01/REPORT.md)。取消全树停止观测上限 4.005 秒；[35秒超时+下一query](evidence/live-f3cd7a3-timeout01/REPORT.md)也通过。保留先前14d失败。
- 崩溃R发现daemon只有启动时才Reconcile；`a022567` 改为每10秒复用同一保守事务。`17d5cd2` 的[定向R](evidence/live-17d5cd2-periodic01/REPORT.md)中，Worker SIGKILL后42.12秒自动uncertain，daemon不重启、旧工作不重派，新代网络和query成功。直接运行Worker的遗留子进程仍由测试精确清理，不能宣称该路径自动清理。
- [95秒真实工具与query补充](evidence/live-17d5cd2-lease02/REPORT.md)完成7个检查点，心跳和Run lease推进约90秒，原工具仅一次，两个Run同Worker。脚本整体因过严的纯marker断言FAIL，原件不改；独立复核已完成子场景PASS。
- [历史库一致副本迁移](evidence/migration-final/README.md)与[实际安装](evidence/installation/installed.json)通过。schema1→2，59条历史任务状态计数不变，quick_check=ok；运行PID/proc可执行文件与同源Go/Web哈希一致。配套旧程序、Web、DB备份在 `/home/sky/.openagentx/backups/agy-installed-20261001T190512Z`。
- [入口I](evidence/installed-17d5cd2-onboarding01-resume02/README.md)：三问新建→真实service ready、重复添加与canonical导入无重复；角色运行中修改仍保持旧Run快照，下一Run用新角色；A运行/B排队时pause，B无Run，resume新代网络后B只执行一次。systemd MainPID SIGKILL后的子进程自动停止，旧任务约32.66秒自动uncertain，daemon PID不变，后续query成功。两次夹具断言失败分别保留，续验没有重复原工具/故障。
- [Console I](evidence/installed-17d5cd2-console01/README.md)：实际80×24 PTY完成读取文件与角色、接受/拒绝、关联继续、退出重开、`/tasks`选回完整结果，无额外Task；[正式Observe补证](evidence/installed-17d5cd2-console01/formal-api/)保存两Task/Run/Journal。空闲15秒CPU ticks无增长。该批journal命令实际无条目，已如实标注，以PTY/API/文件/进程互证。
- [隔离真实浏览器](evidence/browser-f3cd7a3/README.md)：390px文件写入/接受/继续，320px拒绝/认证失效重登保草稿/精确query一次、UI补充两Run及取消无late；Offline禁写/不自动重发、初始session/overview各503后恢复、登录网络错误正确提示均通过。Web字节与17d5一致，但此证据不替代安装大库。
- `OAX:overview` 原空闲shell已启动真实 `agent status --watch`，显示各Agent就绪、当前/最近任务及open/resume入口；[迁移记录](evidence/installation/overview-migration.json)与[实际内容](evidence/installation/overview-after.txt)。未强杀其它活动pane。
- **安装Web首次失败（历史记录，最终复验见下）**：[大历史库页面](evidence/installed-browser-17d5cd2-attempt01/failure.json)登录后39次SSE均从0读取，持续连接中、无法发送。前端为防止不一致快照漏事件而忽略了overview末尾水位，首次只能回放全量旧事件。`9434479` 为overview提供读取前安全下界，首次流由此衔接，重连仍使用已确认序号；下列最终I复验已完成。


### 最终943安装与日用验收

- `9434479e4fd3b6802aa2e4d57610bb710200cd01` 在独立clean clone构建，`vcs.modified=false`。Go SHA256 `6725f3370b3c6bf27293a0f6368cc5bfac9e92c92682cc01a8b1b862231f75c7`；[最终安装](evidence/installation-sse-final/installed.json)与[独立核对](evidence/final-independent-9434479/result.json)确认PID406989、8个磁盘/HTTP Web工件一致、schema2保持、66条历史Task状态计数不变、quick_check=ok。配套备份 `/home/sky/.openagentx/backups/agy-installed-20261001T193510Z`，此前schema1配套回退备份仍保留。
- [最终真实浏览器日用](evidence/installed-browser-9434479-daily01/README.md) PASS：正式resume generation4，历史库首连267881→online；读材料生成1249字节报告→独立核验→接受→关联继续，Offline期间后台追加至2306字节、原文逐字保留→联网从已确认267966补回事件→接受→刷新返回→下一query精确返回材料/角色标记。正式API与SQLite只读确认3Task/3Run，B parent A、C独立，同Worker/PID/代次。全过程没有人工改DB状态。
- mutation的原Task `uncertain/business_effect_unverified` 保留，两个accepted review独立绑定对应Run；外部验收者核对真实文件，系统不凭模型声称完成而升为业务成功。问答为 `succeeded/query_result_delivered`。API取证首次DTO父字段假设错误FAIL与复验均保存，未重复执行模型任务。
- 最终D覆盖SSE安全下界竞态、旧历史事件隔离、Web15项、panel/race及vet/build；[D日志](evidence/sse-bootstrap-deterministic/)和[所有最终构建/影响边界](evidence/final-deterministic/README.md)归档。没有把此前全量初次失败改称最终全量通过。
- 演示Agent `agy-onboarding-e2e` 留为在线ready、当前无任务，用户可直接 `openagentx agent open agy-onboarding-e2e`；其他原本暂停业务Worker未启动。日常工作另用专用目录 `agent add`。原始/脱敏证据、SHA清单与[22组覆盖边界](COVERAGE.md)齐全，使用步骤见[日用指南](../../../operations/agy-daily-workflow.md)。本轮核心工作流收口，后续扩展项不改写为PASS。
