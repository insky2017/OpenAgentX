---
doc_type: validation_report
status: passed-candidate
owner: openagentx
test_id: ADR-009-T08
validated_at: 2026-09-20
---

# OpenAgentX ADR-009 release candidate 验证

## 结论

`passed-candidate`。`codex/adr009-task-console@5bebf148c414c2ca8bd8d2e6367f3e828b2ec2af`
通过完整提交链与 diff 审查、无缓存 Go 普通/race、vet/module、Web、Shell/systemd、release/security、
隔离 tmux/PTY、临时 HOME/DB/UDS/credential/default-profile 用户闭环和关键故障注入。未发现新的 P0/P1
产品缺陷。

候选二进制从同一 revision 的 clean detached standalone clone 构建，`go version -m` 确认
`vcs.revision=5bebf148c414c2ca8bd8d2e6367f3e828b2ec2af` 且 `vcs.modified=false`。本结论只表示候选可进入
最终监督 gate，不表示 Task 08 已完成、ADR-009 已部署或真实主机已升级。feature 未 push/merge，候选未
安装，真实 service、DB、socket、credential、default tmux、user-systemd 和父仓均未修改。

## 基线与边界

| 项目 | 结果 |
| --- | --- |
| ADR-008 权威基线 | `008b2e08923614182811023eddd652df69aa98a9` |
| feature branch | `codex/adr009-task-console` |
| implementation HEAD | `5bebf148c414c2ca8bd8d2e6367f3e828b2ec2af` |
| 远端基线 | `origin/main@c3fc1bba8ddbae3eedace0c7a32537e2f47db307`；提交前 feature ahead 52 |
| ADR-009 范围 | 17 commits；59 files；9100 insertions；353 deletions |
| Task 08 写入边界 | 仅本报告与 `EXECUTION-LOG.md` |
| 外部状态边界 | 未 push/merge/install/restart/migrate/deploy；未获单独真实主机只读核验授权 |
| 候选二进制 | `/home/sky/.cache/openagentx-builds/openagentx-adr009-5bebf14` |
| 候选 SHA-256 | `e482b113311ba618ac45512bf3098d431e739ab61bfc681ec3d2f90f483300c2` |
| 候选权限 | cache directory `0700 sky:sky`；file `0700 sky:sky`；19523320 bytes |
| 冻结文档 | ADR-009 `afb7473...32c69`；AGENTS `b532645...2a95b` |

## Traceability matrix

| ADR-009 条款 | 代码产物 | 测试与文档证据 | 审查结论 |
| --- | --- | --- | --- |
| §1 Pane 0 Task-centric Console | `internal/cli/console/tui.go`；`internal/consolemodel/task_state.go` | focused Task、详情 overlay、真实默认 profile E2E；[安装指南](../../operations/openagentx-user-install-guide.md) | dispatch 自动 focus；完整 ID/version/status 可查；退出只 detach |
| §2 权威 Console Task 投影 | `internal/api/console/task_projection.go`；`internal/persistence/sqlite/console_task_snapshot.go` | ownership、分页、snapshot N/N+1、projection failure tests | Agent/Task/Run/Worker 归属与 high-water 在事务/正式 API 边界内 fail closed |
| §3 生命周期 Timeline | `internal/consolemodel/{reducer,task_state}.go`；`internal/cli/console/tui.go` | queued/claimed/running/waiting/terminal、heartbeat 与真实 E2E | 只显示持久化事实；无证据时明确等待，不制造伪进度 |
| §4 终态与最终回复 | `internal/runtime/{contract,agy,acp,codebuddy}`；Worker execution repository；TUI | empty/malformed/contradictory/terminal tests；Task outcome/Runtime reply dedicated-line tests | Task outcome、Runtime reply、执行证据分层；`uncertain` 不伪装成功 |
| §5 共用安全输出 | `internal/safeoutput/projector.go`；Console/Panel safe projections | redaction、truncation、Normal/Diagnostic、ANSI/control tests | 无 raw JSON/stderr/hidden reasoning/Secret；单项和总量均有界 |
| §6 reducer 唯一状态 | `internal/consolemodel/{reducer,task_state}.go`；`internal/client/console/client.go` | version/terminal/cursor matrix、fuzz seed、replacement、Follow ack/cancel | 重复幂等，倒退/冲突拒绝；失败不 ack、不跨 cursor；仅 gap reattach |
| §7 focused 控制 | Console TUI/application；正式 authenticated client | exactly-once CAS、focused steer/cancel、stale refresh tests 与 E2E | 快捷控制取 reducer 最新 ID/version；断线禁用；不自动重放写入 |
| §8 同 pane Diagnostic | TUI mode-switch epoch；Panel SSE mode authorization | Normal/Diagnostic round-trip、forbidden fallback、old-stream/expiry tests | 先安全结束旧 Follow；失败保持 Normal；Normal 永不显示 Diagnostic |
| §9 有界资源与小终端 | TUI compact layout、bounded timeline/task detail | `80x5`/更小、resize/scroll/overlay、long output、race、三-pane PTY | View 不超过实际高度；滚动不被后台更新拉底；draft/focus 保持 |
| §10 默认用户工作流 | Console/Fleet 默认 profile；`workflow_integration_test.go` | login/Fleet/OAX Attach/dispatch/output/focused steer/Diagnostic/quit/第二 Task | 日常路径无需重复 `--db/--socket/--file/--credentials`；Worker 常驻 |
| 验收失败路径 | Panel SSE、task snapshot、credential/auth、Runtime parser | ownership/projection/encode/write、CAS、token、retention、socket、output faults | 查询/投影错误在 frame 前终止；无误报成功或 cursor 跳过 |
| 明确排除 | 正式 Control API、safeoutput、Fleet tmux 协调 | release scanner、ADR-006/007 diff、禁用命令与 TurnHandle scan | 无 ADR-006/007 语义、Foreground、Runtime TTY、Console TurnHandle 或 tmux 控制协议 |

