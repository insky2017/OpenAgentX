---
doc_type: implementation_log
status: active
owner: openagentx
updated_at: 2026-09-14
---

# ADR-008 持续执行记录

> 本文件是 ADR-008 实施过程的唯一持续记录。它不预写成功结论。执行者必须在每个动作发生后追加事实、命令和结果；失败与纠正不得删除或改写成“从未发生”。最终结论由独立 validation report 给出。

## 0. 记录规则

1. 开始一个任务前，将任务表对应状态从 `pending` 改为 `active`，填写开始时间、基线 SHA 和执行者。
2. 每次修改后记录文件范围；每次验证后记录完整命令、退出码和关键摘要。不得只写“tests passed”。
3. 命令输出含 token、password、Secret、用户隐私或未脱敏 runtime 数据时不得粘贴；只记录脱敏摘要和证据文件 hash。
4. 失败记录只能追加后续“已纠正”条目，不能删除原失败；未解决问题进入 Open Issues。
5. 每个 Task 保持一个实现提交，包含该阶段实现、测试和提交前执行记录；Git 提交无法自包含
   自身 SHA，因此阶段提交后只读核验精确 SHA/status 并停止，不 amend 已提交阶段。
6. 监督者复核后，以独立 docs-only gate record 提交记录实现提交精确 SHA、实际 post-commit
   status 和 `GO`/`NO-GO`，并同步计划状态；gate record 不得修改产品行为或启动下一任务。
7. 未经监督者对下一任务明确 `GO`，下一任务不得标记为 active。
8. 外部状态变化（service、socket、DB、tmux、installed binary、remote branch）必须单列；按计划不应发生的变化一经发现立即停止。

## 1. 计划发布基线

| 项 | 值 |
|---|---|
| ADR 文档提交 | `cb521aaad2470024255c6f870032837dd3d13538` |
| 计划 worktree | `/tmp/openagentx-adr008-worktree` |
| 计划 branch | `adr008-docs` |
| 计划提交 | `16291c731c6f8ce930436e4a6a8e45c2804e595a` (`docs: plan ADR-008 implementation`) |
| 计划发布状态 | 已通过 GitHub SSH 推送；实施开始时再次确认 `origin/main` 包含本计划 |
| 目标实现主机 | `rtx4090` |
| 目标仓库 | `/home/sky/work/touzi/OneAxe/steadyflow/OpenAgentX` |
| Codex pane | `OAX:agentx.2`（计划发布时位置；实施不得依赖该 window 作为身份） |

### 计划发布时受保护现场

```text
 M README.md
M  deploy/systemd/openagentx-user.service
 M internal/worker/runner_test.go
?? docs/operations/openagentx-user-install-guide.md
```

处理规则：只能由原执行 Codex 复核后独立提交，或暂停请示；不得 reset/stash/clean/覆盖，不得混入 ADR-008 阶段提交。

## 2. 总体状态

| Task | 名称 | 状态 | 实现提交 | 监督门禁 |
|---|---|---|---|---|
| P0 | 受保护现场独立收口 | completed | `c3fc1bba8ddbae3eedace0c7a32537e2f47db307` | GO |
| 01 | 基线隔离、契约冻结与测试地图 | completed | `35bd44564773882cfedefb31fad0afd64c0514e4` | GO |
| 02 | 默认路径与 CLI 表面 | completed | `e690bbbd11046e63841a4a869a90f6aeb42c5575` + fix `c0e8d4aeae2516c005cedbce6c5b35d1b8b07553` | GO |
| 03 | 一致 Attach cursor 与代际 reducer | completed | `4aea5a6291b47792b1c69256ae0ae6422a890cb4` + fix `9e18246dc4f61ee8ccc044509229b13b0e191f9d` + fix `2f9772753b3a75e6304abd76c4eb6a259c9e0c6f` | GO |
| 04 | 可撤销 CLI Token 会话 | completed | `c240aa4dbd47565181d22f602ea3203e5fbfe4dc` + fix `0a5e85987f0c9bd29275ece138200083be13171e` | GO |
| 05 | `OAX` workspace 与非破坏绑定 | completed | `c9339bb8c8cfa35a0a2bbd74608d273eb00bd2f6` + fix `e40e28bed11abc9789c143977363e601f067e4d3` + test `5468ffeb634ee5a4aed5577fbea5c1201a591cce` | GO |
| 06 | Console 主菜单、Agent selector 与全屏 TUI | completed | `4b5d2c0675a9b00f6d48e52395710b2639b8acac` + fix `5aa6c973abd864a3c7e80b41f4bdc422600d002c` + fix `c42414b7a21b98bd35ab0de3949778d591705709` | GO |
| 07 | Fleet、user-systemd 与默认 profile 集成 | completed | `40fe06e813dcce5c327cf159c66fff630d3bc236` + fix `f5c0d0d549931e4104bc7248c5e5714305a242d9` + fix `e13db7b7e505cb77c1359c776b9c3c473322062f` | GO |
| 08 | 集成审查、实机候选与发布门禁 | active | — | WAIT |

允许状态：`pending`、`active`、`blocked`、`completed`。监督门禁只允许：`WAIT`、`GO`、`NO-GO`。

## 3. 前置现场收口 P0

### 开始信息

- 执行者：Codex
- 开始时间（UTC）：动作开始时刻未单独采集；可追溯基线提交时间为 `2026-09-14T14:17:06Z`
- 主工作树 HEAD/branch：`603abf388a9bda621fc7c20a1640807bc2e2dcee` / `main`
- `git status --short --branch`：

```text
## main...origin/main
 M README.md
M  deploy/systemd/openagentx-user.service
 M internal/worker/runner_test.go
?? docs/operations/openagentx-user-install-guide.md
```

### 既有变更审查

| 文件 | 原任务意图 | staged | 审查结论 | 处理 |
|---|---|---:|---|---|
| `README.md` | 增加用户级安装指南入口 | no | 完整、链接目标存在，未混入 ADR-008 行为 | 前置提交 |
| `deploy/systemd/openagentx-user.service` | 将 daemon 改为 `%h/.local/bin`、`%h/.openagentx` 和可覆盖 HTTP 地址的用户级 unit | yes | 完整，systemd verify 通过 | 前置提交 |
| `internal/worker/runner_test.go` | 修复 network probe clone 共享测试状态时的 mutex copy 问题 | no | 完整，Worker/全量 Go 测试通过 | 前置提交 |
| `docs/operations/openagentx-user-install-guide.md` | 记录安装、初始化、user service、Fleet 和回滚流程 | untracked | 完整，命令与当前实现边界一致 | 前置提交 |

### 验证与提交

| 时间 UTC | 命令 | 退出码 | 脱敏结果/证据 |
|---|---|---:|---|
| P0 收口期间 | `go test ./...` | 0 | 全部 Go package 通过 |
| P0 收口期间 | `go vet ./...` | 0 | 无新增诊断 |
| P0 收口期间 | `npm run test:observation` | 0 | observation 测试通过 |
| P0 收口期间 | `npm run test:pwa` | 0 | PWA 测试通过 |
| P0 收口期间 | `npm run build` | 0 | Web production build 通过 |
| P0 收口期间 | `go build -buildvcs=false -o /tmp/openagentx-p0-603abf3 ./cmd/openagentx` | 0 | 临时二进制构建成功，未安装 |
| P0 收口期间 | `./scripts/check-legacy-control-paths.sh --release` | 0 | release scanner 通过 |
| P0 收口期间 | `git diff --check` | 0 | 无 whitespace error |
| P0 收口期间 | `systemd-analyze --user verify ~/.config/systemd/user/openagentx.service` | 0 | 当前 user unit 语法验证通过；只读，无 restart |

- 前置提交 SHA：`c3fc1bba8ddbae3eedace0c7a32537e2f47db307`
- 推送目标与结果：`main -> origin/main` 成功，`603abf3..c3fc1bb`；随后
  `git ls-remote` 确认 `refs/heads/main=c3fc1bba8ddbae3eedace0c7a32537e2f47db307`
- 收口后工作树状态：`## main...origin/main`，无 path 条目
- 只读运行状态：`openagentx.service` 为 active/enabled，`NRestarts=0`；HTTP/UDS health
  均返回 `{"status":"ok"}`。未安装、重启或修改真实 DB/socket/service。
- 监督者结论：本轮指令明确允许 P0 完整后进入 Task 01；P0 `GO`

## 4. 实现环境

仅在 P0 通过后填写。

| 项 | 值 |
|---|---|
| implementation branch | `codex/adr008-implementation`，tracking `origin/main`，未 push |
| implementation worktree | `/home/sky/work/touzi/OneAxe/OpenAgentX-adr008-worktree` |
| baseline SHA | `c3fc1bba8ddbae3eedace0c7a32537e2f47db307` |
| origin/main SHA | `c3fc1bba8ddbae3eedace0c7a32537e2f47db307` |
| Go / Node / npm | `go1.22.4` / `v20.19.4` / `10.8.2` |
| tmux / systemd | `tmux 3.4` / `systemd 255` |
| TUI framework/version/license | Bubble Tea `v1.3.4` + Bubbles `v0.20.0` + Lip Gloss `v1.1.0`; all MIT, all `go 1.18` |
| 临时测试根目录约定 | `/tmp/openagentx-adr008-taskNN-*`；tmux 仅 `tmux -L openagentx-adr008-*`；Task 01 probe 为 `/tmp/openagentx-tui-probe.0br9AZ` |

## 5. 契约冻结记录（Task 01）

| 契约 | 冻结结论 | 代码边界 | 测试边界 |
|---|---|---|---|
| 默认路径与环境优先级 | flag > resource env > `OPENAGENTX_HOME` > `~/.openagentx`；空/相对显式 override fail closed | 单一 `internal/localprofile` resolver | 全资源优先级、home 隔离、path escape、调用方接线 |
| Console CLI grammar | 最终仅 `console`、`login`、`logout`、`attach [--agent] [--diagnostic]`；Attach 无非交互 fallback | Task 02 parser，Task 06 原子替换旧入口 | TTY/非 TTY、退出码、未知参数、禁用 foreground |
| Attach snapshot/cursor/reconnect | 单一 `snapshot_sequence`；同一 SQLite 读取事务；重连从 last-applied，gap 为 `409 EVENT_CURSOR_EXPIRED` 后 reattach | SQLite snapshot repository + official Observe SSE + pure reducer | N/N+1 竞态、重复/倒退/gap、generation/WorkerInstance、heartbeat |
| CLI Token endpoint/scope/expiry | UDS-only `/api/auth/v1/cli/{login,session,logout}`；4 scopes；opaque digest-only；最长 30 天 | CLI auth handler 只注册 `unixMux`，Web cookie+CSRF 不变 | role/scope/expiry/revoke/audience、Web 回归、credential permissions |
| tmux marker/conflict/binding | session `OAX`；window options `@openagentx_managed=1`、`@openagentx_agent_id`；pane `0` 稳定且保留其他 pane | structured tmux runner + 单一 bind service | 无 session/错误 pane/多 pane/重排/重复 marker/partial failure/隔离 server |
| TUI model/I/O boundary | Bubble Tea model/update/view；Bubbles textarea/viewport/list；Lip Gloss overlay；I/O 全部 typed Cmd/Msg 注入 | Task 06 TUI package；client/tmux/store/clock/terminal interfaces | message sequence、input stability、resize/overlay/reconnect/terminal restore |
| schema v1 compatible upgrade | `CurrentVersion=1`；target schema 与事务化幂等 `ensureCLITokenTables` 同时更新 | migrations `Apply` 在 `ValidateCurrent` 前升级 | 空库、既有 v1 reopen、重复、故障回滚、required objects |

完整字段、环境变量、状态机、五个强制设计问题答案和 Task 02-08 测试地图见
[`TASK-01-CONTRACT-FREEZE.md`](TASK-01-CONTRACT-FREEZE.md)。

## 6. 阶段执行条目

每个 Task 复制一份以下模板，按时间追加，不删除历史条目。

### Task 01 — 基线隔离、契约冻结与测试地图

#### 开始

- 状态：completed
- 执行者：Codex
- 开始时间（UTC）：`2026-09-14T14:31:31Z`（feature worktree metadata）
- 基线提交：`c3fc1bba8ddbae3eedace0c7a32537e2f47db307`
- 监督者放行依据：本轮用户明确授权只执行 P0 和 Task 01
- 计划文件：`01-baseline-isolation-and-contract-freeze.md`

#### 变更

| 时间 UTC | 文件/package | 变更目的 | 范围偏差 |
|---|---|---|---|
| `2026-09-14T14:39:14Z` | `docs/plans/.../TASK-01-CONTRACT-FREEZE.md` | 记录基线、冻结七类契约、回答设计问题、建立测试地图 | none；仅文档，无产品行为修改 |
| `2026-09-14T14:39:14Z` | `docs/plans/.../EXECUTION-LOG.md` | 记录 P0、环境、Task 01 命令/结果/失败/外部状态 | none |

#### 决策

| ID | 决策 | 依据 | 是否需 ADR/监督确认 |
|---|---|---|---|
| D-01-01 | path env 名、单一 resolver 和 fail-closed 优先级按契约文档第 2 节冻结 | ADR-008 §1、Task 02 明确要求 | 已接受 ADR 范围；Task 02 前仍需监督 `GO` |
| D-01-02 | Attach 只增加单一 `snapshot_sequence`，由 SQLite 单事务取得 | ADR-008 §9、避免双 cursor 漂移 | 已接受 ADR 范围；Task 03 前仍需监督 `GO` |
| D-01-03 | CLI bearer 只挂 UDS mux，scope 按 viewer/operator/owner 分层 | ADR-008 §11-12、现有 Web cookie+CSRF 边界 | 已接受 ADR 范围；Task 04 前仍需监督 `GO` |
| D-01-04 | tmux 使用 `OAX` 和两个 window option，pane 存在性单独查询 | ADR-008 §2-5、禁止 index/pane-id 身份 | 已接受 ADR 范围；Task 05 前仍需监督 `GO` |
| D-01-05 | 固定 Bubble Tea `v1.3.4`、Bubbles `v0.20.0`、Lip Gloss `v1.1.0` | Go 1.22.4 compile probe、MIT、viewport/input/list/overlay 组合 | 已冻结；Task 06 前仍需监督 `GO` |

#### 验证

| 时间 UTC | 命令 | 退出码 | 耗时 | 脱敏结果/证据 |
|---|---|---:|---:|---|
| Task 01 探索期 | `go env GOPROXY GOMODCACHE GOVERSION` | 0 | <1s | Go `1.22.4`；默认 module proxy 为 `goproxy.io,direct` |
| Task 01 探索期 | `GOTOOLCHAIN=local GOPROXY=https://proxy.golang.org,direct go mod download -json <candidate>` | 0/1 | <2s | Bubble Tea `v1.3.4`、Bubbles `v0.20.0`、Lip Gloss `v1.1.0` 均为 `go 1.18`/MIT；Bubbles `v0.21.0` 因要求 Go 1.23 被预期拒绝 |
| Task 01 探索期 | `GOTOOLCHAIN=local GOPROXY=https://proxy.golang.org,direct go get -t <fixed-packages> && go test -run '^$' <fixed-packages>` | 0 | 1.6s | Bubble Tea、textarea、viewport、list、Lip Gloss 全部 compile probe 通过；仅临时 module |
| `2026-09-14T14:47:33Z` | `go test ./internal/cli/console ./internal/client/console ./internal/api/console ./internal/fleet ./internal/cli/fleet` | 0 | 2.76s | 5 个 package 全部通过 |
| `2026-09-14T14:47:33Z` | `go test ./internal/auth/... ./internal/persistence/sqlite/...` | 0 | 12.09s | `internal/auth/web`、`internal/persistence/sqlite`、`migrations` 全部通过 |
| `2026-09-14T14:47:33Z` | `git diff --check` | 0 | <0.01s | 无 whitespace error |

#### 失败与纠正（append-only）

| 时间 UTC | 现象 | 根因 | 安全影响 | 纠正 | 重验结果 |
|---|---|---|---|---|---|
| Task 01 探索期 | `zsh:1: no matches found: internal/persistence/sqlite/*schema*` | zsh 对无匹配 glob fail closed | 无；命令只读，未产生文件/外部状态变化 | 改用 `rg --files` 和明确文件路径读取 migration/schema | 已完整定位 `migrations.go`、`001_target_schema.sql` 及测试 |
| Task 01 探索期 | 默认 `goproxy.io` 查询三个 Charmbracelet module version 均返回 EOF | 当前 `GOPROXY=https://goproxy.io,direct` 的代理连接失败 | 无仓库/产品状态影响 | 临时命令使用 `GOPROXY=https://proxy.golang.org,direct`，不改全局配置 | 成功下载并读取固定版本 metadata/go.mod/LICENSE |
| Task 01 探索期 | 首次运行上游 package tests：Bubbles 缺测试依赖 sum，Lip Gloss 上游 test 报 output permission denied | 探针错误地运行了第三方自身测试，且 `-t` 依赖未预取；不是 OpenAgentX 测试 | 无产品影响；仅 `/tmp` 和 Go module cache | `go get -t` 后改为 `go test -run '^$'` 的 compile-only probe | 五个选定 package 在 Go 1.22.4 下均通过 compile probe |

#### 外部状态核对

| 对象 | 前 | 后 | 是否符合计划 |
|---|---|---|---|
| 真实 user service | P0 只读为 active/enabled | Task 01 未查询、未操作 | yes |
| 真实 DB/socket | 未触碰 | 未触碰 | yes |
| 默认 tmux server | 未触碰 | 未触碰 | yes |
| installed binary | 未触碰 | 未触碰 | yes |
| `steadyflow` 父仓库 | 有用户既有修改 | 未触碰；未提交 | yes |
| remote branch | P0 已按授权推送 `main`；feature 不存在远端 | feature branch 未 push | yes |

补充：TUI probe 只写入 `/tmp/openagentx-tui-probe.0br9AZ` 并填充普通 Go module cache；
未修改仓库 `go.mod`/`go.sum` 或用户全局 Go 配置。

#### 完成安全点

- 完成时间（UTC）：`2026-09-14T14:47:33Z`
- 任务提交 SHA：`35bd44564773882cfedefb31fad0afd64c0514e4`
- `git status --short --branch`：提交后已核验为
  `## codex/adr008-implementation...origin/main [ahead 1]`，无 path 条目
