# 定向真实验证：长租约与 query 补充

候选 `17d5cd22b442fb5fd02bd661b36c5db4299fea9d`。**独立复核：long-renewal PASS，query-supplement PASS；原脚本整体 FAIL 原样保留。**

- 正式无 capture AGY 执行 95 秒工具，七个检查点覆盖 90.013 秒；Run 租约推进 89.999 秒，Worker 心跳推进 89.999 秒，周期回收没有误收活动任务。见 [检查点](cases/long-lease/lease-checkpoints.json)。
- `long-lease.late.txt` 精确为 `b'late\n'`，原工具仅一次调用。首 Run succeeded，回复带进度前言，末行为 `LONG-LEASE-DONE`；不满足脚本的纯标记文本断言。见 [首 Run](cases/long-lease/first-run-terminal.json)。
- 运行中补充及同幂等键重放返回相同回执；只有一条补充消息，mailbox accepted、attempts=1。第二 Run 在同 Worker 成功，Task `query_result_delivered` 且精确回复 `QUERY-SUPPLEMENT--lease02`。见 [正式 Task/Run/Journal](cases/long-lease/task-run-journal.json)、[回执](cases/long-lease/supplement-receipt-replay.json)。
- [独立审计](independent-audit.json) 对实际文件、调用记录、正式快照和只读 SQLite mailbox 逐项断言。原 [verdict.json](verdict.json) 保持 FAIL，不覆写成整套 PASS。后续 query 由补充的第二 Run 证明；脚本中的独立新 Task query 未执行。
- 前一次 `live-17d5cd2-lease01` 因仅接受 running 而拒绝领域有效 starting 状态，证据另存。剩余故障恢复在 `live-17d5cd2-periodic01` 单独验证。

这是隔离 R 证据，不代替 I、首次向导或原始 AGY 字节流；本轮进程已精确清理，私有原始资料保留。
