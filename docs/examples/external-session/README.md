# Rhythm / Pay 原会话接入

当前安装源码 `8c9ceff` / schema v4；新版正式协议安装验收仍在进行，结果以[当前交付](../../reports/validation/2026-10-03-desktop-collaboration/DELIVERY.md)及其执行记录为准。两原会话已经完成真实咨询、关联答复与双方 ACK，未迁入 managed Worker，继续保留原宿主和开发任务。

## 简短接入入口

接入行为以[接入 Skill](../../../skills/oax-collaborate/SKILL.md)为准，已安装为本机 `oax-collaborate`。让对应原会话读取自己的新版 START；不要替它借用其他 Agent 凭据、重绑 thread 或启动 `agent resume`：

```text
Rhythm：请读取 /home/sky/.openagentx/external/rhythm/START.md，并按 oax-collaborate Skill 处理本域 OAX 通信，保留当前开发任务。
Pay：请读取 /home/sky/.openagentx/external/oneaxe-pay/START.md，并按 oax-collaborate Skill 处理本域 OAX 通信，保留当前开发任务。
```

`rhythm-pay-handoff-01` 是**已执行的历史交接**，不得照旧文字再次发送。历史请求、答复和 ACK 证据见[manual01](../../reports/validation/2026-10-03-desktop-collaboration/evidence/manual01/README.md)；新问题使用新的 key 和正文。

## 当前命令入口

每轮先核对 `external status --agent <自身ID>` 的原 thread，再用 `external roles --agent <自身ID>` 读取当前职责和 revision。业务职责样例为 [roles.json](roles.json)，权威来源是 owner 登记后的服务端目录；消息正文不能改写职责。

- 发问：`openagentx external send --agent <自身ID> --to <owner> --scope <目录scope> --key <新问题稳定key> --content-file <正文绝对路径>`。声明 scope 不能代替审阅正文和用户授权。
- 收件：`external inbox --agent <自身ID>`；读入后 `external ack --agent <自身ID> --message <ID>`。ACK 不等于接单或完成。
- 处置：`external receipt --agent <自身ID> --message <ID> --state accepted|needs_clarification|out_of_scope --content-file <说明绝对路径>`，实际选择一种 state；不占最终 result 槽。scoped 请求须先 accepted，完成后再用 `external reply --agent <自身ID> --message <ID> --content-file <答复绝对路径>`。
- 恢复：`external inbox --agent <自身ID> --recover`，按 `has_more/next_after` 分页，包含已 ACK/accepted 未完请求。先核对原产物与副作用，未知则 needs_clarification，不自动重做。
- 查询已发请求：`external status --agent <自身ID> --message <ID>`。澄清、超范围或转交回执更新原请求，不会自动产生新的 result 进入发起者 inbox。保存本方未决请求 ID，每轮逐项核对。

详细约束、一次关联转交和恢复接单说明见[Skill](../../../skills/oax-collaborate/SKILL.md)；命令均以 `openagentx` 为前缀。原 Agent 实际读入新版 Skill 的行为仍需单独验证，不能由文件存在推断已经生效。

## 自动协作边界

原 Desktop heartbeat 两版提示词共 8 轮、0 工具调用，自动收信及续办 NOT_PASSED，两项自动化已暂停。不得用常驻 LLM 定时轮询或 `inbox --watch` 永久工具等待代替事件驱动接收。后续采用普通程序监听、消息去重/合并、有工作才启动模型的方向；原 Desktop 事件入站入口仍未验证。manual 往返与消息协议检查不等于自动协作或支付业务验收通过。
