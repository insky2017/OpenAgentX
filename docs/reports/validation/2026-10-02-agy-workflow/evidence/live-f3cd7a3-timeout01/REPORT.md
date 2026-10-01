# 最终候选独立短超时

## 要点

- 候选 `f3cd7a32fe54bf1c02bad41d6c6ef8bfc97ddec9`、正式无 capture `agy-graft`；独立 Worker 配置 `timeout: 35s`，没有 per-task execution override。
- 真实 AGY 启动 60 秒工具，先独立确认 Python 和 sleep 子进程已存活，随后正式 deadline 生效；Task 保持 `uncertain`，没有错误提升为成功或显式用户取消。
- 两个目标进程最终不存在，超过原 60 秒延迟窗口仍无 `timeout.late.txt`。原始 API、Run deadline、Worker 诊断和独立进程/文件证据可交叉核对。
- 超时后同一 Worker 成功完成 query `task-03c30606-4cbd-457c-b3e0-9fa9cbba70a0`；不与主实例的 Worker 混称。实例 daemon/Worker 已按 PID＋starttime 清理，见 `cleanup.json`。

## 下一步

1. 与主链及真实浏览器/安装证据合并判定，不将此单例概括为完整故障场景均已通过。