- 退出条件逐项：P0 已独立推送；sibling worktree 基于最新远端；七类契约、TUI 决策、五个
  设计问题和 Task 02-08 测试地图已记录；计划内定向测试通过；产品行为未修改
- 剩余问题：Task 01 无阻断问题；Charmbracelet 最新版本与 Go 1.22.4 不兼容，已通过固定
  兼容版本解决，不允许后续无审查升级
- 执行者建议：GO
- 监督者复核：GO（`2026-09-14T14:57:22Z` 收到复核结论）
- 下一步：停止，等待监督者对 Task 02 的明确 `GO`

#### Task 01 监督 Gate correction

- 监督复核确认 P0 `c3fc1bba8ddbae3eedace0c7a32537e2f47db307` 的范围与远端状态通过，
  Task 01 `35bd44564773882cfedefb31fad0afd64c0514e4` 为 docs-only 且契约/测试证据通过。
- 本次只读核验：feature HEAD 为 `35bd445`、相对 `origin/main` ahead 1 且工作树干净；主工作树
  为干净的 `main@c3fc1bb`；远端只有 `main@c3fc1bb`，feature branch 未 push。
- `steadyflow` 父仓库存在用户既有修改，本次未修改或提交；父仓显示的 `M OpenAgentX` 仅反映
  submodule 当前 main HEAD 相对父仓索引的差异，不是本次 gate correction 修改父仓。
- runtime 只读检查显示 `openagentx.service` 为 active/running、enabled、`NRestarts=0`；未操作
  service、真实 DB/socket/default tmux server 或 installed binary。
- 唯一 `NO-GO` 原因为主计划/任务 front matter/执行日志未同步已复核事实；本 docs-only gate
  correction 将 Task 01 同步为 `completed/GO` 并记录精确 SHA，Task 02 保持 `pending/WAIT`。

### Task 02 — 默认路径与 CLI 表面

#### 开始

- 状态：completed
- 执行者：Codex
- 开始时间（UTC）：`2026-09-14T15:07:21Z`
- 基线提交：`01751ce35c1635a8166304e8095cc9b32caf31fd`
- 监督者放行依据：监督者明确 `GO Task 02`，且限制只执行 Task 02
- 计划文件：`02-default-paths-and-cli-surface.md`
- 主计划/任务 front matter：`completed`
- 监督门禁：`GO`

#### 变更

| 时间 UTC | 文件/package | 变更目的 | 范围偏差 |
|---|---|---|---|
| `2026-09-14T15:23:15Z` | `internal/localprofile` | 新增唯一无副作用 resolver、五类资源默认路径、显式 flag 出现性、canonical/fail-closed 与 Agent ID 路径约束 | none |
| `2026-09-14T15:23:15Z` | `internal/cli/{console,fleet,admin}`、`internal/client/console`、`cmd/openagentx` | 将默认 profile 接入 Console/Fleet/init/agent apply/serve/schema；冻结 Console parser，保留旧 Attach/REPL；细分 TTY、credential 与 socket 错误 | none；未实现 Token/TUI/cursor/workspace 新行为 |
| `2026-09-14T15:49:21Z` | `internal/client/console/client_test.go` | review fix：Unix socket fixture 改用短随机临时目录和单字符 socket 名，并显式 cleanup | none；test-only，未修改 `NewUnixClient` 或产品行为 |

#### 决策

| ID | 决策 | 依据 | 是否需 ADR/监督确认 |
|---|---|---|---|
| D-02-01 | resolver 按调用方实际使用的资源逐项解析；未使用资源中的坏环境值不阻断无关命令 | fail closed 应作用于已选择资源，避免 `fleet status` 被无关 socket 配置阻断 | Task 01 已冻结；本 Task 实现 |
| D-02-02 | Fleet Console socket 统一来自 resolver，不再从 Worker YAML 猜测；显式 `--socket` 保持最高优先级 | Task 01 默认路径契约、单一 installation identity | Task 01 已冻结；本 Task 实现 |
| D-02-03 | `console login/logout` 本阶段只冻结 parser、TTY 与 credential 路径错误；明确返回未实现，不创建伪 credential | CLI Token 属于 Task 04，Task 02 禁止提前实现认证行为 | Task 04 前仍需监督 `GO` |
| D-02-04 | `attach --once`、旧 REPL 与正式 API 控制命令全部保留；连续 Attach 继续要求 TTY | Task 06 必须在全屏 TUI 同一提交原子替换旧入口 | Task 06 前仍需监督 `GO` |
| D-02-05 | Console 每个子命令只注册自身有效 flags；legacy 控制仍保留原参数，但不污染 `login/logout/attach` parser | 最终 CLI grammar 必须 fail closed，help 不应暗示无效能力 | Task 01 已冻结；本 Task 实现 |

#### 失败与纠正（append-only）

| 时间 UTC | 现象 | 根因 | 安全影响 | 纠正 | 重验结果 |
|---|---|---|---|---|---|
| `2026-09-14T15:23:15Z` | 首次定向测试中 `internal/cli/fleet` 的 `TestFleetUpPreflightsWorkspaceStartsConsoleAndEnabledSystemdUnits` 失败；其他 5 个 package 通过 | 既有测试假定未传 `--socket` 时从 Worker YAML 推导 `/run/openagentx/openagentx.sock`，与 Task 01 冻结的统一 profile socket 默认值冲突 | 仅测试期断言失败；未执行真实 tmux/systemd/DB/socket | 旧兼容场景显式传原 socket，并新增 profile 默认 socket 接线测试 | 待重验 |
| `2026-09-14T15:29:19Z` | 上述 Fleet 测试纠正后重验 | n/a | 无 | 运行同一 6-package 定向测试，并补齐默认/空/相对路径测试 | 全部通过，退出码 0 |
| 监督复核（独立 Termux review worktree） | `internal/client/console` 两个使用 UDS fixture 的测试在默认 `t.TempDir()` 下报 `bind: invalid argument`；将 `TMPDIR` 设为短路径后全部通过 | `t.TempDir()` 包含完整长测试名，最终路径超过 `sockaddr_un` 长度限制 | 产品代码未失败；测试依赖 CI 临时根和测试名长度，存在平台可移植性缺陷 | fixture 改用 `os.MkdirTemp("", "oax-uds-")` 和 socket 名 `s`，注册 `RemoveAll` cleanup；不放宽 client fail-closed 检查 | 本机不设置 `TMPDIR` 重验全部通过 |

#### 验证

| 时间 UTC | 命令 | 退出码 | 耗时 | 脱敏结果/证据 |
|---|---|---:|---:|---|
| `2026-09-14T15:23:15Z` | `gofmt -w cmd/openagentx/main.go internal/cli/console/command.go internal/client/console/client.go internal/cli/admin/command.go internal/cli/fleet/command.go internal/localprofile/profile.go internal/localprofile/profile_test.go && go test ./cmd/openagentx ./internal/cli/admin ./internal/cli/console ./internal/client/console ./internal/cli/fleet ./internal/localprofile` | 1 | 2.5s | 5 个 package 通过；Fleet 旧默认 socket 断言失败，详见失败与纠正 |
| `2026-09-14T15:29:19Z` | `go test ./internal/cli/... ./internal/fleet/... ./cmd/openagentx/...` | 0 | 2.35s | admin、console、fleet、worker CLI，Fleet model 和 main 全部通过 |
| `2026-09-14T15:29:19Z` | `go test ./internal/localprofile/...` | 0 | 0.38s | 全资源优先级、不同 home、无 home、空/相对/NUL override、`~`、Agent ID 逃逸、冲突、无副作用与不泄漏测试通过 |
| `2026-09-14T15:29:19Z` | `go build -o /tmp/openagentx-adr008-task02 ./cmd/openagentx` | 0 | 2.68s | 临时二进制构建成功；未安装 |
| `2026-09-14T15:29:19Z` | `env OPENAGENTX_DATABASE_PATH=/tmp/task02-help-redacted.db OPENAGENTX_SOCKET_PATH=/tmp/task02-help-redacted.sock OPENAGENTX_CREDENTIALS_PATH=/tmp/task02-help-redacted.json /tmp/openagentx-adr008-task02 --help` | 0 | <0.01s | 顶层 grammar/default precedence 可见，环境值未回显 |
| `2026-09-14T15:29:19Z` | `/tmp/openagentx-adr008-task02 {console help,fleet help,serve --help,schema verify --help,init --help,agent apply --help}`（逐条执行） | 0 | <0.3s total | 6 个子命令 help smoke 全部通过；未访问 DB/socket/tmux/service |
| Task 02 提交前 | `git diff --check` | 0 | <0.01s | 无 whitespace error；提交前将再次复核 staged path 边界 |
| `2026-09-14T15:33:32Z` | `go test ./internal/cli/... ./internal/fleet/... ./cmd/openagentx/...` | 0 | 1.48s | Console 子命令 parser 收紧后全部通过 |
| `2026-09-14T15:33:32Z` | `go test ./internal/localprofile/...` | 0 | 0.13s | resolver 最终测试再次通过 |
| `2026-09-14T15:33:32Z` | `go build -o /tmp/openagentx-adr008-task02 ./cmd/openagentx` | 0 | 2.63s | 最终临时构建通过；未安装 |
| `2026-09-14T15:33:32Z` | `/tmp/openagentx-adr008-task02 console {login --help,attach --help,login --content must-not-be-accepted}`（逐条执行） | 0/0/2 | <0.1s total | help 仅显示子命令相关 flags；login 对无关 legacy flag fail closed |
| 监督复核（独立 Termux review worktree） | `TMPDIR=<short-path> go test ./internal/client/console -count=1` | 0 | 未提供 | 短路径重验通过，确认失败来自 UDS fixture 路径长度；该 workaround 不作为最终验收条件 |
| `2026-09-14T15:49:21Z` | `go test ./internal/client/console -count=1` | 0 | 0.63s | 未设置 `TMPDIR`；短路径 fixture 下 Follow/正式控制 API/client 错误测试全部通过 |
| `2026-09-14T15:49:21Z` | `go test ./internal/cli/... ./internal/fleet/... ./cmd/openagentx/... -count=1` | 0 | 2.08s | Task 02 CLI、Fleet 和 main 定向测试全部通过 |
| `2026-09-14T15:49:21Z` | `go test ./internal/localprofile/... -count=1` | 0 | 0.26s | Task 02 resolver 定向测试通过 |
| `2026-09-14T15:49:21Z` | `go vet ./internal/cli/... ./internal/fleet/... ./internal/client/console ./internal/localprofile/... ./cmd/openagentx/...` | 0 | 0.57s | 受影响 packages 无诊断 |
| `2026-09-14T15:49:21Z` | `go build -o /tmp/openagentx-adr008-task02-review-fix ./cmd/openagentx` | 0 | 2.52s | 临时二进制构建通过；未安装 |
| `2026-09-14T15:49:21Z` | `git diff --check` | 0 | <0.01s | review fix 无 whitespace error；提交前将再次复核 staged path |

#### 外部状态与阶段安全点

- 实现和测试只写 feature worktree、Go build cache、Go test 临时目录及
  `/tmp/openagentx-adr008-task02`；未创建或修改真实 `~/.openagentx`。
- 未操作 service、真实 DB/socket、default tmux server、installed binary、remote feature branch 或
  `steadyflow` 父仓库。
- 主实现提交为 `e690bbbd11046e63841a4a869a90f6aeb42c5575`，提交后 feature 工作树干净、
  相对 `origin/main` ahead 3；13-file 范围经监督复核通过。
- review-fix 提交为 `c0e8d4aeae2516c005cedbce6c5b35d1b8b07553`，提交后 feature 工作树干净、
  相对 `origin/main` ahead 4；2-file test/docs 范围经监督复核通过。
- 主工作树实际核验为干净的 `main@c3fc1bba8ddbae3eedace0c7a32537e2f47db307`；feature
  未 push，运行服务保持 `active/running/enabled` 且 `NRestarts=0`，无外部状态修改。
- 监督最终复核确认：首次独立 Termux review 的 UDS 长路径失败已由 `c0e8d4a` 修复，默认
  `TMPDIR` 下 `go test ./internal/client/console -count=1` 通过，其余受影响 package 定向测试和
  `go vet` 通过；Task 02 结论为 `GO`。
- 主计划和 Task 02 front matter 已同步 `completed`；Task 03 保持 `pending/WAIT`，未开始。

## 6. Task 03：一致 Attach cursor 与代际 reducer

### 开始信息

- 执行者：Codex
- 开始时间（UTC）：`2026-09-14T16:03:37Z`
- feature 基线：`518d8edd76ad6a5b9c7f17f3bf8a0153ae1e64b9`
- branch/worktree：`codex/adr008-implementation` / `/home/sky/work/touzi/OneAxe/OpenAgentX-adr008-worktree`
- 开始状态：工作树干净，相对 `origin/main` ahead 5；监督者已明确 `GO Task 03`。
- 边界：仅实现一致 Attach snapshot/cursor、Observe SSE retention gap 和共享 reducer；不开始
  CLI Token/schema、`OAX` workspace、tmux 身份或 TUI，不操作任何真实运行状态。
- 主计划和 Task 03 front matter 按 gate-record 协议继续保持 `pending`；Task 03 门禁保持 `WAIT`。

### 实现范围与决策

- `internal/domain/console_contract.go` 定义 transport 无关的 `ConsoleSnapshot` 与 Journal bounds；
  persistence 未依赖 API DTO。
- `internal/persistence/sqlite/console_snapshot.go` 在一个 `ReadOnly` SQLite 事务内依次读取
  Agent、确定性当前 Worker（generation/updated_at/instance ID 降序）、有效 Backend health、
  active RunAttempt，最后读取 Journal high-water；空 Journal high-water 为 `0`。
- `internal/persistence/sqlite/worker_execution_repository.go` 仅抽取事务内 Backend 查询 helper，
  保留既有 network binding 校验和 unavailable 降级语义；`journal.go` 增加 earliest/latest bounds。
- `internal/api/console/handler.go` 改为一次 repository snapshot 调用并新增唯一
  `snapshot_sequence`；Normal/Diagnostic 继续共用该路径和既有授权/脱敏边界。
- `internal/api/panel/handler.go` 在写入 SSE headers 前检查 retention gap；稳定返回 HTTP `409`
  和 `EVENT_CURSOR_EXPIRED`。空 Journal、`after=0`、earliest predecessor/earliest、
  `after>latest` 均保持合法。Worker 和 active Run 使用既有安全 read model 投影。
- `internal/client/console/client.go` 首次从 Attach cursor Follow；普通重连从最后成功应用的
  sequence 继续且不重新 Attach；只有结构化 retention gap 才重新 Attach。重复幂等，倒退
  fail closed，callback 失败不推进 cursor。
- `internal/consolemodel/` 新增纯 reducer：旧 generation、同代异 instance 和无关 Agent 事件
  安全忽略但推进 cursor；heartbeat burst 合并，Worker replacement、状态/lease 异常和
  active Run 切换进入 Timeline；输入仅为安全 API read model。
- `internal/cli/console/repl.go` 使用同一 reducer 维护显示和控制身份；`/down` 始终读取 reducer
  最新 WorkerInstanceID/generation，普通/旧代 heartbeat 不刷 Timeline。
- 测试文件覆盖 Attach 投影、N/N+1 事务竞态、Journal/SSE 边界、K/K+1 reconnect、
  duplicate/backward/gap、generation 48/42、Worker replacement、draining、active Run 切换、
  heartbeat burst 和安全输出边界。

### 验证记录

| 时间 UTC | 命令 | 退出码/耗时 | 结果 |
|---|---|---:|---|
| `2026-09-14T16:09Z` | `go test ./internal/consolemodel ./internal/api/console ./internal/client/console ./internal/api/panel ./internal/cli/console ./internal/persistence/sqlite` | `0` / 约 11.4s | 首批实现与新增边界测试通过 |
| `2026-09-14T16:12Z` | `go test ./internal/api/console ./internal/client/console ./internal/persistence/sqlite/... ./internal/safeoutput/... ./internal/api/panel ./internal/cli/console ./internal/consolemodel` | `0` / 约 1.2s（缓存为主） | Task 03 全部定向 package 通过 |
| `2026-09-14T16:12Z` | `go test -race ./internal/api/console ./internal/client/console ./internal/persistence/sqlite/... ./internal/safeoutput/... ./internal/api/panel ./internal/cli/console ./internal/consolemodel` | `0` / 约 16s | race 通过；SQLite 14.198s、panel 15.215s |
| `2026-09-14T16:13Z` | `go vet ./internal/domain ./internal/persistence/sqlite/... ./internal/api ./internal/api/console ./internal/api/panel ./internal/client/console ./internal/consolemodel ./internal/cli/console ./cmd/openagentx` | `0` / 约 1.2s（含后续命令） | 受影响 package vet 通过 |
| `2026-09-14T16:13Z` | `go build -o /tmp/openagentx-adr008-task03 ./cmd/openagentx` | `0` | 构建通过；仅写入 `/tmp` |
| `2026-09-14T16:13Z` | `./scripts/check-legacy-control-paths.sh --release` | `0` | 11 项均为 `CLEAN`，release check 通过 |
| `2026-09-14T16:13Z` | `git diff --check` | `0` | 通过 |
| `2026-09-14T16:18Z` | `go test ./...` | `0` / 约 8s | 全仓 Go 测试通过，无 package 失败 |
| `2026-09-14T16:19Z` | 最终重复执行上述 Task 03 race、vet、build、release scanner、`git diff --check` | `0` / 约 16s | 最终验证全部通过；SQLite race 14.164s、panel race 15.908s |

### 失败、纠正与外部状态

- 本阶段无测试、race、vet、build、release scanner 或 diff-check 失败；无失败记录需要纠正。
- 未修改 ADR、主计划或 Task 03 front matter；未开始 CLI Token/schema、`OAX` workspace、
  tmux 身份或 TUI。
- 仅使用测试临时 SQLite/HTTP/UDS fixture；未读取或修改真实 DB/socket/default tmux，未安装
  `/tmp/openagentx-adr008-task03`，未操作 service、installed binary、remote feature branch 或父仓。
- `2026-09-14T16:20:32Z` 提交前核验：feature 基线仍为 `518d8ed`、ahead 5；主工作树仍为
  干净的 `main@c3fc1bba8ddbae3eedace0c7a32537e2f47db307`。父仓只显示既有 submodule
  pointer 差异，本阶段未修改父仓。

### Task 03 监督 NO-GO 与 review fix

