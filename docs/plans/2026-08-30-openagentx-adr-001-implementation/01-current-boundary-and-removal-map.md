---
doc_type: implementation_task
status: completed
owner: openagentx
updated_at: 2026-08-30
---

# 任务 01：现状边界与删除映射

## 目标

建立当前 AgentBus/Tmux V0 到 ADR-001 目标架构的精确映射，确认可复用基础、必须重写的耦合点和 Atomic Release 必须删除的全部入口。

## 依赖与入口

- 依赖：冻结的 ADR-001；
- 入口：当前 `OpenAgentX/` 代码、配置、测试和有效文档；
- 本任务只形成实施事实清单，不增加目标运行路径。

## 实施范围

- 盘点 module、CLI、socket、数据库、环境变量、service 和日志命名；
- 盘点 `service`、`client`、`server` 对具体 TmuxConnector 和旧 DTO 的耦合；
- 盘点 pane address、attach/bootstrap/ready、AGY hook/Stop Gate 的代码和测试入口；
- 盘点旧 Task active unique、task-scoped Event 和内联 schema 初始化；
- 标记 SQLite WAL、事务 CAS、UDS、socket ownership、long poll 和 race 测试等可复用机制；
- 形成“保留思想 / 重构 / Release 删除 / 文档归档”四类映射。

## 目标产物

- `OpenAgentX/docs/design/OPENAGENTX_CURRENT_TO_TARGET_MAP.md`；
- 可机器执行的旧路径扫描清单，供任务 18 和任务 20 复用；
- 目标 package 所需的现有测试迁移清单。

## 质量与验证

- 使用 `rg` 覆盖 Go、YAML、JSON、Markdown、systemd 和 shell 配置中的旧符号；
- 每个删除项必须有当前文件/符号、目标处理和最终验证方式；
- 清单不得把 tmux 的日志查看用途误判为控制面依赖；
- 与 ADR-001 的 14 项原子切换要求逐项对照。

## 退出条件

- 所有旧控制入口都有唯一处理结论；
- 可复用基础设施与领域语义明确分开；
- 任务 02 可以据此定义不依赖旧 service DTO 的目标契约；
- 未修改产品运行行为。

## 完成记录

- 产出：[当前实现到目标架构映射](../../design/OPENAGENTX_CURRENT_TO_TARGET_MAP.md)；
- 产出：`scripts/check-legacy-control-paths.sh`，支持 `--inventory` 与 `--release`；
- `--inventory` 建立 3 个禁止路径和 8 类符号命中基线；
- `--release` 在当前 V0 上按预期返回退出码 `1`；
- `go test ./...`、`go test -race ./...`、`go vet ./...` 通过；
- ADR-001 与任务开始时的 `HEAD` 完全一致；
- 本任务未修改产品运行代码。
