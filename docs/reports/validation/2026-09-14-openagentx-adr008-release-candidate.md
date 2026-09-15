---
doc_type: validation_report
status: passed-candidate
owner: openagentx
test_id: ADR-008-T08
validated_at: 2026-09-15
---

# OpenAgentX ADR-008 release candidate 验证

## 结论

`passed-candidate`。`codex/adr008-implementation@138b8d8e266783afe368a39d2235fe7cfbb8d979`
通过完整范围审查、无缓存 Go/race/vet、Web、systemd、release/security 检查、隔离 tmux/PTY、临时
DB/UDS/Auth/Control 闭环和目标主机只读核验。候选二进制从 clean detached standalone clone worktree
构建，`go version -m` 确认 `vcs.revision=138b8d8...` 且 `vcs.modified=false`。

首次 `no-go` 的 cursor 缺陷 `T08-01` 已由 Task 03 修复提交 `b26c9c4` 关闭；第二次 `no-go` 的
PTY/small-pane 缺陷 `T08-02` 已由 Task 06 修复提交 `442bc05`、`138b8d8` 关闭。两次失败、归因和
修复证据均保留。监督者本轮明确取消 Termux 作为最终门禁，本报告只声明当前 Linux/Ubuntu 主机和
隔离环境通过，不新增 Termux 结论。

本结论只表示候选可进入最终监督 gate，不表示 ADR-008 已部署。feature 未 push/merge，候选未安装，
真实 service、DB、socket、Worker unit 和 `OAX` workspace 均未修改。

## 基线与边界

| 项目 | 结果 |
| --- | --- |
| feature branch | `codex/adr008-implementation` |
| implementation HEAD | `138b8d8e266783afe368a39d2235fe7cfbb8d979` |
| 比较基线 | `origin/main@c3fc1bba8ddbae3eedace0c7a32537e2f47db307`；feature ahead 29 |
| 提交范围 | 29 commits；88 files；15398 insertions；1643 deletions |
| Task 08 写入边界 | 仅本报告与 `EXECUTION-LOG.md` |
| 外部状态边界 | 未部署、安装、重启、merge/push；真实 DB/socket/default tmux/systemd/父仓仅只读 |
| 候选二进制 | `/home/sky/.cache/openagentx-builds/openagentx-adr008-138b8d8` |
| 候选 SHA-256 | `cb99c6713e1076ca762271b2716d288d9b4bf46a7effffd66b258fc7b1c82f78` |
| 候选权限 | file `0755 sky:sky`；cache directory `0755 sky:sky` |
| module files | `go.mod` SHA-256 `efcf07c...ba5e8`；`go.sum` SHA-256 `0794e1e...1466` |

## Traceability matrix

| ADR-008 条款 | 代码产物 | 测试/文档证据 | 审查结论 |
| --- | --- | --- | --- |
| §1 本地默认路径 | `internal/localprofile`；Console/Fleet/serve/admin 接线 | resolver 正反测试；Task 02 contract/gate | flag/env/home 优先级、空/相对 override 和 path escape 均 fail closed；resolver 无副作用 |
| §2 固定 `OAX` workspace | `internal/fleet/workspace.go`；user Worker unit | fake/isolated tmux；Task 05/07 gate | 大小写敏感 `OAX`；旧 `agentx` 不迁移；pane 1+ 保留 |
| §3 Attach 前置检查 | `internal/fleet/binding.go` | wrong-pane/missing-pane0/conflict/TOCTOU tests | 仅 `OAX` pane 0；失败无 mutation，不暴露 window/pane handle |
| §4 Agent 解析与选择 | Console application/TUI；安全 Agent options API/repository | explicit/marker/selector、pagination >100 tests | 顺序固定；列表鉴权、分页且不扫描目录/tmux 身份 |
| §5 显式绑定和重命名 | Task 05 binding service | mutation compensation 与真实 tmux binding tests | marker/name/control-plane 一致；重绑需确认；冲突非破坏 |
| §6 Console TUI 主入口 | `internal/cli/console/{command,application,tui}.go` | menu/login/logout/selector；真实三-pane PTY smoke | `T08-02` 已关闭；alt-screen 启动、绑定、`/quit` exit 0、pane 1/2 保留 |
| §7 最终 CLI grammar | Console parser；旧 REPL 删除 | `TestFinalConsoleGrammar`、help/非 TTY smoke | 仅冻结命令；`attach --once`、`console status` 和旧行式控制入口拒绝 |
| §8 全屏 TUI | Bubble Tea v1.3.4、Bubbles v0.20.0、Lip Gloss v1.1.0 | pure Update/View、80x5/更小布局、scroll/overlay/ack/cleanup tests | 实际高度不溢出；输入稳定；bounded safe Timeline；无行式 fallback |
| §9 一致 snapshot/cursor | SQLite snapshot transaction；Panel SSE；Follow/reducer ack | N/N+1、retention、reconnect、projection failure recovery | `T08-01` 已关闭；所有 projection/encode/write 失败在 frame/cursor 前终止 |
| §10 heartbeat 合并 | `internal/consolemodel/reducer.go` | generation/instance fencing、burst/replacement/backend tests | 旧代/异 instance 不回退；heartbeat 默认不刷 Timeline |
| §11 CLI Token | CLI token domain/service/repository；UDS-only mux；credential store | expiry/revoke/scope/audience/schema/store/concurrency tests；临时 UDS 闭环 | DB 仅 digest；Token 绝对到期、可撤销、installation-bound；Web bearer 拒绝 |
| §12 Logout | Console shared application service | unavailable/mismatch/repeat tests；临时 UDS revoke 闭环 | 匹配时先远端 revoke；本地副本始终删除；撤销后稳定 401 |
| §13 Fleet | init/workspace/up/status/down/force；user-systemd verification | captured config、unit argv、graceful/force、dead-pane tests | canonical config/binary/unit fail closed；`--user`；T04-01/T05-01 已关闭 |
| 安全输出/非目标 | safeoutput、mux、正式 Control API、release scanner | scope/mux/Secret/legacy/tmux forbidden-command tests | 无 ADR-006/007 语义修改、Console TurnHandle、tmux 控制协议或 Foreground 实现 |