- `2026-09-14T16:42:53Z` 监督者复核主实现
  `4aea5a6291b47792b1c69256ae0ae6422a890cb4` 后暂定 `NO-GO`：事务快照、SSE retention gap
  和旧 Worker heartbeat 主路径通过，但发现三项状态正确性缺口。Task 03 继续保持
  `active/WAIT`，主计划与 Task 03 front matter 继续保持 `pending`。
- 根因一：`Follow` 的公开签名仍接受可误用的初始 `afterSequence`，REPL 和测试显式传 `0`；
  review fix 删除该参数，首次 cursor 现在只能来自事务 Attach 的 `snapshot_sequence`，普通重连
  仍只使用 client 内部最后成功应用的 sequence。
- 根因二：Attach 的 `RunSnapshot` 未携带 Worker identity，reducer 只按 `agent_id` 接受 Run
  投影；review fix 增加安全的 `worker_instance_id`/`worker_generation`，并在更新 ActiveRun 前
  同时校验当前 Agent、Worker ID、generation 和非 offline 状态。合法旧 generation 或同代异
  instance Run 只进入安全 Timeline、推进 cursor，不覆盖当前状态；无效投影 fail closed 且不
  推进 cursor。
- 根因三：Worker replacement/offline 后保留旧 Worker scoped 状态；review fix 在转换时清空
  Backend health、Diagnostic 和 ActiveRun，后续 ActiveRun 仅能由明确绑定当前 Worker 的 Run
  投影恢复。Attach、Worker 与 Run 安全 read model 同时增加必要 identity/status 校验，不投影
  原始 Journal payload、stderr 或凭据。
- review fix 文件范围：`internal/api/console`、`internal/api/panel`、`internal/cli/console`、
  `internal/client/console`、`internal/consolemodel`、`internal/domain`、
  `internal/persistence/sqlite` 的实现与测试，以及本 execution log；未修改 ADR、主计划、
  Task 03 front matter 或 Task 04+ 产品行为。

#### Review fix 验证

| 时间 UTC | 命令 | 退出码/耗时 | 结果 |
|---|---|---:|---|
| `2026-09-14T16:42:53Z` | `go test ./internal/api/console ./internal/client/console ./internal/persistence/sqlite/... ./internal/safeoutput/... ./internal/api/panel ./internal/cli/console ./internal/consolemodel -count=1` | `0` / 10.27s | 无缓存定向测试全部通过；覆盖旧/冲突 Run fencing、replacement/offline 清理、Attach fail-closed 与现有竞态/cursor/safe-output 场景 |
| `2026-09-14T16:42:53Z` | `go test -race ./internal/api/console ./internal/client/console ./internal/persistence/sqlite/... ./internal/safeoutput/... ./internal/api/panel ./internal/cli/console ./internal/consolemodel -count=1` | `0` / 21.86s | Task 03 受影响路径 race 全部通过 |
| `2026-09-14T16:42:53Z` | `go test ./... -count=1` | `0` / 13.18s | 全仓 Go 测试全部通过；仅既有无测试文件 package 提示 |
| `2026-09-14T16:42:53Z` | `go vet ./internal/domain ./internal/persistence/sqlite/... ./internal/api ./internal/api/console ./internal/api/panel ./internal/client/console ./internal/consolemodel ./internal/cli/console ./cmd/openagentx` | `0` / 0.47s | 受影响 package 无诊断 |
| `2026-09-14T16:42:53Z` | `go build -o /tmp/openagentx-adr008-task03-review-fix ./cmd/openagentx` | `0` / 2.58s | 临时二进制构建成功；未安装 |
| `2026-09-14T16:42:53Z` | `./scripts/check-legacy-control-paths.sh --release` | `0` / <0.01s | 11 项 `CLEAN`，release scanner 通过 |
| `2026-09-14T16:42:53Z` | `git diff --check` | `0` / <0.01s | 无 whitespace error；提交前将再次检查 staged 边界 |

#### Review fix 外部状态

- 所有测试使用 Go 临时目录中的 SQLite/HTTP/UDS fixture；构建产物仅写入
  `/tmp/openagentx-adr008-task03-review-fix`。
- 未 push feature、未操作 service、真实 DB/socket/default tmux、installed binary 或
  `steadyflow` 父仓；未开始 CLI Token、schema、`OAX` workspace、TUI 或 Task 04。

### Task 03 第二次监督 NO-GO 与 Backend health review fix

- `2026-09-14T16:59:17Z` 监督者确认前一轮三项 review fix 已通过，但再次 gate 暂定
  `NO-GO`：普通 Follow 重连正确地不重复 Attach，而 Worker SSE 安全投影未携带 Backend
  health，导致同 Worker/generation 的 health 转换无法更新 reducer 状态。Task 03 继续保持
  `active/WAIT`，主计划和 Task 03 front matter 继续保持 `pending`。
- 根因：`WorkerReadModel` 只有 Worker 生命周期字段；Panel 在 `worker_instance` 事件上只读取
  Worker，reducer 也没有以该事件替换 Attach 时的 Backend health。因此 `healthy` 到
  `unavailable` 等同代转换会永久显示旧值，除非发生 retention re-attach 或 Worker replacement。
- 修复：`WorkerReadModel.backend_health` 只承载 `backend_id -> BackendHealth enum`。Panel 通过
  正式 `ListWorkerBackends` state/repository 能力读取当前 Worker health，逐项校验 ID、enum 和
  重复项，只将 map 加入 SSE；descriptor、network policy、诊断与原始 heartbeat payload 均不
  投影。读取或校验失败时，在写入该 event ID/data 前结束 stream，使 client 保持 last-applied
  cursor 并重连，不发送缺字段事件或越过后续事件。
- reducer 对合法当前 Worker event 原子替换 Backend health；同 generation health 变化作为有
  意义转换进入 Timeline，未变化 heartbeat 继续合并。`nil` map 清除旧 health；当前 Worker 的
  无效 map 清除旧 health、返回错误且不推进 cursor。旧 generation/冲突 instance 继续按既有
  fencing 处理，replacement/offline 继续清除旧 Worker scoped 状态。
- 文件范围：`internal/api/observe.go`、`internal/api/panel/handler.go` 及测试、
  `internal/consolemodel/reducer.go` 及测试，以及本 execution log；未修改 Auth、schema、Token、
  workspace、tmux、TUI 或 Task 04+ 行为。

#### Backend health review fix 验证

| 时间 UTC | 命令 | 退出码/耗时 | 结果 |
|---|---|---:|---|
| `2026-09-14T16:59:17Z` | `go test ./internal/api/panel ./internal/consolemodel ./internal/api/console ./internal/client/console ./internal/cli/console -count=1` | `0` / 8.76s | 首轮相关 package 测试通过 |
| `2026-09-14T16:59:17Z` | `go test ./internal/api/panel -run 'TestSSEAgentFilterIncludesSafeWorkerDrainSnapshot\|TestSSEBackendProjectionFailureDoesNotSendOrCrossWorkerEvent' -count=10` | `0` / 6.22s | SSE safe projection 与查询失败边界重复通过；注入 descriptor/network/diagnostic 未泄漏 |
| `2026-09-14T16:59:17Z` | `go test ./internal/consolemodel -run 'TestReducerUpdatesBackendHealthFromCurrentWorkerHeartbeat\|TestReducerClearsWorkerScopedStateOnReplacementAndOffline' -count=20` | `0` / 0.39s | 同代 health 更新、Timeline、nil/invalid、replacement/offline 回归重复通过 |
| `2026-09-14T16:59:17Z` | `go test ./internal/api/console ./internal/client/console ./internal/persistence/sqlite/... ./internal/safeoutput/... ./internal/api/panel ./internal/cli/console ./internal/consolemodel -count=1` | `0` / 11.64s | Task 03 无缓存定向测试全部通过 |
| `2026-09-14T16:59:17Z` | `go test -race ./internal/api/console ./internal/client/console ./internal/persistence/sqlite/... ./internal/safeoutput/... ./internal/api/panel ./internal/cli/console ./internal/consolemodel -count=1` | `0` / 21.63s | Task 03 受影响路径 race 全部通过 |
| `2026-09-14T16:59:17Z` | `go test ./... -count=1` | `0` / 13.29s | 全仓 Go 测试通过；仅既有无测试文件 package 提示 |
| `2026-09-14T16:59:17Z` | `go vet ./internal/domain ./internal/persistence/sqlite/... ./internal/api ./internal/api/console ./internal/api/panel ./internal/client/console ./internal/consolemodel ./internal/cli/console ./cmd/openagentx` | `0` / 0.40s | 受影响 package 无诊断 |
| `2026-09-14T16:59:17Z` | `go build -o /tmp/openagentx-adr008-task03-backend-health-fix ./cmd/openagentx` | `0` / 2.53s | 临时二进制构建成功；未安装 |
| `2026-09-14T16:59:17Z` | `./scripts/check-legacy-control-paths.sh --release` | `0` / <0.01s | 11 项 `CLEAN`，release scanner 通过 |
| `2026-09-14T16:59:17Z` | `git diff --check` | `0` / <0.01s | 无 whitespace error；提交前再次检查 staged 边界 |

#### Backend health review fix 外部状态

- 测试只使用 Go 临时目录中的 SQLite/HTTP/UDS fixture；构建产物仅写入
  `/tmp/openagentx-adr008-task03-backend-health-fix`。
- 未 push feature、未操作 service、真实 DB/socket/default tmux、installed binary 或
  `steadyflow` 父仓；未开始 Task 04。

### Task 03 完成安全点与最终监督 Gate

- 实现提交：主实现 `4aea5a6291b47792b1c69256ae0ae6422a890cb4`、identity/reducer review fix
  `9e18246dc4f61ee8ccc044509229b13b0e191f9d`、Backend health review fix
  `2f9772753b3a75e6304abd76c4eb6a259c9e0c6f`。
- 第一轮监督 `NO-GO` 的 public sequence-0 Follow 参数、Run Worker identity fencing 和
  replacement/offline 旧状态清理缺口由 `9e18246` 修复；第二轮监督 `NO-GO` 的同代 Backend
  health 实时状态缺口由 `2f97727` 修复。两轮失败、根因、纠正和重验记录均保留在上文。
- 监督者独立复核确认：无 public sequence-0 Follow 参数；generation 48 后 generation 42 的
  heartbeat/Run 不回退；replacement/offline 清理旧状态；同 generation Backend health 通过
  结构化安全投影实时更新；定向测试、vet 与 diff check 通过。最终结论为 Task 03 `GO`。
- `2f97727` 提交后实际核验：feature 工作树 clean，相对 `origin/main` ahead 8；主工作树为
  clean 的 `main@c3fc1bba8ddbae3eedace0c7a32537e2f47db307`。feature 未 push，未操作
  service、真实 DB/socket/default tmux、installed binary 或父仓。
- 主计划和 Task 03 front matter 已在本 docs-only gate record 同步为 `completed`；Task 04
  继续保持 `pending/WAIT`，未开始实现。

### Task 04 — 可撤销 CLI Token 会话

### 开始信息

- 状态：completed
- 执行者：Codex
- 开始时间（UTC）：`2026-09-14T17:12:49Z`
- feature 基线：`c1196d7d43e10bf573cbfcc02525973debe9ba1f`
- branch/worktree：`codex/adr008-implementation` / `/home/sky/work/touzi/OneAxe/OpenAgentX-adr008-worktree`
- 开始状态：工作树 clean，相对 `origin/main` ahead 9；监督者已明确 `GO Task 04`。
- 边界：仅实现 CLI Token domain/service/repository、schema v1 compatible ensure、UDS-only auth
  与 scope、credential store 及现有 Console/Fleet API bearer 接线；不开始 `OAX` workspace、
  TUI 或 Fleet 集成，不操作任何真实运行状态。
- 主计划和 Task 04 front matter 按 gate-record 协议继续保持 `pending`；监督门禁保持 `WAIT`。

### 实现范围与安全决策

- 新增 `domain.CLITokenRecord` 与四个冻结 scope；`internal/auth/cli` 使用 32-byte
  CSPRNG opaque Token、SHA-256 digest、可注入 clock 和最长 30 天绝对期限。认证同时校验
  当前 installation、用户状态、principal、当前角色与签发 scope；last-used 更新不延长期限。
- SQLite v1 target schema 和既有 v1 `ensureCLITokenTables` 同时增加
  `installation_metadata`、`cli_tokens` 与两个索引。installation ID 为持久化随机值；ensure
  在一个事务内完成 DDL、ID 初始化、定义/列/索引/单例行校验，故障注入不会留下半表。
- Replace Login 在同一 repository 事务先撤销同 installation/user 的旧 Token，再插入新
  digest；logout 对尚未过期且 audience 匹配的已撤销 Token 幂等。提供按 web user 撤销全部
  CLI Token 的 service/repository hook；当前没有密码修改入口，因此未虚构 UI 集成。
- UDS 独立挂载 versioned CLI login/session/logout，以及必要的 UDS-only installation probe；
  Web mux 对 CLI auth 前缀固定 404。Console/Panel/Admin 分别构造 Web cookie+CSRF 和 UDS
  bearer authorizer，不存在全局 bearer fallback。未知的 UDS network mutation 没有冻结 scope，
  因而 fail closed。
- Normal Attach、Observe/Agent/SSE 使用 `console.read`+viewer；dispatch/steer/cancel/approval
  使用 `console.control`+operator；Diagnostic Attach 使用 `console.diagnostic`+owner；Worker
  drain/stop/force-stop 使用 `fleet.lifecycle`+owner。CLI auth 失败稳定为
  `401 CLI_UNAUTHENTICATED` 或 `403 CLI_FORBIDDEN`。
- `internal/credentialstore` 以 canonical socket、installation ID、username 隔离 credential；
  支持同 socket 当前用户选择，校验 owner、目录不宽于 `0700`、文件不宽于 `0600`，拒绝
  symlink/非普通文件，使用同目录唯一临时文件、fsync、rename、目录 fsync 和失败清理。
- `console login` 是唯一向 UDS login 发送密码的 CLI 路径；后续 Attach/控制先 probe，再加载
  匹配 credential 并使用 Bearer。logout 在 daemon 不可达、Token 无效或 installation 替换时
  仍删除本地副本；installation 不匹配时不发送 Token。共享 client 的旧直接密码 Login 入口
  显式 fail closed，Fleet credential 接线留在 Task 07，不在本任务提前实现。

### 文件范围

- domain/auth/API：`internal/domain/cli_token.go`、`internal/auth/request.go`、
  `internal/auth/cli/`、`internal/api/auth.go`、`internal/api/contracts.go`、
  `internal/api/auth/cli_handler.go`，以及 Console/Panel/Admin handler 的双 authorizer 接线。
- persistence：`internal/persistence/sqlite/cli_token_repository.go`、
  `internal/persistence/sqlite/bootstrap_repository.go`、migrations target/ensure/required validation。
- local client/CLI：`internal/credentialstore/`、`internal/client/console/`、
  `internal/cli/console/`；`cmd/openagentx/main.go` 仅组装 UDS/Web 隔离 mux。
- 测试同步覆盖上述 package 与 `cmd/openagentx`；未修改主计划、Task 04 front matter、ADR、Web
  产品代码、Fleet 实现、workspace、tmux 或 TUI。

### 失败、纠正与验证

| 时间 UTC | 命令/检查 | 退出码 | 脱敏结果/纠正 |
|---|---|---:|---|
| Task 04 阅读阶段 | 首次按章节边界抽取冻结文档 | 非预期输出 | awk 边界产生重复/空输出；随后按精确行号完整重读 Task 04、CONTRACT-FREEZE 5/8、ADR-008 11-12 和 execution log，未把首次输出作为证据 |
| 2026-09-14T17:28Z | 首轮定向 `go test` | 1 | 双 authorizer 返回值指针和旧 client/CLI test fixture 尚未同步，出现编译/旧 Web endpoint 404；集中修正接口与 fixture 后重跑通过，无产品运行状态变化 |
| 2026-09-14T17:36Z | 定向安全 package tests | 1 | 发现 operator 未继承 viewer 读取权限，修正为 owner > operator > viewer；credential fixture 的辅助现场混合导致预期安全拒绝，拆分隔离 fixture；重跑全部通过 |
| 2026-09-14T17:42Z | `go test -race ./internal/auth/... ./internal/api/auth ./internal/api/console ./internal/api/admin ./internal/api/panel ./internal/client/console ./internal/cli/console ./internal/credentialstore ./internal/persistence/sqlite/...` | 0 | Task 04 auth/API/client/CLI/store/schema 与受影响 Panel/Admin race 全部通过 |
| 2026-09-14T17:43Z | `go test ./...` | 0 | 全部 Go package 通过 |
| 2026-09-14T17:43Z | `npm test -- --runInBand` | 1 | 仓库没有通用 `test` script；改用 package.json 现有 `test:observation`，保留本失败记录 |
| 2026-09-14T17:43Z | `npm run test:pwa && npm run build` | 127 | PWA assertions 通过；worktree 缺少 `node_modules`，`vite` 不存在。随后 `npm ci` 按 lockfile 安装 123 packages，audit 为 0 vulnerability |
| 2026-09-14T17:44Z | `npm run test:observation` / `npm run test:pwa` / `npm run build` | 0 | observation 4/4、PWA assertions、Vite production build 全部通过；仅产生 ignored `web/node_modules` 和 `web/dist` |
| 2026-09-14T17:44Z | `go vet ./internal/auth/... ./internal/api/... ./internal/client/console ./internal/cli/console ./internal/credentialstore ./internal/persistence/sqlite/... ./cmd/openagentx` | 0 | 受影响 packages 无 vet 问题 |
| 2026-09-14T17:44Z | `go build ./...` | 0 | 全部 Go targets 构建通过 |
| 2026-09-14T17:44Z | `./scripts/check-legacy-control-paths.sh --release` | 0 | release scanner 全部类别 CLEAN |
| 2026-09-14T17:45Z | 最终 `go test ./...` + 受影响 `go vet` + `go build ./...` + release scanner + `git diff --check` | 0 | 在旧直接密码 Login fail-closed 收口后，最终源码全量通过 |
| 2026-09-14T17:48Z | CLI auth handler nil dependency fail-closed 审计后，`go test -race ./internal/api/auth ./cmd/openagentx` + `go test ./...` + 受影响 vet/build + release scanner + diff check | 0 | 构造器改为拒绝 nil service；最终源码与组装测试全部通过 |

### Task 04 提交前安全点

