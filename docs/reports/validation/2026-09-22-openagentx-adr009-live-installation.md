---
doc_type: validation_report
status: installed-task-success-pending
owner: openagentx
updated_at: 2026-09-22
---

# ADR-009 实机安装与 Console 验收

## 范围与结论

**22:34后用户复核：真实网页过程和回复已验证，Task成功终态仍未完成。** 用户指出最终仍显示
`uncertain`，该反馈成立，不能把“回复已送达”扩大为用户要求的任务成功。现有
`FinishRun`在Runtime succeeded且`SideEffectsKnown=false`时持久化Task uncertain及
business_effect_unverified；AGY未提供独立核验证据，当前没有只读query的独立成功规则。
此缺口已归属仍为Proposed的[ADR-006](../../decisions/ADR-006-task-intent-and-verifiable-terminal-semantics.md)，
本轮不修改被冻结的Task领域语义。`LIVE-03`仅关闭网页发送/跟踪/回复故障，整体任务成功验收保持待完成。

**22:34阶段事实：真实网页发送、Worker执行、过程更新和最终回复已确认。** 用户在
真实页面先看到新Run starting，再看到同一Run succeeded及时间回复；正式Task/Run快照和Journal
核对一致。同一generation52 Worker连续完成两条新任务，随后仍online/primary healthy、无重启。
本结论仅覆盖本轮网页闭环；Task仍为`uncertain/business_effect_unverified`，`LIVE-02` Diagnostic
跨Task显示问题仍待独立修复，不扩称完整ADR现场验收通过。下面保留此前待验和失败事实。

**20:50更新：网页闭环修复`ee46038`已安装，真实owner Web发送与页面回复待验。** 用户继续测试后
发现19:59 Task被默认发给offline orchestrator且一直queued。此前真实Console执行证据不覆盖网页
默认目标，现已明确执行Agent、保存选择、发送后自动追踪准确Task，并把回复置于详情前部。隔离
浏览器多Agent/手机/失败流程已通过，不能将其扩大为真实网页发送验收；本节以下保留原阶段事实。

**19:42更新：本轮重新安装、PC/手机发送区与一次真实Runtime执行验收通过。** 已安装`6d599ac`，
包含主程序摘要漂移warning修复`8b621ce`；quote-service generation52的真实Run已succeeded并返回
可核对的当前时间。Task仍按既有规则为`uncertain/business_effect_unverified`；`LIVE-02`仍独立待办，
不宣称完整ADR现场验收通过。新的构建、安装与任务证据见文末。本节以下保留16–17时的原失败历史。

用户在候选门禁后明确授权安装。已完成 `5bebf14` 候选安装、daemon/Worker 更新和默认 tmux server
的真实 `OAX:quote-service.0` Console 验证。这次不再使用隔离 tmux 或 fake Runtime 冒充现场。

**安装、网络恢复和原任务真实回复已验证；连续第二项 Runtime 执行仍受阻，整体现场验收未通过。**
17:37:59 用户经正式 Web 发布的 inherit binding version6 已由当前 Worker generation51 应用，Backend
恢复healthy；原Task于17:38:10完成Run并返回时间，真实pane0能显示过程、Task终态和Runtime回复。
Task仍为uncertain/business_effect_unverified，按ADR-009如实保留，不改变ADR-006语义。

连续第二项无副作用任务被同一Worker领取，但因外部AGY二进制从已固定的1.2.7变为1.2.8而无法建立
controlled turn。当前文件mtime17:38:07，实际摘要与网络策略固化摘要不同，身份校验正确拒绝；
更新主体/机制尚未确认。不自动重试uncertain任务、不绕过校验、不重复要求用户配置网络。

先前gen47回执误报已由Web修复`77ae678`安装解决，本地及公网资源hash一致。另有真实Diagnostic摘要
跨Task取旧output的显示缺口待修复，见末节；不能把单任务回复通过写成全部ADR现场验收完成。

来源：[候选报告](2026-09-20-openagentx-adr009-release-candidate.md)、
[执行记录](../../plans/2026-09-19-openagentx-adr-009-implementation/EXECUTION-LOG.md)。

## 安装 provenance 与回滚

