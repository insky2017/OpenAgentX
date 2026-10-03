# 原生终端与托管协作交付记录

2026-10-03。源码 `e22e4a8` 已安装，schema v5。本轮真实双 Agent 只读闭环、Fleet 原生输入、前台断开后继续、忙时排队、离线恢复与 30 分钟空闲观察已完成。**原 Rhythm / Pay 保持 external，尚未切换为自动协作。**

- Fleet 新建 Codex pane 0 默认原生终端；活 pane 保留，AGY 保留能力明确的 Console。
- managed 只读咨询原子入持久 Task/Mailbox，固定 backend/thread；结果结算后自动安排请求方只读续办，消费结果不再回信。
- 职责、绑定、backend变化隔离为 needs_review；普通任务可继续。未恢复 LLM heartbeat。
- `go test ./...`、`go vet ./...` 和核心五包 race 通过；真实 Codex 发问、读文件答复、自动消费结果、同 thread 续办已有 Task/Run/Journal 与独立事实证据。完整状态见[覆盖矩阵](COVERAGE.md)，失败及修正见[执行记录](EXECUTION-LOG.md)。

源码规则与用例见[实施计划](../../../plans/2026-10-03-managed-collaboration.md)，用法见[托管协作指南](../../../operations/managed-collaboration.md)。本机证据根目录：`/home/sky/.local/state/openagentx/validation/2026-10-03-managed-collaboration/`。

关键真实结果：

| 用户流程 | 结果与证据 |
|---|---|
| A 发问、B 答复、A 自动续办 | A 实际调用 CLI 发问，B 用工具读取自己 workspace 的随机事实文件，A 自动消费；三个 query Task 均成功且 thread 保持。见[闭环记录](evidence/live-e22e4a8-01/evidence-chain-20261003T180128-1865/collaboration-result.json) |
| 固定 pane 0 输入与结果可见 | 正式 `fleet workspace --respawn-dead` 在独立 tmux server 中打开原生终端；唯一输入创建 1 Task / 1 Run，独立文件与终端结果一致。见[Fleet 记录](evidence/live-e22e4a8-01/evidence-fleet-observe-20261003T184336-73b0/fleet-native-result.json)；不扩大为 `fleet up` / systemd 首启全链已验 |
| 关闭前台、后台继续、重开历史 | 真正挂断前台 PTY 后 query 成功，重开显示结果且 thread 不变。见[前台记录](evidence/live-e22e4a8-01/evidence-native-continuation-20261003T180548-58be/native-result.json) |
| 无工作时不调用模型 | 北京时间 18:06:43–18:36:43，1800.107508 秒内 Task 清单不变，两个测试 thread 新增模型轮次、工具调用均为 0，后台进程及 Worker 在线。见[空闲记录](evidence/live-e22e4a8-01/evidence-native-continuation-20261003T180548-58be/idle-result.json)；不推算全机或账户费用 |
| 忙时排队与离线待办 | 真实 sleep 75 秒期间 37 次采样均 queued / 0 Run，当前任务结束后再执行咨询；离线同 key 两次仍一条消息、一项 Task，重启 Worker 并正式 `agent resume` 使 Runtime 就绪后，原待办答复与消费均成功。见[忙时证据](evidence/live-e22e4a8-01/evidence-recovery-20261003T192604-3f72/M04-result.json)、[离线恢复](evidence/live-e22e4a8-01/evidence-recovery-20261003T192604-3f72/M05-result.json) |
| 找对领域与入口 | 真实 API 拒绝未知 scope、错误 owner 和不支持的行动 request，均无新 Task。见[拒绝回执](evidence/live-e22e4a8-01/evidence-recovery-20261003T192604-3f72/M06-rejections.json) |

各过程的正式 API、Task/Run/Journal、PTY、随机文件与 SHA 清单一并保留，失败记录不被复验覆盖。用户补充的真实行为优先、按需选择测试原则已提交到两处 AGENTS.md；本轮没有为凑覆盖率再扩测试矩阵。

离线恢复的前提是 Runtime 就绪，只有 Worker online 不足以接单。本次夹具漏了正式 `resume`，期间原 Task 保持 queued / 0 Run；补步骤后沿用该任务，没有重发。结尾采集仍引用已退出 Worker 的 PID 而失败，随后只读补采已有结果，新增 Task 为 0。见[补采结论](evidence/live-e22e4a8-01/evidence-recovery-20261003T193650-574a/verdict.json)与[首次失败及修正](EXECUTION-LOG.md)。

安装工件 SHA-256：`14f4221694eedce95a7ce81eefae52b14f58393d8d7368b3057407d10437f2d9`。从独立干净 checkout 构建，内嵌 revision=`e22e4a8`、modified=false；三服务实际工件一致，旧消息/绑定/职责目录保持。见[工件来源](evidence/release01/manifest.json)、[安装](evidence/installation01/result.json)、[正式消息协议复验](evidence/installed-protocol01/result.json)。

原生输入沿用 mutation 结算，Run 成功及独立文件匹配时 Task 仍可保守标为 `uncertain/business_effect_unverified`；本轮未把它伪改成业务成功。自动 consultation 则必须达到 `succeeded/query_result_delivered`。AGY 原生前台、未知结果一键恢复与精确仅目标工具取消仍待补。

隔离验收进程及专属子进程已回收，原始证据和 profile 保留，正式三服务的 PID/starttime/SHA 不变，见[清理证据](evidence/live-e22e4a8-01/evidence-recovery-cleanup-20261003T193815-8229/)。阅读 HTML 已同步当前状态，静态链接和结构校验通过；本轮本地 file 预览受浏览器策略限制，未重新做视觉验收，见[静态核验](evidence/html-status02/verification.json)。

下一步业务切换已有[Rhythm / Pay 具体交接方案](../../../operations/rhythm-pay-managed-handoff.md)，待用户确认后执行。本轮只使用独立测试身份，不将其证据报告为两原 Desktop 自动协作成功。
