# 原生 TUI 补充输入确认修复

## 范围与验收

本批结果：已实际消费的补充输入从原生 Codex TUI 待发送区移除，避免中断时把旧输入恢复成草稿、继而合并再提交。仅修改 Native Bridge 的输入确认投影；Task/Run/Message、Worker、业务内容与控制面 CAS/幂等入口仍为原权威路径。没有安装、重启或操作生产 pane，没有手工重发真实用户消息，没有直接修改数据库。

验收包含：真实三条补充输入进入同一 Run、消费后待发送区清空、随后中断及新输入不重复执行旧消息；黄金协议集成覆盖 API 回执竞态、同 RPC 重试、回读确认、同文不同 ID 不误去重、正常输出继续流动。独立联合候选复验由主交付记录，不将本报告扩大为完整 ADR 验收。

## 根因与实际重复证据

Codex 0.160.1 TUI 用提交的 `clientUserMessageId` 匹配 `userMessage.clientId`（legacy 为 `UserMessage.client_id`）。当服务端提供 ID，TUI 不再按文本兜底匹配。Bridge 原来只把 TUI ID 用于 OAX 幂等键；Worker 转交上游时，`turn/steer` 的 ID 是持久 OAX Message ID，`turn/start` 的 ID 是 Run ID。原样回传这些确认导致 TUI 留存已经消费的 `pending_steers`。

上游 `on_interrupted_turn` 会把未确认输入合并恢复到草稿；Esc 立即提交路径还会合并并重新提交。新提交生成新 UUID，不能把这种新身份、合并内容的提交解释成同 RPC 重试，也不能无条件按文本删除用户的合法重复指令。

[生产只读摘要](evidence/production-duplicate-readonly.json) 证明 10:27:32 UTC 创建的 6644 字节补充消息，完整包含之前九条已提交的消息，并于 10:27:39 UTC 出现在真实 rollout 的 `item_completed/UserMessage`。这已是实际重复送达，不只是残留显示。证据仅保存 ID、时间、长度、SHA-256 与包含关系，未保存真实用户全文。官方协议页、对应 0.160.1 原生源码与实测黄金消息来源见[协议证据](evidence/protocol-provenance.json)。

## 最小修复

- Bridge 用正式控制 API 的 Message 回执及既有 Task/Run 状态建立 OAX ID → 原始 TUI 提交 ID 映射，投影真实上游确认事件与线程历史读取响应；不制造消费确认，不按内容猜测去重。
- `turn/steer` API 调用与映射登记处于同一输入互斥段，防止 Worker 在 API 返回前已发送确认导致关联丢失。正常非输入输出不获取该锁。新 `turn/start` 沿用现有事件排序缓冲，在已知 Run ID 后才释放通知。
- RPC 回执缓存及稳定客户端幂等键维持原行为。同 Bridge WebSocket 重连与读历史复用映射。映射不跨 Bridge 进程持久化；重新启动旧 TUI/Bridge 的历史草稿恢复不在本批自动迁移范围内。已有控制面先按确定 Message ID 探测重放，再检查 CAS，原 ID 重试可以重新取得对应回执。

## 验证与边界

- `/home/sky/tools/go/bin/go test -race ./internal/nativebridge` 通过：[结果](evidence/nativebridge-race.txt)。黄金协议测试使用实际 Codex 0.160.1 `thread/items/list` 消息形状，仅替换身份和内容；它是协议集成证据，不是真实模型 E2E。
- 真实隔离测试由 `scripts/validation/native_pending_input_e2e.py` 执行，复用既有独立 tmux/profile、正式 API、真实 Worker/Bridge/Codex TUI。每次保留 Task/Run、PTY、终端视图与文件效果，结果另附。
- 首次真实夹具失败：提示仅说 Python，模型运行 `python -c`，本机该命令不存在，exit 127；它遵守仅一条命令约束未重试，未生成开始文件，随后夹具等待超时。已保留首次失败与清理证据；最小调整为精确 `python3`，在新的隔离 profile 复验。该失败不是输入确认通过，也未删除证据。
- 本批不宣称跨 Bridge 进程持久映射，不补发历史业务输入，不做按内容去重，不将运行时成功等同于业务任务完成。生产旧 UI 的清理与统一候选发布由主批次按其已获授权范围处理。

## 真实隔离结果

结论 **PASS_INPUT_ACK_WITH_MANUAL_VIEW_REOPEN**：三条补充各实际 append 一次，TUI 消费后清空队列，Esc 后未恢复旧草稿。随后自动重连失败：取消背景工具返回缺失 osPid，既有 fallback 终止原 app-server 并产生新 endpoint，旧 Bridge 仍连接旧端点；ready 与 cancel-processes 原 PID 一致。本批不修改这一取消路径。详见 [实际结果](evidence/live02/pending-input-result.json) 和同目录取消证据。

在同 thread **手工重开 view** 后，新输入写出 FRESH_OK，旧 ACK_A/B/C 仍各只有一行；三个 Task（初始化、主测试、新输入），原测试单 Run、三条 supplement。fresh Run succeeded，但 Task 仍 uncertain/business_effect_unverified，文件效果已独立核验，没有改写控制面结论。首次计数误把 instruction 包含在 supplements，纠正后只读复核；一次收集器文件名录入错误仅影响读取。实际原驱动和后续重开步骤均保留，最终整理的驱动未整套重跑。

候选 SHA-256 `1c410ea963f54ce6006044bf3802a4932fbaf59684fa452dcc0ccf60b34c450f`，最终产品源码重建字节一致。两个隔离 profile 已安全停止，证据保留。正式环境未安装或重启。联合候选自然完成与继续输入由主批次独立复验。
