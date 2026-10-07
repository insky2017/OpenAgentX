# 新会话交接真实验收证据

- **结果 PASS，恰好 6 个真实模型 Run。** 独立 profile、daemon、一个 Worker、原生 Codex PTY 和随机 tmux socket；全部 Task 为 query/succeeded，且各有单 Run/Journal。见 [判定](real01/verdict.json)、[任务与线程账本](real01/task-accounting.json)。
- **主链通过。** preview 未改 Task/Run/绑定及 runtime 路由；busy 申请拒绝；交接发布新 thread/version 1；同 key 复用、旧 CAS 拒绝。首轮只确认身份、workspace 与 nonce，无工具；后续普通 query、managed 咨询、重启后 query 均精确回显同一 nonce 且进入新 thread。
- **原生终端与历史保持。** 旧 view 提交明确拒绝且未新增 Task；退出再 open 的实际 `resume` argv 指向新 thread。Worker 配置特意保留旧 `thread_id`，重启后真实 query 仍进入新 thread。旧 Task/Run/session_bindings 的只读行哈希保持，新旧绑定共存。
- **真实协作范围。** managed binding ID/Agent/peers/原凭据保持，generation 1→2，context/task/thread 更新。无 Worker 的 external peer 收到关联结果；没有运行第二 Worker 或验证 peer 自动续办。
- **首次夹具失败保留。** 两次 `peer-apply` 失败均在新增模型前：首次非交互密码输入失败，第二次使用该命令不支持的 `--password-file`。随后停止该路径，复用初始化已验证的 `Setup.cli` 私有 PTY 输入。始终复用同 root、已完成初始化 Task、nonce、peer ID，没有重跑初始化模型。
- **清理与导出。** 自有 Worker/daemon/tmux 已安全停止，并独立确认两个后台 Codex PID 已退出。完整私有证据保留于 `~/.local/state/openagentx/validation/2026-10-07-session-handoff/real01/`；本目录仅关键脱敏副本，没有凭据、数据库、完整 rollout、完整模型目录或原始 PTY 流。

## 固定输入与执行分段

| 项目 | 证据值 |
| --- | --- |
| 产品 source | `e644511f2763ebe7df40dc31f6efaf7a43067b2b`；全程产品二进制未换 |
| 已验二进制 SHA-256 | `d3dd1a32311badf5f053fdee2305c49bb84f6cd4138a01fa7810e9cedac241a1` |
| 初始 harness SHA-256 | `32a89dd0a555f5c9dcc2ce3a2aab54ea66357471c4a97da4eda0e3cd14337c3f`；保留在 [初始输入](real01/acceptance-inputs.json) |
| 最终实际 harness SHA-256 | `7fa99dfa051bef6dc98a2ebc603ae24f7e3b5c16ac730c1e78cb87cd3bf36149`；见 [最终判定](real01/verdict.json) |
| 初始分段（UTC） | 14:41:22 建夹具；14:42:12–14:42:25 初始化 Run 完成；14:42:27 首次认证夹具失败 |
| 第一次恢复（UTC） | 14:47:22 原 profile 重启恢复，0 新 Run；不支持的 CLI flag 失败，`restored_unconfirmed` 安全检查保留活进程 |
| 最终续验分段（UTC） | 14:50:19 复用已记录活进程；14:50:24–14:53:48 完成剩余 5 Run；14:53:51 PASS；14:54:06 清理完成 |
| old thread | `01a116d0-a0ae-7760-9449-41cf66043de9` |
| new thread | `01a116d9-48fc-7ce0-8bf1-015006451a70` |

恢复过程中有一次前置检查因发现尚存的自有进程而拒绝再次启动；之后按记录的 PID/starttime 确认并直接复用已有进程。不是 Runtime/产品失败，也没有派发模型。最终脚本修复了干净 root 的 peer 登记路径；没有另起 root 重跑整轮，避免超出 6 Run 预算。

**构建来源限制：** [构建检查](real01/checks/build-and-checks.json) 中内嵌 `vcs.revision=c5b93b7.../vcs.modified=true` 来自 linked worktree 外层仓库，**不作为 e644511 源码证明**。输入 source、固定工件 SHA 与内嵌 VCS 信息分开记录。主代理已补充[独立源码/工件映射](real01/checks/source-rebuild-proof.json)：335 个产品源码/依赖文件逐一与 e644511 相等，按原命令重建的 SHA 与实际验收工件完全一致。本批没有安装候选或改变正式业务 thread。

## 关键证据入口

| 核对项 | 原始依据的脱敏副本 |
| --- | --- |
| 六 Task/Run/Journal、线程与时序 | [账本](real01/task-accounting.json)，其各项链接对应 `tasks/`、`runs/` 与 `thread/` |
| preview 零写 | [前](real01/preview-before.json)、[后](real01/preview-after.json)、[历史前](real01/preview-history-before.json)、[历史后](real01/preview-history-after.json)；仅断言排除独立健康刷新时间 `state.updated_at` |
| 首轮收到交接且无工具 | [确认输出](real01/first-ack.json)、[仅 owned 新 thread 的 Runtime 事件](real01/handoff-first-turn-runtime.json) |
| 旧 view 拒绝/新 view | [旧 view 屏幕](real01/stale-native-rejected.rendered.txt)、[新 view 进程 argv](real01/new-native-process-proof.json)、[新 view 屏幕](real01/new-native.rendered.txt) |
| managed 保持及真实答复 | [绑定前](real01/managed-binding-before.json)、[绑定后](real01/managed-binding-after.json)、[关联咨询结果](real01/managed-consultation.json) |
| 重启仍用新 thread | [旧配置输入](real01/legacy-config-input.json)、[重启前](real01/restart-before.json)、[重启后](real01/restart-after.json)、[重开 argv](real01/restarted-native-process-proof.json)；第 6 项账本证明实际执行 |
| 旧历史不变 | [旧完成记录](real01/old-history-settled.json)、[最终记录](real01/history-after-restart.json) |
| 首败与恢复 | `real01/failure-*.json`、`real01/cli/peer-apply-*.json`、[PTY 登记](real01/peer-apply.json)、[恢复记录](real01/resume-after-initialization.json)、`real01/resume-live-initialization-*.json` |
| 清理及导出检查 | [清理](real01/cleanup.json)、[独立进程复核](real01/independent-cleanup.json)、[已知秘密匹配检查](real01/export-review.json) |
| 回归检查（与真实 Runtime 分开） | [固定构建及检查记录](real01/checks/build-and-checks.json)、[go test ./... 输出](real01/checks/go-test-all.log) |

下一步：主代理据本证据更新交付边界，不需要再次调用模型。未覆盖生产安装、peer 自动续办及全部取消/Runtime 失败组合；不得由本报告推断这些已通过。
