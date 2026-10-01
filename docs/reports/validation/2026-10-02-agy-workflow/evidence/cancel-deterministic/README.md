# 取消与恢复确定性验证（D）

本批仅为隔离 SQLite repository 单测与 Go race 检测，**不是 AGY 真实 E2E（R/I）**。未部署、未访问真实业务数据库、未发送真实 Runtime 中断。

## 结果

- `01-before`：先新增反例再改实现，exit 1。queued/waiting_input × pending/claimed 四组合均复现 `find active run for cancel: resource not found`；回滚反例未能到达 commit 注入点；取消后 lease 恢复复现 Task canceled / Run uncertain 不一致。
- `02-after`：最小修复后的定向测试，exit 0，2.991s。
- `03-race`：加入迟到 claim 启动拒绝、历史 Run 保留及 recovery 重放无重复 Journal 断言后的最终 `-race` 验证，exit 0，6.043s，无 race 报告。
- 每次实际命令、工作目录、起止时间、退出码在对应 `.result.json`；stdout/stderr 独立保存。`environment.json` 记录 Go 版本、源码基线及最终四个文件 SHA-256。

## 已验证边界

无活动 Run：Task 在取消事务内变 canceled，保留取消主体/时间；所有 pending/claimed mailbox 变 superseded 并清除领取租约；Task/每个 mailbox 有 Journal；版本递增一次，同主体重复调用幂等，旧 CAS 被拒绝；FaultBeforeCommit 回滚 Task、mailbox、Journal；旧 claim 迟到 Begin 在所有权检查处被拒绝；同 Worker 可领取并通过正式 BeginClaimedRunAttempt 启动下一项；原 waiting_input Run 的终态与结果保留。

有活动 Run：复用既有取消/完成先后各 100 次、双 goroutine barrier 先后各 100 次、并发重复取消、未知副作用不隐藏为 canceled、审批 stale 及活动取消事务回滚测试。取消请求仍只提交 cancel_requested 和定向控制项，不在请求事务里声称进程停止。

Lease recovery：取消意图保留，Run 与 Task 均 uncertain，Journal 为 task.uncertain；重复 recovery 不重复写终态事件。原恢复整体回滚测试通过。

## 证据保存与后续

原始持久目录：`/home/sky/.local/state/openagentx/validation/2026-10-02-agy-workflow/cancel-deterministic/`。仓库副本经检查仅含合成 fixture，无实际凭据，保留原 stdout/stderr。`SHA256SUMS` 覆盖本目录证据。

真实 AGY 进程停止、浏览器取消/继续、服务恢复后的物理副作用仍由主代理执行 R/I 验收。本批未提交 Git，由主代理统一提交。
