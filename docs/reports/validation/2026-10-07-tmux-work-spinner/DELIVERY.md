# 受管 tmux 窗口工作提示

本批实现已受管 Agent 窗口的两态工作提示：running 时在原窗口文字后显示十帧 spinner，idle 不添加字符。保持窗口真实名称、Task/Run 状态、调度、取消、时限和 session 生命周期。源码分支 `codex/tmux-work-spinner` 从 `main f3f1818` 建立，未安装、未重启正式服务；由主代理统一整合和部署。

## 状态来源与接入点

| 权威事件 | 显示状态 | 位置 |
| --- | --- | --- |
| Worker 初始等待、无任务 | idle | `ActiveRunManager.Run` |
| 已通过 BeginAttempt、网络准备及能力检查，调用 Runtime StartTurn | running | `startWorkActivity`，紧邻 `adapter.StartTurn` 前，覆盖同步 Runtime 事件 |
| Runtime `approval.requested` 已成功持久化 | idle | RunManager EventSink → `workActivity.event` |
| 正式控制通道 `DecideApproval` 成功，且本 turn 无其他待审批 | running | RunManager `applyControl` → `workActivity.decided` |
| Runtime `Wait` 返回（成功、失败、取消、未知），或 StartTurn 失败 | idle | `workActivity.finish`，在 FinishRun 网络调用前清除 |
| Worker 正常退出 | idle | RunManager defer 与 WorkStatus.Close |

显示回调只向一个容量为 1 的本地通知通道发信并更新原子 bool，不等 tmux；tmux 工作集中在 `internal/fleet/work_status.go` 的单 goroutine 中。一次发布的全部 tmux 操作共用 1 秒超时，错误不返回业务执行路径。状态变化立即唤醒发布器；10 秒本地对账只用于补发现新窗口和恢复失效显示，不唤醒模型。

当前“等用户”覆盖最终答复后等待下一输入，以及 Codex 支持的 commandExecution/fileChange 审批。现有 Codex Adapter 不支持 `tool/requestUserInput`，会按原契约拒绝并结束 turn，本批未新增该协议。不能据此声称所有外部 Runtime 的等待协议都已验证。

## 窗口和显示配置

Worker 是 systemd 后台服务，没有自身 tmux pane。它只读发现 `@openagentx_managed=1` 且 `@openagentx_agent_id` 等于自身 Agent ID 的窗口，再以精确 `@window-id` 写窗口选项。原生接入沿用既有 `TMUX_PANE` 精确绑定逻辑；不使用 active window、CPU、PID 或 pane 命令推断工作状态。

两个 window-local 选项均为“原格式 + 以下后缀”，原格式通过 `show-options -w -A -v` 读取，包含继承值：

```text
#{?#{==:#{@oax_state},running},#('/absolute/path/openagentx' tmux-spinner),}
```

`@oax_state` 仅 `running` / `idle`；helper `openagentx tmux-spinner` 只按 `time.Now().Unix() % 10` 输出一个前置空格与 `⠋ ⠙ ⠹ ⠸ ⠼ ⠴ ⠦ ⠧ ⠇ ⠏` 中的一帧。没有 tmux 配置中的帧运算、模型调用或额外常驻服务。格式后缀标记用于防止重复追加并替换迁移后的 helper 路径；用户后来修改的窗口格式作为新底稿保留。session-local `status-interval=1`，未写全局 tmux 配置。

不修改普通窗口的格式和名称；同 session 普通窗口的刷新间隔也随 session 变为 1 秒，但显示内容保持。新发现窗口最多等待 10 秒；已有窗口状态变化立即发布。

## 验收与证据

- 真实隔离 tmux + Worker 集成：idle → running → 等审批 idle → 决定后 running → 完成 idle；关闭 tmux 后仍正常完成失败终态，tmux 不在 PATH 时下一任务仍完成。
- 真实隔离 tmux：两个 Agent 分别 running/idle，互不污染；显式与继承格式、current/non-current 格式、窗口名称、用户后改格式及普通窗口保持；重复发布不叠加 spinner。
- 真实 Codex Worker + 正式 API + PTY + 独立文件效果通过：[结果](evidence/live-final/spinner-result.json)、[逐秒采样](evidence/live-final/spinner-samples.json)、[真实渲染状态行](evidence/live-final/spinner-rendered-status.json)、[PTY 原始记录](evidence/live-final/attached-client.pty.log)、[Task/Run/Journal](evidence/live-final/spinner-task.json)。发出任务后 1.047 秒观察到 running，全部十帧出现，完成后 idle；current/non-current 都有运行中观察，名称与普通窗口格式保持。真实模型只完成一个 mutation Run，Run succeeded；Task 仍为 uncertain/business_effect_unverified，验收独立读取文件证明本次测试效果，不改账本。
- 相关回归：`umask 022; go test -count=1 ./internal/worker ./internal/fleet ./internal/cli/worker ./cmd/openagentx`；关键 Worker/tmux 集成 `-race` 通过，见[回归输出](evidence/go-tests-final.log)与[集成输出](evidence/spinner-integration-final.log)。helper 连续十秒逐帧输出另见[实际命令采样](evidence/helper-frames.json)。

首次错误完整保留：默认 umask 077 导致既有 fleet 测试预期 0644 实际为 0600，022 下通过；第二候选真实 Task 按现有 mutation 契约为 `uncertain/business_effect_unverified` 而 Run 成功且 proof 已生成，脚本错误断言 Task 必须 succeeded；同时真实界面发现缺 `show-options -A` 导致继承格式被当为空，修复并新增继承格式的真实 tmux 边界检查，再以最终工件重验。最终真实流程尾部读取 status-interval 的测试命令误用了 `=OAX` 目标，已改为 `OAX`；未重放模型，直接重新核对已经保留的真实采样、PTY、Task/Run、独立 proof 与窗口选项通过，见[首次日志](evidence/spinner-e2e-final.log)、[复核脚本](evidence/verify_existing.py)和[复核输出](evidence/spinner-reverify.log)。不改写首次记录为成功。

测试适配器只用于可重复控制审批和 tmux 故障；不冒充真实模型证据。独立验证由主代理统一安排，本分支不自称独立复审通过。未验证 Worker 被 SIGKILL 后的陈旧显示回收；正常结束/退出会清除，崩溃遗留状态需后续 Worker 启动重新发布。窗口格式设置是当时继承格式的本地副本，后续全局格式变化不会自动穿透本地覆盖；显式窗口格式新编辑会保留。

原始资料保留于 `/home/sky/.local/state/openagentx/validation/2026-10-07-tmux-work-spinner/`，脱敏副本及 SHA-256 [manifest](evidence/manifest.json)入库；[源码/工件绑定](evidence/source-artifact.json)固定全部修改产品源码的逐文件哈希及候选二进制 SHA-256。三个自有隔离 fixture 已按其保存的 PID/starttime 与原生命周期关闭，证据保留；正式六域与默认 tmux 服务器未操作。
