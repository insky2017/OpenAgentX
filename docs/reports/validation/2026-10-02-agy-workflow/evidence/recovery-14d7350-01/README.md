# E17 首次夹具失败

真实 AGY 已执行 daemon-sigkill.py 并产生 started 标记；脚本在读取 Observe Task DTO 的 lease_until 时抛出 KeyError，尚未发送故障 SIGKILL。该 DTO 不暴露领域层此字段。

verdict.json 保留失败。runtime-cleanup.json 与 cleanup.json 记录按精确 PID/starttime 的清理；不得把本批列为恢复通过。私有原始资料位于 /home/sky/.local/state/openagentx/evidence/recovery-14d7350-01。
