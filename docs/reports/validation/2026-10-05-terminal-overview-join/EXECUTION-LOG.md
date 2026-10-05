# 实施记录

## 阶段 0：来源与运行态

- 基线 main `eebac37`；已 fetch，origin/main 一致。指定分支及工作树已建立。
- 安装工件时间为 2026-10-03 22:58:28 +0800，build metadata `886ba7fd255a5f6632ee550d4bdcc786274f48fa`、modified=false、SHA-256 `15fd8699c2f07af80966dc3edc355ee90f260e109c9715f402aedd16b90d97ca`。该 revision 与 main 的 `cmd/internal/go.mod/go.sum` 无差异。
- 安装二进制含 `@openagentx_managed` / `@openagentx_agent_id`；现场六业务窗口使用 `@oax-managed` / `@oax-agent-id`。以产品长名为准迁移；现场选项不能证明窗口的历史创建者，不把用户手补的 rename 锁误写成源码已有能力。
- 已保存 [来源核查](evidence/provenance.json)和[运行基线](evidence/baseline.json)：六域 Worker/systemd PID/启动时间、原生 pane PID、thread、正式 Observe API worker instance/generation，以及其他窗口。没有重启或替换业务进程。
- 采集器初次读取部分 `/proc/PID/exe` 被内核权限拒绝，改为明确记录该限制并用 PID/starttime 和服务/API 证据定位；新工作树未加载原 shell 的 Go PATH，显式指定 `/home/sky/tools/go/bin/go` 后采集成功。这两次是采集工具失败，非产品 E2E。

补充的 [完整进程基线](evidence/baseline-runtime.json)遍历 Go 各 OS thread 的子进程及终端进程树，确认六个后台 app-server PID 和原生前端 PID；后续连续性以该快照为准，最初快照保留。

## 阶段 1b：接入短名

- `$oax-join` 为主入口，旧 `$openagentx-join` 为薄兼容入口；唯一 prepare.py 字节未变，两入口共享实现。安装器预检两链接后安装，旧同源长名链接 inode/mtime/target 不变，冲突不覆盖。
- [独立验收](evidence/join-review/review.json)通过：真实 CLI 离线准备、跨别名重放结果及 profile 文件一致。候选工作树发现两技能均 visible/enabled；用户长名仍链接 main，因此候选 check 明确报异源冲突，属于正确拒绝，最终 canonical 安装另验。
- **真实短指令准备 PASS**：[模型日志](evidence/join-model-short/codex.public.jsonl)、[判定](evidence/join-model-short/verdict.json)、[命令](evidence/join-model-short/invocation.json)。实际 `gpt-6-astra/high`，stdin 精确为 `$oax-join`，已确认资料放在隔离 AGENTS.md；模型读取新短 Skill，自动捕获自身 thread `01a109d7-d300-7102-9808-ee29e245c005`，真实已安装 CLI 返回 local_prepared/ready=false。workspace 不变，无 DB/socket，Fleet disabled。
- 原始日志私密保留；入库日志删除无关记忆查询输出，详见 redactions.json；未复制 profile 的环境文件。
- 本阶段尚未安装新用户技能或二进制，没有接管任何业务 Agent。

终端固化、基础 Overview 和安装迁移尚在实施。
