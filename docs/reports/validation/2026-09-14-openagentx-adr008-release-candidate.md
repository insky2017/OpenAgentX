---
doc_type: validation_report
status: no-go
owner: openagentx
test_id: ADR-008-T08
validated_at: 2026-09-14
---

# OpenAgentX ADR-008 release candidate 验证

## 结论

`no-go`。首次审查在 `f16496c` 发现的 cursor 安全缺陷 `T08-01` 已由 Task 03 独立修复提交
`b26c9c4928d5c1cbdd971473cecb5b535a3d39dc` 关闭，监督复核确认归属、Run、Worker、Backend、编码和
写出失败均不跨过 cursor。恢复 Task 08 后，重新审查 `origin/main..b26c9c4` 的 26 个提交和 88 个
文件，未发现 ADR-006/007 决策正文、父仓文件、生成物或 Secret 混入；逐提交和完整 diff whitespace
检查通过。

随后首次无缓存 `go test ./... -count=1` 在真实 PTY/tmux smoke
`TestIsolatedTTYSmokeUsesAltScreenBindsAndPreservesExtraPanes` 失败：Console 已进入 alt-screen、绑定
`quote` 并收到 `/quit`，但 pane 0 在 10 秒观察窗内仍为 live。该失败登记为 `T08-02`，归属 Task 06。
按 Task 08 硬停止规则，本轮未重跑该测试，也未继续 race、剩余隔离矩阵、候选构建或目标主机只读
核验；当前不能形成 release candidate。

## 基线与边界

| 项目 | 结果 |
| --- | --- |
| feature branch | `codex/adr008-implementation` |
| implementation HEAD | `b26c9c4928d5c1cbdd971473cecb5b535a3d39dc` |
| 比较基线 | `origin/main`，feature ahead 26 |
| 提交范围 | 26 commits；88 files；15094 insertions；1643 deletions |
| Task 08 写入边界 | 仅本报告与 `EXECUTION-LOG.md` |
| 外部状态边界 | 未部署、未安装、未重启、未 merge/push；未操作真实 DB/socket/default tmux/systemd/父仓 |
| 候选二进制 | 未构建；无缓存全仓测试失败后按规则停止 |

## Traceability matrix

| ADR-008 条款 | 代码产物 | 测试/文档证据 | 审查结论 |
| --- | --- | --- | --- |
| §1 本地默认路径 | `internal/localprofile`；Console/Fleet/serve/admin 接线 | `profile_test.go`；Task 02 contract/gate | 已追踪；显式 override fail closed、resolver 无副作用 |
| §2 固定 `OAX` workspace | `internal/fleet/workspace.go`；user Worker unit | workspace fake/isolated tmux tests；Task 05/07 gate | 已追踪；旧 `agentx` 不迁移，pane 1+ 保留 |
| §3 Attach 前置检查 | `internal/fleet/binding.go` | binding tests；isolated tmux wrong-pane/missing-pane0 tests | 已追踪；仅 `OAX` pane 0，失败无 mutation |
| §4 Agent 解析与选择 | Console application/TUI；Console Agent options repository/API | application、TUI、pagination tests | 已追踪；显式参数、marker、鉴权列表顺序明确且列表不截断 |
| §5 显式绑定和重命名 | Task 05 binding service | fake mutation/compensation/TOCTOU 与真实 tmux binding tests | 已追踪；marker/name/control-plane 一致，重绑需确认 |
| §6 Console TUI 主入口 | `internal/cli/console/{command,application,tui}.go` | menu、login/logout、selector、PTY smoke tests | **阻断 `T08-02`**：真实 PTY `/quit` 后 pane 0 未在观察窗内退出 |
| §7 最终 CLI grammar | Console command parser；旧 REPL 删除 | `TestFinalConsoleGrammar`、help/非 TTY tests | 已追踪；无 `attach --once` 或旧行式控制入口 |
| §8 全屏 TUI | Bubble Tea/Bubbles/Lip Gloss 固定版本；bounded Timeline | pure Update/View、resize/scroll/overlay、safe rendering、PTY tests | 静态追踪完成；候选级 PTY 退出证据因 `T08-02` 未通过 |
| §9 一致 snapshot/cursor | SQLite snapshot transaction；Panel SSE；Console Follow/reducer ack | N/N+1、retention、reconnect、ack、backend projection/failure tests | `T08-01` 已由 `b26c9c4` 修复并经监督关闭；所有投影失败在 frame/cursor 前终止 |
| §10 heartbeat 合并 | `internal/consolemodel/reducer.go` | generation/instance fencing、burst、replacement/backend tests | 已追踪；合法旧事件有明确 cursor 规则，当前状态按身份 fencing |
| §11 CLI Token | CLI token domain/service/repository；UDS-only auth mux；credential store | role/scope/expiry/revoke/schema/store/concurrency tests | 已追踪；DB 仅 digest，Token 绝对到期且 audience-bound |
| §12 Logout | Console shared application service | daemon unavailable、audience mismatch、replace/logout tests | 已追踪；匹配时远端 revoke，本地副本始终删除 |
| §13 Fleet | Fleet init/workspace/up/status/down/force；user-systemd verification | atomic config、canonical argv、graceful/force、dead-pane tests | 已追踪；T04-01/T05-01 已由 Task 07 证据关闭 |
| 验收不变量/非目标 | mux、safeoutput、workspace、正式 Control API | release scanner、scope/mux、tmux forbidden-command tests；Task 01 freeze | 范围审查未见 ADR-006/007、TurnHandle、tmux 权威身份或 foreground 实现 |

