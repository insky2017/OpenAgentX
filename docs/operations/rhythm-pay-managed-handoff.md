# Rhythm / Pay：下一步业务接入方案

这是具体交接方案，尚未执行。当前两个身份继续绑定原 Desktop；本轮独立测试通过不等于它们已经自动协作。用户确认切换后再执行以下交接。

| 领域 | 原目录 | 要保留的 Codex thread |
|---|---|---|
| Rhythm | `/home/sky/work/touzi/OneAxe/rhythm/` | `01a0b4e3-7b2e-7822-8ae1-9f0e812115a9` |
| Pay | `/home/sky/work/touzi/OneAxe/oneaxe-pay-service/` | `01a0e016-d951-77d1-bc7e-d13662f4823c` |

## 切换后怎么用

进入 tmux `OAX`，在 `rhythm.0` 或 `oneaxe-pay.0` 使用 Codex 原生终端。ROLE、项目目录与模型历史继续属于原领域身份。跨域问题用一次 `collaborate ask`；回复自动回来，后台安排只读续办。用户不用在两个窗口复制消息，也不用让模型不断查 inbox。

原 Desktop 对话保留作历史查看；切换后不再向同一 thread 投递新工作。OAX Worker 成为该 thread 的执行宿主，避免两处同时执行。若需要继续让 Desktop 执行，则保持现在的 external 人工收件方式。

## 一次交接的顺序

1. 两个原会话完成当前轮，各保存简短交接：当前需求、未完成工作、正在运行的工具、已确认边界。首次只验证支付接口只读咨询，不改回调、补发、资金或部署。
2. 重新读取 OAX 当前 binding generation、scope 目录和未完成消息。2026-10-03 本轮核对时，两个 binding 均为 external / active / generation 1，未发现未终结的业务咨询；实际执行时重新核对，不硬编码旧状态。
3. 按 Pay → Rhythm 顺序，用 `agent join --runtime codex --id <agent-id> --identity /home/sky/.openagentx/external/<agent-id>/identity.yaml --thread-id <上表原thread> --prepare` 准备同名 Agent 的 Worker。复用已有 identity 中的 workspace、ROLE 路径、principal 与组织；不要仅传 `--role`，该方式会复制 ROLE 并生成不同 profile，导致已有身份冲突。准备回执不能当作已接管。先审阅生成配置与交接，再以 `external revoke --agent <id> --expected-generation <当前代次>` 撤销旧 external 通信绑定，并按 `agent resume <id> --no-open` 流程启用托管宿主。任一步失败保留现场，不重建影子 Agent 或猜测新 thread。
4. 先打开定向原生前台，等待初始化只读 Task 完成；`resume` 仅启动后台，不保证已经建立 `state.json` 的 Task/thread 上下文。复查 Task 的 SessionBinding、真实 thread、workspace 与 ROLE，再运行 `collaborate enable --agent <id> --peers <对方> --expected-generation <撤销后的实际代次>`。只允许已显式撤销的 external 绑定迁到 managed，且必须是同 Agent、同原 thread、正确 generation，无未终结 Task、双方未终结咨询/请求或本方未 ACK 的结果；仍活跃的 external 绑定、不同 thread 与反向迁回 external 均拒绝。新通信凭据由 CLI 保存；原 Desktop 的旧通信 token 已失效。原有职责目录保持。
5. 只发一个新 key 的接口咨询：Rhythm 问 Pay 既有接口约束，Pay 只读答复，Rhythm 自动读入后给出本域接入计划。分别核对消息 ID、双方 Task/Run/Journal、原 thread、终端可见结果及是否发生超范围动作；未过此项不称业务接入完成。

## 如何处理交接失败

尚未启用 managed 时，保持原 Desktop 方式。managed 已开始产生新历史后，先停止新投递、核对已运行任务和工具，再决定恢复原宿主；不能在运行中直接重新绑定 external。保留的 OAX 数据库/工件备份用于安装回退，不应覆盖交接后产生的新任务和审计记录。

本批只解决 consultation 的自动往返与只读续办。将答复转成支付变更、迁移、部署等行动，需要单独明确工作目标和授权，不能让消息正文自动扩大领域职责。

## 只创建两个业务窗口

`fleet up` 没有 Agent 过滤选项，会启动所选 manifest 的所有 enabled Agent。使用只含 `oneaxe-pay`、`rhythm` 的专用 manifest，并在 join、resume、fleet 命令中一致传入 `--file <专用manifest>`；仍使用正式 worker-dir、socket、db 与 credentials。按顺序加入和恢复时可先只包含 Pay，验明原 thread 后再加入 Rhythm。`fleet up --file <专用manifest>` 默认创建原生 `OAX:oneaxe-pay.0`、`OAX:rhythm.0`，复用已有 overview，其他窗口只列为 unmanaged/orphaned，不停止或替换。已有活 pane 不会被覆盖，同名目标若非 OAX 管理窗口则拒绝；无关重名 `zsh` 不需要处理。新窗口可能使现有窗口数字索引顺延，因此前后核对使用 window/pane ID 与 PID，不以数字索引判定身份。不得对无关窗口执行 rename、kill 或 respawn。
