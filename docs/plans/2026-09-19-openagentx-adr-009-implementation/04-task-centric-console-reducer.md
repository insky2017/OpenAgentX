---
doc_type: implementation_task
status: completed
owner: openagentx
updated_at: 2026-09-19
---

# 任务 04：Task-centric Console reducer

## 目标

扩展纯 reducer，使 focused Task、Task version/status、Mailbox/Run、最终回复、Message/Approval 和连接模式
成为 Console 当前状态的唯一真相，并保持 cursor ack、Worker generation fencing 与 bounded Timeline。

## 范围边界

- 纯 model/reducer 不做网络、tmux、terminal 或文件 I/O；
- 不修改 UI 布局和公开命令语法；
- 不更改服务端状态转换或 CAS 规则；
- 输入只接受 Task 02/03 的安全 read model。

## 验收矩阵

| 类别 | 验收内容 |
|---|---|
| 正向 | dispatch focus、attach 建议 focus、手工切换；queued->claimed->running->waiting->terminal；最终 reply/outcome 分层 |
| 状态/CAS/幂等 | Task version 单调；Run 绑定 Task+Worker identity/generation；重复 event 幂等；合法忽略事件按规则推进 cursor |
| 失败 | 倒退 version/status、无效 identity、跨 Agent/Task Run、terminal 后非法覆盖、无效 safe result 均拒绝且不 ack |
| 竞态 | dispatch response 与 event 乱序；旧 Worker/Run 迟到；Task replacement；snapshot/event ack；mode switch 旧流迟到 |
| 资源 | active/recent tasks、Timeline、result/output 均同时受 count/byte cap；清理不会保留悬空 current selection |
| 证据格式 | 表驱动状态矩阵记录 before/input/after/cursor/ack；property/fuzz 输入不 panic、不泄漏、不无界增长 |

## 实施步骤

1. 定义 focused Task、Task outcome、Runtime reply、Mailbox/Run、control CAS 和 mode/connection state。
2. 建立 Task/Run 合法转换、replacement/offline 清理和 terminal 不回退规则。
3. 保持 snapshot/event 显式 ack：Apply 成功后 callback 才返回，拒绝时 cursor 不推进且 Follow 可安全结束。
4. 生成有意义 Timeline item：Task/Run 状态转换、控制结果、safe output 和 terminal reply；heartbeat burst
   只更新状态栏。
5. 为 Task/history/result 设置 count/byte cap，测试长输出、重复事件和高频状态更新。

## 验证

```bash
go test ./internal/consolemodel ./internal/client/console -count=1
go test -race ./internal/consolemodel ./internal/client/console -count=1
go test ./internal/consolemodel -run 'Fuzz|Property' -count=1
go vet ./internal/consolemodel ./internal/client/console
git diff --check
```

若仓库没有命名为 `Fuzz|Property` 的测试，执行实际新增 test 名并在 log 记录，不得以空匹配冒充通过。

## 退出条件

- reducer 对 Task/Run/Worker/cursor 的状态矩阵和清理规则完整；
- 所有拒绝路径不推进 cursor，合法忽略路径不会无限重放；
- reducer 无 I/O、无 raw payload、无无界集合；
- 创建一个 Task 04 实现提交后停止等待监督 gate。

## 完成记录

- 实现提交：`874a0e12cbc2cb73d0ed403c8e562a6427a102e6`。
- reducer 已统一 focused/active/recent Task、Task version/status、Mailbox、Run、Message、Approval、Task
  outcome、Runtime reply、connection/mode epoch 与 Event cursor；dispatch、手工选择和 Attach suggestion 是
  仅有 focus 来源。
- Task/Run/Worker/stream fencing、same-version 幂等、terminal 冲突、旧 Worker 历史事件、snapshot/event ack
  和 replacement/offline 清理均有回归测试；旧 active Run/native Approval 不污染当前状态，持久化 terminal
  reply 保留。
- active/recent Task、Task bytes、Timeline count/bytes 和单条 Timeline 均有上限；Normal 拒绝 Diagnostic，
  reducer 只接受 Task 02/03 安全投影。
- 定向普通/race、malformed projection fuzz seeds、全仓普通测试、vet、独立 build、release scanner、
  whitespace、链接和冻结 ADR hash 检查通过；Task 05/06 行为、真实运行状态和 ADR-006/007 未修改。
