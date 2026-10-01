# 常驻 daemon 周期恢复（D）

- 真实 E17 首次故障证据由主流程保留：CLI 直接启动的 Worker 被 SIGKILL 后，其 AGY 子进程尚存；常驻 daemon 没有周期回收，过期 Task/Run 一直 running/starting，需手工重启 daemon 才 uncertain。本批不覆盖该首次 R 证据，不把其改写 PASS。
- 有界源码核实见 `bounded-source-review.json`：旧调用仅 daemon 启动，新 Worker 注册只 fence 旧 Worker；`TryClaimMailbox` 被旧 active-status Run 持续阻塞。Run 创建实际被 Worker lease 截短、心跳续租，因此默认无心跳约 30 秒后租约到期，修复后的下一个 10 秒周期将收口（数据库/服务正常前提）。
- 最小修复仅在 daemon 接入 10 秒串行循环，调用原 Reconcile 事务。启动时仍 fail-closed；运行期错误写 slog，按间隔重试；循环随 ctx 停止，在 repository.Close 前 join。SQL/状态机未改，旧任务仅 uncertain，绝不据租约推断成功或已停止。
- `01-focused-race.*` PASS：回收循环错误重试/诊断/ctx退出；真实 SQLite 服务级心跳续租四轮保持活跃，停止心跳后重复回收三次只产生一次 uncertain，已执行 mailbox 保持 accepted，旧 generation 迟到事件拒绝，无自动重派，新 generation 能领取并开始独立新任务；既有事务回滚与取消意图不误判 canceled 测试仍通过。
- `02-daemon-race.*` daemon 整包 race PASS；`03-daemon-vet.*` daemon/controlplane/sqlite vet PASS。未重复全仓全部测试。
- `fix.diff` 包含新文件及修改，`source-SHA256SUMS` 锁定本次工作树源码；结果 HEAD 是提交前基线，后续主代理提交与构建另记录。
- 进程清理由真实 user-systemd 路径保证并待 I 验证；`systemd-template-selected-properties.txt` 只是只读模板检查（control-group、SIGKILL、Restart=on-failure），不冒充实际崩溃试验。CLI 直接 SIGKILL 的一般进程监督机制未扩展。
- 原始持久目录：`/home/sky/.local/state/openagentx/validation/2026-10-02-agy-workflow/periodic-recovery-deterministic/`。本目录为无真实凭据的可复核副本，manifest 为 `SHA256SUMS`。
