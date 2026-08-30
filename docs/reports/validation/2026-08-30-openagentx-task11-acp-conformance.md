---
doc_type: validation_report
task: 11
status: passed
updated_at: 2026-08-30
---

# Task 11 验证报告：Generic ACP Adapter 与 Conformance

## 覆盖内容

- Generic ACP Adapter 的 descriptor、结构化 prompt、JSON stream 事件和 provider session metadata。
- ACP 进程非零退出、断流、解析失败和取消信号的标准结果分类。
- Codex/Claude/OpenCode descriptor catalog 与统一 capability 语义。
- 共享 conformance descriptor/ExecutionSpec 校验入口。

## 验证命令

```text
go test ./internal/runtime/...
go test -race ./internal/runtime/... ./internal/worker
go test ./...
go vet ./...
git diff --check
python3 scripts/check_docs.py
```

以上命令均通过；冻结 ADR 文件未修改。
