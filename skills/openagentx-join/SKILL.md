---
name: openagentx-join
description: 将用户授权的当前 CLI 职责、工作目录与交接内容离线登记为 OpenAgentX Agent，供当前轮结束后接管。用于用户要求现有 Agent 加入 OAX；不用于让模型长期轮询总线。
---

先运行 `openagentx agent join --help`，以当前已安装命令为准。保留用户确定的职责和目录，不擅自扩大权限或安装全局配置。

确认稳定 Agent ID、名字、工作目录和角色文件；必要时将当前已完成、剩余工作和未确定副作用写入用户授权目录中的简短交接文件。使用明确参数登记：

```sh
openagentx agent join --id research --name Research --workspace "$PWD" --role ./ROLE.md --handoff-file ./HANDOFF.md --prepare
```

仅在当前环境有明确来源时传 `--thread-id` 和受管 `--endpoint`，不要从标题猜测。`--prepare` 只保存本地配置和 receipt；原 CLI 仍在当前轮运行，命令不会启动服务、派单或证明 live 绑定。

报告登记结果，给用户当前轮结束后运行的 `openagentx agent resume research`。不要从正在交接的 turn 调用 resume 来抢占自己；不要靠重复工具 poll 保持工作。跨 turn 监听由宿主执行，角色实际注入由 Runtime adapter 负责。

查看 `openagentx agent status research`；进入受管原生终端使用 `openagentx agent open research --native`。若受管桥接未就绪，保留错误，不绕过仲裁直接运行 `codex resume`、向 tmux/PTY 注入任务或另启同 thread 写者。相同登记可重放；冲突先核对既有配置，不覆盖。
