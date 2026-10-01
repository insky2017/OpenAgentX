# 04 Console、Fleet 与首次使用体验评估

## 要点

- **控制链有实物，完整用户旅程仍不成立。** 本次真实隔离 daemon、Worker、CLI、tmux、PTY/TUI 跑通了登录、Fleet Agent 选择、配置导入、dispatch、waiting_input、steer、最终回复、退出后下一任务和 graceful stop；Runtime 是 fake，systemd 是 shim，不能据此宣称真实 Agent 已完成业务任务。
- **首次进入 OAX 实际落在空 overview shell。** 动态证据为 `OAX:overview.0 current_command=zsh active_window=1`，另一个 Agent window 已在运行 Console。这是未实现 overview 内容与导航，不是刷新故障。
- **最严重的衔接缺口是“已启动”和“可执行”之间没有面向用户的解释。** 未发布网络绑定时 Fleet 可启动、Worker 在线、任务可创建，但 Console 保持 `queued (mailbox claimed)`；放大 `/status` 才能看到 `local=unavailable`，没有缺少哪一步及恢复入口。
- **现有尺寸测试证明不溢出，没有证明信息可读。** `/status` 在普通约 80×24 终端已裁掉 Focused Task、完整 Task ID、结果等内容，PgDown 无效；80×5 仅剩登录与路径信息。真实 TUI 已复现。
- **日常返回与继续对话不顺。** `/quit` 留下 dead pane，`fleet up` 不恢复它也不提示恢复；终态后 `/steer` 明确被拒绝，而指南正让用户在 final reply 后这样追问。ADR-006 活跃工作树 `dcd8fd6` 尚未安装，不计为当前能力。
- **建议保留正式 API、Worker 独立生命周期及凭据边界，集中补用户入口。** tmux 固定位置、canonical 安装形状和无用 DB override 校验是可简化的客户端耦合；不需要增加新的领域状态机或重写控制面。

## 本次范围、基线与证据级别

- 固定源码：`/tmp/oax-assessment-20260923-source`，`34053c02042ca7448587ff8c20bdd82c11ed870c`。本轮产品代码、ADR、正式服务、正式 tmux 均未修改；没有部署、重启、提交。
- 现场版本对应关系由总评估基线记录：Go `6d599ac`、Web `ee46038`；这里的运行证据来自固定副本的隔离构建，不冒充已安装实例验收。
- 环境：Go `go1.22.4 linux/amd64`，tmux `3.4`；临时 HOME/DB/UDS、唯一 `tmux -L oax9-...`、回环临时 HTTP 端口。测试通过公开 CLI/Control/Console API，不直接修改 DB。
- 复用 `internal/cli/console/workflow_integration_test.go`。新增观察通过 Go overlay 替换测试文件完成，产品源码不变；补充 fixture patch 保存在 `evidence/console-*-fixture.patch`。
- **真实部分**：二进制、daemon、SQLite 事务、CLI Token 登录、Worker 进程、Mailbox/RunAttempt/Journal、tmux server、TTY 输入和 TUI 绘制。**替代部分**：fake Runtime 固定回复、systemctl/loginctl shim。shim 的 start 确实启动 Worker 子进程，但不证明真实 user-systemd 单元、linger 或真实 Runtime 可用。
- 范围内测试：`cli/console`、`cli/fleet`、`fleet`、`consolemodel`、`client/console`、`localprofile`、`credentialstore`。首轮最多 35 分钟；本模块在预算内结束，不扩大修复。

## 设计承诺对照