- 状态保持 `active/WAIT`；主计划与 Task 04 front matter 保持 `pending`，Task 05 未开始。
- 实现提交将在本记录随同实现和测试一起创建；提交本身无法包含自身 SHA，精确 SHA、实际
  post-commit clean/ahead 与监督结论由后续独立 docs-only gate record 记录。
- 未 push feature、未安装 binary、未重启服务，未访问真实 DB/socket/default tmux；未修改
  `steadyflow` 父仓或主工作树。所有 DB、HOME、credential 和 UDS 测试均使用临时路径。

### Task 04 监督 NO-GO 与 session boundary review-fix

- `2026-09-14T18:16:09Z` 监督门禁暂定 `NO-GO`：主实现
  `c240aa4dbd47565181d22f602ea3203e5fbfe4dc` 的核心模型、schema 与 UDS/Web mux
  分离通过，但发现三组提交前阻断。Task 04 状态继续 `active/WAIT`，主计划和 Task 04
  front matter 继续 `pending`，未开始 Task 05；`T04-01` 原样保留给 Task 07。
- scope 根因是 `panelRequirement` 为所有 Observe GET 默认分配 `console.read`。修复改为 CLI
  白名单：仅 Agent list 和安全 events stream 使用 `console.read`，health 仍无需认证；冻结的
  task/approval mutation 保持 `console.control`。其余 Observe 与全部 Network route 的 CLI
  scope 为空并稳定 fail closed 为 `403 CLI_FORBIDDEN`；Web authorizer 仍忽略 CLI scope，
  继续执行原角色与 CSRF 校验。表驱动测试以真实 CLI service 签发的 viewer/owner bearer
  逐一覆盖所有未冻结 Observe/Network read/write route。
- credential 生命周期根因是本地 Save 只替换完整三元 key，installation 替换会残留同
  socket/user 的旧 secret；普通命令只做无认证 installation probe，没有验证 Token session；
  JSON 读取也缺少大小、条目、重复 key、current selection 和 metadata 上限。修复后 Save
  清除同 socket/user 的全部旧 installation credential 并保留其他用户；业务 API 前先检查
  本地 absolute expiry，再调用 authenticated `/api/auth/v1/cli/session`，精确比较
  installation ID、token ID、username 与 absolute expiry。结构化
  `401 CLI_UNAUTHENTICATED` 删除本地 credential 并提示重新 login，临时传输错误不删除；
  installation mismatch 仍在发送 Token 前终止。credential reader 限制为 1 MiB/128 entries，
  拒绝重复 key、悬空 current、非 canonical socket、非法 ID/Token/time，错误不包含 secret。
- 并发根因是无跨进程锁的 read-modify-rename 会丢失其他用户，并可能让较慢的旧登录在服务端
  已撤销后覆盖新 Token。修复使用同目录固定 lock file、`O_NOFOLLOW`、当前 owner、精确
  `0600` 和 Linux `flock(LOCK_EX)`；Save/Load/Delete 的完整文件事务均受锁保护。Replace
  Login 将远端签发、session 验证和本地替换置于同一锁内，因而同 socket 的并发 login
  按签发顺序串行；临时文件或 lock 校验失败时清理临时文件并保留旧 credential。

| 时间 UTC | 命令/检查 | 退出码 | 脱敏结果/纠正 |
|---|---|---:|---|
| 2026-09-14T18:02Z | 首轮 `go test ./internal/credentialstore ./internal/cli/console ./internal/api/panel` | 1 | 新 `Session`/`Replace` 接口的 test doubles 尚未同步，旧 store fixture 使用短 token，被新增 256-bit token 校验正确拒绝；同步替身并改为合法 opaque fixture，未放宽产品校验 |
| 2026-09-14T18:05Z | 同一组定向测试重跑 | 1 | 重用 test client 时 mock session expiry 与新临时 credential expiry 不一致，被精确 session 比对正确拒绝；让 fixture 每次绑定同一 expiry 后重跑通过 |
| 2026-09-14T18:09Z | `go test ./internal/credentialstore ./internal/cli/console ./internal/api/panel` 与对应 `-race` | 0 | scope、credential validation、旧 installation 清理、跨 Store `flock`、并发 Save/Replace、local expiry、401 删除与临时错误保留均通过 |
| 2026-09-14T18:11Z | Task 04 全部定向 `go test` | 0 | auth、API auth/Console/Admin/Panel、Console client/CLI、credential store、SQLite migrations/reopen 全部通过 |
| 2026-09-14T18:12Z | Task 04 全部定向 `go test -race` | 0 | 同上受影响安全 packages race 通过 |
| 2026-09-14T18:13Z | `go test ./...` | 0 | 全部 Go packages 通过 |
| 2026-09-14T18:13Z | 受影响 `go vet`、`go build ./...`、release scanner、`git diff --check` | 0 | vet/build 通过，legacy control-path release check 全部 CLEAN，diff 无 whitespace 错误 |
| 2026-09-14T18:14Z | `npm run test:observation && npm run test:pwa && npm run build` | 0 | observation 4/4、PWA assertions 与 Vite production build 通过；未修改 Web 产品代码 |
| 2026-09-14T18:15Z | 新增真实 UDS client session header/结构化 401 测试后的定向测试、race 与 diff check | 0 | Bearer session 请求及 `CLI_UNAUTHENTICATED` 解析证据通过 |
| 2026-09-14T18:19Z | 最终无缓存定向测试、race 与 `go test -count=1 ./...` | 0 | Task 04 受影响 packages、SQLite、全量 Go packages 均实际重跑通过；定向 11.8s、race 19.8s、全量 13.1s |

### Task 04 完成安全点与最终监督 Gate

- 实现提交：主实现 `c240aa4dbd47565181d22f602ea3203e5fbfe4dc`、session boundary
  hardening fix `0a5e85987f0c9bd29275ece138200083be13171e`。
- 监督者独立审查与重验确认：数据库仅存 Token digest；schema v1 空库、既有完整 v1 reopen、
  重复 ensure 和故障 rollback 成立；CLI endpoint 仅挂 UDS，Web mux、cookie 与 CSRF 边界未
  弱化；role+scope 白名单和稳定 401/403 成立。
- 首次监督 `NO-GO` 的 Observe scope 过宽、credential 生命周期不完整和缺少跨进程原子性，
  已由 `0a5e859` 修复。最终复核确认 authenticated session 校验、installation mismatch
  发送前阻断、Replace 清理旧 installation，以及 owner/`0700`/`0600`/`O_NOFOLLOW`/`flock`/
  bounded JSON/结构校验和并发测试均成立；所有既有失败与纠正记录保留在上文。
- `0a5e859` 提交后实际核验：feature 工作树 clean，相对 `origin/main` ahead 11；主工作树为
  clean 的 `main@c3fc1bba8ddbae3eedace0c7a32537e2f47db307`。feature 未 push，未操作
  service、真实 DB/socket/default tmux、installed binary 或 `steadyflow` 父仓。
- Open Issue `T04-01` 保持 Task 07/pending，由 Task 07 关闭，不阻断 Task 04 gate。
- 主计划和 Task 04 front matter 已在本 docs-only gate record 同步为 `completed`；Task 05
  保持 `pending/WAIT`，未开始实现。监督者最终结论为 Task 04 `GO`。
- Gate record 相对链接/状态检查首次调用环境未安装的 `ruby`，退出 `127`；改用 Perl 等价
  只读检查后 relative links 与 status consistency 均通过。staged path 白名单和
  `git diff --cached --check` 同时通过，仅包含三份 `docs/plans` 文件。

### Task 05 — `OAX` workspace 与非破坏绑定

### 开始信息

- 状态：completed
- 执行者：Codex
- 开始时间（UTC）：`2026-09-14T18:36:33Z`
- feature 基线：`0022e9206a4cff01e1e219e3ecf46dcbaa7a46be`
- branch/worktree：`codex/adr008-implementation` /
  `/home/sky/work/touzi/OneAxe/OpenAgentX-adr008-worktree`
- 开始状态：工作树 clean，相对 `origin/main` ahead 12；监督者已明确 `GO Task 05`。
- 边界：仅实现结构化 tmux inspect、`OAX` pane 0/marker 冲突模型、显式 Attach 绑定和
  Fleet workspace reconcile；不开始全屏 TUI、Agent selector UI、Fleet credential/user-systemd
  集成，不操作真实 `OAX`/default tmux/service/DB/socket/installed binary 或父仓。
- 主计划和 Task 05 front matter 按 gate-record 协议继续保持 `pending`；监督门禁保持 `WAIT`。

### 实现范围与安全决策

- `internal/fleet` 将受管 session 统一为大小写敏感的 `OAX`，新增结构化 window/pane/option
  inspection：`list-windows` 只取得内部 window handle 与名称，再对每个唯一 handle 分别执行
  `list-panes` 和 `show-options`。pane `0` 存在性不再由 active pane、进程名、window index 或
  `%pane_id` 推断；内部 handle 只用于把 mutation 锁定到已 preflight 的 window，不进入业务 API
  或用户输出。
- 固定 marker 为 `@openagentx_managed=1` 和 `@openagentx_agent_id=<exact-agent-id>`；overview
  仅有 managed marker。统一 topology validator 在副作用前拒绝缺 pane 0、重复名称/Agent marker、
  非法 marker、name/marker 不一致、unmanaged target、reserved overview 和非精确 Agent ID。
- 新增单一 window binding service：Attach 初次检查当前 `OAX:<window>.0`，认证后从正式
  `/api/observe/v1/agents` 精确验证所选 Agent，再做第二次全量 preflight。相同 Agent 幂等复用；
  另一 Agent 返回 `confirmation-required`，Task 05 CLI 不暴露确认绕过；未绑定且未显式选择时
  明确等待 Task 06 selector 并 fail closed。
- 绑定按 managed marker、Agent marker、rename、结构化重读顺序执行；每个 mutation/verify
  失败均恢复原名称和两个 option 的原始存在性。恢复失败返回显著 `partial-failure` 和人工检查
  提示，且不继续 Attach。pane `1+` 从不关闭、重排、读取内容或按进程推断状态。
- Fleet Reconcile 使用两阶段完整 preflight，只补缺并按内部 handle 配置新 window 的
  `pane-base-index=0`、`remain-on-exit=on` 和 marker，随后重读验证；兼容 window 复用，额外
  unmanaged/orphaned window 与所有辅助 pane 保留。旧 `agentx` session 不迁移、不合并、不参与
  `OAX` inspect。
- Console 现有 `--once`、行式 REPL 和 Follow 能力继续保留；仅将提示符改为 `OAX> `，未添加
  TUI dependency、菜单、Agent selector、Token/schema、user-systemd 或 Task 06/07 行为。
- 用户文档同步 manifest、Attach 前置条件、多 pane 和旧 session 边界。变更文件为
  `README.md`、`docs/operations/openagentx-user-install-guide.md`、`internal/fleet/{manifest,workspace,binding}*`、
  `internal/client/console/client{,_test}.go`、`internal/cli/console/{command,repl}*.go`、
  `internal/cli/fleet/{command,command_test}.go` 与本 execution log。

### 失败与纠正（append-only）

| 时间 UTC | 现象 | 根因 | 安全影响 | 纠正/结果 |
|---|---|---|---|---|
| 2026-09-14T18:42Z | 首次大补丁被 `apply_patch` 拒绝 | 同一 patch 对 `workspace.go` 同时 delete/add 不受工具支持 | patch 原子拒绝，文件未改变，无产品或外部状态影响 | 拆为两个 `apply_patch` 动作后成功 |
| 2026-09-14T18:50Z | README/安装指南只读聚合调用报 `ReferenceError: b is not defined` | 工具编排脚本遗漏第二个结果变量声明 | 只读命令未形成结果，无文件或外部状态变化 | 按两个显式变量重跑并取得实际文本 |
| 2026-09-14T18:56Z | help/零 window 小补丁因 Console usage 上下文不匹配被拒绝 | 预期行与现有 `legacy controls` 文案不同 | patch 原子拒绝，无部分修改 | 读取精确上下文后重做，三项改动成功 |
| 2026-09-14T18:48Z | 首轮四 package 定向测试 | n/a | 无；只使用 fake runner 和临时 UDS | 全部通过，未出现产品测试失败 |

### 验证

| 时间 UTC | 命令 | 退出码 | 耗时 | 脱敏结果/证据 |
|---|---|---:|---:|---|
| 2026-09-14T18:47Z | `tmux -L openagentx-adr008-task05-probe-1838 ...` 结构化 argv probe | 0 | <1s | `new-session -P -F` 返回内部 handle；window-scoped pane base/marker/show-options/list-panes 均符合 tmux 3.4；随后只 kill 该隔离 server |
| 2026-09-14T18:52Z | `go test -v ./internal/fleet -run 'IsolatedTmux|Workspace|BindCurrent|AttachPreflight' -count=1` | 0 | 3.27s | fake 与真实隔离 tmux 覆盖冲突、幂等、TOCTOU、补偿、多 pane、错误 pane/缺 pane 0、重复 name/marker |
| 2026-09-14T18:54Z | `go test -race ./internal/fleet ./internal/client/console ./internal/cli/console ./internal/cli/fleet -count=1` | 0 | 5.20s | 首轮受影响 package race 通过 |
| 2026-09-14T18:55Z | `go test ./... -count=1` | 0 | 13.20s | 首轮全量 Go package 通过 |
| 2026-09-14T18:55Z | `go vet ./internal/fleet ./internal/client/console ./internal/cli/console ./internal/cli/fleet && go build ./...` | 0 | 2.88s | 受影响 package vet 与全 targets build 通过 |
| 2026-09-14T18:55Z | `./scripts/check-legacy-control-paths.sh --release` | 0 | <0.1s | release scanner 全部类别 CLEAN |
| 2026-09-14T18:58Z | `go test ./internal/fleet ./internal/cli/fleet ./internal/cli/console ./internal/client/console -count=1` | 0 | 4.15s | 最终定向测试通过 |
| 2026-09-14T18:58Z | `go test -race ./internal/fleet ./internal/cli/fleet ./internal/cli/console ./internal/client/console -count=1` | 0 | 5.82s | 最终受影响 package race 通过 |
| 2026-09-14T18:58Z | `go test -v ./internal/fleet -run '^TestIsolatedTmux' -count=1` | 0 | 3.95s | 实际运行未 skip；各测试使用唯一 `tmux -L openagentx-adr008-task05-*`，覆盖 pane 0+1+2、wrong pane、缺 pane 0、重复 name/marker、旧 `agentx` 独立保留，cleanup 只 kill 对应 server |
| 2026-09-14T18:59Z | `go test ./... -count=1` | 0 | 13.11s | 最终全量 Go package 通过 |
| 2026-09-14T18:59Z | `go vet ./internal/fleet ./internal/cli/fleet ./internal/cli/console ./internal/client/console && go build -o /tmp/openagentx-adr008-task05 ./cmd/openagentx` | 0 | 2.80s | 受影响 vet 通过；临时二进制构建成功，未安装 |
| 2026-09-14T18:59Z | `/tmp/openagentx-adr008-task05 console --help` / `fleet --help` | 0 / 0 | <0.1s | help 明确 exact `OAX`、pane 0、`--agent` 不绕过 preflight 和旧 `agentx` 不迁移；无 Secret |
| 2026-09-14T18:59Z | `./scripts/check-legacy-control-paths.sh --release` | 0 | <0.1s | release scanner 最终全部类别 CLEAN |
| 2026-09-14T18:59Z | 禁用 tmux 命令/身份字段 `rg` | 0 | <0.1s | 命中仅为负向测试断言；产品代码无 `send-keys`/`paste-buffer`/`capture-pane`、pane process、pane ID、window index、kill/move |
| 2026-09-14T18:59Z | `git diff --check` | 0 | <0.1s | 无 whitespace error；提交前还将复核 staged path |

### Task 05 提交前追加审计与重验

- `2026-09-14T19:04:53Z`：将 `AttachLocation` 的内部 tmux window handle 收紧为私有 opaque
  preflight ticket。Console 调用方只能原样把 ticket 交回 `BindCurrent`，不能读取、构造或把该
  handle 用作 Agent 身份、业务 API 参数或用户输出；`CurrentPane.WindowID` 仍仅存在于
  `internal/fleet` 的结构化 tmux inspection 模型。

| 时间 UTC | 命令 | 退出码 | 耗时 | 脱敏结果/证据 |
|---|---|---:|---:|---|
| 2026-09-14T19:04Z | `go test ./internal/fleet ./internal/cli/fleet ./internal/cli/console ./internal/client/console -count=1` | 0 | 4.12s | opaque ticket 收紧后最终定向测试通过 |
| 2026-09-14T19:04Z | `go test -race ./internal/fleet ./internal/cli/fleet ./internal/cli/console ./internal/client/console -count=1` | 0 | 7.17s | 受影响 package race 通过 |
| 2026-09-14T19:04Z | `go test -v ./internal/fleet -run '^TestIsolatedTmux' -count=1` | 0 | 4.77s | 所有隔离 tmux 用例实际运行且通过；每例仅使用并清理自身唯一 `tmux -L` server |
| 2026-09-14T19:04Z | `go test ./... -count=1` | 0 | 13.49s | 全量 Go package 通过 |
| 2026-09-14T19:04Z | `go vet ./internal/fleet ./internal/cli/fleet ./internal/cli/console ./internal/client/console` | 0 | 0.11s | 受影响 package vet 通过 |
| 2026-09-14T19:04Z | `go build -o /tmp/openagentx-adr008-task05 ./cmd/openagentx` | 0 | 2.54s | 临时二进制构建成功，未安装 |
| 2026-09-14T19:04Z | `/tmp/openagentx-adr008-task05 console --help` / `fleet --help` | 0 / 0 | <0.1s | help 保持精确 `OAX`、pane 0 和旧 `agentx` 不迁移契约 |
| 2026-09-14T19:04Z | `./scripts/check-legacy-control-paths.sh --release` | 0 | <0.1s | release scanner 全部类别 CLEAN |
| 2026-09-14T19:04Z | 产品 Go 文件禁用 tmux command/identity `rg` | 1（预期零命中） | <0.1s | 无 `send-keys`、`paste-buffer`、`capture-pane`、pane process/ID、window index 或 kill/move 控制命令 |
| 2026-09-14T19:04Z | `git diff --check` | 0 | <0.1s | 无 whitespace error |

### Task 05 提交前安全点