| 项目 | 实际值 |
| --- | --- |
| 源码 | `5bebf148c414c2ca8bd8d2e6367f3e828b2ec2af` |
| 候选路径 | `/home/sky/.cache/openagentx-builds/openagentx-adr009-5bebf14` |
| 安装路径 | `/home/sky/.local/bin/openagentx`，`0755 sky:sky` |
| candidate / installed / daemon / Worker / Console SHA-256 | `e482b113311ba618ac45512bf3098d431e739ab61bfc681ec3d2f90f483300c2` |
| Go provenance | `go1.22.4 linux/amd64`、精确 revision 如上、`vcs.modified=false` |
| daemon 启动 | `2026-09-22 16:04:14 CST`，PID `830503` |
| Worker | PID `835790`，`worker-cf83eb66-4b84-4453-84b9-20fc1024d3eb`，generation `51` |
| Console | 首次 PID `835479`；正常退出并恢复后 PID `870030` |
| schema | `schema_meta.version=1`；升级前备份和升级后 `quick_check=ok` |
| 监听 | `127.0.0.1:18100`，未扩大网络暴露 |
| 回滚目录 | `/home/sky/.openagentx/backups/adr009-20260922-DTkpFj`，`0700 sky:sky` |
| release evidence | `/home/sky/.openagentx/release.txt`，`0600 sky:sky` |

回滚目录保留旧 `openagentx-f49cec4`、一致 SQLite backup、原 unit/config/manifest/release evidence。
数据库备份为 `0600`，`quick_check=ok`。原二进制 SHA-256 为
`984bb0df415b18f49959eda474076f0c807d90e6b5392342083249e605771114`。
本批没有恢复数据库；回滚时仍必须先 graceful drain，并只在服务停止且确认需要时恢复 DB。

16:04 安装批次沿用 Worker YAML、Fleet manifest、两个 unit、Wrapper、Web assets 和网络策略。
17:15 单独更新的 Web assets 见下方补充记录，其余相关摘要未变：

| 对象 | SHA-256 |
| --- | --- |
| `quote-service.yaml` | `edfb1dd732d5c063e8ea2b02fcb623812b722d66b03bdd8e13a332cd46352433` |
| `fleet.yaml` | `c19acf1c78f0cc9b550e0d745ac66d96dcbc0fdf3d34129f61b2579e85e2e659` |
| `agy-graft` | `31935c961ea76532962068c0e1a8d0a258c80d2f74940dcff3532705ca38b8de` |

Wrapper 正式路径 `/home/sky/.local/bin/agy-graft`，Adapter `agy-batch`，backend `primary`，
工作目录 `/home/sky/work/touzi/OneAxe/steadyflow`；AGY `--version` 为 `1.2.7`。
这里只核对现有配置/version，没有绕过 Worker 另起 AGY 请求，也没有声称此时已完成外部 Runtime 调用。

## 实际命令与证据

| 步骤 | 命令或验证 | 退出/结果 |
| --- | --- | --- |
| 候选预检 | `sha256sum`、`go version -m`、`stat` | 0；符合候选报告 |
| 正式会话/配置 | `openagentx fleet status`；复用正式 credentialstore/client 的验证器 | 0；session 验证、installation probe 成立，不输出 token |
| drain | 旧 binary `openagentx fleet down` | 0；持久化 stop，约 1s 后 offline，Worker `ExecMainStatus=0` |
| 备份 | SQLite `.backup` 后以 `-readonly`、`query_only` 执行 quick_check | 0；ok/schema 1 |
| Console 旧进程 | 精确校验 pane PID、exe/hash 后 `SIGTERM 313504` | 0；只退出已识别的旧 Console，保留 remain-on-exit pane |
| 安装 | stop daemon；同目录唯一临时 binary、hash、`sync -f`、原子 rename；start daemon | 0；未覆盖运行中 binary inode，未重装 unit/Wrapper/Web |
| schema | `openagentx schema verify` | 0；v1 verified |
| pane 恢复 | `openagentx fleet workspace --respawn-dead` | 0；只恢复 compatible dead `quote-service.0`，overview 复用 |
| Worker 启动 | `openagentx fleet up` | 0；正式 user unit 预检通过，generation 51 |
| 健康/监听 | `curl --fail http://127.0.0.1:18100/api/observe/v1/health`；`ss -H -ltn` | 0；health ok，仅 loopback 18100 |
| 外部 HTTPS | 有界 `curl --noproxy '*' ... https://agentx.oneaxe.cn/api/observe/v1/health`；真实浏览器同 endpoint | 0 / HTTP200 / health ok；见下述 shell 代理探针限制 |
| 真实终端 | `tmux attach-session -t '=OAX:=quote-service.0'`，通过该 client 的 PTY 逐次输入命令/Enter | alt-screen、真实 Task 状态和 overlays 可见；未用 send/paste/capture |
| 同 pane 模式 | `/status`、`/diagnostic`、`/normal` | 当前 Worker/Backend/Task 信息可见；Diagnostic 切换成功并恢复 Normal |
| Console 生命周期 | `/quit`，只读检查，再 `fleet workspace --respawn-dead` | pane dead/status 0；Worker PID835790/gen51 不变；Console 恢复 PID870030 |
| tmux client 退出 | 本次 client 输入其已核验 prefix `C-b d` | 0；仅 detach 本次 client，Console 和 Worker 保持运行 |

