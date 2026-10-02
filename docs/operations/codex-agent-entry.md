# Codex Agent 的登记与入口

`agent add` 默认继续使用 AGY。新建 Codex Agent：

```sh
openagentx agent add --runtime codex --id research --name Research \
  --workspace /absolute/project --role /absolute/project/ROLE.md
```

已有 CLI 在当前轮准备交接时，先看 `openagentx agent join --help`，再执行：

```sh
openagentx agent join --id research --name Research \
  --workspace /absolute/project --role /absolute/project/ROLE.md \
  --handoff-file /absolute/project/HANDOFF.md --prepare
```

`join` 默认 `--runtime codex --prepare=true`，只创建本地身份、Worker 配置、私密环境和准备回执，Fleet 条目禁用。它不会启动 Control 或 Worker，也不会注册数据库身份、领取任务或声称已绑定运行中的 CLI。角色和交接文件保存私密快照；相同输入可重复执行，冲突拒绝。

已知原线程 ID 时可明确传 `--thread-id`；已知受管 app-server 地址时可传 `--endpoint`。不自动猜测 Desktop 会话。线程的首次交接及后续续接仍由运行时与 Task 账本决定，准备回执不是 live readiness。

当前轮结束后由用户激活：

```sh
openagentx agent resume research
openagentx agent status research
openagentx agent open research --native
```

首次 resume 会按现有管理入口注册身份，可能需要本地 owner 密码；可用 `--password-file` 指向私密文件。成功登记后保留输入指纹，后续 resume 不重复注册。只有 Worker、网络和 readiness 全部通过后，才持久启用该 Agent 的 Fleet 条目，供后续批量启动；失败保留禁用状态。当前 CLI 不应在仍执行自己的 turn 时调用 resume 来接管自己。

`--native` 使用受管桥接，输入进入正式 Task/Run 调度；桥接未就绪会报错，不回退到裸 `codex resume` 或 tmux 按键输入。`--console` 仍可打开既有状态 Console。

新 Codex Agent 的代理优先保留既有私密 `<worker-dir>/<id>.env`；也可显式导入 `--environment-file`。无既有文件或显式来源时，从用户 `~/.config/mihomo/config.yaml` 的 `mixed-port` 读取本机代理端口并持久保存；不猜测固定端口。`NO_PROXY/no_proxy` 包含回环地址，保存 `CODEX_HOME` 和已提供的最小 OpenAI provider 环境，不修改全局 Codex 配置。

仓库薄技能位于 `skills/openagentx-join/SKILL.md`，没有自动安装到全局技能目录。该技能用于一次自初始化，常驻监听由宿主承担，模型无需每轮 poll。