- 状态保持 `active/WAIT`；主计划与 Task 05 front matter 保持 `pending`，Task 06 未开始。
- 实现提交将在本记录随同实现和测试创建；提交无法自包含自身 SHA，精确 SHA、actual
  post-commit clean/ahead 与监督结论由后续独立 docs-only gate record 记录。
- 测试仅使用 fake、Go 临时目录/UDS 和唯一 `tmux -L openagentx-adr008-task05-*`；所有隔离
  server 均由对应测试 cleanup kill。未访问或修改真实 `OAX`、default tmux、service、DB/socket、
  installed binary、remote feature branch 或 `steadyflow` 父仓；构建产物仅在 `/tmp`。
- Web 产品代码未修改；Task 05 计划未要求 Web 构建，未将其记为本阶段验证证据。
- Open Issue `T04-01` 继续归属 Task 07/pending，本阶段未触碰。

### Task 05 监督 NO-GO 与 review-fix

- `2026-09-14T19:25:50Z`：监督对实现提交
  `c9339bb8c8cfa35a0a2bbd74608d273eb00bd2f6` 暂定 `NO-GO`。Ubuntu 上的本地测试曾通过，
  但独立 Termux tmux 3.4 将 format 中 literal TAB 规范化为 `_`，正向 inventory/current
  解析均 fail closed；另确认全局 topology 约束误伤无关 unmanaged window，且新 window 在
  marker/pane 验证前直接启动 Console，存在真实启动竞态。Task 05 继续 `active/WAIT`，主计划和
  front matter 继续 `pending`，Task 06 未开始。
- 查询协议改为先用单字段 `#{window_id}` 列出内部 immutable handle，再按 handle 分别读取单字段
  window name、pane index 列表和 window options；current context 对 session/name/window handle/
  pane index 全部单字段读取，并在 inventory 前后完整重读比较。协议不依赖控制字符或任意
  unmanaged window name 中可能出现的分隔符，opaque `AttachLocation` 保持不变。
- topology 校验拆为 managed inventory、Fleet manifest target 和 Attach current 三层：合法 managed
  window、manifest target/overview、当前 Attach window继续要求 pane 0 与 marker 一致；重复 Agent
  marker、目标/当前位置歧义继续 fail closed；无关 unmanaged window 的缺 pane 0 和重复名称只进入
  report 并原样保留。
- 新 Agent window 先启动仅属于本次创建过程的 sentinel，设置 pane-base-index、remain-on-exit 和
  markers 后进行结构化重读；只有验证为唯一 pane 0 后，才对该已知 sentinel pane 使用允许的
  `respawn-pane -k` 启动正式 Console。配置、验证、空 command、respawn 或启动后重读失败均返回错误、
  不加入 `Created`，并保留窗口供诊断；不使用 send-keys/paste/capture，也不 kill 既有/未知 pane。

| 时间 UTC | 现象 | 根因 | 安全影响 | 纠正/结果 |
|---|---|---|---|---|
| 2026-09-14T19:18Z | 新增启动顺序集成 helper 首次只记录 `0/1`，缺 Agent marker | detached tmux helper 未指定 target，最后一次 `show-options` 读取 overview | 仅唯一 `tmux -L` 测试失败；产品与真实状态未修改 | helper 改为精确 `=OAX:=quote` 单字段查询，不使用 pane ID；同组隔离测试重跑通过 |
| 2026-09-14T19:21Z | review-fix 首轮四 package 定向测试中 Fleet/Console CLI fake 失败 | 两个上层测试双桩仍返回旧 TAB 复合记录且不支持 sentinel respawn | 产品 `internal/fleet` 和 client 已通过；CLI 在网络/mutation 前 fail closed，无外部副作用 | 仅更新测试双桩为单字段 format/respawn 模型；四 package 无缓存重跑全部通过 |
| 2026-09-14T19:23Z | current-name/空 command 收紧补丁被 `apply_patch` 拒绝 | patch 上下文顺序与当前文件不一致 | 原子拒绝，无文件部分修改 | 读取精确上下文后拆分应用；定向测试通过 |

### Task 05 review-fix 最终验证

- 提交前审计确认 sentinel 仅用于新 Agent window；overview 保持原有 shell，不被长期 sleep
  替换。新 Agent 的每个配置、验证、空 command、respawn 和启动后重读失败点均由 fake runner
  证明保留诊断 window 且不写入成功 `Created` report。
- 当前直接验证环境为 Ubuntu 24.04 compatible、tmux 3.4、Go 1.22.4。Termux 主机未由本工作树
  远程操作；同一隔离测试无需 `TMPDIR` 或 format workaround，且单元断言证明所有 `-F` 查询
  都是单字段、无 literal TAB/控制字符，消除了监督端 tmux 的已知平台差异触发条件。

| 时间 UTC | 命令 | 退出码 | 耗时 | 脱敏结果/证据 |
|---|---|---:|---:|---|
| 2026-09-14T19:28Z | `go test ./internal/fleet ./internal/cli/fleet ./internal/cli/console ./internal/client/console -count=1` | 0 | 7.78s | Task 05 四 package 无缓存定向测试通过 |
| 2026-09-14T19:28Z | `go test -race ./internal/fleet ./internal/cli/fleet ./internal/cli/console ./internal/client/console -count=1` | 0 | 9.76s | Task 05 四 package race 通过 |
| 2026-09-14T19:28Z | `go test -v ./internal/fleet -run '^TestIsolatedTmux' -count=1` | 0 | 7.50s | 六组唯一 `tmux -L` 测试全部实际运行；覆盖跨构建单字段查询、启动顺序、helper 即时退出后 window 保留、无关 unmanaged duplicate/no-pane0、既有冲突与旧 `agentx` 隔离 |
| 2026-09-14T19:28Z | `go test ./... -count=1` | 0 | 14.04s | 全仓 Go 测试通过 |
| 2026-09-14T19:28Z | `go vet ./internal/fleet ./internal/cli/fleet ./internal/cli/console ./internal/client/console` | 0 | 0.14s | 受影响 package vet 通过 |
| 2026-09-14T19:28Z | `go build ./...` | 0 | 2.60s | 全部 Go targets 构建通过，未安装二进制 |
| 2026-09-14T19:28Z | `./scripts/check-legacy-control-paths.sh --release` | 0 | <0.1s | release scanner 全部类别 CLEAN |
| 2026-09-14T19:28Z | 产品 Go 文件禁用 tmux command/identity `rg` | 1（预期零命中） | <0.1s | 无 send/paste/capture、pane process/ID、window index、既有 pane/window kill/move；唯一 `respawn-pane -k` 严格定位本次新建 sentinel pane 0 |
| 2026-09-14T19:28Z | 旧复合 tmux format/parser `rg` | 1（预期零命中） | <0.1s | 无 TAB format、`windowListFormat`、`currentFormat` 或 `parseRecords` 残留 |
| 2026-09-14T19:28Z | `git diff --check` | 0 | <0.1s | 无 whitespace error |
- review-fix 后状态继续为 Task 05 `active/WAIT`；主计划和 Task 05 front matter 继续 `pending`，
  Task 06 未开始，`T04-01` 继续归属 Task 07。
- `2026-09-14T19:30Z`：将启动 helper 的等待条件收紧为完整三行证据，并等待即时退出的 pane
  明确变为 dead，避免读取部分文件或进程退出竞态造成测试抖动；随后再次运行定向测试
  （7.63s）、race（9.39s）、六组隔离 tmux（7.51s）和 `go test ./...`（13.97s），退出码均为 0。

### Task 05 最终真实 binding integration 补证

- `2026-09-14T19:39:21Z`：监督在独立 Termux 环境完成 `e40e28b` 重验，六组
  `TestIsolatedTmux*` 与 Fleet/CLI Console/client 四 package 定向测试均通过；此前 literal TAB
  format 被规范化为 `_` 的平台失败已由单字段查询协议消除，无 `TMPDIR` 或解析 workaround。
- 新增第七组隔离 tmux integration，实际执行 unmanaged `OAX:scratch.0` 的
  `PreflightAttach -> BindCurrent`：真实 rename、managed/Agent marker、pane `0/1/2` 保留通过；
  同 Agent 再绑定返回幂等复用；异 Agent 未确认返回 `confirmation-required` 且结构化 inventory
  无变化，`confirm=true` 后真实重绑定成功。首轮单测通过，无产品实现修复。

| 时间 UTC | 命令 | 退出码 | 耗时 | 脱敏结果/证据 |
|---|---|---:|---:|---|
| 2026-09-14T19:40Z | `go test -v ./internal/fleet -run '^TestIsolatedTmux' -count=1` | 0 | 8.84s | 七组唯一 `tmux -L` integration 全部实际运行并通过，新增真实 binding 状态机用例 1.72s |
| 2026-09-14T19:40Z | `go test ./internal/fleet ./internal/cli/fleet ./internal/cli/console ./internal/client/console -count=1` | 0 | 9.31s | Task 05 四包无缓存定向测试通过 |
| 2026-09-14T19:40Z | `go test -race ./internal/fleet ./internal/cli/fleet ./internal/cli/console ./internal/client/console -count=1` | 0 | 11.14s | Task 05 四包 race 通过 |
| 2026-09-14T19:40Z | `go test ./... -count=1` | 0 | 13.94s | 全仓 Go 测试通过 |
| 2026-09-14T19:40Z | `go vet ./internal/fleet ./internal/cli/fleet ./internal/cli/console ./internal/client/console` | 0 | 0.11s | 受影响 package vet 通过 |
| 2026-09-14T19:40Z | `go build ./...` | 0 | 2.47s | 全部 Go targets 构建通过，未安装二进制 |
| 2026-09-14T19:40Z | `./scripts/check-legacy-control-paths.sh --release` | 0 | <0.1s | release scanner 全部类别 CLEAN |
| 2026-09-14T19:40Z | `git diff --check` | 0 | <0.1s | 无 whitespace error；提交前继续执行 cached path/check |

### Task 05 完成安全点与最终监督 Gate

- 实现提交：主实现 `c9339bb8c8cfa35a0a2bbd74608d273eb00bd2f6`、tmux workspace
  hardening fix `e40e28bed11abc9789c143977363e601f067e4d3`、真实 binding integration 补证
  `5468ffeb634ee5a4aed5577fbea5c1201a591cce`。
- 首次监督 `NO-GO` 的 literal TAB format 跨平台失败、无关 unmanaged topology 误阻断和
  Console marker 设置前启动竞态，已由 `e40e28b` 的单字段查询、target-aware validation 与
  sentinel 配置/验证后 respawn 修复；opaque AttachLocation、二次 preflight、补偿状态机及
  authenticated Agent list 顺序保持不变。
- 最终 gate 前的真实 binding 证据缺口已由 `5468ffeb` 补齐：隔离 `tmux -L` 中实际执行
  `PreflightAttach -> BindCurrent`，证明 rename、两个 marker、pane `0/1/2` 保留、同 Agent
  幂等、异 Agent confirmation-required 无 mutation 和确认后重绑定。无产品代码修复。
- 监督者独立 Termux tmux 3.4 最终复核确认七组 `TestIsolatedTmux*` 与四包定向测试均通过；
  Ubuntu/Termux format 差异已消除，OAX/pane/marker/binding 和非破坏边界成立。所有既有失败与
  纠正记录保留在上文。
- `5468ffeb` 提交后实际核验：feature 工作树 clean，相对 `origin/main` ahead 15；主工作树为
  clean 的 `main@c3fc1bba8ddbae3eedace0c7a32537e2f47db307`。feature 未 push，未操作
  service、真实 DB/socket/default tmux、installed binary 或 `steadyflow` 父仓。
- Open Issue `T04-01`、`T05-01` 均保持 Task 07/pending，不阻断 Task 05 gate；其中
  `T05-01` 必须在 Task 07 gate 前提供只针对 compatible managed pane 0 的安全显式 respawn 路径。
- 主计划和 Task 05 front matter 已在本 docs-only gate record 同步为 `completed`；Task 06
  保持 `pending/WAIT`，未开始实现。监督者最终结论为 Task 05 `GO`。

### Task 06 — Console 主菜单、Agent selector 与全屏 TUI

### 开始信息

- 状态：completed
- 执行者：Codex
- 开始时间（UTC）：`2026-09-14T19:52:54Z`
- feature 基线：`daf1fb0db4697d04539a108ff90f3cde57ef5a79`
- branch/worktree：`codex/adr008-implementation` /
  `/home/sky/work/touzi/OneAxe/OpenAgentX-adr008-worktree`
- 开始状态：工作树 clean，相对 `origin/main` ahead 16；监督者已明确 `GO Task 06`。
- 边界：仅实现固定 Charmbracelet 依赖、Console 主菜单/认证表单/Agent selector、真正全屏 Attach
  TUI 和最终 CLI grammar；不开始 Fleet credential、user-systemd、dead-pane respawn 或 Task 07，
  不操作真实 `OAX`/default tmux/service/DB/socket/installed binary 或父仓。
- 主计划和 Task 06 front matter 按 gate-record 协议继续保持 `pending`；监督门禁保持 `WAIT`。
- Open Issue `T04-01`、`T05-01` 继续归属 Task 07/pending，本阶段不触碰。

### Task 06 实现记录

- 依赖按冻结版本引入 Bubble Tea `v1.3.4`、Bubbles `v0.20.0`、Lip Gloss `v1.1.0`；
  `go mod tidy` 只补充其传递依赖。Console 默认 runner 使用 Bubble Tea alt-screen，未增加行式或
  自制 ANSI fallback。
- 最终 CLI grammar 仅保留 `console`、`console login`、`console logout`、
  `console attach [--agent] [--diagnostic]` 与 socket/credential path override；原子删除
  `--once`、Attach `--username`、独立 status/dispatch/steer/cancel/approve/reject/down/
  force-stop 和 Scanner REPL。menu/login/attach 在非 TTY 时于 path、credential、tmux、网络前
  code 2；自动化被引导至 Observe API。
- 新增单一 Console application service，主菜单和直接 login/logout 共用 Task 04 的 installation
  probe、authenticated session validation、Replace Login 和本地 credential 生命周期。应用内仅
  保留最新 Attach preparation；login/logout 后清除旧 authenticated client reference。
- Attach 顺序固定为 Task 05 workspace preflight、authenticated CLI session、完整分页安全 Agent
  option list、显式 Agent/compatible marker/selector 解析、`BindCurrent` 二次 preflight 与确认绑定。
  selector endpoint 每页 100 条并验证严格递增 cursor；client 自动遍历且以 10000 条显式安全上限
  fail closed，不会静默截断。SQLite 投影只返回 Agent/organization、display name、当前 Worker
  status/generation 和 fence 到当前 Worker 的 active Run status。
- 全屏 model 提供菜单、masked Login/Replace Login、Agent selector、rebind confirmation、
  Normal/Diagnostic Attach、固定 header/viewport/status/input 与 `/status`、`/help`、
  `/diagnostic` overlay。Timeline 严格限制 256 条、64 KiB、单条 2 KiB（含换行与省略号）；
  View 无 I/O，Follow/控制/resize/tick 全部经 typed Msg/Cmd。
- Follow 公开 connecting/connected/disconnected/reconnecting/retention-reattach typed 状态；继续使用
  Task 03 reducer 作为唯一状态，普通重连从 last-applied cursor 继续，retention gap 才 re-Attach。
  断线和 pending 状态禁用写入、不排队；dispatch/steer/cancel/approve/reject 各只调用正式 client
  API 一次，CAS expected version 由输入命令传入。Timeline 只消费 safe projection/结构化摘要。
- `/foreground` 和菜单 Foreground Takeover 仅显示“规划中，暂不可用”；没有 TurnHandle、tmux
  产品控制、Worker stop/drain、Foreground Runtime TTY 或 Task 07 行为。
- `README.md` 与用户安装指南同步全屏菜单、Attach/selector、非 TTY Observe API 和无 `--once`
  契约；未修改 Fleet credential、user-systemd 或 dead-pane respawn 指引。

### Task 06 测试与失败纠正

- 初始编译探针 `go test ./internal/cli/console ./internal/client/console ./internal/api/console
  ./internal/persistence/sqlite -run '^$'` 通过；随后补回并重写被最终 grammar 取代的 Console CLI
  测试，不以删除旧测试减少覆盖。
- SQLite selector 测试首次失败：fixture 在旧 Worker 仍 online 时注册新 Worker，触发合法的
  `CONFLICT: logical Agent already has an active Worker`。纠正为隔离 fixture 先将旧 Worker 标记
  offline、保留迟到 active Run，再注册 generation 2；证明旧 Run 不进入当前 selector。四包定向
  测试随后通过（约 12 秒）。
- 隔离 TTY smoke 的前三次环境失败均保留：`pane-base-index` 最初误用 session target，纠正为
  本次 window ID 的 `-w` option；zsh 将 shell 字符串中的 `=OAX` 当作 command expansion，测试
  attach target 改为普通精确 `OAX`；伪终端缺 `TERM` 导致 tmux 拒绝 clear，显式设置
  `TERM=xterm-256color`。第四次已完成真实绑定但渲染断言早于 Bubble Tea frame，加入 250ms
  渲染稳定窗口并以 Attach input、pane dead exit 0 和正式 endpoint 共同证明。最终 smoke 约
  1.14 秒通过；仅操作随机 `tmux -L` server、短路径临时 UDS/HOME/credential，未使用
  send-keys/paste/capture，pane `0/1/2`、精确 rename 和两个 marker 均保留。
- 辅助文档检查首次调用不存在的 `scripts/check_docs.py`，退出 2；后续改用仓库现有文件集合上的
  Perl relative-link 检查。禁用扫描两次仅命中 `handler.go:3` 的负向架构注释
  “never receives a Worker TurnHandle”；最终扫描显式允许该注释，其他 TurnHandle 及
  send-keys/paste-buffer/capture-pane 引用为零。
- help 后的两个 shell smoke 首次因 zsh 的只读特殊变量 `status` 退出 1；改名
  `command_status` 后，非 TTY Attach code 2/零副作用提示与旧 `console status` 拒绝均通过。
- 已通过：`go test ./internal/cli/console ./internal/client/console ./internal/api/console
  ./internal/persistence/sqlite -count=1`；`go test -race` 同四包（约 15.2 秒）；`go test ./...`
  （约 11.2 秒）；受影响包 `go vet`；`go build -o /tmp/openagentx-adr008-task06
  ./cmd/openagentx`；三个 Console help/legacy/non-TTY smoke；
  `bash scripts/check-legacy-control-paths.sh --release`；Web observation/PWA tests 与 Vite build；
  禁用控制扫描和 `git diff --check`。

