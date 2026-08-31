---
doc_type: validation_report
status: passed
owner: openagentx
test_id: T04
validated_at: 2026-08-31
---

# OpenAgentX ADR-001 T04 AGY Resident Worker 连续任务验证

## 结论

T04 通过。同一逻辑 `quote-service` Domain Agent 由一个 Resident Worker 连续完成两个真实 `agy-graft` turn。Task A 完成后 Worker 未退出，继续通过 UDS long poll 等待持久 Mailbox；Task B 无需 tmux、pane 或人工终端注入即被自动领取并完成。

## 环境与基线

| 检查 | 结果 |
|---|---|
| 源码基线 | `6c103e54d086beed8752ef912935f4669741efe1` 加本报告同批 T04 修复 |
| Task A/B 执行二进制 SHA-256 | `68402bec041fea86b777a85ce88c2360af4ab4fcd429a6e90cb94119bdf1a9aa` |
| 最终部署二进制 SHA-256 | `71f24dd6d23c777aa4f899b126abbfb239d6605c8f87ef3a2ed6e8bdb5e3b992` |
| SQLite schema | `schema_meta.version=1` |
| daemon / Worker | `openagentx.service`、`openagentx-quote-service-worker.service`，均为 `active/running` |
| Worker transport | 本机 Unix Socket `run/openagentx.sock` |
| Runtime | `agy-graft`，Adapter `agy-batch`，Backend `primary` |
| Runtime 配置 | model `gemini-3.7-flash-low`、reasoning `backend_default`、timeout `30m` |
| 冻结 ADR-001 | SHA-256 `587e9b8f27ddeb68aae8e5a507fefc10973739c8dd2c3def86e2c0bbc51db403`，无 diff |

最终部署二进制比 Task A/B 执行版本多一项 parser fail-closed 修正：显式 `side_effects_known=false` 不得被成功状态覆盖。该修正经定向和全量自动化验证，未重复消耗真实模型任务。

## 连续任务证据

| 断言 | Task A | Task B |
|---|---|---|
| Task | `task-86aec3e6-a87a-4ab6-aac4-c59174d97ae3` | `task-874ebc71-6fd2-4ada-a618-cf1d867ea7a0` |
| Task 状态 | `succeeded` | `succeeded` |
| Mailbox | sequence `16`、`accepted`、attempts `1` | sequence `17`、`accepted`、attempts `1` |
| RunAttempt | `run-71e75ec3-d2ed-4cba-b8a0-4a6cd08c70bc` | `run-45a8d0b6-085b-456c-99ec-8dc1bbd2e998` |
| Run 状态 | `succeeded` | `succeeded` |
| WorkerInstance | `worker-c8c03350-4b43-4690-b283-ab48c435cfad` | 同左 |
| generation / fencing | `9 / 17` | `9 / 17` |
| 实际只读结果 | `pwd` 为 SteadyFlow 根目录；README 首行为 `# OpenAgentX` | `pwd` 为 SteadyFlow 根目录；go.mod 首行为 `module openagentx` |

Task A 的核心 Event Journal sequence 为 `14334` 至 `14353`，Task B 为 `14363` 至 `14381`。两条链路均包含：

```text
task.created
task.running
run_attempt.started
runtime.agy.init
runtime.agy.step_update
runtime.agy.result
run_attempt.finished
task.settled
```

Task A 在 `03:07:19Z` settled 后，Worker heartbeat 持续到 Task B 于 `03:08:47Z` 创建；Task B 后继续 heartbeat。两个 turn 结束时均无 active RunAttempt 和 pending MailboxItem，证明 Worker 生命周期长于 Task 与 turn。

## Runtime 与 workspace 证据

- `resolved_execution_json` 记录 Adapter `agy-batch`、Backend `primary`、model `gemini-3.7-flash-low`、reasoning `backend_default` 和 timeout `30m`；
- `runtime.agy.init` 记录真实模型 `gemini-3.7-flash-low` 和 `cwd=/home/sky/work/touzi/OneAxe/steadyflow`；
- 两个 turn 都实际调用 `run_command` 执行 `pwd`，结果均为 SteadyFlow 根目录；
- workspace 由 Worker/Adapter 配置并注入 Runtime Context，结果经 `agy.init` 与真实工具输出验证。该证据不宣称 OpenAgentX 对每个 Agent CLI 工具调用存在协议级 `Cwd` 强制；
- `backend_default` 是合法的 ResolvedExecutionSpec，Adapter 按契约不传 `--effort`；`effort=low|medium|high` 的 argv 映射由定向测试覆盖。

## Wait、Wake 与资源占用

- Worker Control 修复为“初查、订阅、重查、Broker wakeup、超时末次查库”，并在领取事务中校验 session token、expiry、principal、generation、fencing 和 Worker lease/status；
- 修复前现场 busy loop 为 daemon 约 `171% CPU`、Worker 约 `21%-24% CPU`；修复后跨 long-poll 周期采样不再出现每秒数千次空请求；
- 最终二进制部署后的 10 秒 PID 定向采样：daemon 平均 `0.60% CPU`，Worker `0.00% CPU`；
- Task B 来自持久 Mailbox sequence `17`，没有 TmuxConnector、pane 地址、`paste-buffer` 或 `send-keys` 控制路径。

## Fail-closed 与回归验证

自动化覆盖以下失败边界：AGY timeout、非零退出、空 stdout、畸形 JSON、缺失终态、stderr 超限/脱敏、错误 session/generation/fencing、过期 token/lease、revoke 和丢失 Broker wakeup。无法确认结果或副作用时保持 `uncertain` 且 `side_effects_known=false`。

```text
go test -count=1 ./internal/runtime/agy ./internal/cli/worker ./internal/worker  PASS
go test -count=1 ./...                                                    PASS
go vet ./...                                                             PASS
git diff --check                                                         PASS
ADR-001 git diff --exit-code                                             PASS
```

最终部署后 Worker 以 `generation=10 / fencing=19` 恢复 online，旧 generation 9 正确转为 offline；active RunAttempt 和 pending MailboxItem 均为 0。受控重启期间新 Worker 在旧 lease 到期前拒绝抢占，随后完成安全接管。

## 判定边界

T04 证明真实 AGY 的连续 Task、Resident Worker wait/wake、ExecutionSpec 到 Runtime 的配置生效和主要失败路径 fail-closed。运行中 Message/queued steer、Cancel/Approval 竞态、故障恢复和远程 Worker 分别由 T05-T08 继续验证，不在本报告中提前宣称通过。
