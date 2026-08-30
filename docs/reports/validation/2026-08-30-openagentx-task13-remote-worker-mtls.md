---
doc_type: validation_report
task: 13
status: passed
updated_at: 2026-08-30
---

# Task 13 验证报告：远程 Worker mTLS Binding

## 覆盖内容

- TLS 1.3、CA、客户端证书和私钥加载失败即拒绝。
- HTTPS endpoint 强制校验，复用 Worker API client 的全部协议路径。
- UDS 与 HTTPS 使用同一请求 DTO、Bearer Session Token、generation/lease/fencing 校验。

## 验证命令

```text
go test ./internal/client/worker ./internal/transport/remotehttps ./internal/controlplane
go test ./...
go vet ./...
git diff --check
python3 scripts/check_docs.py
```

以上命令均通过；冻结 ADR 文件未修改。
