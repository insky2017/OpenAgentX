# Agent 清理：独立隔离真实验证

本报告只覆盖 2026-10-05 已执行的隔离环境验证（R），不代表生产安装验收（I）、业务验收或完整 ADR 通过。证据在本报告编写时从已停止的夹具整理；未重新运行测试或模型。主交付后续候选若改变，应另附影响分析与补验，不能把这里的 `3b6989e` 自动改为新版本。

要点：

- 旧 `886ba7f` daemon、Worker、Codex 后台与原生终端保持运行期间，新 CLI 完成 schema v5→v6 和一个目标 Agent 的历史清理；同一原生终端之后仍能输入并执行下一任务。
- 跨迁移任务及下一条 native mutation Task 的权威状态均为 `uncertain`。两条各只有一个 `succeeded` Run，完整答复和精确文件效果已独立核对；没有把 Task 改写或报告为 `succeeded`。
- 迁移前后 daemon、Worker、native、Codex PID/starttime、thread、Worker generation 全部相同。实际 WAL 写锁两段为 43.50 ms、52.83 ms，最大值低于夹具采用的 2 秒保守预算；这不是生产规模性能结论。
- 最终 release `3b6989eb65150e9360e9c734ebf7e1f9e3d7c60c` 在同一已迁移 v6 环境完成计划、删除、重复回执和共享引用拒绝补验；未新增模型 Run，保留 Task/Run/Journal 业务内容不变。
- 首轮夹具失败、后续脚本断言错误与恢复均保留；两个夹具的自有进程及专属 tmux 已安全退出，验证数据库和原文仍在本地。

## 工件与复用关系

| 层次 | 工件与来源 | 验证范围 |
| --- | --- | --- |
| 被保留的旧运行时 | `openagentx-886ba7f`，SHA-256 `15fd8699c2f07af80966dc3edc355ee90f260e109c9715f402aedd16b90d97ca`；历史安装副本，Codex CLI `0.160.0`、`gpt-6-astra / ultra` | 真实 daemon、Worker、native 和模型工具持续性 |
| 活动模型期间执行的候选 | `candidate-working-02` 冻结为 fixture02 的 `candidate-under-test`；完整 SHA 和选定源码哈希见 provenance | v5→v6、精确 purge、共享父任务引用拒绝、最大锁持有时间 |
| 最终 release | commit `3b6989eb65150e9360e9c734ebf7e1f9e3d7c60c`；SHA-256 `ddfaff6bd4370639b40bb6c2760e02e9245d948a2c62d149a4b2328a7a9479a7`；`vcs.modified=false` | 已 v6 夹具的最终 CLI 计划、删除、重放回执及保留状态核对 |

旧运行时来源见 [provenance](evidence/isolated/live/provenance.json)，活动期间候选见 [candidate-provenance](evidence/isolated/live/candidate-provenance.json)，最终 release 见 [final-candidate-provenance](evidence/isolated/final/final-candidate-provenance.json)。早期工作树构建的 Go buildInfo 错误嵌入父仓库 `c5b93b7...` 且 `modified=true`；不能把该字段视作候选源码提交。该层保留实际二进制 SHA 与当时选定源码哈希，不冒充 clean release。

最终 release 的源码哈希对照确认 `006_agent_removal.sql`、迁移执行器、`migrations.go`、CLI `agent_remove.go` 与 `agent_native.go` 相对活动期间候选字节不变。变化集中于本地归属/重试规划、领域返回结构、repository 共享引用与回执处理及对应测试。因此本报告复用此前“真实活动模型跨迁移并继续 native 输入”的证据，再用最终 release 覆盖已变更 CLI 路径；未声称最终 release 重新执行了一次 v5→v6 或模型任务。

## 真实运行、输入与效果

保留 Agent 为 `cleanup-keep-9d6ee7`，thread 为 `01a10a50-cf58-7762-818e-a6554c01435c`，Worker instance 为 `worker-12f05b6f-2681-4efe-aa65-2391c994e032`、generation `1`。

| 流程 | 权威 Task | Run 与实际效果 | 证据 |
| --- | --- | --- | --- |
| 初始化原生终端 | `task-aa507ddd-b2db-4f7f-b7f8-03bc52a86ed8`：`succeeded`（query） | 原 thread 角色/工作区确认 | [初始化 Task/Run/Journal](evidence/isolated/live/native-initialization.json) |
| 工具活动中迁移 | `task-dd82d239-1d98-444b-9808-49af51a0827c`：运行中→`uncertain` | `run-ab12dd37-5fb0-49c5-867f-e6674aa345fb`：`succeeded`，`final_reply=true`；工具先写 before-proof，等待夹具释放，再写 after-proof | [迁移前 running](evidence/isolated/live/active-task-before-migration.json)、[终态 Task/Run/Journal](evidence/isolated/live/active-task-completed.json)、[独立 Run](evidence/isolated/live/active-run.json) |
| 同一 native 下一任务 | `task-09a46163-f230-4482-9c78-091bf37acb64`：`uncertain` | `run-835cc9dc-518f-410b-a2ee-a15394c952ff`：`succeeded`，`final_reply=true`；实际 Python 写 next-proof，完整精确答复，10 条 Journal | [native 下一任务](evidence/isolated/live/native-next-task.json)、[终端渲染](evidence/isolated/live/native-final.rendered.txt) |

