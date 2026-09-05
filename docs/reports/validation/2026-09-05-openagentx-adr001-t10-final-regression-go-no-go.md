---
doc_type: validation_report
status: passed
owner: openagentx
test_id: T10
validated_at: 2026-09-05
---

# OpenAgentX ADR-001 T10 全量回归与 Go/No-Go

## 结论

`GO`。首轮生产二进制来源阻塞已由 Sol high 从 clean `fa4d35b` 构建并原子部署；terra high 独立复验确认磁盘目标、daemon/Worker `/proc` 产物 SHA 一致，VCS revision exact 为 `fa4d35b` 且 `vcs.modified=false`。此前 clean worktree 全量/race/vet/build/legacy、Runtime contract、T05/T06、数据一致性和生产入口证据仍有效，本轮最小 B/E/F 复验全部通过。

## A. 基线与边界

| 检查 | 结果 |
| --- | --- |
| 候选源码 | `fa4d35bea0dccdc0adeccbc5e404bc590c0a1c25` (`fa4d35b`) |
| ADR-001 SHA-256 | `587e9b8f27ddeb68aae8e5a507fefc10973739c8dd2c3def86e2c0bbc51db403`，无修改 |
| clean release worktree | `/tmp/openagentx-t10-clean-WuQP9F`，detached HEAD，工作树干净 |
| T01-T09 | 计划和对应报告均为 `passed`；T09 手机 PWA 安装引用 owner attestation，不重复安装 |
| 主 checkout 预存用户文件 | `docs/decisions/README.md`、`.planning/`、`docs/decisions/ADR-002-agent-cli-proxy-environment.md`、3 个 2026-08-31 T04 文档；未修改、未暂存 |

## B. 自动化回归（clean worktree）

`npm ci --prefix web` 依据现有 `package-lock.json` 安装依赖，未修改 lockfile。

| 命令 | 结果 | 耗时 |
| --- | --- | ---: |
| `go test ./...` | PASS，所有包 | 10.79s |
| `go test -race ./...` | PASS，0 race | 17.22s |
| `go vet ./...` | PASS | 0.93s |
| `npm ci --prefix web` | PASS，22 packages，0 vulnerabilities | 2.34s |
| `npm --prefix web run build` | PASS，Vite 8.2.2 | 0.95s |
| `./scripts/check-legacy-control-paths.sh --release` | PASS，所有类别 CLEAN | 0.13s |
| `git diff --check` | PASS | 0.00s |

## C. 契约与强制矩阵

- 正式 `agy-graft --version`：rc 0，版本 `1.1.26`；`agy-graft --help`：rc 0，包含 `--print`、`--input-format`、`--output-format`、`--conversation`、`--model`、`--effort`、`--print-timeout`、`--dangerously-skip-permissions`。
- `OPENAGENTX_AGY_HELP_CONTRACT=1 go test ./internal/runtime/agy -run '^TestRealAgyHelpContract$' -count=1`：PASS（0.89s）。只调用 `agy-graft`，未调用 `agy`，未发起真实模型 turn。
- T05 最小矩阵 PASS（3.49s）：`TestCreateMessageRoutesByActiveBackendCapability`、`TestCreateMessageDoesNotRouteToAnotherTasksActiveRun`、`TestCreateMessageRejectsUnsupportedAndRollsBack`、`TestCreateMessageCommandReplayIsIdempotent`、`TestCreateMessageConcurrentReplayCreatesOneDelivery`、`TestSupersededControlMessageDefersSameMailboxItem`、`TestWorkerFinishWaitingInputKeepsTaskEligibleForQueuedFollowUp`、`TestWorkerAuthenticationLeaseGenerationFencingAndAgentBindingFailClosed`、`TestWorkerControlLongPollRechecksDatabaseWhenWakeupIsLost`。
- T06 最小矩阵 PASS（5.22s）：`TestCancelAndFinishLinearizeInBothOrders100Times`、`TestConcurrentCancelAndFinishBarrierBothOrders100Times`、`TestConcurrentDuplicateCancelCreatesOneControlItem`、`TestNativeApprovalAndFinishLinearizeInBothOrders100Times`、`TestConcurrentNativeApprovalAndFinishBarrierBothOrders100Times`、`TestConcurrentDuplicateNativeApprovalCreatesOneDecisionAndControlItem`、`TestNativeApprovalCannotOutliveTaskCancelAndRejectIsDelivered`、`TestPreflightApprovalIsScopedAndConsumedOnce`、`TestBeginAttemptAtomicallyConsumesMatchingPreflightApprovalOnce`、`TestTurnCompletionRacesAllControlOperationsWithoutDeadlockOrDuplicateFinish`、`TestCancelRequestedPreventsBeginAttempt`、`TestApprovalScopeContracts`。

## D. 生产入口与浏览器（只读）

- HTTP `301` 到 HTTPS；最终 HTTPS `200`，TLS verify result `0`；HSTS、`nosniff`、Referrer-Policy、Permissions-Policy 和 CSP 均存在。
- `/manifest.webmanifest` 返回 `200`、`application/manifest+json`，`display=standalone`；`/sw.js` 返回 `200` 静态 Service Worker。
- SSE `/api/observe/v1/events/stream?after_sequence=0` 未认证返回 `401`；未认证 Panel/Worker API 按预期 `401` 或 `404`。
- 真实 `agent-browser` 新会话完成首页 DOM、控制台/页面错误和 `412x915` 渲染检查：manifest `/manifest.webmanifest`、Service Worker `/sw.js`、standalone `false`（该会话未安装）。无安全可用的 owner 密码，未猜测或输出凭据；认证双 Session、离线写保护和手机 OS 安装引用 T09 L4/owner attestation。

