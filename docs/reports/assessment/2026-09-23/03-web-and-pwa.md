# 03 Web、PWA 与实际用户体验评估

## 结论要点

- **核心操作不是完全不可用，但首次就绪、取消、状态解释和故障恢复仍有断点。** 本次真实 Chrome 先以 fake Adapter 覆盖等待输入/回复与失败场景，再经正式 AGY wrapper 完成两项真实任务（写文件与只读核对）、同 Worker 连续执行和页面结果核对。两项真实 Run succeeded，平台 Task 仍 uncertain/business_effect_unverified，不能宣称平台任务成功。
- **最优先的问题直接落在日常主链。** Worker 在线却未具备执行条件时仍可发送并长期排队；排队任务取消返回 HTTP 400；多轮任务结果卡选中最旧 Run；首次 overview 短暂失败后不建立 SSE、无法发送。
- **用户的“过度设计但核心薄弱”判断有具体依据。** 网络页要求理解 Worker、Backend、generation、测试/发布/应用；任务页同时展示 Task 与 Runtime 终态、Run ID、副作用来源和业务核验，却没有把“我现在应做什么”稳定放在首位。上述机制有安全价值，问题在于日常用户承担了内部协调成本。
- **已有保护值得保留。** 实际观察到准确新 Task 跟踪、CSRF/幂等请求结构、离线禁写且不重放、SSE 游标恢复、安全 Markdown、移动布局无横向溢出、网络应用回执与提交提示分开。
- **不建议重写前端或删掉安全状态机。** 先用一个权威的任务/Agent 就绪投影消除重复推断，再局部拆分连接恢复、任务详情、输入区和高级诊断；以真实 Runtime 的两任务连续浏览器流程作为回归门槛。

## 范围、基线与证据等级

本模块于 2026-09-23 00:27–00:40 进行首次评估，随后按主代理新增授权，在 00:41–00:46 内完成有界联合真实 AGY E2E 与清理；联合验证自身最多 20 分钟，实际浏览器主链 43.6 秒。未修产品、未改 ADR、未部署或重启正式服务、未提交 Git。正式站点仅无登录只读访问登录页。隔离 daemon、DB、Worker 经正式 CLI 初始化，状态变更经 Web/Control/Worker API；没有直接写 SQLite。

| 对象 | 本次实际输入 |
|---|---|
| 固定源 | `/tmp/oax-assessment-20260923-source`，`34053c0`；Web 与安装的 `ee46038` 相同 |
| 隔离 Go binary | 安装路径只读复用，revision `6d599aca8ce7c32fe11f23244478b495a0a78e68`，`vcs.modified=false` |
| Go SHA-256 | `a26ebf4d84ede2fa60bd4c10aaee704056732dde9dc532cd424d85de76703e89` |
| 独立 Web 构建 | `/tmp/oax-web-assessment/web/dist`；借用已有 node_modules，只写临时目录 |
| app.js SHA-256 | `2e173b5064a89c61ca6a5a57f3e9ea1cc3fe8479b7695c81735edeb2053b1e7c` |
| app.css SHA-256 | `c7e9b0f8d223ff14ab76c2280ded37613bd4c06918f002c1627e9133a89b548f` |
| 浏览器 | `/usr/bin/google-chrome`，Chrome `143.0.7499.109`，Playwright Core，真实 Chromium 内核，无页面数据 mock |
| 视口 | 桌面 `1440×1000`；移动模拟 `390×844`、touch enabled（不冒充实体手机） |
| 临时入口 | `http://127.0.0.1:19173`；loopback 安全上下文可注册 SW |
| Runtime | 首轮 `web-eval-a`/`web-eval-b` 为 fake；联合验证 `web-real-agy` 使用 `agy-batch`、正式 `/home/sky/.local/bin/agy-graft`、`gemini-3.7-flash-low`，独立 workspace，三者 generation 1 |
| 故障注入 | 仅浏览器截断一次 overview 为 503、一次 login 为 503；其余 API 正式实现、隔离 DB |

凭据仅保存在 `/tmp/oax-web-assessment` 的受控文件，不归档、不输出。以下报告及截图仅包含隔离 Task/Worker 标识与显式测试内容。浏览器进程均关闭；联合验证后 Runtime 代理已关闭专用 AGY Worker，本代理已依次关闭两个 fake Worker 与隔离 daemon，精确 PID 的退出核对见 `evidence/web-cleanup.json`。临时 DB/workspace 保留用于审计，不再提供监听服务。

