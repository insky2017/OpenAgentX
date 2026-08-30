---
doc_type: validation_report
task: 18
status: passed
updated_at: 2026-08-30
---

# Task 18 验证报告：统一命名、目标 Schema 与旧控制路径删除

## 验证命令

```text
go test ./...
go test -race ./...
go vet ./...
npm run build
./scripts/check-legacy-control-paths.sh --release
python3 scripts/check_docs.py
git diff --check
```

以上命令均通过。Go module 已显示为 `openagentx`，构建产物仅使用 `cmd/openagentx`。

## 删除与命名扫描

`check-legacy-control-paths.sh --release` 输出全部 CLEAN：

- `cmd/agentbus`、旧 connector 和 Hook 示例路径不存在；
- 当前代码、配置和有效运行文档不含旧 `agentbus`/`AGENTBUS_` 命名；
- 不含 TmuxConnector、pane probe/paste/send/capture、生命周期 Hook；
- Agent 配置不含 connector/address；
- 不含旧 active-task/event schema 符号和旧 socket/database 路径。

历史验证文档仍可保留旧名称作为审计记录，但不在当前运行入口、配置或可执行指令中出现。

## Schema 边界

目标 migration 测试通过：空库创建目标 schema、重复 Apply 幂等、非目标旧库返回 `ErrIncompatibleLegacySchema`、未知版本返回 `ErrUnsupportedSchemaVersion`、不完整目标库返回 `ErrIncompleteSchema`。daemon 不执行旧库在线迁移或双写。

## 代码边界

当前运行链为：

```text
openagentx CLI / Command Center
        -> OpenAgentX daemon API
        -> Transactional SQLite + Mailbox + Event Journal
        -> Resident Worker
        -> Runtime Adapter
        -> Agent CLI / ACP Runtime Process
```

终端布局不再参与业务寻址或控制；Worker 只通过 Unix Socket/mTLS Worker API 与 daemon 通信。
