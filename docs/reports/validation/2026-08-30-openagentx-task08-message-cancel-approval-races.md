---
doc_type: validation_report
task: 08
status: passed
updated_at: 2026-08-30
---

# Task 08 验证报告：Message、Cancel 与 Approval 竞态

## 覆盖内容

- Task Cancel 在事务内进入 `cancel_requested`，并为当前 Active Run 创建 control-lane Cancel；重复请求不重复投递。
- `FinishRun` 在 Message/Finish、Cancel/Finish 的版本推进后仍能线性化，取消意图不会被迟到结果覆盖。
- native Approval 绑定 RunAttempt version；目标 Run 不存在、版本不匹配或已结束时转为 `stale`，不跨 Run 继承。
- preflight Approval 按 Task、scope digest 和有效期一次性消费。
- Worker Active Run Manager 对 Steer、DecideApproval、RequestCancel 保持单 actor 串行调用；目标不匹配的控制项为 `superseded`。

## 验证命令

```text
go test ./internal/persistence/sqlite ./internal/controlplane
go test -race ./internal/persistence/sqlite ./internal/controlplane ./internal/worker
go test ./...
go vet ./...
git diff --check
```

以上命令均通过。冻结 ADR 文件未修改。
