# E17 受控续验：总项 PARTIAL，人工恢复断言通过

E17 总项不能标 PASS：Worker 崩溃自动收口和子进程清理存在缺口。以下通过仅限有操作介入的恢复断言。

本批使用 14d7350 固定 archive 工件，承接 ../recovery-14d7350-02 的隔离数据库，未重新执行旧 daemon 故障任务。原 FAIL 完整保留；本批先启动隔离 daemon/Worker，以原 binding.Version 执行新 generation 的正式 mode test → publish CAS → applied，再做真实 AGY 查询。没有修改数据库或正式服务。

## 结果

- daemon 故障证据需联合前批读取：SIGKILL 前真实 started/PID、故障及 startup reconciliation 在前批；本批 generation 3 新网络应用、真实查询成功、旧 Task uncertain/唯一 Run/脚本 invocation=1。前批生成的 generation 2 只有 online，不等于 ready。
- Worker 故障完整在本批：真实 AGY 执行 worker-sigkill.py 并产生 started 后仅对 Worker 精确 PID 发送 SIGKILL，等待 305 秒上限。隔离 daemon 重启后旧 Task/Run uncertain。generation 4 完成正式网络测试发布及 applied，真实 AGY 查询成功。
- 两个旧脚本都只调用一次，未产生 600 秒延迟效果。没有重派旧 Task、人工更改状态、pkill 或操作正式 daemon/service。

## 实际限制，不扩大通过结论

Worker 租约及 Run 上限已过后，观察 API 仍为 Task running、Run starting；只有 daemon startup 调用 ReconcileExpired 才改为 uncertain。Worker SIGKILL 后四个已记录 AGY/脚本进程仍存活。harness 以已记录 PID/starttime 精确清理这些进程，之后才运行新任务；本批不证明系统自动清理 Worker 孤儿进程，也不证明无需 daemon 重启即可自动恢复。

正式 CLI agent resume 的跨代网络修复另外由 ../network-generation-deterministic 验证；本批是在旧候选上使用对应正式 API 完成恢复，不是新候选或已安装 CLI 的 I 通过证明。真实崩溃从始至终只发生于隔离实例。

## 可复核文件

- continuation-provenance.json、harness-source/continuation.py：续验关系与实际执行脚本；其余 harness-source 保留依赖快照。
- cases/daemon-sigkill/{new-generation-ready,final-task-run-journal,independent-proof}.json。
- cases/worker-sigkill/{fault,expired-process-state,lease-expired-before-daemon-restart,task-run-journal,old-runtime-controlled-cleanup,new-generation-ready,final-task-run-journal,independent-proof}.json。
- http/：正式请求/响应脱敏副本；runtime/、daemon/Worker日志、harness.log、processes.json；SHA256SUMS。
- 私有原始资料继续保留在 /home/sky/.local/state/openagentx/evidence/recovery-14d7350-02，HTTP原始编号续接，新增daemon/Worker在processes.json记录。前批空的worker-daemon-sigkill stdout/stderr因续验同标签复用保持前批入库副本；其余前批原始运行输出未删除。