### Task 06 阶段安全点（提交前）

- 实现提交：待创建；精确 SHA 由提交后只读核验记录，后续监督 gate record 再同步 completed/GO。
- 状态继续为 `active/WAIT`；主计划和 Task 06 front matter 继续 `pending`，Task 07 未开始。
- `T04-01`、`T05-01` 原样保留给 Task 07；未 push、安装、重启或操作真实 service/DB/socket/
  default tmux/installed binary/父仓。

### Task 06 监督 NO-GO 与 Console 状态流 hardening

- `2026-09-14T21:06:53Z`：监督对主实现
  `4b5d2c0675a9b00f6d48e52395710b2639b8acac` 给出 Task 06 `NO-GO`。全屏框架、菜单、selector
  和 smoke 主线保留；仅修复 reducer ack、Diagnostic SSE、绝对到期、正式控制结果和异步交互五组
  阻断，不开始 Task 07。
- 监督端独立 Termux PTY/tmux smoke 已实际进入 alt-screen、完成真实绑定并 `/quit`，但原测试在
  原始终端字节中匹配连续 `> /help`；tmux 3.4/Termux 会在文本中间插入 ANSI 控制序列，导致
  证据断言失败。测试改为状态机剥离 CSI、OSC/DCS 等终端控制序列后检查可见文本，并继续以
  正式 UDS endpoint、pane 0 正常退出、精确 window/marker 及 pane `0/1/2` 保留作为独立证据；
  未使用 default tmux、send-keys、paste-buffer 或 capture-pane。
- Follow 的 snapshot/event typed message 新增一次性 ack。callback 将消息交给 Bubble Tea 后等待
  `Update` 完成 reducer Apply；只有 ack=nil 才允许 client 推进 cursor。无效 snapshot/event 将
  reducer 错误回传 Follow，TUI 继续消费 `followDone`，context cancel 可解除等待，测试证明
  sequence 5 已入 channel 但拒绝后 reducer/client cursor 均停在 4 且 goroutine/channel 正常结束。
- Event SSE 要求唯一、大小写敏感的 `mode=normal|diagnostic`；缺失、重复或未知 mode 在 headers
  前 400。Normal 使用 viewer+`console.read` 并在发送前二次清除 Diagnostic；Diagnostic 使用
  owner+`console.diagnostic`。CLI/Web viewer/operator 均不能请求 Diagnostic；owner 只能看到既有
  限长、脱敏 safe projection。Console client、Follow 与 Web EventSource 都显式发送 mode，TUI
  Normal 视图也不会渲染 Diagnostic。
- request principal 现在携带有效截止时间：CLI 使用 Token absolute expiry，Web 使用本次认证后
  的 effective idle/absolute deadline。SSE 为截止时间建立可注入、可停止 timer，并以独立
  stream context 取消 repository 查询和循环；到期后不再发送事件。TUI tick 在本地 expiry
  立即清除认证态、置 disconnected、禁用写并提示重新 login，保留输入 draft/cursor/focus；
  后续连接状态消息不能恢复已到期会话。
- 正式 dispatch/steer API 响应补充安全的 Task/Message version 与 Task status；应用只抽取
  Task/Message/Approval/Decision ID、version、status/decision 和 mailbox sequence 到 typed
  outcome。空或非法服务端 outcome fail closed，Timeline 不保存 content/result/error/raw JSON；
  每个命令仍只调用一次正式 authenticated API。
- 异步 event/connection/control/snapshot/done 更新仅在用户原本位于 Timeline 底部时自动跟随；
  用户上翻后 offset 保持。`/status`、`/help`、`/diagnostic` 打开 overlay 前清空已消费输入，
  无效、断线或到期禁用的命令保留 draft 供修正。

| 时间 UTC | 现象 | 根因 | 安全影响 | 纠正/结果 |
|---|---|---|---|---|
| 2026-09-14T20:50Z | 首轮定向测试中 Panel SSE 用例均返回 400，应用 control 用例拒绝空 outcome | 既有 fixture 尚未携带新必需 mode，fake client 仍返回旧空响应 | 测试契约失败；无真实网络、状态或敏感数据影响 | fixture 改为显式 normal/diagnostic 与有效安全 outcome，并新增负向测试；定向测试通过 |
| 2026-09-14T20:58Z | 复核到 `time.After` 会让正常断开的长 TTL stream 保留不可停止 timer | 首版到期注入只暴露 channel，没有 cleanup ownership | 潜在本地 timer 资源滞留，不涉及授权放宽 | 改为返回 channel+stop 的 timer factory，并用 stream context 传播到 repository；cleanup 测试通过 |

### Task 06 review-fix 最终验证

| 时间 UTC | 命令 | 退出码 | 耗时 | 脱敏结果/证据 |
|---|---|---:|---:|---|
| 2026-09-14T21:03Z | `go test -race ./internal/auth ./internal/api/console ./internal/api/panel ./internal/client/console ./internal/cli/console ./internal/consolemodel ./internal/persistence/sqlite ./internal/safeoutput ./internal/controlplane -count=1` | 0 | 27.5s | Task 06 状态流、auth/SSE、client、reducer、SQLite/safeoutput 与控制服务 race 全部通过 |
| 2026-09-14T21:04Z | `go test ./... -count=1` | 0 | 15.3s wall | 全仓 Go 测试通过；包含隔离 PTY/tmux smoke |
| 2026-09-14T21:04Z | `go vet` 受影响 10 个 package 与 `cmd/openagentx` | 0 | 15.3s 并行批次 | 无 vet 诊断 |
| 2026-09-14T21:04Z | `go build -o /tmp/openagentx-adr008-task06-review ./cmd/openagentx` | 0 | 15.3s 并行批次 | 构建通过，未安装二进制 |
| 2026-09-14T21:04Z | `npm run test:observation`、`npm run test:pwa`、`npm run build` | 0 | 15.3s 并行批次 | 4 个 observation tests、PWA assertions 与 Vite 266 modules build 通过 |
| 2026-09-14T21:05Z | `go test -v ./internal/cli/console -run '^Test(IsolatedTTYSmokeUsesAltScreenBindsAndPreservesExtraPanes\|StripTerminalControlsPreservesRenderedTextAcrossANSI)$' -count=1` | 0 | 1.17s | Ubuntu tmux 3.4 唯一 `tmux -L`、短 UDS/HOME、真实 alt-screen/bind/quit 与 pane `0/1/2` 保留通过 |
| 2026-09-14T21:05Z | `bash scripts/check-legacy-control-paths.sh --release` | 0 | 2.3s 并行批次 | release scanner 全部类别 CLEAN |
| 2026-09-14T21:05Z | 产品文件禁用控制/旧 CLI `rg` | 1（预期零命中） | <0.1s | 无 send/paste/capture/TurnHandle、Scanner REPL、`--once` 或旧直接控制子命令产品路径 |
| 2026-09-14T21:05Z | `/tmp/openagentx-adr008-task06-review console --help`、旧 `console status`、非 TTY Attach smoke | 0/2/2（预期） | <0.1s | 最终 grammar 正确；旧 status 与非 TTY Attach fail closed 且未访问 workspace/credential/network |
| 2026-09-14T21:05Z | `git diff --check` | 0 | <0.1s | 无 whitespace error |

- 本轮只使用临时 UDS/HOME 和唯一隔离 `tmux -L` server；未操作真实 service、DB/socket、default
  tmux、installed binary 或 `steadyflow` 父仓。Task 06 继续 `active/WAIT`，主计划与 front matter
  继续 `pending`；Task 07 未开始，`T04-01`、`T05-01` 原样保留。
- review-fix 提交精确 SHA 由提交后只读核验并在下一次监督 gate record 中记录；不 amend
  `4b5d2c0`，feature 不 push。

### Task 06 最终 portability/compatibility review-fix

- `2026-09-14T21:25:42Z`：监督确认 `5aa6c97` 的五组状态流修复通过代码审查，但最终 gate
  继续 `NO-GO`。独立 Termux PTY smoke 中真实 TUI 已进入 alt-screen、绑定 `quote` 并正常退出，
  pane 现场无误；测试自制的 `stripTerminalControls` 未识别字符集选择序列 `ESC ( B`，把可见
  `/help` 错判为 `/Bhelp`。测试改为复用已冻结依赖 `github.com/charmbracelet/x/ansi v0.8.0`
  的 `ansi.Strip`，并加入包含该真实序列、CSI 与 OSC 的回归样本；未降低 alt-screen、正式 UDS、
  binding、退出状态或 pane `0/1/2` 证据。
- Observe SSE 保持 v1 向后兼容：完全缺少 `mode` query key 时默认为 `normal`，继续要求
  viewer/`console.read` 并强制清除 Diagnostic；显式 `mode=`、重复或未知值仍在 stream headers
  前返回 400，显式 `diagnostic` 仍要求 owner/`console.diagnostic`。Console client 和 Web 继续显式
  发送 mode。新增测试证明缺省 mode 可读取安全文本且不泄漏 Diagnostic、stderr 或未脱敏值。
- `x/ansi` 从既有 indirect dependency 提升为直接测试依赖，版本未变化，`go.sum` 未变化。
  本轮仍只使用临时 HOME/UDS、测试内 HTTP control plane 和唯一隔离 `tmux -L` server；未操作
  真实 service、DB/socket、default tmux、installed binary 或父仓。Task 06 保持 `active/WAIT`，
  主计划和 front matter 保持 `pending`；Task 07 未开始。

| 时间 UTC | 命令 | 退出码 | 耗时 | 脱敏结果/证据 |
|---|---|---:|---:|---|
| 2026-09-14T21:18Z | `go test -v ./internal/cli/console -run '^Test(IsolatedTTYSmokeUsesAltScreenBindsAndPreservesExtraPanes\|StripTerminalControlsPreservesRenderedTextAcrossANSI)$' -count=1` | 0 | 2.12s wall | 本地 tmux 3.4 真实 PTY alt-screen/bind/quit、pane `0/1/2` 与 `ESC ( B` 样本通过 |
| 2026-09-14T21:18Z | `go test -v ./internal/api/panel -run '^TestSSE(ModeAuthorizationAndValidation\|ModeSeparatesNormalAndDiagnosticSafeProjection)$' -count=1` | 0 | 4.31s wall | 缺省 normal 脱敏、显式 mode 校验及 diagnostic role/scope 边界通过 |
| 2026-09-14T21:18Z | `go mod tidy -diff` | 2 | <0.1s | Go 1.22.4 不支持 `-diff`；无文件变化，改用兼容命令纠正 |
| 2026-09-14T21:19Z | `go mod tidy` | 0 | 0.3s | 仅确认 `x/ansi v0.8.0` 为 direct dependency；`go.sum` 无变化 |
| 2026-09-14T21:22Z | Task 06 九包 `go test -race ... -count=1` | 0 | 26.0s | auth、Console/Panel API、client/TUI、reducer、SQLite、safeoutput、controlplane race 全部通过 |
| 2026-09-14T21:21Z | `go test ./... -count=1` | 0 | 17.0s wall | 全仓 Go 测试通过，包含隔离 PTY/tmux smoke |
| 2026-09-14T21:24Z | `go vet` 受影响 10 个 package 与 `cmd/openagentx` | 0 | 0.31s | 无 vet 诊断 |
| 2026-09-14T21:24Z | `go build -o /tmp/openagentx-adr008-task06-portability ./cmd/openagentx` | 0 | 2.80s | 构建通过，未安装二进制 |
| 2026-09-14T21:24Z | `npm run test:observation`、`npm run test:pwa`、`npm run build` | 0 | 1.0s 并行批次 | 4 项 observation、PWA assertions 与 Vite 266 modules build 通过 |
| 2026-09-14T21:24Z | `bash scripts/check-legacy-control-paths.sh --release` | 0 | <0.1s | release scanner 全类别 CLEAN |

### Task 06 监督门禁完成记录

- `2026-09-14T21:30:38Z`：监督最终复核确认 Task 06 `GO`。主实现
  `4b5d2c0675a9b00f6d48e52395710b2639b8acac`、状态流 hardening
  `5aa6c973abd864a3c7e80b41f4bdc422600d002c` 和 terminal compatibility
  `c42414b7a21b98bd35ab0de3949778d591705709` 的范围与验证均通过。
- 第一轮 `NO-GO` 的 reducer ack/cursor 时序、Diagnostic 越权、absolute expiry、正式控制 outcome
  和 TUI scroll/input 状态问题由 `5aa6c97` 修复；第二轮 `NO-GO` 的 Termux `ESC ( B` ANSI
  portability 与 Observe SSE 缺省 mode 兼容问题由 `c42414b` 修复。既有失败、根因和纠正记录
  均保留在上文。
- 独立 Termux 真实 PTY/tmux smoke 最终确认 alt-screen、正式 UDS、`quote` binding、`/quit`、
  pane 0 正常退出及 pane `0/1/2` 保留。Normal/Diagnostic 安全投影、session expiry、结构化控制
  outcome、全仓 Go/race/vet/build、Web 回归、release scanner 和 diff 检查证据成立。
- `c42414b` 提交后实际核验：feature 工作树 clean，相对 `origin/main` ahead 19；主工作树为 clean
  的 `main@c3fc1bba8ddbae3eedace0c7a32537e2f47db307`。feature 未 push，未操作真实 service、
  DB/socket/default tmux/installed binary 或 `steadyflow` 父仓。
- 主计划和 Task 06 front matter 已在本 docs-only gate record 同步为 `completed`；Task 07 保持
  `pending/WAIT`，未开始实现。Open Issue `T04-01`、`T05-01` 均继续归属 Task 07/pending。

### Task 07 — Fleet、user-systemd 与默认 profile 集成

### 开始信息

- 状态：completed
- 执行者：Codex
- 开始时间（UTC）：`2026-09-14T21:37:57Z`
- feature 基线：`a2ea14650a9deb668c4ab6af861dc03445329d61`
- branch/worktree：`codex/adr008-implementation` /
  `/home/sky/work/touzi/OneAxe/OpenAgentX-adr008-worktree`
- 开始状态：工作树 clean，相对 `origin/main` ahead 20；监督者已明确 `GO Task 07`。
- 边界：只集成 Fleet 默认 profile、显式 manifest/config 初始化、CLI Token lifecycle、user-systemd
  canonical unit、graceful/force 分离和 compatible managed dead pane 0 恢复；不开始 Task 08、发布、
  安装或真实运行状态操作，不修改 ADR-006/007。
- 主计划和 Task 07 front matter 按 gate-record 协议继续保持 `pending`；监督门禁保持 `WAIT`。
- Open Issue `T04-01`、`T05-01` 仅在正式 bearer lifecycle 与 dead-pane respawn 的完整隔离证据
  成立后关闭。

### Task 07 实现记录（append-only）

- 实现时间（UTC）：`2026-09-14T22:08:43Z`
- 默认 profile：Fleet 的 manifest、database、socket、Worker config directory 和 CLI credential
  全部经 `internal/localprofile` 解析；显式 flag 保持最高优先级。新增 `fleet workspace`，不扫描
  Agent/样例/测试目录，也不从 tmux 推断控制面 Agent。
- 初始化：manifest 缺失时，非 TTY 必须重复提供 `--agent`；TTY 使用经认证、完整分页的安全 Agent
  options 做明确多选。Worker config 必须已在 canonical 路径，或由
  `--worker-config agent-id=/absolute/source.yaml` 明确导入；导入前严格验证 Agent ID 和 Unix socket。
  manifest/config 使用当前用户持有的 0700 目录、0600 唯一临时文件、file fsync、Linux
  `renameat2(RENAME_NOREPLACE)` 和 directory fsync。相同内容幂等，不同内容在任何 tmux/systemd
  副作用前返回仅含路径和 SHA-256 前缀的摘要；不生成 Runtime 样例配置。
- 兼容 identity：`identity_file` 改为可选且只读校验 Agent ID、organization 和 display name；删除
  Fleet 的 Owner password、username、Web password digest 和直接 SQLite `ApplyAgent` 路径。需要先经
  正式 Agent 管理入口建立 Agent，不提供明文密码 fallback。
- CLI Token：Fleet 先 probe installation，再按 canonical socket+installation+current user 读取
  credential，发送 token 前检查本地 absolute expiry，再调用正式 `/cli/session` 核对 installation、
  token ID、username 和 expiry。401/本地过期删除本地副本；installation mismatch 不发送 token。
  `init/up/down/force-stop` 要求 owner+`fleet.lifecycle`，`workspace/status` 要求 viewer+`console.read`。
- user-systemd：新增 `deploy/systemd/openagentx-worker-user@.service`，安装后名称为
  `openagentx-worker@.service`，实际 argv 固定为 `%h/.local/bin/openagentx worker run --config
  %h/.openagentx/workers/%i.yaml`。Fleet 所有 systemctl 调用显式带 `--user`；`up` 在任何 mutation
  前读取已加载 unit 的结构化 `LoadState/ExecStart` 并与 canonical config 精确比较。Linger 仅调用
  `loginctl show-user ... --property=Linger --value` 并提示，不 enable daemon/unit/linger。可选 Worker
  `.env` 要求当前用户持有、普通文件且权限不宽于 0600；user Worker 不用会阻止显式 workspace 写入
  的 ProtectHome/ProtectSystem 限制。
- workspace：结构化 inventory 增加仅对 compatible managed Agent pane 0 的 `pane_dead` 查询。
  `--respawn-dead` 经过第二次全量 preflight 后，只对名称、两个 marker、pane 0 和 dead 状态仍一致的
  内部 window handle 执行不带 `-k` 的 `respawn-pane`；live pane 竞态由 tmux fail closed，pane 1+、
  unmanaged/orphaned 和未知进程不读取内容、不 kill、不 respawn。新 pane 命令只含正式
  `console attach --socket ... --credentials ... --agent ...` 路径，不含用户名、密码或 token。
- 生命周期：`fleet down` 继续先为全部 online Agent 持久化 stop intent，再无限观察 busy/draining/
  idle/offline；context cancel 只停止观察。`force-stop` 独立要求 `--confirm-force-stop` 与
  `--confirm-active-run-uncertain`，不调用 systemctl stop，也不伪装 graceful。

### Task 07 测试与失败纠正

- 首次定向编译中，旧 Fleet 测试仍引用 `ReadPassword/SystemdConfigPath`，Console/Tmux fake 尚未响应
  新的 `pane_dead` 查询；同时原子写故障测试的临时目录权限受测试环境 umask 影响。已将测试迁移到
  probe→credential→session 夹具、补结构化 pane 状态，并显式建立 0700 测试目录。随后四包定向测试通过。