## 设计承诺与实测矩阵

| 用户结果/承诺 | 本次证据 | 判断与边界 |
|---|---|---|
| 登录后选择正确 Agent 并发送 | 桌面明确选 A；移动明确选 A/B；正式 create receipt 自动打开新 Task | 通过；凭据过期/角色矩阵未全面覆盖 |
| 首次发送即知道是否可执行 | A 未绑定网络时 UI online/可发送，Task queued；45 秒后完成网络配置才启动 | 不满足；WEB-01 |
| queued→运行→过程→结果 | fake 状态链通过；联合真实 AGY 两任务均截图捕获 running，页面持久 Journal 含 init/step_update/result/finished/settled，结果正文吻合 | 真实浏览器执行/回复通过；Task 仍 uncertain，不宣称平台成功终态 |
| 等待输入→回复→结果→下一项 | A 第一项 waiting_input，按钮回复进入下一轮；第一项 succeeded；同 Worker 第二项 succeeded | 操作链通过，进度/结果元信息有 WEB-03/04 |
| 取消未执行任务 | B 无网络绑定的 queued Task，点击取消 HTTP 400，刷新仍 queued | 不通过；WEB-02 |
| Task 详情内审批 | 详情 JSX 无审批决定控件，仅首页前三项有批准/拒绝 | 承诺未完整兑现；源码确认 WEB-06，未制造真实 Approval |
| 任务详情 URL/重载 | `?view=tasks&task=...` 重载后保持准确选中 Task | 通过；浏览器 Back/Forward 没有完整验收 |
| SSE 断线/恢复 | 移动 offline→online，重建 stream 的 `after_sequence=85`，状态在线 | 通过该场景；初始 overview 故障 WEB-05；锁屏/长断线/保留窗口跨越未覆盖 |
| 离线写保护、恢复不重放 | 输入 `OFFLINE-MUST-NOT-SUBMIT` 后断网，发送 disabled；恢复无额外 POST、草稿保留 | 通过；见 `web-mobile-run.txt` |
| 移动查看/操作 | touch 发送、详情、返回任务列表、取消；390px 无横向溢出 | 通过已测布局；实体软键盘/系统导航/PWA 外壳未测 |
| 内容安全 | script 文本不执行；javascript 链接没有危险 href；远程图隐藏；安全外链 noopener/noreferrer | 通过注入内容；不是完整渗透测试 |
| PWA | SW 注册 1、shell cache 存在、API cache 0；standalone=false | 仅浏览器能力通过；未安装 OS PWA |
| 真实副作用与连续任务 | A 创建 artifact.txt，B 只读，独立核文件精确 21 字节，同 Worker/gen1 两次 Run succeeded | 通过本次限定文件效果；平台没有接收外部核验，Task 两次 uncertain |
| 正式入口 | `https://agentx.oneaxe.cn/` Chrome 打开登录页正常 | 仅只读入口可达，不证明正式登录/执行 |

## 可复现缺陷

### WEB-01：在线与可执行混淆，首次任务滞留时没有恢复引导（高）

- 触发：新初始化 Agent/Worker online、lease 有效，尚未建立已应用的网络 binding。
- 复现：登录→任务→选择 `web-eval-a`→发送 `WEB-EVAL-01`。按钮可点，提示“Worker 在线 · generation 1”；Task 显示“指令已入队，等待 Worker 领取”。另到 Runtime 页才看到 `Backend: 离线`、`实际生效: 未绑定`。
- 期望：发送前明确“连接正常，但执行环境未就绪：网络未配置”，可直接进入最短恢复步骤；排队允许与否由产品明确，已排队时仍说明真正阻塞原因。
- 实际/后果：用户无法从任务页区分忙、离线、网络绑定缺失；反复发送会积压更多任务。完成“测试连接→保存并应用→Worker 回执”后，同一 Task 才开始执行，证明不是无关页面问题。
- 证据：`evidence/web-02-first-send.{txt,png}`、`web-01-initial-network.png`、`web-05-network-success.txt`、`web-06-waiting-input.txt`、`web-18-network-applied.png`。
- 源码：`web/src/task-dispatch-state.js:10` 只检查 online/lease；`web/src/main.jsx:405`/`:408` 据此允许发送；`web/src/task-dispatch-state.js:53` 对 queued 只提供领取等待文案。
- 最小修复：Control/Observe 提供与实际执行一致的 readiness/blocked reason，Web 显示简短原因和定向恢复入口；不要单独在组件重写一套 Backend/network eligibility。与 Console/网络模块合并根因处理。