真实输出证据摘录（只含正式安全投影）：

```text
Focused Task task-3abf6b7c-9... | version 1 | status queued | stage mailbox pending
#195604 mailbox.claimed | Task task-3abf6b7c-9... | version 1 | status queued
Worker: worker-cf83eb66-4b84-4453-84b9-20fc1024d3eb
Generation: 51
Backend health: primary=unavailable
Active run: none
Connection: connected
Task outcome state: pending
Console mode switched to normal
Pane is dead (status 0, Tue Sep 22 16:10:18 2026)
```

现场 `quote-service` window 只有 pane 0；其他 zsh/pi window 的 pane 1、全部 unmanaged window 均保留。
没有为了展示辅助 pane 测试而向真实 window 新建 pane；pane1/2 保留的覆盖仍引用已有隔离测试证据。

## 失败、限制与后续验收

独立验证代理完成一次只读核验：installed binary 精确 hash/revision/modified=false，三个运行进程均映射
同一 binary inode；daemon/Worker active/running、NRestarts=0；真实 pane0/name/markers 正确。另以源码
核对 generation fencing 和 CLI/Web 网络授权边界。该复核没有读取 DB 或调用 Runtime，不扩大为业务通过。
本报告与执行日志的 5 个相对链接、whitespace、两文件提交边界及冻结 ADR/AGENTS hash 均经检查。

- 可选 `quote-service.env` 不存在使辅助 `hash/stat` 返回 1；unit 明确为 optional，保持不存在。
  几次源码定位因误用 import alias 路径或 zsh 不匹配 glob 返回非零；纠正后继续，仅工具查询错误。
- 首个外部 curl 继承当前 shell 的代理环境且遗漏请求 timeout，持续无返回；核对它是本轮唯一精确
  PID893114 后终止，只取消本轮探针。随后显式 `--noproxy '*' --connect-timeout 10 --max-time 20`
  请求约 0.66s 返回 HTTP200/health ok；真实浏览器同请求也200。该环境差异不冒充 daemon 故障，也未修改
  用户全局代理或 Worker 网络。后续外部探针必须有明确 timeout。
- 真实旧 Task `task-3abf6b7c-9b41-4238-b5b2-044f6627d70e` 的安全快照确认 version 1 / queued /
  mailbox pending/claimed，尚无 Run。升级前已经等待数日，本次未重新 dispatch、取消或伪造完成。
- 正式 Attach 返回 `primary=unavailable`；binding 与 current Worker 的 ID/generation fencing 是真实
  调度阻断，不能被 heartbeat 自报 healthy 掩盖。generation 47 不是本次升级引入的新配置。
- Web 会话缺失阻断正式网络重验证；已向用户请求在其 owner 会话执行原模式的测试/应用，不索取密码。
- 外部 Runtime 的真实回复、workspace 副作用和连续第二项任务尚未通过；不把 queued、pane 可见或安装
  正确写成完整 E2E。原请求仅查询时间，不需要文件副作用；后续使用唯一标记、无文件写入的低影响任务。
- 原候选普通/race/Web/systemd/隔离 E2E 证据有效，产品代码和候选未变，本批不重复全仓长测试。
- 未 push/merge、未修改 main 或 steadyflow 父仓，未修改冻结 ADR。安装授权不扩大为 ADR-006/007 实现。

## 2026-09-22 16:48–16:55 保存提示与实际应用复核

用户报告保存成功后，正式 Attach 仍为 gen51 online / primary unavailable，原 Task 仍 queued/version1。
只读核对确认：16:48:09 最新 inherit 测试已成功，目标 gen51、binding revision5；但本轮没有 mode_publish
命令、binding pending event 或 apply work，实际 binding 仍为9月11日的 version5/applied gen47。

前端存在明确的误报分支：`NetworkSettings.handleSaveAndApply` 在最新匹配测试尚未成功时，只检查旧
binding 的 mode 和 desired_status=applied，便提示“当前 Runtime 已经生效”，没有校验 applied Worker /
generation。该分支可以解释成功提示而无发布请求；未取得用户浏览器的请求记录，不将推断写成已复现的点击轨迹。

最小恢复路径是先确认最新测试成功，再在用户现有 Web owner 会话刷新数据并仅保存一次，以实际 Worker
回执为准。已经给出该操作提示；若仍不能发送正式发布请求，不再让用户循环测试/保存，应单独修复页面。
16:55检查点仍未应用，真实 pane 和60s正式 Follow 仅显示旧 Task 的 Mailbox claim。本轮没有新增业务 Task、
改产品代码或真实配置，继续保持 `installed-runtime-validation-blocked`，不宣称完整现场 E2E 通过。

## 2026-09-22 17:15 Web 误报修复安装

