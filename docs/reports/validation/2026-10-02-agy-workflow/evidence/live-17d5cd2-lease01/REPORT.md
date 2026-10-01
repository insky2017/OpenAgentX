# 首次验证夹具失败（保留）

脚本在首个检查点错误地要求 Run 必须为 `running`。实际 Task 为 running、真实 Python/sleep 已启动，Run 为领域有效活动态 `starting`，租约有效。原 verdict 保持 FAIL；本轮进程已清理。修正活动态断言后的复验见 `../live-17d5cd2-lease02/REPORT.md`，不以本次证明完整长等待或恢复通过。
