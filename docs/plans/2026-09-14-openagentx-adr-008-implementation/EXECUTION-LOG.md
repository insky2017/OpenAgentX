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
| 05 | `OAX` workspace 与非破坏绑定 | pending | — | WAIT |
| 06 | Console 主菜单、Agent selector 与全屏 TUI | pending | — | WAIT |
| 07 | Fleet、user-systemd 与默认 profile 集成 | pending | — | WAIT |
| 08 | 集成审查、实机候选与发布门禁 | pending | — | WAIT |

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

## 7. Open Issues

| ID | 首次发现时间 | Task | 严重度 | 问题 | Owner | 状态/处置 |
|---|---|---|---|---|---|---|
| T04-01 | 2026-09-14T17:45:58Z | 07 | P2 | Fleet down/force-stop 仍是 Task 07 的 credential 集成范围；Task 04 后共享 client 的旧直接密码 Login 会在网络前 fail closed，避免从 Fleet 向 UDS login 发送密码或替换 Console Token | Task 07 | pending；不阻断 Task 04 安全边界 |

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

## 9. 最终产物（Task 08 填写）

- validation report：待填
- traceability matrix：待填
- release-candidate binary：待填
- binary SHA-256：待填
- implementation HEAD：待填
- 全量验证结论：待填
- 只读目标主机检查：待填
- 未执行的人工步骤：备份、安装、服务重启/升级、真实 `OAX` workspace 操作、正式 graceful drain
- 最终监督结论：WAIT
