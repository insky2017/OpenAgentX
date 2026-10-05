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

## 阶段 1a：终端身份固化

- Native 在精确 TMUX_PANE 下复用 PreflightAttach/BindCurrent；仅 OAX pane 0 纳管。Console 对 Reconcile 目标操作，再定位 `.0`，不改调用窗口。canonical marker、窗口名与 rename 锁统一；同身份补齐差异选项，保留额外 pane。
- 共享绑定逻辑保存/恢复显式选项及 unset 状态。定位不明、身份冲突及身份配置失败拒绝原地打开；仅边框标签失败告警。新建/dead pane 的既有生命周期与活 pane repair 分离。
- [实现测试记录](evidence/terminal-impl/implementation-evidence.json)及 [最终三包回归](evidence/terminal-impl/final-package-tests.log)通过；覆盖真实隔离 tmux、Console TTY、准确目标与进程保留。native 回调 seam 仅证明启动关口，不能冒充真实模型验收。
- [独立预审](../2026-10-05-terminal-overview-join/terminal-review/REVIEW.md)无阻断；独立真实附着 PTY 验证 attach/switch 都定位 pane 0，源码 SHA-256 已与最终实现核对一致。
- 首次失败保留：umask 077 改变原权限 fixture；新增顶部边框导致旧 TTY 测试的历史提示裁剪，改为核验当前 mode normal header 并保留 SSE 模式断言。复验通过，未删失败记录。
- 尚未安装、迁移现场标记或替换 overview；真实 Codex 及完整 CLI API→终端链在联合验收另行记录。

## 阶段 2：基础 Overview 候选

- 新增 `openagentx overview`，Fleet 新建总览默认调用此入口。复用既有 Console 授权 GET API，展示 Agent、当前已知任务、就绪/阻塞及选中详情；普通程序刷新不调用模型。
- 详情可滚动，Enter 只跳既存且身份唯一的受管 pane 0。原生入口额外比对 Worker profile、原 thread 和真实前台 Codex；不创建或修复业务窗口。断线保留旧内容并禁导航，重连恢复。
- [独立审查](evidence/overview-review/review.json)通过；发现 native 仅凭外层 argv 提前返回的缺口，已限制该路径只适用于 Console，native 必须核验 thread。源文件 SHA 与独立审查一致。
- [实现验证](evidence/overview-impl/integration-second.log)和独立 race 集成通过，覆盖 103 Agent 分页、最大四并发、无写 API、断线/恢复、80×24 等布局约束。现场六域只读进程/profile/thread 核查通过；旧 daemon 所需 API 已静态核对。`go vet` 及 CLI Fleet 接线回归通过（11.249s）。
- 此处集成与只读核查不代替实际 PTY/客户端跳转，完整联合验收和安装另行记录。首次 fixture token 格式失败与审查 Go PATH 失败保留。

基础 Overview 真实联合验收和安装迁移继续实施。