| 用户承诺 | 本次证据 | 判断与边界 |
| --- | --- | --- |
| 默认 profile 免反复输路径；登录持久化 | 空临时 HOME 的 init、agent apply、PTY Console login、Fleet 后续命令 | 已验证默认路径闭环；安装文件和真实 systemd 部署不在本模块动态证据内 |
| 授权 Agent 明确选择、manifest/config 幂等导入 | PTY Fleet 多选输入 Agent ID；重复显式 init/workspace/up | 已验证；Console 独立 selector 的全部登录/选择组合没有另跑全栈 |
| OAX Agent pane 0 可观察任务 | 真实 TUI dispatch→waiting_input→steer→succeeded，安全输出与终态投影断言 | fake 控制链成立；空 overview、状态内容不可达等 UX 缺口仍在 |
| 常驻 Worker 不依赖 Console | `/quit` 后同 PID、instance、generation 完成第二 Task | 已验证；不是只检查进程活着 |
| CAS、幂等和断线写保护 | 相同 idempotency key 返回同 Task ID、总数仍 2；所属 package CAS/expiry/disconnect 测试 | 同 key 实测通过；丢失响应后人工重输的重复任务风险未实测 |
| graceful stop 可观察 | `fleet down` 的持久 stop intent→offline，随后 status worker inactive | 空闲停止已验证；真实 Runtime 忙时 drain、force-stop 未测 |
| 不干扰额外 pane/未知窗口/冲突 | 真实 tmux integration tests，成功链保留 pane 1/2 | 已验证指定场景；没有对正式 OAX 发送按键或读取 pane 内容 |
| compact pane、overlay、resize 可用 | 真实小窗与放大截图；PgDown 观察 | 布局不溢出成立，但 `/status` 内容不可达，不能称完整可用 |
| 最终回复后可继续用户对话 | 真实终态后 `/steer` 拒绝；指南示例相反 | 当前能力缺口；不把 ADR-006 未安装变更计入结果 |

ADR-009:252 明确把隔离集成限定为 fake/isolated Worker。现有测试遵守了该范围；问题在于把这种控制链证明外推为“真实日常工作台已经完整可用”。

## 用户旅程与认知成本

| 步骤 | 实际操作/用户需要理解的对象 | 结果 |
| --- | --- | --- |
| 1. 安装 | canonical binary、两个 user unit、Web 资源、profile 权限 | 必须先读单独安装章节；不是一个可执行的 onboarding 向导 |
| 2. 建立身份 | `init` 的 Owner；`agent apply` 的 Agent identity、workspace、principal；另一次 Console login | 真实路径能完成，但用户需区分系统身份、Agent 身份、CLI session |
| 3. 加入 Fleet | 已注册 Agent 列表、Worker YAML、manifest、canonical config | 本次多选和导入成功；不会替用户生成真实 Runtime 配置，安全合理但缺少配置向导 |
| 4. 启动 | `fleet init/up/status`，systemd Worker 和 tmux Console 是不同进程 | Worker active/online 不等于 Backend 可执行 |
| 5. 找到入口 | `tmux attach -t OAX` 后看到 overview 的 zsh；自行切 Agent window | 默认首页无说明；从 overview 直接 Attach 也被拒绝 |
| 6. 发起工作 | 必须知道 `/dispatch`，普通文字不是对话输入 | 网络未绑定时任务创建成功但停在 queued/claimed，无下一步 |
| 7. 配齐前置 | 离开 Console，到 Web/API 做网络 test→publish→Worker applied | 成功 fixture 自动完成了这段，普通用户快速开始没有这段 |
| 8. 等结果和追问 | 看 Task/Run/Worker、focused Task、`/steer`、`/status` | waiting_input 可继续；终态后不能直接追问；状态详情可能看不到 |
| 9. 日常返回 | quit 与 stop 不同；dead pane 与 Worker offline 不同 | Worker 确实常驻；返回需另外记住 `fleet workspace --respawn-dead` |
| 10. 停止 | `fleet down` 持久意图与等待；force-stop 两项确认 | 空闲正常停止成立；命令输出仍偏运维 JSON |

这些是不同真实概念，但当前入口让用户在首次完成一个任务前同时理解它们。应将“准备好了吗、下一步是什么、当前任务结果在哪里”放在主界面，generation/lease/cursor/配置形状留作可展开诊断。

## 缺陷与最小处置

### CONS-01：首页 overview 是有名字的占位 shell

- **触发/复现**：全新 profile 完成 Fleet init/up，首次 attach OAX。`console-isolated-journey.txt` 和 `console-no-network-ui-r2.txt` 均记录 overview 为 active zsh，Agent Console 在非 active window。
- **影响**：用户不知道 workspace 已部分成功，容易把无内容当作未刷新/故障。`fleet status` 报 `workspace_status=available` 只代表窗口存在，不能证明 overview 有产品内容。
- **源码**：`internal/fleet/workspace.go:388` 创建 overview 时没有程序；`:445` 对 overview 直接 return。`internal/fleet/binding.go:71` 在 overview 执行 Attach 会要求先切 Agent window。ADR-005 的窗口存在要求、ADR-008 的稳定地址都没有实现总览渲染。
- **最小处置**：先在 overview 提供 Agent 列表、是否就绪、明确导航/恢复命令；或者首次创建后明确选中可用 Agent Console 并说明 overview 只是保留页。不要将“Created overview”写成 overview 功能交付。属于本地入口修复，不需更大工作台架构。