## 阻断证据

### T08-01：SSE 投影错误会跨过未应用事件（已关闭）

1. `internal/api/panel/handler.go:1128` 调用 `eventMatchesAgent`。该函数只返回 `bool`；
   `GetTask`、`GetRunAttempt`、`ListWorkers`、`GetMessage`、`GetApprovalRequest` 等任一查询错误均被折叠为
   `false`。调用方随后在 `handler.go:1129` 将 `after` 直接推进到该事件 sequence。
2. `internal/api/panel/handler.go:1147` 调用 `runEventSnapshot`。该函数在 Run/Worker 查询错误或投影无效时
   返回 `nil`，调用方仍在 `handler.go:1153-1154` 发送 SSE frame 并推进 `after`。
3. 现有 `TestSSEBackendProjectionFailureDoesNotSendOrCrossWorkerEvent` 只覆盖 Worker Backend 查询失败；
   Agent filter 查询失败和 Run projection 查询失败没有对应的“不得发送、不得跨过”故障注入测试。
4. 结果是客户端成功应用的 cursor 可能越过未能归属或未能构造安全状态投影的事件；普通重连从该
   cursor 之后开始，无法再恢复被跳过的状态变化。

所需修复边界属于 Task 03：区分“合法不匹配”和“查询/投影失败”；失败时结束当前 stream，保持
last-applied cursor；Run projection 必须与 Worker Backend projection 一样返回结构化错误并阻止发送。
至少补 Agent filter 各关联查询失败、Run/Worker projection 失败及后续 N+1 不被跨过的回归测试。

修复提交 `b26c9c4928d5c1cbdd971473cecb5b535a3d39dc` 将归属判断改为 `(matched, error)`，Worker
归属使用精确查询，Run/Worker/Backend/安全输出/JSON/frame write 错误均在发送与推进 cursor 前终止；
故障注入证明从 last-applied cursor 可恢复 N 和 N+1。监督于恢复 Task 08 时确认该项通过并关闭。

### T08-02：真实 PTY `/quit` 未可靠结束 Console pane

1. 无缓存全仓测试中的真实隔离 smoke 已进入 alt-screen，正式 Attach 完成 `scratch -> quote` 绑定，
   terminal 证据包含 `> /quit`，pane 1/2 仍存在。
2. 测试随后连续 10 秒读取隔离 `tmux -L` 的 pane 状态，最终仍为 `0:1:`、`1:0:`、`2:0:`；pane 0
   没有变成预期的 dead+exit 0 状态，因此失败。
3. 该问题可能是 TUI quit、Follow shutdown 或 PTY/tmux 退出协调的确定性缺口。候选验证不能以再次
   单独重跑偶然通过作为证据，也不能在 Task 08 docs-only 提交中修产品或测试。

所需修复边界归属 Task 06：定位 `/quit` 后程序和 Follow goroutine 的退出状态机，提供不会泄漏
goroutine、不会停止 Worker、并在 Ubuntu/Termux 真实 PTY 下确定性退出的回归证据。

## 已执行检查

