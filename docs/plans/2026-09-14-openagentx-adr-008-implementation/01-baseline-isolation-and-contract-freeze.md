---
doc_type: implementation_task
status: pending
owner: openagentx
updated_at: 2026-09-14
---

# 任务 01：基线隔离、契约冻结与测试地图

## 目标

在不修改产品行为的前提下收口目标主机既有修改，创建独立实现 worktree，冻结 ADR-008 的接口、状态与测试边界，使后续阶段不靠临时兼容或边做边猜。

## 依赖与入口

- ADR-008 和主实施计划；
- `rtx4090` 主工作树中四项受保护修改；
- 当前 Console、Fleet、Auth、SQLite、tmux runner 和 systemd 实现。

## 实施步骤

1. 在主工作树记录 HEAD、branch、remote、完整 `git status --short` 和四项既有 diff；不得修改 `steadyflow` 父仓库。
2. 由原执行 Codex判断既有修改是否完整：完整则运行其原定验证，作为独立前置提交推送；不完整或不确定则停止请示。禁止 stash/reset/clean。
3. 从更新后的 `origin/main` 创建独立 `codex/adr008-implementation` branch 和 sibling worktree；记录路径、基线 SHA 和 Go/Node/tmux/systemd 版本。
4. 建立当前命令/API/schema/事件/tmux 行为的 characterization 清单，明确后续替换点和临时兼容的删除任务。
5. 冻结并在 execution log 记录：
   - path resolver 的输入优先级和全部默认值；
   - Console 子命令/非交互错误契约；
   - Attach snapshot/cursor/reconnect DTO；
   - CLI Token endpoint、scope、过期和撤销错误语义；
   - tmux marker、冲突分类和绑定状态机；
   - TUI framework 选择及兼容的固定版本。
6. TUI 必须采用成熟、可测试、支持 viewport/input/overlay 的 Go terminal framework；验证与仓库 Go 版本兼容，记录新增依赖、license 和选择理由。不得以 `bufio.Scanner`、连续 `fmt.Print` 或自制原始 ANSI 循环冒充全屏 TUI。
7. 为任务 02—08 建立现有测试文件到目标验收项的映射；只增加计划/记录或 characterization 测试，不改变产品路径。

## 必须回答的设计问题

- 一致 snapshot 在哪个 repository/service transaction 边界取得 high-water sequence？
- CLI bearer 认证如何只在预期 UDS 路由启用而不改变 Web cookie+CSRF 安全边界？
- 现行 schema v1 兼容升级如何确保既有 DB 得到 `cli_tokens`，而非只修改空库 schema？
- pane `0` 存在性、当前 pane 和 window 标记如何通过 tmux 的结构化查询证明？
- 哪些 TUI 状态由纯 reducer 管理，哪些 I/O 通过 command/message 注入以便确定性测试？

## 验证

```bash
git status --short --branch
go test ./internal/cli/console ./internal/client/console ./internal/api/console ./internal/fleet ./internal/cli/fleet
go test ./internal/auth/... ./internal/persistence/sqlite/...
git diff --check
```

若 baseline 中某个 package 原本失败，必须记录可复现证据并停止；不得把未知红灯带入任务 02。

## 退出条件

- 受保护修改已独立收口或明确阻塞；
- 独立 worktree 干净且基于最新远端基线；
- 六类接口契约、TUI 依赖决策和测试地图已记录；
- baseline 定向测试通过；
- 未修改产品运行行为；
- 创建本任务提交，更新 execution log 后暂停等待复核。
