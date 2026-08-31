---
doc_type: test_task
status: pending
owner: openagentx
test_id: T10
updated_at: 2026-09-01
---

# T10：全量回归、证据归档与 Go/No-Go

## 目标

汇总 T01-T09 证据，执行全量自动化与生产非破坏性复验，给出 ADR-001 的最终 go/no-go 结论。

## 自动化回归

```bash
go test ./...
go test -race ./...
go vet ./...
cd web && npm run build
./scripts/check-legacy-control-paths.sh --release
git diff --check
```

回归前必须确认每个已通过关卡的报告对应当前源码 commit、运行中二进制 SHA-256、配置和 schema；若共享代码影响 T01-T09 任一关，先回开并完成受影响关卡回归。T04 的 Runtime Contract Gate、T05 的跨 Task/幂等/defer/鉴权矩阵、T06 的 Cancel/Approval 竞态证据属于强制项，不能以单元测试替代真实 UDS、Runtime 或浏览器证据。

同时验证 schema、daemon/Worker systemd、UDS 权限、Nginx、证书、SSE、PWA Manifest/Service Worker 和目标数据库备份恢复。

## 审计清单

- T01-T09 均有验证报告且状态 PASS；
- 冻结 ADR-001 无修改；
- 当前代码、配置和有效文档没有可执行的 agentbus/tmux/pane/Stop Gate fallback；
- 生产密码、Cookie、Worker Session Token、mTLS 私钥未进入 Git 或报告；
- 所有发现的缺陷均有修复提交和重测证据；
- 每个关卡都遵循单一实现批次、一次完整验证和一次独立审计，未存在重复审计造成的证据冲突；
- OpenAgentX 子仓工作树干净，父仓已有用户改动未被覆盖。

## 结论规则

- 所有核心、安全和恢复门槛通过：`GO`。
- 仅有明确不影响 ADR 核心的观察性问题：记录 residual risk 后由 owner 决定。
- 连续任务、运行中控制、fencing、认证授权、移动指挥任一失败：`NO-GO`。
