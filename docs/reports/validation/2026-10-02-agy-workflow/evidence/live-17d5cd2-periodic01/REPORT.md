# Worker 周期恢复真实验证

候选 `17d5cd22b442fb5fd02bd661b36c5db4299fea9d`，**R 定向 PASS**。正式 `agy-graft` 无 capture，独立 profile/DB/socket/端口，未操作正式服务及 60573 主实例。

- 真实 AGY 启动长工具后，仅对本轮 Worker PID/starttime 注入 SIGKILL。daemon PID `331481` 与 starttime `10022832` 全程保持。见 [故障记录](cases/worker-periodic-recovery/fault.json)。
- 42.120 秒后常驻 daemon 自动将旧 Task 与 Run 收口为 `uncertain`；没有人工写库或重启 daemon。见 [逐次观察](cases/worker-periodic-recovery/recovery-observations.json)、[恢复快照](cases/worker-periodic-recovery/recovered-task-run-journal.json)。
- SIGKILL 后部分旧 Runtime 子进程仍存活。已在自动账本恢复后另行以 PID/starttime 精确清理，**这是测试收尾，不是产品自动进程清理能力**。见 [清理前进程](cases/worker-periodic-recovery/old-runtime-before-test-cleanup.json)、[测试清理](cases/worker-periodic-recovery/old-runtime-test-cleanup.json)。
- 新 Worker generation `2` 通过正式网络 test/publish，应用身份/代际/配置回执匹配并就绪；新 query 成功、精确回复且 `query_result_delivered`。见 [新代就绪](cases/worker-periodic-recovery/new-generation-ready.json)、[查询](query-after-worker-periodic-recovery.json)。
- 旧任务最终仍 uncertain，始终只有一个 Run 和一次工具调用，未生成延迟产物。见 [最终旧任务](cases/worker-periodic-recovery/final-old-task-run-journal.json)、[独立证明](cases/worker-periodic-recovery/independent-proof.json)。测试 daemon/Worker/Runtime 已清理，私有原始日志和 HTTP 保留。

这是隔离 R 证据，不代替 I、systemd cgroup 清理或首次向导。正式 Worker/API 诊断不等于 AGY 原始字节流。
