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

## 联合真实验收

- 固定工件：Native/Console `562d463`，SHA `1affad66666a168d2961001531046bdfcf9cd43b839c5becfd51755b26de82b1`；Overview `eb043ca`，SHA `9ff957ac0a6c043b9cd8e8a41ba6270e72e1d8dafe032bfac7f1b6950748144e`。后者未改变前者 Native/Console/Runtime 实现，真实模型证据经影响核对复用。
- [隔离证据索引](evidence/runtime-e2e/INDEX.json)：真实 query Task `task-e87262e4-ae54-4527-b101-ded962cd9e2a`，Run `run-db21804e-ed45-4f2a-b97f-ad0f50fab6aa`，thread `01a109e3-d9b8-77f0-b64b-43b1ef7db7fa`。真实 thread turn_context 为 `gpt-6-astra/ultra`；Run 表内 reasoning_mode=default，分别记录，不用配置推断实跑强度。
- 初始化答复、Task/Run/Journal、终端锁、退出/重开同 thread、额外 pane 保留均通过；Console 从 overview 和无关来源窗口跳到准确目标，来源不变。Overview 在真实 160×45 和 80×24 显示排队任务、详情与选中状态，准确跳 native pane；仅隔离 daemon 有界 SIGSTOP/CONT 验传输中断/恢复。
- 长详情补验实际看到 LINE-01…07 → LINE-32…40/END → START/LINE-01…07 三屏；两项无 Worker 排队测试 Task 最后均取消、0 Run。总计只有一项模型 Task/Run。隔离进程按 PID/启动时间核对后清理，保留所有证据。
- 首败如实保留：run-01 CookieJar 未发送本地 Secure Cookie；run-02 取 Observe 列表误用 task_id 而实际为 id；Console 当前窗口观察参数错误；滚动补验先把列表固定摘要当作详情，再遇续验焦点状态不符。均修正夹具后沿用既有真实 Task，不重复调用模型。没有证据将这些误判为产品失败或隐去记录。
- 本机旧 daemon 的实际读取通过。初次读取脚本误搜不存在的“已连接/已同步”，还要求18个登记 Agent 中六域全在首屏；保留初始 verdict，依据真实 PTY 的“在线/上次同步/18个Agent”做有界判定修正，见 [read-compatibility.json](evidence/overview-live-read/read-compatibility.json)。这只证明读取兼容，不冒充现场六域跳转验收。

## 安装、现场迁移与独立核查

- 2026-10-05 10:53（北京时间）功能提交快进合入 canonical main。用户级短名安装、长名原链接保留，真实 `skills/list` 两个同源入口 visible/enabled，见 [Skill 安装记录](evidence/skill-installed/)。
- 10:54 原子安装经检验的 `eb043ca` CLI；旧工件备份于原始证据根 `installed-before-886ba7f`，未重启任何后台服务/业务终端。此前 linked worktree Go 自动 VCS 来源识别错误已弃用，最终使用真实 `.git` 目录独立 clone 的固定干净源码构建。
- 六域长名 marker 写入读回后清除临时短名；全程只改 window options，原准确边框文字保留。迁移前后六域服务、backend/native进程、thread、generation、配置和额外 pane 均一致。
- 10:56 最后核对 `@2/%2` 仍为原 PID 30743、无子进程且 foreground 为自身的空闲 zsh，再将**仅此** overview 替换为正式 CLI 总览。window/pane ID 保留，新 PID 2304458，连接在线、18 Agent。用户 client 选择未改变；见 [切换命令与快照](evidence/overview-installed/)。
- [最终独立核查](evidence/final-review/review.json)44 项通过，含再次采集与基线比较、真实 Skill 发现、唯一窗口身份及无短名残留。部分 `/proc/exe` 不可读明确保留，未声称运行 Overview 的直接二进制哈希已取得。
- 更新两份 HTML 和使用说明；真实 Chrome 原文渲染、DOM 交互与宽窄布局通过。工具无法 file/HTTP 导航，等价改为自有 about:blank 加载完整单文件 HTML，源 SHA、截图及限制见 [浏览器记录](evidence/browser/README.md)。

本批验收结果和边界以 [DELIVERY](DELIVERY.md) 为准；最终源码/文档推送与工件安装分开记录。
