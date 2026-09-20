---
doc_type: implementation_task
status: completed
owner: openagentx
updated_at: 2026-09-19
---

# 任务 07：隔离用户闭环与操作文档

## 目标

用临时 HOME/DB/UDS、隔离 Worker 和唯一 `tmux -L` 证明默认路径下从 login、Fleet、Attach、dispatch、
观察、steer/Diagnostic 到最终回复的用户闭环，并把该流程写入安装/启动指南。

## 范围边界

- 不操作真实 `~/.openagentx`、default tmux、user-systemd、installed binary 或生产 DB/socket；
- 不改变 Fleet lifecycle、workspace marker 或 user-systemd 设计；
- 文档主流程使用默认路径，不反复要求 `--db/--socket/--file/--credentials`；
- 覆盖参数只放在高级/排障章节。

## 验收矩阵

| 类别 | 验收内容 |
|---|---|
| 正向 | 空临时 HOME 初始化/login/fleet up；OAX pane0 Attach；dispatch->claim->run->safe output->terminal reply；focused steer；Diagnostic 往返；quit 后 Worker 在线 |
| 状态/CAS/幂等 | 重复 init/up/workspace 不破坏；控制 API 一次；重连 cursor 不重复；第二个 Task 仍可被 resident Worker 领取 |
| 失败 | 无 credential、错误 scope、socket replacement、Worker offline、无增量、terminal 无结果、CAS stale、Diagnostic forbidden 有用户可执行提示 |
| 竞态 | Console 退出/SSH 类似断开时 Worker 继续；任务运行中重连；mode switch 与 output；dead pane 恢复仍走正式 Attach |
| 资源/平台 | 唯一 `tmux -L`、长寿命 sentinel、可靠 cleanup；临时文件 0700/0600；无 secret argv/log；small pane PTY |
| 证据格式 | 每步命令、退出码、Task/Run/cursor/Worker identity、可见 UI 摘要和副作用断言；静态文档检查不冒充 E2E |

## 实施步骤

1. 建立可重复的隔离 control plane/Worker/Runtime fixture，输出可预测但不含秘密的安全过程和最终回复。
2. 通过正式 CLI/API 完成 login、Fleet workspace、Attach、dispatch、focused steer/cancel 和 mode switch。
3. 断言 pane 1+、managed marker、Worker PID/generation、Task/Run/cursor 和终态在 Console 退出前后正确。
4. 更新 README、用户安装/启动指南和 Console help，给出从零到日常使用的连续命令链。
5. 文档明确 `queued`、running、final reply、`uncertain`、Diagnostic 和 `/quit` 的真实含义。

## 验证

```bash
go test ./internal/cli/console ./internal/client/console ./internal/fleet ./internal/cli/fleet ./internal/worker -count=1
go test -race ./internal/cli/console ./internal/client/console ./internal/fleet ./internal/worker -count=1
go test ./internal/cli/console ./internal/fleet -run 'TTY|Tmux|Workspace|Workflow|Task' -count=3
bash -n scripts/*.sh deploy/scripts/*.sh deploy/systemd/*.sh
bash scripts/check-legacy-control-paths.sh --release
go build -o /tmp/openagentx-adr009-task07 ./cmd/openagentx
git diff --check
```

## 退出条件

- 一条默认路径用户流程能从命令运行到可见最终回复；
- Console/tmux 退出不影响 Worker，完成后 Worker 可领取下一 Task；
- 安装指南包含 `console attach`、focused control、Diagnostic 和状态解释；
- 创建一个 Task 07 实现提交后停止等待监督 gate。

## 完成记录

- 实现提交：`a5874d70c1209aed52da9b44f68336f04ea40f2c`。
- 默认 profile 隔离闭环已用临时 HOME/DB/UDS/credential、fake user-systemd、唯一 `tmux -L` 和真实
  daemon/Worker 进程跑通：PTY login、重复 Fleet init/workspace/up、OAX pane 0 Attach、dispatch、领取、
  safe output、focused steer、terminal outcome/reply、Diagnostic/Normal 往返和 `/quit`。
- Console 退出后，同一 Worker PID、instance 和 generation 保持在线并完成第二个 Task；pane 1/2、workspace
  marker、文件权限、systemctl argv 和 secret 边界均有断言。
- Runtime output 通过正式 EventSink、Journal、SSE 与 `Client.Follow` 取证；Task outcome 和 Runtime reply
  分层显示，pending result 不误标终态，窄 pane Timeline 保留有界安全多行结果。
- README、用户安装指南和 Console `/help` 已串联默认路径、`console attach`、focused control、状态解释、
  Timeline 滚动、Diagnostic/Normal 与 `/quit`。
- 五包普通测试、四包 race、三轮 TTY/tmux/Workspace/Workflow/Task 稳定性、全仓 Go、vet、Shell、release
  scanner、独立 build、CLI help、文档和 secret 检查均通过；未修改 Web 源码，未操作真实运行状态。
