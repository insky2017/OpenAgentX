# 外部原会话协作执行记录

基线：产品 `9454a81`，已安装 `6b68eeb`，本轮开始工作树 clean。计划见 [本轮计划](../../../plans/2026-10-03-external-session-collaboration.md)。

## 初始事实

- 用户授权保留两个原 thread 及宿主，实施 OAX 消息通道并做只读双向交接试点。
- 当前产品 managed Worker 有真实 Codex 证据，但没有 external-session 通信模式；历史验收不覆盖本轮。
- 本机 CLI 0.160.0；Desktop 内嵌版本与共享 daemon 不同，不能混用其会话所有权。
- 首次 `app-server proxy` 按 JSONL initialize 超时。后续只读 Unix WebSocket initialize 成功，两个 thread 在共享 daemon 返回 `notLoaded`。这只证明可读历史，没有证明原宿主定位或投递。
- 未向两个原会话发送新指令，未启动它们的 Worker，未改业务仓库。

## 验收状态

实施进行中。E01–E06 均未判定通过；后续逐项链接原始/脱敏证据。