### CONS-02：网络未就绪时任务静默停住，Console/Fleet 不解释恢复路径

- **触发/复现**：新的 Agent/Worker 未做任何 network mode 发布；按快速开始走到 up/status，再 `/dispatch`。实测 3 秒后 Task queued、无 RunAttempt，TUI 是 `queued (mailbox claimed)`；放大 `/status` 仅显示 `Backend health: local=unavailable`。
- **影响**：没有告诉用户“网络策略还未测试发布”，用户可能反复 dispatch 或等待。**实测观察窗口为 3 秒，不据此声称永久卡死**；源码解释了未绑定 Backend 一直不可用的原因。
- **源码**：`internal/persistence/sqlite/worker_execution_repository.go:75` 在缺绑定或未 applied 到当前代际时设 BackendUnavailable；`internal/cli/fleet/command.go:243` 的 up 只 preflight/start；`:811` 的 status 列 Worker/window 信息。`internal/cli/console/tui.go:1640` 状态面板没有可恢复原因或网络入口。
- **衔接证据**：`docs/operations/openagentx-user-install-guide.md:71` 的快速开始无网络配置步骤；成功 fixture 在 `internal/cli/console/workflow_integration_test.go:193` 额外调用 bootstrap，`:313` 起通过 Web API test/publish/apply。`/help` 可用命令（`tui.go:1568`）没有该恢复路径。
- **最小处置**：显式呈现“Worker 在线、执行尚未就绪：缺少当前 Backend 的网络绑定”，给出正确 Web 配置入口及 ready 判据；首次 dispatch 前可确认排队，不能伪装已执行。保持网络发布和当前代际回执约束，避免用默认放行掩盖问题。与 Web/网络模块同一首用缺口，综合报告不要重复累计根因。

### CONS-03：状态/帮助/诊断面板裁掉内容后不能滚动

- **触发/复现**：有 focused Task 时打开 `/status`。真实 80×5 只到 Agent；zoom 到普通约 80×24 只到 Mode，看不到 Focused Task 以下；PgDown 前后画面一致。证据 `console-no-network-ui-r2.txt`。
- **影响**：指南承诺 `/status` 可看完整 Task ID/version、Mailbox、RunAttempt、outcome/reply，但常见终端无法访问它们。小窗主时间线虽可滚动，不能解决 overlay 自身内容不可达。
- **源码**：`internal/cli/console/tui.go:909` 的非 tasks overlay 只处理关闭/确认；`:1631` 限长、MaxHeight 和 fitTerminalView 裁切；`:1688` 后才追加 Focused Task。现有 `tui_test.go:305`、`:375` 主要断言尺寸和 reducer，不断言被裁信息可到达。
- **最小处置**：让既有 overlay 使用可滚动 viewport，并显示滚动位置；默认先显示当前任务结果/阻断原因，运维字段折叠。用真实 80×24 和分屏证明确实能到达末尾。无需重写 reducer。
- **复验说明**：第一版观察脚本错误地要求小窗中出现 Backend health，退出 1；保留 `console-no-network-ui.txt`。随后仅改为观察标题、截图并 zoom/PgDown，退出 0，确认是内容被裁而非 overlay 根本没有打开。

### CONS-04：退出后 `fleet up` 成功，却没有恢复 Console 或给恢复提示

- **触发/复现**：正常 `/quit`；`fleet workspace` 回 `Dead:[quote-service]`；再 `fleet up` 只输出 `started user unit ...`，pane 0 仍 dead=1。显式 `fleet workspace --respawn-dead` 才恢复，pane 1/2 不受影响。
- **影响**：用户把 up 当日常打开入口会再次落在不可输入页面。它没有误报 Worker 状态，但把“后台在线”当作唯一成功信号。
- **源码**：`internal/fleet/workspace.go:319` 未设置 RespawnDead 时只登记 Dead；`internal/cli/fleet/command.go:252` 丢弃 Reconcile report；`:267` 仅输出 started。指南 `openagentx-user-install-guide.md:524` 已记录恢复命令，所以这是产品引导缺口，不能说完全无文档。
- **最小处置**：保留不替换 live/unknown pane 的约束，把 dead Console 及明确恢复动作直接显示在 up/退出页；可提供明确的打开/返回操作。正常退出和进程故障不必由用户用同一运维修复流程辨别。

