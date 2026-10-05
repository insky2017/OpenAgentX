# 实施记录

## 阶段 0：来源与运行态

- 基线 main `eebac37`；已 fetch，origin/main 一致。指定分支及工作树已建立。
- 安装工件时间为 2026-10-03 22:58:28 +0800，build metadata `886ba7fd255a5f6632ee550d4bdcc786274f48fa`、modified=false、SHA-256 `15fd8699c2f07af80966dc3edc355ee90f260e109c9715f402aedd16b90d97ca`。该 revision 与 main 的 `cmd/internal/go.mod/go.sum` 无差异。
- 安装二进制含 `@openagentx_managed` / `@openagentx_agent_id`；现场六业务窗口使用 `@oax-managed` / `@oax-agent-id`。以产品长名为准迁移；现场选项不能证明窗口的历史创建者，不把用户手补的 rename 锁误写成源码已有能力。
- 已保存 [来源核查](evidence/provenance.json)和[运行基线](evidence/baseline.json)：六域 Worker/systemd PID/启动时间、原生 pane PID、thread、正式 Observe API worker instance/generation，以及其他窗口。没有重启或替换业务进程。
- 采集器初次读取部分 `/proc/PID/exe` 被内核权限拒绝，改为明确记录该限制并用 PID/starttime 和服务/API 证据定位；新工作树未加载原 shell 的 Go PATH，显式指定 `/home/sky/tools/go/bin/go` 后采集成功。这两次是采集工具失败，非产品 E2E。

阶段 1–3 尚在实施，尚未安装新二进制，也未迁移标记或替换 overview。