用户再次刷新保存后仍无本轮已接受的 mode_publish，binding version5/applied gen47 未变。已按执行日志
裁决完成最小 Web 修复、一次独立验证与静态资源安装；不修改 Go/权限/fencing/代际继承。

修复提交：`77ae6785eb33e30b29155dcc580a083fd79e5cf2`。当前 Worker 是否已应用统一校验 agent/backend/
Worker instance/generation/binding revision/mode 及 policy/profile；历史回执明确标为当前未应用。
测试进行中提示等待，当前成功测试提示可保存，发布提交与实际回执分开展示。附带5行CSS使目标和长回执
在390px屏幕内可读。未改后端和用户网络模式。

| 证据 | 结果 |
|---|---|
| 原组件真实 Chrome 复现 | pending test + gen47 applied 在 gen51 上保存误报“无需重复保存”，0请求 |
| 修复组件真实 Chrome | 同条件不再误报；成功test后保存一次，正式publish路径、gen51、CAS5；回执前警示、精确回执后绿色 |
| 失败/竞态 | offline/无写权限禁写；409不报成功，显式重试复用Idempotency-Key；进行中测试不会自动重试 |
| 桌面/窄屏 | 1280px与390px实际渲染；390px原521px横向溢出已消除 |
| Node / PWA | network 7/7、observation 4/4、PWA通过，退出0 |
| 独立生产构建 | `npm run build`退出0；最后提示边界微调后仅重建，无Go改动 |
| release / whitespace /冻结 | scanner、diff check通过；ADR-009/AGENTS hash未变 |
| local/public/browser assets | 均HTTP200，JS/CSS SHA-256与已验证dist相同 |
| 运行服务 | daemon PID830503、Worker PID835790，active/running，NRestarts均0，gen51不变 |

| 安装产物 | SHA-256 |
|---|---|
| `/home/sky/.openagentx/web/assets/app.js` | `56d5dedce662f81a0c5197b1d4cf70b199fbac7e1886df98d99bf6126feae767` |
| `/home/sky/.openagentx/web/assets/app.css` | `76f34cdd88c2df1f079bc49d97ab2b68eea1e955ecaaa117396a6c9556227e8f` |

原Web目录保留在 `/home/sky/.openagentx/backups/adr009-20260922-DTkpFj/web-77ae678-9I5u5B/web`，
备份父目录0700；只替换JS/CSS，采用同目录唯一临时文件、0644、cmp、fsync与rename。`release.txt`
分别记录Go binary和Web source revision。`staticHandler`逐请求读取文件，因此无需重启服务或引入新代次。

临时UI fixture在`/tmp/oax-network-ui-zKSFFg`，仅监听loopback，实际API回调由fixture控制；它证明组件
行为，不证明真实owner鉴权/Worker执行。原组件在成功test刷新后能正确发送publish，用户本次浏览器的
具体请求轨迹仍未知；真实DB无mode_publish不等于证明从未发过被拒绝的HTTP请求。

17:16正式安全Attach仍为gen51/primary unavailable，原Task仍queued。检查用公网浏览器仍显示登录页，
不能代用CLI credential或提取Web session发布。需要用户整页刷新加载新资源，在现有owner会话确认
`quote-service / primary · gen51`、inherit及“测试成功，可保存并应用”，然后保存一次并核对完整提示。
页面内刷新仅更新数据，不替换已载入JS。没有再创建测试或业务Task；后续以真实Worker回执/Run/回复收口。

## 2026-09-22 17:38–17:48 真实回复与连续执行

用户在新版页面收到发布等待回执及活动Task提示后恢复剩余现场验收；本次最多15分钟、不改产品代码。
只读正式API和受限query_only查询确认：17:37:55 mode_publish接受，17:37:59 inherit binding version6 /
policy13 applied到`worker-cf83eb66-4b84-4453-84b9-20fc1024d3eb` / gen51，primary healthy。

| 项目 | 原Task | 连续验证Task |
|---|---|---|
| Task ID | `task-3abf6b7c-9b41-4238-b5b2-044f6627d70e` | `task-39e79f4e-d4c3-40ef-9aa4-db72e6057383` |
| Run ID | `run-dcb09adc-5a36-4662-b1a6-2350bdbb35c6` | `run-05734f10-cc53-4bf6-929e-84ea59fb5260` |
| Worker/gen | `worker-cf83eb66...` / 51 | 同一Worker / 51 |
| Runtime | succeeded | uncertain，未建立controlled turn |
| Task | version3 / uncertain | version3 / uncertain |
| 安全回复 | `2026-09-22 17:38:06 (UTC+8)` | 无回复 |
| 错误 | `business_effect_unverified` | `Runtime Backend could not establish a controlled turn` |
| Run时间 | 17:38:00–17:38:10 | 17:42:27–17:42:28 |
| 网络快照 | inherit / policy13 / binding6 | 相同 |