### WEB-02：queued 任务无法取消（高，共用后端缺陷）

- 触发：Task queued，尚无 active Run；隔离 B 未配置网络，提供稳定复现。
- 复现：发送 `WEB-EVAL-04`→点击详情“取消任务”。再次刷新后仍 queued。
- 期望：尚未执行的任务可撤回，不再被未来恢复的 Worker 领取。
- 实际：HTTP 400，正文 `find active run for cancel: resource not found`；前端原样显示，按钮保留。
- 后果：已误发/重复排队的任务无法用日常页面撤回，网络恢复后可能执行用户已不需要的工作。
- 证据：`evidence/web-15-mobile-canceled.{txt,png}`、`web-final-probe.txt`。
- 源码：前端 `web/src/main.jsx:1049` 为非终态提供取消；后端 `internal/persistence/sqlite/control_input_repository.go:184` 查询 active Run，`:212` 用 `sql.ErrNoRows` 排除而 scan 层返回领域 not-found，导致事务失败。
- 最小修复：按领域 not-found 处理无 active Run，原事务提交 queued→canceled，并验证 Work Mailbox 不可继续执行；不能仅隐藏按钮。核心代理负责进一步事务结论。

### WEB-03：多轮任务结果卡取到最旧 Run（高）

- 触发：同一 Task 第一轮 waiting_input，用户回复后第二轮 succeeded。
- 复现：WEB-EVAL-01 回复后 Task badge 为 succeeded，但“执行结果”的 Run ID 仍为第一轮，标记 `Runtime waiting_input`；刷新后不变。
- 期望：结果卡与最新 Run 一致，当前回复/状态来源可以核对。
- 实际：API `run_attempts` 按 started_at DESC 返回 `[第二轮 succeeded, 第一轮 waiting_input]`；前端取最后一个，选中第一轮。测试两轮 body 相同，因此本次确证的是 Run/状态来源错配，不额外声称结果正文错误。
- 后果：用户看到互相矛盾的“完成/仍等待”；当 Task result 不存在、回退到 Run body 时，还可能展示旧回复。
- 证据：`evidence/web-07-replied.{txt,png}`、`web-fault-run.txt` 中 `multiTurnAPI` 与 `renderedRunMeta`。
- 源码：`web/src/main.jsx:416`–`:418`；`internal/persistence/sqlite/execution_repository.go:233` 倒序 SQL。
- 最小修复：明确并集中约定 latest Run，直接使用权威 latest 字段或按时间/序号选最新；补“等待补充→下一轮不同回复”的真实浏览器回归。

### WEB-04：waiting_input 的主提示却说执行完成（中）

- 触发：RunAttempt 执行过程成功结束，但其 runtime_status 为 waiting_input、Task 等待输入。
- 复现：WEB-EVAL-01 第一轮，详情 badge `waiting_input`、按钮“回复”，主提示却为“Runtime 已执行完成，回复见下方”。
- 期望：主提示为“需要你补充信息”，把回复作为唯一主要下一步。
- 后果：日常用户会把本轮暂停误解为整个任务已经结束；“Run 技术上 succeeded”泄漏成产品语义。
- 证据：`evidence/web-06-waiting-input.{txt,png}`。
- 源码：`web/src/task-dispatch-state.js:44` 成功 Run 分支优先于 `:50` waiting_input 分支。
- 最小修复：先按 Task 用户状态判定等待输入/审批，再解释 Run 事实；保留高级诊断的 Run succeeded。

### WEB-05：首次 overview 一次瞬时失败，主链无法自动恢复（高）

- 触发：已认证页面初始 `/api/observe/v1/overview` 短暂 503，随后服务器已恢复。
- 复现：真实 Chrome 仅拦截第一次 overview 为 503，后续全部放行；观察 6 秒，overview 请求 1 次、SSE 请求 0 次，任务列表可以成功读取，但无 Agent 选项、始终“连接中”、发送禁用。刷新页面后正常。
- 期望：有界退避重试或显式“重新连接”，保留草稿；不用用户猜测整页刷新。
- 后果：普通服务瞬断把业务入口卡住；浏览器实际在线，故 offline/online 的恢复逻辑不会自然触发。
- 证据：`evidence/web-fault-run.txt`、`web-16-overview-failed.png`。
- 源码：`web/src/main.jsx:329` 只有 refresh 成功才建立 EventSource；`:351` catch 吞掉重试；effect `:358` 仅由 session/browserOnline 触发。
- 最小修复：将初始 snapshot 与 stream 建立放入可取消的连接状态机，有界退避/手动重试，区分认证过期与暂时不可达。

