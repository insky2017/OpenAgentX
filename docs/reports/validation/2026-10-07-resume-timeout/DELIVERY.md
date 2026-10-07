# Codex 持续执行与 Agent 恢复修复

## 当前结果

- Codex 默认不设总执行截止（`timeout: 0s`），任务正常执行到 Runtime 完成；显式正时限和人工取消继续有效。AGY 保持原约定。控制面冻结规格、Worker descriptor、CLI 配置和 Adapter 一致；不是把30分钟简单改成4小时。
- 已登记 Agent 的 `resume` 核对经认证安装中的正式身份、角色路径、workspace 和能力；不再依赖一次性接入回执的原始字节。未登记身份仍使用原注册保护。合法旧 Fleet 没有 `identity_file` 时，以正式档案核对，不伪造接入资料。
- 已在正式安装上用候选 CLI 验证 Rhythm、Pay、Quote 的 `resume --no-open` 成功，原 thread 保持，没有重跑业务任务。该项证明 CLI 恢复链路，不代表正式 Runtime 已完成升级。
- 真实长任务已通过只读复核：[结果](evidence/long01/recheck-result.json)。本地工具1860.005秒、正式Run1898.785秒、冻结timeout0/无deadline；仅一个Run succeeded。Task保留uncertain/business_effect_unverified，由独立文件证据核验本次测试效果。首次脚本列名错误保留为FAILED，没有重跑模型。联合工件及正式安装仍待下文最终结论。

## 为什么之前中断

旧 Worker 的30分钟配置进入冻结 Run 截止，Adapter 到点主动 `turn/interrupt`，即使仍在执行工具也会取消。现在默认没有这个总截止，只有显式配置才设置；显式超时结果标注 `deadline_exceeded`，不与人工取消混淆，也不自动重跑。

Rhythm 的旧 `.registered` 摘要与当前资料组合不符；Pay 的旧 receipt 不是合法 JSON。这些输入何时改变没有确证，不能归因于本次模型设置更新。日常恢复被错误地绑定到一次性引导资料，修复后使用正式登记身份；旧资料保持原样供审计。

## 验证边界

- 真实隔离31分钟任务使用独立 daemon、Worker、Codex app-server、正式 API；以原 Task/单 Run、模型真实工具日志、正式起终时间、文件效果和冻结规格共同判断，不能只信模型输出或文件里的自报时间。
- 初始验收脚本末尾把 SQLite 主键 `run_id` 写成 `id`。运行中的脚本不热改；保留首次夹具失败，再只读核验同一 Run，不重新执行31分钟工具动作。
- 真实 SQLite 集成覆盖损坏 receipt、合法 timeout/模型变化、旧 manifest，以及身份/角色路径/workspace/跨安装冲突。关键状态机覆盖显式超时和无限期任务人工取消；已有 AGY 正时限不放开。
- CLI 恢复与 Runtime 执行、Git 提交与实际安装、Run 成功与 mutation Task 的业务复核状态分别报告。全局默认无限不保证所有任务必然完成：显式用户取消、真实连接错误、宿主退出和不确定副作用仍按原契约处理。

## 相关并行事项

用户另行要求独立指挥者、tmux 两态 spinner、原生 TUI 待提交消息重复恢复修复。它们独立实施与验证，最后核对合并后的工件；不能用时限修复冒充消息确认已修复，也不能将新指挥者会话冒称旧 thread 已恢复。

## 证据

本机原始目录：`~/.local/state/openagentx/validation/2026-10-07-resume-timeout/`。脱敏归档及 SHA manifest 随最终验收补齐；待完成的真实验收不记 PASS。
