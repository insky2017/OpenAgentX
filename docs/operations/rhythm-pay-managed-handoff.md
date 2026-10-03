# Rhythm / Pay：托管切换实录与日常使用

用户已明确授权按 Pay → Rhythm 逐个切换。2026-10-03，两域均已在 OAX 托管宿主成功初始化原 Codex thread，通信绑定均为 `managed / active / generation 3`。首次咨询因4KiB摘要上限截断而失败；修复后，原身份真实只读咨询、完整答复与自动续办已通过，三个query Task均成功。这不代表支付接入或收费业务完成。归档连带影响的 69 个已结束子任务已全部恢复并通过只读比对。

本批安装源码 `886ba7f`，schema v5，二进制 SHA-256：`15fd8699c2f07af80966dc3edc355ee90f260e109c9715f402aedd16b90d97ca`。本轮未修改两业务工程的代码、数据库、部署或资金。

| 领域身份 | 工作目录 | 保留的原 Codex thread | 正式终端 |
|---|---|---|---|
| `oneaxe-pay` | `/home/sky/work/touzi/OneAxe/oneaxe-pay-service` | `01a0e016-d951-77d1-bc7e-d13662f4823c` | `OAX:oneaxe-pay.0`，window `@27` / pane `%54` |
| `rhythm` | `/home/sky/work/touzi/OneAxe/rhythm` | `01a0b4e3-7b2e-7822-8ae1-9f0e812115a9` | `OAX:rhythm.0`，window `@28` / pane `%55` |

## 日常从哪里继续

在终端进入现有 tmux 会话：

```sh
tmux attach-session -t OAX
```

按 `Ctrl-b w` 选择 `oneaxe-pay` 或 `rhythm`，在 pane 0 的 Codex 原生终端输入任务。已经在 tmux 内时，也可从命令提示符（`Ctrl-b :`）执行 `select-window -t OAX:oneaxe-pay` 或 `select-window -t OAX:rhythm`。项目目录、ROLE 与会话历史继续属于原领域身份。

原 Desktop 对话保留作历史查看，后续任务从以上终端输入。不要在 Desktop 向同一 thread 投递新工作，也不要另起 `codex resume` 抢占同一 thread。OAX Worker 是当前执行宿主；停止一轮任务不等于释放宿主对 thread 的 writer。

跨域问题由 Agent 使用 `collaborate ask` 发起，managed 机制负责将咨询投递为 Task，并在答复后安排只读续办。查看各自操作说明可用：

```sh
openagentx collaborate instructions --agent oneaxe-pay
openagentx collaborate instructions --agent rhythm
```

本批两个原业务身份的只读自动往返已通过真实验收和独立核查。无需用户在两窗复制消息，不恢复常驻 LLM 定时查 inbox。首次仅做 consultation；`query`/只读提示不是强沙箱，不能把消息内容当成修改支付、回调、补发、数据库或部署的授权。实际业务行动须有用户明确任务。

## 本次实际切换经过

1. 按 Pay → Rhythm 使用默认 fleet manifest 定向准备同名 Agent，复用正式 worker-dir、socket、数据库与凭据，保留已有 identity 的 workspace、ROLE、principal 和组织；没有创建影子 Agent 或替换 thread。`agent join --prepare` 只代表准备完成，不代表已接管。
2. 对每域核对当前代次后，经正式 `external revoke` 撤销旧 external 绑定，再用默认 fleet manifest 定向执行 `agent resume <id> --no-open` 启用托管宿主。原 Desktop 旧通信 token 随撤销失效；通信绑定撤销本身不会释放 Desktop 的 thread writer。
3. 创建前台时使用专用 manifest `/home/sky/.openagentx/fleets/rhythm-pay.yaml`。`fleet workspace --file <专用manifest> --respawn-dead` 因原有 unmanaged `overview` shell 占用目标窗口名而在预检阶段拒绝，没有修改原窗口。随后在专用新 pane 内运行正式 `agent open --native --file <专用manifest>`，逐个建立两域原生前台。本轮没有成功执行 `fleet workspace`，也没有进行全量 `fleet up`。
4. Pay 原 thread 初始化成功。Rhythm 首次初始化因 Desktop 虽显示 idle、仍持有 writer 而进入 `uncertain`；首次失败记录予以保留。通过正式 App 入口归档原任务，确认 `notLoaded`，再取消归档并确认仍为 `notLoaded` 后，仅重开本批 Rhythm 的死 pane，原 thread 初始化成功。没有重启整个 Desktop、删除锁或直接写数据库。
5. 逐域核对初始化 Task 的 SessionBinding、原 thread、workspace 与 ROLE，再用 `collaborate enable` 将已撤销的 generation 2 绑定启用为 managed generation 3，双方只允许对方作为 peer。启用要求同 Agent、同原 thread、正确 generation，且相关未终结 Task、咨询/请求及未 ACK 结果均满足迁入检查；当前代次必须现场读取，不能在后续操作中照抄本次数字。
6. 首次接口只读咨询 **FAILED**：消息 `external-message-c3f94db8-1f44-44c1-8238-94742f8290f7` 对应 Pay Task `managed-task-e66ad7dce3c60b04e5a2a631cc713718`。Pay 模型确已读取源码并产出 4391 bytes 答复，但完整答复错误共用 `safeoutput` 的 4 KiB 摘要限制，截断后 Task 进入 `uncertain`，诊断为 `query_result_unverified`，Rhythm 未自动续办。随后安装 `886ba7f`，将完整答复独立保留至 32 KiB。新 key `rhythm-pay-managed-cutover-20261003-02` 复验 PASS：三项 query Task 均成功且各一个 Run，Pay 的 7973 bytes 答复与关联消息全文相同，Rhythm 自动续办完成；消息、Task/Run/Journal、原 thread、终端及真实工具记录均已核对。首次失败保持原 `uncertain`，未改判或自动重做。