两条 mutation Run 均报告 `side_effects_source="not_recorded"`、`business_verification_source="not_recorded"`，Task 的 `completion_basis` 为空。这是旧运行时保守记账的事实。独立文件核对支持本次隔离效果正确，但不改变权威 Task 的不确定状态。

原生操作有实际命令记录：[建立专属 tmux](evidence/isolated/live/tmux/0001.json)、[启动 native](evidence/isolated/live/tmux/0005.json)、[唯一一次字面输入](evidence/isolated/live/tmux/0018.json)、[首次 Enter](evidence/isolated/live/tmux/0019.json)、[提交已有草稿](evidence/isolated/live/native-draft-submit.json)、[最终 capture-pane](evidence/isolated/live/tmux/0026.json)。没有重新发送下一任务内容。模型的角色、工作区、模型强度、turn 完成事件见 [精确自有 thread 摘录](evidence/isolated/live/runtime-owned-thread.json)；原始 PTY 留本地，其路径和哈希列入 manifest。

独立文件证明包含 [before-proof](evidence/isolated/live/proof/before-proof.txt)、[after-proof](evidence/isolated/live/proof/after-proof.txt)、[next-proof](evidence/isolated/live/proof/next-proof.txt)。前两者精确为 `OAX-CLEANUP-LIVE-cc572f0a71d3` 加一个 LF；后者为 `OAX-CLEANUP-NEXT-6e5cb6722e93` 加一个 LF。[字节与 SHA 核对](evidence/isolated/proof-check.json)是整理时对保留文件的只读复核，没有重新触发副作用。

## 在线迁移与隔离边界

通过正式 CLI 执行 `agent remove <exact-id> --purge-history --dry-run`，再以 `--yes --wait 30s` 执行。清理 `cleanup-delete-9d6ee7`，其专属身份/配置及一条已取消 Task 历史被删除，业务 workspace 保留。目标规划数量为 1 Agent、1 Task、0 RunAttempt、6 Event Journal、1 Mailbox item；[预览](evidence/isolated/live/delete-preview.json)和[完整 CLI 执行记录](evidence/isolated/live/online-removal.json)保留实际命令、计数、步骤及退出码。

`cleanup-shared-9d6ee7` 的任务引用保留 Agent 的父任务；[共享引用预览](evidence/isolated/live/shared-boundary-preview.json)和[拒绝记录](evidence/isolated/live/shared-boundary-refused.json)证明删除在生命周期动作及迁移前被拒绝。此时 schema 保持 v5；之后仅对允许删除的目标执行迁移与 purge。

[迁移前](evidence/isolated/live/active-before-migration.json)、[迁移后](evidence/isolated/live/active-after-migration.json)及[native 下一任务后](evidence/isolated/live/final-continuity.json)记录同一组进程 PID/starttime、native writer lock、thread、Worker instance/generation 和保留配置 SHA。清理 CLI 总耗时约 2.187 秒，其中最大 SQLite WAL 写锁为 0.052829 秒。锁证据来自实际 `strace -f -ttt -yy -e trace=fcntl` 的 [WAL byte 120 调用摘录](evidence/isolated/live/wal-byte120.fcntl.log)，时间区间见[锁预算计算](evidence/isolated/live/wal-write-lock-budget.json)。原始完整 trace 的本地路径和 SHA 可由 manifest 回查。

本夹具以自有进程启动 daemon/Worker，没有真实 systemd instance 的启停验收。wrapper 只允许对精确 `cleanup-*` 实例执行真实 `/usr/bin/systemctl --user show`，其他操作全部拒绝；无归属、inactive 的共享模板被 preserved。它没有伪造 systemd 状态，也不能证明生产 `disable --now` 已通过。上述锁耗时只适用于这个小目标集，不能外推大量历史数据、生产并发或更大数据库的 deadline 表现。

## 最终 release 最小补验

复用已 v6、原 daemon/Worker/native 未重启的 fixture02，通过最终 release 的正式 `agent join --prepare`、`agent apply` 和公开 Control API 新建 `cleanup-final-fc74d9`，创建并取消 `task-f092be12-fdf1-44a2-b5ea-cc1b990159e0`；目标没有 Worker、没有 Run、没有模型执行。证据包括 [join](evidence/isolated/final/final-candidate-join.json)、[apply](evidence/isolated/final/final-candidate-apply.json)、[取消 Task](evidence/isolated/final/final-candidate-canceled-task.json)。