连续Task只通过真实Console `/dispatch`提交一次，内容要求精确回复唯一marker、不调用工具、不读取或
修改任何文件；未重试原Task，也未因失败重新dispatch。60s正式Follow证明新Task经过created、claimed、
running、run started、mailbox accepted、run finished、settled（sequence196415–196421），没有
runtime.agy事件。它证明resident Worker继续领取，不证明连续Runtime成功。

真实PTY使用自己的tmux attach client（ignore-size，终端大小对齐现有203x52 pane），不使用send/paste/
capture。原Console PID870030保持运行，实际可见原Task的run started、agy init/step_update/result、
Runtime reply、Task outcome result/error；`/status`显示完整Task/Run/Worker身份及上述回复。
`/tasks`方向键选择已恢复原Task focus，Normal模式、空输入draft保留，最后只detach本次client。
原pane和Worker不被重启；17:47 daemon/Worker仍PID830503/835790、active/running、NRestarts=0。

独立验证代理复核了安全TaskSnapshot、Journal类型链、服务状态与二进制漂移；没有读取原始payload/
stderr或执行模型。除上述通过正式Console提交的唯一验证Task外，没有直接修改业务状态、配置、binary
或网络发布，未接触父仓。

### 当前阻断与最小后续范围

- `LIVE-01`：连续Runtime执行受AGY identity drift阻断。两Run及binding固定AGY SHA-256为
  `9991515b6d5307bcf701069622b0537b6b206e605f3c891c0cf3a3d208dea8b0`；当前文件为
  `c20434f0b9278196498069dac5a0a2e72bc0b5f8aebdf17c5d535b5369b76f67`，native/wrapper `--version`
  均报告1.2.8，mtime为17:38:07.954。wrapper/helper摘要未变。`PrepareRunNetwork`与
  `VerifyRuntimeIdentity`在StartTurn前要求精确匹配，此差异足以确定拒绝原因；尚不认定具体更新主体。
  下一步先核实更新来源和固定Runtime版本，再按正式注册/测试/发布刷新身份，最后新建低影响任务验证；
  不自动接受新hash，不假定重测网络本身就能修复Worker注册的旧身份。
- `LIVE-02`：Diagnostic显示最近Timeline output时未按focused Task/Run筛选。第二Task没有Runtime
  output时，overlay把它的`Run/wait category uncertain`与上一Task的`Runtime status succeeded`并列。
  Normal Task outcome与`/status`均准确，但此组合可能误导，必须在Diagnostic范围独立修复并定向验证；
  本次仅记录，不借安装验证扩大代码批次。保持整体现场验收blocked。
- 非阻断UI观察：本次End键没有跳底、`/tasks`过滤输入未收敛，PgDown与方向键选择可用；不影响实际
  回复/权威Task选择，后续键盘交互批次有界复核。首次PTY为80x24显示既有大pane的局部，调整自己的PTY
  大小后可完整观察，未修改用户tmux拓扑。源码定位若干不存在路径/glob返回非零，已纠正，无现场副作用。
- 未覆盖：第二Task成功回复、在活动Task期间退出Console后完成的真实证明。早先idle `/quit`与恢复
  Worker不变证据仍成立；本次不以它替代活动执行验证。没有新增业务文件副作用，不宣称文件业务成功。

## 19:20–19:43：warning版本重新安装、响应式发送区与真实Run

用户再次明确授权重新安装，要求发送入口支持手机/PC，并证明quote-service能真实执行一次。本批不改
Task业务核验、Diagnostic归属、ADR-006/007，也不自动重试旧uncertain任务。

### 发送入口与验证

- 桌面发送区从左侧列表末尾移到主内容顶部；手机列表中位于底部导航上方，进入详情后与详情使用flex
  实际分配高度，不用估算输入区高度。只有一个输入框，沿用原正式API、draft、reply及CAS逻辑。
- 真实Chromium加载生产build和隔离fixture，在1440x1000、390x844、320x568检查无横溢、回复焦点、
  详情/输入区不重叠；SSE和offline/online切换保留草稿，offline/viewer禁写。一次create对应一次正式
  Task路径请求，一次reply对应一次messages请求且携带expected_version。fixture不作为真实业务证据。
- observation 4/4、network 7/7、PWA、production build、release scanner、Worker template和diff均通过。
  Go源码未在UI批次改变，复用`8b621ce`无缓存全仓Go、定向race、vet/build通过的证据。
- 浏览器缺少新session所需Chromium时，复用现有浏览器的自有标签页；`tab new`与`find nth`版本差异
  改用显式open/DOM selector完成验证。手机详情首版估算高度已集中修正并重建复测。

