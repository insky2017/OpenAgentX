# Task 02：CLI 与 TmuxConnector

## 目标

让现有 Codex/AGY 通过同一个 `agentbus` CLI 使用 Hub，并让 Hub 能向注册的 tmux pane 发送安全短通知。

## 实施范围

- CLI subcommands：`serve`、`agent register/list/get`、`task submit/get/list/ack/status/send/complete/fail/cancel/watch`；
- Unix socket Client；
- JSON stdout、诊断 stderr、稳定非零退出码；
- Agent actor 与 Task sender/target 权限校验；
- TmuxConnector pane probe 与通知；
- 唯一 buffer、stdin `load-buffer`、direct argv；
- delivery disposition/Event 与 daemon 日志。

## 必须满足

- 任务正文、结果和错误不进入 tmux 通知；
- 未校验的用户文本不成为 shell 或 tmux command 参数；
- pane 不存在、tmux 不可用和 paste 失败均可诊断；
- Connector delivery 不改变 Task 的业务终态；
- 不捕获 pane 内容作为 Agent 回复。

## 验收

- fake command runner 单测覆盖 argv 与失败路径；
- 临时 tmux session 验证通知注入；
- CLI 端到端覆盖完整成功路径和主要拒绝路径。