[最终计划](evidence/isolated/final/final-candidate-plan.json)、[首次删除](evidence/isolated/final/final-candidate-remove.json)和[重复执行](evidence/isolated/final/final-candidate-repeat.json)证明目标删除完成、workspace 保留、重复回执 `replayed=true` 且 digest/counts 一致；首次 remove 约 0.454 秒，repeat 约 0.395 秒。[共享引用计划](evidence/isolated/final/final-candidate-shared-plan.json)和[实际拒绝](evidence/isolated/final/final-candidate-shared-refused.json)证明禁止删除的目标仍在生命周期动作前被阻断。

[前置进程状态](evidence/isolated/final/final-candidate-before.json)与[后置复核](evidence/isolated/final/final-candidate-recovered-after.json)保持相同；[保留任务前](evidence/isolated/final/final-candidate-tasks-before.json)和[后](evidence/isolated/final/final-candidate-tasks-after.json)的四条 Task、Run、messages、Journal 业务内容完全相同。仅 Observe envelope 的全局 `snapshot_sequence` / `live_after_sequence` 因新建与取消测试任务而从 373 增至 381，比较时排除这两个全局游标。[最终判定](evidence/isolated/final/final-candidate-verdict.json)明确新增模型 Run 为零。

本层没有新建网络资源、制造文件删除故障或自定义目录重叠；因此不单独宣称这些分支在真实隔离 E2E 中通过。其实现/集成验证由对应交付报告说明。

## 失败、恢复及停止

1. 首轮 `fixture` 继承了禁止工具的冻结角色；模型明确拒绝工具，没有生成迁移证明文件，未执行迁移，schema 仍为 v5。[失败记录](evidence/isolated/failures/fixture01-failure-execute-1791174308856209549.json)与[模型拒绝原文](evidence/isolated/failures/fixture01-runtime-owned-thread.json)保留。修正 harness 在 join 前写入正确角色后，建立独立 fixture02；没有复用失败夹具冒充通过。
2. fixture02 首次完成真实工具后，harness 错把 mutation Task 必须 `succeeded` 作为断言而失败。[原失败](evidence/isolated/failures/fixture02-failure-execute-1791174518662448062.json)保留；随后按 Run 完整终态和独立文件核对验证，不修改 Task。
3. 下一条 native 输入先停在粘贴草稿；确认后仅补一次 Enter。另一处等待代码误读列表 API 的 `content`（实际列表仅有截断 summary），改用详情 API 后收齐证据。两次[超时记录一](evidence/isolated/failures/fixture02-failure-continue-verification-1791174762795961172.json)、[超时记录二](evidence/isolated/failures/fixture02-failure-continue-verification-1791174932915278507.json)均保留；没有重复发任务或重复迁移。
4. 最终 release 首个脚本比较了含全局游标的完整 Observe envelope，误判差异；[脚本失败说明](evidence/isolated/final/final-candidate-harness-failure.json)记录纠正，只读取已有证据和正式 API 再核对，没有重做删除。

收尾按已记录 PID/starttime 核对，只停止隔离 daemon、Worker、native、Codex 和其子进程及专属 tmux。fixture01 原进程已停止；fixture02 的 10 个自有进程均退出，无剩余运行 PID。[fixture02 前置核对](evidence/isolated/cleanup/fixture02-cleanup-final-preflight.json)、[实际 stop](evidence/isolated/cleanup/fixture02-owned-cleanup.json)、[tmux kill](evidence/isolated/cleanup/fixture02-tmux-kill-server.json)、[最终收尾](evidence/isolated/cleanup/fixture02-cleanup-final-verdict.json)及[fixture01 收尾](evidence/isolated/failures/fixture01-cleanup-final-verdict.json)可复核。数据库、workspace 和证据全部保留；未停止生产或共享会话。

## 原文、脱敏与可追溯性

本地原文根为 `/home/sky/.local/state/openagentx/validation/2026-10-05-agent-cleanup/live-independent/`；成功夹具证据在 `fixture02/evidence/`，首轮失败在 `fixture/evidence/`。Git 中只保存必要副本，不复制验证数据库、凭据文件、`.env` 内容、Cookie、CSRF、fencing token 或完整运行环境。CLI 文件清单中的 `.env` 路径仅用于说明删除归属，不包含内容；内部 installation ID 也已脱敏。

[SOURCE-MANIFEST.json](evidence/isolated/SOURCE-MANIFEST.json)逐项记录源文件绝对路径、原文 SHA-256、副本 SHA-256、变换说明，以及仅本地保留的 PTY、数据库和补验脚本路径/哈希。[SHA256SUMS](evidence/isolated/SHA256SUMS)覆盖入库证据副本。原始 PTY 与模型本地历史不整体入库；以实际 tmux 操作、终端渲染、精确自有 thread 摘录和正式 API 数据共同支撑结论。

下一步：

- 主交付关联本报告与最终实际安装/生产清理证据，分别报告；本报告不推断生产效果。
- 最终实现若继续变更，按差异补充必要验证与工件绑定，保留本报告的历史候选和失败事实。
