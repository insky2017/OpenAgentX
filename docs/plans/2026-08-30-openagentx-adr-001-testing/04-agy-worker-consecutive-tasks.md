---
doc_type: test_task
status: pending
owner: openagentx
test_id: T04
updated_at: 2026-08-30
---

# T04：AGY Worker 真实连续任务闭环

## 目标

使用真实 `agy-batch` Adapter 重复 T03，验证 Agent CLI 按 turn 启动而 Resident Worker 长期在线。

## 步骤

1. 验证 AGY binary、模型凭据、workspace 和 AgentProfile。
2. 启动 `quote-service` Worker，确认 Backend health online。
3. 提交明确、可逆、可检查的 Task A，核对 AGY 输入、输出和 ResolvedExecutionSpec。
4. turn 完成后确认 AGY 子进程释放、Worker 保持 online。
5. 不通过 tmux 发送 Task B，确认自动唤醒和独立 RunAttempt。
6. 验证超时、AGY 非零退出、输出解析失败均形成确定 Task/Run 状态。

## 通过条件

真实 AGY 连续完成两次工作，模型/推理参数按 ExecutionSpec 生效，Worker 生命周期明显长于两个 turn，且日志中不存在 tmux 控制路径。
