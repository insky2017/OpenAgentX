# 最终候选真实 AGY 主链

## 要点

- **R 主链 PASS。** 候选 `f3cd7a32fe54bf1c02bad41d6c6ef8bfc97ddec9` 来自独立 clean clone，Go 嵌入精确 revision 且 `vcs.modified=false`。实际 daemon/Worker `/proc/exe` 均匹配工件 SHA-256 `a17a9ef559ede808a5baf994170419a8f57470c98c7ecb6a0478aa8dd438cbac`。Go/Web 源及摘要见 manifest 和 running-process-provenance。
- **Role 与连续查询。** 用户 prompt 没有给角色码，AGY 从 Role 注入返回正确代码；初始 Task `task-948b3ad4-f197-4b1d-a29f-521b836e9475` 与末项 `task-ff87f458-50fb-454a-8a3a-edd26ad50550` 都达到 `succeeded/query_result_delivered`，均由 `worker-8d73bb15-de22-4aea-af09-e46f9aa77e90`、generation `1` 执行。没有重启该 Worker 才继续工作。
- **文件、验收、继续。** mutation 独立读取确认 `result.txt` 精确等于 `b'OAX-E2E\n'`，正式 owner API 记录 accepted review，同一请求重放不重复生效且历史执行状态不变；关联新 Task 的 `continue_context` 修改后，独立读取精确为 `b'OAX-E2E\ncontinued\n'`。字节长度、实际及预期 SHA-256 在 `cases/mutation`、`cases/continue`，review 记录在 `cases/review`。这证明测试文件正确，不宣称平台拥有通用副作用验证器。
- **排队补充。** 首个真实工具等待中提交补充，work mailbox 从 pending 到 accepted，新的 Run `run-92124c31-d124-4117-bd66-616d8cdb9f43` 消费补充并写出精确 `b'QUEUED-CONSUMED\n'`；首个脚本执行次数严格为 1。完整 Message、Mailbox、两个 Run、Journal 及独立文件证据在 `cases/queued`。
- **E08＋E09 取消。** 真实 A started 后创建 B，先确认 B queued/无 Run；取消 B 并重放同幂等请求后，B canceled、Mailbox superseded、仍无 Run。再取消 A，实际 AGY→python3→sleep 完整进程树在观测上限 `4.0053s` 内全消失，A 最终 canceled。超过原 30 秒窗口后，A 的 `cancel.late.txt` 与 B 的 `queued-canceled.txt` 均不存在。A 已产生 started 标记，不声称取消撤销全部既往效果。
- **证据与生命周期边界。** 本轮直接运行正式 `/home/sky/.local/bin/agy-graft`，**没有 capture 监督器**；因此本轮保留正式 Worker stdout/stderr、原始私有 HTTP、API Runtime 事件/Task/Run/Journal及独立效果，**不声称保留未经拦截的 AGY 原始字节流**。先前 capture 基线和正式无 capture 失败原件均保留，未改写为通过。主实例 `http://127.0.0.1:60573` 按授权保持运行并交主代理浏览器验收；独立短超时实例已清理。这里的网络配置使用正式底层 API，不证明首次向导或实际安装链。公开证据已扫描所有已知隔离 owner 密码值，零命中。

## 下一步

1. 主代理继续该实例上的真实 Web 验收，并另行确认首次向导和 user-systemd 安装链；这些结论不能从 R 主链自动推导。
2. 浏览器验收完成后按私有 profile 中 PID＋starttime 清理专属进程，保留原始证据：`/home/sky/.local/state/openagentx/validation/2026-10-02-agy-workflow/live-f3cd7a3-main01/`。本代理已交出该实例，不再向它发 Task 或改变配置。
