# 历史数据库实际迁移（D）

最终候选在实际历史库的一致副本上启动两次，schema 1→2，再打开仍为2。Task、Agent、Profile、Run、Journal 原有所有列逐行摘要保持一致，quick_check=ok。该测试未连接Worker，不执行历史排队工作；正式安装另验。命令、PID、哈希及逐表记录见 result.json，启动日志保留。
