# AGY 活动取消确定性验证（D）

- 触发：正式 `agy-graft` 无 capture 的 R 取消已确认业务子进程停止，但 AGY interrupted / signal exit 被 `collect()` 当作 Runtime 未知失败。R 首次失败另见 `../live-14d7350-directcancel01/cases/cancel/`。
- 首次 D 复现：`01-reproduce-explicit-cancel.*`。真实 shell 父子进程、取消期间新增子进程、独立存活检查；新 canceled 断言在原产品上 FAIL。原结果仍为 uncertain，exit143 与空终态诊断完整保留。
- 修复：仅显式取消、无 timeout/context cancellation 竞争、清理无错误且根及已观察后代/进程组成员已核验停止时返回 canceled。Wait error 为 nil，避免通用 Worker 将已证实的停止重新改 unknown；原 parse/stderr/process-exit 诊断保留，`FinalReply=false`、`SideEffectsKnown=false`。自然退出后的迟到取消不改变结果，超时与清理错误继续 uncertain。
- 复验：`02-agy-race-retest.*` 覆盖 AGY 全包，包含真实进程树、截断 stream + interrupted exit1、清理故障注入、超时先/后到达、自然失败后迟到取消。`03-worker-sqlite-race.*` 覆盖 Worker 与持久层全包 race；`04-agy-worker-vet.*` 是静态检查。每条结果以各自 result.json 的 exit_code 为准。
- 边界：这是 D 证据，fixture 是真实 Linux 进程，但不是 AGY 模型调用；cleanup fault 是隔离单元注入。最终候选提交、可追溯构建、真实取消及取消后下一任务 R/I 由主流程继续，不能据此写 R/I PASS。检查范围是当前追踪树和原进程组，未声称任意恶意 daemon double-fork 可追踪。
- 修改源码的 SHA-256 在 `source-SHA256SUMS`，差异在 `fix.diff`。测试记录 HEAD 是当时基线，工作树中这两文件修改由差异和哈希确定，主代理随后提交。
- 原始持久目录：`/home/sky/.local/state/openagentx/validation/2026-10-02-agy-workflow/active-cancel-deterministic/`。本目录副本无运行凭据；首次失败、复验分别保留。