### 构建、备份和安装

| 项目 | 证据 |
|---|---|
| 安装源码 | `6d599aca8ce7c32fe11f23244478b495a0a78e68`，包含`8b621ceeefc85662f1b44dd38e6eb5640490cc0d` |
| 构建命令 | `go -C /home/sky/.cache/openagentx-builds/source-6d599ac.jR1tbo build -o /home/sky/.cache/openagentx-builds/openagentx-6d599ac ./cmd/openagentx` |
| 来源 | 源仓库独立shared clone，detach精确commit，工作树clean；Go1.22.4，`vcs.revision=6d599aca8ce7c32fe11f23244478b495a0a78e68`、`vcs.modified=false` |
| binary SHA-256 | `a26ebf4d84ede2fa60bd4c10aaee704056732dde9dc532cd424d85de76703e89` |
| Web JS / CSS SHA-256 | `74399306f79b3fba474411a4b79a1ab7e4073e12dab50a71081c0aaf69cef11c` / `86ca0c644ae01048bfa7e2821862a687b722f4c2f16185ab34127c6e58323dc4` |
| 私有备份 | `/home/sky/.openagentx/backups/runtime-warning-20260922.ZxKiri`，0700；旧binary/Web/release/Worker YAML和0600一致SQLite backup |
| daemon / Worker / Console | PID1686100 / 1687781 / 1687481，三个`/proc/PID/exe`与候选、installed摘要一致 |
| Worker身份 | `worker-e14b0b6d-5db6-443a-aa19-9edd21ed7be6` / generation52 |
| schema/监听 | backup与真实库quick_check=ok；schema v1；仅127.0.0.1:18100 |
| 权限 | binary0755、Web目录0755；socket/credential/Worker YAML/release为0600 |

首次从原worktree执行Go build时，Go1.22向外找到OneAxe的`.git`目录，错误写入外层revision和
modified=true；`go build -x`证明git查询cwd为OneAxe。该产物未安装，改用上述隔离clone重建并核对
provenance后才部署。旧Worker通过正式`fleet down`持久stop intent约1s内graceful offline；旧Console
经真实PTY输入`/quit`，pane保留且exit0。完成一致DB备份后停止daemon，同目录stage+校验+sync+rename
安装binary/Web，再恢复daemon、`fleet workspace --respawn-dead`和`fleet up`。未改unit/config/wrapper。

本机默认curl代理路径一次HTTPS握手失败；`--noproxy '*'`直连HTTPS health正常，JS/CSS与本地构建精确
同hash；真实浏览器可打开公网登录页。未修改代理或网络暴露。用户在owner Web完成gen52测试/应用，
19:37:57 Worker确认inherit / policy14 / binding7 applied，随后正式Attach显示primary healthy。

### 新任务真实证据

| 项目 | 结果 |
|---|---|
| 唯一提交入口 | 真实`OAX:quote-service.0` Console `/dispatch`，只提交一次只读date查询 |
| Task | `task-43704170-fff1-48f8-a52a-1b1696bdbaa3`，version3 |
| Run | `run-96a8fe19-5344-4105-bb77-41d5f8dd12fb`，version2，`succeeded` |
| 执行时间 | 19:39:15–19:39:28 CST，约13s |
| Runtime | `agy-batch` / `primary` / `gemini-3.7-flash-low`，inherit / policy14 / binding7 |
| 回复 | `验收 OAX-20260922-1938：2026-09-22 19:39:24 +0800`，位于真实执行时间窗口内 |
| 正式Follow | sequence197126–197145：created、claimed、running、Run started、accepted、agy init/step_update/result、Run finished、Task settled |
| Console | Normal画面实际显示过程、Runtime succeeded和完整回复；没有controlled-turn错误 |
| Task状态边界 | `uncertain/business_effect_unverified`；side effects/business verification均not_recorded，未提升为业务成功 |
| 完成后 | 正式Attach：online、primary healthy、active_run=null；daemon/Worker仍原PID、NRestarts=0 |

PTY只使用自己的tmux attach client，不send/paste/capture。结束前尝试查看`/status`时，共享session
被切到其他window；立即停止输入并detach自己的client，没有在那里提交Enter或新任务。原quote-service
Console保持运行，其他window/pane未kill/respawn；本次不声称清理了共享现场的所有输入draft。

`LIVE-01`在本轮范围关闭：摘要漂移的源码warning矩阵已通过，新安装真实Run成功；不声称本次运行中
人为替换了AGY。`LIVE-02`仍待独立Diagnostic修复。未新增业务文件副作用验收，未验证手机真机或所有
浏览器内核；本轮采用真实Chromium手机viewport。未push/merge、未修改main或steadyflow父仓。

## 20:28–20:50：网页执行目标与回复跟踪修复

### 根因与最小修复