| 命令/检查 | 结果 |
| --- | --- |
| `git status --short --branch`、`git rev-parse HEAD`、`git rev-list --count origin/main..HEAD` | PASS；基线 `f16496c`、ahead 24，开始时 clean |
| `git log --reverse origin/main..HEAD` | PASS；24 个 Task 01-07 实现/fix/gate 提交顺序完整 |
| `git diff --name-status origin/main..HEAD`、`git diff --stat origin/main..HEAD` | PASS；87 文件范围已盘点 |
| `git diff --check origin/main..HEAD`、逐提交 `git show --check` | PASS |
| ADR-006/007 与 `docs/decisions` diff 检查 | PASS；无决策正文修改 |
| 新增行禁用 tmux 控制扫描 | PASS；仅测试禁止词清单和禁用 Foreground 文案命中 |
| SSE cursor/projection 语义审查 | FAIL；发现 `T08-01` |
| 监督复核 `b26c9c4` | PASS；关闭 `T08-01`，授权恢复完整 Task 08 矩阵 |
| `git status --short --branch`、HEAD/ahead、26 commits/88 files 重审 | PASS；开始时 clean，`b26c9c4`，ahead 26 |
| 26 次逐提交 `git show --check`、`git diff --check origin/main..HEAD` | PASS |
| ADR-006/007、生成物、禁用 tmux 控制和 Secret 差异扫描 | PASS；测试占位密码仅存在于测试输入，不是凭据 |
| `go mod verify` | PASS；1.03s，all modules verified |
| `npm run test:observation`（`web/`） | PASS；4/4，0.65s |
| `npm run test:pwa`（`web/`） | PASS；0.36s |
| `npm run build`（`web/`） | PASS；266 modules，1.48s；`web/dist` 为已忽略产物 |
| `bash scripts/check-legacy-control-paths.sh --release` | PASS；全部类别 CLEAN，0.14s |
| `go test ./... -count=1` | **FAIL**；17.94s；真实 PTY smoke 的 pane 0 未可靠退出，登记 `T08-02` |

## 首轮 NO-GO 未执行

- `go test ./... -count=1`、`go test -race ./... -count=1`、`go vet ./...`、`go mod verify`；
- Task 08 隔离 tmux/PTY、临时 DB/UDS、fake user-systemd 场景；
- Web observation/PWA/build、release scanner、systemd analyze/static 和最终 secret scan；
- `/home/sky/.cache/openagentx-builds/` 独立候选构建、SHA-256 与 module/VCS provenance；
- `rtx4090` service/binary/DB/schema/socket/credential/Linger/Worker/OAX pane 只读核验。

这些项目不是失败后可选择忽略的证据。修复经独立 gate 后必须无缓存重跑完整 Task 08 矩阵。

## 第二轮 NO-GO 后停止的项目

- `go test -race ./... -count=1`、`go vet ./...`；
- Task 03 cursor 故障、Task 04 token/schema/store、Task 07 Fleet/user-systemd/graceful 的候选级独立重验；
- 全部唯一 `tmux -L` workspace/binding/dead-pane 与单独 PTY 场景；
- shell/systemd static/analyze 和最终 diff/status 检查；
- detached clean worktree 候选构建、SHA-256、`go version -m` provenance；
- `rtx4090` service/binary/DB/schema/socket/credential/Linger/Worker/OAX pane 只读核验。

未执行这些项目是测试失败后的预期停止行为，不是通过或豁免。

## 失败历史与下一门禁

- `2026-09-14T23:18:02Z`：最终语义审查首次发现 `T08-01`，没有通过重复测试或兼容 fallback 掩盖。
- `T08-01`：由 `b26c9c4` 修复并经监督复核关闭；首次 no-go 证据保留。
- `2026-09-14T23:52:33Z`：恢复矩阵后的首次无缓存全仓测试发现 `T08-02`；没有重跑失败项，按规则停止。
- 当前结论仅表示候选不能进入最终 gate；不表示 ADR-008 已部署或实现已完成。
- 下一步必须由监督者决定是否授权 Task 06 独立 review-fix；通过后 Task 08 仍需从完整矩阵重新开始。
- 仍需人工批准的发布步骤保持未执行：备份、安装、服务升级/重启、真实 `OAX` workspace 体验和
  正式 graceful drain。
