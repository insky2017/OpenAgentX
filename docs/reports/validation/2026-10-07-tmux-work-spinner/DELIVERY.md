# 受管 tmux 窗口工作提示

本批实现已受管 Agent 窗口的两态工作提示：running 时在原窗口文字后显示十帧 spinner，idle 不添加字符。保持窗口真实名称、Task/Run 状态、调度、取消、时限和 session 生命周期。源码分支 `codex/tmux-work-spinner` 从 `main f3f1818` 建立，以 `dbdd4b3` 整合，现已随 `c35d702` 联合工件安装并完成正式重启；[统一交付](../2026-10-07-resume-timeout/DELIVERY.md)记录实际 SHA 和部署结果。

## 修改文件

- `internal/worker/work_activity.go`：把已有执行和审批生命周期投影为 bool，不增加持久任务状态。
- `internal/worker/run_manager.go`、`runner.go`、`config.go`：连接生命周期回调及正常退出清理。
- `internal/cli/worker/command.go`：创建显示发布器，将回调注入 Worker。
- `internal/fleet/work_status.go`：受管窗口定位、两态选项、显示格式、十帧 helper。
- `cmd/openagentx/main.go`：`tmux-spinner` 小命令入口。
- `internal/worker/work_activity_integration_test.go`、`internal/fleet/work_status_integration_test.go`、`scripts/validation/tmux_work_spinner_e2e.py`：必要边界与真实验收；本报告及 `evidence/` 保存证据。

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

## 正式安装与最终显示

上文“正式环境未操作”是独立功能验收阶段的边界。统一部署已于 2026-10-07 11:25 UTC 完成。[独立正式核验](../2026-10-07-resume-timeout/evidence/deployment01/independent-verification.json)在11:29 UTC观察到 OpenAgentX 为 running、其余五域 idle；六域原窗口名称及定位保持，current/non-current 格式均有后缀，OAX session 的刷新间隔为1秒。17个普通窗口当时没有 spinner 装饰；正式部署未采普通窗前快照，历史不变的证据来自独立隔离 tmux 验收，不能混称正式前后对比。

实际六域窗口的两个格式均为：

```tmux
# 每个已核对身份的受管窗口使用这两个值；不是全局覆盖普通窗口。
window-status-format "#I:#W#{?window_flags,#{window_flags}, }#{?#{==:#{@oax_state},running},#('/home/sky/.local/bin/openagentx' tmux-spinner),}"
window-status-current-format "#I:#W#{?window_flags,#{window_flags}, }#{?#{==:#{@oax_state},running},#('/home/sky/.local/bin/openagentx' tmux-spinner),}"
# 仅 OAX 所在 session：status-interval 1
```

helper 实现如下，由 CLI 直接 `fmt.Print(fleet.SpinnerFrame(time.Now()))`：

```go
func SpinnerFrame(now time.Time) string {
    frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
    return " " + frames[now.Unix()%int64(len(frames))]
}
```

两个 Agent 的 running/idle 相互独立，动画帧使用共同系统时钟，同秒通常相同；不额外建立每个 Agent 的动画状态。采用 `@oax_state` 作为显示投影，而工作状态直接取自既有 RunManager/Runtime 回调，避免建立第二套调度或状态账本。

## 手动检查

1. 打开已有 OAX session，空闲领域名后没有 spinner。在一个领域终端提交明确获准的短任务，执行过程中观察每秒转动，切换到其他窗口后原窗口仍显示。
2. 在第二个领域提交另一条独立获准任务；观察两个窗口同时显示，其中一个完成后仅其 spinner 消失。普通窗口内容与名称不变。
3. 等任务答复或现有命令/文件审批等待时，确认 spinner 消失。不要为了显示测试重放旧业务指令。
4. 只读查看准确窗口映射及状态：

```sh
tmux list-windows -t OAX -F '#{window_id} #{window_name} #{@openagentx_agent_id} #{@oax_state}'
tmux show-options -w -A -v -t @8 window-status-format
tmux show-options -w -A -v -t @8 window-status-current-format
tmux show-options -v -t OAX status-interval
openagentx tmux-spinner
```

`@8` 是本批 OpenAgentX 原窗口，其他机器先按第一条输出定位。tmux 不存在、窗口关闭、执行失败等故障只在隔离测试环境验证；已有真实 tmux/Worker 集成及回归证据覆盖，不破坏正式终端做测试。
