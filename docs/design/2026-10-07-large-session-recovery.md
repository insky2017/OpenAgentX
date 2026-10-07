# OpenAgentX 长会话恢复记录（2026-10-07）

## 用户结果与范围

用户授权先诊断受控执行失败，再恢复 OpenAgentX 原终端，最后进行一次新的只读真实验收。
原 thread `01a0c98a-bef8-7730-9b2b-cbc2f3594559`、window `@8`、pane `%11` 已恢复；终端实际显示验收回复。
仅重启 `openagentx-worker@openagentx.service`。daemon、其他五域 Worker 和原生终端 PID 均与恢复前一致。
没有重放历史任务、取消业务任务或直接修改数据库；历史失败 Task 保持 `uncertain`。

## 故障证据与最小修复

原 Worker 的健康检查仍为 healthy，但终端已退出。首次按正式入口重新打开原 pane 后，真实 TUI 报：

```text
thread/resume failed: websocket: read limit exceeded (code -32001)
```

只读 `thread/read` 返回 33,858,086 字节、58 个 turn，超过 RPC 客户端旧上限 33,554,432 字节（32 MiB）。
`internal/runtime/codex/rpc.go` 的该客户端由 Worker 与 native bridge 共用。
修复将可信本地 Unix/loopback 传输的响应上限提高至 128 MiB，保留有限大小保护及完整历史，不修改会话记录。

部署回派 Task `task-7d8c2265-010a-4c45-b6e3-67b61041a497` 的错误为通用的
`Runtime Backend could not establish a controlled turn`。现有 `failStartedRun` 在成功记账后没有持久化底层 cause，
所以该历史 Task 与上述大小限制之间的因果关系未得到独立证明；只确认当前可复现的终端故障和修复后的正式执行恢复。
CONTINUE 最新记录说明部署收尾已由另一 Task 完成，本次没有重做该目标。

## 源码、工件与运行时

- 产品 commit：`5e82727a7b7e88192417faa4d209ab244b2a4e31`。
- 新工件 SHA-256：`4c448f6e7cdf94ccc4ad1c0c6e5de61ee4a4c3f86d70affa12dda009d5166f39`。
- 已更新正式 CLI `/home/sky/.local/bin/openagentx`，OpenAgentX Worker 与 `%11` 前台进程的实际 executable SHA 已比对。
- daemon 和其他五域仍运行上批 `c35d702` 工件；本次不声称全部服务已升级。之后从正式 CLI 新启动的进程会使用新工件。
- 原工件副本、单域恢复脚本和完整本机证据保存在
  `/home/sky/.local/state/openagentx/validation/2026-10-07-resume-timeout/openagentx-recovery-20261007T122129Z/`。

## 验证

1. 新增实际 WebSocket 传输回归测试，返回 33 MiB 历史并比对完整内容和 thread ID。旧代码明确失败于 read limit；修复后通过。
2. `go test ./internal/runtime/codex ./internal/nativebridge` 通过。
3. 最终工件先在原 `%11` 打开真实原 thread，确认 TUI 不再启动失败，再自然停止空闲目标 Worker、原子安装并启动该 Worker，通过正式 resume 刷新当前 generation 的网络就绪。
4. 六域原 thread/window/pane 与模型设置保持；其他五域和 daemon 的进程未替换。
5. 一次新 query Task `task-9284cabe-72f2-4933-a4a8-e4e84bc7ed4b`，Run `run-0eaf66cf-0c96-486b-8d82-05a9204d7fd0`：均 succeeded，completion_basis 为 query_result_delivered，deadline 为空。
6. 精确回复 `OAX_RECOVERY_READONLY_OK_20261007` 已在正式任务结果和原 TUI 中核对；完成后 Worker idle/ready。

首次原终端重开失败及旧回派失败均保留。核对历史任务时 observe 接口返回 403，随后使用该凭据有权访问的正式 Console TaskSnapshot 接口成功核对；没有改变权限或绕过接口。
本轮证明原会话可恢复、受控执行和结果投递正常，不代表业务任务效果验收，也未验证 128 MiB 以上历史或修复取消 fallback 的旧端点刷新问题。

## 可复核材料

- [真实只读验收](../reports/validation/2026-10-07-large-session-recovery/evidence/readonly-verification-result.json)
- [首次重开失败](../reports/validation/2026-10-07-large-session-recovery/evidence/first-reopen-failure.json)
- [单域恢复结果](../reports/validation/2026-10-07-large-session-recovery/evidence/recovery-result.json)
- [作用范围核对](../reports/validation/2026-10-07-large-session-recovery/evidence/scope-check.json)
- [测试结果](../reports/validation/2026-10-07-large-session-recovery/evidence/tests.json)
- [SHA 清单](../reports/validation/2026-10-07-large-session-recovery/evidence/manifest.json)
