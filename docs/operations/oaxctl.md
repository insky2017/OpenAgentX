# oaxctl 远程控制客户端

`oaxctl` 从手机 Termux 或其他远程终端通过 OpenAgentX Web HTTPS 接口查看 Agent、查询任务、派发任务及追加普通消息。源码为仓库中的 [`clients/oaxctl`](../../clients/oaxctl)，只依赖 Python 标准库。宿主机维护入口是 `oaxops`；本客户端不管理宿主机服务和原生终端。

安装单个可执行文件：

```sh
install -Dm755 clients/oaxctl "$HOME/.local/bin/oaxctl"
```

在 `${XDG_CONFIG_HOME:-$HOME/.config}/oaxctl/config.json` 写入非秘密配置：

```json
{"url": "https://agentx.oneaxe.cn", "username": "owner"}
```

密码在 `oaxctl login` 的终端提示中输入，不要写入配置文件或命令参数。非交互环境可通过受控环境变量 `OAXCTL_PASSWORD` 传入；`OAXCTL_URL`、`OAXCTL_USER` 可覆盖配置。自定义边缘 User-Agent 使用 `OAXCTL_UA`。会话和 Cookie 存于 `${XDG_STATE_HOME:-$HOME/.local/state}/oaxctl/`，目录必须为 0700，文件必须为 0600。

`agents` 展示服务端总览的就绪原因，以及当前能否立即开始；`agents --json` 返回同一就绪投影。`get /api/auth/v1/session` 只展示主体和到期时间，同时更新本地 CSRF，不显示令牌。客户端拒绝 HTTP 重定向。

```sh
oaxctl login
oaxctl whoami
oaxctl agents
oaxctl tasks --agent pay-service
oaxctl show task-...
oaxctl dispatch pay-service '请只读核对当前状态' --intent query --wait
oaxctl say task-... '补充只读范围' --wait
oaxctl wait task-... --timeout 900 --interval 30
```

任务内容可用 `-` 从 stdin 读取，或用 `@/path/to/prompt.txt` 从文件读取。`dispatch` 与 `say` 的写入响应会打印幂等键；网络结果不明时先核对任务列表，再用原键与完全相同内容重试。`say` 手动重试还需指定首次使用的 `--expected-version`。`--wait` 超时仅停止本地等待，退出码 3；服务端任务继续运行。任务以失败、取消或不确定终态结束时退出码 4；HTTP/配置/传输错误退出码 2。

HTTP 字段与安全边界见 [Web HTTP 客户端契约](../api/http-client.md)。
