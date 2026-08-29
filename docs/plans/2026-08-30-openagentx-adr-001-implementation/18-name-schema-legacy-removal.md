---
doc_type: implementation_task
status: pending
owner: openagentx
updated_at: 2026-08-30
---

# 任务 18：统一命名、目标 Schema 与旧控制路径删除

## 目标

在正式发布分支完成 OpenAgentX 唯一命名、最终目标 schema 和旧 Tmux/Hook/Session 控制路径删除，不保留兼容 alias 或 fallback。

## 依赖与入口

- 依赖：任务 17 / G5；
- 入口：任务 01 删除映射和全部目标能力；
- 本任务是原子发布准备，不开放生产业务入口。

## 实施范围

- 将 Go module/import、二进制和 CLI 统一为 canonical `openagentx`；
- 将 socket、数据库、环境变量、service unit、日志字段和 Manifest 统一为 OpenAgentX 命名；
- 删除 `cmd/agentbus`、旧环境变量 fallback 和旧 service；
- 删除 TmuxConnector、pane probe/paste/send/capture、connector/address Manifest 字段；
- 删除 attach/bootstrap/launch pane 生命周期与 AGY Stop Gate 调度；
- 删除 Task 级 active unique 和 task-scoped Event 旧 schema；
- 固化最终版本化 schema，目标 daemon 拒绝旧数据库；
- 同步 README、当前架构、运行说明和所有有效运维文档。

## 质量与验证

- 使用任务 01 的机器扫描清单检查旧符号和禁止命令；
- 允许历史文档保留历史名称，但不得提供可执行当前指令；
- 构建产物只有 `openagentx`，`agentbus` 命令和环境变量明确失败；
- Manifest/API 无 pane address，业务 API 无 `worker_instance_id` 目标；
- 全量测试期间监控子进程调用，tmux 控制调用为零。

## 退出条件

- 目标代码只存在 daemon/Worker/Adapter 控制链；
- 目标 schema 在空库创建并校验，旧库保持未修改；
- 当前文档和部署文件没有旧路径操作说明；
- 不存在运行时兼容分支或双写逻辑。