- `task-17beccd5-0c86-4a26-b699-85294132a0e7`于19:59:30发给offline orchestrator，queued且无
  Run；同期quote-service仍online/primary healthy、gen52网络binding7 applied。本例尚未进入
  Runtime/代理网络阶段。根因为Web默认`selectedAgent || agents[0]`，刷新不保存目标，列表筛选
  不改变发送对象；上批单Agent UI fixture和Console真实执行漏掉了多Agent网页入口。
- 提交`ee46038102ebd44d387e26ab912001f26b89ade5`新增明确执行Agent selector和Worker状态；
  唯一有效online Worker可默认选中，显式/URL离线或未知目标禁发且不静默替换。目标写入URL。
- 严格使用正式平铺CreateTaskResponse的task_id/task_version/task_status/sequence；接受后清除
  旧筛选并打开准确Task，展开执行过程。后续读取失败仍保留已接受事实，防止用户误重发。
- 回复优先展示；业务核验提示保留原`uncertain/business_effect_unverified`，不把Runtime succeeded
  改成业务已核验。权限/Cookie/CSRF/正式API、Runtime/网络/Task领域语义均未改变。

### 验证与安装证据

- `test:observation`10/10，独立`test:network`7/7、`test:pwa`，production build、release scanner、
  diff和冻结摘要全部通过。Go源码未变，不重复无关全量Go/race；独立代码复核无阻断。
- 真实Chromium多Agent隔离fixture，desktop与mobile分别点击一次，准确打开task-created-1/2，
  从queued/Run starting更新至succeeded回复；刷新保留Agent/Task。显式offline orchestrator禁发。
  第三次POST接受后注入overview503，仍显示task-created-3回复，合计三次create无重复。
  offline保留draft且禁发，viewer禁发；1440x1000、390x844、320x568无页面横溢；390px回复首屏
  y350–414可见，底部composer从y596开始。fixture不作为真实鉴权/Runtime证据。
- 20:47只更新Web资源，同目录临时文件0644/cmp/sync/rename逐文件替换，Web目录无消失窗口；
  私有备份`/home/sky/.openagentx/backups/web-task-flow-20260922.t8ovjm`为0700，保留旧Web/release。
  JS SHA-256 `2e173b5064a89c61ca6a5a57f3e9ea1cc3fe8479b7695c81735edeb2053b1e7c`；CSS
  `c7e9b0f8d223ff14ab76c2280ded37613bd4c06918f002c1627e9133a89b548f`。HTTPS与installed/构建相同。
- binary仍6d599ac，daemon/Worker PID1686100/1687781、NRestarts0，gen52/primary healthy；
  未安装binary、未重启、未改网络/config/unit/DB。旧orchestrator任务只读保留，未迁移/重发。
- 工具失败：两次大块patch因上下文不匹配拒绝，无部分写入；改为局部patch。agent-browser select
  参数版本不一致，改用原生键盘选择；公网导航曾超10s，随后同一tab已显示真实登录表单，未重复
  请求或把超时忽略为鉴权成功。独立代码核对纠正旧fixture的嵌套Task假设，正式测试使用平铺receipt。
- `LIVE-03`（Web真实闭环）保持待验：已给用户明确quote-service链接，等待既有owner会话的一次
  实际发送与页面结果反馈；主代理正式Follow已连接。验证浏览器无Web登录态，不读取密码/Cookie、
  不用CLI Token伪造Web登录，也不再以Console发送作为Web验收。`LIVE-02`仍独立待办。

## 22:27–22:34：用户真实网页反馈与正式状态交叉验收

20:50批次因等待用户Web操作而停止后，用户先贴出19:59旧任务，再连续提供新Run的starting和
succeeded页面内容。本次恢复剩余验收，补证预算10分钟，仅只读核验、更新证据并收口；没有新增
测试任务或重试历史任务。用户页面证据与主代理API核验分别标明，不将隔离fixture替代真实操作。

| 项目 | 第一条新Task | 用户确认页面回复的第二条新Task |
|---|---|---|
| Task ID | `task-01ec8ae3-0895-4984-9e59-11ebf593e75a` | `task-adb0f396-ffee-41f5-9664-3c44e9b7669e` |
| Run ID | `run-61b7ec14-b8db-4d8c-b091-9b26cf5d5505` | `run-d064705f-0e18-4c01-93c4-0be47414700a` |
| Agent | quote-service | quote-service |
| Run窗口（CST） | 22:27:38.028–22:27:51.851，约13.8秒 | 22:28:53.837–22:29:06.225，约12.4秒 |
| Run / Task | succeeded / version3 uncertain | succeeded / version3 uncertain |
| 安全回复中的时间 | `2026-09-22 22:27:48 CST` | `验收 OAX-20260922-1938：2026-09-22 22:29:01 +0800` |
| Work delivery | accepted，attempts=1 | accepted，attempts=1 |
| Worker | `worker-e14b0b6d-5db6-443a-aa19-9edd21ed7be6` / generation52 | 同一Worker / generation52 |
| Runtime / 网络 | agy-batch / primary / gemini-3.7-flash-low；inherit / policy14 / binding7 | 相同 |