### CONS-05：指南把终态后的追问写成可用，而当前 Console 拒绝

- **触发/复现**：首个 Task 已 succeeded 后输入 `/steer follow-up after terminal`，真实 TUI 显示 `Command rejected: focused Task is terminal or has no usable version`，输入保留但无下一步。证据 `console-terminal-followup.txt`。
- **影响**：用户已拿到回复后自然追问失败；现有 fake E2E 使用 waiting_input→steer→succeeded，避开了“普通完成后继续对话”这个常见路径。
- **源码/契约**：`internal/cli/console/tui.go:1315` 拒绝 terminal；`tui_test.go:516` 明确测试拒绝；安装指南 `:485-493` 却按 terminal/final reply 后 steer 排列。ADR-006 `dcd8fd6` 正在旁支实现 explicit intent、尚未安装；本报告不评价它为已解决。
- **最小处置**：先纠正指南，区分“运行中/等待输入的 steer”和“完成后新问题”；界面给新任务/继续对话的明确可用入口。是否延续上下文按 ADR-006 的正式契约处理，不能偷偷重开 terminal Task。

### CONS-06：Fleet 将界面布局、安装形状及未使用的 DB 配置耦合到所有命令

- **现实触发**：把 Console 用作独立终端客户端、在辅助 pane 查看 Agent、使用非标准安装目录或同机多个 profile，都会遇到固定 OAX/pane 0 或 canonical binary/unit 形状约束。边界测试已覆盖拒绝；这些拒绝遵守当前 ADR，但仍是产品设计成本。
- **另一个已复现的小例子**：`OPENAGENTX_DATABASE_PATH=relative-unused-db` 时 `fleet status` 在认证前退出 2；Fleet 根本不读 DB，仅解析并做路径冲突校验。证据 `console-unused-db-override.txt`。
- **源码**：`internal/cli/console/application.go:188` 先做 workspace preflight；`internal/fleet/workspace.go:179` 强制 session/pane；`internal/cli/fleet/systemd.go:17` 精确 argv/WorkingDirectory/EnvironmentFiles；`:60` 固定 `~/.local/bin/openagentx`。`internal/cli/fleet/command.go:350` 对所有子命令解析包括未使用 database 的全部 profile 路径。
- **最小处置**：逐步把“打开有权限的 Agent Console”和“绑定/修复 tmux workspace”分开；API 身份仍权威。按子命令解析真正使用的资源，移除无意义 DB 前置条件。默认安装约定可以保留，非标准宿主支持要明确支持范围，不必扩展成通用部署框架。

## 实现较好且应保留的部分

- 默认本地路径使日常命令足够短；Fleet 使用经鉴权的 Agent 列表，不扫描目录猜身份。真实 PTY 多选和重复 init 已验证。
- Fleet 配置原子写、显式冲突、不接管 unknown window、保留辅助 pane，避免用户工作被覆盖；当前安全边界确实有现实价值。
- installation-bound token、私有 credential file、替换登录锁、撤销/到期清理防止换 socket/换 installation 后把旧凭据发错地方。应隐藏其复杂性，不应删掉保护。
- Console 的正式 Task projection、focused CAS、单次控制请求、流 ack/reducer 游标、Normal/Diagnostic 分离都有实现和测试；需要修入口与可读性，不是重造这些机制。
- Worker 与 Console 解耦不是口号：本次退出 Console 后，同一 Worker 完成下一项任务；同 key 重复 dispatch 不增加 Task；graceful stop 等到 offline。

## 对“过度设计”的判断与重构顺序