## E. 运行与部署

| 项目 | 实际结果 |
| --- | --- |
| `openagentx.service` | active/running，MainPID `2158238`，NRestarts `0` |
| `openagentx-quote-service-worker.service` | active/running，MainPID `2159442`，NRestarts `0` |
| UDS | `run/openagentx.sock`，socket，owner `sky:sky`，mode `0600` |
| 在线 Worker | 1 个 `quote-service`，generation `27`，lease future，heartbeat 约 9s |
| active RunAttempt | 0 |
| idle 采样 | daemon 约 0.1% CPU、Worker 0.0%，15s 内无 busy loop 迹象 |
| schema | `schema_meta.version=1` |

### 首轮 P0 产物追溯失败（已修复）

历史运行中的 `/home/sky/work/touzi/OneAxe/steadyflow/OpenAgentX/bin/openagentx` SHA-256 为 `03ea07333ffa8e59b4649387f4114c8e0ba62321cb85952c7a9675f15665457a`；其 `vcs.revision` 不在当前对象库且 `vcs.modified=true`，因此首轮判定 NO-GO。该产物已保留为回滚备份，未执行回滚。

### P0 修复/重新部署证据

- 从 clean detached `fa4d35bea0dccdc0adeccbc5e404bc590c0a1c25` 构建；由于 submodule worktree 的 `.git` 为文件，Go 1.22.4 不会注入 VCS 信息，故使用同一对象库的临时 VCS-aware clean clone 构建，源工作树状态为空。
- 构建命令类别：`go build -buildvcs=true -o <temporary-artifact> ./cmd/openagentx`；临时产物通过 `--help` smoke。
- 临时产物 SHA-256：`05b4c46deaad2cdb4a7fb39c95f360f33b58006e5b833a110e155c3261bc0234`。
- 临时产物和已部署目标的 `go version -m` 均为 `vcs.revision=fa4d35bea0dccdc0adeccbc5e404bc590c0a1c25`、`vcs.modified=false`；磁盘目标、daemon `/proc` 和 Worker `/proc` SHA 三者一致。
- 旧产物已备份至 `/home/sky/work/touzi/OneAxe/steadyflow/OpenAgentX/bin/openagentx.pre-t10-p0-20260905T083731+0800`，未执行回滚。
- 已按 daemon → Worker 顺序受控重启；两服务 active/running、`NRestarts=0`，Worker 在线、generation 27、`primary=healthy`，active run 为 0，未观察到 panic/409/restart storm。
- terra high 独立复验：磁盘目标及 daemon/Worker `/proc` SHA 均为 `05b4c46deaad2cdb4a7fb39c95f360f33b58006e5b833a110e155c3261bc0234`；`go version -m` 为 exact `vcs.revision=fa4d35bea0dccdc0adeccbc5e404bc590c0a1c25`、`vcs.modified=false`；30 秒低频窗口心跳从 `00:47:05Z` 推进至 `00:47:35Z`，服务保持 active、NRestarts 0、backend `primary=healthy`、active run 0。
- terra high 独立复验同时通过 daemon `schema verify`、health/HTTPS、ADR hash、旧产物备份存在性和日志中的无 panic/409/restart storm 检查。

## F. 数据与恢复

- 正式 SQLite 以 `sqlite3 -readonly` 查询：`PRAGMA integrity_check=ok`，schema version `1`，event journal `58737` 条；在线 Worker 1 个、active run 0。
- 通过 SQLite `.backup` 只读入口建立临时副本；副本 `integrity_check=ok`、schema version `1`、event journal `58742`，临时副本及 WAL/SHM 文件均已清理。
- 当前库存在 1 个历史 `test-fake-multiturn` 目标 pending work mailbox，未发现对应在线 Worker；作为 P2 残留观察，不直接改正式库。

## G. 安全与遗留路径

- `check-legacy-control-paths.sh --release` 与有效代码/config/docs 搜索均未发现可执行 `agentbus`、`tmux/pane`、`Stop Gate` fallback。
- tracked 内容秘密模式搜索仅命中测试 fixture password 和 Worker credential/token 变量或 SQL 字段；未发现凭据、Cookie、Session Token、代理凭据或私钥内容。报告不回显敏感值。

## 失败清单与唯一修复批次建议

1. **P2 residual：** 正式库保留 1 个历史 `test-fake-multiturn` pending mailbox；该身份只有 offline fixture Worker 和 1 个 queued fixture task，不属于 `quote-service` 生产调度，未影响在线 Worker、active run 或 backend health。不得直接改 SQLite；后续可经正式控制面入口归档，当前不阻断 GO。

T10 核心门槛均通过，判定 `GO`。本报告与证据将与 T10 子计划、总测试计划作为唯一限定提交；主 checkout 其他用户文件不纳入提交。

## 本批次文件边界

- 新增：本报告及 `docs/reports/validation/evidence/t10/20260905/summary.txt`。
- 未修改：生产代码、配置、ADR-001、T01-T09 报告、主 checkout 其他预存用户文件。
- 提交边界：仅本报告、T10 证据、T10 子计划和总测试计划；主 checkout 其他用户文件保持原样。
