---
doc_type: test_task
status: passed
owner: openagentx
test_id: T04
updated_at: 2026-09-01
---

# T04：AGY Worker 真实连续任务闭环

## 目标

使用真实 `agy-batch` Adapter 重复 T03，验证 Agent CLI 按 turn 启动而 Resident Worker 长期在线。

## 前置 Runtime Contract Gate

在执行业务任务前，使用正式 `agy-graft` 和 `set_proxy_server` 环境记录并验证：版本、argv、stdin/NDJSON、stdout/stderr、退出码、超时、工作目录、模型、reasoning effort、权限及代理变量。未通过该 Gate，不得把 AGY 结果计入 T04。

## 步骤

1. 验证 AGY binary、模型凭据、workspace 和 AgentProfile。
2. 启动 `quote-service` Worker，确认 Backend health online。
3. 提交明确、可逆、可检查的 Task A，核对 AGY 输入、输出和 ResolvedExecutionSpec。
4. turn 完成后确认 AGY 子进程释放、Worker 保持 online。
5. 不通过 tmux 发送 Task B，确认自动唤醒和独立 RunAttempt。
6. 验证超时、AGY 非零退出、输出解析失败均形成确定 Task/Run 状态。
7. 由于 T05 修复共享 Worker/Session/鉴权逻辑，重新执行 T03 的核心连续任务断言，确认 T04 历史结论未被回归破坏。

## 通过条件

真实 AGY 连续完成两次工作，Runtime Contract Gate 证据完整，模型/推理参数按 ExecutionSpec 生效，Worker 生命周期明显长于两个 turn，且日志中不存在 tmux 控制路径。T05 共享修复后的回归也必须通过。

## 验证结果

- Task A `task-86aec3e6-a87a-4ab6-aac4-c59174d97ae3` 与 Task B `task-874ebc71-6fd2-4ada-a618-cf1d867ea7a0` 均由真实 `agy-graft` turn 完成并进入 `succeeded`；
- 两个 Task 分别形成独立 RunAttempt，但共用 WorkerInstance `worker-c8c03350-4b43-4690-b283-ab48c435cfad`、generation `9` 和 fencing token `17`；
- Mailbox sequence `16`、`17` 均只领取一次；Task A 结束后 Worker 持续 heartbeat，Task B 由持久 Mailbox 自动唤醒；
- Runtime Event 证明实际模型为 `gemini-3.7-flash-low`，工作目录为 SteadyFlow 根目录；`backend_default` 推理模式按 ExecutionSpec 正确省略显式 effort；
- 自动化覆盖超时、非零退出、空流、畸形流、缺失终态和未知副作用，均保持 fail closed；
- 详细证据：[T04 AGY Resident Worker 连续任务验证报告](../../reports/validation/2026-08-31-openagentx-adr001-t04-resident-agy-e2e.md)。

## 共享代码回归

- T05 共享 Worker、SessionBinding 和鉴权修复后的二进制 SHA-256 为 `7959f659...ca651b`；
- 新构建通过正式 HTTPS 创建真实 `agy-graft` Task `task-45ff30d2-f24b-4834-9424-7dab3880b516`，Task 进入 `succeeded`；
- RunAttempt `run-113caee5-02f4-48f2-a5c9-2ba18b93ddd5` 使用模型 `gemini-3.7-flash-low`，实际结果确认工作目录为 SteadyFlow 根目录；
- daemon 与 quote-service Worker 重启后均为 `active`，Worker generation `11`、fencing token `21`、UDS 权限 `0600`。
