# oaxops：宿主运维 Agent

`oaxops` 是运行在 `/home/sky/docs` 的独立原生 Codex 会话入口。它按授权事件执行组织交接、宿主维护和跨 OpenAgentX 重启的核验；不是 OAX Worker，也不自动接收 OAX Mailbox。无事件时不启动模型。当前会话 thread 由私有 `session.json` 保存，迁移时须保持原值。

安装仓库中的 `scripts/operations/oaxops` 至 `~/.local/bin/oaxops` 并赋予执行权限。状态目录遵循 `XDG_STATE_HOME/oaxops`，默认 `~/.local/state/oaxops`；其中 `session.json` 保存活动 thread，`events/<key>/` 保存唯一事件及回执，`writer.lock` 用于排除并行写入。工作目录固定为 `/home/sky/docs`。

```sh
oaxops status
oaxops open
oaxops send --key <稳定事件ID> --prompt <绝对路径文件>
oaxops send --maintenance --key <稳定事件ID> --prompt <已授权维护交接文件>
```

`status` 只读显示活动 thread、最近结果和 writer 是否忙；`open` 在当前终端恢复原 thread。`send` 启动 `oaxops-event-<key>` 用户级临时 systemd 单元，返回只说明单元已提交；最终结果须查 `status` 和 `events/<key>/result.json`。普通事件默认附加只读约束。`--maintenance` 只承接正文明确授权的具体维护，不授予长期维护权限。宿主当前采用 `danger-full-access`，提示词中的只读约束不是操作系统隔离。

同一 key 与相同正文只返回原结果，不重复调用模型；同一 key 改变正文会拒绝。事件已开始但没有结果回执时返回非零，须人工核实，不自动重跑。失败或不确定回执的重复查询也返回非零。前台终端和后台事件共用一把锁；当前本机 Codex 入口直接指向可执行文件，执行进程继承锁，避免 Python 入口意外退出后立即放行第二个 writer。

首次创建独立 thread 仅在明确授权且没有活动 `session.json` 时使用 `oaxops init --key <事件ID> --prompt <文件>`；已有会话应使用 `event` 或 `send`。原有会话及事件回执在安装前迁移至新的状态目录，原 thread 必须保持不变。`ROLE.md` 和 `organization-handoff.md` 随私有状态一起保存；安装和迁移细节见当批交付记录。
