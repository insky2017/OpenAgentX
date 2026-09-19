---
doc_type: implementation_task
status: pending
owner: openagentx
updated_at: 2026-09-19
---

# 任务 05：Pane 0 任务 TUI 与控制易用性

## 目标

把 Task 02—04 的权威状态呈现在 `OAX:<agent-id>.0` 全屏 TUI 中：用户能连续看到任务阶段、最终回复、
完整 ID/version，并可对 focused Task 自然地 steer/cancel。

## 范围边界

- 继续使用 Bubble Tea/Bubbles/Lip Gloss 和 typed Msg/Cmd；View 不做 I/O；
- 只调用现有 authenticated official client，不接触 TurnHandle 或 tmux 业务控制；
- 本任务不实现同 pane Diagnostic 模式切换，只保留既有启动式 Diagnostic Attach；
- 不更改 workspace、user-systemd、Fleet 或默认路径。

## 验收矩阵

| 类别 | 验收内容 |
|---|---|
| 正向 | dispatch 自动 focus；状态栏显示 queued/claimed/run/waiting/terminal；详情显示完整 ID/version；安全过程输出和最终 reply 可滚动查看 |
| 状态/CAS/幂等 | `/steer <content>`、`/cancel` 从 reducer 取最新 CAS；显式命令语法无歧义；API 每次恰好调用一次 |
| 失败 | 无 focus、断线、token expiry、状态不允许、CAS stale、API 失败保留 draft/提示，绝不显示 succeeded 或本地排队 |
| 竞态 | 输入编辑时事件/resize/heartbeat/terminal 到达；滚动上看时新事件；control response 与 Task event 乱序 |
| 资源/平台 | bounded Timeline/result；长行；空状态；`80x5`、`20x3`、`8x1`；退出恢复 terminal 且不停止 Worker |
| 证据格式 | 纯 Update/View before/after、fake client call count、ANSI strip 尺寸、PTY alt-screen/exit/pane 保留 |

## 实施步骤

1. Attach header/status 增加 focused Task、Task status/version、RunAttempt、Worker generation 和连接摘要。
2. Timeline 为 Task/Run transition、safe output、Task outcome 和 Runtime reply 提供明确且不混淆的摘要。
3. 新增 active/recent Task overlay 和显式 focus 操作；完整 ID/version 可查看，不以短 ID 驱动控制。
4. 增加 focused `/steer <content>`、`/cancel`，并把显式控制改为冻结的无歧义形式；保留经批准的兼容语法。
5. CAS conflict 触发只读刷新和提示，不自动重试；control pending/succeeded/failed 使用安全结构化 outcome。
6. 保持输入 draft/cursor/focus、scroll offset 和 compact layout 在所有后台 Msg 下稳定。

## 验证

```bash
go test ./internal/cli/console ./internal/client/console ./internal/consolemodel -count=1
go test -race ./internal/cli/console ./internal/client/console ./internal/consolemodel -count=1
go test ./internal/cli/console -run 'TTY|Compact|Task|Control' -count=10
go build -o /tmp/openagentx-adr009-task05 ./cmd/openagentx
bash scripts/check-legacy-control-paths.sh --release
git diff --check
```

## 退出条件

- 用户无需抄写短 ID 即可控制 focused Task；
- pane 0 明确显示任务执行到哪一步及最终回复/无结果原因；
- 输入、滚动、compact layout、terminal restore 和 Worker 独立生命周期有测试；
- 创建一个 Task 05 实现提交后停止等待监督 gate。