冻结契约与逐 Task 设计证据见 [Task 01 contract](../../plans/2026-09-19-openagentx-adr-009-implementation/TASK-01-CONTRACT-FREEZE.md)
及 [ADR-009 implementation plan](../../plans/2026-09-19-openagentx-adr-009-implementation-plan.md)。默认路径用户命令链见
[README](../../../README.md) 与 [用户安装指南](../../operations/openagentx-user-install-guide.md)。

## 完整提交与范围审查

- `008b2e0..5bebf14` 共 17 个提交逐个执行 `git show --check`，全部通过；完整 diff 为 59 files、
  9100 insertions、353 deletions。
- ADR-006/007 无 diff；`go.mod`/`go.sum` 无 ADR-009 修改；没有生成物、credential/token 文件或父仓文件
  进入提交范围。
- secret-pattern、Console `TurnHandle`、production `send-keys`/`capture-pane`/`paste-buffer` 扫描通过；
  `/foreground` 仍仅为禁用提示。
- 代码、测试和文档均可归属 Task 01-07；未发现未说明的 compatibility fallback。主计划与 Task 08
  front matter 保持 `pending`，execution log 保持 `active/WAIT`，等待监督 gate。

## 自动化验证

| 命令或检查 | 结果 |
| --- | --- |
| `go test ./... -count=1` | PASS；31.55s；Console 30.131s、Panel 19.923s、Fleet 12.191s、SQLite 12.766s |
| `go test -race ./... -count=1` | PASS；46.00s；Panel 41.590s、Console 32.148s、SQLite 17.554s |
| `go vet ./...` | PASS；0.72s |
| `go mod verify` | PASS；all modules verified |
| 受影响包定向 race | PASS；42.68s；Panel/Console/Fleet/Auth/Runtime/safeoutput 全部通过 |
| `npm run test:observation` | PASS；4/4；0.39s |
| `npm run test:pwa` | PASS；0.29s |
| `npm run build` | PASS；266 modules；3.49s；首次缺依赖失败见下文 |
| `bash -n scripts/*.sh deploy/scripts/*.sh deploy/systemd/*.sh` | PASS |
| `bash deploy/systemd/openagentx-worker_template_test.sh` | PASS；system/user templates 静态约束通过 |
| `systemd-analyze --user verify <temp>/openagentx.service <temp>/openagentx-worker@.service` | PASS；0.08s；按实际安装名验证并清理 |
| `bash scripts/check-legacy-control-paths.sh --release` | PASS；全部类别 `CLEAN`；0.14s |
| built candidate Console/Fleet help smoke | PASS；help code 0；旧 status/`--once`/dispatch 均 code 2 |
| changed Markdown relative links 与 README/install-guide bash fences | PASS |
| ADR hash、ADR-006/007、secret/forbidden、完整 diff whitespace | PASS |

## 隔离集成与故障注入

| 场景 | 结果 |
| --- | --- |
| 默认 profile 完整用户闭环 | PASS；26.68s；临时 HOME/DB/UDS/credential、正式 PTY login、fake user-systemd、唯一 `tmux -L`、真实隔离 daemon/Worker |
| Console alt-screen 三-pane PTY | PASS；2.52s；绑定、输入 ready、`/quit` exit 0、pane 1/2 保留 |
| Task snapshot N/N+1 | PASS；Agent snapshot 与 Task snapshot 均含提交状态或从 cursor 可重放 |
| SSE cursor 故障恢复 | PASS；Task/Run/runtime/Worker/Mailbox/Message/Approval 归属，以及 Run/Worker/backend/encode/write 故障均不跨 cursor |
| CLI Token/schema/credential | PASS；digest-only、expiry/revoke/audience/scope、v1 reopen/rollback、permissions/symlink/flock/concurrency |
| Runtime 与 safeoutput | PASS；AGY/ACP/CodeBuddy/fake、empty/malformed/timeout/contradictory terminal、bounded diagnostic 与 redaction |
| reducer/TUI/CAS/Diagnostic | PASS；exactly once、stale refresh、Follow epoch、mode fallback、compact pane、终态 reply |
| Fleet/tmux/user-systemd/graceful | PASS；完整两包 11.11s；workspace/binding/dead-pane、canonical argv、`--user`、graceful/force 分离 |
| Console 退出后的 Worker 常驻 | PASS；同一 Worker PID/instance/generation 完成第二项 Task，无 tmux/Console 生命周期耦合 |

