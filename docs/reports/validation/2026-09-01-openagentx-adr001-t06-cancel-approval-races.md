---
doc_type: validation_report
status: passed
owner: openagentx
test_id: T06
updated_at: 2026-09-01
---

# OpenAgentX ADR-001 T06 验证报告

## 结论

T06 通过。Cancel 是 Task 级 `cancel_requested` 意图，不会因当前 RunAttempt 已完成而作为下一 turn 输入；native Approval 严格绑定目标 RunAttempt 和版本，不能跨 run 继承；preflight Approval 仅作为下一次启动的原子前置条件。正式 Runtime 事件现在能够创建 native ApprovalRequest，Panel HTTP 控制路径可以将审批决定交给 Worker 结算。

## 源码与冻结边界

| 项目 | 证据 |
|---|---|
| 源码提交 | `752f58e` |
| ADR-001 SHA-256 | `587e9b8f27ddeb68aae8e5a507fefc10973739c8dd2c3def86e2c0bbc51db403` |
| 测试数据 | 临时 SQLite 与 `httptest` HTTP Server；不使用生产库或真实凭据 |

## 覆盖结果

| 断言 | 证据等级 | 结果 |
|---|---:|---|
| Cancel/finish 双向线性化 | L1 | 两个固定顺序和两个并发 barrier 顺序各重复 100 次，结果稳定；Cancel 先线性化时 Task 最终为 `canceled`，Finish 先线性化后 Cancel 返回 terminal 拒绝。 |
| native Approval/finish 双向线性化 | L1 | 两个并发顺序各重复 100 次；目标 run 已结束时 ApprovalRequest 变为 `stale`，不会生成下一 turn 控制输入。 |
| 幂等与事务完整性 | L1 | 100 路重复 Cancel 仅产生一个 control item；100 路重复 Approval 仅产生一个 Decision/control item；Cancel、Approval 与 preflight 的故障注入均验证状态、Mailbox 与 Journal 同成同败。 |
| preflight 授权 | L1/L2 | BeginAttempt 在同一事务消费匹配且未过期的 preflight Approval；启动回滚不消耗授权，后续 turn 不可复用已消费授权。 |
| 正式 native ApprovalRequest 入口 | L2 | 已认证 Worker 的 `approval.requested` RuntimeEvent 严格解析后，在同一事务创建 ApprovalRequest、更新 Task 为 `waiting_approval` 并写入 Journal；相同事件重试幂等，绑定冲突或畸形 payload 拒绝。 |
| Panel HTTP 控制与 Worker 结算 | L2 | 真实 Handler 测试通过 CSRF、Idempotency-Key 和 HTTP API 提交 Cancel/Approval；Worker claim、payload resolve、accept/finish 后，权威状态与 Journal 一致。 |

## 验证命令

```text
go test -count=1 ./internal/controlplane ./internal/api/panel ./internal/persistence/sqlite
go test -race -count=1 ./internal/controlplane ./internal/api/panel ./internal/persistence/sqlite
go vet ./...
go test -count=1 ./...
git diff --check
```

以上命令均通过。测试输出未记录密码、Cookie、Session Token、代理凭据或完整 fencing token。

## 后续边界

- 新提交尚未成为运行中二进制；T09/T10 必须在部署当前提交后复验生产 HTTPS、移动浏览器和真实 Worker 状态。
- 本关的 `httptest` Handler 证明 Panel API 与 Worker API 的合同闭环；它不替代 T09 所要求的 Nginx、PWA 和真实手机浏览器证据。
