---
doc_type: validation_report
task: 15
status: passed
updated_at: 2026-08-30
---

# Task 15 验证报告：Web Auth、HTTP API 与 SSE 基础

## 覆盖内容

- Argon2id + 随机 salt 密码摘要与错误密码拒绝。
- 高熵 Session、idle/absolute 过期、立即撤销。
- Secure/HttpOnly/SameSite=Strict Cookie、Auth 登录/Session/登出 handler。
- owner/operator/viewer 角色判断和 CSRF constant-time 校验。

## 验证命令

```text
go test ./internal/auth/web ./internal/api/auth
go test ./...
go vet ./...
git diff --check
python3 scripts/check_docs.py
```

以上命令均通过；冻结 ADR 文件未修改。
