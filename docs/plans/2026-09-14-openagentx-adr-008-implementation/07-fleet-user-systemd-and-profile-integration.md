---
doc_type: implementation_task
status: completed
owner: openagentx
updated_at: 2026-09-14
---

# 任务 07：Fleet、user-systemd 与默认 profile 集成

## 目标

将默认路径、CLI Token、`OAX` workspace 和 user-systemd Worker 宿主组合为幂等、非破坏的日常 Fleet 工作流，并消除配置校验与实际启动路径不一致。

## 依赖

- 任务 06 已通过；Console 新入口完整可用；
- 任务 01 中受保护的 user-systemd 前置修改已独立提交，当前任务以该基线为准而非覆盖它。

## 实施步骤

1. `fleet init/up/down/status/workspace` 使用任务 02 resolver；无显式参数时分别使用 canonical manifest、database、socket 和 worker config 目录。
2. `fleet init` 只处理显式用户选择/声明的 Agent。若 manifest 不存在，交互终端可用经认证控制面列表初始化；非交互环境要求明确输入并 fail closed。不得扫描 `agents/*`、样例或测试目录。
3. manifest 与每个 `workers/<agent-id>.yaml` 原子写入、幂等且非破坏；已有不同内容必须显示 diff/冲突并要求显式决定，禁止静默覆盖。
4. user-level systemd 是默认 Worker 宿主；生成/调用的 unit、Environment/参数和 `worker_config` 必须与 Fleet 预检的 canonical 文件完全一致。所有不一致 fail closed。
5. 默认使用 `systemctl --user` 管理 Worker；不得因 SSH/tmux/window/Console 退出终止 Worker。Linger 只检查/提示，不未经许可变更系统状态。
6. workspace reconcile 创建或补齐 `OAX` 的 overview/Agent window pane `0`，保留 pane `1+`、unmanaged/orphaned window 和未知进程；不得 kill 或注入活动 pane。
7. pane `0` 启动新 Console 入口，只依赖本地 credential store；manifest、unit、process argv、tmux command 和日志均不得含用户名密码或 token。
8. `fleet down` 保持 ADR-005 持久化 graceful drain：默认无限等待当前 RunAttempt 自然结束；force-stop 继续是独立危险路径，不因 TUI 集成改变。
9. 将安装指南、README、systemd unit 示例和 CLI help 同步为 `OAX`、默认路径、user service、login/logout 和新 Attach 语义；不得改写 ADR-006/007。

## 必测场景

- 空 home 首次 init、重复 init、部分文件已存在、内容冲突；
- 控制面 Agent selector 与非交互显式清单；
- unit 实际 `ExecStart` config 等于 Fleet 验证文件；
- Console/tmux 退出后 fake/isolated Worker 仍运行；
- graceful down 有 active run 时等待且不 force；
- workspace 多 pane/unmanaged/orphaned 非破坏；
- argv/unit/manifest/log 不含密码/token。

## 验证

```bash
go test ./internal/cli/fleet ./internal/fleet ./internal/cli/console ./internal/worker
go test -race ./internal/cli/fleet ./internal/fleet ./internal/worker
bash -n scripts/*.sh deploy/scripts/*.sh
bash scripts/check-legacy-control-paths.sh --release
go build -o /tmp/openagentx-adr008-task07 ./cmd/openagentx
git diff --check
```

systemd 行为优先用 fake runner 和临时 user manager 环境；未获批准不得 restart 真实 `openagentx.service` 或 Worker unit。

## 退出条件

- Fleet 默认路径和实际 Worker 配置完全一致；
- 默认 user-systemd、显式 Agent 清单和 graceful drain 有测试；
- workspace reconcile 在多 pane 和冲突现场非破坏；
- 文档与最终 CLI 一致且不含凭据；
- 阶段提交和 execution log 完成后暂停。

## 完成记录

- 主实现：`40fe06e813dcce5c327cf159c66fff630d3bc236`；host consistency hardening：
  `f5c0d0d549931e4104bc7248c5e5714305a242d9`；preflight ordering：
  `e13db7b7e505cb77c1359c776b9c3c473322062f`。
- Fleet 已统一 local profile 默认路径和 installation-bound CLI Token，不读取或传递 Owner 密码；
  user-systemd 使用 canonical `%h/.local/bin/openagentx`、Worker config 和 EnvironmentFile，所有
  `systemctl` 调用使用 `--user`，daemon 与 resident Worker 不使用 stop 级联依赖。
- Worker config/manifest 使用同一捕获内容严格校验、`O_NOFOLLOW`/权限/owner 检查和非覆盖原子安装；
  `init/workspace/up` 在任何 config、manifest、tmux 或 systemd mutation 前验证 canonical binary。
- graceful down 只持久化 stop intent 并等待 busy RunAttempt 自然完成；force-stop 保持独立双确认。
  compatible managed dead pane 0 可显式安全恢复，live pane、pane 1+、unmanaged/orphaned 现场不受影响。
- Ubuntu 24.04/tmux 3.4 的 Go/race、隔离 tmux、systemd static/analyze、Web、release/build 检查通过；
  监督端 Termux 的完整 Fleet/CLI/Console/Worker 最终通过，其中 `internal/fleet` 约 104 秒。
- 未开始 Task 08、发布、安装或真实 service/DB/socket/default tmux 操作。