## 已关闭阻断历史

### T08-01：SSE 投影错误会跨过未应用事件

首次审查发现 Agent filter 和 Run projection 会把 repository/projection error 折叠成可跳过状态，并推进
`after`。`b26c9c4928d5c1cbdd971473cecb5b535a3d39dc` 将归属判断改为 `(matched, error)`，Worker
归属使用精确 `GetWorkerInstance`，Run/Worker/backend/safe-output/JSON/frame write 错误均在发送与推进
cursor 前终止。故障注入覆盖 Task、Run、runtime、Worker、Message、Approval 归属和 N/N+1 恢复；
监督独立复核和本轮 Task 03 定向重验均通过。

### T08-02：真实 PTY `/quit` 与紧凑 pane 不稳定

首次 smoke 在已经绑定并显示 `/quit` 后未可靠退出；`442bc05` 改为线程安全 terminal recorder，等待
真实输入 ready 后逐键输入并单独发送 Enter。随后窄 pane 暴露 `View` 强制至少 8 行、实际 pane 约 5
行的产品问题；`138b8d8` 改为严格按 `WindowSize` 分配 header/viewport/status/input 和 overlay，极小
窗口仍保留 Bubble Tea model，不引入 REPL fallback。

本机恢复门禁通过：三-pane PTY/compact tests `-count=10`、Console package `-count=3`、Console race
和全仓无缓存测试均为 exit 0。强断言仍包括 alt-screen、正式临时 UDS/auth、`scratch -> quote` 绑定、
pane 0 exit status 0 与 pane 1/2 保留。

## 自动化与隔离验证

| 命令/检查 | 结果 |
| --- | --- |
| 29 次逐提交 `git show --check`；`git diff --check origin/main..HEAD` | PASS；完整 diff whitespace clean |
| ADR-006/007、父仓路径、生成物、Secret 与禁用 tmux 控制扫描 | PASS；无范围混入；禁止词仅测试词表/运行时合法接口 |
| `go test ./... -count=1` | PASS；18.60s |
| `go test -race ./... -count=1` | PASS；39.58s |
| `go vet ./...` | PASS；0.74s |
| `go mod verify` | PASS；0.77s，all modules verified |
| `npm run test:observation`（`web/`） | PASS；4/4，0.51s |
| `npm run test:pwa`（`web/`） | PASS；0.33s |
| `npm run build`（`web/`） | PASS；266 modules，1.48s；`web/dist` 为 ignored 产物 |
| `bash -n scripts/*.sh deploy/scripts/*.sh deploy/systemd/*.sh` | PASS |
| `bash deploy/systemd/openagentx-worker_template_test.sh` | PASS；system/user templates 静态约束通过 |
| `systemd-analyze --user verify <temp>/openagentx.service <temp>/openagentx-worker@.service` | PASS；按实际安装名验证，临时目录已清理 |
| `bash scripts/check-legacy-control-paths.sh --release` | PASS；全部类别 CLEAN |
| Console PTY/compact 三项 `-count=10` | PASS；12.158s |
| `go test ./internal/cli/console -count=3` | PASS；3.744s |
| `go test -race ./internal/cli/console -count=1` | PASS；3.351s |
| Task 03 api/panel/client/reducer/sqlite/safeoutput 定向测试 | PASS；projection fault/reconnect 路径包含在内 |
| Task 04 auth/api/store/sqlite/migrations 定向测试 | PASS；token/schema/permission/audience/concurrency 路径包含在内 |
| Task 07 `internal/fleet ./internal/cli/fleet ./internal/worker` | PASS；10.364s/0.269s/1.572s |
| 唯一 `tmux -L` Console/Fleet workspace/binding/dead-pane tests | PASS；pane 1+、冲突、dead-pane、退出边界成立 |
| 最终 Console help/legacy smoke | PASS；help 无旧命令；`console status` 与 `attach --once` 均 code 2 |

