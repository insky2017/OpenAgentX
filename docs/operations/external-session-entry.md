# 让已经工作的 Agent 使用 OAX 通信

适合两个仍在 Codex Desktop/CLI 中开发、希望保留原会话的领域 Agent。与 `agent join/resume` 的 managed Worker 模式不同：外部会话模式只登记通信身份，不启动模型或 Worker，不迁移历史。

**已安装 `8c9ceff` / schema v4，支持持久通信、职责目录和未完成请求恢复。** 两原会话已经完成首条真实咨询、关联答复及双方ACK。Desktop原生heartbeat试点能启动原thread，但8轮均未执行收信工具，且产生长上下文调用成本；两项试点已暂停，自动续办未通过。`active`仅表示通信绑定有效；ACK仅表示已读；回复不代表业务独立验收。当前证据见[本批交付](../reports/validation/2026-10-03-desktop-collaboration/DELIVERY.md)。

## 首次接入

本机 `rhythm`、`oneaxe-pay` 已登记并完成下列绑定和首轮通信，不要重复初次bind或重发`rhythm-pay-handoff-01`。更新后的入口是 `/home/sky/.openagentx/external/rhythm/START.md` 和 `/home/sky/.openagentx/external/oneaxe-pay/START.md`。两会话后续需实际读取新版[接入Skill](../../skills/oax-collaborate/SKILL.md)，不能把本机安装文件当成模型已经读入；全局副本已安装在 `~/.codex/skills/oax-collaborate/SKILL.md`。

管理员先用 `openagentx agent apply --file <identity.yaml>` 登记身份资料（不启动 Worker），然后执行：

```sh
openagentx external bind --agent rhythm --host codex-desktop \
  --thread 01a0b4e3-7b2e-7822-8ae1-9f0e812115a9 --peers oneaxe-pay
openagentx external bind --agent oneaxe-pay --host codex-desktop \
  --thread 01a0e016-d951-77d1-bc7e-d13662f4823c --peers rhythm
```

bind 使用已有 owner 登录，生成的通信凭据自动保存在 `~/.openagentx/external/<agent>/credentials.json`，不打印秘密。Agent 之后使用自己的凭据；不需要 owner 密码。host/thread 目前是用户明确的映射，不能将登记回执当成连接证明。

同 Agent 或同 Codex thread 不能同时成为 OAX managed Worker。换绑或轮换须显式传当前 `--expected-generation`，旧凭据失效。凭据丢失先 `external status --agent <id> --owner` 读代次，再轮换；不要反复盲试新绑。

## 原会话使用

将对应[接入文字](../examples/external-session/README.md)交给原会话执行一次。它可以立即发信、收信、确认和回复。以下命令都是真实 CLI 入口：

```sh
openagentx external --help
openagentx external status --agent rhythm
openagentx external roles --agent rhythm
openagentx external send --agent rhythm --to oneaxe-pay \
  --scope pay.callback_protocol --key <新的稳定咨询key> --content-file /absolute/request.md
openagentx external inbox --agent oneaxe-pay
openagentx external ack --agent oneaxe-pay --message <message_id>
openagentx external receipt --agent oneaxe-pay --message <message_id> \
  --state accepted --content-file /absolute/receipt-note.md
openagentx external reply --agent oneaxe-pay \
  --message <message_id> --content-file /absolute/reply.md
openagentx external inbox --agent rhythm
```

正文保存普通 Markdown 文件，附事实、问题和证据路径；不含密码、Token 或完整配置秘密。发信需要稳定 key；命令超时使用相同 key、相同内容重试，不会创建第二条。不同内容复用 key 会被拒绝。reply 自动指向原发信人；一个请求只允许一条结果，不会自动给结果再回结果。额外问题用新的 consultation/key 明确提出。

默认 inbox 只显示未确认消息；读取不会自动 ack，先读清楚再确认。`--all` 查看历史；`next_after` 是下一页游标，`has_more` 为 true 时用 `--after` 继续。`external status --agent <id> --message <message_id>` 查看双方可见的单条消息。确认只表示已读，不是业务完成。

## 职责、接单与恢复

本机已登记Pay的登记/支付接口/回调协议/资金记录/凭据交接，以及Rhythm的用户映射/登录迁移/权益核验/应用数据库迁移/接收器部署，共10项职责。以 `external roles --agent <id>` 返回的目录和revision为准。目录仅owner可按版本CAS更新，示例配置见[roles.json](../examples/external-session/roles.json)；不要再次按首次revision=0覆盖已配置目录。

目录内Agent发起新请求必须声明scope，服务在发送、accepted、最终reply时校验负责人，错误目标返回`OUT_OF_SCOPE`和owner。正文混合或越界仍由Agent判断，不能把scope标签当语义保证。Pay只能给自己的接口约束和证据，Rhythm侧是否完成必须由Rhythm确认。

`receipt --state accepted|needs_clarification|out_of_scope`独立记录处置，不占唯一result；scoped请求须accepted后才能reply。超范围明确说明负责人；显式授权内可用`external forward`关联转交一次，不扩大peer，也不自动唤醒。回执更新原请求，发起者用`status --message`检查自己的未决请求，不能只等待新inbox消息。

`external inbox --agent <id> --recover`列出非终结请求，包括已ACK/accepted者；每轮从完整恢复快照重新分页。恢复前核对已完成步骤及副作用，未知效果转needs_clarification；从澄清恢复须新note说明新证据，并检查返回状态。未配置目录的Agent保留旧无scope兼容；进入目录后，旧无scope未完请求不能直接接单，需澄清/正确范围的新请求。既有已完成历史不改写。

## 观察与自动化边界

```sh
openagentx external inbox --agent rhythm --watch
```

这个命令持续观察 OAX 收件箱，Ctrl-C 退出。它不启动/恢复模型，也不向 Desktop 输入。不要让 LLM 在工具调用中永远等待它来假装后台监听。

普通程序运行watch不调用模型，可用于低成本观察。它与原会话执行入口是两回事。当前Desktop尚无已验证的外部事件驱动投递入口；原生heartbeat实际触发但不执行工具，不能作为常驻协作方案。未来应在消息到达且可执行时合并/去重后唤醒一次，空闲不调用模型。或者由用户另行选择已有OAX managed runtime；本轮保留两个原宿主，没有迁移、重启或并发resume。

替代方案见[事件触发协作](event-driven-collaboration.md)。详细证据与安装状态见[本轮执行记录](../reports/validation/2026-10-03-desktop-collaboration/EXECUTION-LOG.md)。
