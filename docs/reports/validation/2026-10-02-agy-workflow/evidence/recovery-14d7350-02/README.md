# E17 daemon 故障及原入口跨代网络阻断

候选 14d7350，真实 AGY/capture→agy-graft→agy，独立 profile/database/socket/HTTP；正式服务未修改。命令见 invocation.json，工件/版本见 manifest.json。

已发生：started 标记及 PID/starttime 核验后 SIGKILL 隔离 daemon；Worker 因控制 API EOF 自行退出；等待 305 秒保守上限后重启隔离 daemon，旧 Task/Run 进入 uncertain，恢复 Journal 留存。随后启动 generation 2，其 online，但原网络 binding 仍 applied generation 1、readiness=false，45 秒等待超时。本批 verdict 保持 FAIL，不覆盖为 PASS。

等待上限依据 Run 默认租约 5 分钟及 heartbeat 只续到 max(existing,now+30s)，没有读写数据库来推进租约。API DTO 不暴露 Run lease_until。

该失败同时暴露产品 prepareNetwork 的同样假设，已最小修复为保留原模式并按原版本 CAS 正式 test→publish。受控续验在 ../recovery-14d7350-02-resume01 使用同一私有 DB，不重派旧 Task。旧进程在本批退出时已精确清理。

私有原始资料：/home/sky/.local/state/openagentx/evidence/recovery-14d7350-02。harness-source 保留本批实际夹具；首次失败和续验分别保存。
