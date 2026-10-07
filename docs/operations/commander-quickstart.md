# 独立组织指挥者

指挥者的工作目录是 `/home/sky/docs`，使用新建的原生 Codex 会话 `01a115e8-e528-75f3-9e30-612d617cfb84`，模型 `gpt-6-astra / high`。它独立于 OAX daemon 和六域 Worker，因此能在 OAX 重启期间继续执行授权的维护。

## 在哪里看

```sh
oax-commander status
oax-commander open
```

`status` 查看最近事件和 writer 状态；`open` 在当前终端进入同一指挥者会话。后台事件运行在 `systemd --user`，不会自动创建 OAX tmux 窗口；因此只看现有 OAX 窗口列表，看不到指挥者。事件完成后没有常驻模型进程。

## 按事件工作

```sh
oax-commander send --key <稳定事件ID> --prompt <文件绝对路径>
```

终端和事件共用单 writer 锁；前台终端打开期间，后台事件会拒绝并行写入。退出前台后才可再提交事件。重复 key 不重新执行，已开始但未知终态不自动重试；空闲没有模型轮询。

普通事件默认按只读任务约束执行；本机宿主采用无沙箱模式，该约束不是 OS 强隔离。需要维护时，按具体用户授权交接唯一执行权、固定工件和证据路径，再使用 `send --maintenance`。本次维护已结束，不构成以后任意重启授权。

## 当前结果与边界

2026-10-07 已实际完成 `resume-timeout-rollout-20261007`：独立指挥者执行唯一部署链，跨 OAX 重启继续核验，最终 completed、exit0。六域保持原 thread/window/pane，新配置无30分钟总截止。

角色和组织交接的本机副本位于 `/home/sky/docs/.oax-commander/ROLE.md`、`organization-handoff.md`；会话与事件原始记录也保存在该私有目录。仓库正式文档为 `docs/operations/2026-10-07-commander-handoff.md`，本指南阅读副本为 `/home/sky/docs/COMMANDER.md`。

目前这是独立组织指挥者入口；尚未注册为新的 OAX Worker、尚未自动消费 OAX Mailbox，也没有固定的可见 tmux 窗口。不要把历史 `orchestrator` 注册身份当作此会话。
