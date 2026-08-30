---
doc_type: validation_report
task: 09
status: passed
updated_at: 2026-08-30
---

# Task 09 验证报告：恢复、Fencing 与故障注入

## 覆盖内容

- daemon 启动恢复入口 `WorkerService.Reconcile` 与 SQLite `ReconcileExpired`。
- 过期 mailbox claim 重排队，过期 Worker lease 置 offline。
- 过期 Active RunAttempt 标记 `uncertain`，关联 Task 按取消意图确定为 `canceled` 或 `uncertain`。
- 固定时钟验证 lease 边界和恢复幂等，不依赖真实时间。
- 既有 generation、lease、fencing guard 负向测试覆盖 stale Worker 写入拒绝。

## 验证命令

```text
go test -race ./internal/persistence/sqlite ./internal/controlplane ./internal/worker
go test ./...
go vet ./...
git diff --check
python3 scripts/check_docs.py
```

以上命令均通过；冻结 ADR 文件未修改。
