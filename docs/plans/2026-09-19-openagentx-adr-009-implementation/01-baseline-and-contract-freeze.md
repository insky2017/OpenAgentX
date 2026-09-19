---
doc_type: implementation_task
status: pending
owner: openagentx
updated_at: 2026-09-19
---

# 任务 01：基线、能力盘点与契约冻结

## 目标

在不改变产品行为的前提下，冻结 ADR-009 的 Task projection、状态转换、focus/CAS、cursor、输出上限、
Diagnostic 切换和测试契约，并确认实际 Runtime/Adapter 能提供哪些持久化证据。

## 依赖与范围

- P0 已关闭，ADR-009 已 Accepted；
- 在独立 `codex/adr009-task-console` worktree 执行；
- 只允许文档、characterization tests 和无行为变化的 test helper；
- 不新增 API/schema、Task reducer/TUI 行为或 Diagnostic 切换。

## 验收矩阵

| 类别 | 必须冻结的证据 |
|---|---|
| 正向 | dispatch 到 Task/Run/TurnResult 的现有持久化链路；AGY/CodeBuddy 的 stdout、stderr、退出码、流式能力和终态证据；默认 profile 用户入口 |
| 状态/CAS/幂等 | Task/Run 状态图、focused Task 选择规则、Task version 来源、重复 event/response 规则、写操作只调用一次 |
| 失败 | 空/畸形输出、无 TurnResult、Task terminal 但无安全结果、`uncertain`、token 过期、Diagnostic forbidden、retention gap |
| 竞态 | dispatch response 与 `task.created` 先后、snapshot N/event N+1、Task version 更新与 steer/cancel、mode switch 旧流迟到 |
| 资源/平台 | Timeline/result/diagnostic/page byte 和 count cap；`80x5`/极小 pane；临时 HOME/DB/UDS 与唯一 `tmux -L` |
| 证据格式 | 契约表逐项链接到代码入口、characterization test、预期 API error/status；明确“已验证/未验证/不支持” |

## 实施步骤

1. 记录 ADR-009、ADR-003/005/008 与 Proposed ADR-006/007 的 hash，建立范围排除表。
2. 盘点 domain、SQLite repository、Console/Panel API、client Follow、reducer、TUI 和 safeoutput 当前契约。
3. 以实际 `--help`、wrapper fixture 和 Adapter 测试记录 AGY/CodeBuddy 对 argv、stdin/stdout/stderr、
   timeout、session、流式和终态的承诺；不把未实测能力写入产品契约。
4. 冻结 `ConsoleTaskSnapshot`、Task option、Journal task/run/output projection 的字段和归属校验。
5. 冻结 Task focus、完整 ID/version、显式与快捷控制语法、CAS conflict 行为。
6. 冻结 Normal/Diagnostic mode switch 状态机、旧 Follow cancel/ack 和新 snapshot/cursor 顺序。
7. 冻结安全输出、分页、Timeline 和 compact layout 上限，并建立后续 Task 的测试地图。

## 验证

```bash
go test ./internal/domain ./internal/api/... ./internal/client/console ./internal/consolemodel ./internal/cli/console -count=1
go test ./internal/worker/... -count=1
bash scripts/check-legacy-control-paths.sh --release
git diff --check
```

若 package 布局或 Adapter fixture 的精确命令不同，必须在 execution log 记录等价命令和原因。

## 退出条件

- 契约和测试地图足以让 Task 02—07 无需自行创造跨层语义；
- 未修改产品行为；
- 冻结 ADR hash、命令、退出码、耗时、未覆盖项和外部状态已记录；
- 创建一个 Task 01 docs/characterization 提交后停止等待监督 gate。
