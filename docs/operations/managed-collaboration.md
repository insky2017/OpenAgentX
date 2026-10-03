# 托管领域 Agent：终端工作与自动咨询

本指南适用于由 OAX Worker 执行的 Codex Agent。当前安装 `886ba7f` / schema v5；原 Rhythm / Pay 已按用户授权保留原 thread 迁入托管，真实只读咨询、完整答复与自动续办 PASS，见[当前交付](../reports/validation/2026-10-03-rhythm-pay-managed/DELIVERY.md)、[独立核查](../reports/validation/2026-10-03-rhythm-pay-managed/evidence/independent-review.json)及[日用与切换实录](rhythm-pay-managed-handoff.md)。此前独立身份验证见[历史交付](../reports/validation/2026-10-03-managed-collaboration/DELIVERY.md)。其他 Desktop 会话仍须先确认旧宿主释放，不直接抢占。

## 使用者看到什么

`OAX:<agent-id>.0` 是 Codex 原生交互终端。你输入任务后，OAX 登记 Task，再由该 Agent 的 Worker 使用专属 Codex app-server 执行。终端展示同一后台的过程事件；关闭终端不会停止 Worker，重开仍使用该 Agent 的会话。

需要另一领域的答案时，Agent 发一次 `collaborate ask`。OAX 把咨询保存为对方的待办，对方空闲后回答，结果回来后自动给请求方安排只读续办。无消息时只有程序等待，不会定时叫模型检查收件箱。

## 一次接入

1. 先用 `agent add --runtime codex ...` 新建领域 Agent；已有 CLI 则按[Codex 接入指南](codex-agent-entry.md)交接，确认旧宿主结束当前轮并释放 thread writer 后再 resume；idle 或停止 turn 本身不证明 writer 已释放。Worker、workspace、ROLE 和默认代理由现有接入流程管理。
2. 打开 `openagentx agent open <id> --native`，完成一次对话，使该 Agent 有可核对的真实会话。再登记双方职责与通信许可。
3. 在原生终端让 Agent 运行 `openagentx collaborate instructions --agent <自身ID>`，读取自身身份、职责目录和精确发问命令。让它继续原来的本域工作，无需常驻 watch。

以下使用新建的示例 Agent `app-domain` 与 `pay-domain`，不指向当前两个业务身份。职责目录是组织级完整规则列表；已有目录时先读取并合并现有规则，不能用示例覆盖。

```json
{
  "organization_id": "default",
  "rules": [
    {"scope": "app.entitlements", "owner_agent_id": "app-domain", "description": "应用权益与用户映射，只回答本域问题"},
    {"scope": "pay.payment_api", "owner_agent_id": "pay-domain", "description": "支付接口与回调协议；不负责应用权益和数据迁移"}
  ]
}
```

```sh
# 管理员：首次目录 revision 为 0；已有目录用实际 revision。
openagentx collaborate roles --owner --organization default
openagentx collaborate roles apply --file /absolute/roles.json --expected-version 0
openagentx collaborate enable --agent app-domain --peers pay-domain
openagentx collaborate enable --agent pay-domain --peers app-domain
openagentx fleet up
```

`enable` 从本 Agent 的真实 SessionBinding 验证 thread；已有绑定需要 `--expected-generation <当前值>`。凭据自动保存在专用文件，不需要复制给用户。`automatic_delivery=true` 只表示启用托管投递，在线状态另查 `agent status`。通信凭据有效期为 30 天；过期需管理员核对绑定代次后重新启用。轮换会使旧代次待办进入需核对状态，宜在空闲时完成。

## 发问、回答与观察

让 app-domain 用自己的身份执行：

```sh
openagentx collaborate instructions --agent app-domain
openagentx collaborate ask --agent app-domain --to pay-domain \
  --scope pay.payment_api --key payment-contract-01 \
  --content-file /absolute/question.md
openagentx collaborate status --agent app-domain --message <message_id>
openagentx collaborate inbox --agent app-domain --all
```

`--key` 表示同一次发问；网络断开后重发相同正文和 key 会取回同一个消息与任务。新的问题用新 key。可带 `--origin-task` 关联具体来源任务，省略时关联启用协作时的会话任务。自定义 profile 使用 `instructions` 输出的 socket/session-file 路径。

答复方直接在当前轮给最终答案与证据，后台替它关联发送，不能再手动 reply。同一领域 Worker 忙时持久排队；同 Agent 的协作任务使用登记时的 backend/thread，不能退回其他后台新建会话。请求方消费结果后协议结束，不自动再次回信。消息中的 `task_id` 与 `managed_state` 可核对实际处理；query 成功表示有效回复已交付，不代表发生了业务变更。当前完整答复上限为 32 KiB，事件与错误摘要仍为 4 KiB；实际超限保持 `uncertain`，不伪报成功。

`external inbox --watch` 可在额外终端观察消息，但不是启动协作的必需步骤，也不会调用模型。

## 遇到问题时

| 现象 | 含义与处理 |
|---|---|
| `queued` | 已入持久待办；检查目标 Worker 是否在线、是否正执行其他任务 |
| Worker online，但任务始终 `queued` | 再查 `agent status <id>` 的运行环境就绪状态；若提示 `agent resume`，执行 `openagentx agent resume <id> --no-open`，就绪后原待办继续，不要重发咨询 |
| `needs_review` | 任务失败/取消/结果未知，或职责、绑定、backend发生变化；核对 Task/Run/Journal 后决定下一步，系统不自动重复原任务 |
| scope/owner 拒绝 | 从职责目录找正确 Agent；通信 peer 白名单不等于可回答任意领域 |
| 终端关闭 | 后台仍可处理消息；用 `agent open <id> --native` 重开 |
| Fleet 旧 pane 仍是 Console | 现有活 pane 被保留；先退出该前台，再运行 `fleet workspace --respawn-dead` |
| 原生后台未就绪 | 按 CLI 提示 `agent resume <id> --no-open`，就绪后重开前台 |

本批自动处理只读 consultation，不接受向 managed Agent 的行动 request。query 是结算约定，角色提示是行为约束，都不是操作系统写入沙箱；资金、迁移、部署不在此自动咨询范围。external→managed 的咨询可自动得到答复回原 inbox；managed→external 的人工答复不会自动启动请求方模型。AGY 原生前台适配、未知状态一键恢复及精确只取消目标工具仍待补。
