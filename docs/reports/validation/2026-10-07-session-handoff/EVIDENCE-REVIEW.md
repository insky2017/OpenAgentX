# 新会话交接独立证据复核

## 结论

- **PASS（限定范围）**：隔离身份 `terminal-e2e-6638de` 的六次真实 query Task/Run 支持新会话交接、后续正式 API 路由、managed 咨询答复、Worker 重启后保持新 thread 的结论；未发现阻断本批候选交付的证据缺口。
- 原生 PTY 和进程 argv 支持“旧 view 键盘拒写、新 view 与重启后 view 打开正确新 thread”。新 view 后续两次 nonce query 由正式 API 提交，不能据此宣称新 view 键盘提交正向流程也在本批重新验过。
- 旧 Task、Run、SessionBinding 的两组历史行哈希前后一致，活动指针由版本 0 更新到 1；失败夹具记录保留，六次模型执行没有被重跑来覆盖首次失败。
- 本次为独立证据复核：只读现有证据、保留工件、指定源码 commit 与隔离数据库；未调用模型、未重建工件、未重复测试、未修改产品或运行态。唯一写入为本报告。
- **未部署生产，未切换业务 Agent**；本结论不替代全部取消、Runtime 失败、取消 fallback 重连矩阵或业务执行验收。

## 来源及完整性

复核日期：2026-10-07。产品源码为 `e644511f2763ebe7df40dc31f6efaf7a43067b2b`，真实候选 SHA-256 为 `d3dd1a32311badf5f053fdee2305c49bb84f6cd4138a01fa7810e9cedac241a1`。

- 独立将 [source-rebuild-proof.json](evidence/real01/checks/source-rebuild-proof.json) 中 335 个源码/依赖文件的 SHA-256 与该 commit 的 Git blob 逐一比较：无差异；另读保留候选文件计算 SHA-256，与上述值一致。
- 原执行者记录了原命令重建与真实工件字节一致。本次没有再构建；内嵌 VCS 指向外层旧 revision 的异常保留，**不将内嵌 VCS metadata 用作源码证明**。
- 对 [SHA256SUMS](evidence/SHA256SUMS) 中 90 项逐一重新计算：全部相符。脱敏范围与原始资料目录见 [export-review.json](evidence/real01/export-review.json)。
- [runtime-processes.json](evidence/real01/runtime-processes.json) 记录隔离 daemon 与重启后 Worker 的 `/proc` 工件 SHA，与真实候选一致；[provenance.json](evidence/real01/provenance.json) 记录 Codex CLI `0.160.1`、模型 `gpt-6-astra` 及隔离 tmux server。历史 harness SHA 变化见首次失败与续跑记录，不能将最终脚本 SHA 当作全部前置步骤的原执行脚本。

## 六次真实执行与路由

逐一核对 [Task/Run 索引](evidence/real01/task-accounting.json) 指向的六份 Task、六份 Run、对应 Worker thread 状态：每个 Task 仅一个 Run，均为 `query`、`succeeded`、`query_result_delivered`，模型均为 `gpt-6-astra`；每份 Task 事件包含 created、running、run started、run finished、settled。只读隔离数据库再次计数为六 Task、六 Run。

| 流程 | Task 证据 | 直接观察 |
| --- | --- | --- |
| 初次角色初始化 | [task-06fcf57d](evidence/real01/tasks/task-06fcf57d-fa48-4712-bdc6-da675e3bb1e4.json) | 旧 thread 中返回 Agent 身份和隔离 workspace |
| 忙碌保护输入 | [task-25944d25](evidence/real01/tasks/task-25944d25-536a-4cd5-933d-fec3ae6b97ad.json) | 旧 thread 执行 `sleep 40`，返回 `BUSY_DONE`；同时 handoff CLI 被拒绝 |
| 新会话交接 | [task-229dd0bb](evidence/real01/tasks/task-229dd0bb-a685-4d3d-9235-856536e3494b.json) | 新 thread 确认身份、workspace、交接 nonce |
| 后续普通 query | [task-cb6123d7](evidence/real01/tasks/task-cb6123d7-d58c-413f-89c9-0321fef3abea.json) | 请求只问 nonce 名称，不含值；在新 thread 准确返回交接值 |
| managed 咨询 | [managed-task-3d8e7c62](evidence/real01/tasks/managed-task-3d8e7c62648f9e34f53f9df4345945ca.json) | 新 thread 返回相同 nonce，生成关联 result |
| Worker 重启后 query | [task-f34f8892](evidence/real01/tasks/task-f34f8892-8d42-4f39-8e45-e259dbf4778f.json) | generation 3 的 Worker 仍在新 thread 准确返回 nonce |

旧 thread 为 `01a116d0-a0ae-7760-9449-41cf66043de9`，新 thread 为 `01a116d9-48fc-7ce0-8bf1-015006451a70`。nonce 为 `SESSION-HANDOFF-9172b5b9472a1ac3b0654e43`。六个 Run 的角色文件 SHA 相同：`125b68782c02b25f39e71de1592a9af17030003e96f1ee5ef4cc2af3a6210367`。