### 临时 HOME/DB/UDS 正式闭环

候选二进制在唯一 `/tmp/openagentx-adr008-t08-e2e.*` 下通过 public CLI 初始化 owner 和 Agent，启动真实
daemon/SQLite/UDS；随后完成 CLI login/session、完整 Agent options、normal/diagnostic Attach、正式
dispatch、SSE cursor `6 -> 7`、重连 `7 -> 8`、logout 远端 revoke、本地 credential 删除和撤销后
`401 CLI_UNAUTHENTICATED`。credential/lock 为 `0600`、profile directory 为 `0700`；DB 有一条 token
digest，原 token 搜索为 0，logout 后 revoked row 为 1。所有临时进程和目录均已清理。

闭环首次 login 因测试夹具把 `.openagentx` 父目录建成 `0755` 被 credential store 正确拒绝；修正夹具为
`0700` 后通过。额外 cancel 尝试针对 queued、无 active Run 的 Task，正式 API 以 400 fail closed；
本轮 dispatch 闭环和既有 active-Run cancel 集成测试共同覆盖控制路径，没有修改 ADR-006 Task 语义。

## 候选构建与 provenance

构建命令：

```bash
go build -buildvcs=true \
  -o /home/sky/.cache/openagentx-builds/openagentx-adr008-138b8d8 \
  ./cmd/openagentx
```

首次从 submodule linked worktree 构建时，Go 1.22 未写入 `vcs.*`，该产物判无效。随后从同一
`138b8d8` 的 clean detached standalone clone worktree 重建并覆盖无效产物；最终 `go version -m`
确认：

```text
go1.22.4 linux/amd64
path openagentx/cmd/openagentx
mod  openagentx (devel)
vcs=git
vcs.revision=138b8d8e266783afe368a39d2235fe7cfbb8d979
vcs.modified=false
```

最终文件大小 `18676856` bytes，SHA-256 为
`cb99c6713e1076ca762271b2716d288d9b4bf46a7effffd66b258fc7b1c82f78`。linked worktree、detached
clone 和 probe binary 均已安全清理，只保留候选文件。

## rtx4090 只读现场

| 对象 | 只读结果 | 候选影响 |
| --- | --- | --- |
| main/remote | main 与 `origin/main` 均为 `c3fc1bb` 且 clean；remote 无 feature branch | 符合未 push/merge 边界 |
| feature | `138b8d8`，ahead 29，文档修改前 clean | 与候选 revision 一致 |
| steadyflow 父仓 | 既有 dashboard/quote/docs/luqiyuan dirty 现场；`M OpenAgentX` 为 submodule index 差异 | 未修改、未提交；发布时继续保护 |
| daemon service | user `openagentx.service` enabled、active/running、`NRestarts=0` | 健康；仍运行旧 installed binary |
| ExecStart | `~/.local/bin/openagentx serve --db ~/.openagentx/data/openagentx.db --socket ~/.openagentx/run/openagentx.sock ...` | 路径符合现有 user service；未重启 |
| installed binary | SHA-256 `3f84b06c...a29b`，`0755 sky:sky` | 与候选不同，符合“未安装”预期 |
| real DB | `PRAGMA query_only=ON`；`quick_check=ok`；schema version 1 | 未迁移；当前尚无 CLI token/installation tables，部署前需备份 |
| socket/profile | UDS health `{"status":"ok"}`；socket `0600`；data/run/workers/identities `0700`；config `0600` | 权限符合既有安装；credentials 尚不存在 |
| Linger/Worker unit | `Linger=yes`；user/system 均无已安装 `openagentx-worker@` unit | Task 07 unit 尚未安装，需人工发布步骤 |
| real tmux | `OAX:agentx.2` live，current command `codex`；ADR-008 markers 不存在 | 旧现场未改；不得自动迁移，发布后需显式协调 |

## 失败历史与人工步骤

- `2026-09-14T23:18:02Z`：`T08-01` 首次使 Task 08 `no-go`；没有绕过 cursor 缺陷。
- `2026-09-14T23:52:33Z`：`T08-02` 再次使 Task 08 `no-go`；随后由两个 Task 06 独立提交修复。
- systemd 验证首次错误设置 temp-only `SYSTEMD_UNIT_PATH`，隐藏系统 `basic.target` 并 exit 1；去除该
  夹具覆盖、保留临时安装名后 exit 0，不是 unit 产品失败。
- 候选首次缺 `vcs.*`、临时 credential 目录权限错误和 cancel 无 active Run 的结果均按上文保留；
  没有用偶然重跑或兼容 fallback 掩盖。

仍需监督者/人工明确批准后执行：push feature、merge main、备份真实 DB/profile、安装候选及 Web/unit、
daemon-reload/restart、验证 schema ensure、执行 `console login`、安装/启动 user Worker、显式协调真实
`OAX` workspace，以及对 busy Worker 做正式 graceful drain（force-stop 仍为独立危险路径）。
