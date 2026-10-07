# 架构与流程文档

本目录是 OpenAgentX `main` 分支的正式架构文档入口；工程外的阅读文件仅为副本。

| 文档 | 用途 |
|---|---|
| [OpenAgentX 长会话恢复](2026-10-07-large-session-recovery.md) | 32 MiB 响应上限故障、单域恢复、原 thread/pane 与真实只读验收 |
| [总体架构七视图](current-architecture.html) | 总览、任务流、领域初始化、AGY运行、事件观察、依赖及Codex原生终端 |
| [架构说明与源码依据](CURRENT_ARCHITECTURE.md) | Markdown说明、实现状态及逐项证据 |
| [事件协作与终端方案](event-collaboration.html) | 终端、Native Bridge、Worker 与协作任务；本批基础 Overview 交付与历史证据分别说明 |
| [新的 Codex 会话怎样加入](codex-session-join.html) | 单独 `$oax-join` 开始准备，长名兼容；新建/已有 CLI/Desktop 三条路线及接管边界 |
| [基础 Overview 操作说明](../operations/overview.md) | 列表与详情滚动、Enter 跳转已有 pane、普通 GET 刷新与断线保护 |

HTML下载后可在浏览器中打开；下载整个工程可保留源码和证据相对链接。GitHub文件页显示源码，不直接运行HTML。

2026-10-05 CLI `eb043ca` 已原子安装，`OAX:overview.0` 已替换旧 shell，安装核验时连接在线并列出 18 个 Agent，见[本批 DELIVERY](../reports/validation/2026-10-05-terminal-overview-join/DELIVERY.md)。daemon / Worker 与六域业务原生终端保留 `886ba7f` 原工件及进程，未重启；六域本批仅修正窗口元数据，新原生入口固化适用于以后启动的终端。业务会话连续性和用户 client 选中位置保持。总览按授权 GET 刷新，不调用模型；安装后的真实 `skills/list` 已确认 `$oax-join` 与兼容长名两个同源入口可见且启用；Skill 准备不等于自动接管。当前 Pay 入口为 `OAX:pay-service.0`，历史报告中的 `oneaxe-pay` 保留原样。

此前使用步骤见[Rhythm / Pay 终端与恢复](../operations/rhythm-pay-managed-handoff.md)，2026-10-03 能力边界见[历史切换验收](../reports/validation/2026-10-03-rhythm-pay-managed/DELIVERY.md)。该批次 overview 普通 shell 的记录仍是历史事实。历史图页浏览器检查、截图和校验记录见[文档验证](../reports/validation/2026-10-03-architecture-refresh/README.md)。

维护总体架构时编辑 `architecture-map.json` 或 `render_architecture.py`，再从工程运行 `python3 docs/design/render_architecture.py`。接入图和事件图分别直接维护本目录的 `codex-session-join.html`、`event-collaboration.html`。本地阅读副本需要同步时，仅适配链接；实现状态与证据正文保持一致。
