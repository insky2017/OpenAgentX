---
doc_type: validation_report
status: installed-runtime-validation-blocked
owner: openagentx
updated_at: 2026-09-22
---

# ADR-009 实机安装与 Console 验收

## 范围与结论

用户在候选门禁后明确授权安装。已完成 `5bebf14` 候选安装、daemon/Worker 更新和默认 tmux server
的真实 `OAX:quote-service.0` Console 验证。这次不再使用隔离 tmux 或 fake Runtime 冒充现场。

**安装通过，外部 Runtime 任务闭环仍受阻。** 原网络 binding 的 applied generation=47，更新前 Worker
已为 generation 50、更新后为 51。正式 Backend 解析拒绝沿用旧 Worker 绑定，`primary=unavailable`。
这解释了原请求一直 queued；在线 Worker 不等于存在可调度 Backend。未通过直接改 DB 或扩展 CLI scope
绕过它，也没有实现 ADR-007 代际继承。

17:15 后补充：已修复并安装 Web 对旧 generation 回执误报当前生效的问题，源码 `77ae678`；本地及
公网 HTTPS 资源摘要一致，daemon/Worker 未重启。当前网络发布仍未落地，真实 Runtime 验收仍 blocked。

现有 CLI owner Token 验证通过，但网络管理不属于其冻结 scope；检查用 Web 页面未登录。需要 Web owner
在网络设置选中 `quote-service / primary` 当前 generation 51，保持 `系统默认 (inherit)`，先执行
`测试连接`，成功后 `保存并应用`。完成正式测试/发布后才能继续真实回复与第二项任务验收。

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
