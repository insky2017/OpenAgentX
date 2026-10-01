# AGY 工作流实施与证据记录

## 用户结果与授权

2026-10-02 用户授权更新项目规则、提交计划后直接实施修复、使用现有开发凭据与真实端到端验收。范围见[修复计划](../../../plans/2026-10-02-agy-workflow-repair.md)与[22组用例](../../../plans/2026-10-02-agy-workflow-e2e.md)。目标是AGY日常工作流，CodeBuddy保留而不扩专项覆盖。

## 基线

- 新实施工作树：`OpenAgentX-workflow-worktree`，分支 `codex/agy-workflow`。
- 代码承接 `d0fd561`；合入 `main@2ddd14f` 的既有评估与架构文档，无冲突。
- 已产生修改前真实基线（7400806）及修复定向D证据；最终修复候选R/I仍须单独验收，不复用基线冒充通过。

## 进度

| 工作 | 状态 | 证据 |
|---|---|---|
| 规则与计划、强证据标准 | 已提交 `7400806` | AGENTS.md与上述两计划 |
| 无活动Run取消/取消后失联 | 已提交 `a32965d`，D/race通过 | [日志](evidence/cancel-deterministic/README.md) |
| AGY角色快照/时限 | 已提交 `76b72a9`，D/race通过 | [日志](evidence/agy-inputs-deterministic/README.md) |
| 人工验收/关联继续/权威就绪 | 实现中，定向D/race通过 | [首次失败及复验](evidence/review-deterministic/README.md) |
| Agent简化入口/Console/Web | 实现及交互接线中 | Web定向测试和构建通过；CLI PTY真实daemon+systemctl stub仅D |
| R真实AGY基线 | 7400806连续两个query成功 | [API及Runtime证据](evidence/live-7400806-1002f/) |
| 浏览器基线 | CUA真实登录、发query、看到精确回复；隔离测试服务已清理 | [DOM/截图/API互证](evidence/browser-7400806-1002f/) |
| 最终候选R/I、正式安装 | 待执行 | 真实mutation/continue/queued/cancel/timeout脚本已准备；未记PASS |

## 执行记录

记录保留首次失败及修正，不覆盖旧结果。分批检查点用于收敛范围和报告进度，不因历史时间预算重新请求已授权工作。


### 实施中的发现与处置

1. 无Run取消曾把领域NotFound当数据库异常；已原子收口Task/mailbox。取消后失联保留uncertain，不宣称进程停止。
2. 人工验收首测发现活动Run空result_json先触发数据库扫描错误；先判活动Run后读取结果，复验通过。首次失败日志保留。
3. 新agent open与网络准备被旧CLI范围规则拒绝：仅增加overview/network overview读取及两条mode test/publish的owner+fleet.lifecycle写权限；其余网络管理仍维持原规则。
4. Web首次session/overview暂时失败支持重试；选择其它Task清除旧继续对象，避免带错父工作；运行结果按执行时间选择最新Run。
5. Console既有终态不可变校验会拒绝人工验收的metadata版本推进；正在增加仅同终态、同原结果的兼容，不放开终态改写。
6. `--model`默认生成曾使用错误的单数字段，运行环境遗漏wrapper支持的NATIVE_PROXY；正在修复并增加真实配置解析验证。

D：隔离确定性/集成；R：实际AGY执行；I：真实安装/交互。当前生产daemon仍是旧版本；不以源码或D测试通过宣称用户现场已经更新。