- workspace dead→live TOCTOU 测试首次在第二次 inventory 时已观察到 live，正确幂等复用而未进入
  respawn；测试调整为在 respawn 内部第三次 preflight 注入转换，证明无 `respawn-pane` mutation。
- `systemd-analyze verify` 首次在源码目录同时解析同名系统级 `openagentx.service`，未实例化的
  `User=%i` 导致退出 1。改用临时目录按实际用户安装名放置 `openagentx.service` 和
  `openagentx-worker@.service` 后，`systemd-analyze --user verify` 退出 0；未启动或重载真实 unit。
- `python3 scripts/check_docs.py` 退出 2，因为仓库仍不存在该 README 历史引用脚本；这与 Task 06 已记录
  的既有缺口一致。本轮文档改动改用 whitespace、文件路径和相对链接只读检查，不把缺失脚本声称为通过。
- 已通过：Task 07 四包定向测试；新增 Fleet/workspace/Worker race；`go test ./...`；受影响包
  `go vet`；`go build -o /tmp/openagentx-adr008-task07 ./cmd/openagentx`；隔离随机 `tmux -L` 的全部
  integration（含 pane 0/1/2、dead respawn、Console/tmux 退出后独立 Worker 存活）；Worker unit 静态
  shell 断言；按安装名的 `systemd-analyze --user verify`；Web observation/PWA tests 和 Vite build；
  release scanner、禁用 tmux 控制扫描、旧 Fleet password/username 扫描、help smoke 和 `git diff --check`。
- 测试全部只使用临时 HOME/config/credential、fake UDS/session/systemd/loginctl 和唯一 tmux server；
  未读取或修改真实 credential/token、service、DB、socket、默认 tmux、installed binary 或父仓库。
- `T04-01` 关闭证据：Fleet production package 中不存在密码读取、password Login、owner username flag 或
  直接 DB identity apply；role/scope/expiry/401/installation replacement 与 argv/输出不含 secret 均有测试。
- `T05-01` 关闭证据：fake runner 和真实隔离 tmux 均证明只有 compatible managed dead pane 0 可显式
  恢复，live/竞态/pane 1+/unmanaged 不 mutation，Console/tmux 退出不影响独立 Worker。
- 当前 Task 07 仍为 `active/WAIT`；主计划和 Task 07 front matter 按 gate-record 协议保持 `pending`，
  等待阶段实现提交后的监督复核。Task 08 未开始。

### Task 07 host consistency 监督 review-fix（append-only）

- `2026-09-14T22:36:00Z`：监督对 Task 07 主实现
  `40fe06e813dcce5c327cf159c66fff630d3bc236` 给出临时 `NO-GO`；Fleet/credential/graceful/dead-pane
  主线保留，只修 tmux fixture 稳定性、配置捕获原子性和 user-systemd host consistency，不开始
  Task 08。
- 监督端完整 `go test ./internal/fleet` 超过 30 秒时，辅助 pane 的 `sleep 30` 自然结束并让 pane 2
  消失，造成“未确认 binding 发生 mutation”的误报。全部隔离 tmux Console、辅助 pane、unmanaged
  window 和独立 Worker fixture 已统一改为 `sleep 86400` sentinel，仅由各测试唯一 `tmux -L`
  server 的 `kill-server` 或显式 process cleanup 结束；Ubuntu 24.04/tmux 3.4 完整包和 race 均通过，
  不再依赖测试套件在固定秒数内完成。Termux 由监督端在本提交后独立重验。
- Worker config 新增严格 `io.Reader` decode 入口，拒绝 unknown field、尾随 YAML document 和无效领域
  配置；文件入口复用该 decoder，并以 `O_NOFOLLOW` 打开和对已打开 fd 做 regular/size 检查。Fleet
  对 source、canonical Worker config、manifest 和可选 env 使用 `O_NOFOLLOW + fstat + owner/mode +
  size` 单句柄捕获；显式 source 可为当前用户持有的较宽 mode，但不能是 symlink/非普通文件。
  初始化只校验将安装的捕获 bytes，导入时拒绝会因复制改变语义的已知相对文件路径；原子写继续使用
  0600 临时文件、fsync、`RENAME_NOREPLACE` 和 directory fsync，不覆盖冲突目标。
- 故障测试证明：初始 source symlink 拒绝；source 在捕获后变化时安装内容仍精确等于已验证 bytes；
  canonical 在 rename 前被 symlink 占位时 fail closed、外部 target 和 manifest 均不改变；manifest
  symlink/0644 拒绝；相对导入路径拒绝；原子失败继续清理临时文件并保留旧有效文件。
- user Worker unit 从 `Requires=openagentx.service` 改为 `Wants=` 加 `After=`，避免 daemon stop/restart
  通过依赖关系强停 resident Worker。`fleet up` 在任何 tmux/start mutation 前验证固定 canonical
  `%h/.local/bin/openagentx` 是当前用户持有、非 symlink、非 group/other writable 且 owner-executable
  的普通文件；实际 user manager 的 `ExecStart`、`WorkingDirectory` 和 `EnvironmentFiles` 必须分别
  精确匹配 canonical binary/config、用户 home 和 `%h/.openagentx/workers/%i.env`，否则 fail closed。
  workspace pane command 与 user unit 使用同一 binary，不再使用 `os.Executable()` 临时路径。
- Web 回归首次从仓库根运行 `npm run test:observation`、`npm run test:pwa` 和 `npm run build`，均因
  根目录没有 `package.json` 退出 `254`；纠正到 `web/` 后三项均退出 `0`，分别通过 4 项 observation、
  PWA assertions 和 Vite 266 modules build。该失败是验证命令工作目录错误，未修改产品或外部状态。
- 本轮通过：四包定向测试（含完整隔离 tmux）`10.9s`；三包 race `12.6s`；`go test ./...`
  `15.0s`；受影响包 vet、Go build、shell syntax、Worker unit 静态断言、临时目录中按实际安装名执行的
  `systemd-analyze --user verify`、Web 三项、release scanner 和 `git diff --check`。所有文件、HOME、
  UDS、tmux server 和 unit 验证均为临时/隔离输入；未操作真实 service、DB/socket、default tmux、
  installed binary 或父仓。
- 最终串行重验中的第一次 `go test ./... -count=1` 在既有 Task 06 PTY smoke 失败：TUI 已绑定并处理
  `/quit`，tmux 输出为 `0:1:`，证明 pane 0 已 dead，但当次 `pane_dead_status` 为空而测试要求
  `0:1:0`，因此约 11.5 秒后退出 1。未改 Task 06 产品或测试；立即单独重跑
  `TestIsolatedTTYSmokeUsesAltScreenBindsAndPreservesExtraPanes` 在 1.16 秒退出 0，随后再次
  `go test ./... -count=1` 在 14.1 秒退出 0。
- `T04-01`、`T05-01` 从主实现中的提前 closed 恢复为 `reopened/pending`；既有实现证据保留，只有本
  review-fix 提交与最终独立重验通过后才可在 Task 07 gate record 中关闭。Task 07 继续
  `active/WAIT`，主计划/front matter 继续 `pending`，Task 08 未开始。

### Task 07 preflight ordering 最终监督 review-fix（append-only）

- `2026-09-14T22:50:57Z`：监督确认 `f5c0d0d549931e4104bc7248c5e5714305a242d9` 的三组
  host consistency hardening 代码审查通过，Termux 默认环境完整 `go test ./internal/fleet`
  已稳定通过，耗时约 99 秒；Task 07 gate 仍为 `NO-GO`，只修测试 umask 可移植性和 init
  preflight 顺序，不进入 Task 08。
- Termux 默认 umask 会把测试 `os.WriteFile(..., 0722)` 收窄，使文件实际不再 group/other writable，
  原测试却继续要求产品拒绝。fixture 现在写入后显式 `os.Chmod(binary, 0722)`，先通过 `os.Stat`
  断言实际 mode 精确为 0722，再验证 `canonicalUserBinary` fail closed；产品对不安全 binary 的检查
  未放宽。
- `fleet init` 原先在 manifest 缺失时先原子安装 Worker config 和 manifest，之后才调用
  `newWorkspace()` 验证 canonical binary；missing/non-executable binary 会返回失败但已留下文件。
  现在 `init/workspace/up` 在认证、manifest 检查和任何 config/manifest/tmux/systemd mutation 前
  构造并验证 workspace，后续阶段复用该已验证 builder。`status/down` 不执行 binary preflight，
  保持观察与 lifecycle 命令在 binary 缺失时可用。
- 新增表驱动回归证明 missing 和 non-executable binary 均使 `fleet init` 返回失败，且 canonical
  Worker config、manifest 和 tmux calls 全部为空；missing binary 下 `workspace/up` 的 tmux 和
  systemctl calls 也全部为空。另有测试删除 binary 后分别执行 `fleet status` 和 `fleet down`，
  两者均成功。umask 回归、init 零副作用、host 零 mutation 和 status/down 独立性定向测试均通过。
- 本机 Ubuntu 24.04/tmux 3.4 验证通过：`go test ./internal/cli/fleet ./internal/fleet -count=1`
  （含唯一 `tmux -L` 全部隔离集成）；Task 07 三包 race；`go test ./... -count=1`；受影响包 vet；
  Go build；shell syntax；Worker unit 静态断言；临时目录、实际安装名下的
  `systemd-analyze --user verify`；Web observation/PWA/build；release scanner 和 diff check。
  本轮未操作真实 service、DB/socket、default tmux、installed binary 或父仓。
- review-fix 提交精确 SHA 由提交后只读核验，并由下一次监督 gate record 记录；不 amend
  `f5c0d0d`，feature 不 push。Task 07 继续 `active/WAIT`，主计划/front matter 继续 `pending`，
  `T04-01`、`T05-01` 继续 `reopened/pending`，Task 08 未开始。

### Task 07 监督门禁完成记录

- `2026-09-14T23:02:54Z`：监督最终复核确认 Task 07 `GO`。主实现
  `40fe06e813dcce5c327cf159c66fff630d3bc236`、host consistency hardening
  `f5c0d0d549931e4104bc7248c5e5714305a242d9` 和 preflight ordering
  `e13db7b7e505cb77c1359c776b9c3c473322062f` 的范围与证据均通过。
- 第一轮 `NO-GO` 的 30 秒 tmux fixture、captured-bytes/TOCTOU、user-systemd residency、实际 unit
  属性和 canonical binary 缺口由 `f5c0d0d` 修复；第二轮 `NO-GO` 的 Termux umask fixture 与
  init 文件副作用早于 binary preflight 问题由 `e13db7b` 修复。既有失败和纠正记录全部保留。
- 监督端 Termux 最终确认完整 Fleet/CLI/Console/Worker package 通过，`internal/fleet` 约 104 秒；
  Ubuntu 侧已确认 Task 07 定向/race、全仓 Go、隔离 tmux、systemd static/analyze、vet/build、Web、
  release scanner 和 diff check。T04-01 的 CLI Token lifecycle 与 T05-01 的 compatible dead pane 0
  安全 respawn 证据完整，本 gate 关闭两项。
- `e13db7b` 提交后实际核验：feature 工作树 clean，相对 `origin/main` ahead 23；主工作树为 clean 的
  `main@c3fc1bba8ddbae3eedace0c7a32537e2f47db307`。feature 未 push，未操作真实 service、DB/socket、
  default tmux、installed binary 或 `steadyflow` 父仓。
- 主计划和 Task 07 front matter 已在本 docs-only gate record 同步为 `completed`；Task 08 保持
  `pending/WAIT`，未开始实现或发布。

### Task 08 — 集成审查、实机候选与发布门禁

### 开始信息

- 状态：active
- 监督门禁：WAIT
- 执行者：Codex
- 开始时间（UTC）：`2026-09-14T23:11:53Z`
- feature 基线：`f16496c9da33c2ee8013b6b61f719ddc358e3b41`
- branch/worktree：`codex/adr008-implementation` /
  `/home/sky/work/touzi/OneAxe/OpenAgentX-adr008-worktree`
- 开始状态：工作树 clean，相对 `origin/main` ahead 24；监督者已明确 `GO Task 08`。
- 边界：只执行最终集成审查、隔离候选验证、独立 cache 构建和目标主机只读核验；不部署、
  不 merge/push、不安装候选、不重启服务，不修改真实 DB/socket/default tmux 或父仓。
- 主计划和 Task 08 front matter 按 gate-record 协议保持 `pending`；ADR-008 继续为已接受、尚未完成
  实施且未部署。Task 07 总体状态表的遗留 `active/WAIT` 同步为已通过 gate 的 `completed/GO` 事实。

### Task 08 首轮集成审查 NO-GO（append-only）

- `2026-09-14T23:18:02Z`：完成 `origin/main..f16496c` 的 24 提交/87 文件范围盘点、逐提交与完整
  diff whitespace 检查、ADR/生成物/禁用 tmux 控制边界审查，并建立 ADR-008 条款到代码、测试、
  文档的 traceability matrix。未发现 ADR-006/007 决策正文、父仓文件、生成物或 Secret 混入。
- 最终 SSE 语义审查发现 `T08-01`：`internal/api/panel/handler.go` 的 Agent filter 把关联状态查询
  错误折叠为不匹配并推进 `after`；Run 安全投影在 Run/Worker 查询错误或投影无效时返回 `nil`，
  调用方仍发送事件并推进 `after`。普通重连会从已跨过的 sequence 之后开始，无法恢复未应用状态。
- 现有 Worker Backend projection 故障测试正确证明查询失败时不发送/不跨过，但没有覆盖 Agent
  filter 和 Run projection 的同类失败注入。该产品缺陷归属 Task 03，必须独立 review-fix 并经过
  监督 gate；Task 08 文档提交不得夹带代码。
- 按“任一审查/验证失败即 `no-go` 并停止”规则，本轮没有继续全量/race、Web/systemd/release、
  隔离运行场景、独立候选构建或 `rtx4090` 只读核验；未执行项全部保留，不能冒充候选证据。
- validation report：
  [2026-09-14-openagentx-adr008-release-candidate.md](../../reports/validation/2026-09-14-openagentx-adr008-release-candidate.md)，
  结论 `no-go`。Task 08 保持 `active/WAIT`，主计划与 Task 08 front matter 保持 `pending`；未部署、
  未 merge/push、未安装/重启，未操作真实 DB/socket/default tmux/systemd 或父仓。

### T08-01 / Task 03 cursor safety review-fix（append-only）

- `2026-09-14T23:39:44Z`：监督授权只修 `T08-01`，归属 Task 03；不恢复 Task 08 其余候选矩阵。
  `eventMatchesAgent` 现在返回 `(matched, error)`，只有明确的合法其他 Agent 才返回
  `false, nil`；Task、Run/runtime、Worker、Message、Approval 及 Message/Approval 所属 Task 的
  任一查询错误（包括 `ErrNotFound`）都会在当前事件写出和 `after` 推进前结束 stream。
- Worker 归属由 `ListWorkers(1000)` 改为精确 `GetWorkerInstance`，消除截断导致旧 Worker 静默漏判的
  风险。`runEventSnapshot` 改为返回 `(*RunAttemptReadModel, error)`；Run、所属 Worker 查询和
  Run/Worker identity、generation、status 校验失败均不再返回空 projection 继续。
- SSE 对安全 runtime event projection、JSON marshal 和 frame write 全部检查 error，且仅在 frame
  成功写出后推进 `after`。服务端生成的合法无 safe-output runtime event 仍返回空 Output，不被误判
  为损坏；畸形 envelope 或存在但不可安全投影的 payload 才 fail closed。
- 新增表驱动故障注入：Task、Run、runtime、Worker、Message、Message Task、Approval、Approval Task
  ownership；Run 二次查询、Run Worker generation 查询、Worker 二次精确查询、Backend 查询；Run/
  Worker 无效 identity/generation/status；runtime projection 与 JSON 编码错误。每项均断言失败事件 N
  和后续 N+1 未发送，并以原 last-applied cursor 的下一请求恢复 N、N+1；同时断言 Worker 路径从未
  调用 `ListWorkers`。
- 实现中间自审发现“缺少 safe-output payload”可能是合法的无展示 runtime event，而非投影错误；
  在最终验证前纠正为 `nil, nil` 并增加兼容测试，没有用空 projection 掩盖畸形 payload。
- 验证结果：
  - `go test ./internal/api/console ./internal/api/panel ./internal/client/console ./internal/persistence/sqlite/... ./internal/safeoutput/... ./internal/cli/console ./internal/consolemodel -count=1`：exit 0，15.89s；
  - 同包 `go test -race ... -count=1`：exit 0，35.79s；
  - `go test ./... -count=1`：exit 0，18.47s；
  - `go vet ./...`：exit 0，0.71s；
  - `go build -o /tmp/openagentx-t0801-final-check ./cmd/openagentx`：exit 0，2.95s，临时产物已删除；
  - `bash scripts/check-legacy-control-paths.sh --release`：exit 0，0.13s，全部类别 `CLEAN`；
  - `git diff --check`：exit 0。
- 临时 build 首次清理尝试 `rm -f /tmp/openagentx-t0801-check` 被工具安全策略拒绝、未删除文件；改用
  精确 `unlink` 后成功，并对最终 build 产物同样使用精确 `unlink`，两路径均确认不存在。
- 首次 log 状态一致性 `rg` 命令因 pattern 中反引号未使用 shell-safe 引用而尝试执行
  `active/WAIT`，输出 `no such file or directory`；改用单引号 pattern 后 exit 0，确认 Task 08
  `active/WAIT`、validation `no-go` 与 `T08-01 open` 一致。该失败未读取或修改外部状态。
- 本 review-fix 只涉及 Panel handler、Panel 测试与本 append-only log；validation report 保持
  `no-go`，`T08-01` 保持 open，Task 08 保持 `active/WAIT`，等待监督复核。未 push、部署、安装、
  重启或操作真实 service/DB/socket/default tmux/systemd/父仓。

### T08-01 监督关闭与 Task 08 恢复验证（append-only）

- `2026-09-14T23:52:33Z`：监督复核确认 `b26c9c4928d5c1cbdd971473cecb5b535a3d39dc`
  通过；归属、Run、Worker、Backend、安全输出、JSON encode 和 frame write 失败均不会发送当前事件或
  推进 cursor，独立定向重验通过。`T08-01` 关闭，Task 08 获准从完整矩阵重新执行。
