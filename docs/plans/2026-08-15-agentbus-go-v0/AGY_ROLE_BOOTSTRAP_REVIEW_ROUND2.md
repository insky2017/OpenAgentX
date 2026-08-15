# AgentBus V0.1 角色 Bootstrap：Codex 审核修正 Round 2

只修改 `AgentBus/`；不要操作真实 panes，不修改父仓库，不提交或 push。无需考虑旧版兼容性。

## 1. Session delivery CAS 失败必须向上返回

当前 `Service.AttachAgent` / `BootstrapAgent` 在 `store.UpdateSessionDelivery` 返回错误时，会修改内存 Session 并继续返回成功。此时 API response 与数据库状态可能不一致。

要求：

- `UpdateSessionDelivery` 失败时，Service 必须返回错误，不能伪造成功 response。
- 保留原始 delivery error 的上下文，错误信息应能同时定位“通知失败”和“持久 delivery 状态失败”。
- 用可控 Store wrapper/mock 强制 `UpdateSessionDelivery` 失败，分别覆盖 attach/bootstrap，断言 Service 返回错误而非 response。

## 2. `agent launch` 必须等待 attach 与 child exit 的先后结果

当前只在 bootstrap delay 阶段监听 `childDone`；同步 attach 期间若 child 先退出，仍可能完成 tmux 注入并把 child 的退出码当作成功。

要求：

- bootstrap delay 结束后，将 attach 放入可取消的并发流程，并同时等待 `attach result`、`childDone`、context cancel。
- attach 完成前 child 退出一律视为受管启动失败并返回非零；取消/等待 attach 流程，不能让 Bootstrap 在函数返回后继续注入。
- attach 只有 disposition 精确等于 `notified` 才算成功；`skipped` 和 `delivery_failed` 都 fail closed、终止并回收 child。
- attach API error 同样终止、等待回收 child 并返回非零。
- 增加确定性 CLI 测试：
  1. connector/HTTP attach 人为延迟，child 在 attach 完成前退出 0，CLI 必须返回非零；
  2. daemon 返回 `skipped`，CLI 必须终止仍在运行的 child 并返回非零；
  3. attach API 失败，CLI 必须终止仍在运行的 child 并返回非零。
- 测试只使用 helper/受控非 Agent 子进程，不启动真实 coding Agent。

## 验证

```bash
cd AgentBus
gofmt -w .
go mod tidy
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./...
go build -o bin/agentbus ./cmd/agentbus
git diff --check
git status --short
```

完成后简要报告修改、测试和结果，不提交。