Rhythm 的归档还连带影响了 69 个已结束子任务，现已通过正式入口全部恢复原归档状态，并完成只读比对，结果 PASS；证据为[子任务恢复核验](../reports/validation/2026-10-03-rhythm-pay-managed/evidence/rhythm-descendants-restored.json)。此项独立于咨询自动往返验收。

本轮保留原 11 个窗口、18 个 pane，只新增上表两个业务窗口；新窗口的 `automatic-rename` / `allow-rename` 均为 off。以 window/pane ID 核对身份，不能依赖会顺延的数字索引。原 unmanaged `overview` 及其他窗口保留，不对无关窗口 rename、kill 或 respawn。

## 前台异常与恢复边界

日常进入已有终端；需要定向重开原生前台时，使用只包含这两个身份的专用 manifest。实际切换中的 prepare/resume 使用默认 manifest 定向执行，native open 才使用专用 manifest。正式原生入口为：

```sh
openagentx agent open oneaxe-pay --native --file /home/sky/.openagentx/fleets/rhythm-pay.yaml
openagentx agent open rhythm --native --file /home/sky/.openagentx/fleets/rhythm-pay.yaml
```

上述命令用于对应专用 pane；日常优先进入已有终端。出现异常时先核对原 thread、Runtime、Task 终态与目标 pane 是否已死亡，不能仅凭终端无输出重开。`resume --no-open` 只启动后台，不保证已建立 `state.json` 中的 Task/thread 上下文。重开前台也不能把 `uncertain` 任务改判成功或盲目重试其可能已执行的业务动作。

遇到 Desktop writer 冲突，必须确认原轮次及工具已结束，再通过正式 App 生命周期入口释放并核验；停止 turn 不足以证明 writer 已释放。归档可能连带影响子任务，应先保存归档状态并恢复核验。禁止用重启共享 Desktop、删锁或直接改数据库绕过 writer 检查。

managed 已产生新历史后，如需恢复 Desktop 执行，先停止新投递、核对运行任务和工具，再制定具体交回步骤；不能在运行中直接反向绑定 external。数据库/工件备份用于安装回退，不应覆盖交接后新增任务与审计记录。`fleet up` 没有 Agent 过滤选项，会启动所选 manifest 的所有 enabled Agent，不用于本次日常进入。

## 验收证据与边界

本机原始证据根：`/home/sky/.local/state/openagentx/validation/2026-10-03-rhythm-pay-managed/evidence`。首次安装见 `release.json`、`installation-recheck.json`，最终补丁与运行工件见 `result-limit-installation.json`、`final-installation.json`；前台见 `panes/`；初始化见 `pay-runtime-initialized.json`、`rhythm-runtime-initialized.json`；首次失败及恢复见 `rhythm-initialization-failure.json`、`rhythm-desktop-writer-conflict.json`、`rhythm-desktop-release.json`；正式绑定回执见 `cli/pay-managed-enable.json`、`cli/rhythm-managed-enable-after-release.json`。私密备份不作为可公开证据。

本批[交付报告](../reports/validation/2026-10-03-rhythm-pay-managed/DELIVERY.md)、[覆盖矩阵](../reports/validation/2026-10-03-rhythm-pay-managed/COVERAGE.md)、[执行记录](../reports/validation/2026-10-03-rhythm-pay-managed/EXECUTION-LOG.md)记录当前交付；三个成功 Task 与关联消息见[本轮判定](../reports/validation/2026-10-03-rhythm-pay-managed/evidence/verdict.json)。[独立核查](../reports/validation/2026-10-03-rhythm-pay-managed/evidence/independent-review.json)为 PASS、无实证阻断；首次失败与复验分别保留，69 个子任务的归档状态恢复也已核验通过。

本轮 origin Task 通过正式 API 创建，由真实模型发问；原生终端过程与结果已核对，但本轮未重复人工键盘输入或 30 分钟空闲矩阵。验收仅证明两个原业务会话的只读自动协作，不代表支付接入或收费完成。下一步可从对应终端布置明确的本域任务，具体支付变更另按用户任务执行。