### WEB-06：详情内无法审批，首页超过三项无完整处理入口（中，源码确认）

- 触发：Task waiting_approval；或组织同时有超过三项审批。
- 期望：ADR-003 要求在 Task 详情上下文完成回复/审批/取消，并能找到全部待处理项。
- 实际：详情 actions 仅回复/取消/日志；批准/拒绝仅在首页 `approvals.slice(0,3)`；没有“查看全部审批”入口。第四项只能先处理前三项才能露出。
- 后果：用户在任务详情看到需要审批却必须自行回首页找请求；多任务时难以判断审批和当前任务的对应关系。
- 源码：`docs/decisions/ADR-003-command-center-task-observation-and-content.md:32`；`web/src/main.jsx:918`–`:927`；`:1048`–`:1054`。
- 最小修复：详情呈现该 Task 的授权审批投影与决策按钮；首页提供完整待处理队列。未覆盖真实 Approval 生成/决策 API，所以本项不声称审批服务端不可用。

### WEB-07：登录服务故障被归为密码错误（中）

- 触发：login API 返回 503，或 fetch 网络失败。
- 复现：正确隔离凭据、单次 login 503，页面显示“用户名或密码错误”。
- 期望：认证失败与服务不可达分别提示；后者保留输入并允许重试。
- 后果：错误引导用户改密码/重复提交，掩盖服务恢复问题。
- 证据：`evidence/web-fault-run.txt`、`web-17-login503.png`。
- 源码：`web/src/main.jsx:173`–`:174` 捕获全部错误并使用同一文案。
- 最小修复：按 APIError.status 和网络异常分流，使用与 NetworkSettings 一致的可行动错误模型。

### WEB-08：首页忙闲状态随是否访问任务页改变（中）

- 触发：有未完成任务，但刚登录或重新加载首页，taskPage 尚未加载；任务页筛选也会污染首页判断。
- 复现：同一个 B queued Task，直接首页显示 `online · idle`；进入任务页再返回，变为 `online · busy`，期间 Task 未改变。
- 期望：组织态势来自全局权威概览，不依赖用户当前筛选或浏览历史。
- 后果：用户误判 Agent 空闲，无法据此安排工作；不同用户页面给出不同业务状态。
- 证据：`evidence/web-home-state.txt`、`web-20-home-idle.png`。
- 源码：`web/src/main.jsx:414` 将 tasks 指向 taskPage.tasks，`:888` Agent 卡从此筛选；已有 overviewTasks `:391` 没被用于这里。
- 最小修复：首页使用概览中的完整活动任务/Agent 投影，不复用分页/筛选列表；补初次登录与筛选后返回的回归。

## 信息架构与局部重构取舍

| 当前用户负担 | 对用户目标的影响 | 最小改动方向 |
|---|---|---|
| 日常入口四项“指挥/任务/组织/Runtime” | “我要开始工作”与“管理底层执行”并列；Runtime 实际只有网络设置，命名也不直接 | 以任务为主入口；Agent 可执行状态与恢复入口相邻；组织/网络归到管理或设置 |
| Task/Run/Worker/Backend/generation 同时露出 | 为理解“有没有开始、等谁、有什么结果”先学领域对象 | 主区只显示用户状态/当前动作/最新结果；内部 ID、generation、版本放可展开诊断 |
| 查看 Task 时自动展开运行日志 | `selectTask` 总是 setShowRunLog(true)，正常任务也展示 spec/binding/事件内部名 | 默认结果与可读过程摘要；遇错或用户主动展开时展示全日志 |
| 移动详情常驻完整新任务输入区 | 截图里输入区占约 180px；已完成任务阅读空间被压缩，且容易误把新任务当继续对话 | 按任务状态提供“回复/新任务”明确动作；阅读态折叠输入，保留就绪提示 |
| 同一 Agent 有筛选选项、执行选项、首页工作台入口 | 列表筛选与发送目标是不同状态，但名称近似，页面未解释两者关系 | 列表筛选独立标注；任务详情内的新指令明确“新建给谁”，避免暗示继续当前 Task |
| 网络配置必须测试后再保存，最终还要等应用 | 安全上合理，但首次配置未绑定和任务阻塞没有串成引导 | 保留服务端测试/版本/回执；用户侧提供“检查并启用”，展示每阶段进展和失败点 |
| raw 英文 status/event 与原始错误 | 页面同时中英文、错误难解释，用户不知能否重试 | 集中状态词典/诊断映射，保留技术代码在详情中 |

