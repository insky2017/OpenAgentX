# oaxctl 远程控制客户端

`oaxctl` 通过 HTTPS 查询、派发任务及追加消息，只依赖 Python 3 标准库。宿主维护使用 [oaxops](oaxops.md)。

## 首次使用

从 [仓库](https://github.com/insky2017/OpenAgentX) 获取 `clients/oaxctl`，以下安装命令在仓库根执行。原生 Termux 先安装 `python`、`coreutils`（`pkg install python coreutils`）。

```sh
install -Dm755 clients/oaxctl "$HOME/.local/bin/oaxctl"
export PATH="$HOME/.local/bin:$PATH"
if [ -n "${TERMUX_VERSION:-}" ]; then
  termux-fix-shebang "$HOME/.local/bin/oaxctl"
fi

oaxctl_config_dir="${XDG_CONFIG_HOME:-$HOME/.config}/oaxctl"
mkdir -p "$oaxctl_config_dir"
cat > "$oaxctl_config_dir/config.json" <<'JSON'
{"url": "https://agentx.oneaxe.cn", "username": "owner"}
JSON
oaxctl login
```

上述配置创建仅用于首次使用；已有配置直接编辑。将 PATH 那一行加入自己的 `~/.bashrc` 或 `~/.zshrc`，新终端即可使用。

密码在登录提示中输入，不写入配置或命令参数；自动化可用受控环境变量 `OAXCTL_PASSWORD`。`OAXCTL_URL`、`OAXCTL_USER` 覆盖配置，`OAXCTL_UA` 自定义 User-Agent。会话自动保存于 `${XDG_STATE_HOME:-$HOME/.local/state}/oaxctl/`，目录 0700、文件 0600。

## 常用命令

```sh
oaxctl whoami
oaxctl agents
oaxctl tasks --agent pay-service
oaxctl show task-...
oaxctl dispatch pay-service '请只读核对当前状态' --intent query --wait
oaxctl say task-... '补充只读范围' --wait
oaxctl wait task-... --timeout 900 --interval 30
```

`agents` 显示就绪原因及能否立即执行，`--json` 输出详情。任务内容支持 `-`（stdin）或 `@文件`。`--intent` 必须符合实际任务：只读用 `query`，变更用 `mutation`。

## 断线与重试

`dispatch`、`say` 发送前会打印幂等键，`say` 同时打印版本。断线后先用 `tasks` / `show` 核对结果；确需重试时，把下面占位值替换为**首次的键、版本和原请求内容**，目标及 intent 也保持一致。`dispatch` 会重读 Agent 的 `organization_id`，若组织归属已变，应先核对原请求，不直接重派。

```sh
oaxctl dispatch AGENT_ID @/path/task.txt --intent query --key ORIGINAL_KEY
oaxctl say TASK_ID @/path/message.txt --key ORIGINAL_KEY --expected-version ORIGINAL_VERSION
```

不要换键重派未知结果。`--wait` 到时仅退出客户端（exit 3），可用 `wait TASK_ID` 继续；不会取消任务。失败、取消或不确定终态为 exit 4，HTTP/配置/传输错误为 exit 2。HTTP 401 可重新 `login`；遇 403 或重定向，核对账户权限、HTTPS 入口及边缘策略。

HTTP 字段与安全边界见 [Web HTTP 客户端契约](../api/http-client.md)。
