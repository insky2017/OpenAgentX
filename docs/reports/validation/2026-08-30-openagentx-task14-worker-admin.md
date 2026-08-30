---
doc_type: validation_report
task: 14
status: passed
updated_at: 2026-08-30
---

# Task 14 验证报告：Worker Admin 与运维控制

## 覆盖内容

- WorkerCommand 持久化 create/claim/ack、generation 绑定和 idempotency。
- Worker control claim/ack HTTP 路由及认证。
- WorkerAdminService 的命令创建和 lease revoke/fencing 推进。

## 验证命令

```text
go test ./internal/controlplane ./internal/api/workerapi ./internal/persistence/sqlite
go test ./...
go vet ./...
git diff --check
python3 scripts/check_docs.py
```

以上命令均通过；冻结 ADR 文件未修改。