局部重构**有必要但应由已复现问题驱动**：main.jsx 把认证、连接、概览、列表、详情、输入、审批、网络协调混在同一 App；WEB-03/08 已证明状态来源混用不是单纯文件长度审美。优先提取 authoritative task view/readiness、connection lifecycle、task actions 三个边界；Markdown/PWA/幂等保护继续复用。当前没有证据支持换框架、加通用工作流 DSL、重写整套投影或扩大组织模型。

## E2E 现状：哪些是真实，哪些缺失

- `web/package.json` 的 observation/network 测试是 Node 对状态辅助函数的单测，本次 17/17；`test:pwa` 只是检查 main.jsx 中若干安装字符串。它们不能覆盖真实 DOM、接口顺序、浏览器故障或业务效果。本次 WEB-03/05/08 均未被这些测试拦住。
- 2026-08-30 Task17 报告确实记录真实浏览器、视口、离线和 SW，但没有完整“选择 Agent→真实 Runtime→回复→下一项”证据；不能从报告标题 E2E 推导全业务流程。
- 2026-09-20 ADR-009 候选报告的默认 profile 闭环使用真实 daemon/Worker/PTY 与 fake Runtime、fake systemctl。2026-09-22 追加说明明确了这一边界；这是控制面/Console 集成，不是外部 Agent 业务验收。
- 2026-09-22 安装报告后续已经记录**真实网页发送、真实 Runtime 回复、同 Worker 连续两项**及用户复核，且明确 Task 仍 uncertain/business_effect_unverified。不能说项目完全没有真实 E2E，也不能把这些历史单次现场证据当作持续运行的浏览器回归套件或本次已复验。
- 本次隔离浏览器是可以重复的真实 HTTP/DOM/Worker 链，但 fake Runtime 只能证明产品接线与交互。随后主代理协调同一浏览器环境接入真实 AGY，完整链路已直接贯通，详见下节；该结论来自同一链的证据，不是不同链路 PASS 相加。

### 下一步最小 E2E 验收

1. 修复高优先级断点后，在单个隔离环境做真实 Chrome→认证→真实 Agent 选择→正式 wrapper 执行→可核对结果→同 Worker 下一项；同时观察 Task 和 Run，按既定语义报告 uncertain，不伪装业务成功。
2. 只增加四条关键失败回归：首次网络未就绪有明确引导、queued 可取消且后续不执行、多轮显示最新回复、初始 overview 瞬断可恢复且不重复发送。
3. 保留移动离线/不重放和安全 Markdown smoke；真实审批、实体键盘/PWA 安装、长断线等作为明确未覆盖项，不先扩建通用测试平台。

## 命令、结果与限制

| 命令/脚本 | 退出与结果 | 证据 |
|---|---|---|
| `node --test src/task-observation-state.test.js src/task-dispatch-state.test.js src/network-binding-state.test.js` | 0，17 passed | `evidence/web-unit.txt` |
| `npm run build`（独立 web copy） | 0，Vite 8.2.2、268 modules | `evidence/web-build.txt` |
| `node scripts/test-pwa-install.mjs` | 0，source markers only | `evidence/web-pwa-static.txt` |
| `probe.mjs` | 0，真实 Chrome 登录/网络页 | `web-01-initial-network.png` |
| `journey.mjs` / `complete.mjs` 首轮 | 1，导航 locator exact 未包含图标导致超时；不是产品故障 | 纠正为 nav 内含名查找后继续，不降低验证层级 |
| `continue.mjs` | 0，reply→success→next Task | `web-06`–`web-09` 截图/DOM |
| `mobile.mjs` | 0，移动/安全/离线/SSE；脚本完成不代表取消产品成功 | `web-mobile-run.txt`，`web-10`–`web-15` |
| `faults.mjs` | 0，overview/login 故障与多 Run 错配复现 | `web-fault-run.txt`，`web-16`/`web-17` |
| `final-probe.mjs` | 0，取消 400、重载、实际网络应用、正式登录页只读 | `web-final-probe.txt`，`web-18`/`web-19` |
| `home-state.mjs` | 0，首页状态依赖浏览历史复现 | `web-home-state.txt`，`web-20` |