[handoff-first-turn-runtime.json](evidence/real01/handoff-first-turn-runtime.json) 与保留的原始 rollout 首 turn 区间复核一致：workspace 正确，实际模型 `gpt-6-astra`、effort `ultra`，首 turn 没有工具调用。正式 Run 的 reasoning metadata 是 `backend_default`，不能改写成冻结 spec 显式 `ultra`；实际首 turn 的 ultra 由 Runtime 记录证明。

## 活动指针、原生 view、协作与重启

- [预览前](evidence/real01/preview-before.json)与[预览后](evidence/real01/preview-after.json)的会话、Task 集合、归一化 Runtime 状态及 workspace 文件哈希一致；[预览历史前](evidence/real01/preview-history-before.json)与[预览历史后](evidence/real01/preview-history-after.json)三类账本行哈希一致。这里的“只读”指预览未引入领域状态变化，不声称停止了后台 heartbeat。
- [busy-reject](evidence/real01/cli/busy-reject-1791384624901752732.json) 与 [stale-cas-reject](evidence/real01/cli/stale-cas-reject-1791384740182651204.json) 有实际非零退出及对应冲突原因；[幂等重放](evidence/real01/cli/handoff-idempotent-replay-1791384740158758138.json)与最终六 Run 计数相互支持未新增初始化执行。
- [旧 view 渲染](evidence/real01/stale-native-rejected.rendered.txt)及 [send-keys](evidence/real01/tmux/0020.json)/[Enter](evidence/real01/tmux/0021.json) 证明 `STALE_VIEW_MUST_NOT_RUN` 键盘提交被 Bridge 拒绝；最终账本没有额外 Task/Run。
- [新 view argv](evidence/real01/new-native-process-proof.json)、[新 view 渲染](evidence/real01/new-native.rendered.txt)、[重启 view argv](evidence/real01/restarted-native-process-proof.json)与[重启 view 渲染](evidence/real01/restarted-native.rendered.txt)都指向新 thread。覆盖正确连接和历史显示，不扩大为新 view 正向键盘提交验收。
- [managed binding 前](evidence/real01/managed-binding-before.json)与[后](evidence/real01/managed-binding-after.json)保留 binding/Agent/principal/organization/peer，generation 1→2，thread/context 同步切到交接 Task。[咨询证据](evidence/real01/managed-consultation.json)的 request、Run、result、reply_to_message_id、nonce 相互一致。peer 没有 Worker，result 的 delivery_state 仍为 pending；只证明结果已持久化且 peer inbox 可读，不证明 peer 自动续办或 ACK。
- [重启前](evidence/real01/restart-before.json)与[后](evidence/real01/restart-after.json)显示 Worker PID/starttime 和 Runtime endpoint 改变，活动会话版本/thread 不变；第六次真实 Run 使用新 Worker instance、generation 3。[旧配置输入](evidence/real01/legacy-config-input.json)仍指向旧 thread，结合第六次 Run 证明此次重启未回流旧配置。
- 只读隔离数据库核实 `agent_sessions` 指向新 thread、版本 1、pending 为空；`agent.session_handoff.completed` 事件关联交接 Task，时间为 `2026-10-07T14:52:18.681879462Z`，与 Task 完成及活动指针更新时间一致。

## 历史、失败与边界

- [切换前已结算历史](evidence/real01/old-history-settled.json)中的两 Task、两 Run、两 SessionBinding，在[重启后历史](evidence/real01/history-after-restart.json)中逐行 SHA-256 不变；旧 binding 仍保留旧 thread。这证明本隔离样本的账本保全，不声称全局历史数据库或整个 rollout 文件字节从未增长。
- 两次 `peer-apply` 失败分别是无交互终端读取 owner password、使用不存在的 `--password-file` 参数；[首次失败](evidence/real01/failure-1791384147659059724.json)、[第二次失败](evidence/real01/failure-1791384442937879198.json)及 CLI stderr 都保留。[续跑记录](evidence/real01/resume-live-initialization-1791384619182468792.json)复用已完成的首个模型 Run，未以重新执行覆盖失败。失败属于验收夹具入口，不计作产品成功。
- 清理证据见 [independent-cleanup.json](evidence/real01/independent-cleanup.json)：记录的隔离 daemon、Worker、backend PID 已停止；证据与隔离数据库保留。本次复核没有再次操作这些进程。
- 本批六次 Run 没有真实制造 failed/uncertain handoff 或全部取消组合；这些失败事务保护依赖本批已有集成测试，不提升为真实 Runtime 全矩阵通过。未覆盖 peer 自动续办、生产安装/服务重启/业务 Agent 切换、任意旧 thread 切回、取消 fallback 后旧 endpoint 重连。

## 下一步

- 主代理将本报告与最终 DELIVERY 的限定结论保持一致，并完成本批源码/证据交付；生产能力须按后续明确部署与运行态证据另行记录。
