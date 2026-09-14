---
doc_type: validation_report
status: no-go
owner: openagentx
test_id: ADR-008-T08
validated_at: 2026-09-14
---

# OpenAgentX ADR-008 release candidate 验证

## 结论

`no-go`。`origin/main..f16496c9da33c2ee8013b6b61f719ddc358e3b41` 的 24 个提交和
87 个文件已完成范围盘点，未发现 ADR-006/007 决策正文、父仓文件、生成物或 Secret 混入；逐提交
whitespace 检查和完整 diff whitespace 检查通过。最终语义审查在正式 Observe SSE 路径发现
`T08-01`：Agent 事件归属查询失败会被误判为“不匹配”并推进 cursor，Run 安全投影查询失败也会
发送缺失 Run projection 的事件并推进 cursor。这会永久跳过尚未安全投影和应用的事件，违反
ADR-008 第 9 节的 last-applied cursor 与 fail-closed 不变量。

该缺陷归属 Task 03。按 Task 08 停止规则，本轮没有修改产品代码，也没有继续全量/race、候选构建、
隔离运行场景或目标主机只读核验；不得把未执行项写成候选通过。Task 03 需要独立 review-fix 和监督
复核后，Task 08 才能从同一验证矩阵重新开始。

## 基线与边界

| 项目 | 结果 |
| --- | --- |
| feature branch | `codex/adr008-implementation` |
| implementation HEAD | `f16496c9da33c2ee8013b6b61f719ddc358e3b41` |
| 比较基线 | `origin/main`，feature ahead 24 |
| 提交范围 | 24 commits；87 files；14582 insertions；1601 deletions |
| Task 08 写入边界 | 仅本报告与 `EXECUTION-LOG.md` |
| 外部状态边界 | 未部署、未安装、未重启、未 merge/push；未操作真实 DB/socket/default tmux/systemd/父仓 |
| 候选二进制 | 未构建；语义审查先行失败后按规则停止 |

## Traceability matrix

| ADR-008 条款 | 代码产物 | 测试/文档证据 | 审查结论 |
| --- | --- | --- | --- |
| §1 本地默认路径 | `internal/localprofile`；Console/Fleet/serve/admin 接线 | `profile_test.go`；Task 02 contract/gate | 已追踪；显式 override fail closed、resolver 无副作用 |
| §2 固定 `OAX` workspace | `internal/fleet/workspace.go`；user Worker unit | workspace fake/isolated tmux tests；Task 05/07 gate | 已追踪；旧 `agentx` 不迁移，pane 1+ 保留 |
| §3 Attach 前置检查 | `internal/fleet/binding.go` | binding tests；isolated tmux wrong-pane/missing-pane0 tests | 已追踪；仅 `OAX` pane 0，失败无 mutation |
| §4 Agent 解析与选择 | Console application/TUI；Console Agent options repository/API | application、TUI、pagination tests | 已追踪；显式参数、marker、鉴权列表顺序明确且列表不截断 |
| §5 显式绑定和重命名 | Task 05 binding service | fake mutation/compensation/TOCTOU 与真实 tmux binding tests | 已追踪；marker/name/control-plane 一致，重绑需确认 |
| §6 Console TUI 主入口 | `internal/cli/console/{command,application,tui}.go` | menu、login/logout、selector、PTY smoke tests | 已追踪；菜单项可执行，Foreground 项禁用 |
| §7 最终 CLI grammar | Console command parser；旧 REPL 删除 | `TestFinalConsoleGrammar`、help/非 TTY tests | 已追踪；无 `attach --once` 或旧行式控制入口 |
| §8 全屏 TUI | Bubble Tea/Bubbles/Lip Gloss 固定版本；bounded Timeline | pure Update/View、resize/scroll/overlay、safe rendering、PTY tests | 已追踪；alt-screen、固定输入、typed Msg/Cmd、无 JSON fallback |
| §9 一致 snapshot/cursor | SQLite snapshot transaction；Panel SSE；Console Follow/reducer ack | N/N+1、retention、reconnect、ack、backend projection tests | **阻断 `T08-01`**：部分 filter/Run projection 查询错误仍会跨过 cursor |
| §10 heartbeat 合并 | `internal/consolemodel/reducer.go` | generation/instance fencing、burst、replacement/backend tests | 已追踪；合法旧事件有明确 cursor 规则，当前状态按身份 fencing |
| §11 CLI Token | CLI token domain/service/repository；UDS-only auth mux；credential store | role/scope/expiry/revoke/schema/store/concurrency tests | 已追踪；DB 仅 digest，Token 绝对到期且 audience-bound |
| §12 Logout | Console shared application service | daemon unavailable、audience mismatch、replace/logout tests | 已追踪；匹配时远端 revoke，本地副本始终删除 |
| §13 Fleet | Fleet init/workspace/up/status/down/force；user-systemd verification | atomic config、canonical argv、graceful/force、dead-pane tests | 已追踪；T04-01/T05-01 已由 Task 07 证据关闭 |
| 验收不变量/非目标 | mux、safeoutput、workspace、正式 Control API | release scanner、scope/mux、tmux forbidden-command tests；Task 01 freeze | 范围审查未见 ADR-006/007、TurnHandle、tmux 权威身份或 foreground 实现 |

## 阻断证据

### T08-01：SSE 投影错误会跨过未应用事件

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

## 因 NO-GO 未执行

- `go test ./... -count=1`、`go test -race ./... -count=1`、`go vet ./...`、`go mod verify`；
- Task 08 隔离 tmux/PTY、临时 DB/UDS、fake user-systemd 场景；
- Web observation/PWA/build、release scanner、systemd analyze/static 和最终 secret scan；
- `/home/sky/.cache/openagentx-builds/` 独立候选构建、SHA-256 与 module/VCS provenance；
- `rtx4090` service/binary/DB/schema/socket/credential/Linger/Worker/OAX pane 只读核验。

这些项目不是失败后可选择忽略的证据。修复经独立 gate 后必须无缓存重跑完整 Task 08 矩阵。

## 失败历史与下一门禁

- `2026-09-14T23:18:02Z`：最终语义审查首次发现 `T08-01`，没有通过重复测试或兼容 fallback 掩盖。
- 当前结论仅表示候选不能进入最终 gate；不表示 ADR-008 已部署或实现已完成。
- 下一步必须由监督者授权 Task 03 独立 review-fix；修复提交与 gate 通过后，再恢复 Task 08。
- 仍需人工批准的发布步骤保持未执行：备份、安装、服务升级/重启、真实 `OAX` workspace 体验和
  正式 graceful drain。