- 真实浏览器证据由用户当前Web会话提供：第二条Task先显示`task.created`、`task.running`、
  `run_attempt.started`及阶段starting，随后同一详情显示`runtime.agy.init`、8条step_update、
  `runtime.agy.result`、`run_attempt.finished`、`task.settled`和上述完整回复。用户可见的Run ID、
  Worker/generation及时间与正式安全投影一致。没有获取用户密码/Cookie或模拟Web登录。
- 以只读`query_only`查询Journal的sequence/type/aggregate ID/time，第二条链为#198181 created、
  #198182 claimed、#198183 running、#198184 started、#198185 accepted、#198187 init、
  #198188–198195 step_update、#198196 result、#198198 finished、#198199 settled。未读取原始
  payload或stderr。页面早先的19:59:30/#197266属于旧orchestrator任务，不能当成新Task状态。
- 两条Task均保留`business_effect_unverified`；Run的`side_effects_source`和
  `business_verification_source`均为`not_recorded`。本次证明执行链、时间回复和网页可观察性，
  不证明文件/工具副作用已独立核验，不自动把Task改为succeeded，不自动重发。
- 22:32正式Attach摘要cursor198223、heartbeat22:32:56、lease22:33:26，Worker仍online、
  primary healthy、无active Run。daemon/Worker PID1686100/1687781、active/running、NRestarts均0，
  与20:50相同；连续两条任务无需重启或人工驱动Worker领取。

| 本次命令/证据 | 退出码与结果 |
|---|---|
| 私有`verify-api task <上述各Task ID>` | 均0；正式credential/session验证后读取安全快照，两个Run succeeded，回复存在 |
| 私有`verify-api snapshot` | 0；online/healthy/gen52，无active Run |
| `sqlite3 -json 'file:/home/sky/.openagentx/data/openagentx.db?mode=ro'`，`PRAGMA query_only=ON`后查询上述Journal元数据 | 0；与用户页面事件一致，无DB写入 |
| `systemctl --user show openagentx.service openagentx-worker@quote-service.service -p Id -p ActiveState -p SubState -p MainPID -p NRestarts` | 0；进程和重启次数不变 |
| `sha256sum` installed binary、两个`/proc/<pid>/exe`及installed Web JS/CSS | 0；均与19:43 binary / 20:47 Web安装摘要一致 |

`LIVE-03`关闭，Web源码仍为`ee46038102ebd44d387e26ab912001f26b89ade5`，binary仍为`6d599ac`。
当前批次只追加本报告、执行日志及0600私有release验收事实；不重建、安装、重启、改网络或产品代码。
一次辅助`jq`误以为snapshot摘要使用DTO的`snapshot_sequence`字段而得到null，改读实际`cursor`
字段确认198223；不是产品cursor故障。既有Web/独立浏览器测试经影响核对复用，不重复全仓长测试。
whitespace、相对链接、状态/提交路径和冻结ADR检查随本次文档提交核验。`LIVE-02`仍待独立修复；
手机真机、其他浏览器内核和独立业务副作用核验不在本次新增证据中。旧queued任务保持原状。

### 用户指出uncertain后的终态边界复核

- 用户已确认页面最终回复，同时明确指出Task仍uncertain。主代理纠正此前“闭环跑通”的宽泛表述：
  发送/执行/观察/回复可用，不代表Task已判定成功，报告状态改为`installed-task-success-pending`。
- 根因位于`internal/persistence/sqlite/worker_execution_repository.go`的`FinishRun`：
  Runtime succeeded且`!turnResult.SideEffectsKnown`时将Task置uncertain并设置
  business_effect_unverified。`internal/runtime/agy/stream.go`只将Runtime声明保存在
  `RuntimeSideEffectsKnown`，没有升级为独立证据。不是SSE延迟或仍在等待Runtime。
- ADR-006已明确记录只读问答的这一缺口，但仍Proposed/未授权实施；ADR-009明确排除改变intent和
  成功判定。下一项应单独裁决query/mutation、调用者声明权限和可持久化成功证据，不能因提示词写了
  “只读”或模型回复成功就放宽判定。本批只做根因核实，不修改代码、旧Task或冻结ADR。
- 源码定位两次包含不存在的`internal/service`或`internal/runtime/types.go`导致rg退出2；改为已存在
  的实际package/全仓符号搜索定位，未产生产品或现场变更。
