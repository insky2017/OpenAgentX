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
5. 任务实现、测试和本文件更新必须进入同一阶段提交。提交后填写 SHA 和 `git status --short`，停止等待监督者。
6. 未经监督者明确 `GO`，下一任务不得标记为 active。
7. 外部状态变化（service、socket、DB、tmux、installed binary、remote branch）必须单列；按计划不应发生的变化一经发现立即停止。

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
| 01 | 基线隔离、契约冻结与测试地图 | completed | 本文件所属 Task 01 提交（见 feature branch tip） | WAIT |
| 02 | 默认路径与 CLI 表面 | pending | — | WAIT |
| 03 | 一致 Attach cursor 与代际 reducer | pending | — | WAIT |
| 04 | 可撤销 CLI Token 会话 | pending | — | WAIT |
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
- 任务提交 SHA：本文件位于该提交内，Git 提交无法自包含自身 SHA；精确 SHA 由提交后的
  feature branch tip 提供，并在最终报告及下一阶段 append-only 记录中引用
- `git status --short --branch`：提交后应为
  `## codex/adr008-implementation...origin/main [ahead 1]` 且无 path 条目；提交后必须复核
- 退出条件逐项：P0 已独立推送；sibling worktree 基于最新远端；七类契约、TUI 决策、五个
  设计问题和 Task 02-08 测试地图已记录；计划内定向测试通过；产品行为未修改
- 剩余问题：Task 01 无阻断问题；Charmbracelet 最新版本与 Go 1.22.4 不兼容，已通过固定
  兼容版本解决，不允许后续无审查升级
- 执行者建议：GO
- 监督者复核：WAIT
- 下一步：停止，等待监督者明确指令

## 7. Open Issues

| ID | 首次发现时间 | Task | 严重度 | 问题 | Owner | 状态/处置 |
|---|---|---|---|---|---|---|
| — | — | — | — | 当前无已登记实施问题 | — | — |

## 8. 安全与范围事件

| 时间 UTC | 事件 | 影响 | 立即动作 | 监督结论 |
|---|---|---|---|---|
| 2026-09-14T14:16:32Z | 首次按 `origin` HTTPS URL 推送时因无交互凭据失败；远端未改变 | 无产品/运行状态影响 | 改用仓库既有 GitHub SSH 身份推送，并以 `ls-remote` 验证 `main=16291c7` | 计划发布成功；保留失败记录 |
| 2026-09-14T14:16:32Z | 计划创建与发布仅改变 `docs/plans/` | 无实现或运行状态变化 | `git diff --check`、链接/语义覆盖和 staged-path 边界检查通过 | 待远端同步复核 |
| 2026-09-14T14:30:35Z | P0 四项受保护修改独立提交并推送 `main` | 远端 main 前进到 `c3fc1bb`；无 ADR-008 文件或运行状态修改 | `ls-remote` 验证 SHA，确认主工作树干净 | P0 GO |
| 2026-09-14T14:31:31Z | 从最新 `origin/main` 创建 sibling worktree 和 `codex/adr008-implementation` | ADR-008 文档工作与 main/父仓隔离 | 核对 branch、HEAD、tracking 和 clean status | Task 01 执行中，门禁 WAIT |

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