最初复制 web 时 cwd 已在 web 下却使用 `web/.`，一次失败后改绝对路径；检查 pexpect 不存在后直接使用 Python 标准库 PTY，未安装依赖。上述工具错误不计为产品失败。所有临时脚本保留在隔离 fixture root，供主代理复核；归档的 `web-probe-*.mjs.txt` 不包含凭据值。

未覆盖：真实 Approval、运行中取消、viewer/operator/owner 完整权限交互、SSE 保留窗口跨越、长时间锁屏、实体手机软键盘、OS PWA 安装与独立启动、极大内容/可访问性完整审计、限定文件测试以外的真实业务副作用。修复 commit：无（本轮只评估）。本报告的问题是后续裁决输入，不自动扩成本轮实现任务。


## 联合真实 AGY 浏览器 E2E 补充（本次动态证据）

本节更新前文矩阵的真实 Runtime 结论；原 fake 场景继续承担可控等待输入、离线、安全和故障验证，不冒充本节。Runtime 代理用正式 `agent apply` 注册专用身份并运行 Worker；Web 代理全部通过真实 Chrome UI 执行网络测试、发布、等待应用和两次任务发送。

| 核对项 | 结果 |
|---|---|
| Agent / Worker | `web-real-agy` / `worker-ebf9cff7-2ee5-4fa1-9670-4b3ca003db7d` / generation 1；两任务间无重启 |
| 正式路径 | Chrome → `127.0.0.1:19173` → 正式 Control/Worker API → `agy-batch` → `/home/sky/.local/bin/agy-graft` |
| Task A | `task-bb2dbeeb-f297-40b8-97ed-0e4f97c08f16`；Run `run-47294afb-2cb0-4a1f-a540-9a3124bca575` |
| Task B | `task-9434387d-3d34-4b0b-beb8-54769a773747`；Run `run-ebc767ac-0a97-4f63-956d-93569fcb2771` |
| A 操作 | 仅在隔离 workspace 写 `artifact.txt`，内容 `OAX-WEB-E2E-20260923` 加 LF，并读回核对 |
| B 操作 | 只读同一文件，返回原文和 21 字节；不再写文件 |
| DOM / 结果 | 两次真实 running 截图，随后各显示 Runtime succeeded、原文/字节数；无页面异常 |
| 产品终态 | 两次 Task 均 `uncertain` / `business_effect_unverified`，未自动重试或人为改为成功 |
| 独立文件核验 | 精确 21 bytes，SHA-256 `7459efc1414b18e032060b711f8fd712bb6ffa6321c36bed2f1fe38656a06e06` |
| 持久证据 | 每 Run 有 started/finished、AGY init/result；A/B 各 29/22 个 step_update；各 Mailbox accepted、attempts=1 |
| 唯一 POST | 网络 test、network publish、Task A、Task B，共 4 次；没有额外任务重放 |
| 时间 / 退出 | 浏览器联合脚本 43,610ms，exit 0；未触达 20 分钟上限 |

证据：`evidence/web-real-e2e.json`（安全 API 投影与实际 DOM）、`web-real-e2e-run.txt`、`web-21-real-network.png`、`web-22`/`web-24` running 截图、`web-23`/`web-25` 完整结果、`web-26-real-result-desktop.png`/`web-27-real-result-mobile.png` 可读视口；独立 Runtime 核验见 `runtime-web-final.json`、`runtime-web-setup.json`、`runtime-web-cleanup.json`。

这一真实链也暴露过程呈现偏薄：运行日志大量重复 `runtime.agy.step_update` 事件名而没有可读活动说明；结果全文再在 raw event 中重复。这里没有要求泄漏隐藏推理或原始 stderr，而是建议合并重复事件、显示安全阶段摘要和时间，保留高级事实展开。Task `uncertain` 是既有终态规则下的真实状态，本次外部人工核验不构成产品自动提升终态的授权或实现。

清理：Runtime 代理以 SIGTERM 关闭自有 AGY Worker PID 3225460，数据库记录 offline；本代理随后关闭 fake Worker PID 3142660/3142662、daemon PID 3140838，全部进程已不存在。未修改任何生产服务。归档脚本与复现前置说明见 `evidence/web-reproduction.md`；不含 password/state 文件或真实秘密值。
