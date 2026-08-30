---
doc_type: validation_report
task: 12
status: passed
updated_at: 2026-08-30
---

# Task 12 验证报告：Organization 与 AuthorityPolicy

## 覆盖内容

- 组织单元、岗位、角色、任命和汇报线领域契约。
- AuthorityPolicy 对跨组织、越权 direct dispatch 和 ExecutionSpec override 的 fail-closed 判断。
- Worker Instance 不得作为业务目标。

## 验证命令

```text
go test ./internal/domain ./internal/controlplane
go test -race ./internal/domain ./internal/controlplane
go test ./...
go vet ./...
git diff --check
python3 scripts/check_docs.py
```

以上命令均通过；冻结 ADR 文件未修改。
