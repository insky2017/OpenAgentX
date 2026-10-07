# Codex Agent 的登记与入口

日常只需记住三步：登记职责 → 启用后台 → 打开终端。后台 Worker 持续接单，模型每轮结束后可以空闲；终端关闭不会关闭 Worker。网页工作台为 [本机入口](http://127.0.0.1:18100)，能观察正式 Task、Run、消息与结果。

## 安装一次，再给 Agent 一句指令

从 OpenAgentX 工程运行以下命令，建立指向仓库单一来源的用户级软链：

```sh
python3 scripts/oax-skill.py install --scope user
python3 scripts/oax-skill.py check --project /absolute/project
```

安装器显式安装两个入口：标准短名 `~/.agents/skills/oax-join` 和兼容长名 `~/.agents/skills/openagentx-join`；仅供某个工程使用时，改用 `install --scope project --project /absolute/project`，写入该工程的 `.agents/skills/`。通常选择一种安装范围。安装器先检查两个入口，不覆盖已有不同目录或链接；重复安装同一来源保持不变。已有指向同一工程的长名链接保留，只补齐短名链接；不同来源的旧链接会报冲突，需要先核对，安装器不替换它。移动或删除来源仓库会使软链失效，届时自检会明确失败。

`install` 只证明链接已建立；`check` 会启动独立、短生命周期的 Codex app-server，通过真实 `skills/list` 分别核对两个名称、对应源码路径和 enabled，既不创建 thread/turn，也不调用模型。它不保证已经运行中的对话立即读入 Skill；在目标 Codex 用 `/skills` 或 `$` 查看并选择 `oax-join`（旧名称 `openagentx-join` 仍可使用），必要时按宿主提示刷新。不要为了刷新技能中断正在托管的任务。

最短调用只需单独输入：

```text
$oax-join
```

Skill 会利用当前目录与已有对话，只补问稳定身份、职责、协作对象等缺失或有歧义的资料。资料已经齐全时直接准备；显示名默认同稳定 ID，当前目录不另问。长名 `$openagentx-join` 以同一方式兼容调用。也可一次提供资料：

> 使用 `$oax-join`，将当前工作准备为长期领域 Agent。稳定名字为 research，职责是本工程研究与实现，需要向 pay-domain 咨询支付接口；工作目录沿用当前目录。保留当前会话历史，整理已完成、下一步及未知副作用，只完成准备并报告回执。

Skill 会根据已有上下文收集四项资料：**稳定名字、工作目录、职责、协作对象**。显示名可默认同稳定 ID；协作对象用明确的空列表表示暂不协作。模型写一份私密 JSON，再调用随 Skill 提供的 helper：

```sh
python3 ~/.agents/skills/oax-join/scripts/prepare.py --inspect
python3 ~/.agents/skills/oax-join/scripts/prepare.py --example
python3 ~/.agents/skills/oax-join/scripts/prepare.py --brief /private/join-brief.json
```

helper 从 `CODEX_THREAD_ID` / `CODEX_SESSION_ID` 自动捕获当前 ID，或接受 brief 中明确提供的 ID；所有来源必须一致。未获得可信 ID 时默认失败，不扫描“最新会话”，也不静默放弃历史。用户明确选择 `history: "summary"` 时才只交接摘要。资料格式见[brief 格式与流程](../../skills/oax-join/SKILL.md)。

它在 OAX 私密目录生成标准 ROLE、HANDOFF、资料来源及原 CLI 回执，复用已安装的 `agent join --prepare`；不写业务工作树。协作对象只是接入意向，不授予通信权限。重复相同输入复用资料；冲突不覆盖。若要隔离验证，可传 `--profile-home /absolute/isolated-profile`，生成的后续 status/resume/open 命令也保留同一 profile。

准备成功必须为 `local_prepared / ready=false`。启用仍需要在原宿主释放之后由管理侧完成；真实会话绑定、职责目录、双方通信许可与咨询验收见[托管协作指南](managed-collaboration.md)。当前会话若已经托管，应先查看自身状态，不能用 join 接管自己。

## 没有安装 Skill 时

> 请先运行 `openagentx agent join --help`。将当前目录与我们约定的职责登记为长期领域 Agent，使用稳定名称；保存简短交接说明，并以 `--prepare` 完成自初始化。只有能够明确获得当前 Codex thread ID 时才传 `--thread-id`，不要猜测。完成本轮后告诉我登记结果以及 resume/open 命令，由我在本轮结束后启用。

这条指令可直接使用，不要求安装 Skill；也可让 Agent 读取本仓库 `skills/oax-join/SKILL.md`。没有明确 thread ID 时，可以明确选择职责与摘要交接，但不能声称完整原生历史已迁入。已知 ID 时，旧 CLI 完成当前轮并退出、实际释放会话后，才启用该身份，避免两个写者同时操作同一会话。

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

标准说明位于 `skills/oax-join/SKILL.md`，`skills/openagentx-join/SKILL.md` 为薄兼容入口。唯一 helper 实现保留在 `skills/openagentx-join/scripts/prepare.py`，短名 `scripts` 通过仓库内相对软链共用，已有外部脚本调用仍兼容。两个 Skill 通过本页安装器显式链接到 Codex 发现目录，不会随普通 `agent join` 隐式安装。该技能用于一次准备，常驻监听由宿主承担，模型无需每轮 poll。

## 在受管终端切换模型

在当前 Agent 的 Codex 终端输入 `/model`，选择模型和推理强度即可。选择保存在 OAX 当前 Agent 的执行配置中；重新打开终端、重启后台后仍保留。其他 Agent 和全局 Codex 配置不变。

新设置用于之后开始规划的 Run，包括队列中尚未执行的任务；已经开始的 Run 保留原模型。模型菜单显示的是下一轮的偏好，正在执行任务实际使用的模型以工作台 Run 记录为准。`requested_execution_json`、`resolved_execution_json` 与实际 Runtime 记录应一致，来源标为 `agent_model_settings:<版本>`。

支持 Codex 0.160.1 的默认交互模式，兼容其模型菜单附带的 `collaborationMode.settings` 模型字段。模型和推理强度按后台启动时的真实 Codex 目录校验，不支持的组合直接报错；目录变化后可重启相应 Worker 刷新。此入口仅保存模型与推理强度，修改工作目录、角色、审批策略或全局配置仍使用对应 OAX 管理入口。

出现旧版“受管终端暂不支持 config/batchWrite”时，说明原生入口或后台还在运行旧工件；单独替换磁盘上的 CLI 不会升级已运行的进程。本次实施与正式安装状态见[修复交付](../reports/validation/2026-10-07-native-model-settings/DELIVERY.md)。

## 日常观察与边界

- `openagentx agent status <id>` 查看是否可开始、当前任务及最近结果。`openagentx agent open <id> --native` 打开原生 Codex；`--console` 打开 OAX 状态终端。两种界面都不承担后台监听职责。
- 原生输入会创建正式 Task/Run；来自网页或 API 的后续任务排队执行。同一个 thread 不会同时启动两轮。未登记活动会话时保留默认新任务新会话、明确 `--thread-id` 加入则续原会话的行为。使用 [`agent new-session`](agent-new-session.md) 完成正式交接后，后续任务采用已发布的新活动会话，旧历史保留。
- 默认代理在创建时保存进 Agent 的私密环境文件。修改代理来源后，已经运行的进程不会自动更新；需要显式调整该 Agent 配置并恢复服务。Codex 当前支持 inherit/direct，命名网络 profile 暂不开放。
- 当前 Codex 0.160.0 单独的 `turn/interrupt` 可能留下工具进程。本实现会核验实际停止，必要时终止该 Agent 的专属引擎，再在下一任务重建。同一引擎中旧任务留下的后台服务也可能停止，打开的原生终端需重开；其它 Agent、共享或外部引擎不会被这样终止。不要把需永久驻留的业务服务托管在该工具引擎中。
- 工具副作用无法核实时保留 `uncertain`。文件 mutation 的 Run 成功而 Task 标记“效果待确认”不代表写文件失败；请结合工作目录中的实际产物验收。取消不会撤销已经写出的文件。
- 真正失联或未知终态会隔离该 Agent，避免盲目重放；目前普通 `resume`/重启不能自动解除这类隔离。需先核对旧任务副作用并做受控恢复，此入口仍待补齐。不要删状态文件或改数据库伪造恢复。

当前精确验证范围见 [Codex 交付](../reports/validation/2026-10-02-codex-workflow/DELIVERY.md) 与 [D/R/I 覆盖矩阵](../reports/validation/2026-10-02-codex-workflow/COVERAGE.md)。
