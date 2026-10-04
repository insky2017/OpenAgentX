# 架构与流程文档

本目录是 OpenAgentX `main` 分支的正式架构文档入口；工程外的阅读文件仅为副本。

| 文档 | 用途 |
|---|---|
| [总体架构七视图](current-architecture.html) | 总览、任务流、领域初始化、AGY运行、事件观察、依赖及Codex原生终端 |
| [架构说明与源码依据](CURRENT_ARCHITECTURE.md) | Markdown说明、实现状态及逐项证据 |
| [事件协作与终端方案](event-collaboration.html) | 终端、Native Bridge、Worker、app-server与消息任务如何配合 |
| [新的 Codex 会话怎样加入](codex-session-join.html) | Skill 现状、新建/已有 CLI/Desktop 三条路线、接通判据与最小改进建议 |

HTML下载后可在浏览器中打开；下载整个工程可保留源码和证据相对链接。GitHub文件页显示源码，不直接运行HTML。

当前使用见[Rhythm / Pay 终端与恢复](../operations/rhythm-pay-managed-handoff.md)，实际能力边界见[切换验收](../reports/validation/2026-10-03-rhythm-pay-managed/DELIVERY.md)。图页的浏览器检查、截图和校验记录见[文档验证](../reports/validation/2026-10-03-architecture-refresh/README.md)。

维护总体架构时编辑 `architecture-map.json` 或 `render_architecture.py`，再从工程运行 `python3 docs/design/render_architecture.py`。事件图直接维护本目录的 `event-collaboration.html`。本地阅读副本需要同步时，仅适配链接；实现状态与证据正文保持一致。