| 机制 | 判断 | 建议 |
| --- | --- | --- |
| 服务端鉴权、资源归属、CAS、幂等、当前 Worker fencing/网络回执 | 必须保留，解决真实误执行和越权问题 | 不用 UX 简化作理由移除 |
| Console 连接/Task reducer、安全输出边界 | 有价值，但用户不该先理解其字段 | 保留内部机制，把结果、阻断和下一步置顶 |
| OAX/pane 0/window marker 强制限制所有 Attach | 主要是 workspace 产品政策，不是 API 身份安全前提 | 先改善导航；后续可独立于 tmux 运行授权 Console |
| 精确 canonical argv/home/env 校验 | 防止 Fleet 验证 A 却启动 B 有价值；精确字符串形状限制部署选择 | 默认模板保持；更广支持有需求再做，不先造部署框架 |
| 所有 Fleet 命令解析未使用 DB 路径 | 无用户收益的耦合 | 局部移除 |
| 继续增加状态机、统一万能 Console/Fleet 管理层 | 不解决本次已观察入口问题 | 不建议 |

优先小批结果是：①首次进入有内容并准确告诉用户是否 ready；②当前任务及最终回复能看到，/status 可滚动；③终态后的下一步和退出后的返回有明确入口。完成后，用真实 Runtime 执行一个可核验低副作用任务，再回来问一个后续问题；只用 fake 的固定字符串不能覆盖这一验收。

取消问题由控制面/Web 评估归并到 `CORE-01`；本模块没有另跑取消 E2E，也不把共享根因另计一个 Console 缺陷。

## 测试记录

时间均为 Asia/Shanghai；具体命令、stdout、退出码、时长在同名 evidence 文件。`go test` 通过代表相应断言成立，不是产品没有上述缺陷。

| 开始时间 | 命令/用途 | 退出码与耗时 | 文件 |
| --- | --- | --- | --- |
| 00:30:08 | `go test -count=1 -v ./internal/cli/console ./internal/cli/fleet ./internal/fleet ./internal/consolemodel ./internal/client/console ./internal/localprofile ./internal/credentialstore` | 0；32.328s | `console-package-tests.txt` |
| 00:32:15 | overlay `TestAssessmentConsoleJourney`，无网络首次任务 API 观察 | 0；30.257s | `console-onboarding-no-network.txt` |
| 00:33:25 | overlay 同名测试：PTY Agent 多选、完整控制链、重复任务、返回、停止 | 0；33.526s | `console-isolated-journey.txt` |
| 00:34:24 | 无网络真实 TUI；第一版要求小窗 Backend health | 1；36.007s，观察脚本断言与裁切行为不符 | `console-no-network-ui.txt` |
| 00:35:53 | 仅修正观察方式：小窗/zoom/PgDown | 0；31.685s | `console-no-network-ui-r2.txt` |
| 见 evidence | 终态后真实 TUI `/steer` | 0；31.609s，断言拒绝行为 | `console-terminal-followup.txt` |
| 见 evidence | 独立 profile 的未使用 DB override 阻断 | 2，预期复现 | `console-unused-db-override.txt` |

Overlay 完整命令形如 `go test -overlay=/tmp/oax-console-assessment/overlay.json -count=1 -v ./internal/cli/console -run '^TestAssessmentConsoleJourney$' -timeout 120s`。对应 patch 仅作用于临时测试文件；可在固定源码副本应用 patch 后用同一 `-run` 复现，不要向产品分支应用。

## 未覆盖与剩余风险

- 未动态验证真实模型的工具调用、工作区文件副作用、真实 user-systemd/linger、SSH 断开后宿主行为；本报告不宣布产品真实 Runtime E2E 通过。
- 未在本模块另跑 busy Runtime drain/force-stop、取消、审批完整 TUI E2E、断网后重复 dispatch、CLI token 30 天真实到期、完整 Console Agent selector 全栈组合。相关单测存在，不升级为本轮端到端证据。
- no-network 观察为短窗口和源码相互印证；未做长时任务资源/CPU 观察。既有测试成功路径额外预配网络，不能证明安装指南的原始路径自洽。
- runtime 本身的业务成功/恢复缺陷归 Worker/Runtime 模块；取消共享根因归 CORE-01；ADR-006 未安装改动不纳入当前可用范围。
- 现有失败记录未删除；没有修复 commit，因为本轮只评估。清理和源码/ADR 未变证据见 `console-cleanup-and-provenance.txt`。

## 下一步

1. 主代理按共同根因合并网络就绪、终态继续和取消问题；保留本模块独立的空 overview、overlay 不可达、dead pane 恢复发现。
2. 下一实施批只修用户能完成的最短旅程，并以首次用户真实终端和真实 Runtime 副作用作为验收，不再把窗口/单元测试数量当完成度。