- 恢复基线为 clean 的 `codex/adr008-implementation@b26c9c4`，相对 `origin/main` ahead 26；重新盘点
  为 26 commits、88 files、15094 insertions、1643 deletions。26 个提交逐一 `git show --check`、
  完整 diff whitespace、ADR-006/007、生成物、禁用 tmux 控制和 Secret 差异审查通过。
- 无缓存矩阵已执行部分：`go mod verify` exit 0（1.03s）；Web observation 4/4 exit 0（0.65s）、PWA
  exit 0（0.36s）、build 266 modules exit 0（1.48s）；release scanner exit 0（0.14s），全部类别
  `CLEAN`。Web build 只产生 `.gitignore` 已覆盖的 `web/dist`，工作树仍 clean。
- `go test ./... -count=1` 首次执行 exit 1（17.94s）。唯一失败为
  `TestIsolatedTTYSmokeUsesAltScreenBindsAndPreservesExtraPanes`：真实隔离 `tmux -L` 已进入 alt-screen、
  绑定 `quote` 并收到 `/quit`，但 pane 状态在 10 秒观察窗结束时仍为 `0:1:`、`1:0:`、`2:0:`，pane 0
  未成为 dead+exit 0。该次失败登记 `T08-02`，归属 Task 06 的 TUI/Follow/PTY 确定性退出状态机。
- 按 Task 08“任一全量/隔离检查失败即 no-go 并停止，禁止重复直到偶然绿色”规则，没有重跑该测试，
  也没有继续 race、vet、剩余隔离场景、systemd analyze、detached 候选构建或 `rtx4090` 只读核验。
  当前 validation report 保持 `no-go`，Task 08 保持 `active/WAIT`；未修改产品代码或真实状态。

### T08-02 / Task 06 Console PTY shutdown review-fix（append-only）

- `2026-09-15T00:05:00Z`：监督授权只修 `T08-02`，归属 Task 06；Task 08 保持
  `active/NO-GO`，本修复完成后不恢复 Task 08 其余候选矩阵。
- 诊断确认产品 Update 路径在 Attach binding 完成时调用 `m.input.Focus()`，只在独立
  `tea.KeyEnter` 上执行当前 draft，`/quit` 解析后直接返回 `tea.Quit`。失败 smoke 只等待 SSE
  connected，随后固定等待 250ms、单次写入 `/quit\r` 并立即关闭 stdin；terminal 已显示 draft
  但不能证明输入 focus 和 Enter 被作为独立 key 处理。测试还在 PTY 写入期间并发读取
  `bytes.Buffer`，两个辅助 pane 使用仅 30 秒的进程。
- `internal/cli/console/smoke_test.go` 改用 mutex 保护的 terminal recorder；SSE connected 后继续等待
  `ansi.Strip` 输出出现 Attach 输入 placeholder `> /help`，证明输入画面 ready。测试逐字符写入
  `/quit`，每键间隔 30ms，再等待 100ms 单独写 Enter；在 pane 0 被结构化证明为
  `pane_dead=1/pane_dead_status=0` 前不关闭 stdin。没有 kill/respawn pane，也没有放宽 alt-screen、
  正式临时 UDS/auth、OAX bind 或 pane 1/2 保留断言。
- script cleanup 通过 `sync.Once` 在所有成功/失败路径关闭 stdin、终止并 wait 本测试创建的
  `script` 进程；唯一 `tmux -L` server 仍由测试 cleanup 的 `kill-server` 回收。辅助 pane 改用
  86400 秒 sentinel，仅依赖隔离 server cleanup，不再要求整套测试在 30 秒内完成。
- 修复后 Ubuntu 验证：
  - 单次 smoke + ANSI strip：`go test ./internal/cli/console -run
    '^Test(IsolatedTTYSmokeUsesAltScreenBindsAndPreservesExtraPanes|StripTerminalControlsPreservesRenderedTextAcrossANSI)$'
    -count=1`，exit 0，package 1.171s；
  - 真实 PTY smoke 连续十次：`go test ./internal/cli/console -run
    '^TestIsolatedTTYSmokeUsesAltScreenBindsAndPreservesExtraPanes$' -count=10`，exit 0，package 11.706s；
  - Console package 三轮：`go test ./internal/cli/console -count=3`，exit 0，package 3.578s；
  - Console race：`go test -race ./internal/cli/console -count=1`，exit 0，package 3.326s；
  - 全仓无缓存确认：`go test ./... -count=1`，exit 0，17.89s；所有 package 通过。
- 真实键序下连续验证均正常退出，因此没有修改产品 TUI/Follow cancellation。`gofmt` 和
  `git diff --check` 通过。validation report 继续为 `no-go`，`T08-02` 保持 open，Task 08 保持
  `active/WAIT`，等待监督端 Termux 独立复核；未 push、安装、重启或操作真实 service/DB/socket/
  default tmux/systemd/父仓。

### T08-02 compact pane 根因修复（append-only）

- `2026-09-15T00:22:15Z`：监督端 Termux 在 `442bc058046d688f6a83654dc10326c5717b3300`
  上再次失败，且失败提前到 10 秒内未连接 fake SSE。window 已从 `scratch` 正式绑定为 `quote`，但
  terminal 持续重复 tmux 重绘且未出现 Attach 输入 placeholder，证明键序修复不是完整根因。
- 审查确认三 pane 纵向布局使 Termux pane 0 内容高度约 5 行，而 `resize` 将 model height 强制提升到
  8，`View` 和 overlay 又以至少 8 行渲染；header、viewport、status 和三行 textarea 的输出高于真实
  PTY。持续滚动/重绘可延迟 Bubble Tea typed message 与 Follow ack，且直接违反窄终端布局不变量。
- `internal/cli/console/tui.go` 现在保存实际 `WindowSize`（最小仅为一个 cell），高度低于 8 时将输入
  缩为一行，并按实际高度动态分配 viewport。Attach 在高度 4+ 显示 header/viewport/status/input，
  高度 3 显示 header/status/input，高度 2 显示 header/input，高度 1 保留可编辑 input。
- 所有 screen 的最终 View 使用 `ansi.Truncate` 按实际列宽裁剪，并按实际高度截断行数；不再使用
  `.Height(max(8, ...))`。小于 20x6 的 overlay 使用同一安全紧凑视图，较大 overlay 的 box/place 也受
  实际宽高和最终裁剪约束。实现仍是 Bubble Tea model/update/view，没有行式 REPL 或 ANSI fallback。
- `internal/cli/console/tui_test.go` 新增纯 model 回归：`80x5`、`20x3`、`8x1`、`1x1` 下 Attach 和
  menu/login/loading/selector 全部 screen，以及 status/help/diagnostic/confirmation/error overlay，
  经 ANSI strip 后行数不超过实际高度且显示宽度不超过实际宽度。另证明 compact resize 后输入
  focus/draft/cursor 不变，snapshot ack 成功，event ack 成功且 reducer cursor 从 1204 推进到 1205。
- 保留 `442bc05` 的三 pane smoke、线程安全 recorder、placeholder ready、逐字符输入和独立 Enter；
  没有改水平 split、增加 pane 高度、放宽 pane 0 exit status 0 或 pane 1/2 保留断言。
- Ubuntu 最终验证：compact 纯 model tests exit 0（0.020s）；真实三-pane PTY smoke `-count=10`
  exit 0（package 11.696s）；Console package `-count=3` exit 0（3.612s）；Console race exit 0
  （3.386s）；`go test ./... -count=1` exit 0（18.19s），全仓 package 通过；
  `go vet ./internal/cli/console` exit 0（0.29s）。
- validation report 仍为 `no-go`，`T08-02` 仍 open，Task 08 仍 `active/WAIT`；等待监督端 Termux
  重验。本轮不恢复 Task 08 候选矩阵，未 push、安装、重启或操作真实 service/DB/socket/default
  tmux/systemd/父仓。

## 7. Open Issues

| ID | 首次发现时间 | Task | 严重度 | 问题 | Owner | 状态/处置 |
|---|---|---|---|---|---|---|
| T04-01 | 2026-09-14T17:45:58Z | 07 | P2 | Fleet down/force-stop 仍是 Task 07 的 credential 集成范围；Task 04 后共享 client 的旧直接密码 Login 会在网络前 fail closed，避免从 Fleet 向 UDS login 发送密码或替换 Console Token | Task 07 | closed；[Fleet credential/lifecycle 测试](../../../internal/cli/fleet/command_test.go)证明 installation-bound session、owner+lifecycle scope、无密码路径和 graceful/force 分离，监督双平台复核通过 |
| T05-01 | 2026-09-14T19:39:21Z | 07 | P2 | Console 进程退出后，compatible managed window 的 pane 0 由 `remain-on-exit` 保留为 dead；Task 07 必须提供安全、显式且只针对 compatible managed pane 0 的重新进入/respawn 路径，不得触碰 pane 1+ 或未知进程 | Task 07 | closed；[真实隔离 tmux 测试](../../../internal/fleet/workspace_integration_test.go)与 [workspace 状态机测试](../../../internal/fleet/workspace_test.go)证明二次 preflight 只 respawn compatible dead pane 0，并保留 live/pane 1+/unmanaged 现场 |
| T08-01 | 2026-09-14T23:18:02Z | 03 | P0 | Observe SSE Agent filter/Run projection 查询或校验失败时会推进 event cursor，可能永久跳过未应用状态 | Task 03 | closed；`b26c9c4` 以精确归属查询和 projection/encode/write fail-closed 修复，故障注入证明失败事件与 N+1 不跨 cursor，监督独立复核通过 |
| T08-02 | 2026-09-14T23:52:33Z | 06 | P0 | 真实 PTY/tmux smoke 收到 `/quit` 后 pane 0 未在 10 秒内退出，候选无法证明 Console detach 确定性 | Task 06 | open；需定位 TUI、Follow 与 PTY 退出协调并提供 Ubuntu/Termux 确定性回归，禁止靠重复测试偶然变绿 |

## 8. 安全与范围事件

| 时间 UTC | 事件 | 影响 | 立即动作 | 监督结论 |
|---|---|---|---|---|
| 2026-09-14T14:16:32Z | 首次按 `origin` HTTPS URL 推送时因无交互凭据失败；远端未改变 | 无产品/运行状态影响 | 改用仓库既有 GitHub SSH 身份推送，并以 `ls-remote` 验证 `main=16291c7` | 计划发布成功；保留失败记录 |
| 2026-09-14T14:16:32Z | 计划创建与发布仅改变 `docs/plans/` | 无实现或运行状态变化 | `git diff --check`、链接/语义覆盖和 staged-path 边界检查通过 | 待远端同步复核 |
| 2026-09-14T14:30:35Z | P0 四项受保护修改独立提交并推送 `main` | 远端 main 前进到 `c3fc1bb`；无 ADR-008 文件或运行状态修改 | `ls-remote` 验证 SHA，确认主工作树干净 | P0 GO |
| 2026-09-14T14:31:31Z | 从最新 `origin/main` 创建 sibling worktree 和 `codex/adr008-implementation` | ADR-008 文档工作与 main/父仓隔离 | 核对 branch、HEAD、tracking 和 clean status | Task 01 执行中，门禁 WAIT |
| 2026-09-14T14:57:22Z | 监督复核通过 P0 与 Task 01；发现计划状态和提交证据不一致 | 仅文档 gate 状态不一致；产品、runtime 和远端 feature 未变化 | 独立 docs-only gate correction 记录 `35bd445`、实际 status 与 `GO`；Task 02 保持 `pending/WAIT` | Task 01 GO；等待 Task 02 单独授权 |
| 2026-09-14T15:54:57Z | 监督最终复核 Task 02 主实现与 review-fix | 首次 Termux UDS 长路径失败已由 `c0e8d4a` 修复；最终默认 `TMPDIR` 和其余定向测试/vet 均通过 | 核验主实现 13-file、fix 2-file 范围及无产品语义偏移；同步 docs-only gate record | Task 02 GO；Task 03 保持 WAIT |
| 2026-09-14T17:07:08Z | 三个 Task 03 实现/review-fix 提交及独立验证均通过 | 两轮 NO-GO 缺口已分别由 `9e18246`、`2f97727` 修复；无剩余 Task 03 阻断 | 记录三个精确 SHA、实际 clean/ahead 与最终结论；仅同步 docs gate 状态 | Task 03 GO；Task 04 保持 `pending/WAIT` |
| 2026-09-14T17:45:58Z | Task 04 实现与隔离验证完成，等待阶段提交 | CLI Token、v1 ensure、UDS/Web auth 隔离、credential store 和 Console bearer 路径已形成最小闭环；Fleet 集成未越界 | 保留两次代码测试失败和两次 Web 环境/命令失败及纠正；最终 Go/race/Web/release/diff 通过 | Task 04 保持 active/WAIT，提交后停止等待 gate |
| 2026-09-14T18:16:09Z | 监督对 `c240aa4` 给出 Task 04 临时 NO-GO | 发现 Observe CLI scope 过宽、credential 生命周期校验/清理不完整、跨进程并发原子性缺口；核心模型/schema/mux 分离结论不变 | 仅在 Task 04 范围实现 scope 白名单、authenticated session validation、严格 bounded credential document 与安全 `flock`，追加测试和日志；未触碰真实状态 | Task 04 保持 `active/WAIT`；等待 review-fix 提交后的再次 gate |
| 2026-09-14T18:26:41Z | 监督最终复核 Task 04 主实现与 hardening fix | 独立审查确认 schema/auth/mux/store/session/scope/并发安全边界全部成立；`T04-01` 明确留给 Task 07 | 记录两个精确 SHA、实际 clean/ahead、首次 NO-GO 修复和最终结论；仅同步三份 docs gate 状态 | Task 04 GO；Task 05 保持 `pending/WAIT` |
| 2026-09-14T19:46:50Z | 监督最终复核 Task 05 三个提交 | 独立 Termux tmux 3.4 七组真实集成最终通过；主实现、hardening 和真实 binding 补证范围均通过；`T04-01`、`T05-01` 保留给 Task 07 | 记录三个精确 SHA、实际 clean/ahead、首次 NO-GO 三项修复和最终真实 binding 证据；仅同步三份 docs gate 状态 | Task 05 GO；Task 06 保持 `pending/WAIT` |
| 2026-09-14T21:30:38Z | 监督最终复核 Task 06 三个提交 | 两轮 NO-GO 分别由 `5aa6c97`、`c42414b` 修复；独立 Termux 真实 PTY/tmux smoke 最终通过，TUI 状态流和 SSE 安全/兼容边界成立 | 记录三个精确 SHA、实际 clean/ahead 与最终验证；仅同步三份 docs gate 状态，保留 `T04-01`、`T05-01` | Task 06 GO；Task 07 保持 `pending/WAIT` |
| 2026-09-14T22:36:00Z | 监督对 Task 07 主实现 `40fe06e` 给出 host consistency `NO-GO` | Termux 完整 Fleet 包暴露 30 秒 fixture 竞态；配置存在重复打开/TOCTOU；user Worker `Requires=` 与 unit 属性/canonical binary 证明不足 | 仅修 Task 07：长寿命隔离 sentinel、captured-bytes 安全读取、Wants+After、三项实际 unit 属性和固定 binary；恢复 T04-01/T05-01 pending 并完整重验 | Task 07 保持 `active/WAIT`；等待 review-fix 提交后的再次 gate |
| 2026-09-14T22:50:57Z | 监督确认 `f5c0d0d` hardening 与 Termux 完整 Fleet 通过，给出最终两项 `NO-GO` | umask 使 0722 fixture 实际权限收窄；init 在 binary preflight 前已写入 config/manifest | 显式 chmod 并断言 fixture mode；把 init/workspace/up builder preflight 提升到所有 Fleet 文件/tmux/systemd mutation 之前，补零副作用和 status/down 回归 | Task 07 保持 `active/WAIT`；等待最终 gate，Task 08 未开始 |
| 2026-09-14T23:02:54Z | 监督最终复核 Task 07 三个提交 | 两轮 `NO-GO` 缺口已由 `f5c0d0d`、`e13db7b` 修复；Termux 完整 Fleet/CLI/Console/Worker 和 Ubuntu systemd/tmux 证据通过 | 记录三个精确 SHA、实际 clean/ahead、双平台证据并关闭 T04-01/T05-01；仅同步三份 docs gate 状态 | Task 07 GO；Task 08 保持 `pending/WAIT` |
| 2026-09-14T23:18:02Z | Task 08 最终语义审查发现 SSE cursor fail-closed 缺口 | Agent filter/Run projection 查询错误可能被当作已处理并跨过 sequence；候选不能证明不丢状态 | 登记 `T08-01` 归属 Task 03，生成 `no-go` validation report；停止后续全量/隔离/候选/现场检查，不夹带代码修复 | Task 08 保持 `active/WAIT`；等待监督授权 Task 03 review-fix |
| 2026-09-14T23:52:33Z | 监督关闭 `T08-01` 后恢复完整矩阵；首次无缓存全仓 Go 测试发现 PTY quit 失败 | T08-01 已关闭；新 `T08-02` 使 Console 确定性 detach 证据不成立，候选仍不能发布 | 保留失败输出，未重跑；停止 race/隔离/候选/现场检查，登记 Task 06 owner | Task 08 保持 `active/WAIT` 和 `no-go`；等待监督决定 T08-02 修复 gate |

## 9. 最终产物（Task 08 填写）

- validation report：[2026-09-14-openagentx-adr008-release-candidate.md](../../reports/validation/2026-09-14-openagentx-adr008-release-candidate.md)，结论 `no-go`
- traceability matrix：已按 `origin/main..b26c9c4` 重审并写入 validation report；§9 的 `T08-01`
  已关闭，§6/§8 因 `T08-02` 阻断
- release-candidate binary：未构建；无缓存全仓测试失败后按停止规则退出
- binary SHA-256：无
- implementation HEAD：`b26c9c4928d5c1cbdd971473cecb5b535a3d39dc`
- 全量验证结论：`go test ./... -count=1` 因 `T08-02` 失败；其余已执行 Web/module/release 检查通过，
  race/vet/剩余隔离矩阵未执行，不得声称候选通过
- 只读目标主机检查：未执行；全仓测试失败后未继续接触外部状态
- 未执行的人工步骤：备份、安装、服务重启/升级、真实 `OAX` workspace 操作、正式 graceful drain
- 最终监督结论：WAIT