默认 profile 闭环中的三段 Runtime safe output 通过正式 `Client.Follow` 和 cursor 明确确认；PTY recorder
只负责可交互界面、绑定、输入和退出证据，没有把短暂终端重绘冒充完整事件历史。所有隔离进程、tmux
server、HOME、DB、UDS 和 credential fixture 均由测试清理，未接触默认 tmux 或真实运行状态。

## 失败与纠正

| 首次失败 | 根因 | 纠正与最终证据 |
| --- | --- | --- |
| `npm run build` 返回 `vite: not found` | feature worktree 没有 `web/node_modules` | `npm ci --no-audit --no-fund` 按 lock 安装 123 packages；只重跑 build 后 PASS |
| 首次 systemd 临时验证命令被安全策略拒绝 | cleanup 使用了递归删除形式 | 改为唯一 `mktemp -d`、精确 `unlink`/`rmdir`；unit verify PASS，未操作 manager |
| 首次禁用路径 ad-hoc scan 命中 `TurnHandle` | 错把 Worker/Runtime 合法接口纳入 Console 边界 | 收紧为 Console client/model 无 TurnHandle、Fleet/Console 无禁用 tmux 命令；release scanner 仍独立 PASS |
| linked worktree 构建无 `vcs.*` | submodule 共享 Git 配置保留原 `core.worktree`，Go 1.22 未写 VCS stamping | 删除两个无 provenance 产物；从 clean standalone clone 以 `-buildvcs=true` 重建，revision/modified 均符合 |
| `gio trash` 不支持 `/tmp` internal mount | 临时 clone 所在挂载不提供 Trash | 核验精确路径/HEAD/clean 后用限定路径清理；clone 与临时 worktree 均不存在 |

这些失败均保留为验证环境或审计命令纠正记录，没有通过重复运行掩盖产品失败，也没有修改产品代码。

## 候选构建与 provenance

构建命令：

```bash
go build -buildvcs=true \
  -o /home/sky/.cache/openagentx-builds/openagentx-adr009-5bebf14 \
  ./cmd/openagentx
```

构建源为 standalone clone 中 detached、clean 的
`5bebf148c414c2ca8bd8d2e6367f3e828b2ec2af`。最终 `go version -m` 关键字段：

```text
go1.22.4 linux/amd64
path openagentx/cmd/openagentx
mod  openagentx (devel)
vcs=git
vcs.revision=5bebf148c414c2ca8bd8d2e6367f3e828b2ec2af
vcs.modified=false
```

最终文件大小 19523320 bytes，SHA-256 为
`e482b113311ba618ac45512bf3098d431e739ab61bfc681ec3d2f90f483300c2`。候选目录与文件均为 `0700`，
owner/group 为 `sky:sky`。临时 linked worktree、standalone clone 和无 provenance probe binary 已清理，
只保留上述候选。

## 仓库与外部状态

| 对象 | 只读结果 | 说明 |
| --- | --- | --- |
| feature | 提交前 `5bebf14`；相对 `origin/main@c3fc1bb` ahead 52；无 remote feature ref | 本报告提交后只增加 docs-only validation commit |
| OpenAgentX main | clean `main@3723c7731076d3f8d4b6a31b65cbcb48394c7d8f`；相对 origin ahead 1 | 未修改、未 merge/push |
| ADR-008 worktree | clean `008b2e08923614182811023eddd652df69aa98a9` | ADR-009 权威起点未修改 |
| steadyflow 父仓 | `main@d3fd774`，存在大量既有 dirty/ahead 现场 | 只读检查；未修改、暂存或提交 |
| 真实运行环境 | 未检查 | Task 08 要求单独授权；本轮未读取 service/DB/socket/credential/default tmux/user-systemd |

## Open issues 与人工步骤

- 当前无 ADR-009 P0/P1 open issue，候选结论为 `passed-candidate`。
- Task 08 仍为 `active/WAIT`；本 docs-only validation commit 需监督复核，GO 后再以独立 gate record 同步
  主计划、Task 08 front matter 和 execution log 为 `completed/GO`。
- push feature、merge main、安装候选、schema/配置检查、服务重启和真实主机 E2E 均未执行，必须另行授权并
  记录源码 commit、binary SHA-256、schema、Worker identity/generation 和服务状态。
- ADR-009 不承诺 Adapter 不具备的 token/tool 细粒度过程；无持久化增量时，Console 正确显示运行状态并
  等待最终结果，不制造隐藏推理或伪进度。
