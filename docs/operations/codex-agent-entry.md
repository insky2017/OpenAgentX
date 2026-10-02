# Codex Agent 的登记与入口

日常只需记住三步：登记职责 → 启用后台 → 打开终端。后台 Worker 持续接单，模型每轮结束后可以空闲；终端关闭不会关闭 Worker。网页工作台为 [本机入口](http://127.0.0.1:18100)，能观察正式 Task、Run、消息与结果。

## 给正在工作的 Agent 的一句指令

> 请先运行 `openagentx agent join --help`。将当前目录与我们约定的职责登记为长期领域 Agent，使用稳定名称；保存简短交接说明，并以 `--prepare` 完成自初始化。只有能够明确获得当前 Codex thread ID 时才传 `--thread-id`，不要猜测。完成本轮后告诉我登记结果以及 resume/open 命令，由我在本轮结束后启用。

这条指令已可使用，不要求安装 Skill。仓库的薄 Skill 只是重复使用时的说明入口。没有明确 thread ID 时，可以传递职责和交接摘要，但不能声称完整原生历史已迁入。已知 ID 时，旧 CLI 完成当前轮并退出后，才启用该身份，避免两个写者同时操作同一会话。

## 新建与迁入

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

## 日常观察与边界

- `openagentx agent status <id>` 查看是否可开始、当前任务及最近结果。`openagentx agent open <id> --native` 打开原生 Codex；`--console` 打开 OAX 状态终端。两种界面都不承担后台监听职责。
- 原生输入会创建正式 Task/Run；来自网页或 API 的后续任务排队执行。同一个 thread 不会同时启动两轮。默认新任务新会话；明确 `--thread-id` 加入的长期 Agent 延续该会话。
- 默认代理在创建时保存进 Agent 的私密环境文件。修改代理来源后，已经运行的进程不会自动更新；需要显式调整该 Agent 配置并恢复服务。Codex 当前支持 inherit/direct，命名网络 profile 暂不开放。
- 当前 Codex 0.160.0 单独的 `turn/interrupt` 可能留下工具进程。本实现会核验实际停止，必要时终止该 Agent 的专属引擎，再在下一任务重建。同一引擎中旧任务留下的后台服务也可能停止，打开的原生终端需重开；其它 Agent、共享或外部引擎不会被这样终止。不要把需永久驻留的业务服务托管在该工具引擎中。
- 工具副作用无法核实时保留 `uncertain`。文件 mutation 的 Run 成功而 Task 标记“效果待确认”不代表写文件失败；请结合工作目录中的实际产物验收。取消不会撤销已经写出的文件。
- 真正失联或未知终态会隔离该 Agent，避免盲目重放；目前普通 `resume`/重启不能自动解除这类隔离。需先核对旧任务副作用并做受控恢复，此入口仍待补齐。不要删状态文件或改数据库伪造恢复。

当前精确验证范围见 [Codex 交付](../reports/validation/2026-10-02-codex-workflow/DELIVERY.md) 与 [D/R/I 覆盖矩阵](../reports/validation/2026-10-02-codex-workflow/COVERAGE.md)。
