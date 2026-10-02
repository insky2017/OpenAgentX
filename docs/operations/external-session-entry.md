# 让已经工作的 Agent 使用 OAX 通信

适合两个仍在 Codex Desktop/CLI 中开发、希望保留原会话的领域 Agent。与 `agent join/resume` 的 managed Worker 模式不同：外部会话模式只登记通信身份，不启动模型或 Worker，不迁移历史。

**本轮边界：持久通信通道正在验收；当前 Desktop 的自动排队唤醒尚未接通。** `active` 表示通信绑定有效，不表示原宿主在线。`pending` 表示消息已入 OAX；`acknowledged` 表示收信 Agent 显式确认读入。回复是关联消息，不代表业务独立验收。

## 首次接入

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
openagentx external send --agent rhythm --to oneaxe-pay \
  --key rhythm-pay-handoff-01 --content-file /absolute/request.md
openagentx external inbox --agent oneaxe-pay
openagentx external ack --agent oneaxe-pay --message <message_id>
openagentx external reply --agent oneaxe-pay \
  --message <message_id> --content-file /absolute/reply.md
openagentx external inbox --agent rhythm
```

正文保存普通 Markdown 文件，附事实、问题和证据路径；不含密码、Token 或完整配置秘密。发信需要稳定 key；命令超时使用相同 key、相同内容重试，不会创建第二条。不同内容复用 key 会被拒绝。reply 自动指向原发信人；一个请求只允许一条结果，不会自动给结果再回结果。额外问题用新的 consultation/key 明确提出。

默认 inbox 只显示未确认消息；读取不会自动 ack，先读清楚再确认。`--all` 查看历史；`next_after` 是下一页游标，`has_more` 为 true 时用 `--after` 继续。`external status --agent <id> --message <message_id>` 查看双方可见的单条消息。确认只表示已读，不是业务完成。

## 观察与自动化边界

```sh
openagentx external inbox --agent rhythm --watch
```

这个命令持续观察 OAX 收件箱，Ctrl-C 退出。它不启动/恢复模型，也不向 Desktop 输入。不要让 LLM 在工具调用中永远等待它来假装后台监听。

当前 Desktop 内嵌 app-server 使用独立 stdio；共享 daemon 对上述两 thread 返回 `notLoaded`。Desktop 自己的 queued-follow-ups 不是已证接入的 app-server queue。现有内部直接消息入口会选择即时发送/steer，也不满足只排队约定。因此**两个原会话自动收信、闲时唤醒与回复后续办还没有通过验收**。完整目标需要宿主提供 queue-only 与稳定回执，或另行决定迁入已有 OAX managed runtime；本轮没有替用户迁移。

详细证据与安装状态见[本轮执行记录](../reports/validation/2026-10-03-external-session/EXECUTION-LOG.md)。
