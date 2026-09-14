---
doc_type: implementation_task
status: completed
owner: openagentx
updated_at: 2026-09-14
---

# 任务 06：Console 主菜单、Agent selector 与全屏 TUI

## 目标

用真正全屏、事件驱动的 TUI 替换行式 REPL 和连续 JSON 输出，提供稳定输入、可滚动 Timeline、状态栏、overlay、主菜单和 Agent selector。

## 依赖

- 任务 05 已通过；path、cursor/reducer、CLI Token 和 workspace 绑定服务均稳定。

## 实施步骤

1. 使用任务 01 固定的 TUI framework 构建纯 model/update/view 核心；网络、timer、terminal 和 tmux I/O 通过 command/message 注入，便于无真实终端的确定性测试。
2. `openagentx console` 无子命令时进入主菜单，显示认证状态并包含：
   - Attach；
   - Diagnostic Attach；
   - Login / Replace Login；
   - Logout；
   - `Foreground Takeover（规划中，暂不可用）`；
   - Exit。
3. Agent 解析顺序必须固定：显式 `--agent` -> 当前受管 window marker -> 经 CLI Token 认证的控制面 Agent 列表。列表显示必要安全状态并支持搜索/键盘选择；非交互环境无法唯一决定时 fail closed。
4. 选择 Agent 后调用任务 05 的验证/绑定服务，再 Attach；不得在 selector 内自行调用 tmux 命令形成第二套逻辑。
5. Attach 视图至少包含固定输入区、滚动 Timeline、状态栏、连接/模式/Agent/Worker generation 摘要，以及 `/status` overlay。resize、窄终端、空数据和长行必须稳定。
6. Event 更新不得重置 cursor、抢焦点或冲刷用户正在编辑的输入；heartbeat burst 只刷新状态，不生成逐条 Timeline。
7. 输入命令通过正式 client API 执行 `steer`、`cancel`、`approval`、`dispatch`；显示 pending/succeeded/failed 的结构化结果，断线时禁用写操作，不本地排队假装成功。
8. Normal 与 Diagnostic 共用 TUI 和 reducer；Diagnostic 只显示授权后的脱敏诊断字段，并清晰标注模式。
9. `/status` 显示认证用户/到期、socket identity、Agent、WorkerInstance/generation、backend health、active run、cursor、reconnect 状态；不得显示 token、password、Secret 或隐藏推理。
10. 本任务原子删除 `console attach --once`、独立 `console status` 和旧 `bufio.Scanner` REPL 路径；帮助和错误给出新入口，不保留隐式 fallback。
11. 退出 TUI 只 detach Console；不得 stop/drain Worker。EOF、SIGINT、terminal restore 和 panic/error 路径都必须恢复 terminal 状态。

## 必测场景

- 主菜单登录/替换登录/logout/禁用 takeover；
- 三种 Agent 选择来源和非交互 fail-closed；
- 输入编辑期间事件到达，文本与 cursor 不变；
- timeline scroll、resize、overlay 打开/关闭、断线重连；
- heartbeat 合并、旧代际丢弃、safe-output 渲染；
- 每个控制命令只调用正式 client 一次，失败不显示成功；
- 退出后 Worker/daemon 仍运行；terminal cleanup 被调用。

## 验证

```bash
go test ./internal/cli/console ./internal/client/console ./internal/api/console
go test -race ./internal/cli/console ./internal/client/console
go build -o /tmp/openagentx-adr008-task06 ./cmd/openagentx
/tmp/openagentx-adr008-task06 console --help
bash scripts/check-legacy-control-paths.sh --release
git diff --check
```

另外在临时 home、fake client 和隔离 tmux server 中完成可重复 TUI smoke；不得连接真实 socket。

## 退出条件

- 无行式 REPL/连续 JSON/fallback；
- 主菜单、selector、Attach TUI 和 `/status` 均可测试；
- 编辑、事件、重连和 terminal restore 通过；
- `--once` 与独立 status 已删除且帮助同步；
- 阶段提交和 execution log 完成后暂停。

## 完成记录

- 主实现：`4b5d2c0675a9b00f6d48e52395710b2639b8acac`；按冻结版本引入 Bubble Tea、
  Bubbles 和 Lip Gloss，交付真正 alt-screen 全屏 TUI、认证状态主菜单、masked Login/Replace
  Login、Logout、禁用的 Foreground Takeover、Normal/Diagnostic Attach、`/status`/`/help`
  overlay、固定输入区、bounded Timeline 和 terminal cleanup。
- Agent selector 完成显式 `--agent`、当前 compatible managed window、经认证控制面分页列表三种
  来源，并只通过 Task 05 binding service 执行确认重绑。列表安全投影、超过 100 条分页、冲突提示、
  非 TTY fail-closed 和 pane `1+` 保留均有测试；tmux 仍不承担业务身份或控制。
- 状态流 hardening：`5aa6c973abd864a3c7e80b41f4bdc422600d002c`；Follow snapshot/event
  增加 reducer Apply ack，只有成功应用才推进 cursor。普通重连、retention re-Attach、旧代际 fencing、
  timeline scroll、overlay 输入、断线禁写和 session absolute expiry 均由 typed Msg/Cmd 驱动。
- Normal SSE 强制移除 Diagnostic，Diagnostic 要求 owner 与 `console.diagnostic`；长连接在 CLI/Web
  session 到期时关闭。正式 dispatch/steer/cancel/approve/reject 只调用 authenticated API 一次，
  Timeline 仅显示 ID、version、status、decision、sequence 等结构化安全 outcome。
- Terminal compatibility：`c42414b7a21b98bd35ab0de3949778d591705709`；smoke 使用锁定的
  `ansi.Strip` 处理 Termux 的 `ESC ( B` 等真实终端序列。Observe SSE 缺少 mode 时向后兼容为
  normal 并继续脱敏，显式空值、重复或未知 mode 仍 fail closed。
- Task 06 定向与 race、全仓 Go 测试、受影响 vet、Go build、Web observation/PWA/build、release
  scanner、CLI grammar、隔离 PTY/UDS/`tmux -L` smoke 和 diff check 均通过；独立 Termux 真实
  PTY/tmux smoke 最终通过，监督者结论为 Task 06 `GO`。
- Task 07 未开始；`T04-01`、`T05-01` 继续归属 Task 07/pending。未 push、安装、重启或操作真实
  service、DB/socket/default tmux/installed binary/父仓。
